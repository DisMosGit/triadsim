package gnmi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/router"
)

// contentFor maps the gNMI requested data type onto the data-tree content
// selection. The model has no separate operational annotation, so STATE and
// OPERATIONAL both select the config:"false" leaves.
func contentFor(dataType gnmi.GetRequest_DataType) datatree.Content {
	switch dataType {
	case gnmi.GetRequest_CONFIG:
		return datatree.ContentConfig
	case gnmi.GetRequest_STATE, gnmi.GetRequest_OPERATIONAL:
		return datatree.ContentNonConfig
	default:
		return datatree.ContentAll
	}
}

// encodingFor accepts the encodings the server advertises and rejects the rest
// with InvalidArgument. The zero value is JSON, which is also gNMI's default.
func encodingFor(encoding gnmi.Encoding) (gnmi.Encoding, error) {
	switch encoding {
	case gnmi.Encoding_JSON:
		return gnmi.Encoding_JSON, nil
	case gnmi.Encoding_JSON_IETF:
		return gnmi.Encoding_JSON_IETF, nil
	case gnmi.Encoding_PROTO:
		return gnmi.Encoding_PROTO, nil
	default:
		return gnmi.Encoding_JSON, status.Errorf(codes.InvalidArgument,
			"unsupported encoding %s: the server supports JSON, JSON_IETF and PROTO", encoding)
	}
}

// typedValue renders one resolved leaf as a gNMI value.
//
// JSON and JSON_IETF carry the JSON encoding of the leaf value, which is what a
// gNMI client expects for a leaf update; PROTO uses the concrete scalar field
// of the leaf's Go type.
func typedValue(node *datatree.Node, encoding gnmi.Encoding) (*gnmi.TypedValue, error) {
	encoded, err := json.Marshal(node.Value)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode %s: %v", node.Path, err)
	}

	if encoding == gnmi.Encoding_JSON {
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_JsonVal{JsonVal: encoded}}, nil
	}
	if encoding == gnmi.Encoding_JSON_IETF {
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_JsonIetfVal{JsonIetfVal: encoded}}, nil
	}

	switch value := node.Value.(type) {
	case bool:
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_BoolVal{BoolVal: value}}, nil
	case int:
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_IntVal{IntVal: int64(value)}}, nil
	case int64:
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_IntVal{IntVal: value}}, nil
	case uint:
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: uint64(value)}}, nil
	case uint8:
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: uint64(value)}}, nil
	case uint16:
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: uint64(value)}}, nil
	case uint32:
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: uint64(value)}}, nil
	case uint64:
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: value}}, nil
	case float32:
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_DoubleVal{DoubleVal: float64(value)}}, nil
	case float64:
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_DoubleVal{DoubleVal: value}}, nil
	case string:
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_StringVal{StringVal: value}}, nil
	default:
		// A leaf type the switch does not know still travels as JSON.
		return &gnmi.TypedValue{Value: &gnmi.TypedValue_JsonIetfVal{JsonIetfVal: encoded}}, nil
	}
}

// textOf renders a Set value as the text a model leaf carries. Object and array
// values are rejected: a Set addresses one leaf per operation.
func textOf(value *gnmi.TypedValue) (string, error) {
	switch typed := value.GetValue().(type) {
	case *gnmi.TypedValue_StringVal:
		return typed.StringVal, nil
	case *gnmi.TypedValue_AsciiVal:
		return typed.AsciiVal, nil
	case *gnmi.TypedValue_IntVal:
		return strconv.FormatInt(typed.IntVal, 10), nil
	case *gnmi.TypedValue_UintVal:
		return strconv.FormatUint(typed.UintVal, 10), nil
	case *gnmi.TypedValue_BoolVal:
		return strconv.FormatBool(typed.BoolVal), nil
	case *gnmi.TypedValue_FloatVal:
		return strconv.FormatFloat(float64(typed.FloatVal), 'g', -1, 32), nil
	case *gnmi.TypedValue_DoubleVal:
		return strconv.FormatFloat(typed.DoubleVal, 'g', -1, 64), nil
	case *gnmi.TypedValue_DecimalVal:
		text, err := decimalText(typed.DecimalVal)
		if err != nil {
			return "", err
		}
		return text, nil
	case *gnmi.TypedValue_BytesVal:
		return string(typed.BytesVal), nil
	case *gnmi.TypedValue_LeaflistVal:
		return leaflistText(typed.LeaflistVal)
	case *gnmi.TypedValue_JsonVal:
		return jsonScalarText(typed.JsonVal)
	case *gnmi.TypedValue_JsonIetfVal:
		return jsonScalarText(typed.JsonIetfVal)
	case *gnmi.TypedValue_ProtoBytes:
		return string(typed.ProtoBytes), nil
	case nil:
		return "", status.Error(codes.InvalidArgument, "the operation carries no value")
	default:
		return "", status.Errorf(codes.InvalidArgument, "unsupported value type %T", typed)
	}
}

// decimalText renders a gNMI Decimal64 as its decimal text.
func decimalText(decimal *gnmi.Decimal64) (string, error) {
	if decimal == nil {
		return "", status.Error(codes.InvalidArgument, "the decimal value is empty")
	}
	if decimal.Precision > 18 {
		return "", status.Errorf(codes.InvalidArgument,
			"decimal precision %d is out of range", decimal.Precision)
	}
	scale := math.Pow10(int(decimal.Precision))
	return strconv.FormatFloat(float64(decimal.Digits)/scale, 'f', int(decimal.Precision), 64), nil
}

// leaflistText renders a leaflist value holding exactly one scalar.
func leaflistText(list *gnmi.ScalarArray) (string, error) {
	if list == nil || len(list.Element) != 1 {
		return "", status.Error(codes.InvalidArgument,
			"a model leaf takes one value, not a leaf-list")
	}
	return textOf(list.Element[0])
}

// jsonScalarText renders a JSON-encoded value that must be a scalar.
func jsonScalarText(data []byte) (string, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "", status.Error(codes.InvalidArgument, "the JSON value is empty")
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", status.Errorf(codes.InvalidArgument, "malformed JSON value: %v", err)
	}

	switch value := value.(type) {
	case string:
		return value, nil
	case json.Number:
		return value.String(), nil
	case bool:
		return strconv.FormatBool(value), nil
	case nil:
		return "", nil
	default:
		return "", status.Error(codes.InvalidArgument,
			"a JSON object or array cannot be assigned to one leaf; address each leaf with its own update")
	}
}

// statusError maps a router or data-tree error onto a gRPC status code.
func statusError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, err.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, err.Error())
	}

	var treeErr *datatree.Error
	if errors.As(err, &treeErr) {
		return status.Errorf(codeForTag(treeErr.Tag), "%s", treeErr.Error())
	}
	switch {
	case errors.Is(err, router.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, router.ErrInvalidPath):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, router.ErrReadOnly):
		return status.Error(codes.PermissionDenied, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// codeForTag maps an RFC 6241 error tag onto a gRPC status code.
func codeForTag(tag string) codes.Code {
	switch tag {
	case datatree.TagDataMissing:
		return codes.NotFound
	case datatree.TagAccessDenied:
		return codes.PermissionDenied
	case datatree.TagDataExists:
		return codes.AlreadyExists
	case datatree.TagOperationNotSupported:
		return codes.Unimplemented
	case datatree.TagMalformedMessage, datatree.TagTooBig, datatree.TagMissingElement,
		datatree.TagUnknownElement, datatree.TagInvalidValue, datatree.TagOperationFailed:
		return codes.InvalidArgument
	default:
		return codes.Internal
	}
}
