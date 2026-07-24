// Package httpftest provides a scripted HTTP server for testing the calls a service makes through
// [github.com/a-novel-kit/golib/httpf.NewClient].
//
// A test builds one with the replies it wants, points the client at it, and afterwards reads back
// every request the server received. Two of the replies exist for failures no recorded fixture can
// reproduce — a server that accepts a request and then goes quiet, and one that drops the
// connection without answering — because those are the failures a timeout, a retry, or a lease is
// written to survive, and they are otherwise only reachable in production.
//
// It lives in regular (non-_test.go) files so tests in other packages can import it: Go excludes
// _test.go files from a package's exported surface.
package httpftest

import (
	"cmp"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"
)

// Response is one scripted reply. The zero value answers 200 with an empty body.
type Response struct {
	// Status is the code to write. Zero writes http.StatusOK.
	Status int
	// Header is written before the status line.
	Header http.Header
	// Body is written after it.
	Body string

	// Delay holds the reply back before anything is written, which is how a test reaches a
	// caller's deadline without waiting out a real server's latency.
	Delay time.Duration

	// Hang holds the reply open until the request's context is cancelled, and writes nothing. It
	// stands for a server that accepted the request and then stopped answering.
	Hang bool

	// Drop closes the connection without writing a reply, which surfaces to the caller as a
	// transport error rather than a status code.
	Drop bool
}

// Request is one request the server received, captured before its reply was chosen.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// A Server replays the responses it was built with, one per request in order, and records
// everything it received. Build one with [NewServer]; it shuts down with the test that made it.
type Server struct {
	t *testing.T

	server *httptest.Server

	mu        sync.Mutex
	responses []Response
	requests  []Request
}

// NewServer starts a server that replays responses in order. A request arriving after the last
// scripted reply fails the test rather than being answered with something nobody chose.
func NewServer(t *testing.T, responses ...Response) *Server {
	t.Helper()

	scripted := &Server{t: t, responses: responses}

	scripted.server = httptest.NewServer(http.HandlerFunc(scripted.handle))
	t.Cleanup(scripted.server.Close)

	return scripted
}

// URL is the base address to point a client at.
func (scripted *Server) URL() string {
	return scripted.server.URL
}

// Requests returns what the server received, in order.
func (scripted *Server) Requests() []Request {
	scripted.mu.Lock()
	defer scripted.mu.Unlock()

	return slices.Clone(scripted.requests)
}

func (scripted *Server) handle(writer http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		scripted.t.Errorf("httpftest: read request body: %v", err)
	}

	response, ok := scripted.take(Request{
		Method: request.Method,
		Path:   request.URL.Path,
		Query:  request.URL.Query(),
		Header: request.Header.Clone(),
		Body:   body,
	})
	if !ok {
		// Errorf rather than Fatalf: this runs on the server's goroutine, and only Errorf is safe
		// to call from one other than the test's own.
		scripted.t.Errorf("httpftest: unscripted request %s %s", request.Method, request.URL.Path)
		writer.WriteHeader(http.StatusInternalServerError)

		return
	}

	if response.Drop {
		scripted.drop(writer)

		return
	}

	if response.Delay > 0 && !wait(request, response.Delay) {
		return
	}

	if response.Hang {
		<-request.Context().Done()

		return
	}

	for key, values := range response.Header {
		for _, value := range values {
			writer.Header().Add(key, value)
		}
	}

	writer.WriteHeader(cmp.Or(response.Status, http.StatusOK))

	_, err = io.WriteString(writer, response.Body)
	if err != nil {
		scripted.t.Errorf("httpftest: write response body: %v", err)
	}
}

func (scripted *Server) drop(writer http.ResponseWriter) {
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		scripted.t.Errorf("httpftest: response writer cannot be hijacked, so the connection cannot be dropped")

		return
	}

	conn, _, err := hijacker.Hijack()
	if err != nil {
		scripted.t.Errorf("httpftest: hijack connection: %v", err)

		return
	}

	_ = conn.Close()
}

// take appends the request and pops the reply scripted for it, reporting false once the script has
// run out.
func (scripted *Server) take(request Request) (Response, bool) {
	scripted.mu.Lock()
	defer scripted.mu.Unlock()

	scripted.requests = append(scripted.requests, request)

	if len(scripted.responses) == 0 {
		return Response{}, false
	}

	response := scripted.responses[0]
	scripted.responses = scripted.responses[1:]

	return response, true
}

// wait waits out duration, reporting whether it finished rather than the request going away.
func wait(request *http.Request, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-request.Context().Done():
		return false
	case <-timer.C:
		return true
	}
}
