package l2

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/store"
)

// VLANs returns every configured VLAN ordered by identifier.
func (m *Manager) VLANs(ctx context.Context) ([]model.VLAN, error) {
	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return nil, fmt.Errorf("l2: list vlans: %w", err)
	}
	vlans := slices.Clone(device.VLANs)
	slices.SortFunc(vlans, func(a, b model.VLAN) int { return int(a.ID) - int(b.ID) })
	return vlans, nil
}

// VLAN looks up one VLAN by identifier. It reports ErrVLANNotFound when the
// VLAN is not configured.
func (m *Manager) VLAN(ctx context.Context, id uint16) (model.VLAN, error) {
	vlans, err := m.VLANs(ctx)
	if err != nil {
		return model.VLAN{}, err
	}
	vlan, ok := findVLAN(vlans, id)
	if !ok {
		return model.VLAN{}, fmt.Errorf("%w: %d", ErrVLANNotFound, id)
	}
	return vlan, nil
}

// CreateVLAN writes a new VLAN with its member ports. The VLAN is validated
// first, so an out-of-range identifier, an unknown port mode or an inconsistent
// QinQ tag pair is rejected before anything is stored. Creating an identifier
// that already exists reports ErrVLANExists.
func (m *Manager) CreateVLAN(ctx context.Context, vlan model.VLAN) error {
	if err := vlan.Validate(); err != nil {
		return fmt.Errorf("l2: vlan %d: %w", vlan.ID, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := m.VLAN(ctx, vlan.ID); err == nil {
		return fmt.Errorf("%w: %d", ErrVLANExists, vlan.ID)
	} else if !errors.Is(err, ErrVLANNotFound) {
		return err
	}

	if err := m.writeVLAN(ctx, vlan); err != nil {
		return err
	}
	for _, member := range vlan.Ports {
		if err := m.writeMember(ctx, vlan.ID, member); err != nil {
			return err
		}
	}
	return nil
}

// ReplaceVLAN overwrites the name, description and member list of an existing
// VLAN. Members that are no longer listed are removed. It reports
// ErrVLANNotFound when the VLAN does not exist.
func (m *Manager) ReplaceVLAN(ctx context.Context, vlan model.VLAN) error {
	if err := vlan.Validate(); err != nil {
		return fmt.Errorf("l2: vlan %d: %w", vlan.ID, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	current, err := m.VLAN(ctx, vlan.ID)
	if err != nil {
		return err
	}
	for _, member := range current.Ports {
		if _, keep := findMember(vlan.Ports, member.Port); !keep {
			if err := m.removeMember(ctx, vlan.ID, member.Port); err != nil {
				return err
			}
		}
	}
	if err := m.writeVLAN(ctx, vlan); err != nil {
		return err
	}
	for _, member := range vlan.Ports {
		if err := m.writeMember(ctx, vlan.ID, member); err != nil {
			return err
		}
	}
	return nil
}

// DeleteVLAN removes a VLAN and every member port it holds. It reports
// ErrVLANNotFound when the VLAN does not exist.
func (m *Manager) DeleteVLAN(ctx context.Context, id uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := m.VLAN(ctx, id); err != nil {
		return err
	}
	return m.removeSubtree(ctx, store.Running, vlanPrefix(id))
}

// Members returns the member ports of a VLAN ordered by port name.
func (m *Manager) Members(ctx context.Context, id uint16) ([]model.VLANPort, error) {
	vlan, err := m.VLAN(ctx, id)
	if err != nil {
		return nil, err
	}
	members := slices.Clone(vlan.Ports)
	slices.SortFunc(members, func(a, b model.VLANPort) int {
		switch {
		case a.Port < b.Port:
			return -1
		case a.Port > b.Port:
			return 1
		default:
			return 0
		}
	})
	return members, nil
}

// Member reports the membership of port in a VLAN, or ErrMemberNotFound.
func (m *Manager) Member(ctx context.Context, id uint16, port string) (model.VLANPort, error) {
	vlan, err := m.VLAN(ctx, id)
	if err != nil {
		return model.VLANPort{}, err
	}
	member, ok := findMember(vlan.Ports, port)
	if !ok {
		return model.VLANPort{}, fmt.Errorf("%w: vlan %d port %s", ErrMemberNotFound, id, port)
	}
	return member, nil
}

// SetMember adds port to a VLAN or replaces its membership when it is already
// present. The membership is validated before it is stored, so an access port
// cannot be tagged and a QinQ outer tag must differ from the inner PVID.
func (m *Manager) SetMember(ctx context.Context, id uint16, member model.VLANPort) error {
	if err := member.Validate(); err != nil {
		return fmt.Errorf("l2: vlan %d: %w", id, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := m.VLAN(ctx, id); err != nil {
		return err
	}
	return m.writeMember(ctx, id, member)
}

// RemoveMember deletes port from a VLAN. It reports ErrVLANNotFound for an
// unknown VLAN and ErrMemberNotFound when the port is not a member.
func (m *Manager) RemoveMember(ctx context.Context, id uint16, port string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := m.Member(ctx, id, port); err != nil {
		return err
	}
	return m.removeMember(ctx, id, port)
}

// PortVLANs returns every VLAN that port is a member of, ordered by VLAN
// identifier. An access port is a member of the VLAN whose PVID it carries
// even without an explicit member entry, so that VLAN is included too.
func (m *Manager) PortVLANs(ctx context.Context, port string) ([]model.VLAN, error) {
	vlans, err := m.VLANs(ctx)
	if err != nil {
		return nil, err
	}

	members := make([]model.VLAN, 0, len(vlans))
	for _, vlan := range vlans {
		if _, ok := findMember(vlan.Ports, port); ok {
			members = append(members, vlan)
		}
	}
	return members, nil
}

// Accepts reports whether port carries traffic of vlanID, that is, whether it
// is a member of that VLAN. An access port is a member of the VLAN whose PVID
// it carries, a trunk port is a member of every VLAN it is listed in.
func (m *Manager) Accepts(ctx context.Context, port string, vlanID uint16) (bool, error) {
	vlan, err := m.VLAN(ctx, vlanID)
	if err != nil {
		if errors.Is(err, ErrVLANNotFound) {
			return false, nil
		}
		return false, err
	}
	_, ok := findMember(vlan.Ports, port)
	return ok, nil
}

// TagStack returns the 802.1Q tags a frame carries on port for a VLAN: the
// inner (customer) tag and, on a QinQ port, the outer (service) tag. A port
// that sends the VLAN untagged has an inner tag of zero. It reports
// ErrMemberNotFound when the port is not a member of the VLAN.
func (m *Manager) TagStack(ctx context.Context, id uint16, port string) (inner uint16, outer uint16, err error) {
	member, err := m.Member(ctx, id, port)
	if err != nil {
		return 0, 0, err
	}
	if member.Tagged {
		inner = member.PVID
	}
	if member.QinQ {
		outer = member.OuterVID
	}
	return inner, outer, nil
}

// findVLAN returns the VLAN with id from vlans.
func findVLAN(vlans []model.VLAN, id uint16) (model.VLAN, bool) {
	for _, vlan := range vlans {
		if vlan.ID == id {
			return vlan, true
		}
	}
	return model.VLAN{}, false
}

// findMember returns the member entry of port from members.
func findMember(members []model.VLANPort, port string) (model.VLANPort, bool) {
	for _, member := range members {
		if member.Port == port {
			return member, true
		}
	}
	return model.VLANPort{}, false
}

// vlanPrefix is the router path of one VLAN list instance.
func vlanPrefix(id uint16) string {
	return "vlans/vlan[id=" + strconv.FormatUint(uint64(id), 10) + "]"
}

// memberPrefix is the router path of one member port of a VLAN.
func memberPrefix(id uint16, port string) string {
	return vlanPrefix(id) + "/ports/port[port=" + port + "]"
}

// writeVLAN stores the scalar fields of one VLAN.
func (m *Manager) writeVLAN(ctx context.Context, vlan model.VLAN) error {
	fields := []struct {
		name  string
		value any
	}{
		{"name", vlan.Name},
		{"description", vlan.Description},
	}
	for _, field := range fields {
		path := vlanPrefix(vlan.ID) + "/" + field.name
		if _, err := m.router.Set(ctx, store.Running, path, field.value); err != nil {
			return fmt.Errorf("l2: vlan %d: set %s: %w", vlan.ID, field.name, err)
		}
	}
	return nil
}

// writeMember stores every leaf of one member port.
func (m *Manager) writeMember(ctx context.Context, id uint16, member model.VLANPort) error {
	fields := []struct {
		name  string
		value any
	}{
		{"mode", member.Mode},
		{"pvid", member.PVID},
		{"tagged", member.Tagged},
		{"qinq", member.QinQ},
		{"s-tag-vid", member.OuterVID},
		{"c-tag-handling", member.CTagHandling},
	}
	for _, field := range fields {
		path := memberPrefix(id, member.Port) + "/" + field.name
		if _, err := m.router.Set(ctx, store.Running, path, field.value); err != nil {
			return fmt.Errorf("l2: vlan %d port %s: set %s: %w", id, member.Port, field.name, err)
		}
	}
	return nil
}

// removeMember deletes every leaf of one member port without checking that the
// member exists.
func (m *Manager) removeMember(ctx context.Context, id uint16, port string) error {
	return m.removeSubtree(ctx, store.Running, memberPrefix(id, port))
}

// removeSubtree deletes every stored leaf below prefix from ds.
func (m *Manager) removeSubtree(ctx context.Context, ds store.Datastore, prefix string) error {
	leaves, err := m.router.List(ctx, ds, prefix)
	if err != nil {
		return err
	}
	for _, leaf := range leaves {
		if err := m.router.Delete(ctx, ds, leaf.Path); err != nil {
			return err
		}
	}
	return nil
}
