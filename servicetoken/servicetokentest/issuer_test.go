package servicetokentest_test

import (
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/servicetoken"
	"github.com/a-novel-kit/golib/servicetoken/servicetokentest"
)

const (
	testAudience = "authentication"
	testCaller   = "system:serviceaccount:account:web"
)

func TestIssuer(t *testing.T) {
	t.Parallel()

	issuer := servicetokentest.NewIssuer(t)

	verifier := servicetoken.NewVerifier(servicetoken.Config{
		Issuer:   issuer.URL,
		Audience: testAudience,
		Callers:  []string{testCaller},
	}, http.DefaultClient)

	testCases := []struct {
		name string

		query url.Values

		expectStatus int
	}{
		{
			name: "Success",

			query: url.Values{"subject": {testCaller}, "audience": {testAudience}},

			expectStatus: http.StatusOK,
		},
		{
			name: "Error/NoSubject",

			query: url.Values{"audience": {testAudience}},

			expectStatus: http.StatusBadRequest,
		},
		{
			name: "Error/NoAudience",

			query: url.Values{"subject": {testCaller}},

			expectStatus: http.StatusBadRequest,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request, err := http.NewRequestWithContext(
				t.Context(), http.MethodGet, issuer.URL+"/token?"+testCase.query.Encode(), nil,
			)
			require.NoError(t, err)

			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)

			defer func() { _ = response.Body.Close() }()

			require.Equal(t, testCase.expectStatus, response.StatusCode)

			if testCase.expectStatus != http.StatusOK {
				return
			}

			token, err := io.ReadAll(response.Body)
			require.NoError(t, err)

			caller, err := verifier.Verify(t.Context(), string(token))
			require.NoError(t, err)
			require.Equal(t, testCaller, caller)
		})
	}
}
