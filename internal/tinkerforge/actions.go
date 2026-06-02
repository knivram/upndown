package tinkerforge

import (
	"log/slog"
	"time"
)

const (
	// Heights in millimeters as reported by the distance IR sensor. A larger
	// distance reading means the platform is higher up.
	PositionUp   = 1095
	PositionDown = 670

	// positionTolerance is the deadband (mm) around a target within which the
	// platform is considered "already there", so a press toward a target you are
	// already at does nothing instead of making a tiny corrective move. Tunable.
	positionTolerance = 10

	// Move-watchdog defaults, copied into Client fields by NewClient so tests can
	// override them. They must satisfy
	//   pollInterval < requestTimeout < sensorStaleTimeout < maxMoveDuration
	// (asserted by TestWatchdogConstantsAreOrdered).
	//
	// defaultPollInterval is how often the worker polls GetDistance while moving:
	// small enough to stop promptly, large enough to leave the socket idle between
	// polls so a stop command is not queued behind a poll.
	defaultPollInterval = 50 * time.Millisecond
	// defaultSensorStaleTimeout force-stops a move if no fresh reading arrives for
	// this long (= 3 × requestTimeout, so a couple of transient timeouts do not
	// abort a healthy move, but a genuinely dead sensor never runs the platform
	// blind). This is the fast safety path.
	defaultSensorStaleTimeout = 1500 * time.Millisecond
	// defaultMaxMoveDuration force-stops a move that never reaches its target
	// (mechanical fault, unreachable target). Tighter than the old 30s — a normal
	// full-travel move takes ~10s, so 15s is a real backstop rather than a value
	// looser than a normal move.
	defaultMaxMoveDuration = 15 * time.Second
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
//
// SetValue on the Industrial Dual Relay is configured response-not-expected in
// the bindings: it is fire-and-forget, so relayStop never blocks and (barring an
// encode/connection error) never reports failure. A nil return therefore means
// "the stop command was queued", not "the motor is confirmed off" — the safety
// guarantee comes from the worker deciding to stop promptly (polling + watchdog),
// not from confirming the relay. Do not "helpfully" add a retry: there is no ack
// to retry on, and TCP already delivers the queued command.
func (c *Client) relayUp() error   { return c.relay.SetValue(false, true) }
func (c *Client) relayDown() error { return c.relay.SetValue(true, false) }
func (c *Client) relayStop() error { return c.relay.SetValue(true, true) }

// forceStop drives the relay to the safe state and logs if the (rare) error path
// fires. It is the single stop used by every move-ending path.
func (c *Client) forceStop() {
	if err := c.relayStop(); err != nil {
		slog.Error("failed to stop relay", "err", err)
	}
}

// reachedTarget reports whether reading d has reached target for the given
// direction. Up moves increase the distance reading, down moves decrease it.
func reachedTarget(dir direction, d, target uint16) bool {
	if dir == directionUp {
		return d >= target
	}
	return d <= target
}

// startMove begins moving toward target. It runs only on the worker goroutine
// and only when no move is in progress. It returns true when a move was actually
// started (relay engaged). The single GetDistance here is the only blocking call
// on the idle path; a press queued during it waits at most one requestTimeout.
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
	dir := directionUp
	if current > target {
		dir = directionDown
	}
	return c.beginMove(current, target, dir)
}

// absDiff returns |a-b| without underflowing uint16.
func absDiff(a, b uint16) uint16 {
	if a > b {
		return a - b
	}
	return b - a
}

// beginMove engages the relay and records the move so the worker's poll loop can
// watch it. It does no sensor I/O — stop detection is done by polling. The move
// state (including moveStart, which seeds the watchdog clock) is recorded before
// the relay engages, so it is fixed the moment the platform can begin to move.
func (c *Client) beginMove(current, target uint16, dir direction) bool {
	c.moveDir = dir
	c.moveTarget = target
	c.moveStart = c.now()

	var relayErr error
	if dir == directionUp {
		relayErr = c.relayUp()
	} else {
		relayErr = c.relayDown()
	}
	if relayErr != nil {
		slog.Error("failed to engage relay; aborting move", "dir", dir.String(), "err", relayErr)
		_ = c.relayStop()
		return false
	}

	c.moving = true
	slog.Info("move started", "from", current, "to", target, "dir", dir.String())
	return true
}

// moveAction is the outcome of a single poll of an in-progress move.
type moveAction int

const (
	moveContinue moveAction = iota
	moveReached
	moveStale
	moveTimedOut
)

// pollResult carries a poll outcome plus the data the worker needs to log it.
type pollResult struct {
	action  moveAction
	at      uint16        // last reading (valid for moveReached / good moveContinue)
	elapsed time.Duration // move duration (timeout) or time since last good read (stale)
	readErr error         // non-nil when this poll's GetDistance failed
}

// checkProgress performs one poll of an in-progress move and decides what to do.
// It is called only by the worker. It updates *lastGood on a successful read so
// the staleness watchdog measures the gap since the last *good* reading.
// Durations are evaluated against c.now() (the injected clock), which makes the
// watchdog deterministic in tests.
func (c *Client) checkProgress(moveStart time.Time, lastGood *time.Time) pollResult {
	now := c.now()
	if elapsed := now.Sub(moveStart); elapsed >= c.maxMoveDuration {
		return pollResult{action: moveTimedOut, elapsed: elapsed}
	}
	d, err := c.sensor.GetDistance()
	if err != nil {
		if stale := now.Sub(*lastGood); stale >= c.sensorStaleTimeout {
			return pollResult{action: moveStale, elapsed: stale, readErr: err}
		}
		return pollResult{action: moveContinue, readErr: err} // transient; lastGood unchanged
	}
	*lastGood = now
	if reachedTarget(c.moveDir, d, c.moveTarget) {
		return pollResult{action: moveReached, at: d}
	}
	return pollResult{action: moveContinue, at: d}
}

// finishMove ends a move that reached its target.
func (c *Client) finishMove(at uint16) {
	c.forceStop()
	c.moving = false
	slog.Info("move complete", "position", at, "target", c.moveTarget)
}

// abortMove force-stops a move that the watchdog ended (sensor went stale or the
// max duration elapsed). reason distinguishes the two production faults.
func (c *Client) abortMove(reason string, after time.Duration) {
	c.forceStop()
	c.moving = false
	slog.Warn("move aborted; relay force-stopped", "reason", reason, "after", after, "target", c.moveTarget)
}

// stopMove ends an in-progress move on request (button press or shutdown).
func (c *Client) stopMove(reason string) {
	c.forceStop()
	c.moving = false
	slog.Info("move stopped", "reason", reason)
}
