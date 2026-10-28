package httpf_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/httpf"
	"github.com/a-novel-kit/golib/servicetoken"
	"github.com/a-novel-kit/golib/servicetoken/servicetokentest"
)

func TestServiceTokenMiddleware(t *testing.T) {
	t.Parallel()

	const (
		audience = "authentication"
		caller   = "system:serviceaccount:studio:platform"
		liveness = "/v2/ping"
	)

	unreachable := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(unreachable.Close)

	issuer := servicetokentest.NewIssuer(t)
	config := servicetoken.Config{Issuer: issuer.URL, Audience: audience, Callers: []string{caller}}
	verifier := servicetoken.NewVerifier(config, http.DefaultClient)

	config.Issuer = unreachable.URL
	faulty := servicetoken.NewVerifier(config, http.DefaultClient)

	valid := issuer.Sign(t, issuer.Claims(caller, audience))

	testCases := []struct {
		name string

		verifier *servicetoken.Verifier
		path     string
		tokens   []string

		expectStatus int
	}{
		{
			name: "Success",

			verifier: verifier,
			path:     "/v2/session",
			tokens:   []string{valid},

			expectStatus: http.StatusOK,
		},
		{
			// The kubelet probes the liveness route without a token.
			name: "Success/OpenPath",

			verifier: verifier,
			path:     liveness,

			expectStatus: http.StatusOK,
		},
		{
			// An open path admits that path alone, never the routes it prefixes.
			name: "Error/OpenPathPrefix",

			verifier: verifier,
			path:     liveness + "/more",

			expectStatus: http.StatusUnauthorized,
		},
		{
			name: "Error/Missing",

			verifier: verifier,
			path:     "/v2/session",

			expectStatus: http.StatusUnauthorized,
		},
		{
			name: "Error/Several",

			verifier: verifier,
			path:     "/v2/session",
			tokens:   []string{valid, valid},

			expectStatus: http.StatusUnauthorized,
		},
		{
			name: "Error/Rejected",

			verifier: verifier,
			path:     "/v2/session",
			tokens:   []string{issuer.Sign(t, issuer.Claims(caller, "json-keys"))},

			expectStatus: http.StatusUnauthorized,
		},
		{
			name: "Error/VerifierFault",

			verifier: faulty,
			path:     "/v2/session",
			tokens:   []string{valid},

			expectStatus: http.StatusInternalServerError,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			handler := httpf.ServiceTokenMiddleware(testCase.verifier, &recordingLog{}, liveness)(next)

			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, testCase.path, nil)
			for _, token := range testCase.tokens {
				request.Header.Add(servicetoken.Header, token)
			}

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			require.Equal(t, testCase.expectStatus, recorder.Code)
		})
	}
}
