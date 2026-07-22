package loggingpresets_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
)

func rendered(t *testing.T, msg string, fields []any) string {
	t.Helper()

	out := &bytes.Buffer{}
	logger := &loggingpresets.LogLocal{Out: out}

	logger.Err(t.Context(), msg, fields...)

	// The severity colour arrives as ANSI escapes around the text.
	return strings.TrimSpace(out.String())
}

// A message with no operands goes out as written. Passing it through Sprintf would
// rewrite any % it contains, and the path that reaches here with none —
// httpf.HandleError, with an error text — is the one whose detail matters most.
func TestLogLocalDoesNotFormatWithoutFields(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		msg string
	}{
		{
			name: "PercentSign",
			msg:  "disk usage at 95% on /var",
		},
		{
			name: "VerbLikeSequence",
			msg:  `parse "%s" failed`,
		},
		{
			name: "TrailingPercent",
			msg:  "progress: 100%",
		},
		{
			name: "Plain",
			msg:  "nothing special here",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := rendered(t, testCase.msg, nil)

			require.Contains(t, got, testCase.msg)
			require.NotContains(t, got, "%!", "the message must not be rewritten by a format pass")
		})
	}
}

// With operands, LogLocal renders msg as a format string. That is the local
// convention, and the doc on logging.Log says so.
func TestLogLocalFormatsWithFields(t *testing.T) {
	t.Parallel()

	got := rendered(t, "took %s for %d items", []any{"1.2s", 42})

	require.Contains(t, got, "took 1.2s for 42 items")
}

// A field with no verb to land on is appended rather than dropped, which is what
// makes the divergence between the two presets lossless.
func TestLogLocalKeepsAFieldWithNoVerb(t *testing.T) {
	t.Parallel()

	got := rendered(t, "item rejected", []any{"credential-42"})

	require.Contains(t, got, "item rejected")
	require.Contains(t, got, "credential-42")
}
