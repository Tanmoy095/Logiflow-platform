package application

// TODO: define command types

import (
	"fmt"
	"strings"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

const CurrentContractVersion = "ai-completion.v1"

// CompleteCommand is the transport-neutral application use-case contract.
//
// HTTP/gRPC/MCP adapters will eventually translate their own request shape into
// this command. Keeping this struct transport-neutral prevents protocol details
// from leaking into business orchestration and makes the use case easy to test.
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

// Validate performs cheap command-shape validation. Capability-specific rules
// are delegated to the TaskDefinition resolved from the registry. This keeps a
// generic gateway command from becoming a giant switch statement.
func (c CompleteCommand) Validate() error {
	if strings.TrimSpace(c.ContractVersion) == "" {
		return fmt.Errorf("contract_version must not be empty")
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
	for i, ref := range c.EvidenceRefs {
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("evidence_refs[%d]: %w", i, err)
		}
	}
	return nil
}

// ProviderRequest is deliberately smaller than CompleteCommand. The provider
// should receive only the fields it needs for inference. Tenant/request IDs are
// application metadata, not vendor prompt data.
type ProviderRequest struct {
	TaskType            domain.TaskType
	Prompt              string
	PromptVersion       string
	OutputSchemaVersion string
	MaxTokens           int
}

// ProviderResponse contains raw provider output plus non-sensitive execution
// metadata. RawOutput remains untrusted until the application validates it.
type ProviderResponse struct {
	RawOutput    string
	Provider     string
	Model        string
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	EstimatedUSD float64
	Attempts     int
}
