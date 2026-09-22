package model

import (
	"errors"
	"fmt"
)

// Device is the root managed object: the system information, the interface
// list, the L2 switching state and the synchronization state. Router paths
// start here, for example
// interfaces/interface[name=radio0]/radio-link/tx-power, vlans/vlan[id=100]/name
// or ptp/clock/state.
type Device struct {
	SystemInfo SystemInfo  `path:"system-info" xml:"system-info" json:"system-info"`
	Interfaces []Interface `path:"interfaces/interface" xml:"interfaces>interface" json:"interfaces"`
	VLANs      []VLAN      `path:"vlans/vlan" creatable:"true" xml:"vlans>vlan" json:"vlans"`
	MACTable   MACTable    `path:"mac-table" xml:"mac-table" json:"mac-table"`
	STP        STPState    `path:"stp/state" xml:"stp>state" json:"stp"`
	LLDP       LLDPConfig  `path:"lldp" xml:"lldp" json:"lldp"`
	PTP        PTPClock    `path:"ptp/clock" xml:"ptp>clock" json:"ptp"`
	SyncE      SyncEState  `path:"synce" xml:"synce" json:"synce"`
}

// SystemInfo is the device identity reported to every management plane. The
// uptime is read-only and counts seconds since the simulator started.
type SystemInfo struct {
	DeviceID    string `path:"device-id" xml:"device-id" json:"device-id"`
	Name        string `path:"name" xml:"name" json:"name"`
	Description string `path:"description" xml:"description,omitempty" json:"description,omitempty"`
	Contact     string `path:"contact" xml:"contact,omitempty" json:"contact,omitempty"`
	Location    string `path:"location" xml:"location,omitempty" json:"location,omitempty"`
	Uptime      uint32 `path:"uptime" xml:"uptime" json:"uptime" config:"false"`
}

// Validate checks the device id.
func (s SystemInfo) Validate() error {
	if s.DeviceID == "" {
		return errors.New("device-id must not be empty")
	}
	return nil
}

// Validate checks the system information, every interface, the VLAN list, the
// MAC table, the STP state, the LLDP configuration and the synchronization
// clock, and rejects duplicate interface names and VLAN ids.
func (d Device) Validate() error {
	if err := d.SystemInfo.Validate(); err != nil {
		return fmt.Errorf("system-info: %w", err)
	}

	seen := make(map[string]struct{}, len(d.Interfaces))
	for i, iface := range d.Interfaces {
		if err := iface.Validate(); err != nil {
			return fmt.Errorf("interfaces[%d]: %w", i, err)
		}
		if _, dup := seen[iface.Name]; dup {
			return fmt.Errorf("interfaces[%d]: duplicate interface %q", i, iface.Name)
		}
		seen[iface.Name] = struct{}{}
	}

	vlanIDs := make(map[uint16]struct{}, len(d.VLANs))
	for i, vlan := range d.VLANs {
		if err := vlan.Validate(); err != nil {
			return fmt.Errorf("vlans[%d]: %w", i, err)
		}
		if _, dup := vlanIDs[vlan.ID]; dup {
			return fmt.Errorf("vlans[%d]: duplicate vlan id %d", i, vlan.ID)
		}
		vlanIDs[vlan.ID] = struct{}{}
	}

	if err := d.MACTable.Validate(); err != nil {
		return fmt.Errorf("mac-table: %w", err)
	}
	if err := d.STP.Validate(); err != nil {
		return fmt.Errorf("stp: %w", err)
	}
	if err := d.LLDP.Validate(); err != nil {
		return fmt.Errorf("lldp: %w", err)
	}
	if err := d.PTP.Validate(); err != nil {
		return fmt.Errorf("ptp: %w", err)
	}
	if err := d.SyncE.Validate(); err != nil {
		return fmt.Errorf("synce: %w", err)
	}
	return nil
}
