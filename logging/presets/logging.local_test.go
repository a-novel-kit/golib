package loggingpresets_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
)

// rendered returns the line LogLocal writes for msg and fields at error level.
func rendered(t *testing.T, msg string, fields []any) string {
	t.Helper()

	out := &bytes.Buffer{}
	logger := &loggingpresets.LogLocal{Out: out}

	logger.Err(t.Context(), msg, fields...)

	// The severity color arrives as ANSI escapes around the text.
	return strings.TrimSpace(out.String())
}

func TestLogLocal(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		msg    string
		fields []any

		expect []string
	}{
		// A message with no operands goes out as written, so a % in the error text httpf.HandleError
		// logs survives.
		{
			name:   "Success/PercentSign",
			msg:    "disk usage at 95% on /var",
			expect: []string{"disk usage at 95% on /var"},
		},
		{
			name:   "Success/VerbLikeSequence",
			msg:    `parse "%s" failed`,
			expect: []string{`parse "%s" failed`},
		},
		{
			name:   "Success/TrailingPercent",
			msg:    "progress: 100%",
			expect: []string{"progress: 100%"},
		},
		{
			name:   "Success/Plain",
			msg:    "nothing special here",
			expect: []string{"nothing special here"},
		},
		// With operands, LogLocal renders msg as a format string. That is the local convention, and
		// the doc on logging.Log says so.
		{
			name:   "Success/FormatsFields",
			msg:    "took %s for %d items",
			fields: []any{"1.2s", 42},
			expect: []string{"took 1.2s for 42 items"},
		},
		// A field with no verb to land on is appended rather than dropped, which is what makes the
		// divergence between the two presets lossless.
		{
			name:   "Success/KeepsAFieldWithNoVerb",
			msg:    "item rejected",
			fields: []any{"credential-42"},
			expect: []string{"item rejected", "credential-42"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := rendered(t, testCase.msg, testCase.fields)

			for _, expect := range testCase.expect {
				require.Contains(t, got, expect)
			}

			if len(testCase.fields) == 0 {
				require.NotContains(t, got, "%!", "the message must not be rewritten by a format pass")
			}
		})
	}
}
