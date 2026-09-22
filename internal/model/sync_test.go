package model

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validPTPClock() PTPClock {
	return PTPClock{
		Mode:            PTPModeBoundary,
		Domain:          24,
		Priority1:       128,
		Priority2:       128,
		ClockClass:      6,
		ClockAccuracy:   0x21,
		HoldoverTimeout: 300,
		State:           PTPStateLocked,
		Offset:          12.5,
	}
}

func validSyncEInterface() SyncEInterface {
	return SyncEInterface{
		Name:       "eth0",
		SSMEnabled: true,
		QL:         QLPRC,
		Priority:   10,
	}
}

func validSyncEState() SyncEState {
	return SyncEState{
		Enabled:        true,
		SelectedSource: "eth0",
		Interfaces:     []SyncEInterface{validSyncEInterface()},
	}
}

func validESMC() ESMC {
	return ESMC{Enabled: true, TxInterval: 1, ExtendedCodes: false}
}

func TestQLValidate(t *testing.T) {
	tests := []struct {
		name    string
		ql      QL
		wantErr bool
	}{
		{name: "PRC", ql: QLPRC},
		{name: "SSU-A", ql: QLSSUA},
		{name: "SSU-B", ql: QLSSUB},
		{name: "SEC", ql: QLSEC},
		{name: "DNU", ql: QLDNU},
		{name: "empty", ql: "", wantErr: true},
		{name: "unknown", ql: "QL-BOGUS", wantErr: true},
		{name: "wrong case", ql: "ql-prc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ql.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestPTPClockValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*PTPClock)
		wantErr bool
	}{
		{name: "valid", mutate: func(*PTPClock) {}},
		{name: "unset state", mutate: func(c *PTPClock) { c.State = "" }},
		{name: "master mode", mutate: func(c *PTPClock) { c.Mode = PTPModeMaster }},
		{name: "slave mode", mutate: func(c *PTPClock) { c.Mode = PTPModeSlave }},
		{name: "freerun state", mutate: func(c *PTPClock) { c.State = PTPStateFreerun }},
		{name: "acquiring state", mutate: func(c *PTPClock) { c.State = PTPStateAcquiring }},
		{name: "holdover in spec", mutate: func(c *PTPClock) { c.State = PTPStateHoldoverInSpec }},
		{name: "holdover out of spec", mutate: func(c *PTPClock) { c.State = PTPStateHoldoverOutOfSpec }},
		{name: "unknown mode", mutate: func(c *PTPClock) { c.Mode = "grandmaster" }, wantErr: true},
		{name: "empty mode", mutate: func(c *PTPClock) { c.Mode = "" }, wantErr: true},
		{name: "unknown state", mutate: func(c *PTPClock) { c.State = "synced" }, wantErr: true},
		{name: "domain at maximum", mutate: func(c *PTPClock) { c.Domain = PTPDomainMax }},
		{name: "domain above maximum", mutate: func(c *PTPClock) { c.Domain = 128 }, wantErr: true},
		{name: "holdover timeout zero", mutate: func(c *PTPClock) { c.HoldoverTimeout = 0 }, wantErr: true},
		{name: "holdover timeout one", mutate: func(c *PTPClock) { c.HoldoverTimeout = 1 }},
		{name: "offset NaN", mutate: func(c *PTPClock) { c.Offset = math.NaN() }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := validPTPClock()
			tt.mutate(&clock)

			err := clock.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestSyncEInterfaceValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*SyncEInterface)
		wantErr bool
	}{
		{name: "valid", mutate: func(*SyncEInterface) {}},
		{name: "extended PRTC", mutate: func(i *SyncEInterface) { i.ExtendedQL = ExtendedQLPRTC }},
		{name: "extended ePRTC", mutate: func(i *SyncEInterface) { i.ExtendedQL = ExtendedQLPRTCEnhanced }},
		{name: "extended eEEC", mutate: func(i *SyncEInterface) { i.ExtendedQL = ExtendedQLEECEnhanced }},
		{name: "empty name", mutate: func(i *SyncEInterface) { i.Name = "" }, wantErr: true},
		{name: "empty ql", mutate: func(i *SyncEInterface) { i.QL = "" }, wantErr: true},
		{name: "unknown ql", mutate: func(i *SyncEInterface) { i.QL = "QL-NOPE" }, wantErr: true},
		{name: "unknown extended ql", mutate: func(i *SyncEInterface) { i.ExtendedQL = "QL-NOPE" }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iface := validSyncEInterface()
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

func TestSyncEStateValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*SyncEState)
		wantErr bool
	}{
		{name: "valid", mutate: func(*SyncEState) {}},
		{name: "no interfaces and no selection", mutate: func(s *SyncEState) {
			s.Interfaces = nil
			s.SelectedSource = ""
		}},
		{name: "duplicate interface names", mutate: func(s *SyncEState) {
			s.Interfaces = []SyncEInterface{validSyncEInterface(), validSyncEInterface()}
		}, wantErr: true},
		{name: "selected source unknown", mutate: func(s *SyncEState) { s.SelectedSource = "eth9" }, wantErr: true},
		{name: "invalid interface", mutate: func(s *SyncEState) {
			s.Interfaces = []SyncEInterface{validSyncEInterface()}
			s.Interfaces[0].QL = ""
		}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := validSyncEState()
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

func TestESMCValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ESMC)
		wantErr bool
	}{
		{name: "valid", mutate: func(*ESMC) {}},
		{name: "disabled", mutate: func(e *ESMC) { e.Enabled = false }},
		{name: "zero interval", mutate: func(e *ESMC) { e.TxInterval = 0 }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			esmc := validESMC()
			tt.mutate(&esmc)

			err := esmc.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
