package downtime_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/downtime"
)

func TestParseWindow(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		value string

		expect    *downtime.Window
		expectErr error
	}{
		{
			name:  "Success",
			value: `{"services":["json-keys","authentication"],"start":"2026-10-09T08:00:00Z","end":"2026-10-09T09:00:00Z"}`,
			expect: &downtime.Window{
				Services: []string{"json-keys", "authentication"},
				Start:    time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC),
				End:      time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "Success/NoWindow",
		},
		{
			name:      "Error/NoServices",
			value:     `{"services":[],"start":"2026-10-09T08:00:00Z","end":"2026-10-09T09:00:00Z"}`,
			expectErr: downtime.ErrInvalidWindow,
		},
		{
			name:      "Error/NoStart",
			value:     `{"services":["json-keys"],"end":"2026-10-09T09:00:00Z"}`,
			expectErr: downtime.ErrInvalidWindow,
		},
		{
			name:      "Error/EndBeforeStart",
			value:     `{"services":["json-keys"],"start":"2026-10-09T08:00:00Z","end":"2026-10-09T07:00:00Z"}`,
			expectErr: downtime.ErrInvalidWindow,
		},
		{
			name:      "Error/UnknownField",
			value:     `{"services":["json-keys"],"start":"2026-10-09T08:00:00Z","end":"2026-10-09T09:00:00Z","note":"x"}`,
			expectErr: downtime.ErrInvalidWindow,
		},
		{
			name:      "Error/Malformed",
			value:     `json-keys`,
			expectErr: downtime.ErrInvalidWindow,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			window, err := downtime.ParseWindow(testCase.value)
			require.ErrorIs(t, err, testCase.expectErr)
			require.Equal(t, testCase.expect, window)
		})
	}
}

func TestWindow(t *testing.T) {
	t.Parallel()

	now := time.Now()
	window := &downtime.Window{Services: []string{"json-keys"}, Start: now, End: now.Add(time.Hour)}

	testCases := []struct {
		name string

		window  *downtime.Window
		service string
		at      time.Time

		expect bool
	}{
		{
			name:    "Success/InProgress",
			window:  window,
			service: "json-keys",
			at:      now,
			expect:  true,
		},
		{
			// The end only informs clients: the window lasts until operators clear it.
			name:    "Success/PastEnd",
			window:  window,
			service: "json-keys",
			at:      now.Add(2 * time.Hour),
			expect:  true,
		},
		{
			name:    "Success/Notice",
			window:  window,
			service: "json-keys",
			at:      now.Add(-time.Minute),
		},
		{
			name:    "Success/Unlisted",
			window:  window,
			service: "authentication",
			at:      now,
		},
		{
			name:    "Success/NoWindow",
			service: "json-keys",
			at:      now,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expect, testCase.window.InProgress(testCase.service, testCase.at))
		})
	}
}
