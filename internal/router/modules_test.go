package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModuleFor(t *testing.T) {
	tests := []struct {
		name string
		path string
		want Module
	}{
		{name: "system info", path: "system-info/device-id", want: ModuleDevice},
		{name: "interface", path: "interfaces/interface[name=radio0]/mtu", want: ModuleDevice},
		{name: "radio link", path: "interfaces/interface[name=radio0]/radio-link/tx-power", want: ModuleRadioLink},
		{name: "modulation profile", path: "interfaces/interface[name=radio0]/radio-link/modulation-profile[id=5]/capacity", want: ModuleRadioLink},
		{name: "vlan", path: "vlans/vlan[id=100]/name", want: ModuleL2},
		{name: "vlan port", path: "vlans/vlan[id=100]/ports/port[port=eth0]/qinq", want: ModuleL2},
		{name: "mac table", path: "mac-table/entry[mac-address=02:00:00:00:00:02]/port", want: ModuleL2},
		{name: "stp", path: "stp/state/bridge-priority", want: ModuleL2},
		{name: "lldp", path: "lldp/neighbors/neighbor[port=eth0]/ttl", want: ModuleL2},
		{name: "sync", path: "ptp/clock/state", want: ModuleSync},
		{name: "root", path: "", want: ModuleDevice},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ModuleFor(tt.path))
		})
	}
}

func TestModulesHaveNameAndNamespace(t *testing.T) {
	for _, module := range []Module{ModuleDevice, ModuleRadioLink, ModuleL2, ModuleSync} {
		assert.NotEmpty(t, module.Name)
		assert.NotEmpty(t, module.Namespace)
	}
}
