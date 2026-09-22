package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Interface type values.
const (
	InterfaceTypeRadio    = "radio"
	InterfaceTypeEthernet = "ethernet"
)

// Interface MTU bounds.
const (
	InterfaceMTUMin uint32 = 576
	InterfaceMTUMax uint32 = 9216
)

// VLAN identifier bounds (IEEE 802.1Q: 1-4094).
const (
	VLANIDMin uint16 = 1
	VLANIDMax uint16 = 4094
)

// VLAN port modes (IEEE 802.1Q).
const (
	VLANPortModeAccess = "access"
	VLANPortModeTrunk  = "trunk"
	VLANPortModeHybrid = "hybrid"
)

// MAC-table entry types.
const (
	MACEntryTypeDynamic = "dynamic"
	MACEntryTypeStatic  = "static"
)

// MAC-table bounds. The aging time follows the IEEE 802.1D suggestion of
// 10 s to 10^6 s; max-entries and current-count are bounded by the same limit.
const (
	MACAgingTimeMin       uint32 = 10
	MACAgingTimeMax       uint32 = 1000000
	MACTableMaxEntriesMin uint32 = 1
	MACTableMaxEntriesMax uint32 = 1000000
)

// C-TAG handling values of a QinQ port (IEEE 802.1ad).
const (
	CTagHandlingPush        = "push"
	CTagHandlingPop         = "pop"
	CTagHandlingTransparent = "transparent"
)

// LLDP bounds (IEEE 802.1AB defaults: tx-interval 30 s, hold multiplier 4).
const (
	LLDPTxIntervalMin  uint32 = 1
	LLDPTxIntervalMax  uint32 = 3600
	LLDPTxHoldMin      uint32 = 2
	LLDPTxHoldMax      uint32 = 10
	LLDPReinitDelayMin uint32 = 1
	LLDPReinitDelayMax uint32 = 10
	LLDPTxDelayMin     uint32 = 1
	LLDPTxDelayMax     uint32 = 10
)

// STP/RSTP protocol values.
const (
	STPProtocolSTP  = "stp"
	STPProtocolRSTP = "rstp"
)

// RSTP port roles.
const (
	STPPortRoleRoot       = "root"
	STPPortRoleDesignated = "designated"
	STPPortRoleAlternate  = "alternate"
	STPPortRoleBackup     = "backup"
)

// STP port states. Classic STP (IEEE 802.1D) walks a port through disabled,
// blocking, listening, learning and forwarding; RSTP (IEEE 802.1D-2004)
// collapses the first three into discarding.
const (
	STPPortStateDisabled   = "disabled"
	STPPortStateBlocking   = "blocking"
	STPPortStateListening  = "listening"
	STPPortStateDiscarding = "discarding"
	STPPortStateLearning   = "learning"
	STPPortStateForwarding = "forwarding"
)

// STPPortStates returns the port-state vocabulary of a protocol, which is what
// STPState.Validate accepts for its ports.
func STPPortStates(protocol string) []string {
	if protocol == STPProtocolRSTP {
		return []string{STPPortStateDiscarding, STPPortStateLearning, STPPortStateForwarding}
	}
	return []string{
		STPPortStateDisabled,
		STPPortStateBlocking,
		STPPortStateListening,
		STPPortStateLearning,
		STPPortStateForwarding,
	}
}

// STP priority bounds and steps (both are multiples of 4096/16).
const (
	STPBridgePriorityMax  uint16 = 61440
	STPBridgePriorityStep uint16 = 4096
	STPPortPriorityMax    uint8  = 240
	STPPortPriorityStep   uint8  = 16
)

// LLDPTTLMax is the largest valid LLDP time-to-live in seconds.
const LLDPTTLMax uint32 = 65535

// Interface is one managed interface of the device. Type selects the domain it
// belongs to: a radio interface carries a RadioLink, an ethernet interface does
// not. The counters are read-only.
type Interface struct {
	Name       string            `path:"name" key:"true" xml:"name" json:"name"`
	Type       string            `path:"type" xml:"type" json:"type"`
	Enabled    bool              `path:"enabled" xml:"enabled" json:"enabled"`
	MTU        uint32            `path:"mtu" xml:"mtu" json:"mtu"`
	MACAddress string            `path:"mac-address" xml:"mac-address,omitempty" json:"mac-address,omitempty"`
	RadioLink  *RadioLink        `path:"radio-link" xml:"radio-link,omitempty" json:"radio-link,omitempty"`
	Counters   InterfaceCounters `path:"counters" xml:"counters" json:"counters"`
}

