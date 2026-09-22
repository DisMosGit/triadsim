package clock

import (
	"sync"
	"time"
)

// FakeClock is a deterministic Clock for tests.
//
// Time only moves when Set or Advance is called, so timers fire in a
// predictable order and tests never sleep.
type FakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

// NewFakeClock returns a FakeClock frozen at the Unix epoch in UTC. Capture
// Now before scheduling timers when a test needs absolute deadlines.
func NewFakeClock() *FakeClock {
	return &FakeClock{now: time.Unix(0, 0).UTC()}
}

// Now returns the clock's current time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set moves the clock to t without firing timers. Timers whose deadline has
// passed stay pending until the next Advance.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Advance moves the clock forward by d and fires every timer that becomes due,
// in deadline order. Callbacks run without the clock lock held, so a callback
// may schedule further timers, which fire within the same Advance when their
// deadline falls inside the window. A negative d is ignored.
func (c *FakeClock) Advance(d time.Duration) {
	if d < 0 {
		return
	}

	c.mu.Lock()
	target := c.now.Add(d)
	for {
		timer := c.nextDueLocked(target)
		if timer == nil {
			break
		}
		timer.active = false
		c.now = timer.deadline
		c.mu.Unlock()
		timer.fn()
		c.mu.Lock()
	}
	c.now = target
	c.mu.Unlock()
}

// Pending reports how many timers are scheduled and have neither fired nor
// been stopped.
func (c *FakeClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	pending := 0
	for _, timer := range c.timers {
		if timer.active {
			pending++
		}
	}
	return pending
}

// AfterFunc schedules f to run once Advance reaches now+d. A zero or negative d
// is due immediately on the next Advance.
func (c *FakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()

	timer := &fakeTimer{clock: c, deadline: c.now.Add(d), fn: f, active: true}
	c.timers = append(c.timers, timer)
	return timer
}

// nextDueLocked returns the active timer with the earliest deadline that is not
// after target, or nil. The caller must hold c.mu.
func (c *FakeClock) nextDueLocked(target time.Time) *fakeTimer {
	var due *fakeTimer
	for _, timer := range c.timers {
		if !timer.active || timer.deadline.After(target) {
			continue
		}
		if due == nil || timer.deadline.Before(due.deadline) {
			due = timer
		}
	}
	return due
}

// fakeTimer is the Timer returned by FakeClock.AfterFunc.
type fakeTimer struct {
	clock    *FakeClock
	deadline time.Time
	fn       func()
	active   bool
}

// Stop prevents the timer from firing.
func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()

	wasActive := t.active
	t.active = false
	return wasActive
}

// Reset reschedules the timer to fire d after the clock's current time and
// reports whether it was still active.
func (t *fakeTimer) Reset(d time.Duration) bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()

	wasActive := t.active
	t.deadline = t.clock.now.Add(d)
	t.active = true
	return wasActive
}
