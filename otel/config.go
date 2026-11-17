package otel

import (
	"fmt"
)

// Config wires one OpenTelemetry backend into the process. Each implementation targets a single
// destination for traces and logs, and the presets subpackage ships ready-made ones. httpf and grpcf
// trace every request and call through the providers it installs.
type Config interface {
	// Init installs the backend's propagator, tracer and logger providers as the process-global
	// OpenTelemetry state, and prints its startup banner.
	Init() error
	// Flush drains buffered spans and logs; run it before the process exits.
	Flush()
}

// Init installs the providers of config as the process-global OpenTelemetry state. A nil config
// leaves telemetry disabled and is a no-op.
func Init(config Config) error {
	if config == nil {
		return nil
	}

	err := config.Init()
	if err != nil {
		return fmt.Errorf("initialize otel: %w", err)
	}

	return nil
}
