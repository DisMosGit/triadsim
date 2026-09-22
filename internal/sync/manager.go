package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	stdsync "sync"
	"time"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Domain errors returned by the manager. Callers match them with errors.Is.
var (
	// ErrNoRouter reports a manager built without a router.
	ErrNoRouter = errors.New("sync: router must not be nil")
	// ErrUnknownEvent reports an event type the state machine does not know.
	ErrUnknownEvent = errors.New("sync: unknown event")
	// ErrUnknownSource reports a synchronization source that is not a SyncE
	// interface of the device.
	ErrUnknownSource = errors.New("sync: unknown source")
)

// Defaults of the periodic work and of the simulated quality of the clock.
const (
	// DefaultTickInterval is the period of the domain's background work: the
	// SyncE source selection and the offset/jitter refresh.
	DefaultTickInterval = 5 * time.Second
	// DefaultDriftPPB is the holdover drift of the simulated oscillator, in
	// parts per billion. One ppb is one nanosecond per second.
	DefaultDriftPPB = 100.0
	// DefaultJitterNS is the peak offset noise of a locked clock, in
	// nanoseconds.
	DefaultJitterNS = 25.0
	// DefaultFreerunOffsetNS is the offset bound of a free-running clock, in
	// nanoseconds.
	DefaultFreerunOffsetNS = 1000.0
	// rngSeed keeps the simulated noise reproducible, which tests rely on.
	rngSeed int64 = 20250501
)

// Deps is what the sync domain needs from the rest of the simulator.
type Deps struct {
	// Router is the path-to-model mapping the manager reads snapshots and
	// writes state through. It is required.
	Router *router.Router
	// Bus publishes domain events: PTP state transitions and holdover alarms. A
	// nil bus swallows events, which unit tests rely on.
	Bus *event.Bus
	// Clock is the injected time source. RealClock is used when nil.
	Clock clock.Clock
	// TickInterval is the period of Run's background work. Zero selects
	// DefaultTickInterval.
	TickInterval time.Duration
}

// Manager implements the synchronization domain. The PTP clock is driven by
// explicit events (Handle and SyncLoss), SyncE re-selects its source on every
// tick, and the offset/jitter of the clock are simulated from its state.
//
// The manager is safe for concurrent use. Configuration is read from the
// running datastore through Router.Snapshot; operational state is written with
// Router.SetState, which keeps running and candidate in step.
type Manager struct {
	router       *router.Router
	bus          *event.Bus
	clock        clock.Clock
	tickInterval time.Duration

	// mu serialises the read-modify-write cycles of Run, Handle and SyncLoss.
	mu stdsync.Mutex
	// state is the current PTP clock state. known reports whether it has been
	// adopted from the datastore or produced by a transition.
	state State
	known bool
	// offset is the last simulated clock offset in nanoseconds, and
	// holdoverOffset its value when the current holdover started.
	offset         float64
	holdoverOffset float64
	holdoverStart  time.Time
	timer          clock.Timer
	// expired receives the holdover timer's expiry, so Run applies it outside
	// the timer's goroutine.
	expired chan struct{}
	rng     *rand.Rand
}

// New builds the synchronization domain manager. It fails when no router is
// supplied.
func New(deps Deps) (*Manager, error) {
	if deps.Router == nil {
		return nil, ErrNoRouter
	}
	if deps.Clock == nil {
		deps.Clock = clock.RealClock{}
	}
	if deps.TickInterval <= 0 {
		deps.TickInterval = DefaultTickInterval
	}
	return &Manager{
		router:       deps.Router,
		bus:          deps.Bus,
		clock:        deps.Clock,
		tickInterval: deps.TickInterval,
		expired:      make(chan struct{}, 1),
		rng:          rand.New(rand.NewSource(rngSeed)),
	}, nil
}

// Clock exposes the manager's time source, which the periodic work and the
// holdover timer share.
func (m *Manager) Clock() clock.Clock { return m.clock }

// Run performs the periodic domain work until ctx is cancelled. The first tick
// runs immediately, so a restart adopts the persisted clock state without
// waiting for a period. Every period is scheduled on the injected clock, so
// tests drive it with FakeClock and never sleep.
func (m *Manager) Run(ctx context.Context) {
	if err := m.Tick(ctx); err != nil {
		slog.WarnContext(ctx, "sync: initial tick failed", "error", err)
	}

	ticks := make(chan struct{}, 1)
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.stopHoldover()
	}()
	for {
		timer := m.clock.AfterFunc(m.tickInterval, func() {
			select {
			case ticks <- struct{}{}:
			default:
			}
		})
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-ticks:
			if err := m.Tick(ctx); err != nil {
				slog.WarnContext(ctx, "sync: periodic tick failed", "error", err)
			}
		case <-m.expired:
			if err := m.expireHoldover(ctx); err != nil {
				slog.WarnContext(ctx, "sync: holdover expiry failed", "error", err)
			}
		}
	}
}

