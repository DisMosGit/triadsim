package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRealClockNow(t *testing.T) {
	before := time.Now()

	got := RealClock{}.Now()

	assert.False(t, got.IsZero())
	assert.WithinDuration(t, before, got, time.Second)
}

func TestRealClockAfterFuncFires(t *testing.T) {
	fired := make(chan struct{})
	RealClock{}.AfterFunc(time.Millisecond, func() { close(fired) })

	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("timer did not fire within 5s")
	}
}

func TestRealClockStopPreventsFiring(t *testing.T) {
	timer := RealClock{}.AfterFunc(time.Hour, func() { t.Error("stopped timer must not fire") })

	assert.True(t, timer.Stop(), "stopping an active timer reports true")
	assert.False(t, timer.Stop(), "stopping a stopped timer reports false")
}

func TestRealClockReset(t *testing.T) {
	fired := make(chan struct{})
	timer := RealClock{}.AfterFunc(time.Hour, func() { close(fired) })
	timer.Reset(time.Millisecond)

	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("reset timer did not fire within 5s")
	}
}
