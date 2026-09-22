package domain

import (
	"fmt"
	"strings"
)

// TaskType is the closed vocabulary of governed AI capabilities that this
// gateway is willing to execute.
//
// This is intentionally NOT an arbitrary prompt category. A TaskType must
// correspond to a registered capability with a known input contract, output
// schema, policy, and tests. A caller cannot invent a new task at runtime
// and expect the gateway to execute arbitrary work.
type TaskType string

const (
	// TaskShipmentDelayRisk is the first real business capability.
	// More capabilities can be added later without changing the generic
	// CompleteCommand contract.
	TaskShipmentDelayRisk TaskType = "shipment_delay_risk"
)

// EntityType identifies a business subject used by an AI capability.
//
// The gateway stores references rather than domain entities so it does not
// become the owner of shipment or customer state.
type EntityType string

const (
	EntityShipment EntityType = "shipment"

	// Future capabilities can add more entity types here without touching
	// the generic reference type:
	//   EntityCustomer EntityType = "customer"
	//   EntityCarrier  EntityType = "carrier"
)

// EntityReference points at a business entity owned by another bounded
// context. The gateway references it; it does not load or own the entity.
//
// The ID field is generic because the reference could be a shipment, customer,
// carrier, or another future subject. Durable evidence is different — it gets
// its own strongly named EvidenceReference below.
type EntityReference struct {
	Type EntityType `json:"type"`
	ID   string     `json:"id"`
}

// Validate rejects incomplete references before they can reach provider work.
// Keeping this validation in the domain means every transport gets the same
// business-safe behavior.
//
// Note: this is structural validation only. Existence and ownership checks
// belong to the bounded context that owns the entity.
func (r EntityReference) Validate() error {
	if strings.TrimSpace(string(r.Type)) == "" {
		return fmt.Errorf("entity type must not be empty")
	}
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("entity id must not be empty")
	}
	return nil
}

// EvidenceReference identifies durable business evidence owned by the evidence
// subsystem.
//
// IMPORTANT: use EvidenceID explicitly. Evidence identity is not the same
// thing as request identity or shipment identity — they have different
// lifecycles and different security/audit meanings.
type EvidenceReference struct {
	EvidenceID string `json:"evidence_id"`
}

// Validate performs structural validation only. Existence and tenant
// ownership remain the responsibility of the evidence/RAG boundary that
// supplied the reference.
func (r EvidenceReference) Validate() error {
	if strings.TrimSpace(r.EvidenceID) == "" {
		return fmt.Errorf("evidence_id must not be empty")
	}
	return nil
}

// ExecutionStatus describes the lifecycle outcome of the gateway use case.
//
// Transport adapters may map these values to HTTP/gRPC status codes, but the
// domain itself remains transport-neutral. The prefix "execution_" prevents
// collisions with other status vocabularies in shared logs/dashboards.
type ExecutionStatus string

const (
	ExecutionSucceeded ExecutionStatus = "execution_succeeded"

	// Preflight failures happen before any provider call. They consumed no
	// tokens and must not be billed. Splitting preflight from runtime
	// failures lets alerting distinguish "bad request" from "vendor down".
	ExecutionPreflightFailed ExecutionStatus = "execution_preflight_failed"
	ExecutionUnsupportedTask ExecutionStatus = "execution_unsupported_task"

	// Runtime failures happen after a provider call was attempted.
	ExecutionProviderFailed   ExecutionStatus = "execution_provider_failed"
	ExecutionValidationFailed ExecutionStatus = "execution_validation_failed"

	// Caller lifecycle: the request ended because the caller went away.
	// These are neither retryable nor billable.
	ExecutionRequestCanceled  ExecutionStatus = "execution_request_canceled"
	ExecutionDeadlineExceeded ExecutionStatus = "execution_deadline_exceeded"
)

// ValidationStatus records whether the untrusted provider result crossed the
// validation boundary.
//
// A provider call can succeed operationally while validation still fails.
// Keeping this separate from ExecutionStatus is what lets us publish a usage
// event for a validation failure (tokens were consumed) without pretending
// the request succeeded.
type ValidationStatus string

const (
	ValidationNotRun ValidationStatus = "validation_not_run"
	ValidationPassed ValidationStatus = "validation_passed"
	ValidationFailed ValidationStatus = "validation_failed"
)
