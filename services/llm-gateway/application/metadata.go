// services/llm-gateway/application/metadata.go
package application

import (
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

// ExecutionMetadata captures complete operational telemetry for a request.
//
// It is deliberately separated from business data (TaskResult) so that
// FinOps, SecOps, and DevOps can analyze execution behavior without the
// risk of corrupting domain models. The struct is a pure DTO: no behavior,
// no dependencies on infrastructure.
//
// Every field is populated at a specific stage of Service.Complete. On
// failure paths, the fields captured up to the point of failure are
// preserved so audit and billing can still see partial progress.
type ExecutionMetadata struct {
	// ---- Identity ---------------------------------------------------------

	RequestID       string          `json:"request_id"`
	TenantID        string          `json:"tenant_id"`
	ContractVersion string          `json:"contract_version"`
	TaskType        domain.TaskType `json:"task_type"`

	// Prompt/schema versioning lets us correlate quality regressions with
	// specific template or schema changes in production.
	TaskSchemaVersion   string `json:"task_schema_version,omitempty"`
	OutputSchemaVersion string `json:"output_schema_version,omitempty"`
	PromptVersion       string `json:"prompt_version,omitempty"`

	// ---- Provider identity ------------------------------------------------

	// Provider and Model reflect whoever actually served the request.
	// When a ProviderRouter is in the stack, these identify the FINAL
	// provider in the fallback chain, not the primary.
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`

	// ---- Outcome ----------------------------------------------------------

	Status           domain.ExecutionStatus  `json:"status"`
	ValidationStatus domain.ValidationStatus `json:"validation_status"`

	// ErrorKind is empty on success. On failure it carries the stable
	// machine-readable category (see domain.Kind). This is what alerting
	// and SLO dashboards key off — never parse Error() strings.
	ErrorKind domain.Kind `json:"error_kind,omitempty"`

	// ---- Latency ----------------------------------------------------------
	//
	// All latencies are fractional milliseconds. Total = provider + validation
	// + orchestration overhead. Comparing provider vs validation latency
	// quickly reveals whether a slowdown is vendor-side or decode-side.
	ProviderLatencyMs     float64 `json:"provider_latency_ms"`
	ValidationLatencyMs   float64 `json:"validation_latency_ms"`
	TotalGatewayLatencyMs float64 `json:"total_gateway_latency_ms"`

	// ---- Usage / billing --------------------------------------------------
	//
	// Populated even on partial failures so that any tokens the vendor
	// actually charged for are reflected in the audit trail.
	InputTokens      int64   `json:"input_tokens,omitempty"`
	OutputTokens     int64   `json:"output_tokens,omitempty"`
	TotalTokens      int64   `json:"total_tokens,omitempty"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd,omitempty"`

	// Attempts is the router's aggregate: 1 for a first-try success, N for
	// a request that required retries or fallback. Billing uses this to
	// reflect true vendor call economics.
	Attempts int `json:"attempts"`
}
