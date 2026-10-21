package loggingpresets_test

import (
	"testing"

	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
)

func TestGRPCLocal(t *testing.T) {
	t.Parallel()

	t.Run("Error/PanicUnary", func(t *testing.T) {
		t.Parallel()

		requirePanicRecovered(t, &loggingpresets.GRPCLocal{}, panicUnaryCall)
	})

	t.Run("Error/PanicStream", func(t *testing.T) {
		t.Parallel()

		requirePanicRecovered(t, &loggingpresets.GRPCLocal{}, panicStreamCall)
	})
}
