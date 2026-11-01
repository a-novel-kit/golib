package httpf_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/httpf"
)

// limitRequest is one request TestLimits sends, from visitor to path.
type limitRequest struct {
	path    string
	visitor string
}

// times repeats one request, so a case reads as the budget it spends.
func times(count int, req limitRequest) []limitRequest {
	return slices.Repeat([]limitRequest{req}, count)
}

// sendLimitRequest serves req on router, from the visitor it names.
func sendLimitRequest(t *testing.T, router http.Handler, req limitRequest) *httptest.ResponseRecorder {
	t.Helper()

	httpRequest := httptest.NewRequestWithContext(t.Context(), http.MethodGet, req.path, nil)
	httpRequest.Header.Set("X-Client-Ip", req.visitor)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httpRequest)

	return response
}

func TestLimits(t *testing.T) {
	t.Parallel()

	visitor := limitRequest{path: "/items", visitor: "192.0.2.1"}
	twoPerMinute := func(limits httpf.Limits) func(http.Handler) http.Handler {
		return limits.By(httpf.Rate{Requests: 2, Window: time.Minute}, httpf.VisitorKey)
	}

	testCases := []struct {
		name string

		disabled bool
		limit    func(limits httpf.Limits) func(http.Handler) http.Handler

		// allowed requests all pass; refused, when set, then gets 429 with expectRetryAfter.
		allowed []limitRequest
		refused *limitRequest

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

			allowed: append(times(2, visitor), limitRequest{path: "/items", visitor: "192.0.2.2"}),
		},
		{
			name: "Success/OtherRoute",

			limit: twoPerMinute,

			allowed: append(times(2, visitor), limitRequest{path: "/items/1", visitor: "192.0.2.1"}),
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

			allowed: []limitRequest{{path: "/items/1", visitor: "192.0.2.1"}, {path: "/items/2", visitor: "192.0.2.1"}},
			refused: &limitRequest{path: "/items/3", visitor: "192.0.2.1"},

			expectRetryAfter: "60",
		},
		{
			// One IPv6 client controls its whole /64.
			name: "Error/By/IPv6Prefix",

			limit: twoPerMinute,

			allowed: []limitRequest{{path: "/items", visitor: "2001:db8::1"}, {path: "/items", visitor: "2001:db8::2"}},
			refused: &limitRequest{path: "/items", visitor: "2001:db8::3"},

			expectRetryAfter: "60",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := httpf.ServerConfigDefault
			config.RateLimitsEnabled = !testCase.disabled

			ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

			router := chi.NewRouter()
			router.Use(middleware.ClientIPFromHeader("X-Client-Ip"))
			router.Group(func(api chi.Router) {
				api.Use(testCase.limit(httpf.NewLimits(config)))
				api.Get("/items", ok)
				api.Get("/items/{id}", ok)
			})

			for _, req := range testCase.allowed {
				require.Equal(t, http.StatusOK, sendLimitRequest(t, router, req).Code, req.path)
			}

			if testCase.refused != nil {
				response := sendLimitRequest(t, router, *testCase.refused)
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

func TestLimitConcurrencyBy(t *testing.T) {
	t.Parallel()

	errKey := errors.New("no key")

	release := make(chan struct{})
	started := make(chan struct{}, 2)

	// Both routes share the cap of one request per caller, named by the Caller header.
	limit := httpf.LimitConcurrencyBy(1, func(r *http.Request) (string, error) {
		caller := r.Header.Get("Caller")
		if caller == "" {
			return "", errKey
		}

		return caller, nil
	})

	slow := func(w http.ResponseWriter, _ *http.Request) {
		started <- struct{}{}

		<-release
		w.WriteHeader(http.StatusOK)
	}

	router := chi.NewRouter()
	router.With(limit).Get("/first", slow)
	router.With(limit).Get("/second", slow)

	send := func(path, caller string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		if caller != "" {
			request.Header.Set("Caller", caller)
		}

		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		return response
	}

	held := make(chan *httptest.ResponseRecorder, 2)

	go func() { held <- send("/first", "marie") }()
	go func() { held <- send("/first", "paul") }()

	<-started
	<-started

	// Marie holds her one slot, on either route; Paul's request does not count against her.
	refused := send("/second", "marie")
	require.Equal(t, http.StatusTooManyRequests, refused.Code)
	require.Equal(t, "1", refused.Header().Get("Retry-After"))

	require.Equal(t, http.StatusBadRequest, send("/first", "").Code)

	close(release)
	require.Equal(t, http.StatusOK, (<-held).Code)
	require.Equal(t, http.StatusOK, (<-held).Code)

	// Her slot freed up once her request returned.
	require.Equal(t, http.StatusOK, send("/second", "marie").Code)
}

func TestBodyKeyRate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		body string

		expect    string
		expectErr error
	}{
		{name: "Success/Plain", body: `{"email":"a@example.com"}`, expect: "email:a@example.com"},
		{name: "Success/CaseAndSpace", body: `{"email":" A@Example.com "}`, expect: "email:a@example.com"},
		// Escapes cannot spell the same target a second way.
		{name: "Success/Escapes", body: `{"email":"\u0041@example.com"}`, expect: "email:a@example.com"},
		{name: "Success/Number", body: `{"email":3}`, expect: "email:3"},
		{name: "Success/MissingField", body: `{"name":"a"}`, expect: "email:"},
		{name: "Success/Malformed", body: `{"email":`, expect: "email:"},
		{
			name:      "Error/TooLong",
			body:      `{"email":"` + strings.Repeat("a", 1024) + `"}`,
			expectErr: httpf.ErrInvalidBodyKey,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequestWithContext(
				t.Context(), http.MethodPut, "/", strings.NewReader(testCase.body),
			)

			key, err := httpf.BodyKeyRate("email")(request)
			require.ErrorIs(t, err, testCase.expectErr)
			require.Equal(t, testCase.expect, key)

			// The handler still reads the whole body.
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.Equal(t, testCase.body, string(body))
		})
	}

	t.Run("Error/TooLong/Refused", func(t *testing.T) {
		t.Parallel()

		router := chi.NewRouter()
		router.Group(func(api chi.Router) {
			rate := httpf.Rate{Requests: 1, Window: time.Minute}
			api.Use(httpf.NewLimits(httpf.ServerConfigDefault).By(rate, httpf.BodyKeyRate("email")))
			api.Put("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		})

		body := strings.NewReader(`{"email":"` + strings.Repeat("a", 1024) + `"}`)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/", body))

		require.Equal(t, http.StatusBadRequest, response.Code)
	})
}
