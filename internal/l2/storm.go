package l2

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Storm limits of the injector.
const (
	// DefaultStormThresholdPPS is the broadcast rate above which the injector
	// raises an alarm and starts discarding the excess.
	DefaultStormThresholdPPS uint32 = 1000
	// DefaultStormWindow is the sliding window the rate is measured over.
	DefaultStormWindow = time.Second
	// MaxStormPackets bounds one injection, so a single request cannot move the
	// counters by an absurd amount.
	MaxStormPackets uint32 = 1_000_000
)

// ErrStormTooLarge reports an injection above MaxStormPackets.
var ErrStormTooLarge = errors.New("storm injection is too large")

// stormSample is one measurement of the injector's sliding window.
type stormSample struct {
	at      time.Time
	packets uint32
}

// Storm injects a broadcast storm of packets frames on one port: the ingress
// counters grow, the frames are flooded to the other member ports of the port's
// VLAN, and a rate above the configured threshold raises an alarm on the event
// bus and counts the excess as input discards, the way storm control drops
// frames. The alarm clears on the first injection that measures a rate back
// below the threshold.
func (m *Manager) Storm(ctx context.Context, port string, packets uint32) error {
	if packets == 0 {
		return nil
	}
	if packets > MaxStormPackets {
		return fmt.Errorf("%w: %d packets, limit %d", ErrStormTooLarge, packets, MaxStormPackets)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	device, err := m.router.Snapshot(ctx, store.Running)
	if err != nil {
		return fmt.Errorf("l2: storm: %w", err)
	}
	if _, ok := findInterface(device, port); !ok {
		return fmt.Errorf("%w: %s", ErrPortUnknown, port)
	}

	now := m.clock.Now()
	rate := m.recordStorm(port, now, packets)
	storming := rate > m.stormThreshold

	// Storm control: the frames above the limit are discarded.
	accepted := packets
	var discarded uint32
	if storming && rate-packets < m.stormThreshold {
		// The injection crossed the limit: keep what fits and drop the rest.
		accepted = m.stormThreshold - (rate - packets)
		discarded = packets - accepted
	} else if storming {
		accepted, discarded = 0, packets
	}

	delta := model.InterfaceCounters{
		InOctets:    uint64(accepted) * MinFrameBytes,
		InUcastPkts: uint64(accepted),
		InDiscards:  discarded,
	}
	if err := m.increment(ctx, port, delta); err != nil {
		return err
	}

	vlan, ok := stormVLAN(device, port)
	if ok {
		for _, member := range vlan.Ports {
			if member.Port == port {
				continue
			}
			egress := model.InterfaceCounters{
				OutOctets:    uint64(accepted) * MinFrameBytes,
				OutUcastPkts: uint64(accepted),
			}
			if err := m.increment(ctx, member.Port, egress); err != nil {
				return err
			}
		}
	}

	return m.reportStorm(port, rate, storming)
}

// Storming reports whether the measured broadcast rate of a port is currently
// above the threshold.
func (m *Manager) Storming(port string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.stormAlarm[port]
}

// recordStorm appends an injection to the port's window, drops the samples that
// fell out of it and returns the packets per second it currently holds.
func (m *Manager) recordStorm(port string, now time.Time, packets uint32) uint32 {
	cutoff := now.Add(-m.stormWindow)
	window := make([]stormSample, 0, len(m.stormSamples[port])+1)
	rate := uint32(0)
	for _, sample := range m.stormSamples[port] {
		if sample.at.Before(cutoff) {
			continue
		}
		window = append(window, sample)
		rate += sample.packets
	}
	window = append(window, stormSample{at: now, packets: packets})
	rate += packets

	if m.stormSamples == nil {
		m.stormSamples = make(map[string][]stormSample)
	}
	m.stormSamples[port] = window
	return rate
}

// reportStorm publishes the alarm transitions of one injection.
func (m *Manager) reportStorm(port string, rate uint32, storming bool) error {
	if m.stormAlarm == nil {
		m.stormAlarm = make(map[string]bool)
	}
	previous := m.stormAlarm[port]
	if previous == storming {
		return nil
	}
	m.stormAlarm[port] = storming

	if storming {
		m.publish(event.Event{
			Type:     event.TypeAlarmRaised,
			Resource: "l2/storm/" + port,
			Severity: "major",
			Domain:   event.DomainL2,
			Alarm:    event.AlarmL2Storm,
			Message:  fmt.Sprintf("broadcast storm on %s: %d pps above the %d pps limit", port, rate, m.stormThreshold),
		})
		return nil
	}
	m.publish(event.Event{
		Type:     event.TypeAlarmCleared,
		Resource: "l2/storm/" + port,
		Severity: "cleared",
		Domain:   event.DomainL2,
		Alarm:    event.AlarmL2Storm,
		Message:  fmt.Sprintf("broadcast storm on %s cleared: %d pps within the %d pps limit", port, rate, m.stormThreshold),
	})
	return nil
}

// stormVLAN returns the VLAN a broadcast storm on port belongs to: the port's
// lowest-numbered member VLAN, which is its access VLAN.
func stormVLAN(device *model.Device, port string) (model.VLAN, bool) {
	vlans := make([]model.VLAN, 0, len(device.VLANs))
	for _, vlan := range device.VLANs {
		if _, ok := findMember(vlan.Ports, port); ok {
			vlans = append(vlans, vlan)
		}
	}
	if len(vlans) == 0 {
		return model.VLAN{}, false
	}
	slices.SortFunc(vlans, func(a, b model.VLAN) int { return int(a.ID) - int(b.ID) })
	return vlans[0], true
}
