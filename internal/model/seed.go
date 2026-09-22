package model

import "strconv"

// DefaultDevice returns the boot configuration: one radio link and two
// Ethernet interfaces. It is the template the router indexes and the data the
// simulator seeds its store with on the first boot, so every value is valid.
//
// The modulation-profile table reproduces the ACM table of
// docs/protocols/RADIO-RRL.md §5.2.
func DefaultDevice() *Device {
	link := RadioLink{
		Name:       "radio0",
		TxPower:    20,
		RSSI:       -72.5,
		FadeMargin: 12.5,
		Capacity:   112,
		LinkBudget: LinkBudget{
			LinkLength:    12.5,
			Frequency:     18,
			TxAntennaGain: 38,
			RxAntennaGain: 38,
			FeedLoss:      2,
			CalculatedRSL: -72.5,
		},
		ATPC: ATPC{
			Enabled:      true,
			TargetRSL:    -45,
			MinPower:     -10,
			MaxPower:     30,
			Range:        20,
			CurrentPower: 18.5,
		},
		ACM: ACM{
			Enabled:         true,
			Mode:            ACMModeAdaptive,
			MinProfile:      MinACMProfile,
			MaxProfile:      MaxACMProfile,
			CurrentProfile:  5,
			CurrentCapacity: 112,
		},
		Profiles: defaultProfiles(),
	}

	return &Device{
		SystemInfo: SystemInfo{
			DeviceID:    "sim-001",
			Name:        "triadsim-01",
			Description: "TriadSim simulated telecom device",
			Contact:     "noc@example.net",
			Location:    "lab",
		},
		Interfaces: []Interface{
			{
				Name:      "radio0",
				Type:      InterfaceTypeRadio,
				Enabled:   true,
				MTU:       1500,
				RadioLink: &link,
			},
			{
				Name:       "eth0",
				Type:       InterfaceTypeEthernet,
				Enabled:    true,
				MTU:        1500,
				MACAddress: "02:00:00:00:00:01",
			},
			{
				Name:       "eth1",
				Type:       InterfaceTypeEthernet,
				Enabled:    true,
				MTU:        1500,
				MACAddress: "02:00:00:00:00:02",
			},
		},
	}
}

// defaultProfiles builds the ACM profile table from RADIO-RRL.md §5.2.
func defaultProfiles() []ModProfile {
	rows := []struct {
		modulation string
		codingRate string
		efficiency float64
		threshold  float64
		capacity   uint32
	}{
		{"QPSK", "1/2", 1.0, -88, 28},
		{"QPSK", "3/4", 1.5, -86, 42},
		{"8PSK", "2/3", 2.0, -83, 56},
		{"16QAM", "3/4", 3.0, -79, 84},
		{"32QAM", "4/5", 4.0, -76, 112},
		{"64QAM", "4/5", 5.0, -73, 140},
		{"128QAM", "5/6", 6.0, -70, 168},
		{"256QAM", "5/6", 7.0, -67, 196},
		{"512QAM", "7/8", 8.0, -64, 224},
		{"1024QAM", "7/8", 9.0, -61, 252},
		{"2048QAM", "8/9", 10.0, -58, 280},
		{"4096QAM", "9/10", 11.0, -55, 308},
	}

	profiles := make([]ModProfile, 0, len(rows))
	for i, row := range rows {
		id := uint8(i + 1)
		profiles = append(profiles, ModProfile{
			ID:                 id,
			Name:               "acm-" + strconv.Itoa(int(id)),
			Modulation:         row.modulation,
			CodingRate:         row.codingRate,
			SpectralEfficiency: row.efficiency,
			RSLThreshold:       row.threshold,
			Capacity:           row.capacity,
		})
	}
	return profiles
}
