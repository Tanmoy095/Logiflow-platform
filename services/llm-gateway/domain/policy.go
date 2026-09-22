package domain

import (
	"fmt"
	"strings"
	"time"
)

// ExecutionContext is the small, capability-neutral portion of an AI request
// that the domain needs to validate.
//
// Keeping this type in the domain package prevents domain/policy.go from
// importing application.CompleteCommand, preserving the dependency rule:
//
//	domain <- application <- interfaces/infrastructure
//
// Now carries the clock value so policies can implement time-based rules
// (rate windows, business hours) without the domain package depending on
// a Clock interface.
type ExecutionContext struct {
	ContractVersion string
	TenantID        string
	RequestID       string
	TaskType        TaskType
	EvidenceRefs    []EvidenceReference
	Now             time.Time
}

// ExecutionPolicy protects the gateway BEFORE any billable provider work
// happens.
//
// These are gateway-wide rules, not shipment-risk rules. Shipment-specific
// semantics belong in domain/shipmentrisk/policy.go.
type ExecutionPolicy struct {
	// CurrentContractVersion is the only contract version this gateway
	// currently accepts. Callers must send requests matching it.
	CurrentContractVersion string

	// MaxEvidenceReferences caps how many evidence references a single
	// request may carry. Semantics:
	//   n > 0  → at most n references allowed
	//   n == 0 → NO references allowed (strictest)
	//   n < 0  → unlimited (explicit opt-in to no cap)
	MaxEvidenceReferences int
}

// ValidateExecutionContext performs cheap in-memory guardrails before any
// provider work happens.
//
// Why here?
//  1. The rules are true regardless of transport or provider.
//  2. They are cheaper to enforce than an external provider call.
//  3. They make the trust boundary explicit at the domain layer, so every
//     transport adapter inherits the same enforcement.
func (p ExecutionPolicy) ValidateExecutionContext(ctx ExecutionContext) error {
	if strings.TrimSpace(p.CurrentContractVersion) == "" {
		return fmt.Errorf("execution policy current contract version must not be empty")
	}
	if strings.TrimSpace(ctx.ContractVersion) == "" {
		return fmt.Errorf("contract_version must not be empty")
	}
	if ctx.ContractVersion != p.CurrentContractVersion {
		return fmt.Errorf(
			"unsupported contract_version %q; supported version is %q",
			ctx.ContractVersion,
			p.CurrentContractVersion,
		)
	}
	if strings.TrimSpace(ctx.TenantID) == "" {
		return fmt.Errorf("tenant_id must not be empty")
	}
	if strings.TrimSpace(ctx.RequestID) == "" {
		return fmt.Errorf("request_id must not be empty")
	}
	if strings.TrimSpace(string(ctx.TaskType)) == "" {
		return fmt.Errorf("task_type must not be empty")
	}

	// Evidence-count enforcement. See MaxEvidenceReferences docs for the
	// semantics of each sign. The check is deliberately before per-reference
	// validation so we fail fast on the cheapest condition.
	if p.MaxEvidenceReferences >= 0 && len(ctx.EvidenceRefs) > p.MaxEvidenceReferences {
		return fmt.Errorf(
			"too many evidence references: got %d, maximum is %d",
			len(ctx.EvidenceRefs),
			p.MaxEvidenceReferences,
		)
	}

	// Structural validation of each reference. Existence/ownership checks
	// belong to the evidence subsystem, not the gateway.
	for i, ref := range ctx.EvidenceRefs {
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("evidence_refs[%d]: %w", i, err)
		}
	}

	return nil
}
