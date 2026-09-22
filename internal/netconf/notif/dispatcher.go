package notif

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/DisMosGit/triadsim/internal/event"
)

// ErrClosed means the dispatcher has been closed and no longer accepts
// subscriptions.
var ErrClosed = errors.New("netconf: notification dispatcher is closed")

// Dispatcher delivers the events of one EventBus subscription to the NETCONF
// sessions that asked for them with <create-subscription>.
//
// It owns one bus subscription, not one per NETCONF session: the bus is
// subscribed when the dispatcher is created, so no event is lost between the
// server starting and the first client subscribing, and each event is rendered
// to XML once and then queued for every session.
type Dispatcher struct {
	bus    *event.Bus
	events <-chan event.Event

	mu     sync.Mutex
	closed bool
	subs   map[uint32]*Subscription
}

// NewDispatcher subscribes to bus. Call Run to deliver events and Close to
// release the bus subscription.
func NewDispatcher(bus *event.Bus) *Dispatcher {
	return &Dispatcher{
		bus:    bus,
		events: bus.Subscribe(),
		subs:   make(map[uint32]*Subscription),
	}
}

// Run delivers bus events until ctx is cancelled or the bus subscription is
// closed.
func (d *Dispatcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-d.events:
			if !ok {
				return
			}
			d.dispatch(e)
		}
	}
}

// Subscribe registers the notification subscription of one session and replaces
// the previous one, if any. stream must be a stream the simulator exposes.
func (d *Dispatcher) Subscribe(sessionID uint32, stream string, send Sender) error {
	if stream != StreamName {
		return fmt.Errorf("%w: %q", ErrUnknownStream, stream)
	}
	if send == nil {
		return errors.New("netconf: notification sender is nil")
	}

	sub := newSubscription(sessionID, stream, send)

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		sub.stop()
		return ErrClosed
	}
	if previous, ok := d.subs[sessionID]; ok {
		previous.stop()
	}
	d.subs[sessionID] = sub
	return nil
}

// Unsubscribe ends the subscription of one session, if it has one.
func (d *Dispatcher) Unsubscribe(sessionID uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()

	sub, ok := d.subs[sessionID]
	if !ok {
		return
	}
	delete(d.subs, sessionID)
	sub.stop()
}

// Close ends every subscription and releases the bus channel. It is
// idempotent, and Run returns once the bus channel is closed.
func (d *Dispatcher) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	subs := make([]*Subscription, 0, len(d.subs))
	for _, sub := range d.subs {
		subs = append(subs, sub)
	}
	d.subs = make(map[uint32]*Subscription)
	events := d.events
	d.mu.Unlock()

	d.bus.Unsubscribe(events)
	for _, sub := range subs {
		sub.stop()
	}
}

// Subscribers reports how many sessions are subscribed. It is used by tests and
// shutdown logs.
func (d *Dispatcher) Subscribers() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return len(d.subs)
}

// dispatch renders one event and queues it for every subscription.
func (d *Dispatcher) dispatch(e event.Event) {
	data, err := Render(e)
	if err != nil {
		slog.Error("netconf: encoding notification failed", "type", e.Type, "error", err)
		return
	}

	d.mu.Lock()
	subs := make([]*Subscription, 0, len(d.subs))
	for _, sub := range d.subs {
		subs = append(subs, sub)
	}
	d.mu.Unlock()

	for _, sub := range subs {
		sub.deliver(data)
	}
}
