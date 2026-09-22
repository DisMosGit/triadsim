package gnmi

import (
	"testing"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/router"
)

func TestContentFor(t *testing.T) {
	tests := map[gnmi.GetRequest_DataType]datatree.Content{
		gnmi.GetRequest_ALL:         datatree.ContentAll,
		gnmi.GetRequest_CONFIG:      datatree.ContentConfig,
		gnmi.GetRequest_STATE:       datatree.ContentNonConfig,
		gnmi.GetRequest_OPERATIONAL: datatree.ContentNonConfig,
	}

	for dataType, want := range tests {
		assert.Equal(t, want, contentFor(dataType), "data type %s", dataType)
	}
}

func TestEncodingFor(t *testing.T) {
	tests := map[gnmi.Encoding]codes.Code{
		gnmi.Encoding_JSON:      codes.OK,
		gnmi.Encoding_JSON_IETF: codes.OK,
		gnmi.Encoding_PROTO:     codes.OK,
		gnmi.Encoding_BYTES:     codes.InvalidArgument,
		gnmi.Encoding_ASCII:     codes.InvalidArgument,
	}

	for encoding, want := range tests {
		got, err := encodingFor(encoding)
		if want == codes.OK {
			require.NoError(t, err)
			assert.Equal(t, encoding, got)
			continue
		}
		require.Error(t, err)
		assert.Equal(t, want, status.Code(err))
	}
}

func TestTextOf(t *testing.T) {
	tests := []struct {
		name    string
		value   *gnmi.TypedValue
		want    string
		wantErr bool
	}{
		{name: "string", value: stringValue("VOICE"), want: "VOICE"},
		{name: "ascii", value: &gnmi.TypedValue{Value: &gnmi.TypedValue_AsciiVal{AsciiVal: "VOICE"}}, want: "VOICE"},
		{name: "int", value: &gnmi.TypedValue{Value: &gnmi.TypedValue_IntVal{IntVal: -7}}, want: "-7"},
		{name: "uint", value: uintValue(1500), want: "1500"},
		{name: "bool", value: &gnmi.TypedValue{Value: &gnmi.TypedValue_BoolVal{BoolVal: true}}, want: "true"},
		{name: "double", value: &gnmi.TypedValue{Value: &gnmi.TypedValue_DoubleVal{DoubleVal: 12.5}}, want: "12.5"},
		{name: "bytes", value: &gnmi.TypedValue{Value: &gnmi.TypedValue_BytesVal{BytesVal: []byte("x")}}, want: "x"},
		{
			name: "decimal",
			value: &gnmi.TypedValue{Value: &gnmi.TypedValue_DecimalVal{
				DecimalVal: &gnmi.Decimal64{Digits: 125, Precision: 2},
			}},
			want: "1.25",
		},
		{
			name: "leaflist with one element",
			value: &gnmi.TypedValue{Value: &gnmi.TypedValue_LeaflistVal{
				LeaflistVal: &gnmi.ScalarArray{Element: []*gnmi.TypedValue{stringValue("VOICE")}},
			}},
			want: "VOICE",
		},
		{
			name: "JSON string",
			value: &gnmi.TypedValue{Value: &gnmi.TypedValue_JsonIetfVal{
				JsonIetfVal: []byte(`"VOICE"`),
			}},
			want: "VOICE",
		},
		{
			name: "JSON number",
			value: &gnmi.TypedValue{Value: &gnmi.TypedValue_JsonVal{
				JsonVal: []byte(`1500`),
			}},
			want: "1500",
		},
		{
			name:    "JSON object",
			value:   &gnmi.TypedValue{Value: &gnmi.TypedValue_JsonIetfVal{JsonIetfVal: []byte(`{"a":1}`)}},
			wantErr: true,
		},
		{name: "no value", value: &gnmi.TypedValue{}, wantErr: true},
		{
			name: "leaflist with two elements",
			value: &gnmi.TypedValue{Value: &gnmi.TypedValue_LeaflistVal{
				LeaflistVal: &gnmi.ScalarArray{Element: []*gnmi.TypedValue{stringValue("a"), stringValue("b")}},
			}},
			wantErr: true,
		},
		{
			name: "decimal precision out of range",
			value: &gnmi.TypedValue{Value: &gnmi.TypedValue_DecimalVal{
				DecimalVal: &gnmi.Decimal64{Digits: 1, Precision: 19},
			}},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := textOf(test.value)
			if test.wantErr {
				require.Error(t, err)
				assert.Equal(t, codes.InvalidArgument, status.Code(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestTypedValue(t *testing.T) {
	stringLeaf := &datatree.Node{Path: "system-info/name", Kind: router.KindLeaf, Value: "triadsim-01"}
	uintLeaf := &datatree.Node{Path: "ptp/clock/domain", Kind: router.KindLeaf, Value: uint8(24)}
	floatLeaf := &datatree.Node{Path: "radio-link/rssi", Kind: router.KindLeaf, Value: -72.5}
	boolLeaf := &datatree.Node{Path: "stp/enabled", Kind: router.KindLeaf, Value: true}

	protoString, err := typedValue(stringLeaf, gnmi.Encoding_PROTO)
	require.NoError(t, err)
	assert.Equal(t, "triadsim-01", protoString.GetStringVal())

	protoUint, err := typedValue(uintLeaf, gnmi.Encoding_PROTO)
	require.NoError(t, err)
	assert.Equal(t, uint64(24), protoUint.GetUintVal())

	protoFloat, err := typedValue(floatLeaf, gnmi.Encoding_PROTO)
	require.NoError(t, err)
	assert.InDelta(t, -72.5, protoFloat.GetDoubleVal(), 0.001)

	protoBool, err := typedValue(boolLeaf, gnmi.Encoding_PROTO)
	require.NoError(t, err)
	assert.True(t, protoBool.GetBoolVal())

	jsonValue, err := typedValue(stringLeaf, gnmi.Encoding_JSON)
	require.NoError(t, err)
	assert.JSONEq(t, `"triadsim-01"`, string(jsonValue.GetJsonVal()))

	ietfValue, err := typedValue(uintLeaf, gnmi.Encoding_JSON_IETF)
	require.NoError(t, err)
	assert.JSONEq(t, `24`, string(ietfValue.GetJsonIetfVal()))
}

func TestCodeForTag(t *testing.T) {
	tests := map[string]codes.Code{
		datatree.TagDataMissing:           codes.NotFound,
		datatree.TagAccessDenied:          codes.PermissionDenied,
		datatree.TagDataExists:            codes.AlreadyExists,
		datatree.TagOperationNotSupported: codes.Unimplemented,
		datatree.TagInvalidValue:          codes.InvalidArgument,
		datatree.TagUnknownElement:        codes.InvalidArgument,
		datatree.TagMalformedMessage:      codes.InvalidArgument,
		"something-else":                  codes.Internal,
	}

	for tag, want := range tests {
		assert.Equal(t, want, codeForTag(tag), "tag %s", tag)
	}
}
