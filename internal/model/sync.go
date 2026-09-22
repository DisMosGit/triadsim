package model

import (
	"errors"
	"fmt"
)

// QL is an ITU-T G.781 Option I synchronization quality level.
type QL string

// Quality levels of an Option I synchronization network.
const (
	QLPRC  QL = "QL-PRC"
	QLSSUA QL = "QL-SSU-A"
	QLSSUB QL = "QL-SSU-B"
	QLSEC  QL = "QL-SEC"
	QLDNU  QL = "QL-DNU"
)

// Extended (eSSM) quality levels from ITU-T G.8264 Amendment 2.
const (
	ExtendedQLPRTC         = "QL-PRTC"
	ExtendedQLPRTCEnhanced = "QL-ePRTC"
	ExtendedQLEECEnhanced  = "QL-eEEC"
)

// PTP clock modes.
const (
	PTPModeMaster   = "master"
	PTPModeSlave    = "slave"
	PTPModeBoundary = "boundary"
)

// PTP domain bounds (IEEE 1588: 0-127).
const PTPDomainMax uint8 = 127

// PTP clock-level states from ITU-T G.8275.1.
const (
	PTPStateFreerun           = "freerun"
	PTPStateAcquiring         = "acquiring"
	PTPStateLocked            = "locked"
	PTPStateHoldoverInSpec    = "holdover-in-spec"
	PTPStateHoldoverOutOfSpec = "holdover-out-of-spec"
)

// PTPClock is the managed object of the PTP clock.
type PTPClock struct {
	Mode            string  `path:"mode" xml:"mode" json:"mode"`
	Domain          uint8   `path:"domain" xml:"domain" json:"domain"`
	Priority1       uint8   `path:"priority1" xml:"priority1" json:"priority1"`
	Priority2       uint8   `path:"priority2" xml:"priority2" json:"priority2"`
	ClockClass      uint8   `path:"clock-class" xml:"clock-class" json:"clock-class"`
	ClockAccuracy   uint8   `path:"clock-accuracy" xml:"clock-accuracy" json:"clock-accuracy"`
	HoldoverTimeout uint32  `path:"holdover-timeout" xml:"holdover-timeout" json:"holdover-timeout"`
	State           string  `path:"state" xml:"state" json:"state" config:"false"`
	Offset          float64 `path:"offset" xml:"offset" json:"offset" config:"false"`
}

// SyncEState is the managed object of the SyncE domain: the global switch, the
// selected source and the per-interface quality levels.
type SyncEState struct {
	Enabled        bool             `path:"enabled" xml:"enabled" json:"enabled"`
	SelectedSource string           `path:"selected-source" xml:"selected-source,omitempty" json:"selected-source,omitempty" config:"false"`
	Interfaces     []SyncEInterface `path:"interfaces/interface" xml:"interfaces>interface" json:"interfaces"`
}

// SyncEInterface is one SyncE-capable interface and its SSM state.
type SyncEInterface struct {
	Name          string `path:"name" key:"true" xml:"name" json:"name"`
	SSMEnabled    bool   `path:"ssm-enabled" xml:"ssm-enabled" json:"ssm-enabled"`
	QL            QL     `path:"ql" xml:"ql" json:"ql"`
	ExtendedQL    string `path:"extended-ql" xml:"extended-ql,omitempty" json:"extended-ql,omitempty"`
	Priority      uint8  `path:"priority" xml:"priority" json:"priority"`
	PTPPreference bool   `path:"ptp-preference" xml:"ptp-preference" json:"ptp-preference"`
}

// ESMC is the Ethernet Synchronization Messaging Channel configuration.
type ESMC struct {
	Enabled       bool   `path:"enabled" xml:"enabled" json:"enabled"`
	TxInterval    uint32 `path:"tx-interval" xml:"tx-interval" json:"tx-interval"`
	ExtendedCodes bool   `path:"extended-codes" xml:"extended-codes" json:"extended-codes"`
}

// Validate reports whether q is one of the Option I quality levels.
func (q QL) Validate() error {
	switch q {
	case QLPRC, QLSSUA, QLSSUB, QLSEC, QLDNU:
		return nil
	default:
		return fmt.Errorf("unknown quality level %q", string(q))
	}
}

// Validate checks the PTP mode, domain, holdover timeout and, when set, the
// clock state.
func (c PTPClock) Validate() error {
	switch c.Mode {
	case PTPModeMaster, PTPModeSlave, PTPModeBoundary:
	default:
		return fmt.Errorf("unknown mode %q (want %s, %s or %s)", c.Mode, PTPModeMaster, PTPModeSlave, PTPModeBoundary)
	}
	if c.Domain > PTPDomainMax {
		return fmt.Errorf("domain %d out of range 0..%d", c.Domain, PTPDomainMax)
	}
	if c.HoldoverTimeout == 0 {
		return errors.New("holdover-timeout must be greater than 0")
	}
	if !finite(c.Offset) {
		return errors.New("offset must be a finite number")
	}
	if c.State == "" {
		return nil
	}
	switch c.State {
	case PTPStateFreerun, PTPStateAcquiring, PTPStateLocked, PTPStateHoldoverInSpec, PTPStateHoldoverOutOfSpec:
		return nil
	default:
		return fmt.Errorf("unknown state %q", c.State)
	}
}

// Validate checks the SyncE switch, the selected source and every interface.
func (s SyncEState) Validate() error {
	seen := make(map[string]struct{}, len(s.Interfaces))
	for i, iface := range s.Interfaces {
		if err := iface.Validate(); err != nil {
			return fmt.Errorf("interfaces[%d]: %w", i, err)
		}
		if _, dup := seen[iface.Name]; dup {
			return fmt.Errorf("duplicate interface %q", iface.Name)
		}
		seen[iface.Name] = struct{}{}
	}
	if s.SelectedSource != "" {
		if _, ok := seen[s.SelectedSource]; !ok {
			return fmt.Errorf("selected-source %q does not name an interface", s.SelectedSource)
		}
	}
	return nil
}

// Validate checks the interface name, quality level and extended quality level.
func (i SyncEInterface) Validate() error {
	if i.Name == "" {
		return errors.New("name must not be empty")
	}
	if err := i.QL.Validate(); err != nil {
		return fmt.Errorf("interface %s: %w", i.Name, err)
	}
	switch i.ExtendedQL {
	case "", ExtendedQLPRTC, ExtendedQLPRTCEnhanced, ExtendedQLEECEnhanced:
	default:
		return fmt.Errorf("interface %s: unknown extended-ql %q", i.Name, i.ExtendedQL)
	}
	return nil
}

// Validate checks the ESMC transmission interval.
func (e ESMC) Validate() error {
	if e.TxInterval == 0 {
		return errors.New("tx-interval must be greater than 0")
	}
	return nil
}