// Tick performs one period of background work: it adopts the persisted clock
// state, reconciles a master clock, re-runs the SyncE source selection and
// refreshes the simulated offset and jitter.
func (m *Manager) Tick(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	if err := m.reconcilePTP(ctx, device); err != nil {
		return err
	}
	if err := m.refreshSyncE(ctx, device); err != nil {
		return err
	}
	return m.refreshQuality(ctx, device)
}

// expireHoldover applies the holdover timer's expiry. It is a no-op unless the
// clock is still in holdover-in-spec, so a late or repeated expiry cannot
// resurrect a holdover that a restore already ended.
func (m *Manager) expireHoldover(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state != StateHoldoverInSpec {
		return nil
	}
	_, err := m.handleLocked(ctx, Event{Type: EventHoldoverExpired, Reason: "holdover timer expired"})
	return err
}

// publish puts one event on the bus when a bus is configured.
func (m *Manager) publish(kind event.EventType, resource, severity, message string) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(event.Event{
		Type:      kind,
		Resource:  resource,
		Severity:  severity,
		Message:   message,
		Timestamp: m.clock.Now(),
	})
}

// stateWrite writes one state leaf, which only the owning domain may do.
func (m *Manager) stateWrite(ctx context.Context, path string, value any) error {
	if _, err := m.router.SetState(ctx, path, value); err != nil {
		return fmt.Errorf("sync: write %s: %w", path, err)
	}
	return nil
}

// adopt records the clock state the datastore holds when the manager has not
// seen one yet. A state the model does not know is treated as freerun.
func (m *Manager) adopt(device *model.Device) {
	if m.known {
		return
	}
	state := State(device.PTP.State)
	if !validState(state) {
		state = StateFreerun
	}
	m.state = state
	m.known = true
	if state == StateHoldoverInSpec {
		m.startHoldover(device)
	}
}

// ensureState adopts the persisted clock state, reading a snapshot when the
// manager has not done so yet.
func (m *Manager) ensureState(ctx context.Context) error {
	if m.known {
		return nil
	}
	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	m.adopt(device)
	return nil
}

// reconcilePTP brings the clock state in line with the configuration: a master
// clock that is still free-running locks to its own reference.
func (m *Manager) reconcilePTP(ctx context.Context, device *model.Device) error {
	m.adopt(device)
	if device.PTP.Mode != model.PTPModeMaster || m.state != StateFreerun {
		return nil
	}
	if _, err := m.handleLocked(ctx, Event{Type: EventSourceDetected, Reason: "master clock reference present"}); err != nil {
		return err
	}
	_, err := m.handleLocked(ctx, Event{Type: EventCalibrationComplete, Reason: "master clock calibrated"})
	return err
}

// startHoldover schedules the holdover expiry on the injected clock. The timer
// only wakes Run, which applies the transition with the domain lock held.
func (m *Manager) startHoldover(device *model.Device) {
	m.stopHoldover()
	m.holdoverStart = m.clock.Now()
	m.holdoverOffset = m.offset
	timeout := time.Duration(device.PTP.HoldoverTimeout) * time.Second
	m.timer = m.clock.AfterFunc(timeout, func() {
		select {
		case m.expired <- struct{}{}:
		default:
		}
	})
}

// stopHoldover cancels a pending holdover expiry and forgets its start.
func (m *Manager) stopHoldover() {
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.holdoverStart = time.Time{}
	m.holdoverOffset = 0
}

// refreshQuality simulates the clock offset and jitter from the current state:
// a locked clock stays within the jitter bound, a holdover clock drifts from
// the offset it held when the source was lost, and a free-running clock wanders
// without a bound.
func (m *Manager) refreshQuality(ctx context.Context, device *model.Device) error {
	switch m.state {
	case StateLocked:
		m.offset = m.noise(DefaultJitterNS)
	case StateHoldoverInSpec, StateHoldoverOutOfSpec:
		elapsed := m.clock.Now().Sub(m.holdoverStart).Seconds()
		if elapsed < 0 {
			elapsed = 0
		}
		m.offset = m.holdoverOffset + DefaultDriftPPB*elapsed
	default: // freerun
		m.offset = m.noise(DefaultFreerunOffsetNS)
	}

	jitter := m.offset
	if m.state == StateHoldoverInSpec || m.state == StateHoldoverOutOfSpec {
		jitter = m.offset - m.holdoverOffset
	}
	if jitter < 0 {
		jitter = -jitter
	}

	var err error
	if device.PTP.Offset != m.offset {
		err = errors.Join(err, m.stateWrite(ctx, "ptp/clock/offset", m.offset))
	}
	if device.PTP.Jitter != jitter {
		err = errors.Join(err, m.stateWrite(ctx, "ptp/clock/jitter", jitter))
	}
	return err
}

// noise returns a value in [-scale, +scale] from the manager's deterministic
// generator.
func (m *Manager) noise(scale float64) float64 {
	return (m.rng.Float64()*2 - 1) * scale
}
