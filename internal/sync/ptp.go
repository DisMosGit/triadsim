package sync

import (
	"context"
	"fmt"

	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/store"
)

// State is a G.8275.1 clock-level state of the PTP clock.
//
// It is an alias of string rather than a defined type, so a manager satisfies
// the management planes' simulation seams (whose signatures are primitive)
// without an adapter, while the constants below keep the state names in one
// place.
type State = string

// Clock-level states. They mirror the model constants, which the router stores
// in ptp/clock/state.
const (
	StateFreerun           State = model.PTPStateFreerun
	StateAcquiring         State = model.PTPStateAcquiring
	StateLocked            State = model.PTPStateLocked
	StateHoldoverInSpec    State = model.PTPStateHoldoverInSpec
	StateHoldoverOutOfSpec State = model.PTPStateHoldoverOutOfSpec
)

// validState reports whether the state is one of the model's clock states.
func validState(s State) bool {
	switch s {
	case StateFreerun, StateAcquiring, StateLocked, StateHoldoverInSpec, StateHoldoverOutOfSpec:
		return true
	default:
		return false
	}
}

// EventType identifies a PTP state-machine event.
type EventType string

// Events of the simplified state machine. SourceDetected, SourceLost and
// SourceRestored model the reference of the clock; CalibrationComplete models
// the end of the acquisition; HoldoverTimerExpired is produced by the manager
// itself and ManualReset is a forced return to free-run.
const (
	EventSourceDetected      EventType = "SourceDetected"
	EventCalibrationComplete EventType = "CalibrationComplete"
	EventSourceLost          EventType = "SourceLost"
	EventSourceRestored      EventType = "SourceRestored"
	EventHoldoverExpired     EventType = "HoldoverTimerExpired"
	EventReset               EventType = "ManualReset"
)

// valid reports whether the event type is known to the state machine.
func (e EventType) valid() bool {
	switch e {
	case EventSourceDetected, EventCalibrationComplete, EventSourceLost,
		EventSourceRestored, EventHoldoverExpired, EventReset:
		return true
	default:
		return false
	}
}

// Event is one input of the PTP state machine.
type Event struct {
	// Type selects the transition.
	Type EventType
	// Reason is recorded in the state transition event. It is optional.
	Reason string
}

// nextState returns the state an event leads to, and reports whether the pair
// is defined. An undefined pair is a no-op, not an error.
func nextState(from State, typ EventType) (State, bool) {
	switch typ {
	case EventSourceDetected:
		if from == StateFreerun {
			return StateAcquiring, true
		}
	case EventCalibrationComplete:
		if from == StateAcquiring {
			return StateLocked, true
		}
	case EventSourceLost:
		switch from {
		case StateLocked:
			return StateHoldoverInSpec, true
		case StateAcquiring:
			return StateFreerun, true
		}
	case EventHoldoverExpired:
		if from == StateHoldoverInSpec {
			return StateHoldoverOutOfSpec, true
		}
	case EventSourceRestored:
		if from == StateHoldoverInSpec || from == StateHoldoverOutOfSpec {
			return StateLocked, true
		}
	case EventReset:
		if from != StateFreerun {
			return StateFreerun, true
		}
	}
	return from, false
}

// Handle feeds one event to the PTP state machine and returns the resulting
// state. A transition writes ptp/clock/state, publishes StateTransition and,
// when the holdover expires, AlarmRaised; an undefined event pair is a no-op.
func (m *Manager) Handle(ctx context.Context, e Event) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.handleLocked(ctx, e)
}

// SyncLoss drives the clock to holdover because its synchronization source was
// lost. A non-empty source must name a SyncE interface of the device. The
// returned state is the state after the event, which is unchanged when the lost
// source does not affect the clock.
func (m *Manager) SyncLoss(ctx context.Context, source string) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	reason := "synchronization source lost"
	if source == "" {
		return m.handleLocked(ctx, Event{Type: EventSourceLost, Reason: reason})
	}

	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return m.state, fmt.Errorf("sync: %w", err)
	}
	if !hasSyncInterface(device, source) {
		return m.state, fmt.Errorf("%w: %q", ErrUnknownSource, source)
	}
	m.adopt(device)
	reason = fmt.Sprintf("synchronization source %s lost", source)
	return m.handleLocked(ctx, Event{Type: EventSourceLost, Reason: reason})
}

// handleLocked is the state machine proper. The caller must hold the domain
// lock.
func (m *Manager) handleLocked(ctx context.Context, e Event) (State, error) {
	if !e.Type.valid() {
		return m.state, fmt.Errorf("%w: %q", ErrUnknownEvent, e.Type)
	}
	if err := m.ensureState(ctx); err != nil {
		return m.state, err
	}

	next, ok := nextState(m.state, e.Type)
	if !ok || next == m.state {
		return m.state, nil
	}
	return m.transition(ctx, next, e.Reason)
}

// transition applies one state change: it writes the new state, updates the
// holdover timer, publishes the transition and the holdover alarm.
func (m *Manager) transition(ctx context.Context, next State, reason string) (State, error) {
	from := m.state

	// Entering holdover needs the configured timeout, so the snapshot is read
	// before anything is written: a failed read must not leave the store and
	// the manager disagreeing about the state.
	var holdoverDevice *model.Device
	if next == StateHoldoverInSpec {
		device, err := m.router.Snapshot(ctx, store.Running)
		if err != nil {
			return from, fmt.Errorf("sync: %w", err)
		}
		holdoverDevice = device
	}

	if err := m.stateWrite(ctx, "ptp/clock/state", string(next)); err != nil {
		return from, err
	}
	m.state = next

	switch next {
	case StateHoldoverInSpec:
		m.startHoldover(holdoverDevice)
	case StateHoldoverOutOfSpec:
		// The timer has fired: keep the holdover start the drift is measured
		// from, but forget the spent timer.
		m.timer = nil
	default:
		m.stopHoldover()
	}

	if reason == "" {
		reason = string(next)
	}
	m.publish(event.TypeStateTransition, "ptp/clock", "",
		fmt.Sprintf("ptp clock: %s -> %s (%s)", from, next, reason))

	if next == StateHoldoverOutOfSpec {
		m.publish(event.TypeAlarmRaised, "ptp/clock", "major",
			fmt.Sprintf("ptp clock holdover expired in %s: source not restored", from))
	} else if from == StateHoldoverOutOfSpec {
		m.publish(event.TypeAlarmCleared, "ptp/clock", "cleared",
			fmt.Sprintf("ptp clock recovered from holdover: %s -> %s", from, next))
	}
	return next, nil
}

// hasSyncInterface reports whether name is a SyncE interface of the device.
func hasSyncInterface(device *model.Device, name string) bool {
	for _, iface := range device.SyncE.Interfaces {
		if iface.Name == name {
			return true
		}
	}
	return false
}
