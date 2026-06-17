package tinkerforge

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- fakes -----------------------------------------------------------------

type fakeRelay struct {
	mu    sync.Mutex
	c0    bool
	c1    bool
	calls int
	err   error
	// stopped is signalled every time the relay enters the stop state (false,false);
	// engaged every time it enters a move state (channels differ).
	stopped chan struct{}
	engaged chan struct{}
}

func newFakeRelay() *fakeRelay {
	return &fakeRelay{stopped: make(chan struct{}, 16), engaged: make(chan struct{}, 16)}
}

func (r *fakeRelay) SetValue(c0, c1 bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.c0, r.c1 = c0, c1
	r.calls++
	switch {
	case !c0 && !c1:
		select {
		case r.stopped <- struct{}{}:
		default:
		}
	case c0 != c1:
		select {
		case r.engaged <- struct{}{}:
		default:
		}
	}
	return nil
}

func (r *fakeRelay) value() (bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.c0, r.c1
}

func (r *fakeRelay) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// reading is one scripted GetDistance result.
type reading struct {
	d   uint16
	err error
}

func ok(d uint16) reading { return reading{d: d} }
func fail() reading       { return reading{err: errors.New("request timed out")} }

// fakeSensor returns a scripted sequence of readings; the last entry repeats once
// the sequence is exhausted (so "moving but never reaching" / "errors forever"
// are expressed by ending on that reading). The worker is the only reader; the
// sequence is set before run() starts and not mutated afterwards.
type fakeSensor struct {
	mu  sync.Mutex
	seq []reading
	i   int
}

func newFakeSensor(seq ...reading) *fakeSensor {
	if len(seq) == 0 {
		seq = []reading{ok(0)}
	}
	return &fakeSensor{seq: seq}
}

func (s *fakeSensor) GetDistance() (uint16, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.seq[min(s.i, len(s.seq)-1)]
	s.i++
	return r.d, r.err
}

// fakeClock is an atomic-backed clock so a test goroutine can advance it while
// the worker goroutine reads it without tripping the race detector.
type fakeClock struct {
	ns atomic.Int64
}

func newFakeClock() *fakeClock { return &fakeClock{} }

func (c *fakeClock) now() time.Time          { return time.Unix(0, c.ns.Load()) }
func (c *fakeClock) advance(d time.Duration) { c.ns.Add(int64(d)) }

func newTestClient(s distanceSensor, r relayController) *Client {
	return &Client{
		sensor:             s,
		relay:              r,
		cmd:                make(chan uint16, commandQueueSize),
		done:               make(chan struct{}),
		now:                time.Now,
		pollInterval:       time.Millisecond, // poll fast so worker tests finish quickly
		sensorStaleTimeout: defaultSensorStaleTimeout,
		maxMoveDuration:    defaultMaxMoveDuration,
	}
}

// --- constants -------------------------------------------------------------

func TestWatchdogConstantsAreOrdered(t *testing.T) {
	// The whole stop story depends on this ordering; pin it so a future tweak
	// can't silently break it. requestTimeout is the same const Connect() passes
	// to ipcon.SetTimeout, so this guards the real production value.
	if !(defaultPollInterval < requestTimeout &&
		requestTimeout < defaultSensorStaleTimeout &&
		defaultSensorStaleTimeout < defaultMaxMoveDuration) {
		t.Fatalf("watchdog constants out of order: poll=%v req=%v stale=%v max=%v",
			defaultPollInterval, requestTimeout, defaultSensorStaleTimeout, defaultMaxMoveDuration)
	}
}

// --- reachedTarget ---------------------------------------------------------

