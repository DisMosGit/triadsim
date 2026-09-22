package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validInterfaceEthernet() Interface {
	return Interface{
		Name:       "eth0",
		Type:       InterfaceTypeEthernet,
		Enabled:    true,
		MTU:        1500,
		MACAddress: "00:11:22:33:44:55",
	}
}

func validInterfaceRadio() Interface {
	link := validRadioLink()
	return Interface{
		Name:      "radio0",
		Type:      InterfaceTypeRadio,
		Enabled:   true,
		MTU:       1500,
		RadioLink: &link,
	}
}

func validVLANPort() VLANPort {
	return VLANPort{Port: "eth0", Mode: VLANPortModeAccess, PVID: 100}
}

func validVLAN() VLAN {
	return VLAN{ID: 100, Name: "DATA", Ports: []VLANPort{validVLANPort()}}
}

func validMACEntry() MACEntry {
	return MACEntry{MAC: "00:11:22:33:44:55", VLAN: 100, Port: 1, Type: MACEntryTypeDynamic}
}

func validSTPPort() STPPort {
	return STPPort{Port: "eth0", Role: STPPortRoleRoot, State: STPPortStateForwarding, Priority: 128, PathCost: 20000}
}

func validSTPState() STPState {
	return STPState{
		Enabled:        true,
		Protocol:       STPProtocolRSTP,
		BridgePriority: 32768,
		BridgeAddress:  "00:11:22:33:44:55",
		Ports:          []STPPort{validSTPPort()},
	}
}

func validLLDPNeighbor() LLDPNeighbor {
	return LLDPNeighbor{
		Port:      "eth0",
		ChassisID: "00:1a:2b:3c:4d:5e",
		PortID:    "Gi0/1",
		TTL:       120,
	}
}

func TestInterfaceValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Interface)
		wantErr bool
	}{
		{name: "valid ethernet", mutate: func(*Interface) {}},
		{name: "valid radio without radio-link", mutate: func(i *Interface) {
			*i = validInterfaceRadio()
			i.RadioLink = nil
		}},
		{name: "empty name", mutate: func(i *Interface) { i.Name = "" }, wantErr: true},
		{name: "unknown type", mutate: func(i *Interface) { i.Type = "atm" }, wantErr: true},
		{name: "empty type", mutate: func(i *Interface) { i.Type = "" }, wantErr: true},
		{name: "mtu at minimum", mutate: func(i *Interface) { i.MTU = InterfaceMTUMin }},
		{name: "mtu at maximum", mutate: func(i *Interface) { i.MTU = InterfaceMTUMax }},
		{name: "mtu below minimum", mutate: func(i *Interface) { i.MTU = 575 }, wantErr: true},
		{name: "mtu above maximum", mutate: func(i *Interface) { i.MTU = 9217 }, wantErr: true},
		{name: "malformed mac address", mutate: func(i *Interface) { i.MACAddress = "00:11:22" }, wantErr: true},
		{name: "invalid radio-link", mutate: func(i *Interface) {
			*i = validInterfaceRadio()
			i.RadioLink.TxPower = 31
		}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iface := validInterfaceEthernet()
			tt.mutate(&iface)

			err := iface.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestInterfaceCountersValidate(t *testing.T) {
	assert.NoError(t, InterfaceCounters{}.Validate())
	assert.NoError(t, InterfaceCounters{InOctets: 1, InErrors: 2}.Validate())
}

func TestVLANValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*VLAN)
		wantErr bool
	}{
		{name: "valid", mutate: func(*VLAN) {}},
		{name: "id at minimum", mutate: func(v *VLAN) { v.ID = VLANIDMin }},
		{name: "id at maximum", mutate: func(v *VLAN) { v.ID = VLANIDMax }},
		{name: "id zero", mutate: func(v *VLAN) { v.ID = 0 }, wantErr: true},
		{name: "id above maximum", mutate: func(v *VLAN) { v.ID = 4095 }, wantErr: true},
		{name: "empty name", mutate: func(v *VLAN) { v.Name = "" }, wantErr: true},
		{name: "no ports", mutate: func(v *VLAN) { v.Ports = nil }},
		{name: "invalid port", mutate: func(v *VLAN) { v.Ports = []VLANPort{validVLANPort()}; v.Ports[0].PVID = 0 }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vlan := validVLAN()
			tt.mutate(&vlan)

			err := vlan.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestVLANPortValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*VLANPort)
		wantErr bool
	}{
		{name: "access untagged", mutate: func(*VLANPort) {}},
		{name: "trunk tagged", mutate: func(p *VLANPort) { p.Mode, p.Tagged = VLANPortModeTrunk, true }},
		{name: "hybrid tagged", mutate: func(p *VLANPort) { p.Mode, p.Tagged = VLANPortModeHybrid, true }},
		{name: "empty port", mutate: func(p *VLANPort) { p.Port = "" }, wantErr: true},
		{name: "unknown mode", mutate: func(p *VLANPort) { p.Mode = "native" }, wantErr: true},
		{name: "empty mode", mutate: func(p *VLANPort) { p.Mode = "" }, wantErr: true},
		{name: "pvid at minimum", mutate: func(p *VLANPort) { p.PVID = VLANIDMin }},
		{name: "pvid at maximum", mutate: func(p *VLANPort) { p.PVID = VLANIDMax }},
		{name: "pvid zero", mutate: func(p *VLANPort) { p.PVID = 0 }, wantErr: true},
		{name: "pvid above maximum", mutate: func(p *VLANPort) { p.PVID = 4095 }, wantErr: true},
		{name: "access tagged", mutate: func(p *VLANPort) { p.Tagged = true }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := validVLANPort()
			tt.mutate(&port)

			err := port.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestMACEntryValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*MACEntry)
		wantErr bool
	}{
		{name: "valid dynamic", mutate: func(*MACEntry) {}},
		{name: "valid static permanent", mutate: func(m *MACEntry) {
			m.Type, m.Permanent = MACEntryTypeStatic, true
		}},
		{name: "vlan zero means untagged", mutate: func(m *MACEntry) { m.VLAN = 0 }},
		{name: "vlan at maximum", mutate: func(m *MACEntry) { m.VLAN = VLANIDMax }},
		{name: "vlan above maximum", mutate: func(m *MACEntry) { m.VLAN = 4095 }, wantErr: true},
		{name: "malformed mac", mutate: func(m *MACEntry) { m.MAC = "00:11:22" }, wantErr: true},
		{name: "port zero", mutate: func(m *MACEntry) { m.Port = 0 }, wantErr: true},
		{name: "unknown type", mutate: func(m *MACEntry) { m.Type = "learned" }, wantErr: true},
		{name: "permanent dynamic", mutate: func(m *MACEntry) { m.Permanent = true }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := validMACEntry()
			tt.mutate(&entry)

			err := entry.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestSTPStateValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*STPState)
		wantErr bool
	}{
		{name: "valid rstp", mutate: func(*STPState) {}},
		{name: "valid stp", mutate: func(s *STPState) { s.Protocol = STPProtocolSTP }},
		{name: "disabled ignores unset ports", mutate: func(s *STPState) {
			s.Enabled = false
			s.Ports = []STPPort{{Port: "eth0"}}
		}},
		{name: "no ports", mutate: func(s *STPState) { s.Ports = nil }},
		{name: "unknown protocol", mutate: func(s *STPState) { s.Protocol = "mstp" }, wantErr: true},
		{name: "bridge priority at maximum", mutate: func(s *STPState) { s.BridgePriority = STPBridgePriorityMax }},
		{name: "bridge priority not a step", mutate: func(s *STPState) { s.BridgePriority = 32767 }, wantErr: true},
		{name: "bridge priority above maximum", mutate: func(s *STPState) { s.BridgePriority = 65535 }, wantErr: true},
		{name: "malformed bridge address", mutate: func(s *STPState) { s.BridgeAddress = "nope" }, wantErr: true},
		{name: "malformed root id", mutate: func(s *STPState) { s.RootID = "nope" }, wantErr: true},
		{name: "enabled with invalid port", mutate: func(s *STPState) {
			s.Ports = []STPPort{validSTPPort()}
			s.Ports[0].Role = "master"
		}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := validSTPState()
			tt.mutate(&state)

			err := state.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestSTPPortValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*STPPort)
		wantErr bool
	}{
		{name: "valid", mutate: func(*STPPort) {}},
		{name: "empty port", mutate: func(p *STPPort) { p.Port = "" }, wantErr: true},
		{name: "unknown role", mutate: func(p *STPPort) { p.Role = "master" }, wantErr: true},
		{name: "empty role", mutate: func(p *STPPort) { p.Role = "" }, wantErr: true},
		{name: "unknown state", mutate: func(p *STPPort) { p.State = "blocking" }, wantErr: true},
		{name: "empty state", mutate: func(p *STPPort) { p.State = "" }, wantErr: true},
		{name: "priority at minimum", mutate: func(p *STPPort) { p.Priority = 0 }},
		{name: "priority at maximum", mutate: func(p *STPPort) { p.Priority = STPPortPriorityMax }},
		{name: "priority not a step", mutate: func(p *STPPort) { p.Priority = 15 }, wantErr: true},
		{name: "priority above maximum", mutate: func(p *STPPort) { p.Priority = 241 }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := validSTPPort()
			tt.mutate(&port)

			err := port.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestLLDPNeighborValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*LLDPNeighbor)
		wantErr bool
	}{
		{name: "valid", mutate: func(*LLDPNeighbor) {}},
		{name: "empty port", mutate: func(n *LLDPNeighbor) { n.Port = "" }, wantErr: true},
		{name: "empty chassis id", mutate: func(n *LLDPNeighbor) { n.ChassisID = "" }, wantErr: true},
		{name: "empty port id", mutate: func(n *LLDPNeighbor) { n.PortID = "" }, wantErr: true},
		{name: "ttl at minimum", mutate: func(n *LLDPNeighbor) { n.TTL = 1 }},
		{name: "ttl at maximum", mutate: func(n *LLDPNeighbor) { n.TTL = LLDPTTLMax }},
		{name: "ttl zero", mutate: func(n *LLDPNeighbor) { n.TTL = 0 }, wantErr: true},
		{name: "ttl above maximum", mutate: func(n *LLDPNeighbor) { n.TTL = 65536 }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			neighbor := validLLDPNeighbor()
			tt.mutate(&neighbor)

			err := neighbor.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
