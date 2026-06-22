package observability

import (
    "log/slog"
    "os"
)

// Bootstrap holds all telemetry components for a service.
// It currently provides a structured logger; soon it will also hold
// a Prometheus meter, OpenTelemetry tracer, etc.
type Bootstrap struct {
    Logger *slog.Logger
}

// New creates a Bootstrap configured for the given service name.
// The logger writes JSON to stdout, which is the container/Docker convention.
func New(serviceName string) *Bootstrap {
    handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    })
    logger := slog.New(handler).With("service", serviceName)

    return &Bootstrap{
        Logger: logger,
    }
}