package event

import (
	"bytes"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
)

// captureLogs installs a JSON logger writing into a buffer as the slog default
// for the duration of the test and returns that buffer.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return &buf
}

// receive requires exactly one event from ch and returns it.
func receive(t *testing.T, ch <-chan Event) Event {
	t.Helper()

	select {
	case e, ok := <-ch:
		require.True(t, ok, "subscriber channel must be open")
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no event received within 5s")
		return Event{}
	}
}

func TestPublishReachesEverySubscriber(t *testing.T) {
	bus := New(1, clock.NewFakeClock())
	first := bus.Subscribe()
	second := bus.Subscribe()

	bus.Publish(Event{Type: TypeAlarmRaised, Resource: "radio0"})

	gotFirst := receive(t, first)
	gotSecond := receive(t, second)
	assert.Equal(t, gotFirst, gotSecond)
	assert.Equal(t, TypeAlarmRaised, gotFirst.Type)
	assert.Equal(t, "radio0", gotFirst.Resource)
}

func TestPublishStampsTimestampFromClock(t *testing.T) {
	fake := clock.NewFakeClock()
	fake.Advance(time.Hour)
	bus := New(1, fake)
	ch := bus.Subscribe()

	bus.Publish(Event{Type: TypeConfigChanged, Resource: "device"})
	assert.Equal(t, fake.Now(), receive(t, ch).Timestamp)

	explicit := time.Date(2026, time.March, 4, 5, 6, 7, 0, time.UTC)
	bus.Publish(Event{Type: TypeConfigChanged, Resource: "device", Timestamp: explicit})
	assert.Equal(t, explicit, receive(t, ch).Timestamp)
}

func TestPublishWithNilClockUsesRealTime(t *testing.T) {
	bus := New(1, nil)
	ch := bus.Subscribe()

	bus.Publish(Event{Type: TypeConfigChanged})

	got := receive(t, ch)
	assert.WithinDuration(t, time.Now(), got.Timestamp, time.Minute)
}

func TestNewUsesDefaultBuffer(t *testing.T) {
	tests := []struct {
		name   string
		buffer int
	}{
		{name: "zero", buffer: 0},
		{name: "negative", buffer: -3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := New(tt.buffer, clock.NewFakeClock())
			assert.Equal(t, DefaultBuffer, bus.buffer)
			assert.Equal(t, DefaultBuffer, cap(bus.Subscribe()))
		})
	}
}

func TestPublishDropsWhenSubscriberBufferIsFull(t *testing.T) {
	logs := captureLogs(t)
	bus := New(1, clock.NewFakeClock())
	slow := bus.Subscribe()
	fast := bus.Subscribe()

	var fastReceived []Event
	for i := range 3 {
		bus.Publish(Event{Type: TypeAlarmRaised, Resource: "radio0", Message: fmt.Sprintf("e%d", i)})
		fastReceived = append(fastReceived, receive(t, fast))
	}

	require.Len(t, fastReceived, 3, "a draining subscriber must receive every event")
	assert.Len(t, slow, 1, "the undrained subscriber keeps only what fits its buffer")
	assert.Contains(t, logs.String(), "subscriber buffer full")
	assert.Contains(t, logs.String(), `"type":"AlarmRaised"`)

	// The slow subscriber recovers as soon as it drains.
	<-slow
	assert.Empty(t, slow)
	bus.Publish(Event{Type: TypeAlarmCleared, Resource: "radio0"})
	assert.Len(t, slow, 1)
}

func TestDropLogMentionsBufferSize(t *testing.T) {
	logs := captureLogs(t)
	bus := New(1, clock.NewFakeClock())
	bus.Subscribe()

	bus.Publish(Event{Type: TypeConfigChanged, Resource: "device"})
	bus.Publish(Event{Type: TypeConfigChanged, Resource: "device"})

	assert.Contains(t, logs.String(), `"buffer":1`)
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	bus := New(1, clock.NewFakeClock())
	removed := bus.Subscribe()
	kept := bus.Subscribe()

	require.True(t, bus.Unsubscribe(removed))
	assert.False(t, bus.Unsubscribe(removed), "unsubscribing twice reports false")
	assert.False(t, bus.Unsubscribe(make(chan Event)), "unknown channel reports false")

	bus.Publish(Event{Type: TypeAlarmRaised, Resource: "radio0"})

	_, ok := <-removed
	assert.False(t, ok, "unsubscribed channel must be closed")
	assert.Len(t, kept, 1)
}

func TestCloseClosesSubscribersAndIsIdempotent(t *testing.T) {
	bus := New(1, clock.NewFakeClock())
	first := bus.Subscribe()
	second := bus.Subscribe()

	bus.Close()
	bus.Close()

	for _, ch := range []<-chan Event{first, second} {
		_, ok := <-ch
		assert.False(t, ok, "subscriber channel must be closed")
	}
	assert.Zero(t, bus.pending())
}

func TestPublishAfterCloseIsNoop(t *testing.T) {
	logs := captureLogs(t)
	bus := New(1, clock.NewFakeClock())
	bus.Close()

	assert.NotPanics(t, func() { bus.Publish(Event{Type: TypeAlarmRaised, Resource: "radio0"}) })
	assert.Contains(t, logs.String(), "event bus is closed")
}

func TestSubscribeAfterCloseReturnsClosedChannel(t *testing.T) {
	bus := New(1, clock.NewFakeClock())
	bus.Close()

	ch := bus.Subscribe()

	_, ok := <-ch
	assert.False(t, ok)
}

func TestNoSubscribersIsNotAnError(t *testing.T) {
	bus := New(1, clock.NewFakeClock())

	assert.NotPanics(t, func() { bus.Publish(Event{Type: TypeConfigChanged}) })
	assert.Zero(t, bus.pending())
}

func TestBusIsSafeForConcurrentUse(t *testing.T) {
	captureLogs(t)
	bus := New(4, clock.NewFakeClock())

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range bus.Subscribe() {
		}
	}()

	var wg sync.WaitGroup
	for range 32 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			bus.Publish(Event{Type: TypeStateTransition, Resource: "ptp"})
		}()
		go func() {
			defer wg.Done()
			bus.Unsubscribe(bus.Subscribe())
		}()
	}
	wg.Wait()

	bus.Close()
	<-drained
	assert.Zero(t, bus.pending())
}
