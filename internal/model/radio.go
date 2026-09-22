package model

import (
	"errors"
	"fmt"
)

// Radio-link operating limits, in the units of the corresponding leaf.
const (
	TxPowerMinDBM = -10.0
	TxPowerMaxDBM = 30.0

	RSSIMinDBM = -99.0
	RSSIMaxDBM = -20.0

	FrequencyMinGHz = 6.0
	FrequencyMaxGHz = 86.0

	AntennaGainMinDBI = 30.0
	AntennaGainMaxDBI = 45.0

	FeedLossMinDB = 1.0
	FeedLossMaxDB = 5.0
)

// ACM operating modes.
const (
	ACMModeAdaptive = "adaptive"
	ACMModeFixed    = "fixed"
)

// Radio-link operational states. They are written by the radio domain into the
// read-only link-state leaf and are what its alarms report.
const (
	RadioLinkStateUp       = "up"
	RadioLinkStateDegraded = "degraded"
	RadioLinkStateDown     = "down"
)

// ACM profile index bounds. Profiles are the entries of RadioLink.Profiles.
const (
	MinACMProfile uint8 = 1
	MaxACMProfile uint8 = 12
)

// RadioLink is the managed object of the radio-link (RRL) domain.
//
// The fields marked config:"false" are reported by the device but cannot be
// written through a management plane; the router enforces that.
type RadioLink struct {
	Name       string       `path:"name" xml:"name" json:"name"`
	TxPower    float64      `path:"tx-power" xml:"tx-power" json:"tx-power"`
	RSSI       float64      `path:"rssi" xml:"rssi" json:"rssi" config:"false"`
	FadeMargin float64      `path:"fade-margin" xml:"fade-margin" json:"fade-margin" config:"false"`
	Capacity   uint32       `path:"capacity" xml:"capacity" json:"capacity" config:"false"`
	LinkState  string       `path:"link-state" xml:"link-state" json:"link-state" config:"false"`
	LinkBudget LinkBudget   `path:"link-budget" xml:"link-budget" json:"link-budget"`
	ATPC       ATPC         `path:"atpc" xml:"atpc" json:"atpc"`
	ACM        ACM          `path:"acm" xml:"acm" json:"acm"`
	Profiles   []ModProfile `path:"modulation-profile" xml:"modulation-profile" json:"modulation-profile"`
}

// LinkBudget holds the static link parameters and the derived received level.
type LinkBudget struct {
	LinkLength    float64 `path:"link-length" xml:"link-length" json:"link-length"`
	Frequency     float64 `path:"frequency" xml:"frequency" json:"frequency"`
	TxAntennaGain float64 `path:"tx-antenna-gain" xml:"tx-antenna-gain" json:"tx-antenna-gain"`
	RxAntennaGain float64 `path:"rx-antenna-gain" xml:"rx-antenna-gain" json:"rx-antenna-gain"`
	FeedLoss      float64 `path:"feed-loss" xml:"feed-loss" json:"feed-loss"`
	CalculatedRSL float64 `path:"calculated-rsl" xml:"calculated-rsl" json:"calculated-rsl" config:"false"`
}

// ATPC is the automatic transmit power control configuration.
type ATPC struct {
	Enabled      bool    `path:"enabled" xml:"enabled" json:"enabled"`
	TargetRSL    float64 `path:"target-rsl" xml:"target-rsl" json:"target-rsl"`
	MinPower     float64 `path:"min-power" xml:"min-power" json:"min-power"`
	MaxPower     float64 `path:"max-power" xml:"max-power" json:"max-power"`
	Range        float64 `path:"range" xml:"range" json:"range"`
	CurrentPower float64 `path:"current-power" xml:"current-power" json:"current-power" config:"false"`
}

// ACM is the adaptive coding and modulation configuration.
type ACM struct {
	Enabled         bool   `path:"enabled" xml:"enabled" json:"enabled"`
	Mode            string `path:"mode" xml:"mode" json:"mode"`
	MinProfile      uint8  `path:"min-profile" xml:"min-profile" json:"min-profile"`
	MaxProfile      uint8  `path:"max-profile" xml:"max-profile" json:"max-profile"`
	CurrentProfile  uint8  `path:"current-profile" xml:"current-profile" json:"current-profile" config:"false"`
	CurrentCapacity uint32 `path:"current-capacity" xml:"current-capacity" json:"current-capacity" config:"false"`
}

