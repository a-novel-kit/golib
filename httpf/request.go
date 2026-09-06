package httpf

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrJSONMultipleValues is returned when a request body contains more than one JSON value.
var ErrJSONMultipleValues = errors.New("JSON request body contains multiple values")

// DecodeJSON decodes one JSON value from body into destination. It preserves encoding/json's
// compatibility behavior and returns an error when body is empty, malformed, or contains another
// value. Handlers retain responsibility for validating destination and writing error responses.
//
//	var request requestCreate
//	if err := httpf.DecodeJSON(r.Body, &request); err != nil {
//		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
//		return
//	}
func DecodeJSON(body io.Reader, destination any) error {
	decoder := json.NewDecoder(body)

	err := decoder.Decode(destination)
	if err != nil {
		return fmt.Errorf("decode JSON request body: %w", err)
	}

	err = decoder.Decode(&struct{}{})
	// Decode returns io.EOF after one JSON value and trailing whitespace.
	if !errors.Is(err, io.EOF) {
		if err == nil {
			// A successful second decode means the body held another JSON value.
			return ErrJSONMultipleValues
		}

		// Preserve parsing and body-size errors for callers that classify them with errors.Is or errors.As.
		return fmt.Errorf("decode JSON request body: %w", err)
	}

	return nil
}
