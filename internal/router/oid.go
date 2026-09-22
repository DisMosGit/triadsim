package router

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/DisMosGit/triadsim/internal/model"
)

// EnterpriseOID is the vendor subtree of the simulator (placeholder number).
const EnterpriseOID = "1.3.6.1.4.1.99999"

// SNMPType is the ASN.1 type used when a model leaf is exposed over SNMP.
type SNMPType string

// SNMP types used by the OID tables.
const (
	TypeInteger      SNMPType = "Integer"
	TypeOctetString  SNMPType = "OctetString"
	TypeObjectID     SNMPType = "ObjectIdentifier"
	TypeGauge32      SNMPType = "Gauge32"
	TypeCounter32    SNMPType = "Counter32"
	TypeCounter64    SNMPType = "Counter64"
	TypeTimeTicks    SNMPType = "TimeTicks"
	TypeOpaqueDouble SNMPType = "OpaqueDouble"
)

// scope says how an OID rule is applied to the model tree.
type scope uint8

const (
	// scopeScalar exposes one fixed model path as a scalar object (OID + ".0").
	scopeScalar scope = iota
	// scopeInterface exposes a leaf of every interface as a table column whose
	// instance is the 1-based interface index.
	scopeInterface
	// scopeRadio exposes a leaf of the first radio link as a scalar object.
	scopeRadio
	// scopeSTPPort exposes a leaf of every bridge port as a table column whose
	// instance is the bridge port number, the interface index of the port.
	scopeSTPPort
	// scopeMAC exposes a leaf of every forwarding-database entry as a table
	// column whose instance is the 6-octet MAC address.
	scopeMAC
	// scopeVLAN exposes a leaf of every VLAN as a table column whose instance is
	// the VLAN identifier.
	scopeVLAN
	// scopeSyncE exposes a leaf of every SyncE-capable interface as a table
	// column whose instance is the interface index of the port.
	scopeSyncE
)

// objectRule maps a model path to one SNMP object.
type objectRule struct {
	scope    scope
	suffix   string // scalar: full path; interface: relative to the interface; radio: relative to radio-link
	oid      string // base OID, without the instance suffix
	typ      SNMPType
	writable bool
	convert  func(any) any          // nil means the value is exposed unchanged
	decode   func(any) (any, error) // nil means an incoming value is used unchanged
}

