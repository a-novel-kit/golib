package httpf_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/httpf"
)

func TestInvalidFieldsTag(t *testing.T) {
	t.Parallel()

	type scene struct {
		ID string `validate:"isdefault"`
	}

	// A field breaks its rule once set, so each case sets only the field it names.
	type request struct {
		Name       string   `validate:"isdefault"`
		ID         string   `validate:"isdefault"`
		ProjectID  string   `validate:"isdefault"`
		HTTPServer string   `validate:"isdefault"`
		IDs        []string `validate:"isdefault"`
		URLs       []string `validate:"isdefault"`
		UUIDs      []string `validate:"dive,isdefault"`
		Scenes     []scene  `validate:"dive"`
	}

	testCases := []struct {
		name string

		request request

		expect string
	}{
		{name: "Word", request: request{Name: "set"}, expect: "name"},
		{name: "Initialism", request: request{ID: "set"}, expect: "id"},
		{name: "TrailingInitialism", request: request{ProjectID: "set"}, expect: "projectID"},
		{name: "LeadingInitialism", request: request{HTTPServer: "set"}, expect: "httpServer"},
		{name: "PluralInitialism", request: request{IDs: []string{}}, expect: "ids"},
		{name: "PluralInitialism/Long", request: request{URLs: []string{}}, expect: "urls"},
		{name: "PluralInitialism/Indexed", request: request{UUIDs: []string{"set"}}, expect: "uuids[0]"},
		{name: "Nested", request: request{Scenes: []scene{{ID: "set"}}}, expect: "scenes[0].id"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			errValidation := validator.New().Struct(testCase.request)

			httpf.HandleError(
				t.Context(), &recordingLog{}, w, trace.SpanFromContext(t.Context()),
				httpf.ErrMap{nil: http.StatusBadRequest}, errValidation,
			)

			var body struct {
				Tags map[string]map[string]string `json:"tags"`
			}

			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, map[string]string{testCase.expect: "isdefault"}, body.Tags[httpf.InvalidFieldsTag])
		})
	}
}
