package grpcf

import (
	"encoding/json"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// MarshalJSONAsAny serialises an arbitrary Go value to JSON and packs the
// resulting bytes into a google.protobuf.Any whose contained message type is
// google.protobuf.BytesValue. Use it only when an RPC field is typed as Any
// and you actually want to carry an opaque JSON payload — Any is normally
// reserved for genuine protobuf messages, so this is a deliberate escape
// hatch, not the default way to transit data.
//
// The inverse is UnmarshalJSONFromAny.
func MarshalJSONAsAny(v any) (*anypb.Any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	out := &anypb.Any{}

	return out, anypb.MarshalFrom(out, &wrapperspb.BytesValue{Value: data}, proto.MarshalOptions{})
}

// UnmarshalJSONFromAny is the inverse of MarshalJSONAsAny: it extracts the
// JSON payload from a google.protobuf.Any (assumed to wrap a
// google.protobuf.BytesValue) and decodes it into an `any` Go value.
func UnmarshalJSONFromAny(anyValue *anypb.Any) (any, error) {
	bytesValue := &wrapperspb.BytesValue{}

	err := anypb.UnmarshalTo(anyValue, bytesValue, proto.UnmarshalOptions{})
	if err != nil {
		return nil, err
	}

	var value any
	if err := json.Unmarshal(bytesValue.Value, &value); err != nil {
		return nil, err
	}

	return value, nil
}

// InterfaceToProtoAny serialises an arbitrary Go value to JSON, packs the
// bytes into a google.protobuf.Any wrapping a google.protobuf.BytesValue,
// and returns it.
//
// Deprecated: use MarshalJSONAsAny — same behaviour, but the name surfaces
// the JSON-over-Any nature of the helper rather than implying a generic
// "interface to Any" conversion.
func InterfaceToProtoAny(v any) (*anypb.Any, error) {
	return MarshalJSONAsAny(v)
}

// ProtoAnyToInterface is the inverse of InterfaceToProtoAny.
//
// Deprecated: use UnmarshalJSONFromAny.
func ProtoAnyToInterface(anyValue *anypb.Any) (any, error) {
	return UnmarshalJSONFromAny(anyValue)
}
