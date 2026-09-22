package radio

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
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
	ErrNoRouter = errors.New("radio: router must not be nil")
	// ErrUnknownLink reports a radio link the device does not have.
	ErrUnknownLink = errors.New("radio: unknown radio link")
)

// Defaults of the periodic work and of the simulated alarm levels. The link
// budget, fade margin and alarm thresholds follow docs/protocols/RADIO-RRL.md.
const (
	// DefaultTickInterval is the period of the domain's background work: the
	// link-budget recalculation, the ATPC step and the ACM selection.
	DefaultTickInterval = 5 * time.Second
	// DefaultRSSIAlarmThreshold is the received level below which the link is
	// reported down.
	DefaultRSSIAlarmThreshold = -85.0
	// DefaultRSSIClearThreshold is the received level a down link must recover
	// past before it is reported up again.
	DefaultRSSIClearThreshold = -82.0
	// DefaultFadeMarginMinDB is the fade margin below which the link is
	// reported degraded.
	DefaultFadeMarginMinDB = 6.0
	// DefaultFadeMarginClearDB is the fade margin a degraded link must recover
	// to before it is reported healthy again.
	DefaultFadeMarginClearDB = 8.0
	// DefaultATPCStepDB is the largest transmit-power change of one step.
	DefaultATPCStepDB = 1.0
	// DefaultFailureFadeDB is the fade a radio failure injects.
	DefaultFailureFadeDB = 60.0
	// DefaultDegradeFadeDB is the fade that typically lands a link in the
	// degraded band; callers may inject any depth they like.
	DefaultDegradeFadeDB = 32.0
	// MaxInjectedFadeDB bounds one injection.
	MaxInjectedFadeDB = 200.0
)

// Deps is what the radio domain needs from the rest of the simulator.
type Deps struct {
	// Router is the path-to-model mapping the manager reads snapshots and
	// writes state through. It is required.
	Router *router.Router
	// Bus publishes domain events: the radio-link alarms. A nil bus swallows
	// events, which unit tests rely on.
	Bus *event.Bus
	// Clock is the injected time source. RealClock is used when nil.
	Clock clock.Clock
	// TickInterval is the period of Run's background work. Zero selects
	// DefaultTickInterval.
	TickInterval time.Duration
	// RSSIAlarmThreshold, RSSIClearThreshold, FadeMarginMinDB,
	// FadeMarginClearDB and ATPCStepDB override the alarm levels and the ATPC
	// step. Zero selects the matching default.
	RSSIAlarmThreshold float64
	RSSIClearThreshold float64
	FadeMarginMinDB    float64
	FadeMarginClearDB  float64
	ATPCStepDB         float64
	// FailureFadeDB is the fade depth a radio failure injects by default.
	// Zero selects DefaultFailureFadeDB.
	FailureFadeDB float64
}

// Manager implements the radio-link (RRL) domain: the link budget, RSSI, fade
// margin and capacity, the ATPC control loop, the ACM profile selection and the
// radioLinkDown/radioLinkDegraded alarms.
//
// The manager is safe for concurrent use. Configuration and the seeded state
// are read from the running datastore through Router.Snapshot; measured state
// is written with Router.SetState, which keeps running and candidate in step.
type Manager struct {
	router       *router.Router
	bus          *event.Bus
	clock        clock.Clock
	tickInterval time.Duration

	rssiAlarmThreshold float64
	rssiClearThreshold float64
	fadeMarginMinDB    float64
	fadeMarginClearDB  float64
	atpcStepDB         float64
	failureFadeDB      float64

	// mu serialises the read-modify-write cycles of Run, Tick and the
	// simulation helpers.
	mu sync.Mutex
	// fade is the fade depth a simulation injected into one link, in dB.
	fade map[string]float64
	// states is the last link state the manager reported, known tracks which
	// links it has adopted from the datastore already.
	states map[string]string
	known  map[string]bool
}

