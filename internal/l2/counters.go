package l2

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Frame is one simulated Ethernet frame offered to the bridge by Transmit.
type Frame struct {
	// Src and Dst are the source and destination MAC addresses.
	Src string
	// Dst is the destination MAC address.
	Dst string
	// VLAN is the VLAN the frame belongs to.
	VLAN uint16
	// Ingress is the local port the frame arrived on.
	Ingress string
	// Bytes is the frame length in octets. Zero means MinFrameBytes.
	Bytes uint64
	// Broadcast forces flooding even when the destination is known, which is
	// how a broadcast or multicast frame behaves.
	Broadcast bool
}

// MinFrameBytes is the length assumed for a frame that does not name one: the
// 64-byte minimum Ethernet frame.
const MinFrameBytes uint64 = 64

// FrameResult reports what the bridge did with one frame.
type FrameResult struct {
	// Ingress is the port the frame arrived on.
	Ingress string
	// Egress lists the ports the frame was forwarded to, in port order.
	Egress []string
	// Learned reports whether the source address was added to the forwarding
	// database.
	Learned bool
	// Dropped names the reason the frame was not forwarded, and is empty when
	// it was.
	Dropped string
}

// Transmit pushes one frame through the bridge: the ingress counters grow, the
// source address is learned on the ingress port, and the frame is forwarded to
// the port the destination is known on or flooded to every other member port of
// the VLAN. A frame on a port that does not carry its VLAN is discarded, and an
// oversized frame is counted as an input error.
func (m *Manager) Transmit(ctx context.Context, frame Frame) (FrameResult, error) {
	source, err := normalizeMAC(frame.Src)
	if err != nil {
		return FrameResult{}, err
	}
	destination, err := normalizeMAC(frame.Dst)
	if err != nil {
		return FrameResult{}, err
	}
	if frame.Bytes == 0 {
		frame.Bytes = MinFrameBytes
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return FrameResult{}, fmt.Errorf("l2: transmit: %w", err)
	}
	iface, ok := findInterface(device, frame.Ingress)
	if !ok {
		return FrameResult{}, fmt.Errorf("%w: %s", ErrPortUnknown, frame.Ingress)
	}
	ingressIndex, err := bridgePortOf(device, frame.Ingress)
	if err != nil {
		return FrameResult{}, err
	}
	result := FrameResult{Ingress: frame.Ingress}

	if !portAccepts(device, frame.Ingress, frame.VLAN) {
		result.Dropped = fmt.Sprintf("vlan %d is not on port %s", frame.VLAN, frame.Ingress)
		return result, m.increment(ctx, frame.Ingress, model.InterfaceCounters{InDiscards: 1})
	}
	if frame.Bytes > uint64(iface.MTU) {
		result.Dropped = fmt.Sprintf("frame of %d bytes exceeds the %d byte MTU", frame.Bytes, iface.MTU)
		return result, m.increment(ctx, frame.Ingress, model.InterfaceCounters{InErrors: 1})
	}

	delta := model.InterfaceCounters{InOctets: frame.Bytes, InUcastPkts: 1}
	if err := m.increment(ctx, frame.Ingress, delta); err != nil {
		return result, err
	}

	if _, err := m.learn(ctx, source, frame.VLAN, frame.Ingress); err == nil {
		result.Learned = true
	} else if !errors.Is(err, ErrTableFull) {
		return result, err
	}

	result.Egress = m.egressPorts(device, frame, destination, ingressIndex)
	for _, port := range result.Egress {
		delta := model.InterfaceCounters{OutOctets: frame.Bytes, OutUcastPkts: 1}
		if err := m.increment(ctx, port, delta); err != nil {
			return result, err
		}
	}
	return result, nil
}

// egressPorts decides where a frame goes: the known destination port, or every
// other member port of the VLAN for a broadcast or an unknown destination. A
// destination known on the ingress port itself is filtered.
func (m *Manager) egressPorts(device *model.Device, frame Frame, destination string, ingressIndex uint32) []string {
	if !frame.Broadcast {
		if entry, found := findMAC(device.MACTable.Entries, destination); found {
			if entry.Port == ingressIndex {
				return nil
			}
			if name, err := portNameOf(device, entry.Port); err == nil {
				return []string{name}
			}
		}
	}

	vlan, ok := findVLAN(device.VLANs, frame.VLAN)
	if !ok {
		return nil
	}
	ports := make([]string, 0, len(vlan.Ports))
	for _, member := range vlan.Ports {
		if member.Port == frame.Ingress {
			continue
		}
		ports = append(ports, member.Port)
	}
	slices.Sort(ports)
	return ports
}

// Counters returns the interface counters of one port.
func (m *Manager) Counters(ctx context.Context, port string) (model.InterfaceCounters, error) {
	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return model.InterfaceCounters{}, fmt.Errorf("l2: counters: %w", err)
	}
	iface, ok := findInterface(device, port)
	if !ok {
		return model.InterfaceCounters{}, fmt.Errorf("%w: %s", ErrPortUnknown, port)
	}
	return iface.Counters, nil
}

// Increment adds delta to the counters of one interface. It is what the traffic
// simulation and the storm injector use to move counters.
func (m *Manager) Increment(ctx context.Context, port string, delta model.InterfaceCounters) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.increment(ctx, port, delta)
}

// increment adds delta to the stored counters of one port with the manager lock
// already held.
func (m *Manager) increment(ctx context.Context, port string, delta model.InterfaceCounters) error {
	current, err := m.Counters(ctx, port)
	if err != nil {
		return err
	}
	sum := addCounters(current, delta)

	fields := []struct {
		name  string
		value any
	}{
		{"in-octets", sum.InOctets},
		{"out-octets", sum.OutOctets},
		{"in-ucast-pkts", sum.InUcastPkts},
		{"out-ucast-pkts", sum.OutUcastPkts},
		{"in-errors", sum.InErrors},
		{"out-errors", sum.OutErrors},
		{"in-discards", sum.InDiscards},
		{"out-discards", sum.OutDiscards},
	}
	for _, field := range fields {
		path := "interfaces/interface[name=" + port + "]/counters/" + field.name
		if _, err := m.router.SetState(ctx, path, field.value); err != nil {
			return fmt.Errorf("l2: counters %s: set %s: %w", port, field.name, err)
		}
	}
	return nil
}

// addCounters returns the component-wise sum of two counter sets.
func addCounters(a, b model.InterfaceCounters) model.InterfaceCounters {
	return model.InterfaceCounters{
		InOctets:     a.InOctets + b.InOctets,
		OutOctets:    a.OutOctets + b.OutOctets,
		InUcastPkts:  a.InUcastPkts + b.InUcastPkts,
		OutUcastPkts: a.OutUcastPkts + b.OutUcastPkts,
		InErrors:     a.InErrors + b.InErrors,
		OutErrors:    a.OutErrors + b.OutErrors,
		InDiscards:   a.InDiscards + b.InDiscards,
		OutDiscards:  a.OutDiscards + b.OutDiscards,
	}
}

// findInterface returns one interface of a device snapshot.
func findInterface(device *model.Device, name string) (model.Interface, bool) {
	for _, iface := range device.Interfaces {
		if iface.Name == name {
			return iface, true
		}
	}
	return model.Interface{}, false
}

// portNameOf maps a bridge port number back to its interface name.
func portNameOf(device *model.Device, number uint32) (string, error) {
	if number == 0 || number > uint32(len(device.Interfaces)) {
		return "", fmt.Errorf("%w: %d", ErrPortUnknown, number)
	}
	return device.Interfaces[number-1].Name, nil
}