func TestReachedTarget(t *testing.T) {
	cases := []struct {
		dir    direction
		d      uint16
		target uint16
		want   bool
	}{
		{directionUp, 1094, 1095, false},
		{directionUp, 1095, 1095, true},
		{directionUp, 1100, 1095, true},
		{directionDown, 671, 670, false},
		{directionDown, 670, 670, true},
		{directionDown, 665, 670, true},
	}
	for _, tc := range cases {
		if got := reachedTarget(tc.dir, tc.d, tc.target); got != tc.want {
			t.Errorf("reachedTarget(%v, %d, %d) = %v, want %v", tc.dir, tc.d, tc.target, got, tc.want)
		}
	}
}

// --- startMove: direction and deadband -------------------------------------

func TestStartMoveUp(t *testing.T) {
	c := newTestClient(newFakeSensor(ok(1000)), newFakeRelay())

	if !c.startMove(1095) {
		t.Fatal("expected a move to start")
	}
	if c0, c1 := c.relay.(*fakeRelay).value(); !c0 || c1 {
		t.Errorf("relay = (%v,%v), want up (true,false)", c0, c1)
	}
	if c.moveDir != directionUp || c.moveTarget != 1095 {
		t.Errorf("move = (%v,%d), want (up,1095)", c.moveDir, c.moveTarget)
	}
	if !c.moving {
		t.Error("moving = false, want true")
	}
}

func TestStartMoveDown(t *testing.T) {
	c := newTestClient(newFakeSensor(ok(1000)), newFakeRelay())

	if !c.startMove(670) {
		t.Fatal("expected a move to start")
	}
	if c0, c1 := c.relay.(*fakeRelay).value(); c0 || !c1 {
		t.Errorf("relay = (%v,%v), want down (false,true)", c0, c1)
	}
	if c.moveDir != directionDown {
		t.Errorf("dir = %v, want down", c.moveDir)
	}
	if !c.moving {
		t.Error("moving = false, want true")
	}
}

func TestStartMoveWithinToleranceDoesNothing(t *testing.T) {
	// 1090 is within positionTolerance (10) of target 1095 -> no move.
	r := newFakeRelay()
	c := newTestClient(newFakeSensor(ok(1090)), r)

	if c.startMove(1095) {
		t.Error("expected no move when within the deadband")
	}
	if c.moving {
		t.Error("moving = true, want false")
	}
	if r.callCount() != 0 {
		t.Errorf("relay engaged %d times, want 0", r.callCount())
	}
}

func TestStartMoveJustOutsideToleranceMoves(t *testing.T) {
	// 1080 is 15mm from target 1095, outside the 10mm deadband -> moves up.
	c := newTestClient(newFakeSensor(ok(1080)), newFakeRelay())

	if !c.startMove(1095) {
		t.Fatal("expected a move just outside the deadband")
	}
	if c0, c1 := c.relay.(*fakeRelay).value(); !c0 || c1 {
		t.Errorf("relay = (%v,%v), want up (true,false)", c0, c1)
	}
}

func TestStartMoveSensorError(t *testing.T) {
	r := newFakeRelay()
	c := newTestClient(newFakeSensor(fail()), r)

	if c.startMove(1095) {
		t.Error("expected no move when the sensor read fails")
	}
	if c.moving {
		t.Error("moving = true, want false")
	}
	if r.callCount() != 0 {
		t.Errorf("relay engaged %d times, want 0", r.callCount())
	}
}

// --- checkProgress: the poll decision (deterministic, no goroutine) --------

// movingClient returns an idle test client wired to the given sensor with a
// fake clock, then puts it into the moving-up-toward-target state as if a move
// had just started at the clock's current time.
func movingClient(s distanceSensor, dir direction, target uint16) (*Client, *fakeClock, time.Time, *time.Time) {
	clk := newFakeClock()
	c := newTestClient(s, newFakeRelay())
	c.now = clk.now
	c.moving = true
	c.moveDir = dir
	c.moveTarget = target
	start := clk.now()
	lastGood := start
	return c, clk, start, &lastGood
}