// InterfaceCounters is the IF-MIB-style counter set of one interface. Every
// value is read-only.
type InterfaceCounters struct {
	InOctets     uint64 `path:"in-octets" xml:"in-octets" json:"in-octets" config:"false"`
	OutOctets    uint64 `path:"out-octets" xml:"out-octets" json:"out-octets" config:"false"`
	InUcastPkts  uint64 `path:"in-ucast-pkts" xml:"in-ucast-pkts" json:"in-ucast-pkts" config:"false"`
	OutUcastPkts uint64 `path:"out-ucast-pkts" xml:"out-ucast-pkts" json:"out-ucast-pkts" config:"false"`
	InErrors     uint32 `path:"in-errors" xml:"in-errors" json:"in-errors" config:"false"`
	OutErrors    uint32 `path:"out-errors" xml:"out-errors" json:"out-errors" config:"false"`
	InDiscards   uint32 `path:"in-discards" xml:"in-discards" json:"in-discards" config:"false"`
	OutDiscards  uint32 `path:"out-discards" xml:"out-discards" json:"out-discards" config:"false"`
}

// VLAN is one 802.1Q VLAN and its member ports.
type VLAN struct {
	ID          uint16     `path:"id" key:"true" xml:"id" json:"id"`
	Name        string     `path:"name" xml:"name" json:"name"`
	Description string     `path:"description" xml:"description,omitempty" json:"description,omitempty"`
	Ports       []VLANPort `path:"ports/port" creatable:"true" xml:"ports>port" json:"ports"`
}

// VLANPort is one member port of a VLAN. QinQ (IEEE 802.1ad) pushes an outer
// S-TAG with OuterVID on top of the customer C-TAG; the C-TAG is the port's
// PVID for an access port.
type VLANPort struct {
	Port         string `path:"port" key:"true" xml:"port" json:"port"`
	Mode         string `path:"mode" xml:"mode" json:"mode"`
	PVID         uint16 `path:"pvid" xml:"pvid" json:"pvid"`
	Tagged       bool   `path:"tagged" xml:"tagged" json:"tagged"`
	QinQ         bool   `path:"qinq" xml:"qinq" json:"qinq"`
	OuterVID     uint16 `path:"s-tag-vid" xml:"s-tag-vid,omitempty" json:"s-tag-vid,omitempty"`
	CTagHandling string `path:"c-tag-handling" xml:"c-tag-handling,omitempty" json:"c-tag-handling,omitempty"`
}

// MACTable is the bridge forwarding database: the learned and static entries
// plus the table parameters. Entries and current-count are read-only state;
// aging-time and max-entries are configuration.
type MACTable struct {
	Entries      []MACEntry `path:"entry" creatable:"true" xml:"entry" json:"entry"`
	AgingTime    uint32     `path:"aging-time" xml:"aging-time" json:"aging-time"`
	MaxEntries   uint32     `path:"max-entries" xml:"max-entries" json:"max-entries"`
	CurrentCount uint32     `path:"current-count" xml:"current-count" json:"current-count" config:"false"`
}

// LLDPConfig is the local LLDP configuration and the table of remote
// neighbours. The neighbour table is a simplified management-plane model:
// neighbours are configured or injected, never learned from the wire.
type LLDPConfig struct {
	Enabled           bool           `path:"enabled" xml:"enabled" json:"enabled"`
	TxInterval        uint32         `path:"tx-interval" xml:"tx-interval" json:"tx-interval"`
	TxHoldMultiplier  uint32         `path:"tx-hold-multiplier" xml:"tx-hold-multiplier" json:"tx-hold-multiplier"`
	ReinitDelay       uint32         `path:"reinit-delay" xml:"reinit-delay" json:"reinit-delay"`
	TxDelay           uint32         `path:"tx-delay" xml:"tx-delay" json:"tx-delay"`
	ChassisID         string         `path:"chassis-id" xml:"chassis-id" json:"chassis-id"`
	SystemName        string         `path:"system-name" xml:"system-name" json:"system-name"`
	SystemDescription string         `path:"system-description" xml:"system-description,omitempty" json:"system-description,omitempty"`
	Neighbors         []LLDPNeighbor `path:"neighbors/neighbor" creatable:"true" xml:"neighbors>neighbor" json:"neighbors"`
}

// MACEntry is one entry of the MAC forwarding database.
type MACEntry struct {
	MAC       string `path:"mac-address" key:"true" xml:"mac-address" json:"mac-address"`
	VLAN      uint16 `path:"vlan-id" xml:"vlan-id" json:"vlan-id"`
	Port      uint32 `path:"port" xml:"port" json:"port"`
	Type      string `path:"type" xml:"type" json:"type"`
	Age       uint32 `path:"age" xml:"age,omitempty" json:"age,omitempty" config:"false"`
	Permanent bool   `path:"permanent" xml:"permanent" json:"permanent"`
}

