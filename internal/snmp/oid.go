package snmp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gosnmp/gosnmp"

	"github.com/DisMosGit/triadsim/internal/router"
)

// pduIndex is a sorted view of the router's bindings, used to answer Get,
// GetNext and GetBulk requests.
type pduIndex struct {
	bindings []router.Binding
	byOID    map[string]router.Binding
}

// newPDUIndex sorts bindings by OID and indexes them for exact lookups.
func newPDUIndex(bindings []router.Binding) *pduIndex {
	sorted := make([]router.Binding, len(bindings))
	copy(sorted, bindings)
	sort.Slice(sorted, func(i, j int) bool {
		return router.CompareOID(sorted[i].OID, sorted[j].OID) < 0
	})

	byOID := make(map[string]router.Binding, len(sorted))
	for _, binding := range sorted {
		byOID[binding.OID] = binding
	}
	return &pduIndex{bindings: sorted, byOID: byOID}
}

// trimOID removes the leading dot gosnmp's decoder adds to OIDs, so request
// names compare equal to the router's table entries.
func trimOID(oid string) string {
	return strings.TrimPrefix(oid, ".")
}

// exact returns the binding for an OID.
func (x *pduIndex) exact(oid string) (router.Binding, bool) {
	binding, ok := x.byOID[trimOID(oid)]
	return binding, ok
}

// next returns the first binding whose OID sorts after oid.
func (x *pduIndex) next(oid string) (router.Binding, bool) {
	oid = trimOID(oid)
	i := sort.Search(len(x.bindings), func(i int) bool {
		return router.CompareOID(x.bindings[i].OID, oid) > 0
	})
	if i == len(x.bindings) {
		return router.Binding{}, false
	}
	return x.bindings[i], true
}

// asn1Type maps the router's SNMP type onto gosnmp.
func asn1Type(t router.SNMPType) gosnmp.Asn1BER {
	switch t {
	case router.TypeInteger:
		return gosnmp.Integer
	case router.TypeOctetString:
		return gosnmp.OctetString
	case router.TypeObjectID:
		return gosnmp.ObjectIdentifier
	case router.TypeGauge32:
		return gosnmp.Gauge32
	case router.TypeCounter32:
		return gosnmp.Counter32
	case router.TypeCounter64:
		return gosnmp.Counter64
	case router.TypeTimeTicks:
		return gosnmp.TimeTicks
	case router.TypeOpaqueDouble:
		return gosnmp.OpaqueDouble
	default:
		return gosnmp.Null
	}
}

// toPDU builds a varbind. gosnmp's marshaller expects a specific Go type per
// ASN.1 type, so the value is normalised here; this also keeps the router free
// of gosnmp's exact-type requirements.
func toPDU(binding router.Binding) gosnmp.SnmpPDU {
	pdu := gosnmp.SnmpPDU{Name: binding.OID, Type: asn1Type(binding.Type)}

	switch binding.Type {
	case router.TypeInteger:
		if value, ok := asInt(binding.Value); ok {
			pdu.Value = value
			return pdu
		}
	case router.TypeGauge32, router.TypeCounter32, router.TypeTimeTicks:
		if value, ok := asUint32(binding.Value); ok {
			pdu.Value = value
			return pdu
		}
	case router.TypeCounter64:
		if value, ok := asUint64(binding.Value); ok {
			pdu.Value = value
			return pdu
		}
	case router.TypeOctetString:
		if value, ok := binding.Value.([]byte); ok {
			pdu.Value = value
			return pdu
		}
		if value, ok := asString(binding.Value); ok {
			pdu.Value = value
			return pdu
		}
	case router.TypeObjectID:
		if value, ok := binding.Value.(string); ok {
			pdu.Value = value
			return pdu
		}
	case router.TypeOpaqueDouble:
		if value, ok := asFloat64(binding.Value); ok {
			pdu.Value = value
			return pdu
		}
	}

	pdu.Type = gosnmp.Null
	pdu.Value = nil
	return pdu
}

// exception builds a varbind for a missing object or the end of a walk.
func exception(oid string, typ gosnmp.Asn1BER) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: trimOID(oid), Type: typ, Value: nil}
}

// fromPDU converts a request varbind value into a Go value the router can
// coerce. gosnmp decodes OctetString as []byte and the integer types as int,
// uint32 or uint64.
func fromPDU(pdu gosnmp.SnmpPDU) (any, error) {
	switch value := pdu.Value.(type) {
	case nil:
		return nil, fmt.Errorf("snmp: varbind %s has no value", pdu.Name)
	case bool:
		return value, nil
	case int:
		return value, nil
	case int64:
		return value, nil
	case uint32:
		return value, nil
	case uint64:
		return value, nil
	case float32:
		return float64(value), nil
	case float64:
		return value, nil
	case string:
		return value, nil
	case []byte:
		return string(value), nil
	default:
		return nil, fmt.Errorf("snmp: varbind %s has unsupported type %T", pdu.Name, pdu.Value)
	}
}

func asInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint8:
		return int(v), true
	case uint16:
		return int(v), true
	case uint32:
		return int(v), true
	case uint64:
		return int(v), true
	default:
		return 0, false
	}
}

func asUint32(value any) (uint32, bool) {
	switch v := value.(type) {
	case uint8:
		return uint32(v), true
	case uint16:
		return uint32(v), true
	case uint32:
		return v, true
	case uint64:
		return uint32(v), true
	case int:
		return uint32(v), true
	default:
		return 0, false
	}
}

func asUint64(value any) (uint64, bool) {
	switch v := value.(type) {
	case uint8:
		return uint64(v), true
	case uint16:
		return uint64(v), true
	case uint32:
		return uint64(v), true
	case uint64:
		return v, true
	case int:
		return uint64(v), true
	case int64:
		return uint64(v), true
	default:
		return 0, false
	}
}

func asFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case int:
		return float64(v), true
	case uint32:
		return float64(v), true
	default:
		return 0, false
	}
}

func asString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	default:
		return "", false
	}
}
