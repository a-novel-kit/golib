package httpf_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/httpf"
)

func TestLimits(t *testing.T) {
	t.Parallel()

	type request struct {
		path    string
		visitor string
	}

	// times repeats one request, so a case reads as the budget it spends.
	times := func(count int, req request) []request {
		requests := make([]request, count)
		for i := range requests {
			requests[i] = req
		}

		return requests
	}

	visitor := request{path: "/items", visitor: "192.0.2.1"}
	twoPerMinute := func(limits httpf.Limits) func(http.Handler) http.Handler {
		return limits.By(httpf.Rate{Requests: 2, Window: time.Minute}, httpf.VisitorKey)
	}

	testCases := []struct {
		name string

		disabled bool
		rate     *httpf.Rate
		limit    func(limits httpf.Limits) func(http.Handler) http.Handler

		// allowed requests all pass; refused, when set, then gets 429 with expectRetryAfter.
		allowed []request
		refused *request

		expectRetryAfter string
	}{
		{
			name: "Success/Disabled",

			disabled: true,
			limit:    twoPerMinute,

			allowed: times(5, visitor),
		},
		{
			name: "Success/OtherVisitor",

			limit: twoPerMinute,

			allowed: append(times(2, visitor), request{path: "/items", visitor: "192.0.2.2"}),
		},
		{
			name: "Success/OtherRoute",

			limit: twoPerMinute,

			allowed: append(times(2, visitor), request{path: "/items/1", visitor: "192.0.2.1"}),
		},
		{
			name: "Error/Standard",

			limit: httpf.Limits.Standard,

			allowed: times(5, visitor),
			refused: &visitor,

			expectRetryAfter: "1",
		},
		{
			name: "Error/Standard/ConfiguredRate",

			rate:  &httpf.Rate{Requests: 2, Window: time.Minute},
			limit: httpf.Limits.Standard,

			allowed: times(2, visitor),
			refused: &visitor,

			expectRetryAfter: "60",
		},
		{
			name: "Error/By",

			limit: twoPerMinute,

			allowed: times(2, visitor),
			refused: &visitor,

			expectRetryAfter: "60",
		},
		{
			// Path parameters must not open a fresh budget per value.
			name: "Error/By/PathParameters",

			limit: twoPerMinute,

			allowed: []request{{path: "/items/1", visitor: "192.0.2.1"}, {path: "/items/2", visitor: "192.0.2.1"}},
			refused: &request{path: "/items/3", visitor: "192.0.2.1"},

			expectRetryAfter: "60",
		},
		{
			// One IPv6 client controls its whole /64.
			name: "Error/By/IPv6Prefix",

			limit: twoPerMinute,

			allowed: []request{{path: "/items", visitor: "2001:db8::1"}, {path: "/items", visitor: "2001:db8::2"}},
			refused: &request{path: "/items", visitor: "2001:db8::3"},

			expectRetryAfter: "60",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := httpf.ServerConfigDefault
			config.RateLimitsEnabled = !testCase.disabled

			if testCase.rate != nil {
				config.RateLimit = *testCase.rate
			}

			ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

			router := chi.NewRouter()
			router.Use(middleware.ClientIPFromHeader("X-Client-Ip"))
			router.Group(func(api chi.Router) {
				api.Use(testCase.limit(httpf.NewLimits(config)))
				api.Get("/items", ok)
				api.Get("/items/{id}", ok)
			})

			send := func(req request) *httptest.ResponseRecorder {
				httpRequest := httptest.NewRequestWithContext(t.Context(), http.MethodGet, req.path, nil)
				httpRequest.Header.Set("X-Client-Ip", req.visitor)

				response := httptest.NewRecorder()
				router.ServeHTTP(response, httpRequest)

				return response
			}

			for _, req := range testCase.allowed {
				require.Equal(t, http.StatusOK, send(req).Code, req.path)
			}

			if testCase.refused != nil {
				response := send(*testCase.refused)
				require.Equal(t, http.StatusTooManyRequests, response.Code)
				require.Equal(t, testCase.expectRetryAfter, response.Header().Get("Retry-After"))
			}
		})
	}
}

func TestLimitConcurrency(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	started := make(chan struct{}, 2)

	router := chi.NewRouter()
	router.With(httpf.LimitConcurrency(2)).Get("/slow", func(w http.ResponseWriter, _ *http.Request) {
		started <- struct{}{}

		<-release
		w.WriteHeader(http.StatusOK)
	})

	send := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/slow", nil))

		return response
	}

	held := make(chan *httptest.ResponseRecorder, 2)

	for range 2 {
		go func() { held <- send() }()
	}

	<-started
	<-started

	// Both slots are taken, so a third request is refused at once.
	refused := send()
	require.Equal(t, http.StatusTooManyRequests, refused.Code)
	require.Equal(t, "1", refused.Header().Get("Retry-After"))

	close(release)
	require.Equal(t, http.StatusOK, (<-held).Code)
	require.Equal(t, http.StatusOK, (<-held).Code)
}

func TestJSONFieldKey(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		body string

		expect string
	}{
		{name: "Success", body: `{"email":"a@example.com"}`, expect: "email:a@example.com"},
		{name: "Success/CaseAndSpace", body: `{"email":" A@Example.com "}`, expect: "email:a@example.com"},
		{name: "Success/MissingField", body: `{"name":"a"}`, expect: "email:"},
		{name: "Success/NotAString", body: `{"email":3}`, expect: "email:"},
		{name: "Success/Malformed", body: `{"email":`, expect: "email:"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequestWithContext(
				t.Context(), http.MethodPut, "/", strings.NewReader(testCase.body),
			)

			key, err := httpf.JSONFieldKey("email")(request)
			require.NoError(t, err)
			require.Equal(t, testCase.expect, key)

			// The handler still reads the whole body.
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.Equal(t, testCase.body, string(body))
		})
	}
}
