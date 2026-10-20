package httpf

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrJSONMultipleValues is returned when a request body contains more than one JSON value.
var ErrJSONMultipleValues = errors.New("JSON request body contains multiple values")

// DecodeJSON decodes the one JSON value body holds into destination, with encoding/json's rules,
// so an unknown field passes. It returns an error when body is empty, malformed, or holds a second
// value. The handler still validates destination and writes the error response.
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

	// A body holding nothing past the first value ends in io.EOF.
	err = decoder.Decode(&struct{}{})

	switch {
	case errors.Is(err, io.EOF):
		return nil
	case err == nil:
		return ErrJSONMultipleValues
	default:
		return fmt.Errorf("decode JSON request body: %w", err)
	}
}
