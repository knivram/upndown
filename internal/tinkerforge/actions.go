package tinkerforge

import (
	"log/slog"
	"time"

	"github.com/Tinkerforge/go-api-bindings/distance_ir_v2_bricklet"
)

const (
	// Heights in millimeters as reported by the distance IR sensor. A larger
	// distance reading means the platform is higher up.
	PositionUp   = 1095
	PositionDown = 670

	// callbackPeriodMs is how often (in ms) the sensor firmware evaluates the
	// stop threshold while a move is in progress. Small enough to stop promptly,
	// large enough not to flood the connection. Streaming is fully off when idle.
	callbackPeriodMs = 25

	// maxMoveDuration force-stops the relay if a move never reaches its target,
	// preventing a runaway motor.
	maxMoveDuration = 30 * time.Second

	// positionTolerance is the deadband (mm) around a target within which the
	// platform is considered "already there", so a press toward a target you are
	// already at does nothing instead of making a tiny corrective move. Tunable.
	positionTolerance = 10
)

type direction int

const (
	directionUp direction = iota
	directionDown
)

func (d direction) String() string {
	if d == directionDown {
		return "down"
	}
	return "up"
}

// Relay wiring: channel0/channel1 map to the motor contactors. Both true is the
// stopped (safe) state. These values must match the physical wiring.
func (c *Client) relayUp() error   { return c.relay.SetValue(false, true) }
func (c *Client) relayDown() error { return c.relay.SetValue(true, false) }
func (c *Client) relayStop() error { return c.relay.SetValue(true, true) }

// startMove begins moving toward target. It runs only on the worker goroutine
// and only when no move is in progress. It returns true when a move was actually
// started (relay engaged and stop callback armed).
func (c *Client) startMove(target uint16) bool {
	current, err := c.sensor.GetDistance()
	if err != nil {
		slog.Error("could not read current position; ignoring request", "target", target, "err", err)
		return false
	}

	if absDiff(current, target) <= positionTolerance {
		slog.Info("already at target; nothing to do", "position", current, "target", target)
		return false
	}
	if current < target {
		return c.beginMove(current, target, directionUp)
	}
	return c.beginMove(current, target, directionDown)
}

// absDiff returns |a-b| without underflowing uint16.
func absDiff(a, b uint16) uint16 {
	if a > b {
		return a - b
	}
	return b - a
}

// beginMove arms the stop callback first, then engages the relay, so that a
// failure to configure the sensor never leaves the motor running.
func (c *Client) beginMove(current, target uint16, dir direction) bool {
	// Stream changed distance samples while moving and decide in code (below)
	// when the target is reached. The firmware "smaller-than" threshold proved
	// unreliable for downward moves — it let the platform run past the target —
	// so we compare here instead. Streaming is turned off again as soon as we stop.
	if err := c.sensor.SetDistanceCallbackConfiguration(callbackPeriodMs, true, distance_ir_v2_bricklet.ThresholdOptionOff, 0, 0); err != nil {
		slog.Error("failed to configure distance callback; aborting move", "err", err)
		return false
	}

	c.moveEpoch++
	epoch := c.moveEpoch
	c.activeCallbackID = c.sensor.RegisterDistanceCallback(func(d uint16) {
		// Defensive: the firmware threshold should already guarantee this, but
		// re-check so a stray callback can never stop the move early.
		if (dir == directionUp && d >= target) || (dir == directionDown && d <= target) {
			select {
			case c.reached <- reachEvent{epoch: epoch, at: d}:
			default: // a signal is already pending; one is enough
			}
		}
	})

	var relayErr error
	if dir == directionUp {
		relayErr = c.relayUp()
	} else {
		relayErr = c.relayDown()
	}
	if relayErr != nil {
		slog.Error("failed to engage relay; aborting move", "dir", dir.String(), "err", relayErr)
		_ = c.relayStop()
		c.disarmCallback()
		return false
	}

	c.moving = true
	slog.Info("move started", "from", current, "to", target, "dir", dir.String(), "cb", c.activeCallbackID)
	return true
}

// finishMove handles a target-crossing for the current move.
func (c *Client) finishMove(ev reachEvent) {
	if !c.moving || ev.epoch != c.moveEpoch {
		return // leftover signal from a finished or preempted move
	}
	c.teardown()
	slog.Info("move complete", "position", ev.at)
}

// abortMove stops a move that overran the safety timeout.
func (c *Client) abortMove() {
	if !c.moving {
		return
	}
	c.teardown()
	slog.Warn("move aborted by safety timeout; relay force-stopped", "after", maxMoveDuration)
}

// stopMove cancels an in-progress move. It is a no-op when nothing is moving.
func (c *Client) stopMove(reason string) {
	if !c.moving {
		return
	}
	c.teardown()
	slog.Info("move stopped", "reason", reason)
}

// teardown stops the motor and tears down the active move's callback/state.
func (c *Client) teardown() {
	if err := c.relayStop(); err != nil {
		slog.Error("failed to stop relay", "err", err)
	}
	c.disarmCallback()
	c.moving = false
}

// disarmCallback turns sensor streaming off, deregisters the active callback,
// and clears any pending reach signal.
func (c *Client) disarmCallback() {
	if err := c.sensor.SetDistanceCallbackConfiguration(0, false, distance_ir_v2_bricklet.ThresholdOptionOff, 0, 0); err != nil {
		slog.Error("failed to disable distance callback", "err", err)
	}
	c.sensor.DeregisterDistanceCallback(c.activeCallbackID)
	select { // drop any stale reach signal
	case <-c.reached:
	default:
	}
	c.activeCallbackID = 0
}