// objectRules is the static OID table. It is applied to the model template, so
// interface names, indexes and the presence of a radio link come from the
// model rather than from hardcoded paths.
//
// The interface columns follow MIB-II ifTable and the 64-bit ifXTable counters;
// the bridge columns follow BRIDGE-MIB (dot1dStpPortTable, dot1dTpFdbTable,
// dot1dTpAgingTime) and the Q-BRIDGE-MIB VLAN name table.
var objectRules = []objectRule{
	{scope: scopeScalar, suffix: "system-info/description", oid: "1.3.6.1.2.1.1.1", typ: TypeOctetString},
	{scope: scopeScalar, suffix: "system-info/uptime", oid: "1.3.6.1.2.1.1.3", typ: TypeTimeTicks, convert: ticksFromSeconds},
	{scope: scopeScalar, suffix: "system-info/contact", oid: "1.3.6.1.2.1.1.4", typ: TypeOctetString, writable: true},
	{scope: scopeScalar, suffix: "system-info/name", oid: "1.3.6.1.2.1.1.5", typ: TypeOctetString, writable: true},
	{scope: scopeScalar, suffix: "system-info/location", oid: "1.3.6.1.2.1.1.6", typ: TypeOctetString, writable: true},

	// MIB-II ifTable.
	{scope: scopeInterface, suffix: "name", oid: "1.3.6.1.2.1.2.2.1.2", typ: TypeOctetString},
	{scope: scopeInterface, suffix: "type", oid: "1.3.6.1.2.1.2.2.1.3", typ: TypeInteger, convert: ifTypeValue},
	{scope: scopeInterface, suffix: "mtu", oid: "1.3.6.1.2.1.2.2.1.4", typ: TypeInteger},
	{scope: scopeInterface, suffix: "mac-address", oid: "1.3.6.1.2.1.2.2.1.6", typ: TypeOctetString, convert: macOctets},
	{scope: scopeInterface, suffix: "enabled", oid: "1.3.6.1.2.1.2.2.1.7", typ: TypeInteger,
		convert: ifOperStatusValue, decode: truthFromInt, writable: true},
	{scope: scopeInterface, suffix: "enabled", oid: "1.3.6.1.2.1.2.2.1.8", typ: TypeInteger, convert: ifOperStatusValue},
	{scope: scopeInterface, suffix: "counters/in-octets", oid: "1.3.6.1.2.1.2.2.1.10", typ: TypeCounter32},
	{scope: scopeInterface, suffix: "counters/in-ucast-pkts", oid: "1.3.6.1.2.1.2.2.1.11", typ: TypeCounter32},
	{scope: scopeInterface, suffix: "counters/in-discards", oid: "1.3.6.1.2.1.2.2.1.13", typ: TypeCounter32},
	{scope: scopeInterface, suffix: "counters/in-errors", oid: "1.3.6.1.2.1.2.2.1.14", typ: TypeCounter32},
	{scope: scopeInterface, suffix: "counters/out-octets", oid: "1.3.6.1.2.1.2.2.1.16", typ: TypeCounter32},
	{scope: scopeInterface, suffix: "counters/out-ucast-pkts", oid: "1.3.6.1.2.1.2.2.1.17", typ: TypeCounter32},
	{scope: scopeInterface, suffix: "counters/out-discards", oid: "1.3.6.1.2.1.2.2.1.19", typ: TypeCounter32},
	{scope: scopeInterface, suffix: "counters/out-errors", oid: "1.3.6.1.2.1.2.2.1.20", typ: TypeCounter32},
	{scope: scopeInterface, suffix: "name", oid: "1.3.6.1.2.1.31.1.1.1.1", typ: TypeOctetString},
	{scope: scopeInterface, suffix: "counters/in-octets", oid: "1.3.6.1.2.1.31.1.1.1.6", typ: TypeCounter64},
	{scope: scopeInterface, suffix: "counters/in-ucast-pkts", oid: "1.3.6.1.2.1.31.1.1.1.7", typ: TypeCounter64},
	{scope: scopeInterface, suffix: "counters/out-octets", oid: "1.3.6.1.2.1.31.1.1.1.10", typ: TypeCounter64},
	{scope: scopeInterface, suffix: "counters/out-ucast-pkts", oid: "1.3.6.1.2.1.31.1.1.1.11", typ: TypeCounter64},

	// BRIDGE-MIB: the bridge identity, the spanning-tree scalars and the
	// forwarding database.
	{scope: scopeScalar, suffix: "stp/state/bridge-address", oid: "1.3.6.1.2.1.17.1.1", typ: TypeOctetString, convert: macOctets},
	{scope: scopeScalar, suffix: "stp/state/bridge-priority", oid: "1.3.6.1.2.1.17.2.2", typ: TypeInteger, writable: true},
	{scope: scopeSTPPort, suffix: "priority", oid: "1.3.6.1.2.1.17.2.15.1.2", typ: TypeInteger, writable: true},
	{scope: scopeSTPPort, suffix: "state", oid: "1.3.6.1.2.1.17.2.15.1.3", typ: TypeInteger, convert: stpPortStateValue},
	{scope: scopeSTPPort, suffix: "state", oid: "1.3.6.1.2.1.17.2.15.1.4", typ: TypeInteger, convert: stpPortEnableValue},
	{scope: scopeSTPPort, suffix: "path-cost", oid: "1.3.6.1.2.1.17.2.15.1.5", typ: TypeInteger, writable: true},
	{scope: scopeScalar, suffix: "mac-table/aging-time", oid: "1.3.6.1.2.1.17.4.2", typ: TypeInteger, writable: true},
	{scope: scopeMAC, suffix: "mac-address", oid: "1.3.6.1.2.1.17.4.3.1.1", typ: TypeOctetString, convert: macOctets},
	{scope: scopeMAC, suffix: "port", oid: "1.3.6.1.2.1.17.4.3.1.2", typ: TypeInteger},
	{scope: scopeMAC, suffix: "type", oid: "1.3.6.1.2.1.17.4.3.1.3", typ: TypeInteger, convert: fdbStatusValue},

	// Q-BRIDGE-MIB: the static VLAN name table.
	{scope: scopeVLAN, suffix: "name", oid: "1.3.6.1.2.1.17.7.1.4.3.1.1", typ: TypeOctetString},

	{scope: scopeRadio, suffix: "rssi", oid: EnterpriseOID + ".1.1.1", typ: TypeOpaqueDouble},
	{scope: scopeRadio, suffix: "fade-margin", oid: EnterpriseOID + ".1.1.2", typ: TypeOpaqueDouble},
	{scope: scopeRadio, suffix: "capacity", oid: EnterpriseOID + ".1.1.3", typ: TypeGauge32},
	{scope: scopeRadio, suffix: "tx-power", oid: EnterpriseOID + ".1.1.4", typ: TypeOpaqueDouble, writable: true},
	{scope: scopeRadio, suffix: "atpc/enabled", oid: EnterpriseOID + ".1.1.5", typ: TypeInteger, convert: truthValue, decode: truthFromInt, writable: true},
	{scope: scopeRadio, suffix: "acm/current-profile", oid: EnterpriseOID + ".1.1.6", typ: TypeInteger},
	{scope: scopeRadio, suffix: "acm/current-capacity", oid: EnterpriseOID + ".1.1.7", typ: TypeGauge32},
	{scope: scopeRadio, suffix: "link-state", oid: EnterpriseOID + ".1.1.8", typ: TypeInteger, convert: linkStateValue},

	// Vendor synchronization objects. The clock is a single object (scalar
	// ".0"); the SyncE interface table is indexed by the interface index, the
	// same instance the interface MIB tables use.
	{scope: scopeScalar, suffix: "ptp/clock/state", oid: EnterpriseOID + ".2.1.1", typ: TypeInteger, convert: ptpStateValue},
	{scope: scopeScalar, suffix: "ptp/clock/offset", oid: EnterpriseOID + ".2.1.2", typ: TypeOpaqueDouble},
	{scope: scopeScalar, suffix: "ptp/clock/domain", oid: EnterpriseOID + ".2.1.3", typ: TypeGauge32, writable: true},
	{scope: scopeScalar, suffix: "ptp/clock/priority1", oid: EnterpriseOID + ".2.1.4", typ: TypeGauge32, writable: true},
	{scope: scopeScalar, suffix: "synce/selected-ql", oid: EnterpriseOID + ".2.1.5", typ: TypeInteger, convert: qlValue},
	{scope: scopeScalar, suffix: "synce/selected-extended-ql", oid: EnterpriseOID + ".2.1.6", typ: TypeInteger, convert: extendedQLValue},
	{scope: scopeSyncE, suffix: "ql", oid: EnterpriseOID + ".2.1.7.1.1", typ: TypeInteger, convert: qlValue},
	{scope: scopeSyncE, suffix: "ssm-enabled", oid: EnterpriseOID + ".2.1.7.1.2", typ: TypeInteger, convert: truthValue},
	{scope: scopeScalar, suffix: "ptp/clock/jitter", oid: EnterpriseOID + ".2.1.8", typ: TypeOpaqueDouble},
}

