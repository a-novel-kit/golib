package httpf_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/httpf"
	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
	otelpresets "github.com/a-novel-kit/golib/otel/presets"
)

// newStreamRouter returns a router from [httpf.NewRouter] whose requests time out after timeout.
func newStreamRouter(timeout time.Duration) *chi.Mux {
	config := httpf.ServerConfigDefault
	config.RateLimitsEnabled = false
	config.Timeouts.Request = timeout

	return httpf.NewRouter(
		config, &otelpresets.Disabled{},
		&loggingpresets.HTTPLocal{BaseLogger: &loggingpresets.LogLocal{Out: io.Discard}}, nil,
	)
}

func TestStreaming(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		// streaming mounts the route under Streaming, with limit.
		streaming bool
		limit     time.Duration
		// wait is how long the handler works before it writes, unless its context ends first.
		wait time.Duration
		// disconnect ends the client's connection while the handler works.
		disconnect bool

		expectStatus int
		expectBody   string
		expectErr    error
	}{
		{
			name: "Success/PastRequestTimeout",

			streaming: true,
			limit:     serverTestTimeout,
			wait:      100 * time.Millisecond,

			expectStatus: http.StatusOK,
			expectBody:   "late",
		},
		{
			name: "Error/StreamLimit",

			streaming: true,
			limit:     50 * time.Millisecond,
			wait:      serverTestTimeout,

			expectStatus: http.StatusOK,
			expectErr:    context.DeadlineExceeded,
		},
		{
			name: "Error/ClientDisconnects",

			streaming:  true,
			limit:      serverTestTimeout,
			wait:       serverTestTimeout,
			disconnect: true,

			expectStatus: http.StatusOK,
			expectErr:    context.Canceled,
		},
		{
			name: "Error/RequestTimeout",

			wait: serverTestTimeout,

			expectStatus: http.StatusGatewayTimeout,
			expectErr:    context.DeadlineExceeded,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var handlerErr error

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
					handlerErr = r.Context().Err()
				case <-time.After(testCase.wait):
					_, _ = w.Write([]byte("late"))
				}
			})

			router := newStreamRouter(20 * time.Millisecond)
			if testCase.streaming {
				router.With(httpf.Streaming(testCase.limit)).Get("/stream", handler)
			} else {
				router.Get("/stream", handler)
			}

			ctx, disconnect := context.WithCancel(t.Context())
			t.Cleanup(disconnect)

			if testCase.disconnect {
				time.AfterFunc(50*time.Millisecond, disconnect)
			}

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, "/stream", nil))

			require.Equal(t, testCase.expectStatus, response.Code)
			require.Equal(t, testCase.expectBody, response.Body.String())
			require.ErrorIs(t, handlerErr, testCase.expectErr)
		})
	}
}

func TestEventStream(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		events []httpf.Event

		expectBody string
		expectErr  error
	}{
		{
			name: "Success",

			events: []httpf.Event{
				{ID: "1", Type: "delta", Data: []byte(`{"text":"Il était"}`)},
				{Data: []byte("first\r\nsecond\rthird")},
			},

			expectBody: "id: 1\nevent: delta\ndata: {\"text\":\"Il était\"}\n\n" +
				"data: first\ndata: second\ndata: third\n\n",
		},
		{
			name: "Error/NoData",

			events: []httpf.Event{{ID: "1"}},

			expectErr: httpf.ErrInvalidEvent,
		},
		{
			name: "Error/LineBreakInID",

			events: []httpf.Event{{ID: "1\n2", Data: []byte("{}")}},

			expectErr: httpf.ErrInvalidEvent,
		},
		{
			name: "Error/NulInType",

			events: []httpf.Event{{Type: "delta\x00", Data: []byte("{}")}},

			expectErr: httpf.ErrInvalidEvent,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			response := httptest.NewRecorder()

			stream, err := httpf.NewEventStream(t.Context(), response, time.Hour)
			require.NoError(t, err)

			for _, event := range testCase.events {
				err = stream.Send(event)
				if err != nil {
					break
				}
			}

			stream.Close()

			require.ErrorIs(t, err, testCase.expectErr)
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, "text/event-stream", response.Header().Get("Content-Type"))
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.Equal(t, testCase.expectBody, response.Body.String())
		})
	}

	t.Run("Success/Heartbeat", func(t *testing.T) {
		t.Parallel()

		response := httptest.NewRecorder()

		stream, err := httpf.NewEventStream(t.Context(), response, 5*time.Millisecond)
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)
		stream.Close()

		require.Contains(t, response.Body.String(), ":\n\n")
	})

	// The client reads an event while its handler still runs, through every middleware of
	// NewRouter and a real connection.
	t.Run("Success/ThroughRouter", func(t *testing.T) {
		t.Parallel()

		received := make(chan struct{})

		router := newStreamRouter(20 * time.Millisecond)
		router.With(httpf.Streaming(serverTestTimeout)).Get("/stream", http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				stream, err := httpf.NewEventStream(r.Context(), w, time.Hour)
				if err != nil {
					panic(err)
				}
				defer stream.Close()

				_ = stream.Send(httpf.Event{ID: "1", Data: []byte("{}")})

				// The handler outlives the request timeout until the client holds the event.
				select {
				case <-received:
				case <-r.Context().Done():
				}
			},
		))

		server := httptest.NewServer(router)
		t.Cleanup(server.Close)

		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/stream", nil)
		require.NoError(t, err)

		response, err := server.Client().Do(request)
		require.NoError(t, err)

		defer func() { _ = response.Body.Close() }()

		require.Equal(t, http.StatusOK, response.StatusCode)

		reader := bufio.NewReader(response.Body)

		var frame strings.Builder

		for !strings.HasSuffix(frame.String(), "\n\n") {
			line, readErr := reader.ReadString('\n')
			require.NoError(t, readErr)
			frame.WriteString(line)
		}

		close(received)

		require.Equal(t, "id: 1\ndata: {}\n\n", frame.String())
	})
}