// New builds the radio domain manager. It fails when no router is supplied.
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
	if deps.RSSIAlarmThreshold == 0 {
		deps.RSSIAlarmThreshold = DefaultRSSIAlarmThreshold
	}
	if deps.RSSIClearThreshold == 0 {
		deps.RSSIClearThreshold = DefaultRSSIClearThreshold
	}
	if deps.FadeMarginMinDB == 0 {
		deps.FadeMarginMinDB = DefaultFadeMarginMinDB
	}
	if deps.FadeMarginClearDB == 0 {
		deps.FadeMarginClearDB = DefaultFadeMarginClearDB
	}
	if deps.ATPCStepDB == 0 {
		deps.ATPCStepDB = DefaultATPCStepDB
	}
	if deps.FailureFadeDB == 0 {
		deps.FailureFadeDB = DefaultFailureFadeDB
	}
	return &Manager{
		router:             deps.Router,
		bus:                deps.Bus,
		clock:              deps.Clock,
		tickInterval:       deps.TickInterval,
		rssiAlarmThreshold: deps.RSSIAlarmThreshold,
		rssiClearThreshold: deps.RSSIClearThreshold,
		fadeMarginMinDB:    deps.FadeMarginMinDB,
		fadeMarginClearDB:  deps.FadeMarginClearDB,
		atpcStepDB:         deps.ATPCStepDB,
		failureFadeDB:      deps.FailureFadeDB,
		fade:               make(map[string]float64),
		states:             make(map[string]string),
		known:              make(map[string]bool),
	}, nil
}

// Run performs the periodic domain work until ctx is cancelled. The first tick
// runs immediately, so a restart publishes the alarm state of the persisted
// device without waiting for a period. Every period is scheduled on the
// injected clock, so tests drive it with FakeClock and never sleep.
func (m *Manager) Run(ctx context.Context) {
	if err := m.Tick(ctx); err != nil {
		slog.WarnContext(ctx, "radio: initial tick failed", "error", err)
	}

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
				slog.WarnContext(ctx, "radio: periodic tick failed", "error", err)
			}
		}
	}
}

// Tick recalculates every radio link once: the link budget, the ATPC step, the
// ACM profile selection and the alarm state machine. It returns the joined
// error of the links it could not write.
func (m *Manager) Tick(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return fmt.Errorf("radio: %w", err)
	}

	var errs error
	for i := range device.Interfaces {
		iface := device.Interfaces[i]
		if iface.RadioLink == nil {
			continue
		}
		errs = errors.Join(errs, m.evaluateLink(ctx, iface))
	}
	return errs
}

// RadioFailure injects a fade into a radio link, so the link budget, the fade
// margin and the alarms reflect a failing link. An empty link selects the first
// radio interface; fadeDB selects the depth and DefaultFailureFadeDB is used
// when it is not positive. The returned state is the link state after the
// injection.
func (m *Manager) RadioFailure(ctx context.Context, link string, fadeDB float64) (string, error) {
	return m.inject(ctx, link, fadeDB, false)
}

// RadioRestore removes the fade a simulation injected into a radio link and
// re-evaluates it. The returned state is the link state after the restore.
func (m *Manager) RadioRestore(ctx context.Context, link string) (string, error) {
	return m.inject(ctx, link, 0, true)
}

// inject applies or clears the simulated fade of one link and evaluates it.
func (m *Manager) inject(ctx context.Context, link string, fadeDB float64, restore bool) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return "", fmt.Errorf("radio: %w", err)
	}
	iface, ok := findRadioInterface(device, link)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownLink, link)
	}

	if restore {
		delete(m.fade, iface.Name)
	} else {
		if fadeDB <= 0 {
			fadeDB = m.failureFadeDB
		}
		m.fade[iface.Name] = math.Min(fadeDB, MaxInjectedFadeDB)
	}

	if err := m.evaluateLink(ctx, *iface); err != nil {
		return m.states[iface.Name], err
	}
	return m.states[iface.Name], nil
}