// Constant objects that have no model leaf.
const (
	// sysObjectIDOID identifies the simulated device vendor subtree.
	sysObjectIDOID = "1.3.6.1.2.1.1.2.0"
	// ifIndexOID is the ifTable index column.
	ifIndexOID = "1.3.6.1.2.1.2.2.1.1"
)

// ifTypeValue maps a model interface type to the IANA ifType value; radio is
// reported as other(1) and ethernet as ethernetCsmacd(6).
func ifTypeValue(v any) any {
	if s, ok := v.(string); ok && s == "radio" {
		return 1
	}
	return 6
}

// ifOperStatusValue maps the enabled flag to ifOperStatus up(1)/down(2).
func ifOperStatusValue(v any) any {
	if b, ok := v.(bool); ok && b {
		return 1
	}
	return 2
}

// truthValue maps a bool to the SMI TruthValue true(1)/false(2).
func truthValue(v any) any {
	if b, ok := v.(bool); ok && b {
		return 1
	}
	return 2
}

// truthFromInt is the inverse of truthValue for a writable TruthValue object.
func truthFromInt(v any) (any, error) {
	switch n := v.(type) {
	case bool:
		return n, nil
	case int:
		switch n {
		case 1:
			return true, nil
		case 2:
			return false, nil
		}
	case uint32:
		switch n {
		case 1:
			return true, nil
		case 2:
			return false, nil
		}
	}
	return nil, fmt.Errorf("expected TruthValue 1 or 2, got %v", v)
}

// ticksFromSeconds converts the seconds-valued uptime leaf to TimeTicks
// (hundredths of a second).
func ticksFromSeconds(v any) any {
	if n, ok := v.(uint32); ok {
		return n * 100
	}
	return v
}

// macOctets converts a MAC address to the six octets an OctetString object
// carries. A value that does not parse is passed through unchanged.
func macOctets(v any) any {
	text, ok := v.(string)
	if !ok {
		return v
	}
	address, err := net.ParseMAC(text)
	if err != nil || len(address) != 6 {
		return v
	}
	return []byte(address)
}

