package grpcf_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/grpcf"
)

func TestMarshalJSONAsAny_roundtrip(t *testing.T) {
	t.Parallel()

	original := map[string]any{
		"message": "hello world",
	}

	anyValue, err := grpcf.MarshalJSONAsAny(original)
	require.NoError(t, err)

	decoded, err := grpcf.UnmarshalJSONFromAny(anyValue)
	require.NoError(t, err)

	require.Equal(t, original, decoded)
}

// TestProtoAnyConversion exercises the deprecated names to ensure they keep
// routing through to the new helpers without behaviour change.
func TestProtoAnyConversion(t *testing.T) {
	t.Parallel()

	anyValue := map[string]any{
		"message": "hello world",
	}

	//nolint:staticcheck // exercising deprecated aliases on purpose
	toProto, err := grpcf.InterfaceToProtoAny(anyValue)
	require.NoError(t, err)

	//nolint:staticcheck // exercising deprecated aliases on purpose
	fromProto, err := grpcf.ProtoAnyToInterface(toProto)
	require.NoError(t, err)

	require.Equal(t, anyValue, fromProto)
}
