package ops

import "strings"

// Module namespaces of the simulated device. The XML data tree is emitted with
// one namespace declaration per module subtree; the router paths crossed by
// radio-link belong to the radio module, everything else to the device module.
const (
	// DeviceNamespace is the module that holds the device identity and the
	// interface list.
	DeviceNamespace = "urn:sim:device"
	// RadioNamespace is the module that holds the radio-link configuration.
	RadioNamespace = "urn:sim:radio-link"
)

// namespaceFor returns the module namespace of a model path.
func namespaceFor(path string) string {
	for _, segment := range strings.Split(path, "/") {
		if index := strings.IndexByte(segment, '['); index >= 0 {
			segment = segment[:index]
		}
		switch segment {
		case "radio-link", "modulation-profile":
			return RadioNamespace
		}
	}
	return DeviceNamespace
}
