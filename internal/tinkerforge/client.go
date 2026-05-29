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
	// for the daemon before failing. The binding default is 2.5s; a shorter
	// timeout lets the movement worker recover quickly from a sensor hiccup
	// instead of stalling. Getters normally answer in well under 50ms.
	requestTimeout = 1 * time.Second

	// commandQueueSize bounds how many button presses can be buffered while the
	// worker is briefly busy. Presses beyond this are dropped (logged) rather
	// than blocking the hotkey handler.
	commandQueueSize = 4
)

// distanceSensor and relayController are the slices of the Tinkerforge bricklet
// APIs this package actually uses. Depending on interfaces (rather than the
// concrete bricklet types) keeps the movement logic unit-testable with fakes.
type distanceSensor interface {
	GetDistance() (uint16, error)
	SetDistanceCallbackConfiguration(period uint32, valueHasToChange bool, option distance_ir_v2_bricklet.ThresholdOption, min uint16, max uint16) error
	RegisterDistanceCallback(func(uint16)) uint64
	DeregisterDistanceCallback(uint64)
}

type relayController interface {
	SetValue(channel0 bool, channel1 bool) error
}

// reachEvent is sent by the distance callback when the configured target has
// been crossed. epoch identifies the move it belongs to so the worker can
// ignore signals left over from an already-finished or preempted move.
type reachEvent struct {
	epoch uint64
	at    uint16
}

type Client struct {
	ipcon  *ipconnection.IPConnection
	sensor distanceSensor
	relay  relayController

	cmd      chan uint16     // button presses; buffered FIFO, processed by run()
	reached  chan reachEvent // target-crossed signal from the sensor callback
	done     chan struct{}   // closed to stop the worker
	wg       sync.WaitGroup
	stopOnce sync.Once

	// The fields below are owned exclusively by the run() goroutine and must
	// not be touched from anywhere else, so they need no locking.
	moving           bool
	activeCallbackID uint64
	moveEpoch        uint64
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
		ipcon:   &ipcon,
		sensor:  &distanceIR,
		relay:   &dualRelay,
		cmd:     make(chan uint16, commandQueueSize),
		reached: make(chan reachEvent, 1),
		done:    make(chan struct{}),
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
// hotkey handlers.
func (c *Client) run() {
	defer c.wg.Done()

	// safety fires if a move never reaches its target (e.g. mechanical fault),
	// forcing the relay off. It is nil while idle, so that case never selects.
	var safety <-chan time.Time

	for {
		select {
		case <-c.done:
			c.stopMove("shutdown")
			if err := c.relayStop(); err != nil {
				slog.Error("failed to stop relay on shutdown", "err", err)
			}
			return
		case target := <-c.cmd:
			switch {
			case c.moving:
				// A press while moving means "stop here" — do not start a new move.
				c.stopMove("stopped by button press")
				safety = nil
			case c.startMove(target):
				safety = time.After(maxMoveDuration)
			default:
				safety = nil
			}
		case ev := <-c.reached:
			c.finishMove(ev)
			safety = nil
		case <-safety:
			c.abortMove()
			safety = nil
		}
	}
}
