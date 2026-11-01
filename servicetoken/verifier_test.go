package servicetoken_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/jwt/v2"

	"github.com/a-novel-kit/golib/servicetoken"
	"github.com/a-novel-kit/golib/servicetoken/servicetokentest"
)

const (
	testAudience = "json-keys"
	testCaller   = "system:serviceaccount:authentication:api"
)

func TestVerifier(t *testing.T) {
	t.Parallel()

	issuer := servicetokentest.NewIssuer(t)
	impostor := servicetokentest.NewIssuer(t)

	verifier := servicetoken.NewVerifier(servicetoken.Config{
		Issuer:   issuer.URL,
		Audience: testAudience,
		Callers:  []string{testCaller},
	}, http.DefaultClient)

	wrongAudience := issuer.Claims(testCaller, "genai")

	expired := issuer.Claims(testCaller, testAudience)
	expired.Iat = time.Now().Add(-2 * time.Hour).Unix()
	expired.Nbf = expired.Iat
	expired.Exp = time.Now().Add(-time.Hour).Unix()

	wrongIssuer := issuer.Claims(testCaller, testAudience)
	wrongIssuer.Iss = impostor.URL

	// The impostor signs claims naming the trusted issuer, with a key outside its key set.
	forged := impostor.Claims(testCaller, testAudience)
	forged.Iss = issuer.URL

	testCases := []struct {
		name string

		token string

		expectRejected bool
	}{
		{
			name: "Success",

			token: issuer.Sign(t, issuer.Claims(testCaller, testAudience)),
		},
		{
			name: "Error/WrongAudience",

			token: issuer.Sign(t, wrongAudience),

			expectRejected: true,
		},
		{
			name: "Error/Expired",

			token: issuer.Sign(t, expired),

			expectRejected: true,
		},
		{
			name: "Error/UnknownSubject",

			token: issuer.Sign(t, issuer.Claims("system:serviceaccount:studio:platform", testAudience)),

			expectRejected: true,
		},
		{
			name: "Error/BadSignature",

			token: impostor.Sign(t, forged),

			expectRejected: true,
		},
		{
			name: "Error/WrongIssuer",

			token: issuer.Sign(t, wrongIssuer),

			expectRejected: true,
		},
		{
			name: "Error/Missing",

			expectRejected: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			caller, err := verifier.Verify(t.Context(), testCase.token)
			if !testCase.expectRejected {
				require.NoError(t, err)
				require.Equal(t, testCaller, caller)

				return
			}

			require.ErrorIs(t, err, jwt.ErrTokenRejected)
		})
	}

	t.Run("Error/IssuerUnreachable", func(t *testing.T) {
		t.Parallel()

		// A verifier that cannot read the issuer's keys is at fault, whatever the token.
		unreachable := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(unreachable.Close)

		faulty := servicetoken.NewVerifier(servicetoken.Config{
			Issuer:   unreachable.URL,
			Audience: testAudience,
			Callers:  []string{testCaller},
		}, http.DefaultClient)

		_, err := faulty.Verify(t.Context(), issuer.Sign(t, issuer.Claims(testCaller, testAudience)))
		require.Error(t, err)
		require.NotErrorIs(t, err, jwt.ErrTokenRejected)
	})
}
