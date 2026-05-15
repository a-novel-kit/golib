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
