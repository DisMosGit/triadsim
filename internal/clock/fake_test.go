package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fireOrder returns a callback that records label in *order.
func fireOrder(order *[]string, label string) func() {
	return func() { *order = append(*order, label) }
}

func TestFakeClockStartsAtUnixEpoch(t *testing.T) {
	c := NewFakeClock()

	assert.Equal(t, time.Unix(0, 0).UTC(), c.Now())
	assert.Equal(t, 0, c.Pending())
}

func TestFakeClockSetMovesTimeWithoutFiring(t *testing.T) {
	c := NewFakeClock()
	var order []string
	c.AfterFunc(time.Minute, fireOrder(&order, "a"))

	target := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	c.Set(target)

	assert.Equal(t, target, c.Now())
	assert.Empty(t, order, "Set must not fire timers")
	assert.Equal(t, 1, c.Pending())
}

func TestFakeClockAdvanceMovesTimeAndFiresAtDeadline(t *testing.T) {
	c := NewFakeClock()
	start := c.Now()
	var order []string
	c.AfterFunc(5*time.Minute, fireOrder(&order, "a"))

	c.Advance(4*time.Minute + 59*time.Second)
	assert.Empty(t, order, "timer must not fire before its deadline")
	assert.Equal(t, start.Add(4*time.Minute+59*time.Second), c.Now())

	c.Advance(time.Second)
	assert.Equal(t, []string{"a"}, order)
	assert.Equal(t, start.Add(5*time.Minute), c.Now())
	assert.Equal(t, 0, c.Pending())
}

func TestFakeClockAdvanceFiresInDeadlineOrder(t *testing.T) {
	c := NewFakeClock()
	var order []string
	c.AfterFunc(3*time.Minute, fireOrder(&order, "third"))
	c.AfterFunc(time.Minute, fireOrder(&order, "first"))
	c.AfterFunc(2*time.Minute, fireOrder(&order, "second"))

	c.Advance(time.Hour)

	assert.Equal(t, []string{"first", "second", "third"}, order)
	assert.Equal(t, 0, c.Pending())
}

func TestFakeClockFiresChainedTimers(t *testing.T) {
	c := NewFakeClock()
	var order []string
	c.AfterFunc(time.Minute, func() {
		order = append(order, "outer")
		c.AfterFunc(time.Minute, fireOrder(&order, "inner"))
	})

	c.Advance(10 * time.Minute)

	assert.Equal(t, []string{"outer", "inner"}, order)
}

func TestFakeClockStopPreventsFiring(t *testing.T) {
	c := NewFakeClock()
	var order []string
	timer := c.AfterFunc(time.Minute, fireOrder(&order, "a"))

	assert.True(t, timer.Stop())
	assert.Equal(t, 0, c.Pending())

	c.Advance(time.Hour)

	assert.Empty(t, order)
	assert.False(t, timer.Stop(), "stopping twice reports false")
}

func TestFakeClockResetReschedules(t *testing.T) {
	c := NewFakeClock()
	start := c.Now()
	var order []string
	timer := c.AfterFunc(10*time.Minute, fireOrder(&order, "a"))

	c.Advance(time.Minute)
	require.Empty(t, order)

	timer.Reset(2 * time.Minute)
	c.Advance(time.Minute)
	assert.Empty(t, order, "reset deadline is two minutes after the reset")

	c.Advance(time.Minute)
	assert.Equal(t, []string{"a"}, order)
	assert.Equal(t, start.Add(3*time.Minute), c.Now())
}

func TestFakeClockNonPositiveDelayFiresOnNextAdvance(t *testing.T) {
	c := NewFakeClock()
	var order []string
	c.AfterFunc(0, fireOrder(&order, "a"))

	c.Advance(0)

	assert.Equal(t, []string{"a"}, order)
}

func TestFakeClockNegativeAdvanceIsIgnored(t *testing.T) {
	c := NewFakeClock()
	start := c.Now()
	var order []string
	c.AfterFunc(time.Minute, fireOrder(&order, "a"))

	c.Advance(-time.Hour)

	assert.Equal(t, start, c.Now())
	assert.Empty(t, order)
	assert.Equal(t, 1, c.Pending())
}