// macInstance renders a MAC address as the six-sub-identifier instance of a
// forwarding-database entry.
func macInstance(text string) string {
	address, err := net.ParseMAC(text)
	if err != nil || len(address) != 6 {
		return ""
	}
	parts := make([]string, 0, len(address))
	for _, octet := range address {
		parts = append(parts, strconv.Itoa(int(octet)))
	}
	return strings.Join(parts, ".")
}

// stpPortStateValue maps a port state onto dot1dStpPortState. The RSTP
// discarding state is reported as blocking(2).
func stpPortStateValue(v any) any {
	name, ok := v.(string)
	if !ok {
		return v
	}
	switch name {
	case "disabled":
		return 1
	case "blocking", "discarding":
		return 2
	case "listening":
		return 3
	case "learning":
		return 4
	case "forwarding":
		return 5
	default:
		return 1
	}
}

// stpPortEnableValue maps a port state onto dot1dStpPortEnable: a disabled
// port is disabled(2), every other state is enabled(1).
func stpPortEnableValue(v any) any {
	if name, ok := v.(string); ok && name == "disabled" {
		return 2
	}
	return 1
}

// fdbStatusValue maps a forwarding-database entry type onto dot1dTpFdbStatus:
// learned entries are learned(3), permanent ones are management(5).
func fdbStatusValue(v any) any {
	if typ, ok := v.(string); ok && typ == "static" {
		return 5
	}
	return 3
}

// ptpStateValue maps a G.8275.1 clock state onto the integer the vendor object
// reports: freerun(1), acquiring(2), locked(3), holdover-in-spec(4) and
// holdover-out-of-spec(5). An unknown state is reported as freerun(1).
func ptpStateValue(v any) any {
	name, ok := v.(string)
	if !ok {
		return v
	}
	switch name {
	case "freerun":
		return 1
	case "acquiring":
		return 2
	case "locked":
		return 3
	case "holdover-in-spec":
		return 4
	case "holdover-out-of-spec":
		return 5
	default:
		return 1
	}
}

// linkStateValue maps a radio link-state onto the integer the vendor object
// reports: up(1), degraded(2), down(3). An unknown state is reported as up(1).
func linkStateValue(v any) any {
	name, ok := v.(string)
	if !ok {
		return v
	}
	switch name {
	case "degraded":
		return 2
	case "down":
		return 3
	default:
		return 1
	}
}

// qlValue maps an ITU-T G.781 Option I quality level onto its 4-bit SSM code.
// An empty quality level, which means no source is selected, is reported as
// zero.
func qlValue(v any) any {
	name, ok := v.(string)
	if !ok {
		return v
	}
	switch name {
	case "QL-PRC":
		return 2
	case "QL-SSU-A":
		return 4
	case "QL-SSU-B":
		return 8
	case "QL-SEC":
		return 11
	case "QL-DNU":
		return 15
	default:
		return 0
	}
}

// extendedQLValue maps an extended (eSSM) quality level onto its code from
// G.8264 Amendment 2. An empty value is reported as zero.
func extendedQLValue(v any) any {
	name, ok := v.(string)
	if !ok {
		return v
	}
	switch name {
	case "QL-PRTC":
		return 0x20
	case "QL-ePRTC":
		return 0x21
	case "QL-eEEC":
		return 0x22
	default:
		return 0
	}
}

// CompareOID compares two OIDs component by component, numerically when a
// component is a number. It returns -1, 0 or 1, matching strings.Compare.
func CompareOID(a, b string) int {
	as := strings.Split(strings.TrimPrefix(a, "."), ".")
	bs := strings.Split(strings.TrimPrefix(b, "."), ".")

	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, aerr := strconv.Atoi(as[i])
		bi, berr := strconv.Atoi(bs[i])
		if aerr != nil || berr != nil {
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
			continue
		}
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}

	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	default:
		return 0
	}
}

// instanceOID appends a numeric instance to a base OID.
func instanceOID(base string, instance int) string {
	return base + "." + strconv.Itoa(instance)
}

// indexOID appends an already rendered instance, such as a MAC address in its
// sub-identifier form, to a base OID.
func indexOID(base, index string) string {
	return base + "." + index
}

// interfaceIndexOf returns the 1-based interface index of an interface name, or
// zero when the name is not an interface.
func interfaceIndexOf(root *model.Device, name string) int {
	for i := range root.Interfaces {
		if root.Interfaces[i].Name == name {
			return i + 1
		}
	}
	return 0
}
