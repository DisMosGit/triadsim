package router

import "strings"

// Module is the YANG module a model path belongs to: Name is the module prefix
// RESTCONF uses (sim-device) and Namespace is the XML namespace NETCONF
// declares (urn:sim:device).
type Module struct {
	Name      string
	Namespace string
}

// Modules of the simulated device. The XML namespaces are the ones the NETCONF
// <hello> and data tree already used before RESTCONF existed.
var (
	// ModuleDevice holds the device identity and the interface list.
	ModuleDevice = Module{Name: "sim-device", Namespace: "urn:sim:device"}
	// ModuleRadioLink holds the radio-link configuration.
	ModuleRadioLink = Module{Name: "sim-radio-link", Namespace: "urn:sim:radio-link"}
	// ModuleL2 holds the L2 switching objects (VLANs, MAC table, STP, LLDP).
	ModuleL2 = Module{Name: "sim-l2-switching", Namespace: "urn:sim:l2-switching"}
	// ModuleSync holds the synchronization objects (Phase 5).
	ModuleSync = Module{Name: "sim-sync", Namespace: "urn:sim:sync"}
)

// ModuleFor returns the module of a model path by its first matching container
// segment, so both planes can qualify the same node consistently. A path no
// module claims belongs to the device module.
func ModuleFor(path string) Module {
	for _, segment := range strings.Split(path, "/") {
		if index := strings.IndexByte(segment, '['); index >= 0 {
			segment = segment[:index]
		}
		switch segment {
		case "radio-link", "modulation-profile":
			return ModuleRadioLink
		case "vlans", "mac-table", "stp", "lldp":
			return ModuleL2
		case "ptp", "synce":
			return ModuleSync
		}
	}
	return ModuleDevice
}