// ModProfile is one entry of the modulation-profile table: a modulation and
// coding scheme with its receiver threshold and capacity.
type ModProfile struct {
	ID                 uint8   `path:"id" key:"true" xml:"id" json:"id"`
	Name               string  `path:"name" xml:"name" json:"name"`
	Modulation         string  `path:"modulation" xml:"modulation" json:"modulation"`
	CodingRate         string  `path:"coding-rate" xml:"coding-rate" json:"coding-rate"`
	SpectralEfficiency float64 `path:"spectral-efficiency" xml:"spectral-efficiency" json:"spectral-efficiency"`
	RSLThreshold       float64 `path:"rsl-threshold" xml:"rsl-threshold" json:"rsl-threshold"`
	Capacity           uint32  `path:"capacity" xml:"capacity" json:"capacity"`
}

// Validate checks the radio link, its link budget, ATPC, ACM and every
// modulation profile.
func (r RadioLink) Validate() error {
	if r.Name == "" {
		return errors.New("radio-link: name must not be empty")
	}
	if !inRange(r.TxPower, TxPowerMinDBM, TxPowerMaxDBM) {
		return fmt.Errorf("radio-link %s: tx-power %.1f out of range %.0f..%.0f dBm",
			r.Name, r.TxPower, TxPowerMinDBM, TxPowerMaxDBM)
	}
	if !inRange(r.RSSI, RSSIMinDBM, RSSIMaxDBM) {
		return fmt.Errorf("radio-link %s: rssi %.1f out of range %.0f..%.0f dBm",
			r.Name, r.RSSI, RSSIMinDBM, RSSIMaxDBM)
	}
	if !finite(r.FadeMargin) {
		return fmt.Errorf("radio-link %s: fade-margin must be a finite number", r.Name)
	}
	if r.Capacity == 0 {
		return fmt.Errorf("radio-link %s: capacity must be greater than 0", r.Name)
	}
	switch r.LinkState {
	case RadioLinkStateUp, RadioLinkStateDegraded, RadioLinkStateDown:
	default:
		return fmt.Errorf("radio-link %s: unknown link-state %q (want %s, %s or %s)",
			r.Name, r.LinkState, RadioLinkStateUp, RadioLinkStateDegraded, RadioLinkStateDown)
	}
	if err := r.LinkBudget.Validate(); err != nil {
		return fmt.Errorf("radio-link %s: link-budget: %w", r.Name, err)
	}
	if err := r.ATPC.Validate(); err != nil {
		return fmt.Errorf("radio-link %s: atpc: %w", r.Name, err)
	}
	if err := r.ACM.Validate(); err != nil {
		return fmt.Errorf("radio-link %s: acm: %w", r.Name, err)
	}

	seen := make(map[uint8]struct{}, len(r.Profiles))
	for i, profile := range r.Profiles {
		if err := profile.Validate(); err != nil {
			return fmt.Errorf("radio-link %s: modulation-profile[%d]: %w", r.Name, i, err)
		}
		if _, dup := seen[profile.ID]; dup {
			return fmt.Errorf("radio-link %s: duplicate modulation-profile id %d", r.Name, profile.ID)
		}
		seen[profile.ID] = struct{}{}
	}
	return nil
}

// Validate checks the link-budget parameters and the derived received level.
func (l LinkBudget) Validate() error {
	if !finite(l.LinkLength) || l.LinkLength <= 0 {
		return fmt.Errorf("link-length %.1f must be greater than 0", l.LinkLength)
	}
	if !inRange(l.Frequency, FrequencyMinGHz, FrequencyMaxGHz) {
		return fmt.Errorf("frequency %.1f out of range %.0f..%.0f GHz", l.Frequency, FrequencyMinGHz, FrequencyMaxGHz)
	}
	if !inRange(l.TxAntennaGain, AntennaGainMinDBI, AntennaGainMaxDBI) {
		return fmt.Errorf("tx-antenna-gain %.1f out of range %.0f..%.0f dBi", l.TxAntennaGain, AntennaGainMinDBI, AntennaGainMaxDBI)
	}
	if !inRange(l.RxAntennaGain, AntennaGainMinDBI, AntennaGainMaxDBI) {
		return fmt.Errorf("rx-antenna-gain %.1f out of range %.0f..%.0f dBi", l.RxAntennaGain, AntennaGainMinDBI, AntennaGainMaxDBI)
	}
	if !inRange(l.FeedLoss, FeedLossMinDB, FeedLossMaxDB) {
		return fmt.Errorf("feed-loss %.1f out of range %.0f..%.0f dB", l.FeedLoss, FeedLossMinDB, FeedLossMaxDB)
	}
	if !inRange(l.CalculatedRSL, RSSIMinDBM, RSSIMaxDBM) {
		return fmt.Errorf("calculated-rsl %.1f out of range %.0f..%.0f dBm", l.CalculatedRSL, RSSIMinDBM, RSSIMaxDBM)
	}
	return nil
}

