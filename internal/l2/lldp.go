package l2

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/store"
)

// ErrNeighborNotFound reports an unknown LLDP neighbour.
var ErrNeighborNotFound = errors.New("lldp neighbour not found")

// lldpPrefix is the router path of the LLDP container.
const lldpPrefix = "lldp"

// lldpNeighborPrefix is the router path of one neighbour entry.
func lldpNeighborPrefix(port string) string {
	return lldpPrefix + "/neighbors/neighbor[port=" + port + "]"
}

// LLDP returns the local LLDP configuration.
func (m *Manager) LLDP(ctx context.Context) (model.LLDPConfig, error) {
	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return model.LLDPConfig{}, fmt.Errorf("l2: lldp config: %w", err)
	}
	return device.LLDP, nil
}

// Neighbors returns the remote LLDP neighbours ordered by local port.
func (m *Manager) Neighbors(ctx context.Context) ([]model.LLDPNeighbor, error) {
	config, err := m.LLDP(ctx)
	if err != nil {
		return nil, err
	}
	neighbors := slices.Clone(config.Neighbors)
	slices.SortFunc(neighbors, func(a, b model.LLDPNeighbor) int { return strings.Compare(a.Port, b.Port) })
	return neighbors, nil
}

// Neighbor returns the neighbour learned on one local port, or
// ErrNeighborNotFound.
func (m *Manager) Neighbor(ctx context.Context, port string) (model.LLDPNeighbor, error) {
	neighbors, err := m.Neighbors(ctx)
	if err != nil {
		return model.LLDPNeighbor{}, err
	}
	for _, neighbor := range neighbors {
		if neighbor.Port == port {
			return neighbor, nil
		}
	}
	return model.LLDPNeighbor{}, fmt.Errorf("%w: %s", ErrNeighborNotFound, port)
}

// SetNeighbor adds or replaces the remote neighbour announced on one local
// port. The entry is validated first, so a neighbour without a chassis or port
// identifier or with a TTL outside 1..65535 is rejected before anything is
// stored.
func (m *Manager) SetNeighbor(ctx context.Context, neighbor model.LLDPNeighbor) error {
	if err := neighbor.Validate(); err != nil {
		return fmt.Errorf("l2: lldp neighbour: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	fields := []struct {
		name  string
		value any
	}{
		{"chassis-id", neighbor.ChassisID},
		{"port-id", neighbor.PortID},
		{"system-name", neighbor.SystemName},
		{"system-description", neighbor.SystemDescription},
		{"ttl", neighbor.TTL},
		{"capabilities", neighbor.Capabilities},
	}
	for _, field := range fields {
		path := lldpNeighborPrefix(neighbor.Port) + "/" + field.name
		if _, err := m.router.Set(ctx, store.Running, path, field.value); err != nil {
			return fmt.Errorf("l2: lldp neighbour %s: set %s: %w", neighbor.Port, field.name, err)
		}
	}
	return nil
}

// RemoveNeighbor deletes the neighbour on one local port. It reports
// ErrNeighborNotFound when no neighbour is known on that port.
func (m *Manager) RemoveNeighbor(ctx context.Context, port string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := m.Neighbor(ctx, port); err != nil {
		return err
	}
	return m.removeSubtree(ctx, store.Running, lldpNeighborPrefix(port))
}

// refreshLLDP is the periodic TLV exchange of the simplified LLDP: every
// advertised neighbour is refreshed to the configured TTL, tx-interval times
// tx-hold-multiplier, so a neighbour stays in the table as long as the peer
// keeps announcing itself. A disabled LLDP transmitter does nothing.
func (m *Manager) refreshLLDP(ctx context.Context) error {
	config, err := m.LLDP(ctx)
	if err != nil {
		return err
	}
	if !config.Enabled || len(config.Neighbors) == 0 {
		return nil
	}
	ttl := config.TxInterval * config.TxHoldMultiplier
	if ttl == 0 || ttl > model.LLDPTTLMax {
		return nil
	}

	for _, neighbor := range config.Neighbors {
		if neighbor.TTL == ttl {
			continue
		}
		path := lldpNeighborPrefix(neighbor.Port) + "/ttl"
		if _, err := m.router.Set(ctx, store.Running, path, ttl); err != nil {
			return fmt.Errorf("l2: lldp refresh %s: %w", neighbor.Port, err)
		}
	}
	return nil
}
