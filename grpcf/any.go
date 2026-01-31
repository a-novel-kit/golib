package grpcf

import (
	"encoding/json"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Found here: https://ravina01997.medium.com/converting-interface-to-any-proto-and-vice-versa-in-golang-27badc3e23f1

func InterfaceToProtoAny(v any) (*anypb.Any, error) {
	anyValue := &anypb.Any{}

	bytes, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	bytesValue := &wrapperspb.BytesValue{Value: bytes}

	return anyValue, anypb.MarshalFrom(anyValue, bytesValue, proto.MarshalOptions{})
}

func ProtoAnyToInterface(anyValue *anypb.Any) (any, error) {
	var value any

	bytesValue := &wrapperspb.BytesValue{}

	err := anypb.UnmarshalTo(anyValue, bytesValue, proto.UnmarshalOptions{})
	if err != nil {
		return value, err
	}

	uErr := json.Unmarshal(bytesValue.Value, &value)
	if uErr != nil {
		return value, uErr
	}

	return value, nil
}
