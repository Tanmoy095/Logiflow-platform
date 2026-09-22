package domain

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// CurrentUsageEventVersion is the wire contract version for AIUsageEvent.
// Bump this when the schema changes in a way consumers must react to.
const CurrentUsageEventVersion = "llm-gateway.usage.v1"

// AIUsageEvent is the gateway's privacy-safe "receipt" for a model execution.
//
// It deliberately contains metadata, not model content. In particular,
// prompts, completions, retrieved chunks, raw evidence, and provider secrets
// must NEVER be copied into this event. The event can therefore travel
// through Kafka/analytics/billing systems without becoming a second
// sensitive-data store.
//
// Serialization tags live in the adapter layer, not here — the domain type
// only describes what the fact IS, not how it is encoded.
type AIUsageEvent struct {
	// ---- Tracing & security metadata -------------------------------------
	EventID      string   `json:"event_id"`
	EventVersion string   `json:"event_version"`
	TenantID     string   `json:"tenant_id"`
	RequestID    string   `json:"request_id"`
	TaskType     TaskType `json:"task_type"`

	// ---- Model execution details ----------------------------------------
	//
	// When the application uses a ProviderRouter, Provider and Model
	// identify the FINAL provider in the fallback chain, not the primary.
	// Attempts reflects the aggregate across retries and fallbacks.
	Provider         string           `json:"provider,omitempty"`
	Model            string           `json:"model,omitempty"`
	Status           ExecutionStatus  `json:"status"`
	ValidationStatus ValidationStatus `json:"validation_status"`

	// ---- Financial & token accounting ------------------------------------
	//
	// Attempts is included because billing cares about true vendor call
	// economics: a request that succeeded on attempt 3 cost more than one
	// that succeeded on attempt 1.
	InputTokens      int64     `json:"input_tokens,omitempty"`
	OutputTokens     int64     `json:"output_tokens,omitempty"`
	TotalTokens      int64     `json:"total_tokens,omitempty"`
	EstimatedCostUSD float64   `json:"estimated_cost_usd,omitempty"`
	Attempts         int       `json:"attempts"`
	OccurredAt       time.Time `json:"occurred_at"`
}

// Validate protects the event contract before an infrastructure publisher
// serializes it onto an event broker.
//
// Checks are ordered cheapest-first and all use TrimSpace so whitespace-only
// strings are rejected alongside empties.
func (e AIUsageEvent) Validate() error {
	if strings.TrimSpace(e.EventID) == "" {
		return fmt.Errorf("event_id must not be empty")
	}
	if strings.TrimSpace(e.EventVersion) == "" {
		return fmt.Errorf("event_version must not be empty")
	}
	if strings.TrimSpace(e.TenantID) == "" {
		return fmt.Errorf("tenant_id must not be empty")
	}
	if strings.TrimSpace(e.RequestID) == "" {
		return fmt.Errorf("request_id must not be empty")
	}
	if strings.TrimSpace(string(e.TaskType)) == "" {
		return fmt.Errorf("task_type must not be empty")
	}

	// Status and ValidationStatus must be populated. Downstream consumers
	// key off these values for billing and SLO calculation — an empty
	// string silently breaks aggregation.
	if strings.TrimSpace(string(e.Status)) == "" {
		return fmt.Errorf("status must not be empty")
	}
	if strings.TrimSpace(string(e.ValidationStatus)) == "" {
		return fmt.Errorf("validation_status must not be empty")
	}

	// Token arithmetic must be internally consistent. This catches wiring
	// bugs where the provider or fake sets TotalTokens independently of
	// Input+Output.
	if e.InputTokens < 0 || e.OutputTokens < 0 || e.TotalTokens < 0 {
		return fmt.Errorf("token counts must not be negative")
	}
	if e.TotalTokens != e.InputTokens+e.OutputTokens {
		return fmt.Errorf("total_tokens must equal input_tokens + output_tokens")
	}

	// Cost must be a finite non-negative number. NaN or Inf would corrupt
	// billing pipelines silently.
	if math.IsNaN(e.EstimatedCostUSD) ||
		math.IsInf(e.EstimatedCostUSD, 0) ||
		e.EstimatedCostUSD < 0 {
		return fmt.Errorf("estimated_cost_usd must be finite and non-negative")
	}

	// Attempts must be non-negative. A negative value indicates a
	// misconfigured provider or fake.
	if e.Attempts < 0 {
		return fmt.Errorf("attempts must not be negative")
	}

	if e.OccurredAt.IsZero() {
		return fmt.Errorf("occurred_at must not be zero")
	}

	return nil
}
