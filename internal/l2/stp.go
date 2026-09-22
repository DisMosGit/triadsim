package l2

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/store"
)

// ErrSTPDisabled reports an STP input while the bridge has STP disabled.
var ErrSTPDisabled = errors.New("stp is disabled")

// DefaultForwardDelay is the IEEE 802.1D listening and learning delay.
const DefaultForwardDelay = 15 * time.Second

// stpPrefix is the router path of the STP state container.
const stpPrefix = "stp/state"

// stpPath returns the router path of one STP state leaf.
func stpPath(leaf string) string { return stpPrefix + "/" + leaf }

// stpPortPath returns the router path of one leaf of a bridge port.
func stpPortPath(port, leaf string) string {
	return stpPrefix + "/ports/port[port=" + port + "]/" + leaf
}

// STPEventKind identifies one input of the simplified spanning-tree machine.
type STPEventKind string

// STP inputs understood by Handle.
const (
	// EventLinkUp reports that a bridge port started forwarding frames.
	EventLinkUp STPEventKind = "link-up"
	// EventLinkDown reports that a bridge port went down.
	EventLinkDown STPEventKind = "link-down"
	// EventBPDU reports a configuration BPDU received on a port.
	EventBPDU STPEventKind = "bpdu"
)

// STPEvent is one input of the simplified spanning-tree machine.
//
// A BPDU carries the announcing bridge's identity, so the machine can run its
// simplified root election: the bridge with the lower (priority, address) is
// the root, and a port that hears a better bridge becomes the root port.
type STPEvent struct {
	Kind STPEventKind
	// Port is the bridge port the input applies to.
	Port string
	// RootID, RootPriority and PathCost describe the BPDU received on Port.
	RootID       string
	RootPriority uint16
	PathCost     uint32
}

// STPPhase is one phase of the simplified state machine. It is the classic
// IEEE 802.1D walk; an RSTP bridge collapses the first three phases into
// discarding when the phase is written to the model.
type STPPhase string

// The phases of the simplified state machine.
const (
	STPPhaseDisabled   STPPhase = "disabled"
	STPPhaseBlocking   STPPhase = "blocking"
	STPPhaseListening  STPPhase = "listening"
	STPPhaseLearning   STPPhase = "learning"
	STPPhaseForwarding STPPhase = "forwarding"
)

// stpDeadline is one scheduled phase change of a bridge port.
type stpDeadline struct {
	at    time.Time
	phase STPPhase
}

// STPState returns the current spanning-tree state of the bridge.
func (m *Manager) STPState(ctx context.Context) (model.STPState, error) {
	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return model.STPState{}, fmt.Errorf("l2: stp state: %w", err)
	}
	return device.STP, nil
}

// Handle applies one spanning-tree input to a bridge port: a link change or a
// received BPDU. The port's role and state are written to the datastore, the
// transition is published on the bus and the next phase change, if any, is
// scheduled on the injected clock.
func (m *Manager) Handle(ctx context.Context, ev STPEvent) error {
	if ev.Port == "" {
		return errors.New("l2: stp event without a port")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return fmt.Errorf("l2: stp event: %w", err)
	}
	state := device.STP
	if !state.Enabled {
		return ErrSTPDisabled
	}
	port, ok := findSTPPort(state.Ports, ev.Port)
	if !ok {
		return fmt.Errorf("%w: %s", ErrPortUnknown, ev.Port)
	}

	switch ev.Kind {
	case EventLinkDown:
		return m.transition(ctx, &state, port.Port, model.STPPortRoleAlternate, STPPhaseDisabled, "link down")

	case EventLinkUp:
		phase := STPPhaseBlocking
		if port.EdgePort {
			phase = STPPhaseForwarding
		}
		return m.transition(ctx, &state, port.Port, model.STPPortRoleDesignated, phase, "link up")

	case EventBPDU:
		return m.elect(ctx, &state, ev, port)

	default:
		return fmt.Errorf("l2: unknown stp event %q", ev.Kind)
	}
}

// elect runs the simplified root election for a received BPDU: a bridge that
// hears a better bridge identifier becomes a non-root bridge and makes the
// receiving port its root port; otherwise the bridge keeps the root role and
// the port stays designated. Only the root identifier and the root path cost
// are recorded, so the announcing bridge's own priority is not stored.
func (m *Manager) elect(ctx context.Context, state *model.STPState, ev STPEvent, port model.STPPort) error {
	better := compareBridgeID(ev.RootPriority, ev.RootID, state.BridgePriority, state.BridgeAddress) < 0

	rootID := state.BridgeAddress
	rootCost := uint32(0)
	if better {
		rootID = ev.RootID
		rootCost = ev.PathCost + port.PathCost
	}
	if err := m.state(ctx, stpPath("root-id"), rootID); err != nil {
		return err
	}
	if err := m.state(ctx, stpPath("root-cost"), rootCost); err != nil {
		return err
	}
	state.RootID, state.RootCost = rootID, rootCost

	// Ports that are not the root port become alternate ports when another
	// bridge is the root, and designated ports when this bridge is the root.
	for i := range state.Ports {
		other := &state.Ports[i]
		if other.Port == port.Port {
			continue
		}
		role, phase := model.STPPortRoleDesignated, STPPhaseForwarding
		if better {
			role, phase = model.STPPortRoleAlternate, STPPhaseBlocking
		}
		if err := m.transition(ctx, state, other.Port, role, phase, "root election"); err != nil {
			return err
		}
	}

	role, phase := model.STPPortRoleDesignated, STPPhaseForwarding
	if better {
		// A root port forwards at once under RSTP; classic STP walks the
		// forward delays first.
		role, phase = model.STPPortRoleRoot, STPPhaseBlocking
		if state.Protocol == model.STPProtocolRSTP {
			phase = STPPhaseForwarding
		}
	}
	return m.transition(ctx, state, port.Port, role, phase, "bpdu from "+ev.RootID)
}