// STPState is the simplified STP/RSTP state of the bridge.
type STPState struct {
	Enabled        bool      `path:"enabled" xml:"enabled" json:"enabled"`
	Protocol       string    `path:"protocol" xml:"protocol" json:"protocol"`
	BridgePriority uint16    `path:"bridge-priority" xml:"bridge-priority" json:"bridge-priority"`
	BridgeAddress  string    `path:"bridge-address" xml:"bridge-address,omitempty" json:"bridge-address,omitempty"`
	RootID         string    `path:"root-id" xml:"root-id,omitempty" json:"root-id,omitempty" config:"false"`
	RootCost       uint32    `path:"root-cost" xml:"root-cost" json:"root-cost" config:"false"`
	Ports          []STPPort `path:"ports/port" xml:"ports>port" json:"ports"`
}

// STPPort is one bridge port of the STP/RSTP state.
type STPPort struct {
	Port     string `path:"port" key:"true" xml:"port" json:"port"`
	Role     string `path:"role" xml:"role" json:"role"`
	State    string `path:"state" xml:"state" json:"state"`
	Priority uint8  `path:"priority" xml:"priority" json:"priority"`
	PathCost uint32 `path:"path-cost" xml:"path-cost" json:"path-cost"`
	EdgePort bool   `path:"edge-port" xml:"edge-port" json:"edge-port"`
}

// LLDPNeighbor is one remote LLDP neighbour of an interface.
type LLDPNeighbor struct {
	Port              string `path:"port" key:"true" xml:"port" json:"port"`
	ChassisID         string `path:"chassis-id" xml:"chassis-id" json:"chassis-id"`
	PortID            string `path:"port-id" xml:"port-id" json:"port-id"`
	SystemName        string `path:"system-name" xml:"system-name,omitempty" json:"system-name,omitempty"`
	SystemDescription string `path:"system-description" xml:"system-description,omitempty" json:"system-description,omitempty"`
	TTL               uint32 `path:"ttl" xml:"ttl" json:"ttl"`
	Capabilities      string `path:"capabilities" xml:"capabilities,omitempty" json:"capabilities,omitempty"`
}

// Validate checks the interface type, MTU, optional MAC address, the radio
// link when present and the counters.
func (i Interface) Validate() error {
	if i.Name == "" {
		return errors.New("interface: name must not be empty")
	}
	switch i.Type {
	case InterfaceTypeRadio, InterfaceTypeEthernet:
	default:
		return fmt.Errorf("interface %s: unknown type %q (want %s or %s)",
			i.Name, i.Type, InterfaceTypeRadio, InterfaceTypeEthernet)
	}
	if i.MTU < InterfaceMTUMin || i.MTU > InterfaceMTUMax {
		return fmt.Errorf("interface %s: mtu %d out of range %d..%d", i.Name, i.MTU, InterfaceMTUMin, InterfaceMTUMax)
	}
	if i.MACAddress != "" && !validMAC(i.MACAddress) {
		return fmt.Errorf("interface %s: malformed mac-address %q", i.Name, i.MACAddress)
	}
	if i.RadioLink != nil {
		if err := i.RadioLink.Validate(); err != nil {
			return fmt.Errorf("interface %s: radio-link: %w", i.Name, err)
		}
	}
	if err := i.Counters.Validate(); err != nil {
		return fmt.Errorf("interface %s: counters: %w", i.Name, err)
	}
	return nil
}

// Validate always succeeds: every counter value is valid and read-only.
func (InterfaceCounters) Validate() error { return nil }

// Validate checks the VLAN id, name and every member port.
func (v VLAN) Validate() error {
	if v.ID < VLANIDMin || v.ID > VLANIDMax {
		return fmt.Errorf("vlan: id %d out of range %d..%d", v.ID, VLANIDMin, VLANIDMax)
	}
	if v.Name == "" {
		return fmt.Errorf("vlan %d: name must not be empty", v.ID)
	}
	for i, port := range v.Ports {
		if err := port.Validate(); err != nil {
			return fmt.Errorf("vlan %d: ports[%d]: %w", v.ID, i, err)
		}
	}
	return nil
}

