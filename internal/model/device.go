package model

import (
	"errors"
	"fmt"
)

// Device is the root managed object: the system information and the interface
// list. Router paths start here, for example
// interfaces/interface[name=radio0]/radio-link/tx-power.
type Device struct {
	SystemInfo SystemInfo  `path:"system-info" xml:"system-info" json:"system-info"`
	Interfaces []Interface `path:"interfaces/interface" xml:"interfaces>interface" json:"interfaces"`
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

// Validate checks the system information and every interface, and rejects
// duplicate interface names.
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
	return nil
}
