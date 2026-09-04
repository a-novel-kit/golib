package otelpresets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	otelpresets "github.com/a-novel-kit/golib/otel/presets"
)

func TestGcloud(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		expectFields []string
	}{
		{
			name:         "Success/StandardPropagation",
			expectFields: []string{"traceparent", "tracestate", "baggage"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := &otelpresets.Gcloud{}
			propagators, err := config.GetPropagators()
			require.NoError(t, err)
			require.ElementsMatch(t, testCase.expectFields, propagators.Fields())
		})
	}
}
