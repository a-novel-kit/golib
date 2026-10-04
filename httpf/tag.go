package httpf

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"unicode"

	"github.com/go-playground/validator/v10"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
)

// InvalidFieldsTag is the tag HandleError sets on a client error caused by a validator error. It
// maps the lower camel path of each invalid field ("email", "roles[0]") to the tag of the rule it
// broke ("required").
const InvalidFieldsTag = "invalidFields"

// errUnencodableTag reports a tag value that json cannot encode.
var errUnencodableTag = errors.New("unencodable tag")

// taggedError carries the tags a handler attached to an error.
type taggedError struct {
	error

	tags map[string]any
}

func (err *taggedError) Unwrap() error {
	return err.error
}

// WithTag attaches a tag to err, which HandleError returns to the client in the response body. A
// nil value sends key as true; any other value is sent as is, so it must hold only data the client
// may see. Tagging an error that already carries tags adds to them.
//
// Call it in the handler, on the error it is about to pass to HandleError.
func WithTag(err error, key string, value any) error {
	if err == nil {
		return nil
	}

	if value == nil {
		value = true
	}

	var tagged *taggedError
	if !errors.As(err, &tagged) {
		tagged = &taggedError{error: err, tags: map[string]any{}}
		err = tagged
	}

	tagged.tags[key] = value

	return err
}

// problem is an RFC 9457 problem details body. Its "about:blank" type makes the title the status
// text; tags extends it with the tags of the error.
type problem struct {
	Type   string                     `json:"type"`
	Title  string                     `json:"title"`
	Status int                        `json:"status"`
	Tags   map[string]json.RawMessage `json:"tags"`
}

// problemBody returns the RFC 9457 body for err under status, or nil when err carries no tags. A
// tag that json cannot encode is left out and reported on the span.
func problemBody(span trace.Span, err error, status int) []byte {
	tags := map[string]any{}

	var tagged *taggedError
	if errors.As(err, &tagged) {
		maps.Copy(tags, tagged.tags)
	}

	// A server error can come from a validator rejecting the server's own data, whose fields stay
	// internal.
	var fields validator.ValidationErrors
	if _, set := tags[InvalidFieldsTag]; !set && status < http.StatusInternalServerError && errors.As(err, &fields) {
		tags[InvalidFieldsTag] = invalidFields(fields)
	}

	encoded := make(map[string]json.RawMessage, len(tags))

	for key, value := range tags {
		raw, encodeErr := json.Marshal(value)
		if encodeErr != nil {
			_ = otel.ReportError(span, fmt.Errorf("%w %q: %w", errUnencodableTag, key, encodeErr))

			continue
		}

		encoded[key] = raw
	}

	if len(encoded) == 0 {
		return nil
	}

	body, err := json.Marshal(problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Tags:   encoded,
	})
	if err != nil {
		_ = otel.ReportError(span, fmt.Errorf("encode problem body: %w", err))

		return nil
	}

	return body
}

// invalidFields maps the lower camel path of each field in fields to the tag of the rule it broke.
func invalidFields(fields validator.ValidationErrors) map[string]string {
	paths := make(map[string]string, len(fields))

	for _, field := range fields {
		// The namespace starts with the request type, which is not a field of the request.
		_, path, _ := strings.Cut(field.Namespace(), ".")
		segments := strings.Split(path, ".")

		for i, segment := range segments {
			segments[i] = lowerCamel(segment)
		}

		paths[strings.Join(segments, ".")] = field.Tag()
	}

	return paths
}

// lowerCamel lowers the leading capital of a Go name, or its whole leading initialism: "UserID"
// becomes "userID", "ID" becomes "id", "URLPath" becomes "urlPath".
func lowerCamel(name string) string {
	runes := []rune(name)

	upper := 0
	for upper < len(runes) && unicode.IsUpper(runes[upper]) {
		upper++
	}

	// When an initialism precedes a word, its last capital starts that word.
	if upper > 1 && upper < len(runes) && unicode.IsLower(runes[upper]) {
		upper--
	}

	return strings.ToLower(string(runes[:upper])) + string(runes[upper:])
}