// Validate checks the port name, mode, PVID and tagging consistency.
func (p VLANPort) Validate() error {
	if p.Port == "" {
		return errors.New("port must not be empty")
	}
	switch p.Mode {
	case VLANPortModeAccess, VLANPortModeTrunk, VLANPortModeHybrid:
	default:
		return fmt.Errorf("port %s: unknown mode %q", p.Port, p.Mode)
	}
	if p.PVID < VLANIDMin || p.PVID > VLANIDMax {
		return fmt.Errorf("port %s: pvid %d out of range %d..%d", p.Port, p.PVID, VLANIDMin, VLANIDMax)
	}
	if p.Mode == VLANPortModeAccess && p.Tagged {
		return fmt.Errorf("port %s: access mode must not be tagged", p.Port)
	}
	if err := p.validateQinQ(); err != nil {
		return err
	}
	return nil
}

// validateQinQ checks the stacked-tag configuration of one port: a QinQ port
// pushes a valid outer tag that differs from the inner (PVID) tag and names a
// C-TAG handling mode; a plain port must leave both fields unset.
func (p VLANPort) validateQinQ() error {
	if !p.QinQ {
		if p.OuterVID != 0 {
			return fmt.Errorf("port %s: s-tag-vid %d requires qinq", p.Port, p.OuterVID)
		}
		if p.CTagHandling != "" {
			return fmt.Errorf("port %s: c-tag-handling %q requires qinq", p.Port, p.CTagHandling)
		}
		return nil
	}
	if p.OuterVID < VLANIDMin || p.OuterVID > VLANIDMax {
		return fmt.Errorf("port %s: s-tag-vid %d out of range %d..%d", p.Port, p.OuterVID, VLANIDMin, VLANIDMax)
	}
	if p.OuterVID == p.PVID {
		return fmt.Errorf("port %s: s-tag-vid %d must differ from the inner pvid", p.Port, p.OuterVID)
	}
	switch p.CTagHandling {
	case CTagHandlingPush, CTagHandlingPop, CTagHandlingTransparent:
	default:
		return fmt.Errorf("port %s: unknown c-tag-handling %q (want %s, %s or %s)",
			p.Port, p.CTagHandling, CTagHandlingPush, CTagHandlingPop, CTagHandlingTransparent)
	}
	return nil
}

// Validate checks the MAC address, VLAN, port, type and permanence flag.
func (m MACEntry) Validate() error {
	if !validMAC(m.MAC) {
		return fmt.Errorf("mac-address %q is not a 48-bit MAC address", m.MAC)
	}
	if m.VLAN > VLANIDMax {
		return fmt.Errorf("vlan-id %d out of range 0..%d", m.VLAN, VLANIDMax)
	}
	switch m.Type {
	case MACEntryTypeDynamic, MACEntryTypeStatic:
	default:
		return fmt.Errorf("unknown type %q (want %s or %s)", m.Type, MACEntryTypeDynamic, MACEntryTypeStatic)
	}
	if m.Port == 0 {
		return errors.New("port must be greater than 0")
	}
	if m.Permanent && m.Type != MACEntryTypeStatic {
		return fmt.Errorf("permanent entry must have type %s", MACEntryTypeStatic)
	}
	return nil
}

// Validate checks the table parameters and every entry.
func (m MACTable) Validate() error {
	if m.AgingTime < MACAgingTimeMin || m.AgingTime > MACAgingTimeMax {
		return fmt.Errorf("aging-time %d out of range %d..%d", m.AgingTime, MACAgingTimeMin, MACAgingTimeMax)
	}
	if m.MaxEntries < MACTableMaxEntriesMin || m.MaxEntries > MACTableMaxEntriesMax {
		return fmt.Errorf("max-entries %d out of range %d..%d",
			m.MaxEntries, MACTableMaxEntriesMin, MACTableMaxEntriesMax)
	}
	if m.CurrentCount > m.MaxEntries {
		return fmt.Errorf("current-count %d exceeds max-entries %d", m.CurrentCount, m.MaxEntries)
	}

	seen := make(map[string]struct{}, len(m.Entries))
	for i, entry := range m.Entries {
		if err := entry.Validate(); err != nil {
			return fmt.Errorf("entry[%d]: %w", i, err)
		}
		// The forwarding database is addressed by MAC address alone, so the
		// same MAC cannot appear twice even in different VLANs.
		if _, dup := seen[entry.MAC]; dup {
			return fmt.Errorf("entry[%d]: duplicate mac-address %s", i, entry.MAC)
		}
		seen[entry.MAC] = struct{}{}
	}
	return nil
}

