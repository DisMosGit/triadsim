package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validSystemInfo() SystemInfo {
	return SystemInfo{
		DeviceID:    "sim-001",
		Name:        "triadsim-01",
		Description: "TriadSim device",
		Contact:     "noc@example.net",
		Location:    "lab",
		Uptime:      123,
	}
}

func validDevice() Device {
	return Device{
		SystemInfo: validSystemInfo(),
		Interfaces: []Interface{
			validInterfaceRadio(),
			validInterfaceEthernet(),
		},
	}
}

func TestSystemInfoValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*SystemInfo)
		wantErr bool
	}{
		{name: "valid", mutate: func(*SystemInfo) {}},
		{name: "only device id", mutate: func(s *SystemInfo) { *s = SystemInfo{DeviceID: "sim-001"} }},
		{name: "empty device id", mutate: func(s *SystemInfo) { s.DeviceID = "" }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := validSystemInfo()
			tt.mutate(&info)

			err := info.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestDeviceValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Device)
		wantErr bool
	}{
		{name: "valid", mutate: func(*Device) {}},
		{name: "no interfaces", mutate: func(d *Device) { d.Interfaces = nil }},
		{name: "three interfaces", mutate: func(d *Device) {
			d.Interfaces = []Interface{
				validInterfaceRadio(),
				validInterfaceEthernet(),
				{Name: "eth1", Type: InterfaceTypeEthernet, Enabled: true, MTU: 1500},
			}
		}},
		{name: "invalid system info", mutate: func(d *Device) { d.SystemInfo.DeviceID = "" }, wantErr: true},
		{name: "duplicate interface name", mutate: func(d *Device) {
			d.Interfaces = []Interface{validInterfaceEthernet(), validInterfaceEthernet()}
		}, wantErr: true},
		{name: "invalid interface", mutate: func(d *Device) {
			d.Interfaces = []Interface{validInterfaceEthernet()}
			d.Interfaces[0].MTU = 0
		}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := validDevice()
			tt.mutate(&device)

			err := device.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
