package model

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validATPC returns an ATPC block that passes Validate.
func validATPC() ATPC {
	return ATPC{
		Enabled:      true,
		TargetRSL:    -45,
		MinPower:     -10,
		MaxPower:     30,
		Range:        20,
		CurrentPower: 18.5,
	}
}

// validACM returns an ACM block that passes Validate.
func validACM() ACM {
	return ACM{
		Enabled:         true,
		Mode:            ACMModeAdaptive,
		MinProfile:      1,
		MaxProfile:      8,
		CurrentProfile:  5,
		CurrentCapacity: 112,
	}
}

// validLinkBudget returns a link budget that passes Validate.
func validLinkBudget() LinkBudget {
	return LinkBudget{
		LinkLength:    12.5,
		Frequency:     18,
		TxAntennaGain: 38,
		RxAntennaGain: 38,
		FeedLoss:      2,
		CalculatedRSL: -72.5,
	}
}

// validModProfile returns a modulation profile that passes Validate.
func validModProfile(id uint8) ModProfile {
	return ModProfile{
		ID:                 id,
		Name:               "acm-1",
		Modulation:         "QPSK",
		CodingRate:         "1/2",
		SpectralEfficiency: 1.0,
		RSLThreshold:       -88,
		Capacity:           28,
	}
}

// validRadioLink returns a radio link that passes Validate.
func validRadioLink() RadioLink {
	return RadioLink{
		Name:       "radio0",
		TxPower:    20,
		RSSI:       -72.5,
		FadeMargin: 12.5,
		Capacity:   112,
		LinkBudget: validLinkBudget(),
		ATPC:       validATPC(),
		ACM:        validACM(),
		Profiles:   []ModProfile{validModProfile(1)},
	}
}

func TestRadioLinkValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*RadioLink)
		wantErr bool
	}{
		{name: "valid", mutate: func(*RadioLink) {}},
		{name: "empty name", mutate: func(r *RadioLink) { r.Name = "" }, wantErr: true},
		{name: "tx-power at minimum", mutate: func(r *RadioLink) { r.TxPower = TxPowerMinDBM }},
		{name: "tx-power at maximum", mutate: func(r *RadioLink) { r.TxPower = TxPowerMaxDBM }},
		{name: "tx-power below minimum", mutate: func(r *RadioLink) { r.TxPower = -10.1 }, wantErr: true},
		{name: "tx-power above maximum", mutate: func(r *RadioLink) { r.TxPower = 30.1 }, wantErr: true},
		{name: "rssi at minimum", mutate: func(r *RadioLink) { r.RSSI = RSSIMinDBM }},
		{name: "rssi at maximum", mutate: func(r *RadioLink) { r.RSSI = RSSIMaxDBM }},
		{name: "rssi below minimum", mutate: func(r *RadioLink) { r.RSSI = -99.1 }, wantErr: true},
		{name: "rssi above maximum", mutate: func(r *RadioLink) { r.RSSI = -19.9 }, wantErr: true},
		{name: "capacity zero", mutate: func(r *RadioLink) { r.Capacity = 0 }, wantErr: true},
		{name: "fade-margin NaN", mutate: func(r *RadioLink) { r.FadeMargin = math.NaN() }, wantErr: true},
		{name: "no profiles", mutate: func(r *RadioLink) { r.Profiles = nil }},
		{name: "duplicate profile id", mutate: func(r *RadioLink) {
			r.Profiles = []ModProfile{validModProfile(1), validModProfile(1)}
		}, wantErr: true},
		{name: "invalid profile", mutate: func(r *RadioLink) {
			r.Profiles = []ModProfile{validModProfile(1)}
			r.Profiles[0].Capacity = 0
		}, wantErr: true},
		{name: "invalid link budget", mutate: func(r *RadioLink) { r.LinkBudget.Frequency = 5.9 }, wantErr: true},
		{name: "atpc min equals max", mutate: func(r *RadioLink) {
			r.ATPC.MinPower = 10
			r.ATPC.MaxPower = 10
		}, wantErr: true},
		{name: "acm min above max", mutate: func(r *RadioLink) { r.ACM.MinProfile, r.ACM.MaxProfile = 8, 1 }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := validRadioLink()
			tt.mutate(&link)

			err := link.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestLinkBudgetValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*LinkBudget)
		wantErr bool
	}{
		{name: "valid", mutate: func(*LinkBudget) {}},
		{name: "link-length zero", mutate: func(l *LinkBudget) { l.LinkLength = 0 }, wantErr: true},
		{name: "link-length negative", mutate: func(l *LinkBudget) { l.LinkLength = -1 }, wantErr: true},
		{name: "frequency at minimum", mutate: func(l *LinkBudget) { l.Frequency = FrequencyMinGHz }},
		{name: "frequency at maximum", mutate: func(l *LinkBudget) { l.Frequency = FrequencyMaxGHz }},
		{name: "frequency below minimum", mutate: func(l *LinkBudget) { l.Frequency = 5.9 }, wantErr: true},
		{name: "frequency above maximum", mutate: func(l *LinkBudget) { l.Frequency = 86.1 }, wantErr: true},
		{name: "tx gain at minimum", mutate: func(l *LinkBudget) { l.TxAntennaGain = AntennaGainMinDBI }},
		{name: "tx gain below minimum", mutate: func(l *LinkBudget) { l.TxAntennaGain = 29.9 }, wantErr: true},
		{name: "rx gain above maximum", mutate: func(l *LinkBudget) { l.RxAntennaGain = 45.1 }, wantErr: true},
		{name: "feed loss at minimum", mutate: func(l *LinkBudget) { l.FeedLoss = FeedLossMinDB }},
		{name: "feed loss above maximum", mutate: func(l *LinkBudget) { l.FeedLoss = 5.1 }, wantErr: true},
		{name: "calculated rsl out of range", mutate: func(l *LinkBudget) { l.CalculatedRSL = -100 }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := validLinkBudget()
			tt.mutate(&budget)

			err := budget.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestATPCValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ATPC)
		wantErr bool
	}{
		{name: "valid enabled", mutate: func(*ATPC) {}},
		{name: "disabled ignores min/max ordering", mutate: func(a *ATPC) {
			a.Enabled = false
			a.MinPower, a.MaxPower = 0, 0
		}},
		{name: "enabled min equals max", mutate: func(a *ATPC) { a.MinPower, a.MaxPower = 10, 10 }, wantErr: true},
		{name: "enabled min above max", mutate: func(a *ATPC) { a.MinPower, a.MaxPower = 20, 10 }, wantErr: true},
		{name: "enabled current below min", mutate: func(a *ATPC) { a.CurrentPower = -11 }, wantErr: true},
		{name: "enabled current above max", mutate: func(a *ATPC) { a.CurrentPower = 31 }, wantErr: true},
		{name: "target rsl out of range", mutate: func(a *ATPC) { a.TargetRSL = -100 }, wantErr: true},
		{name: "range zero", mutate: func(a *ATPC) { a.Range = 0 }, wantErr: true},
		{name: "range negative", mutate: func(a *ATPC) { a.Range = -1 }, wantErr: true},
		{name: "min power out of range", mutate: func(a *ATPC) { a.MinPower = -11 }, wantErr: true},
		{name: "max power out of range", mutate: func(a *ATPC) { a.MaxPower = 31 }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			atpc := validATPC()
			tt.mutate(&atpc)

			err := atpc.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestACMValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ACM)
		wantErr bool
	}{
		{name: "adaptive", mutate: func(*ACM) {}},
		{name: "fixed", mutate: func(a *ACM) { a.Mode = ACMModeFixed }},
		{name: "unknown mode", mutate: func(a *ACM) { a.Mode = "auto" }, wantErr: true},
		{name: "empty mode", mutate: func(a *ACM) { a.Mode = "" }, wantErr: true},
		{name: "min profile at lower bound", mutate: func(a *ACM) { a.MinProfile, a.MaxProfile = MinACMProfile, MinACMProfile }},
		{name: "max profile at upper bound", mutate: func(a *ACM) { a.MinProfile, a.MaxProfile = MaxACMProfile, MaxACMProfile }},
		{name: "min profile below range", mutate: func(a *ACM) { a.MinProfile = 0 }, wantErr: true},
		{name: "max profile above range", mutate: func(a *ACM) { a.MaxProfile = 13 }, wantErr: true},
		{name: "min profile above max profile", mutate: func(a *ACM) { a.MinProfile, a.MaxProfile = 8, 1 }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acm := validACM()
			tt.mutate(&acm)

			err := acm.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestModProfileValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ModProfile)
		wantErr bool
	}{
		{name: "valid", mutate: func(*ModProfile) {}},
		{name: "id at lower bound", mutate: func(p *ModProfile) { p.ID = MinACMProfile }},
		{name: "id at upper bound", mutate: func(p *ModProfile) { p.ID = MaxACMProfile }},
		{name: "id zero", mutate: func(p *ModProfile) { p.ID = 0 }, wantErr: true},
		{name: "id above range", mutate: func(p *ModProfile) { p.ID = 13 }, wantErr: true},
		{name: "empty name", mutate: func(p *ModProfile) { p.Name = "" }, wantErr: true},
		{name: "empty modulation", mutate: func(p *ModProfile) { p.Modulation = "" }, wantErr: true},
		{name: "empty coding rate", mutate: func(p *ModProfile) { p.CodingRate = "" }, wantErr: true},
		{name: "spectral efficiency zero", mutate: func(p *ModProfile) { p.SpectralEfficiency = 0 }, wantErr: true},
		{name: "spectral efficiency negative", mutate: func(p *ModProfile) { p.SpectralEfficiency = -1 }, wantErr: true},
		{name: "rsl threshold out of range", mutate: func(p *ModProfile) { p.RSLThreshold = -99.1 }, wantErr: true},
		{name: "capacity zero", mutate: func(p *ModProfile) { p.Capacity = 0 }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := validModProfile(1)
			tt.mutate(&profile)

			err := profile.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
