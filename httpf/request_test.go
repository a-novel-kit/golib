package httpf_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/httpf"
)

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	type request struct {
		Name string `json:"name"`
	}

	testCases := []struct {
		name string

		body string

		expect    request
		expectErr bool
		errorIs   error
	}{
		{
			name: "Success/Whitespace",
			body: " \n{\"name\":\"Ada\"}\t ",

			expect: request{Name: "Ada"},
		},
		{
			name:      "Success/UnknownField",
			body:      `{"name":"Ada","extra":true}`,
			expect:    request{Name: "Ada"},
			expectErr: false,
		},
		{
			name: "Success/Null",
			body: "null",
		},
		{
			name:      "Error/Empty",
			body:      "  \n\t",
			expectErr: true,
		},
		{
			name:      "Error/Malformed",
			body:      `{"name":`,
			expectErr: true,
		},
		{
			name:      "Error/SecondValue",
			body:      `{"name":"Ada"} {"name":"Grace"}`,
			expectErr: true,
			errorIs:   httpf.ErrJSONMultipleValues,
		},
		{
			name:      "Error/TrailingGarbage",
			body:      `{"name":"Ada"} garbage`,
			expectErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var got request

			err := httpf.DecodeJSON(strings.NewReader(testCase.body), &got)
			if testCase.expectErr {
				require.Error(t, err)

				if testCase.errorIs != nil {
					require.ErrorIs(t, err, testCase.errorIs)
				}

				return
			}

			require.NoError(t, err)
			require.Equal(t, testCase.expect, got)
		})
	}
}

func TestDecodeJSONBodyLimit(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	body := http.MaxBytesReader(recorder, io.NopCloser(strings.NewReader(`{"name":"Ada"} {"name":"Grace"}`)), 16)

	t.Cleanup(func() { require.NoError(t, body.Close()) })

	var request struct {
		Name string `json:"name"`
	}

	err := httpf.DecodeJSON(body, &request)

	var limitErr *http.MaxBytesError
	require.ErrorAs(t, err, &limitErr)
}