// stateWrite is one measured leaf the domain writes after a recalculation.
type stateWrite struct {
	path  string
	value any
}

// evaluateLink recalculates one radio link and writes its measured state. The
// caller must hold the domain lock.
func (m *Manager) evaluateLink(ctx context.Context, iface model.Interface) error {
	link := *iface.RadioLink
	name := iface.Name
	txPower := effectiveTxPower(link)
	clearSky := receivedLevel(link, txPower)
	rssi := applyFade(clearSky, m.fade[name])

	profile, hasProfile := selectProfile(link.ACM, link.Profiles, rssi)
	fadeMargin := math.Inf(1)
	capacity := link.Capacity
	if hasProfile {
		fadeMargin = rssi - profile.RSLThreshold
		capacity = profile.Capacity
	}

	writes := []stateWrite{
		{linkPath(name, "rssi"), rssi},
		{linkPath(name, "link-budget/calculated-rsl"), applyFade(clearSky, 0)},
	}
	if hasProfile {
		writes = append(writes,
			stateWrite{linkPath(name, "fade-margin"), fadeMargin},
			stateWrite{linkPath(name, "capacity"), capacity},
			stateWrite{linkPath(name, "acm/current-profile"), profile.ID},
			stateWrite{linkPath(name, "acm/current-capacity"), profile.Capacity},
		)
	}
	if link.ATPC.Enabled {
		writes = append(writes, stateWrite{linkPath(name, "atpc/current-power"), atpcStep(link.ATPC, rssi, m.atpcStepDB)})
	}

	var errs error
	for _, write := range writes {
		if _, err := m.router.SetState(ctx, write.path, write.value); err != nil {
			errs = errors.Join(errs, fmt.Errorf("radio: write %s: %w", write.path, err))
		}
	}
	if errs != nil {
		return errs
	}

	current := m.states[name]
	if !m.known[name] {
		current = link.LinkState
		if !validLinkState(current) {
			current = model.RadioLinkStateUp
		}
		m.known[name] = true
	}
	next := nextLinkState(current, rssi, fadeMargin, m.thresholds())
	if next != current {
		if _, err := m.router.SetState(ctx, linkPath(name, "link-state"), next); err != nil {
			return fmt.Errorf("radio: write %s: %w", linkPath(name, "link-state"), err)
		}
		m.report(name, current, next, rssi, fadeMargin)
	}
	m.states[name] = next
	return nil
}

// thresholds returns the alarm levels of the domain.
func (m *Manager) thresholds() thresholds {
	return thresholds{
		rssiAlarm: m.rssiAlarmThreshold,
		rssiClear: m.rssiClearThreshold,
		fadeMin:   m.fadeMarginMinDB,
		fadeClear: m.fadeMarginClearDB,
	}
}

// publish stamps and puts one event on the bus when a bus is configured.
func (m *Manager) publish(e event.Event) {
	if m.bus == nil {
		return
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = m.clock.Now()
	}
	m.bus.Publish(e)
}

// findRadioInterface returns the radio interface a link name addresses. An
// empty name selects the device's first radio interface.
func findRadioInterface(device *model.Device, link string) (*model.Interface, bool) {
	for i := range device.Interfaces {
		iface := &device.Interfaces[i]
		if iface.RadioLink == nil {
			continue
		}
		if link == "" || iface.Name == link {
			return iface, true
		}
	}
	return nil, false
}

// validLinkState reports whether the state is one the model knows.
func validLinkState(state string) bool {
	switch state {
	case model.RadioLinkStateUp, model.RadioLinkStateDegraded, model.RadioLinkStateDown:
		return true
	default:
		return false
	}
}

// linkPath returns the model path of a leaf of one interface's radio link.
func linkPath(ifaceName, suffix string) string {
	return "interfaces/interface[name=" + ifaceName + "]/radio-link/" + suffix
}
