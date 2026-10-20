package config_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/config"
)

var errPanic = errors.New("panic")

// unmarshalValid decodes the literal "valid" into a string, and refuses anything else.
func unmarshalValid(data []byte, v any) error {
	if string(data) != "valid" {
		return errPanic
	}

	*v.(*string) = "unmarshaled"

	return nil
}

func TestMust(t *testing.T) {
	t.Parallel()

	fn1 := func() (string, error) {
		return "foo", nil
	}

	fn2 := func() (string, error) {
		return "", errPanic
	}

	require.Equal(t, "foo", config.Must(fn1()))
	require.PanicsWithValue(t, errPanic, func() {
		config.Must(fn2())
	})
}

func TestMustUnmarshal(t *testing.T) {
	t.Parallel()

	validData := []byte("valid")
	invalidData := []byte("invalid")

	require.Equal(t, "unmarshaled", config.MustUnmarshal[string](unmarshalValid, validData))
	require.PanicsWithValue(t, errPanic, func() {
		config.MustUnmarshal[string](unmarshalValid, invalidData)
	})
}
