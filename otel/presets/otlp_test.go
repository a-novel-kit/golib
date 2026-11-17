package otelpresets_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	collectorlogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"

	"github.com/a-novel-kit/golib/httpf"
	"github.com/a-novel-kit/golib/otel"
	otelpresets "github.com/a-novel-kit/golib/otel/presets"
)

const serviceNameKey = "service.name"

// otlpExport is one request an OTLP/HTTP backend received.
type otlpExport struct {
	auth     string
	encoding string
	body     []byte
}

// otlpReceiver stands in for an OTLP/HTTP backend. It keeps each export it receives, by path.
type otlpReceiver struct {
	mu      sync.Mutex
	exports map[string][]otlpExport
}

func (receiver *otlpReceiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	receiver.mu.Lock()
	receiver.exports[r.URL.Path] = append(receiver.exports[r.URL.Path], otlpExport{
		auth:     r.Header.Get("Authorization"),
		encoding: r.Header.Get("Content-Encoding"),
		body:     body,
	})
	receiver.mu.Unlock()

	w.Header().Set("Content-Type", "application/x-protobuf")
}

// decodeExports checks that each export carries the header and the compression the variables set,
// then decodes it into a new message.
func decodeExports[M proto.Message](t *testing.T, exports []otlpExport, newMessage func() M) []M {
	t.Helper()

	messages := make([]M, 0, len(exports))

	for _, export := range exports {
		require.Equal(t, "Basic b3RscDp0ZXN0", export.auth)
		require.Equal(t, "gzip", export.encoding)

		reader, err := gzip.NewReader(bytes.NewReader(export.body))
		require.NoError(t, err)

		body, err := io.ReadAll(reader)
		require.NoError(t, err)

		message := newMessage()
		require.NoError(t, proto.Unmarshal(body, message))

		messages = append(messages, message)
	}

	return messages
}

// serviceName returns the service.name attribute of a resource.
func serviceName(attributes []*commonv1.KeyValue) string {
	for _, attribute := range attributes {
		if attribute.GetKey() == serviceNameKey {
			return attribute.GetValue().GetStringValue()
		}
	}

	return ""
}

// defaultServiceName returns the service name of the SDK's default resource, which reads
// OTEL_SERVICE_NAME. The SDK computes that resource once per process, so a test cannot set it.
func defaultServiceName() string {
	value, _ := resource.Default().Set().Value(attribute.Key(serviceNameKey))

	return value.AsString()
}

func TestOTLP(t *testing.T) {
	t.Run("Success/Export", func(t *testing.T) {
		receiver := &otlpReceiver{exports: map[string][]otlpExport{}}
		server := httptest.NewServer(receiver)
		t.Cleanup(server.Close)

		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
		t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=Basic b3RscDp0ZXN0")
		t.Setenv("OTEL_EXPORTER_OTLP_COMPRESSION", "gzip")

		restoreGlobals(t)

		config := &otelpresets.OTLP{}
		require.NoError(t, otel.Init(config))

		handler := httpf.NewMux(httpf.ServerConfigDefault, nil, nil)
		handler.Get("/{path}", func(_ http.ResponseWriter, r *http.Request) {
			_, span := otel.Tracer().Start(r.Context(), "rest.Handle")
			span.End()
		})

		// The router leaves its liveness path untraced, and the preset's sampler drops the spans
		// opened under it.
		for _, path := range []string{httpf.ServerConfigDefault.Liveness, "/items"} {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
			handler.ServeHTTP(httptest.NewRecorder(), request)
		}

		otel.Logger().InfoContext(t.Context(), "exported")

		// Flush shuts the providers down, which sends what they still buffer.
		config.Flush()

		var traceIDs, spanNames []string

		traceRequests := decodeExports(t, receiver.exports["/v1/traces"], func() *collectortrace.ExportTraceServiceRequest {
			return &collectortrace.ExportTraceServiceRequest{}
		})
		for _, request := range traceRequests {
			for _, resourceSpans := range request.GetResourceSpans() {
				require.Equal(t, defaultServiceName(), serviceName(resourceSpans.GetResource().GetAttributes()))

				for _, scopeSpans := range resourceSpans.GetScopeSpans() {
					for _, span := range scopeSpans.GetSpans() {
						traceIDs = append(traceIDs, string(span.GetTraceId()))
						spanNames = append(spanNames, span.GetName())
					}
				}
			}
		}

		// The probe leaves no span: the server span and its child both belong to the request to /items.
		require.Len(t, spanNames, 2)
		require.Contains(t, spanNames, "rest.Handle")
		require.Equal(t, traceIDs[0], traceIDs[1])

		var logBodies []string

		logRequests := decodeExports(t, receiver.exports["/v1/logs"], func() *collectorlogs.ExportLogsServiceRequest {
			return &collectorlogs.ExportLogsServiceRequest{}
		})
		for _, request := range logRequests {
			for _, resourceLogs := range request.GetResourceLogs() {
				require.Equal(t, defaultServiceName(), serviceName(resourceLogs.GetResource().GetAttributes()))

				for _, scopeLogs := range resourceLogs.GetScopeLogs() {
					for _, record := range scopeLogs.GetLogRecords() {
						logBodies = append(logBodies, record.GetBody().GetStringValue())
					}
				}
			}
		}

		require.Equal(t, []string{"exported"}, logBodies)
	})
}
