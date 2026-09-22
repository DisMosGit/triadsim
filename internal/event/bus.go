package event

import (
	"log/slog"
	"sync"

	"github.com/DisMosGit/triadsim/internal/clock"
)

// DefaultBuffer is the per-subscriber channel capacity used when New is called
// with a non-positive buffer size.
const DefaultBuffer = 64

// Bus fans events out to subscribers over buffered channels.
//
// Publish never blocks. When a subscriber does not drain its channel quickly
// enough it loses the event and the drop is logged, so one slow consumer can
// neither stall the publisher nor delay the other subscribers.
type Bus struct {
	mu     sync.Mutex
	subs   map[<-chan Event]chan Event
	buffer int
	clk    clock.Clock
	closed bool
}

// New returns a bus whose subscribers get channels buffered with bufferSize.
// A non-positive bufferSize means DefaultBuffer; a nil clk means
// clock.RealClock{}.
func New(bufferSize int, clk clock.Clock) *Bus {
	if bufferSize <= 0 {
		bufferSize = DefaultBuffer
	}
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &Bus{
		subs:   make(map[<-chan Event]chan Event),
		buffer: bufferSize,
		clk:    clk,
	}
}

// Publish delivers e to every current subscriber. Delivery is best effort: an
// event is dropped for a subscriber whose buffer is full, and publishing on a
// closed bus is a no-op. A zero Timestamp is stamped from the bus clock.
func (b *Bus) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		slog.Debug("event bus is closed, event dropped", "type", e.Type, "resource", e.Resource)
		return
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = b.clk.Now()
	}
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default:
			slog.Warn("event dropped, subscriber buffer full",
				"type", e.Type, "resource", e.Resource, "buffer", b.buffer)
		}
	}
}

// Subscribe registers a subscriber and returns its receive-only channel. The
// channel is closed by Close or Unsubscribe. After Close it returns an
// already-closed channel, so a consumer ranging over it terminates at once.
func (b *Bus) Subscribe() <-chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, b.buffer)
	if b.closed {
		close(ch)
		return ch
	}
	b.subs[ch] = ch
	return ch
}

// Unsubscribe removes ch, closes its channel and reports whether it was
// registered. The channel stays valid for the reader, which observes the
// close.
func (b *Bus) Unsubscribe(ch <-chan Event) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	inner, ok := b.subs[ch]
	if !ok {
		return false
	}
	delete(b.subs, ch)
	close(inner)
	return true
}

// Close closes every subscriber channel and marks the bus closed. It is
// idempotent; after Close, Subscribe returns closed channels and Publish is a
// no-op.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true
	for _, ch := range b.subs {
		close(ch)
	}
	b.subs = make(map[<-chan Event]chan Event)
}

// pending reports the number of registered subscribers.
func (b *Bus) pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