func TestCheckProgressReachedUp(t *testing.T) {
	c, _, start, lastGood := movingClient(newFakeSensor(ok(1100)), directionUp, 1095)
	res := c.checkProgress(start, lastGood)
	if res.action != moveReached || res.at != 1100 {
		t.Fatalf("got action=%v at=%d, want reached@1100", res.action, res.at)
	}
}

func TestCheckProgressReachedDown(t *testing.T) {
	// The downward-move regression: must stop once distance <= target.
	c, _, start, lastGood := movingClient(newFakeSensor(ok(665)), directionDown, 670)
	res := c.checkProgress(start, lastGood)
	if res.action != moveReached || res.at != 665 {
		t.Fatalf("got action=%v at=%d, want reached@665", res.action, res.at)
	}
}

func TestCheckProgressContinuesAndAdvancesLastGood(t *testing.T) {
	c, clk, start, lastGood := movingClient(newFakeSensor(ok(800)), directionUp, 1095)
	clk.advance(123 * time.Millisecond)
	res := c.checkProgress(start, lastGood)
	if res.action != moveContinue {
		t.Fatalf("action = %v, want continue", res.action)
	}
	if !lastGood.Equal(clk.now()) {
		t.Errorf("lastGood = %v, want advanced to %v on a good read", *lastGood, clk.now())
	}
}

func TestCheckProgressTransientErrorContinuesWithoutAdvancingLastGood(t *testing.T) {
	c, clk, start, lastGood := movingClient(newFakeSensor(fail()), directionUp, 1095)
	before := *lastGood
	clk.advance(c.sensorStaleTimeout - time.Millisecond) // not stale yet
	res := c.checkProgress(start, lastGood)
	if res.action != moveContinue {
		t.Fatalf("action = %v, want continue (transient error)", res.action)
	}
	if res.readErr == nil {
		t.Error("readErr = nil, want the transient error reported")
	}
	if !lastGood.Equal(before) {
		t.Error("lastGood advanced on a failed read; it must only advance on success")
	}
}

func TestCheckProgressStaleSensorAborts(t *testing.T) {
	c, clk, start, lastGood := movingClient(newFakeSensor(fail()), directionUp, 1095)
	clk.advance(c.sensorStaleTimeout) // no good reading for the whole window
	res := c.checkProgress(start, lastGood)
	if res.action != moveStale {
		t.Fatalf("action = %v, want stale", res.action)
	}
	if res.elapsed < c.sensorStaleTimeout {
		t.Errorf("elapsed = %v, want >= %v", res.elapsed, c.sensorStaleTimeout)
	}
}

func TestCheckProgressMaxDurationAborts(t *testing.T) {
	// Good readings that never reach the target; max duration must still stop it.
	c, clk, start, lastGood := movingClient(newFakeSensor(ok(800)), directionUp, 1095)
	clk.advance(c.maxMoveDuration)
	res := c.checkProgress(start, lastGood)
	if res.action != moveTimedOut {
		t.Fatalf("action = %v, want timed out", res.action)
	}
}

// --- move-ending methods: relay + logging ----------------------------------

func TestFinishMoveStopsRelay(t *testing.T) {
	r := newFakeRelay()
	c := newTestClient(newFakeSensor(ok(1000)), r)
	c.moving = true
	c.moveTarget = 1095

	c.finishMove(1100)

	if c0, c1 := r.value(); c0 || c1 {
		t.Errorf("relay = (%v,%v), want stopped (false,false)", c0, c1)
	}
	if c.moving {
		t.Error("moving = true, want false")
	}
}

func TestAbortMoveLogsDistinctReasons(t *testing.T) {
	for _, reason := range []string{"sensor_stale", "max_duration"} {
		var buf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))

		r := newFakeRelay()
		c := newTestClient(newFakeSensor(ok(1000)), r)
		c.moving = true
		c.moveTarget = 670
		c.abortMove(reason, 1500*time.Millisecond)

		slog.SetDefault(prev)

		if c0, c1 := r.value(); c0 || c1 {
			t.Errorf("[%s] relay = (%v,%v), want stopped (false,false)", reason, c0, c1)
		}
		out := buf.String()
		if !strings.Contains(out, "reason="+reason) {
			t.Errorf("[%s] log missing reason: %s", reason, out)
		}
		if !strings.Contains(out, "target=670") {
			t.Errorf("[%s] log missing target: %s", reason, out)
		}
	}
}

