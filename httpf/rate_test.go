package httpf_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/httpf"
)

func TestParseRate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		value string

		expect    httpf.Rate
		expectErr error
	}{
		{name: "Success/Simple", value: "3/5m", expect: httpf.Rate{Requests: 3, Window: 5 * time.Minute}},
		{name: "Success/Compound", value: "10/1h30m", expect: httpf.Rate{Requests: 10, Window: 90 * time.Minute}},
		{name: "Error/NoSeparator", value: "5", expectErr: httpf.ErrInvalidRate},
		{name: "Error/NoUnit", value: "5/1", expectErr: httpf.ErrInvalidRate},
		{name: "Error/ZeroRequests", value: "0/1s", expectErr: httpf.ErrInvalidRate},
		{name: "Error/NegativeWindow", value: "5/-1s", expectErr: httpf.ErrInvalidRate},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rate, err := httpf.ParseRate(testCase.value)
			require.ErrorIs(t, err, testCase.expectErr)
			require.Equal(t, testCase.expect, rate)

			if testCase.expectErr == nil {
				// String writes what ParseRate reads back.
				parsed, err := httpf.ParseRate(rate.String())
				require.NoError(t, err)
				require.Equal(t, rate, parsed)
			}
		})
	}
}
