package apikey_test

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/jwt/v2"

	"github.com/a-novel-kit/golib/apikey"
	"github.com/a-novel-kit/golib/apikey/apikeytest"
)

const (
	testPurpose = "publish-narrative-engine-plugin"
	testBody    = `{"plugin":"scenes-composer","version":"1.0.0"}`
)

func TestNewVerifier(t *testing.T) {
	t.Parallel()

	key := apikeytest.NewKey()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)

	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	ecdsaDER, err := x509.MarshalPKIXPublicKey(&ecdsaKey.PublicKey)
	require.NoError(t, err)

	testCases := []struct {
		name string

		config apikey.Config

		expectErr error
	}{
		{
			name: "Success",

			config: apikey.Config{Purpose: testPurpose, PublicKey: key.PublicKey},
		},
		{
			// A secret mounted from a file often ends with blank lines.
			name: "Success/TrailingBlankLines",

			config: apikey.Config{Purpose: testPurpose, PublicKey: key.PublicKey + "\n\n"},
		},
		{
			name: "Error/NoPurpose",

			config: apikey.Config{PublicKey: key.PublicKey},

			expectErr: apikey.ErrPurposeRequired,
		},
		{
			name: "Error/NoPublicKey",

			config: apikey.Config{Purpose: testPurpose},

			expectErr: apikey.ErrInvalidPublicKey,
		},
		{
			name: "Error/NotPEM",

			config: apikey.Config{Purpose: testPurpose, PublicKey: "MCowBQYDK2VwAyEA"},

			expectErr: apikey.ErrInvalidPublicKey,
		},
		{
			// Two keys would leave one unused without a word.
			name: "Error/SeveralKeys",

			config: apikey.Config{Purpose: testPurpose, PublicKey: key.PublicKey + apikeytest.NewKey().PublicKey},

			expectErr: apikey.ErrInvalidPublicKey,
		},
		{
			// The verifier never holds the private key.
			name: "Error/PrivateKey",

			config: apikey.Config{
				Purpose:   testPurpose,
				PublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
			},

			expectErr: apikey.ErrInvalidPublicKey,
		},
		{
			name: "Error/NotEd25519",

			config: apikey.Config{
				Purpose:   testPurpose,
				PublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: ecdsaDER})),
			},

			expectErr: apikey.ErrInvalidPublicKey,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			verifier, err := apikey.NewVerifier(testCase.config)
			require.ErrorIs(t, err, testCase.expectErr)
			require.Equal(t, testCase.expectErr == nil, verifier != nil)
		})
	}
}

