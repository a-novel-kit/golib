package grpcf

import (
	"encoding/json"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// MarshalJSONAsAny serializes an arbitrary Go value to JSON and packs the
// resulting bytes into a google.protobuf.Any whose contained message type is
// google.protobuf.BytesValue. Use it where an RPC field typed as Any must carry
// an opaque JSON payload; Any otherwise holds genuine protobuf messages.
//
// The inverse is [UnmarshalJSONFromAny].
func MarshalJSONAsAny(v any) (*anypb.Any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	out := &anypb.Any{}

	return out, anypb.MarshalFrom(out, &wrapperspb.BytesValue{Value: data}, proto.MarshalOptions{})
}

// UnmarshalJSONFromAny is the inverse of [MarshalJSONAsAny]: it extracts the
// JSON payload from a google.protobuf.Any (assumed to wrap a
// google.protobuf.BytesValue) and decodes it into an `any` Go value.
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
