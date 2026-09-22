package l2

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/router"
)

// Domain errors returned by the manager. Callers match them with errors.Is.
var (
	// ErrVLANNotFound reports an unknown VLAN identifier.
	ErrVLANNotFound = errors.New("vlan not found")
	// ErrVLANExists reports a VLAN identifier that is already configured.
	ErrVLANExists = errors.New("vlan already exists")
	// ErrMemberNotFound reports a port that is not a member of the VLAN.
	ErrMemberNotFound = errors.New("vlan member not found")
	// ErrMACNotFound reports an unknown forwarding-database entry.
	ErrMACNotFound = errors.New("mac entry not found")
	// ErrInvalidMAC reports a MAC address that is not 48 bits.
	ErrInvalidMAC = errors.New("invalid mac address")
	// ErrTableFull reports a MAC table at its max-entries limit.
	ErrTableFull = errors.New("mac table is full")
)

// DefaultTickInterval is the period of the domain's background work: MAC aging
// and the periodic LLDP refresh.
const DefaultTickInterval = 5 * time.Second

// Deps is what the L2 domain needs from the rest of the simulator.
type Deps struct {
	// Router is the path-to-model mapping the manager reads snapshots and
	// writes state through. It is required.
	Router *router.Router
	// Bus publishes domain events: STP transitions and storm alarms. A nil bus
	// swallows events, which unit tests rely on.
	Bus *event.Bus
	// Clock is the injected time source. RealClock is used when nil.
	Clock clock.Clock
	// TickInterval is the period of Run's background work. Zero selects
	// DefaultTickInterval.
	TickInterval time.Duration
}

// Manager implements the L2 switching domain: VLANs and QinQ, the MAC
// forwarding database, the simplified STP/RSTP machine, LLDP neighbours,
// interface counters and broadcast-storm simulation.
//
// The manager is safe for concurrent use. Configuration is written to the
// running datastore, the way a management plane writes it; learned and
// measured state goes through Router.SetState, which also keeps candidate in
// step.
type Manager struct {
	router       *router.Router
	bus          *event.Bus
	clock        clock.Clock
	tickInterval time.Duration

	// mu serialises the read-modify-write cycles of the background work
	// (aging, LLDP refresh, counter bumps) and storm accounting.
	mu sync.Mutex

	// lastAging is the clock reading of the previous MAC-aging sweep. The zero
	// value means no sweep has run yet.
	lastAging time.Time
}

// New builds the L2 domain manager. It fails when no router is supplied.
func New(deps Deps) (*Manager, error) {
	if deps.Router == nil {
		return nil, errors.New("l2: router must not be nil")
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
	}, nil
}

// Clock exposes the manager's time source, which the periodic work and the
// storm rate window share.
func (m *Manager) Clock() clock.Clock { return m.clock }

// Run performs the periodic domain work — MAC aging and the LLDP refresh —
// until ctx is cancelled. Every period is scheduled on the injected clock, so
// tests drive it with FakeClock and never sleep.
func (m *Manager) Run(ctx context.Context) {
	ticks := make(chan struct{}, 1)
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
				slog.WarnContext(ctx, "l2: periodic tick failed", "error", err)
			}
		}
	}
}

// Tick performs one period of background work and returns the joined error of
// its steps.
func (m *Manager) Tick(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.age(ctx)
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

// state writes one state leaf, which only the owning domain may do.
func (m *Manager) state(ctx context.Context, path string, value any) error {
	if _, err := m.router.SetState(ctx, path, value); err != nil {
		return err
	}
	return nil
}

// erase removes one state leaf. A leaf that is already gone is not an error.
func (m *Manager) erase(ctx context.Context, path string) error {
	if err := m.router.DeleteState(ctx, path); err != nil && !errors.Is(err, router.ErrNotFound) {
		return err
	}
	return nil
}
