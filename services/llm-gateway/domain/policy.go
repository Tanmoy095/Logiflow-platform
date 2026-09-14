package domain

import (
	"fmt"
	"strings"
)

// TODO: define domain policies and rules

// policy.go (The Security Guard): Runs BEFORE the LLM call to reject bad requests at zero cost.

type ExecutionPolicy struct {
	CurrentContractVersion string
	MaxEvidenceReferences  int //how many evidence references can be passed in a single request
}

// ValidateCommandContext enforces invariants that are meaningful to every governed AI operation.

func (p ExecutionPolicy) ValidateCommandContext(
	contractVersion string,
	tenantID string,
	requestID string,
	taskType TaskType,
	evidenceRefs []EvidenceReference,
) error {
	if strings.TrimSpace(p.CurrentContractVersion) == "" {
		return fmt.Errorf("execution policy current contract version must not be empty")
	}
	if contractVersion != p.CurrentContractVersion {
		return fmt.Errorf("unsupported contract_version: %q", contractVersion)
	}
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("tenant_id must not be empty")
	}
	if strings.TrimSpace(requestID) == "" {
		return fmt.Errorf("request_id must not be empty")
	}
	if strings.TrimSpace(string(taskType)) == "" {
		return fmt.Errorf("task_type must not be empty")
	}
	if p.MaxEvidenceReferences > 0 && len(evidenceRefs) > p.MaxEvidenceReferences {
		return fmt.Errorf("evidence_refs exceeds maximum of %d", p.MaxEvidenceReferences)
	}
	for i, ref := range evidenceRefs {
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("evidence_refs[%d]: %w", i, err)
		}
	}
	return nil
}
