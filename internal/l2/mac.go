package l2

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"

	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/store"
)

// ErrPortUnknown reports an interface name that is not a bridge port.
var ErrPortUnknown = errors.New("bridge port not found")

// Learn records or refreshes a dynamic forwarding-database entry for mac on the
// given VLAN and ingress port. The table is VLAN-aware: the ingress port must
// be a member of the VLAN, so a frame on a port that does not carry the VLAN is
// rejected with ErrMemberNotFound.
//
// An entry that already exists is refreshed: its age restarts and it follows
// the station when it moves to another port. A permanent (static) entry is
// never overwritten by learning.
func (m *Manager) Learn(ctx context.Context, mac string, vlanID uint16, port string) (model.MACEntry, error) {
	address, err := normalizeMAC(mac)
	if err != nil {
		return model.MACEntry{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return model.MACEntry{}, fmt.Errorf("l2: learn %s: %w", mac, err)
	}
	index, err := bridgePortOf(device, port)
	if err != nil {
		return model.MACEntry{}, err
	}
	if !portAccepts(device, port, vlanID) {
		return model.MACEntry{}, fmt.Errorf("%w: vlan %d port %s", ErrMemberNotFound, vlanID, port)
	}

	existing, found := findMAC(device.MACTable.Entries, address)
	if found && existing.Type == model.MACEntryTypeStatic {
		return existing, nil
	}
	if !found && uint32(len(device.MACTable.Entries)) >= device.MACTable.MaxEntries {
		return model.MACEntry{}, fmt.Errorf("%w: %d entries", ErrTableFull, device.MACTable.MaxEntries)
	}

	entry := model.MACEntry{
		MAC:       address,
		VLAN:      vlanID,
		Port:      index,
		Type:      model.MACEntryTypeDynamic,
		Permanent: found && existing.Permanent,
	}
	if err := m.writeMAC(ctx, entry); err != nil {
		return model.MACEntry{}, err
	}
	return entry, nil
}

// SetStatic adds or replaces a permanent forwarding-database entry. Static
// entries do not age out.
func (m *Manager) SetStatic(ctx context.Context, entry model.MACEntry) (model.MACEntry, error) {
	address, err := normalizeMAC(entry.MAC)
	if err != nil {
		return model.MACEntry{}, err
	}
	entry.MAC = address
	entry.Type = model.MACEntryTypeStatic
	entry.Permanent = true
	entry.Age = 0
	if err := entry.Validate(); err != nil {
		return model.MACEntry{}, fmt.Errorf("l2: static mac entry: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return model.MACEntry{}, fmt.Errorf("l2: static mac entry: %w", err)
	}
	if _, found := findMAC(device.MACTable.Entries, address); !found &&
		uint32(len(device.MACTable.Entries)) >= device.MACTable.MaxEntries {
		return model.MACEntry{}, fmt.Errorf("%w: %d entries", ErrTableFull, device.MACTable.MaxEntries)
	}
	if err := m.writeMAC(ctx, entry); err != nil {
		return model.MACEntry{}, err
	}
	return entry, nil
}

// MACEntries returns the forwarding database ordered by VLAN and then by MAC
// address.
func (m *Manager) MACEntries(ctx context.Context) ([]model.MACEntry, error) {
	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return nil, fmt.Errorf("l2: mac table: %w", err)
	}
	entries := slices.Clone(device.MACTable.Entries)
	slices.SortFunc(entries, func(a, b model.MACEntry) int {
		if a.VLAN != b.VLAN {
			return int(a.VLAN) - int(b.VLAN)
		}
		return compareMAC(a.MAC, b.MAC)
	})
	return entries, nil
}

// MACEntry returns one forwarding-database entry, or ErrMACNotFound.
func (m *Manager) MACEntry(ctx context.Context, mac string) (model.MACEntry, error) {
	address, err := normalizeMAC(mac)
	if err != nil {
		return model.MACEntry{}, err
	}
	entries, err := m.MACEntries(ctx)
	if err != nil {
		return model.MACEntry{}, err
	}
	entry, ok := findMAC(entries, address)
	if !ok {
		return model.MACEntry{}, fmt.Errorf("%w: %s", ErrMACNotFound, address)
	}
	return entry, nil
}

// DeleteMAC removes one forwarding-database entry.
func (m *Manager) DeleteMAC(ctx context.Context, mac string) error {
	address, err := normalizeMAC(mac)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return fmt.Errorf("l2: delete mac %s: %w", address, err)
	}
	if _, found := findMAC(device.MACTable.Entries, address); !found {
		return fmt.Errorf("%w: %s", ErrMACNotFound, address)
	}
	if err := m.removeMAC(ctx, address); err != nil {
		return err
	}
	return m.syncCount(ctx)
}

// FlushMAC removes dynamic entries, all of them or only those of one VLAN when
// vlanID is non-zero, and reports how many entries were removed. Permanent
// entries survive.
func (m *Manager) FlushMAC(ctx context.Context, vlanID uint16) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := m.MACEntries(ctx)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if entry.Type != model.MACEntryTypeDynamic {
			continue
		}
		if vlanID != 0 && entry.VLAN != vlanID {
			continue
		}
		if err := m.removeMAC(ctx, entry.MAC); err != nil {
			return removed, err
		}
		removed++
	}
	if removed > 0 {
		return removed, m.syncCount(ctx)
	}
	return 0, nil
}

