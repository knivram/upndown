package tinkerforge

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Tinkerforge/go-api-bindings/distance_ir_v2_bricklet"
)

// --- fakes -----------------------------------------------------------------

type fakeRelay struct {
	mu    sync.Mutex
	c0    bool
	c1    bool
	calls int
	err   error
	// stopped is signalled every time the relay enters the stop state (true,true).
	stopped chan struct{}
}

func newFakeRelay() *fakeRelay {
	return &fakeRelay{stopped: make(chan struct{}, 16)}
}

func (r *fakeRelay) SetValue(c0, c1 bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.c0, r.c1 = c0, c1
	r.calls++
	if c0 && c1 {
		select {
		case r.stopped <- struct{}{}:
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

type fakeSensor struct {
	mu sync.Mutex

	distance uint16
	distErr  error

	cb   func(uint16)
	cbID uint64
	next uint64

	lastPeriod       uint32
	lastValueChanged bool
	lastOption       distance_ir_v2_bricklet.ThresholdOption
	lastMin          uint16
	configCalls      int

	deregistered  uint64
	deregisterCnt int
	registered    chan struct{} // signalled when a callback is registered
}

func newFakeSensor(distance uint16) *fakeSensor {
	return &fakeSensor{distance: distance, registered: make(chan struct{}, 8)}
}

func (s *fakeSensor) GetDistance() (uint16, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.distance, s.distErr
}

func (s *fakeSensor) SetDistanceCallbackConfiguration(period uint32, valueHasToChange bool, option distance_ir_v2_bricklet.ThresholdOption, min uint16, _ uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPeriod = period
	s.lastValueChanged = valueHasToChange
	s.lastOption = option
	s.lastMin = min
	s.configCalls++
	return nil
}

func (s *fakeSensor) RegisterDistanceCallback(fn func(uint16)) uint64 {
	s.mu.Lock()
	s.next++
	s.cb = fn
	s.cbID = s.next
	id := s.next
	s.mu.Unlock()
	select {
	case s.registered <- struct{}{}:
	default:
	}
	return id
}

func (s *fakeSensor) DeregisterDistanceCallback(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deregistered = id
	s.deregisterCnt++
	if id == s.cbID {
		s.cb = nil
	}
}

// fire simulates the sensor firmware invoking the registered callback.
func (s *fakeSensor) fire(d uint16) {
	s.mu.Lock()
	cb := s.cb
	s.mu.Unlock()
	if cb != nil {
		cb(d)
	}
}

func newTestClient(s distanceSensor, r relayController) *Client {
	return &Client{
		sensor:  s,
		relay:   r,
		cmd:     make(chan uint16, commandQueueSize),
		reached: make(chan reachEvent, 1),
		done:    make(chan struct{}),
	}
}

// --- startMove: direction, deadband, streaming config ----------------------

func TestStartMoveUp(t *testing.T) {
	s := newFakeSensor(1000)
	r := newFakeRelay()
	c := newTestClient(s, r)

	if !c.startMove(1095) {
		t.Fatal("expected a move to start")
	}
	if c0, c1 := r.value(); c0 || !c1 {
		t.Errorf("relay = (%v,%v), want up (false,true)", c0, c1)
	}
	// Stop detection is done in code, so the sensor just streams changed samples.
	if s.lastOption != distance_ir_v2_bricklet.ThresholdOptionOff || !s.lastValueChanged {
		t.Errorf("want streaming config (Off, valueHasToChange=true); got option=%q changed=%v", s.lastOption, s.lastValueChanged)
	}
	if s.lastPeriod != callbackPeriodMs {
		t.Errorf("callback period = %d, want %d", s.lastPeriod, callbackPeriodMs)
	}
	if !c.moving {
		t.Error("moving = false, want true")
	}
}

func TestStartMoveDown(t *testing.T) {
	s := newFakeSensor(1000)
	r := newFakeRelay()
	c := newTestClient(s, r)

	if !c.startMove(670) {
		t.Fatal("expected a move to start")
	}
	if c0, c1 := r.value(); !c0 || c1 {
		t.Errorf("relay = (%v,%v), want down (true,false)", c0, c1)
	}
	if !c.moving {
		t.Error("moving = false, want true")
	}
}

func TestStartMoveWithinToleranceDoesNothing(t *testing.T) {
	// 1090 is within positionTolerance (10) of target 1095 -> no move.
	s := newFakeSensor(1090)
	r := newFakeRelay()
	c := newTestClient(s, r)

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
	s := newFakeSensor(1080)
	r := newFakeRelay()
	c := newTestClient(s, r)

	if !c.startMove(1095) {
		t.Fatal("expected a move just outside the deadband")
	}
	if c0, c1 := r.value(); c0 || !c1 {
		t.Errorf("relay = (%v,%v), want up (false,true)", c0, c1)
	}
}

func TestStartMoveSensorError(t *testing.T) {
	s := newFakeSensor(0)
	s.distErr = errors.New("request timed out")
	r := newFakeRelay()
	c := newTestClient(s, r)

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

// --- stop detection (in code) ----------------------------------------------

func TestCallbackSignalsWhenUpReachesTarget(t *testing.T) {
	s := newFakeSensor(1000)
	c := newTestClient(s, newFakeRelay())
	c.startMove(1095) // up: stop once distance >= 1095

	s.fire(1090) // not there yet
	select {
	case <-c.reached:
		t.Fatal("signalled before reaching the target")
	default:
	}

	s.fire(1100) // crossed
	select {
	case ev := <-c.reached:
		if ev.at != 1100 {
			t.Errorf("reach position = %d, want 1100", ev.at)
		}
	default:
		t.Fatal("did not signal after crossing the target")
	}
}

func TestCallbackSignalsWhenDownReachesTarget(t *testing.T) {
	s := newFakeSensor(700)
	c := newTestClient(s, newFakeRelay())
	c.startMove(670) // down: stop once distance <= 670

	s.fire(680) // not there yet
	select {
	case <-c.reached:
		t.Fatal("signalled before reaching the target")
	default:
	}

	s.fire(665) // crossed
	select {
	case ev := <-c.reached:
		if ev.at != 665 {
			t.Errorf("reach position = %d, want 665", ev.at)
		}
	default:
		t.Fatal("did not signal after crossing the target (the downward-move regression)")
	}
}

func TestFinishMoveStopsEverything(t *testing.T) {
	s := newFakeSensor(1000)
	r := newFakeRelay()
	c := newTestClient(s, r)

	c.startMove(1095)
	cbID := c.activeCallbackID
	c.finishMove(reachEvent{epoch: c.moveEpoch, at: 1100})

	if c0, c1 := r.value(); !c0 || !c1 {
		t.Errorf("relay = (%v,%v), want stopped (true,true)", c0, c1)
	}
	if s.lastPeriod != 0 || s.lastOption != distance_ir_v2_bricklet.ThresholdOptionOff {
		t.Errorf("streaming not disabled: period=%d option=%q", s.lastPeriod, s.lastOption)
	}
	if s.deregisterCnt == 0 || s.deregistered != cbID {
		t.Errorf("callback %d not deregistered (last=%d, count=%d)", cbID, s.deregistered, s.deregisterCnt)
	}
	if c.moving {
		t.Error("moving = true, want false")
	}
}

func TestFinishMoveIgnoresStaleEpoch(t *testing.T) {
	s := newFakeSensor(1000)
	r := newFakeRelay()
	c := newTestClient(s, r)

	c.startMove(1095)
	c.finishMove(reachEvent{epoch: c.moveEpoch - 1, at: 1100}) // stale

	if !c.moving {
		t.Error("a stale-epoch signal ended the current move")
	}
	if c0, c1 := r.value(); c0 || !c1 {
		t.Errorf("relay = (%v,%v), want still up (false,true)", c0, c1)
	}
}

// --- GoTo handoff ----------------------------------------------------------

func TestGoToQueuesPressesInOrder(t *testing.T) {
	c := newTestClient(newFakeSensor(1000), newFakeRelay())

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
	c := newTestClient(newFakeSensor(1000), newFakeRelay())
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
	s := newFakeSensor(1000)
	r := newFakeRelay()
	c := newTestClient(s, r)

	c.wg.Add(1)
	go c.run()
	defer func() { close(c.done); c.wg.Wait() }()

	c.GoTo(1095) // up
	select {
	case <-s.registered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start the move")
	}

	s.fire(1100) // reach the target on its own
	select {
	case <-r.stopped:
	case <-time.After(time.Second):
		t.Fatal("relay was not stopped after the target was reached")
	}
}

func TestWorkerPressWhileMovingStopsWithoutRestarting(t *testing.T) {
	s := newFakeSensor(1000)
	r := newFakeRelay()
	c := newTestClient(s, r)

	c.wg.Add(1)
	go c.run()
	defer func() { close(c.done); c.wg.Wait() }()

	c.GoTo(670) // start moving down
	select {
	case <-s.registered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start the move")
	}

	c.GoTo(670) // a press while moving should stop, not start a new move
	select {
	case <-r.stopped:
	case <-time.After(time.Second):
		t.Fatal("relay was not stopped by the press")
	}

	// No new move may be started by that press.
	select {
	case <-s.registered:
		t.Fatal("a press while moving started a new move instead of just stopping")
	case <-time.After(200 * time.Millisecond):
	}
}