// --- GoTo handoff ----------------------------------------------------------

func TestGoToQueuesPressesInOrder(t *testing.T) {
	c := newTestClient(newFakeSensor(ok(1000)), newFakeRelay())

	c.GoTo(100)
	c.GoTo(200)

	if got := <-c.cmd; got != 100 {
		t.Errorf("first press = %d, want 100 (FIFO)", got)
	}
	if got := <-c.cmd; got != 200 {
		t.Errorf("second press = %d, want 200", got)
	}
}

func TestGoToDoesNotBlockWhenFull(t *testing.T) {
	c := newTestClient(newFakeSensor(ok(1000)), newFakeRelay())
	// Fill the queue and push one extra; GoTo must not block (test would hang).
	for i := 0; i < commandQueueSize+2; i++ {
		c.GoTo(uint16(i))
	}
	if len(c.cmd) != commandQueueSize {
		t.Errorf("queued %d presses, want %d (extras dropped)", len(c.cmd), commandQueueSize)
	}
}

// --- worker (goroutine) behaviour ------------------------------------------

func TestWorkerAutoStopsAtTarget(t *testing.T) {
	// First reading picks direction (up: 1000 < 1095); later polls reach 1100.
	s := newFakeSensor(ok(1000), ok(1090), ok(1100))
	r := newFakeRelay()
	c := newTestClient(s, r)

	c.wg.Add(1)
	go c.run()
	defer func() { close(c.done); c.wg.Wait() }()

	c.GoTo(1095)
	select {
	case <-r.engaged:
	case <-time.After(time.Second):
		t.Fatal("worker did not start the move")
	}

	select {
	case <-r.stopped:
	case <-time.After(time.Second):
		t.Fatal("relay was not stopped after the target was reached")
	}
}

func TestWorkerPressWhileMovingStopsWithoutRestarting(t *testing.T) {
	// Direction read 1000 (>670 -> down), then 800 forever: never reaches target.
	s := newFakeSensor(ok(1000), ok(800))
	r := newFakeRelay()
	c := newTestClient(s, r)

	c.wg.Add(1)
	go c.run()
	defer func() { close(c.done); c.wg.Wait() }()

	c.GoTo(670) // start moving down
	select {
	case <-r.engaged:
	case <-time.After(time.Second):
		t.Fatal("worker did not start the move")
	}

	c.GoTo(670) // a press while moving should stop, not start a new move
	select {
	case <-r.stopped:
	case <-time.After(time.Second):
		t.Fatal("relay was not stopped by the press")
	}

	// No new move may be engaged by that press.
	select {
	case <-r.engaged:
		t.Fatal("a press while moving started a new move instead of just stopping")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWorkerForceStopsOnStaleSensor(t *testing.T) {
	// Direction read succeeds (1000 > 670 -> down); every poll after that fails.
	s := newFakeSensor(ok(1000), fail())
	r := newFakeRelay()
	clk := newFakeClock()
	c := newTestClient(s, r)
	c.now = clk.now

	c.wg.Add(1)
	go c.run()
	defer func() { close(c.done); c.wg.Wait() }()

	c.GoTo(670)
	select {
	case <-r.engaged:
	case <-time.After(time.Second):
		t.Fatal("worker did not start the move")
	}

	// Sensor is now unresponsive; advancing past the stale window must force-stop.
	clk.advance(c.sensorStaleTimeout + time.Second)
	select {
	case <-r.stopped:
	case <-time.After(time.Second):
		t.Fatal("relay was not force-stopped after the sensor went stale")
	}
}
