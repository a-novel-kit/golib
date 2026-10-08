package downtime_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/downtime"
)

func TestParseStart(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	testCases := []struct {
		name string

		value string

		expect    *time.Time
		expectErr bool
	}{
		{
			name:   "Success",
			value:  "2026-10-09T08:00:00Z",
			expect: &start,
		},
		{
			name:   "Success/Offset",
			value:  "2026-10-09T10:00:00+02:00",
			expect: &start,
		},
		{
			name:      "Error/NotRFC3339",
			value:     "2026-10-09 08:00",
			expectErr: true,
		},
		{
			name:      "Error/Empty",
			expectErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := downtime.ParseStart(testCase.value)

			if testCase.expectErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.True(t, testCase.expect.Equal(*parsed))
		})
	}
}

func TestStarted(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	testCases := []struct {
		name string

		start *time.Time
		now   time.Time

		expect bool
	}{
		{
			name: "NoDowntime",
			now:  start,
		},
		{
			name:  "Before",
			start: &start,
			now:   start.Add(-time.Nanosecond),
		},
		{
			name:   "AtStart",
			start:  &start,
			now:    start,
			expect: true,
		},
		{
			// Only removing the start ends a downtime, whatever end was announced.
			name:   "LongAfter",
			start:  &start,
			now:    start.Add(30 * 24 * time.Hour),
			expect: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expect, downtime.Started(testCase.start, testCase.now))
		})
	}
}
