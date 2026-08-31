// services/llm-gateway/application/metadata.go

package application

import (
	"crypto/rand"
	"encoding/hex"
)

// ExecutionMetadata captures operational context for a single completion.
//
// It is intentionally separate from domain.CompletionResult to keep business
// truth free of operational concerns. This struct can later be logged or
// exported to a tracing/metrics system without modifying the domain.
type ExecutionMetadata struct {
	RequestID             string  `json:"request_id"`
	Provider              string  `json:"provider"`
	PromptVersion         string  `json:"prompt_version"`
	ProviderLatencyMs     float64 `json:"provider_latency_ms"`
	ValidationLatencyMs   float64 `json:"validation_latency_ms"`
	TotalGatewayLatencyMs float64 `json:"total_gateway_latency_ms"`
	Status                string  `json:"status"`               // success, provider_error, validation_failed, invalid_argument, provider_timeout, request_canceled, provider_unavailable
	ErrorKind             string  `json:"error_kind,omitempty"` // domain.Kind value when error, otherwise empty
}

// newRequestID generates a random hex string for request correlation.
// In production this would likely come from the incoming request context.
func newRequestID() string {
	b := make([]byte, 8) // 16 hex characters
	if _, err := rand.Read(b); err != nil {
		return "unknown-request-id"
	}
	return hex.EncodeToString(b)
}
