package httpf_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/apikey"
	"github.com/a-novel-kit/golib/apikey/apikeytest"
	"github.com/a-novel-kit/golib/httpf"
)

func TestAPIKeyGate(t *testing.T) {
	t.Parallel()

	const (
		purpose = "publish-narrative-engine-plugin"
		body    = `{"plugin":"scenes-composer","version":"1.0.0"}`
	)

	key := apikeytest.NewKey()
	otherKey := apikeytest.NewKey()

	config := apikey.Config{Purpose: purpose, PublicKey: key.PublicKey}

	valid := key.Sign(t, key.Claims(purpose, []byte(body)))

	expired := key.Claims(purpose, []byte(body))
	expired.Iat = time.Now().Add(-time.Hour).Unix()
	expired.Exp = expired.Iat + int64(apikey.MaxLifetime/time.Second)

	testCases := []struct {
		name string

		config        apikey.Config
		authorization string
		maxBodySize   int64
		brokenBody    bool

		expectErr    error
		expectStatus int
	}{
		{
			name: "Success",

			config:        config,
			authorization: "Bearer " + valid,

			expectStatus: http.StatusOK,
		},
		{
			name: "Success/SchemeCase",

			config:        config,
			authorization: "bearer " + valid,

			expectStatus: http.StatusOK,
		},
		{
			// A local or test stack runs without a key, and admits every request.
			name: "Success/Optional",

			config: apikey.Config{Purpose: purpose, Optional: true},

			expectStatus: http.StatusOK,
		},
		{
			name: "Error/NoToken",

			config: config,

			expectStatus: http.StatusUnauthorized,
		},
		{
			name: "Error/OtherScheme",

			config:        config,
			authorization: "Basic " + valid,

			expectStatus: http.StatusUnauthorized,
		},
		{
			name: "Error/OtherKey",

			config:        config,
			authorization: "Bearer " + otherKey.Sign(t, otherKey.Claims(purpose, []byte(body))),

			expectStatus: http.StatusUnauthorized,
		},
		{
			name: "Error/OtherPurpose",

			config:        config,
			authorization: "Bearer " + key.Sign(t, key.Claims("delete-narrative-engine-plugin", []byte(body))),

			expectStatus: http.StatusUnauthorized,
		},
		{
			name: "Error/Expired",

			config:        config,
			authorization: "Bearer " + key.Sign(t, expired),

			expectStatus: http.StatusUnauthorized,
		},
		{
			name: "Error/OtherBody",

			config:        config,
			authorization: "Bearer " + key.Sign(t, key.Claims(purpose, []byte(body+" "))),

			expectStatus: http.StatusUnauthorized,
		},
		{
			// A deployment that forgets the key keeps serving, and refuses every request of the gate.
			name: "Error/NoPublicKey",

			config:        apikey.Config{Purpose: purpose},
			authorization: "Bearer " + valid,

			expectStatus: http.StatusUnauthorized,
		},
		{
			// A configured gate ignores the opt-out.
			name: "Error/OptionalWithPublicKey",

			config: apikey.Config{Purpose: purpose, PublicKey: key.PublicKey, Optional: true},

			expectStatus: http.StatusUnauthorized,
		},
		{
			name: "Error/BodyTooLarge",

			config:        config,
			authorization: "Bearer " + valid,
			maxBodySize:   int64(len(body)) - 1,

			expectStatus: http.StatusRequestEntityTooLarge,
		},
		{
			// A body that fails to read answers as a handler's own decoding does.
			name: "Error/UnreadableBody",

			config:        config,
			authorization: "Bearer " + valid,
			brokenBody:    true,

			expectStatus: http.StatusBadRequest,
		},
		{
			name: "Error/InvalidPublicKey",

			config: apikey.Config{Purpose: purpose, PublicKey: "not a key"},

			expectErr: apikey.ErrInvalidPublicKey,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gate, err := httpf.APIKeyGate(testCase.config, &recordingLog{})
			require.ErrorIs(t, err, testCase.expectErr)

			if err != nil {
				return
			}

			// The handler reads the body the gate checked.
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received, err := io.ReadAll(r.Body)
				if err != nil || string(received) != body {
					w.WriteHeader(http.StatusInternalServerError)

					return
				}

				w.WriteHeader(http.StatusOK)
			})

			request := httptest.NewRequestWithContext(
				t.Context(), http.MethodPost, "/v1/plugin/version", strings.NewReader(body),
			)
			if testCase.authorization != "" {
				request.Header.Set("Authorization", testCase.authorization)
			}

			recorder := httptest.NewRecorder()
			if testCase.maxBodySize > 0 {
				request.Body = http.MaxBytesReader(recorder, request.Body, testCase.maxBodySize)
			}

			if testCase.brokenBody {
				request.Body = io.NopCloser(iotest.ErrReader(errors.New("foo")))
			}

			gate(next).ServeHTTP(recorder, request)

			require.Equal(t, testCase.expectStatus, recorder.Code)

			// Every refused token reads alike, so the answer never says which check failed.
			if testCase.expectStatus == http.StatusUnauthorized {
				require.Equal(t, "Bearer", recorder.Header().Get("WWW-Authenticate"))
				require.Equal(t, http.StatusText(http.StatusUnauthorized)+"\n", recorder.Body.String())
			}
		})
	}
}
