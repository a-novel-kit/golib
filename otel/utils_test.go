package otel_test

import (
	"testing"

	"github.com/a-novel-kit/golib/otel"
)

func TestRecoverPanic(t *testing.T) {
	t.Parallel()

	t.Run("AbsorbsAPanic", func(t *testing.T) {
		t.Parallel()

		done := make(chan struct{})

		// Reaching done is the assertion: a panic RecoverPanic misses ends the test binary itself.
		go func() {
			defer close(done)

			_, span := otel.Tracer().Start(t.Context(), "test.RecoverPanic")
			defer span.End()
			defer otel.RecoverPanic(t.Context(), span)

			panic("boom")
		}()

		<-done
	})

	t.Run("NoOpWithoutAPanic", func(t *testing.T) {
		t.Parallel()

		_, span := otel.Tracer().Start(t.Context(), "test.RecoverPanic")
		defer span.End()

		otel.RecoverPanic(t.Context(), span)
	})
}