// BridgePort returns the 1-based bridge port number of an interface, which is
// the interface's position in the interface list and its SNMP ifIndex.
func (m *Manager) BridgePort(ctx context.Context, name string) (uint32, error) {
	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return 0, fmt.Errorf("l2: bridge port: %w", err)
	}
	return bridgePortOf(device, name)
}

// PortName returns the interface name behind a bridge port number.
func (m *Manager) PortName(ctx context.Context, number uint32) (string, error) {
	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return "", fmt.Errorf("l2: bridge port: %w", err)
	}
	if number == 0 || number > uint32(len(device.Interfaces)) {
		return "", fmt.Errorf("%w: %d", ErrPortUnknown, number)
	}
	return device.Interfaces[number-1].Name, nil
}

// age is one MAC-aging sweep. Dynamic entries advance by the time elapsed since
// the previous sweep and are removed once they reach the configured aging time;
// permanent entries are left alone. The elapsed time comes from the injected
// clock, so the sweep is deterministic under FakeClock.
func (m *Manager) age(ctx context.Context) error {
	now := m.clock.Now()
	elapsed := uint32(0)
	if !m.lastAging.IsZero() && now.After(m.lastAging) {
		elapsed = uint32(now.Sub(m.lastAging) / time.Second)
	}
	m.lastAging = now
	if elapsed == 0 {
		return nil
	}

	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return fmt.Errorf("l2: mac aging: %w", err)
	}
	removed := 0
	for _, entry := range device.MACTable.Entries {
		if entry.Type != model.MACEntryTypeDynamic {
			continue
		}
		age := entry.Age + elapsed
		if age >= device.MACTable.AgingTime {
			if err := m.removeMAC(ctx, entry.MAC); err != nil {
				return err
			}
			removed++
			continue
		}
		if err := m.state(ctx, macPrefix(entry.MAC)+"/age", age); err != nil {
			return err
		}
	}
	if removed > 0 {
		return m.syncCount(ctx)
	}
	return nil
}

// writeMAC stores every leaf of one forwarding-database entry. The age is
// read-only state, the rest is configuration.
func (m *Manager) writeMAC(ctx context.Context, entry model.MACEntry) error {
	fields := []struct {
		name  string
		value any
	}{
		{"vlan-id", entry.VLAN},
		{"port", entry.Port},
		{"type", entry.Type},
		{"permanent", entry.Permanent},
	}
	for _, field := range fields {
		path := macPrefix(entry.MAC) + "/" + field.name
		if _, err := m.router.Set(ctx, store.Running, path, field.value); err != nil {
			return fmt.Errorf("l2: mac %s: set %s: %w", entry.MAC, field.name, err)
		}
	}
	if err := m.state(ctx, macPrefix(entry.MAC)+"/age", entry.Age); err != nil {
		return fmt.Errorf("l2: mac %s: set age: %w", entry.MAC, err)
	}
	if err := m.syncCount(ctx); err != nil {
		return err
	}
	return nil
}

// removeMAC deletes every leaf of one entry without checking that it exists.
func (m *Manager) removeMAC(ctx context.Context, mac string) error {
	return m.removeSubtree(ctx, store.Running, macPrefix(mac))
}

// syncCount republishes current-count from the entries the datastore holds.
func (m *Manager) syncCount(ctx context.Context) error {
	entries, err := m.MACEntries(ctx)
	if err != nil {
		return err
	}
	count := uint32(len(entries))
	if err := m.state(ctx, "mac-table/current-count", count); err != nil {
		return fmt.Errorf("l2: mac count: %w", err)
	}
	return nil
}

// macPrefix is the router path of one forwarding-database entry.
func macPrefix(mac string) string {
	return "mac-table/entry[mac-address=" + mac + "]"
}

// bridgePortOf maps an interface name to its 1-based bridge port number.
func bridgePortOf(device *model.Device, name string) (uint32, error) {
	for i, iface := range device.Interfaces {
		if iface.Name == name {
			return uint32(i + 1), nil
		}
	}
	return 0, fmt.Errorf("%w: %s", ErrPortUnknown, name)
}

// portAccepts reports whether a device port is a member of a VLAN.
func portAccepts(device *model.Device, port string, vlanID uint16) bool {
	vlan, ok := findVLAN(device.VLANs, vlanID)
	if !ok {
		return false
	}
	_, ok = findMember(vlan.Ports, port)
	return ok
}

// findMAC returns the entry with the given address from a forwarding database.
func findMAC(entries []model.MACEntry, mac string) (model.MACEntry, bool) {
	for _, entry := range entries {
		if equalMAC(entry.MAC, mac) {
			return entry, true
		}
	}
	return model.MACEntry{}, false
}

// normalizeMAC parses a MAC address and returns its canonical lowercase
// colon-separated form, which is what the datastore keys entries by.
func normalizeMAC(text string) (string, error) {
	address, err := net.ParseMAC(text)
	if err != nil || len(address) != 6 {
		return "", fmt.Errorf("%w: %q is not a 48-bit MAC address", ErrInvalidMAC, text)
	}
	return address.String(), nil
}

// equalMAC compares two MAC addresses case-insensitively.
func equalMAC(a, b string) bool {
	left, err := normalizeMAC(a)
	if err != nil {
		return a == b
	}
	right, err := normalizeMAC(b)
	if err != nil {
		return a == b
	}
	return left == right
}

// compareMAC orders two MAC addresses by their bytes.
func compareMAC(a, b string) int {
	left, err := normalizeMAC(a)
	if err != nil {
		return 0
	}
	right, err := normalizeMAC(b)
	if err != nil {
		return 0
	}
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
