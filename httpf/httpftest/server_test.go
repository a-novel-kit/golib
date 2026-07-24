package httpftest_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/httpf"
	"github.com/a-novel-kit/golib/httpf/httpftest"
)

// serverDeadline is short enough to keep the suite quick, and long enough that a machine under load
// does not trip it before the request is even sent.
const serverDeadline = 250 * time.Millisecond

func TestServer(t *testing.T) {
	t.Parallel()

	t.Run("Success/ReplaysAndRecords", func(t *testing.T) {
		t.Parallel()

		golden := httpftest.Golden(t, "response.json")

		server := httpftest.NewServer(t, httpftest.Response{
			Status: http.StatusCreated,
			Header: http.Header{"Content-Type": []string{"application/json"}},
			Body:   golden,
		})

		request, err := http.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			server.URL()+"/things?mode=test",
			strings.NewReader(`{"prompt":"hi"}`),
		)
		require.NoError(t, err)

		response, err := httpf.NewClient(httpf.ClientOptions{}).Do(request)
		require.NoError(t, err)

		body, err := io.ReadAll(response.Body)
		require.NoError(t, errors.Join(err, response.Body.Close()))

		require.Equal(t, http.StatusCreated, response.StatusCode)
		require.Equal(t, "application/json", response.Header.Get("Content-Type"))
		require.JSONEq(t, golden, string(body))

		recorded := server.Requests()
		require.Len(t, recorded, 1)
		require.Equal(t, http.MethodPost, recorded[0].Method)
		require.Equal(t, "/things", recorded[0].Path)
		require.Equal(t, "test", recorded[0].Query.Get("mode"))
		require.JSONEq(t, `{"prompt":"hi"}`, string(recorded[0].Body))
	})

	t.Run("Error/HeldOpenPastTheDeadline", func(t *testing.T) {
		t.Parallel()

		server := httpftest.NewServer(t, httpftest.Response{Hang: true})

		ctx, cancel := context.WithTimeout(t.Context(), serverDeadline)
		defer cancel()

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL(), nil)
		require.NoError(t, err)

		//nolint:bodyclose // The call never returns a response, so there is no body to close.
		_, err = httpf.NewClient(httpf.ClientOptions{}).Do(request)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		require.Len(t, server.Requests(), 1)
	})

	t.Run("Error/DroppedConnection", func(t *testing.T) {
		t.Parallel()

		server := httpftest.NewServer(t, httpftest.Response{Drop: true})

		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL(), nil)
		require.NoError(t, err)

		//nolint:bodyclose // The call never returns a response, so there is no body to close.
		_, err = httpf.NewClient(httpf.ClientOptions{}).Do(request)
		require.Error(t, err)

		require.Len(t, server.Requests(), 1)
	})
}
