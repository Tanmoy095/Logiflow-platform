package application

import (
	"fmt"
	"strings"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

// CurrentContractVersion is the only application contract version this
// gateway currently accepts. Bump this when the CompleteCommand shape
// changes in a way callers must react to.
const CurrentContractVersion = "ai-completion.v1"

// CompleteCommand is the transport-neutral application use-case contract.
//
// HTTP/gRPC/MCP adapters translate their own request shape into this struct.
// Keeping it transport-neutral prevents protocol details from leaking into
// business orchestration and makes the use case easy to test.
//
// It is intentionally richer than ProviderRequest because the application
// boundary needs tenant, correlation, evidence, and capability semantics.
//
// It is intentionally NOT a ChatGPT-style arbitrary prompt request.
// TaskType selects a registered, governed capability — callers cannot
// invoke arbitrary work.
type CompleteCommand struct {
	ContractVersion     string
	TenantID            string
	RequestID           string
	IdempotencyKey      string
	TaskType            domain.TaskType
	TaskSchemaVersion   string
	Prompt              string
	PromptVersion       string
	OutputSchemaVersion string
	MaxTokens           int
	Deadline            time.Time
	Subjects            []domain.EntityReference
	EvidenceRefs        []domain.EvidenceReference
}

// Validate performs only capability-independent validation.
//
// Capability-specific rules (e.g. "shipment_delay_risk requires exactly one
// shipment subject") live in the TaskDefinition resolved from the registry.
// Keeping them out of here prevents this method from becoming a growing
// switch statement over every supported TaskType.
//
// Do not move shipment-risk-specific rules into this method — they belong
// in the task definition and the shipmentrisk domain policy.
func (c CompleteCommand) Validate() error {
	if strings.TrimSpace(c.ContractVersion) == "" {
		return fmt.Errorf("contract_version must not be empty")
	}
	if c.ContractVersion != CurrentContractVersion {
		return fmt.Errorf("unsupported contract_version %q", c.ContractVersion)
	}
	if strings.TrimSpace(c.TenantID) == "" {
		return fmt.Errorf("tenant_id must not be empty")
	}
	if strings.TrimSpace(c.RequestID) == "" {
		return fmt.Errorf("request_id must not be empty")
	}
	if strings.TrimSpace(string(c.TaskType)) == "" {
		return fmt.Errorf("task_type must not be empty")
	}
	if strings.TrimSpace(c.TaskSchemaVersion) == "" {
		return fmt.Errorf("task_schema_version must not be empty")
	}
	if strings.TrimSpace(c.Prompt) == "" {
		return fmt.Errorf("prompt must not be empty")
	}
	if strings.TrimSpace(c.PromptVersion) == "" {
		return fmt.Errorf("prompt_version must not be empty")
	}
	if strings.TrimSpace(c.OutputSchemaVersion) == "" {
		return fmt.Errorf("output_schema_version must not be empty")
	}
	if c.MaxTokens < 0 {
		return fmt.Errorf("max_tokens must not be negative")
	}

	for i, subject := range c.Subjects {
		if err := subject.Validate(); err != nil {
			return fmt.Errorf("subjects[%d]: %w", i, err)
		}
	}
	for i, evidence := range c.EvidenceRefs {
		if err := evidence.Validate(); err != nil {
			return fmt.Errorf("evidence_refs[%d]: %w", i, err)
		}
	}

	return nil
}

// ProviderRequest is deliberately smaller than CompleteCommand. The provider
// should receive only the fields it needs for inference.
//
// This is a security/privacy boundary as well as an anti-corruption layer:
// internal tenant and business identifiers must never accidentally become
// vendor-facing fields.
type ProviderRequest struct {
	TaskType            domain.TaskType
	Prompt              string
	PromptVersion       string
	OutputSchemaVersion string
	MaxTokens           int
}

// ProviderResponse contains raw provider output plus non-sensitive execution
// metadata.
//
// RawOutput remains untrusted until the application layer validates it against
// the capability schema and domain policy. The other fields (Provider, Model,
// tokens, cost, Attempts) are telemetry used by the service for billing,
// analytics, and SLO tracking — they are NOT trusted business data.
//
// Provider identity semantics: when a ProviderRouter is in the stack, Provider
// and Model identify the FINAL provider in the fallback chain, not the primary
// that was tried first. Attempts is the aggregate across all retries and
// fallbacks for the entire request.
type ProviderResponse struct {
	RawOutput        string
	Provider         string
	Model            string
	InputTokens      int64
	OutputTokens     int64
	TotalTokens      int64
	EstimatedCostUSD float64
	Attempts         int
}
