package application

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

// ExecutionMetadata captures complete operational telemetry for a request.
// It is intentionally separated from business data (TaskResult) so FinOps,
// SecOps, and DevOps can analyze execution performance without corrupting domain models.
type ExecutionMetadata struct {
	RequestID        string                  `json:"request_id"`
	TenantID         string                  `json:"tenant_id"`
	ContractVersion  string                  `json:"contract_version"`
	TaskType         domain.TaskType         `json:"task_type"`
	Provider         string                  `json:"provider"`
	Model            string                  `json:"model,omitempty"`
	Status           domain.ExecutionStatus  `json:"status"`
	ValidationStatus domain.ValidationStatus `json:"validation_status"`

	// ErrorKind indicates the type of error that occurred during execution. It is empty on success. This field is crucial for understanding the nature of failures and for implementing appropriate retry or fallback strategies.
	ErrorKind domain.Kind `json:"error_kind,omitempty"`

	// Latency measurements are crucial for understanding the performance of the gateway and the provider. They help identify bottlenecks and optimize the system. The latencies are measured in milliseconds to provide a fine-grained view of the performance.
	// e.g., if the provider latency is high, it may indicate that the provider is under heavy load or that the request is complex. If the validation latency is high, it may indicate that the validation logic needs optimization.
	ProviderLatencyMs     float64 `json:"provider_latency_ms"`
	ValidationLatencyMs   float64 `json:"validation_latency_ms"`
	TotalGatewayLatencyMs float64 `json:"total_gateway_latency_ms"`

	// Token counts and estimated costs are important for billing and resource management. They help track how many tokens were consumed by the request and estimate the cost incurred. This information is useful for both the service provider and the customer to manage usage and costs effectively.
	InputTokens      int64   `json:"input_tokens,omitempty"`
	OutputTokens     int64   `json:"output_tokens,omitempty"`
	TotalTokens      int64   `json:"total_tokens,omitempty"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd,omitempty"`
	Attempts         int     `json:"attempts"`
}

// newEventID generates a cryptographically secure 128-bit random hex string.
func newEventID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

// elapsedMilliseconds converts a time duration into fractional millisecond precision.
func elapsedMilliseconds(start, end time.Time) float64 {
	return float64(end.Sub(start).Microseconds()) / 1000.0
}
