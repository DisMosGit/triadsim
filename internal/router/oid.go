package router

import (
	"fmt"
	"strconv"
	"strings"
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
var objectRules = []objectRule{
	{scope: scopeScalar, suffix: "system-info/description", oid: "1.3.6.1.2.1.1.1", typ: TypeOctetString},
	{scope: scopeScalar, suffix: "system-info/uptime", oid: "1.3.6.1.2.1.1.3", typ: TypeTimeTicks, convert: ticksFromSeconds},
	{scope: scopeScalar, suffix: "system-info/contact", oid: "1.3.6.1.2.1.1.4", typ: TypeOctetString, writable: true},
	{scope: scopeScalar, suffix: "system-info/name", oid: "1.3.6.1.2.1.1.5", typ: TypeOctetString, writable: true},
	{scope: scopeScalar, suffix: "system-info/location", oid: "1.3.6.1.2.1.1.6", typ: TypeOctetString, writable: true},

	{scope: scopeInterface, suffix: "name", oid: "1.3.6.1.2.1.2.2.1.2", typ: TypeOctetString},
	{scope: scopeInterface, suffix: "type", oid: "1.3.6.1.2.1.2.2.1.3", typ: TypeInteger, convert: ifTypeValue},
	{scope: scopeInterface, suffix: "enabled", oid: "1.3.6.1.2.1.2.2.1.8", typ: TypeInteger, convert: ifOperStatusValue},

	{scope: scopeRadio, suffix: "rssi", oid: EnterpriseOID + ".1.1.1", typ: TypeOpaqueDouble},
	{scope: scopeRadio, suffix: "fade-margin", oid: EnterpriseOID + ".1.1.2", typ: TypeOpaqueDouble},
	{scope: scopeRadio, suffix: "capacity", oid: EnterpriseOID + ".1.1.3", typ: TypeGauge32},
	{scope: scopeRadio, suffix: "tx-power", oid: EnterpriseOID + ".1.1.4", typ: TypeOpaqueDouble, writable: true},
	{scope: scopeRadio, suffix: "atpc/enabled", oid: EnterpriseOID + ".1.1.5", typ: TypeInteger, convert: truthValue, decode: truthFromInt, writable: true},
	{scope: scopeRadio, suffix: "acm/current-profile", oid: EnterpriseOID + ".1.1.6", typ: TypeInteger},
	{scope: scopeRadio, suffix: "acm/current-capacity", oid: EnterpriseOID + ".1.1.7", typ: TypeGauge32},
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
