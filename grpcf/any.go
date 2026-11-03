package grpcf

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// MarshalJSONAsAny encodes v as JSON and packs the bytes into a google.protobuf.Any holding a
// google.protobuf.BytesValue. Use it where an RPC field typed as Any carries an opaque JSON payload.
// [UnmarshalJSONFromAny] reverses it.
func MarshalJSONAsAny(v any) (*anypb.Any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal JSON: %w", err)
	}

	out, err := anypb.New(wrapperspb.Bytes(data))
	if err != nil {
		return nil, fmt.Errorf("pack JSON into Any: %w", err)
	}

	return out, nil
}

// UnmarshalJSONFromAny reverses [MarshalJSONAsAny]. It expects anyValue to hold a
// google.protobuf.BytesValue, and decodes the JSON payload of that value into a Go value.
func UnmarshalJSONFromAny(anyValue *anypb.Any) (any, error) {
	bytesValue := &wrapperspb.BytesValue{}

	err := anyValue.UnmarshalTo(bytesValue)
	if err != nil {
		return nil, fmt.Errorf("unpack JSON from Any: %w", err)
	}

	var value any

	err = json.Unmarshal(bytesValue.GetValue(), &value)
	if err != nil {
		return nil, fmt.Errorf("unmarshal JSON: %w", err)
	}

	return value, nil
}