// Validate checks the ATPC configuration. The min/max power ordering, the
// current power and the range are checked only while ATPC is enabled.
func (a ATPC) Validate() error {
	if !inRange(a.TargetRSL, RSSIMinDBM, RSSIMaxDBM) {
		return fmt.Errorf("target-rsl %.1f out of range %.0f..%.0f dBm", a.TargetRSL, RSSIMinDBM, RSSIMaxDBM)
	}
	if !inRange(a.MinPower, TxPowerMinDBM, TxPowerMaxDBM) {
		return fmt.Errorf("min-power %.1f out of range %.0f..%.0f dBm", a.MinPower, TxPowerMinDBM, TxPowerMaxDBM)
	}
	if !inRange(a.MaxPower, TxPowerMinDBM, TxPowerMaxDBM) {
		return fmt.Errorf("max-power %.1f out of range %.0f..%.0f dBm", a.MaxPower, TxPowerMinDBM, TxPowerMaxDBM)
	}
	if !finite(a.Range) || a.Range <= 0 {
		return fmt.Errorf("range %.1f must be greater than 0", a.Range)
	}
	if a.Enabled {
		if a.MinPower >= a.MaxPower {
			return fmt.Errorf("min-power %.1f must be less than max-power %.1f", a.MinPower, a.MaxPower)
		}
		if a.CurrentPower < a.MinPower || a.CurrentPower > a.MaxPower {
			return fmt.Errorf("current-power %.1f outside min-power %.1f..max-power %.1f",
				a.CurrentPower, a.MinPower, a.MaxPower)
		}
	}
	return nil
}

// Validate checks the ACM mode and the min/max profile window.
func (a ACM) Validate() error {
	switch a.Mode {
	case ACMModeAdaptive, ACMModeFixed:
	default:
		return fmt.Errorf("unknown mode %q (want %s or %s)", a.Mode, ACMModeAdaptive, ACMModeFixed)
	}
	if a.MinProfile < MinACMProfile || a.MinProfile > MaxACMProfile {
		return fmt.Errorf("min-profile %d out of range %d..%d", a.MinProfile, MinACMProfile, MaxACMProfile)
	}
	if a.MaxProfile < MinACMProfile || a.MaxProfile > MaxACMProfile {
		return fmt.Errorf("max-profile %d out of range %d..%d", a.MaxProfile, MinACMProfile, MaxACMProfile)
	}
	if a.MinProfile > a.MaxProfile {
		return fmt.Errorf("min-profile %d must not exceed max-profile %d", a.MinProfile, a.MaxProfile)
	}
	return nil
}

// Validate checks one modulation profile.
func (p ModProfile) Validate() error {
	if p.ID < MinACMProfile || p.ID > MaxACMProfile {
		return fmt.Errorf("id %d out of range %d..%d", p.ID, MinACMProfile, MaxACMProfile)
	}
	if p.Name == "" {
		return fmt.Errorf("id %d: name must not be empty", p.ID)
	}
	if p.Modulation == "" {
		return fmt.Errorf("id %d: modulation must not be empty", p.ID)
	}
	if p.CodingRate == "" {
		return fmt.Errorf("id %d: coding-rate must not be empty", p.ID)
	}
	if !finite(p.SpectralEfficiency) || p.SpectralEfficiency <= 0 {
		return fmt.Errorf("id %d: spectral-efficiency %.1f must be greater than 0", p.ID, p.SpectralEfficiency)
	}
	if !inRange(p.RSLThreshold, RSSIMinDBM, RSSIMaxDBM) {
		return fmt.Errorf("id %d: rsl-threshold %.1f out of range %.0f..%.0f dBm", p.ID, p.RSLThreshold, RSSIMinDBM, RSSIMaxDBM)
	}
	if p.Capacity == 0 {
		return fmt.Errorf("id %d: capacity must be greater than 0", p.ID)
	}
	return nil
}