// Validate checks the LLDP parameters and every remote neighbour.
func (c LLDPConfig) Validate() error {
	if c.TxInterval < LLDPTxIntervalMin || c.TxInterval > LLDPTxIntervalMax {
		return fmt.Errorf("tx-interval %d out of range %d..%d", c.TxInterval, LLDPTxIntervalMin, LLDPTxIntervalMax)
	}
	if c.TxHoldMultiplier < LLDPTxHoldMin || c.TxHoldMultiplier > LLDPTxHoldMax {
		return fmt.Errorf("tx-hold-multiplier %d out of range %d..%d",
			c.TxHoldMultiplier, LLDPTxHoldMin, LLDPTxHoldMax)
	}
	if c.ReinitDelay < LLDPReinitDelayMin || c.ReinitDelay > LLDPReinitDelayMax {
		return fmt.Errorf("reinit-delay %d out of range %d..%d", c.ReinitDelay, LLDPReinitDelayMin, LLDPReinitDelayMax)
	}
	if c.TxDelay < LLDPTxDelayMin || c.TxDelay > LLDPTxDelayMax {
		return fmt.Errorf("tx-delay %d out of range %d..%d", c.TxDelay, LLDPTxDelayMin, LLDPTxDelayMax)
	}
	if c.ChassisID == "" {
		return errors.New("chassis-id must not be empty")
	}

	seen := make(map[string]struct{}, len(c.Neighbors))
	for i, neighbor := range c.Neighbors {
		if err := neighbor.Validate(); err != nil {
			return fmt.Errorf("neighbors[%d]: %w", i, err)
		}
		if _, dup := seen[neighbor.Port]; dup {
			return fmt.Errorf("neighbors[%d]: duplicate neighbour on port %s", i, neighbor.Port)
		}
		seen[neighbor.Port] = struct{}{}
	}
	return nil
}

// Validate checks the protocol, bridge priority and addresses, plus every port
// when STP is enabled.
func (s STPState) Validate() error {
	switch s.Protocol {
	case STPProtocolSTP, STPProtocolRSTP:
	default:
		return fmt.Errorf("unknown protocol %q (want %s or %s)", s.Protocol, STPProtocolSTP, STPProtocolRSTP)
	}
	if s.BridgePriority > STPBridgePriorityMax || s.BridgePriority%STPBridgePriorityStep != 0 {
		return fmt.Errorf("bridge-priority %d out of range 0..%d in steps of %d",
			s.BridgePriority, STPBridgePriorityMax, STPBridgePriorityStep)
	}
	if s.BridgeAddress != "" && !validMAC(s.BridgeAddress) {
		return fmt.Errorf("malformed bridge-address %q", s.BridgeAddress)
	}
	if s.RootID != "" && !validMAC(s.RootID) {
		return fmt.Errorf("malformed root-id %q", s.RootID)
	}
	if s.Enabled {
		states := STPPortStates(s.Protocol)
		for i, port := range s.Ports {
			if err := port.Validate(); err != nil {
				return fmt.Errorf("ports[%d]: %w", i, err)
			}
			if !slices.Contains(states, port.State) {
				return fmt.Errorf("ports[%d]: state %q is not a %s state (want one of %s)",
					i, port.State, s.Protocol, strings.Join(states, ", "))
			}
		}
	}
	return nil
}

// Validate checks one STP port. Roles and states must be set when the bridge
// has STP enabled.
func (p STPPort) Validate() error {
	if p.Port == "" {
		return errors.New("port must not be empty")
	}
	switch p.Role {
	case STPPortRoleRoot, STPPortRoleDesignated, STPPortRoleAlternate, STPPortRoleBackup:
	default:
		return fmt.Errorf("port %s: unknown role %q", p.Port, p.Role)
	}
	switch p.State {
	case STPPortStateDisabled, STPPortStateBlocking, STPPortStateListening,
		STPPortStateDiscarding, STPPortStateLearning, STPPortStateForwarding:
	default:
		return fmt.Errorf("port %s: unknown state %q", p.Port, p.State)
	}
	if p.Priority > STPPortPriorityMax || p.Priority%STPPortPriorityStep != 0 {
		return fmt.Errorf("port %s: priority %d out of range 0..%d in steps of %d",
			p.Port, p.Priority, STPPortPriorityMax, STPPortPriorityStep)
	}
	return nil
}

// Validate checks the mandatory LLDP fields and the TTL.
func (n LLDPNeighbor) Validate() error {
	if n.Port == "" {
		return errors.New("port must not be empty")
	}
	if n.ChassisID == "" {
		return errors.New("chassis-id must not be empty")
	}
	if n.PortID == "" {
		return errors.New("port-id must not be empty")
	}
	if n.TTL == 0 || n.TTL > LLDPTTLMax {
		return fmt.Errorf("ttl %d out of range 1..%d", n.TTL, LLDPTTLMax)
	}
	return nil
}
