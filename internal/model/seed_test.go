package model

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultDeviceValid(t *testing.T) {
	device := DefaultDevice()

	require.NoError(t, device.Validate())
}

func TestDefaultDeviceInterfaces(t *testing.T) {
	device := DefaultDevice()

	require.Len(t, device.Interfaces, 3)

	want := []struct {
		name     string
		ifType   string
		hasRadio bool
		macAddr  string
	}{
		{name: "radio0", ifType: InterfaceTypeRadio, hasRadio: true},
		{name: "eth0", ifType: InterfaceTypeEthernet, macAddr: "02:00:00:00:00:01"},
		{name: "eth1", ifType: InterfaceTypeEthernet, macAddr: "02:00:00:00:00:02"},
	}

	for i, wantInterface := range want {
		iface := device.Interfaces[i]
		assert.Equal(t, wantInterface.name, iface.Name)
		assert.Equal(t, wantInterface.ifType, iface.Type)
		assert.Equal(t, wantInterface.hasRadio, iface.RadioLink != nil)
		assert.Equal(t, wantInterface.macAddr, iface.MACAddress)
	}
}

func TestDefaultDeviceRadioLink(t *testing.T) {
	link := DefaultDevice().Interfaces[0].RadioLink
	require.NotNil(t, link)

	assert.Equal(t, "radio0", link.Name)
	assert.Equal(t, 20.0, link.TxPower)
	assert.Equal(t, -72.5, link.RSSI)
	assert.Equal(t, uint32(112), link.Capacity)
	assert.Equal(t, uint8(5), link.ACM.CurrentProfile)

	require.Len(t, link.Profiles, 12)
	seen := make(map[uint8]bool, len(link.Profiles))
	for i, profile := range link.Profiles {
		id := uint8(i + 1)
		assert.Equal(t, id, profile.ID)
		assert.Equal(t, "acm-"+strconv.Itoa(int(id)), profile.Name)
		seen[id] = true
	}
	assert.Len(t, seen, 12)
	assert.Equal(t, uint32(112), link.Profiles[4].Capacity)
	assert.Equal(t, "4096QAM", link.Profiles[11].Modulation)
}

func TestDefaultDeviceReturnsFreshCopy(t *testing.T) {
	first := DefaultDevice()
	first.Interfaces[0].RadioLink.TxPower = 1
	first.Interfaces[0].RadioLink.Profiles[0].Capacity = 999

	second := DefaultDevice()

	assert.Equal(t, 20.0, second.Interfaces[0].RadioLink.TxPower)
	assert.Equal(t, uint32(28), second.Interfaces[0].RadioLink.Profiles[0].Capacity)
}
