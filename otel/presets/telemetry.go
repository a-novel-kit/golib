package otelpresets

import (
	"os"
	"strconv"
	"time"

	"github.com/a-novel-kit/golib/config"
	"github.com/a-novel-kit/golib/logging"
	loggingpresets "github.com/a-novel-kit/golib/logging/presets"
	"github.com/a-novel-kit/golib/otel"
)

const (
	// ServerFlushTimeout bounds a server's wait on exit for its pending spans and logs. It fits, with
	// the drain of httpf and grpcf, in the 30 seconds Kubernetes leaves a pod before it kills it.
	ServerFlushTimeout = 2 * time.Second
	// JobFlushTimeout bounds a job's wait on exit for its pending spans and logs. A job often ends
	// before the network lets its first connection out. The exporter then sends the spans, then the
	// logs, within 30 seconds each.
	JobFlushTimeout = time.Minute
)

// Telemetry is the OpenTelemetry backend of a process, with the loggers that match it.
type Telemetry struct {
	// Otel exports the spans and logs of the process. Pass it to otel.Init, and defer its Flush.
	Otel otel.Config
	// Logger writes the logs of the application.
	Logger logging.Log
	// HTTPLogger writes a line per REST request. It is nil when the server spans record each request.
	HTTPLogger logging.HTTPConfig
	// RPCLogger writes a line per gRPC call, and logs a recovered panic.
	RPCLogger logging.RPCConfig
}

// LoadTelemetry reads OTEL through getenv, which applies the service's prefix, and the standard
// OTEL_EXPORTER_OTLP_ENDPOINT, which the OpenTelemetry SDK reads unprefixed. It picks:
//
//   - without OTEL, the [Disabled] exporter and loggers that write to a terminal;
//   - with OTEL and no endpoint, the [Local] exporter and the same loggers;
//   - with both, the [OTLP] exporter and loggers that write through it. The server spans then record
//     each request, so no request log is written.
//
// The exporter flushes within flushTimeout, such as [ServerFlushTimeout] or [JobFlushTimeout]. It
// panics on an OTEL value it cannot parse.
func LoadTelemetry(getenv func(name string) string, flushTimeout time.Duration) Telemetry {
	enabled := config.LoadEnv(getenv("OTEL"), false, strconv.ParseBool)

	if enabled && os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		return Telemetry{
			Otel:      &OTLP{FlushTimeout: flushTimeout},
			Logger:    &loggingpresets.LogOTel{},
			RPCLogger: &loggingpresets.GRPCOTel{},
		}
	}

	terminal := &loggingpresets.LogLocal{Out: os.Stdout}
	telemetry := Telemetry{
		Otel:       &Disabled{},
		Logger:     terminal,
		HTTPLogger: &loggingpresets.HTTPLocal{BaseLogger: terminal},
		RPCLogger:  &loggingpresets.GRPCLocal{},
	}

	if enabled {
		telemetry.Otel = &Local{FlushTimeout: flushTimeout}
	}

	return telemetry
}
