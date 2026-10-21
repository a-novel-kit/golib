package grpcf

import (
	"encoding/json"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// MarshalJSONAsAny encodes v as JSON and packs the bytes into a google.protobuf.Any holding a
// google.protobuf.BytesValue. Use it where an RPC field typed as Any carries an opaque JSON payload.
// [UnmarshalJSONFromAny] reverses it.
func MarshalJSONAsAny(v any) (*anypb.Any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	out := &anypb.Any{}

	return out, anypb.MarshalFrom(out, &wrapperspb.BytesValue{Value: data}, proto.MarshalOptions{})
}

// UnmarshalJSONFromAny reverses [MarshalJSONAsAny]. It expects anyValue to hold a
// google.protobuf.BytesValue, and decodes the JSON payload of that value into a Go value.
func UnmarshalJSONFromAny(anyValue *anypb.Any) (any, error) {
	bytesValue := &wrapperspb.BytesValue{}

	err := anypb.UnmarshalTo(anyValue, bytesValue, proto.UnmarshalOptions{})
	if err != nil {
		return nil, err
	}

	var value any

	err = json.Unmarshal(bytesValue.Value, &value)
	if err != nil {
		return nil, err
	}

	return value, nil
}