// transition writes a new role and phase for one port, publishes the change and
// schedules the next forward-delay step. A port that already is in the target
// role and phase is left alone.
func (m *Manager) transition(ctx context.Context, state *model.STPState, port, role string, phase STPPhase, reason string) error {
	name := stateName(state.Protocol, phase)
	current := currentPort(state, port)
	if current.Role == role && current.State == name {
		return nil
	}
	if err := m.setSTPPort(ctx, state, port, role, name); err != nil {
		return err
	}
	m.publish(event.TypeStateTransition, "l2/stp/"+port, "",
		fmt.Sprintf("stp %s: %s -> %s (%s)", port, current.State, name, reason))
	m.scheduleSTP(port, phase)
	return nil
}

// advanceSTP applies the phase changes whose forward delay has elapsed.
func (m *Manager) advanceSTP(ctx context.Context) error {
	if len(m.stpPending) == 0 {
		return nil
	}
	now := m.clock.Now()
	due := make([]string, 0, len(m.stpPending))
	for port, deadline := range m.stpPending {
		if !now.Before(deadline.at) {
			due = append(due, port)
		}
	}
	if len(due) == 0 {
		return nil
	}
	sort.Strings(due)

	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return fmt.Errorf("l2: stp forward delay: %w", err)
	}
	state := device.STP
	if !state.Enabled {
		m.stpPending = make(map[string]stpDeadline)
		return nil
	}
	for _, port := range due {
		deadline := m.stpPending[port]
		delete(m.stpPending, port)
		current, ok := findSTPPort(state.Ports, port)
		if !ok {
			continue
		}
		if err := m.transition(ctx, &state, port, current.Role, deadline.phase, "forward delay"); err != nil {
			return err
		}
	}
	return nil
}

// scheduleSTP records the next phase change a port walks through. Entering the
// learning phase arms the forwarding change, and a blocking or listening port
// arms learning; forwarding, disabled and an unknown port cancel the timer.
func (m *Manager) scheduleSTP(port string, phase STPPhase) {
	if m.stpPending == nil {
		m.stpPending = make(map[string]stpDeadline)
	}
	switch phase {
	case STPPhaseBlocking, STPPhaseListening:
		m.stpPending[port] = stpDeadline{at: m.clock.Now().Add(m.forwardDelay), phase: STPPhaseLearning}
	case STPPhaseLearning:
		m.stpPending[port] = stpDeadline{at: m.clock.Now().Add(m.forwardDelay), phase: STPPhaseForwarding}
	default:
		delete(m.stpPending, port)
	}
}

// stateName maps a machine phase to the port-state vocabulary of a protocol.
func stateName(protocol string, phase STPPhase) string {
	if protocol == model.STPProtocolRSTP {
		switch phase {
		case STPPhaseDisabled, STPPhaseBlocking, STPPhaseListening:
			return model.STPPortStateDiscarding
		}
	}
	return string(phase)
}

// setSTPPort writes the role and state of one bridge port and keeps the passed
// snapshot in step, so a caller may transition several ports in one pass.
func (m *Manager) setSTPPort(ctx context.Context, state *model.STPState, port, role, name string) error {
	if _, err := m.router.Set(ctx, store.Running, stpPortPath(port, "role"), role); err != nil {
		return fmt.Errorf("l2: stp %s: set role: %w", port, err)
	}
	if _, err := m.router.Set(ctx, store.Running, stpPortPath(port, "state"), name); err != nil {
		return fmt.Errorf("l2: stp %s: set state: %w", port, err)
	}
	for i := range state.Ports {
		if state.Ports[i].Port == port {
			state.Ports[i].Role = role
			state.Ports[i].State = name
			return nil
		}
	}
	return nil
}

// findSTPPort returns the bridge port with the given name.
func findSTPPort(ports []model.STPPort, port string) (model.STPPort, bool) {
	for _, candidate := range ports {
		if candidate.Port == port {
			return candidate, true
		}
	}
	return model.STPPort{}, false
}

// currentPort returns the bridge port with the given name, or a zero port.
func currentPort(state *model.STPState, port string) model.STPPort {
	found, _ := findSTPPort(state.Ports, port)
	return found
}

// compareBridgeID compares two bridge identifiers, the 8-byte (priority,
// address) pair of IEEE 802.1D. It returns -1 when a is better (lower).
func compareBridgeID(aPriority uint16, aID string, bPriority uint16, bID string) int {
	if aPriority != bPriority {
		if aPriority < bPriority {
			return -1
		}
		return 1
	}
	return strings.Compare(normalizeMACString(aID), normalizeMACString(bID))
}

// normalizeMACString lowercases a MAC address for comparison, leaving the text
// alone when it does not parse.
func normalizeMACString(text string) string {
	address, err := normalizeMAC(text)
	if err != nil {
		return text
	}
	return address
}

// STPPorts returns the bridge ports ordered by port name.
func (m *Manager) STPPorts(ctx context.Context) ([]model.STPPort, error) {
	state, err := m.STPState(ctx)
	if err != nil {
		return nil, err
	}
	ports := slices.Clone(state.Ports)
	slices.SortFunc(ports, func(a, b model.STPPort) int { return strings.Compare(a.Port, b.Port) })
	return ports, nil
}
