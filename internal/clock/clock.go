// Package clock provides the injectable time source used by every component
// that schedules work.
//
// Production code uses RealClock. Tests use FakeClock from fake.go, so timer
// behaviour is deterministic and no test ever sleeps.
package clock

import "time"

// Clock abstracts reading the current time and scheduling timers.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// AfterFunc waits for d to elapse and then calls f in its own goroutine.
	// A zero or negative d fires as soon as possible.
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is the subset of time.Timer the simulator relies on.
type Timer interface {
	// Stop prevents the timer from firing and reports whether it was still
	// active, that is, whether the stop took effect.
	Stop() bool
	// Reset reschedules the timer to fire after d, relative to the current
	// time.
	Reset(d time.Duration) bool
}

// RealClock is the production Clock backed by the time package.
type RealClock struct{}

// Now returns the current wall time.
func (RealClock) Now() time.Time { return time.Now() }

// AfterFunc runs f in its own goroutine after d has elapsed.
func (RealClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }
