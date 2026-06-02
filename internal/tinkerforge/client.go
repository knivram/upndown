package tinkerforge

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/Tinkerforge/go-api-bindings/distance_ir_v2_bricklet"
	"github.com/Tinkerforge/go-api-bindings/industrial_dual_relay_bricklet"
	"github.com/Tinkerforge/go-api-bindings/ipconnection"
)

const (
	Host                           = "localhost"
	Port                           = 4223
	DistanceIRV2BrickletUID        = "YZ2"
	IndustrialDualRelayBrickletUID = "2bFm"

	// requestTimeout bounds how long a blocking getter (e.g. GetDistance) waits
	// for the daemon before failing. Getters normally answer in well under 50ms;
	// 500ms cuts off a genuine daemon hang quickly without false-tripping on a
	// slow-but-alive read. It also bounds the worst-case stop latency: the worker
	// can only be blocked inside a single GetDistance poll, so a button press
	// waits at most one requestTimeout before the worker can act on it. The
	// invariant requestTimeout < sensorStaleTimeout (asserted in a test) ensures
	// one transient timeout never force-stops a healthy move.
	requestTimeout = 500 * time.Millisecond

	// commandQueueSize bounds how many button presses can be buffered while the
	// worker is briefly busy. Presses beyond this are dropped (logged) rather
	// than blocking the hotkey handler.
	commandQueueSize = 4
)

// distanceSensor is the slice of the Distance IR v2 bricklet API this package
// uses. Stop detection is done by actively polling GetDistance from the worker
// (a pull model), so the daemon pushing streamed samples is no longer on the
// critical path; only GetDistance is needed. Depending on an interface (rather
// than the concrete bricklet) keeps the movement logic unit-testable with fakes.
type distanceSensor interface {
	GetDistance() (uint16, error)
}

type relayController interface {
	SetValue(channel0 bool, channel1 bool) error
}

type Client struct {
	ipcon  *ipconnection.IPConnection
	sensor distanceSensor
	relay  relayController

	cmd      chan uint16   // button presses; buffered FIFO, processed by run()
	done     chan struct{} // closed to stop the worker
	wg       sync.WaitGroup
	stopOnce sync.Once

	// now is the clock used for the move watchdog. It is set once before run()
	// starts and is time.Now in production; tests inject a fake clock so the
	// staleness / max-duration logic is deterministic.
	now func() time.Time

	// Move tuning. Defaulted from the consts in NewClient; tests override them.
	pollInterval       time.Duration
	sensorStaleTimeout time.Duration
	maxMoveDuration    time.Duration

	// The fields below are owned exclusively by the run() goroutine and must
	// not be touched from anywhere else, so they need no locking.
	moving     bool
	moveDir    direction
	moveTarget uint16
	moveStart  time.Time // when the current move engaged the relay
}

func NewClient() *Client {
	ipcon := ipconnection.New()
	distanceIR, err := distance_ir_v2_bricklet.New(DistanceIRV2BrickletUID, &ipcon)
	if err != nil {
		slog.Error("failed to create distance IR v2 bricklet", "err", err)
		os.Exit(1)
	}
	dualRelay, err := industrial_dual_relay_bricklet.New(IndustrialDualRelayBrickletUID, &ipcon)
	if err != nil {
		slog.Error("failed to create industrial dual relay bricklet", "err", err)
		os.Exit(1)
	}
	return &Client{
		ipcon:              &ipcon,
		sensor:             &distanceIR,
		relay:              &dualRelay,
		cmd:                make(chan uint16, commandQueueSize),
		done:               make(chan struct{}),
		now:                time.Now,
		pollInterval:       defaultPollInterval,
		sensorStaleTimeout: defaultSensorStaleTimeout,
		maxMoveDuration:    defaultMaxMoveDuration,
	}
}

// Connect establishes the daemon connection, puts the hardware into a known
// safe state, and starts the movement worker.
func (c *Client) Connect() error {
	if err := c.ipcon.Connect(fmt.Sprintf("%s:%d", Host, Port)); err != nil {
		return err
	}
	c.ipcon.SetTimeout(requestTimeout)

	// The relay keeps its state across app restarts, so make sure the motor is
	// off before we start listening for commands.
	if err := c.relayStop(); err != nil {
		slog.Warn("could not stop relay on startup", "err", err)
	}

	c.wg.Add(1)
	go c.run()
	return nil
}

// Disconnect stops the worker (which force-stops the relay) and closes the
// daemon connection. It is safe to call more than once.
func (c *Client) Disconnect() {
	c.stopOnce.Do(func() {
		close(c.done)
		c.wg.Wait()
		if c.ipcon != nil {
			c.ipcon.Disconnect()
		}
	})
}

// GoTo handles a button press for the given target height. It never blocks. The
// worker decides what the press means: when idle it starts a move toward the
// target; when a move is already in progress the press stops it where it is and
// the target is ignored (press again to start a new move).
func (c *Client) GoTo(target uint16) {
	select {
	case c.cmd <- target:
		slog.Debug("button press queued", "target", target)
	default:
		slog.Warn("command queue full; dropping button press", "target", target)
	}
}

// run is the single owner of all hardware I/O. Everything that talks to the
// bricklets happens here, so a slow or timing-out call can never block the
// hotkey handlers for longer than one requestTimeout.
//
// A move is watched by actively polling the sensor on a ticker (see
// checkProgress); the ticker only fires while a move is in progress and is torn
// down on every move-ending path. There are no asynchronous hardware callbacks,
// so no move state needs epoch-tagging or locking.
func (c *Client) run() {
	defer c.wg.Done()

	var (
		poll          *time.Ticker
		pollC         <-chan time.Time // nil while idle, so that case never selects
		lastGood      time.Time        // time of the last good reading of the current move
		readErrStreak bool
	)
	// stopPolling tears down the move ticker. It is idempotent and is called on
	// every path that clears c.moving.
	stopPolling := func() {
		if poll != nil {
			poll.Stop()
			poll = nil
		}
		pollC = nil
		readErrStreak = false
	}

	for {
		select {
		case <-c.done:
			stopPolling()
			if c.moving {
				c.stopMove("shutdown")
			} else {
				c.forceStop() // leave the relay in the known-safe state
			}
			return

		case target := <-c.cmd:
			switch {
			case c.moving:
				// A press while moving means "stop here" — do not start a new move.
				c.stopMove("button press")
				stopPolling()
			case c.startMove(target):
				// A move only ever starts from idle, so poll is nil here.
				// beginMove already recorded c.moveStart; seed the staleness
				// clock from it so the watchdog measures from the move's start.
				lastGood = c.moveStart
				readErrStreak = false
				poll = time.NewTicker(c.pollInterval)
				pollC = poll.C
			}

		case <-pollC:
			res := c.checkProgress(c.moveStart, &lastGood)
			if res.readErr != nil {
				if !readErrStreak {
					slog.Debug("distance read failed during move; will force-stop if it persists", "err", res.readErr)
					readErrStreak = true
				}
			} else {
				readErrStreak = false
			}
			switch res.action {
			case moveReached:
				c.finishMove(res.at)
				stopPolling()
			case moveStale:
				c.abortMove("sensor_stale", res.elapsed)
				stopPolling()
			case moveTimedOut:
				c.abortMove("max_duration", res.elapsed)
				stopPolling()
			case moveContinue:
				// keep moving
			}
		}
	}
}
