package domain

// TODO: define domain events

import "time"

// AIUsageEvent is the domain-level fact produced after an AI execution attempt
// has enough information to describe its outcome.
//
// It is intentionally metadata-only. Never put raw prompts, retrieved document
// chunks, provider secrets, or full evidence contents into this event. The event
// may eventually cross an asynchronous boundary (for example Kafka), so keeping
// sensitive payloads out of it makes replay, retention and analytics safer.
type AIUsageEvent struct {
	// Tracing & Security Metadata
	EventID      string   `json:"event_id"`
	EventVersion string   `json:"event_version"`
	TenantID     string   `json:"tenant_id"`
	RequestID    string   `json:"request_id"`
	TaskType     TaskType `json:"task_type"`

	// Model Execution Details
	Provider         string           `json:"provider,omitempty"` // e.g., "openai"
	Model            string           `json:"model,omitempty"`    // e.g., "gpt-4o"
	Status           ExecutionStatus  `json:"status"`             // e.g., "succeeded"
	ValidationStatus ValidationStatus `json:"validation_status"`  // e.g., "passed"

	// Financial & Token Accounting
	InputTokens      int64     `json:"input_tokens,omitempty"`
	OutputTokens     int64     `json:"output_tokens,omitempty"`
	TotalTokens      int64     `json:"total_tokens,omitempty"`
	EstimatedCostUSD float64   `json:"estimated_cost_usd,omitempty"`
	OccurredAt       time.Time `json:"occurred_at"`
}

const CurrentUsageEventVersion = "llm-gateway.usage.v1"

// Validate prevents obviously malformed events from reaching a publisher.
// Publishing itself is an infrastructure/application responsibility.
func (e AIUsageEvent) Validate() error {
	if e.EventID == "" {
		return ErrInvalidEvent("event_id must not be empty")
	}
	if e.EventVersion == "" {
		return ErrInvalidEvent("event_version must not be empty")
	}
	if e.TenantID == "" {
		return ErrInvalidEvent("tenant_id must not be empty")
	}
	if e.RequestID == "" {
		return ErrInvalidEvent("request_id must not be empty")
	}
	if e.TaskType == "" {
		return ErrInvalidEvent("task_type must not be empty")
	}
	if e.Status == "" {
		return ErrInvalidEvent("status must not be empty")
	}
	if e.ValidationStatus == "" {
		return ErrInvalidEvent("validation_status must not be empty")
	}
	if e.OccurredAt.IsZero() {
		return ErrInvalidEvent("occurred_at must not be zero")
	}
	if e.InputTokens < 0 || e.OutputTokens < 0 || e.TotalTokens < 0 {
		return ErrInvalidEvent("token counts must not be negative")
	}
	if e.EstimatedCostUSD < 0 {
		return ErrInvalidEvent("estimated_cost_usd must not be negative")
	}
	return nil
}

// InvalidEventError is intentionally small. A publisher can map this to its
// transport-specific failure without exposing transport details here.
type InvalidEventError struct{ Message string }

func (e InvalidEventError) Error() string  { return e.Message }
func ErrInvalidEvent(message string) error { return InvalidEventError{Message: message} }