func TestVerifier(t *testing.T) {
	t.Parallel()

	key := apikeytest.NewKey()
	otherKey := apikeytest.NewKey()

	verifier, err := apikey.NewVerifier(apikey.Config{Purpose: testPurpose, PublicKey: key.PublicKey})
	require.NoError(t, err)

	now := time.Now()

	claims := func(change func(claims *apikey.Claims)) *apikey.Claims {
		changed := key.Claims(testPurpose, []byte(testBody))
		change(changed)

		return changed
	}

	testCases := []struct {
		name string

		token string
		body  string

		expectRejected bool
	}{
		{
			name: "Success",

			token: key.Sign(t, key.Claims(testPurpose, []byte(testBody))),
			body:  testBody,
		},
		{
			name: "Success/EmptyBody",

			token: key.Sign(t, key.Claims(testPurpose, nil)),
		},
		{
			// The signer's clock runs ahead of the server's, within the leeway.
			name: "Success/SignedWithinLeeway",

			token: key.Sign(t, claims(func(claims *apikey.Claims) {
				claims.Iat = now.Add(apikey.Leeway / 2).Unix()
				claims.Exp = now.Add(apikey.Leeway/2 + apikey.MaxLifetime).Unix()
			})),
			body: testBody,
		},
		{
			// The signer's clock runs behind the server's, within the leeway.
			name: "Success/ExpiredWithinLeeway",

			token: key.Sign(t, claims(func(claims *apikey.Claims) {
				claims.Iat = now.Add(-apikey.Leeway/2 - apikey.MaxLifetime).Unix()
				claims.Exp = now.Add(-apikey.Leeway / 2).Unix()
			})),
			body: testBody,
		},
		{
			name: "Error/OtherKey",

			token: otherKey.Sign(t, otherKey.Claims(testPurpose, []byte(testBody))),
			body:  testBody,

			expectRejected: true,
		},
		{
			name: "Error/OtherPurpose",

			token: key.Sign(t, key.Claims("delete-narrative-engine-plugin", []byte(testBody))),
			body:  testBody,

			expectRejected: true,
		},
		{
			name: "Error/NoPurpose",

			token: key.Sign(t, claims(func(claims *apikey.Claims) { claims.Aud = "" })),
			body:  testBody,

			expectRejected: true,
		},
		{
			// A key has one purpose, so a token names one.
			name: "Error/SeveralPurposes",

			token: key.Sign(t, map[string]any{
				"aud":        []string{testPurpose, "delete-narrative-engine-plugin"},
				"iat":        now.Unix(),
				"exp":        now.Add(apikey.MaxLifetime).Unix(),
				"bodySHA256": key.Claims(testPurpose, []byte(testBody)).BodySHA256,
			}),
			body: testBody,

			expectRejected: true,
		},
		{
			name: "Error/Expired",

			token: key.Sign(t, claims(func(claims *apikey.Claims) {
				claims.Iat = now.Add(-2*apikey.Leeway - apikey.MaxLifetime).Unix()
				claims.Exp = now.Add(-2 * apikey.Leeway).Unix()
			})),
			body: testBody,

			expectRejected: true,
		},
		{
			name: "Error/SignedInTheFuture",

			token: key.Sign(t, claims(func(claims *apikey.Claims) {
				claims.Iat = now.Add(2 * apikey.Leeway).Unix()
				claims.Exp = now.Add(2*apikey.Leeway + apikey.MaxLifetime).Unix()
			})),
			body: testBody,

			expectRejected: true,
		},
		{
			name: "Error/LivesTooLong",

			token: key.Sign(t, claims(func(claims *apikey.Claims) { claims.Exp = claims.Iat + 301 })),
			body:  testBody,

			expectRejected: true,
		},
		{
			name: "Error/ExpiresBeforeSigned",

			token: key.Sign(t, claims(func(claims *apikey.Claims) { claims.Exp = claims.Iat })),
			body:  testBody,

			expectRejected: true,
		},
		{
			name: "Error/NoIssueDate",

			token: key.Sign(t, claims(func(claims *apikey.Claims) { claims.Iat = 0 })),
			body:  testBody,

			expectRejected: true,
		},
		{
			name: "Error/NoExpiry",

			token: key.Sign(t, claims(func(claims *apikey.Claims) { claims.Exp = 0 })),
			body:  testBody,

			expectRejected: true,
		},
		{
			name: "Error/OtherBody",

			token: key.Sign(t, key.Claims(testPurpose, []byte(testBody))),
			body:  testBody + " ",

			expectRejected: true,
		},
		{
			name: "Error/NoDigest",

			token: key.Sign(t, claims(func(claims *apikey.Claims) { claims.BodySHA256 = "" })),
			body:  testBody,

			expectRejected: true,
		},
		{
			// The digest is lowercase hex.
			name: "Error/UppercaseDigest",

			token: key.Sign(t, claims(func(claims *apikey.Claims) {
				claims.BodySHA256 = strings.ToUpper(claims.BodySHA256)
			})),
			body: testBody,

			expectRejected: true,
		},
		{
			// A claim of another type fails to decode, which refuses the token like any other fault.
			name: "Error/DigestNotAString",

			token: key.Sign(t, map[string]any{
				"aud":        testPurpose,
				"iat":        now.Unix(),
				"exp":        now.Add(apikey.MaxLifetime).Unix(),
				"bodySHA256": 12,
			}),
			body: testBody,

			expectRejected: true,
		},
		{
			name: "Error/NotAToken",

			token: "not-a-token",
			body:  testBody,

			expectRejected: true,
		},
		{
			name: "Error/NoToken",

			body: testBody,

			expectRejected: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			body, err := verifier.Verify(t.Context(), testCase.token, strings.NewReader(testCase.body))
			if !testCase.expectRejected {
				require.NoError(t, err)
				require.Equal(t, testCase.body, string(body))

				return
			}

			require.ErrorIs(t, err, jwt.ErrTokenRejected)
			require.Nil(t, body)
		})
	}

	// A caller without the key makes the server read nothing.
	t.Run("Error/RejectedBeforeBody", func(t *testing.T) {
		t.Parallel()

		token := otherKey.Sign(t, otherKey.Claims(testPurpose, []byte(testBody)))
		body := strings.NewReader(testBody)

		_, err := verifier.Verify(t.Context(), token, body)
		require.ErrorIs(t, err, jwt.ErrTokenRejected)
		require.Equal(t, len(testBody), body.Len())
	})

	// A body that fails to read is no fault of the token.
	t.Run("Error/UnreadableBody", func(t *testing.T) {
		t.Parallel()

		errFoo := errors.New("foo")
		token := key.Sign(t, key.Claims(testPurpose, []byte(testBody)))

		_, err := verifier.Verify(t.Context(), token, iotest.ErrReader(errFoo))
		require.ErrorIs(t, err, errFoo)
		require.NotErrorIs(t, err, jwt.ErrTokenRejected)
	})
}
