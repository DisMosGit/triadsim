package snmp

import (
	"testing"

	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/router"
)

func TestToPDUNormalizesValueTypes(t *testing.T) {
	tests := []struct {
		name      string
		binding   router.Binding
		wantType  gosnmp.Asn1BER
		wantValue any
	}{
		{
			name:      "uint8 integer becomes int",
			binding:   router.Binding{OID: "1.2.3", Type: router.TypeInteger, Value: uint8(7)},
			wantType:  gosnmp.Integer,
			wantValue: 7,
		},
		{
			name:      "int integer stays int",
			binding:   router.Binding{OID: "1.2.3", Type: router.TypeInteger, Value: 1},
			wantType:  gosnmp.Integer,
			wantValue: 1,
		},
		{
			name:      "uint32 gauge",
			binding:   router.Binding{OID: "1.2.3", Type: router.TypeGauge32, Value: uint32(112)},
			wantType:  gosnmp.Gauge32,
			wantValue: uint32(112),
		},
		{
			name:      "uint16 time ticks",
			binding:   router.Binding{OID: "1.2.3", Type: router.TypeTimeTicks, Value: uint16(100)},
			wantType:  gosnmp.TimeTicks,
			wantValue: uint32(100),
		},
		{
			name:      "string octet string",
			binding:   router.Binding{OID: "1.2.3", Type: router.TypeOctetString, Value: "radio0"},
			wantType:  gosnmp.OctetString,
			wantValue: "radio0",
		},
		{
			name:      "string object id",
			binding:   router.Binding{OID: "1.2.3", Type: router.TypeObjectID, Value: "1.3.6.1.4.1.99999.1"},
			wantType:  gosnmp.ObjectIdentifier,
			wantValue: "1.3.6.1.4.1.99999.1",
		},
		{
			name:      "float64 opaque double",
			binding:   router.Binding{OID: "1.2.3", Type: router.TypeOpaqueDouble, Value: -72.5},
			wantType:  gosnmp.OpaqueDouble,
			wantValue: -72.5,
		},
		{
			name:     "unrepresentable value becomes null",
			binding:  router.Binding{OID: "1.2.3", Type: router.TypeInteger, Value: true},
			wantType: gosnmp.Null,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdu := toPDU(tt.binding)

			assert.Equal(t, tt.binding.OID, pdu.Name)
			assert.Equal(t, tt.wantType, pdu.Type)
			if tt.wantType == gosnmp.Null {
				assert.Nil(t, pdu.Value)
				return
			}
			assert.Equal(t, tt.wantValue, pdu.Value)
		})
	}
}

func TestFromPDU(t *testing.T) {
	tests := []struct {
		name    string
		pdu     gosnmp.SnmpPDU
		want    any
		wantErr bool
	}{
		{name: "int", pdu: gosnmp.SnmpPDU{Type: gosnmp.Integer, Value: 7}, want: 7},
		{name: "uint32", pdu: gosnmp.SnmpPDU{Type: gosnmp.Gauge32, Value: uint32(112)}, want: uint32(112)},
		{name: "uint64", pdu: gosnmp.SnmpPDU{Type: gosnmp.Counter64, Value: uint64(1) << 40}, want: uint64(1) << 40},
		{name: "bytes become string", pdu: gosnmp.SnmpPDU{Type: gosnmp.OctetString, Value: []byte("radio0")}, want: "radio0"},
		{name: "string", pdu: gosnmp.SnmpPDU{Type: gosnmp.OctetString, Value: "radio0"}, want: "radio0"},
		{name: "float32 widens", pdu: gosnmp.SnmpPDU{Type: gosnmp.OpaqueFloat, Value: float32(1.5)}, want: 1.5},
		{name: "float64", pdu: gosnmp.SnmpPDU{Type: gosnmp.OpaqueDouble, Value: 1.5}, want: 1.5},
		{name: "nil is an error", pdu: gosnmp.SnmpPDU{Type: gosnmp.Null, Value: nil}, wantErr: true},
		{name: "unsupported is an error", pdu: gosnmp.SnmpPDU{Type: gosnmp.IPAddress, Value: struct{}{}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fromPDU(tt.pdu)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
