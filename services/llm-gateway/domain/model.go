package domain

import (
	"fmt"
	"strings"
)

// TaskType identifies a governed AI capability exposed by the LLM Gateway.
//
// This is deliberately a closed vocabulary inside the gateway. A caller cannot
// invent a new task at runtime and expect the gateway to execute arbitrary work.
// Every supported TaskType must have a registered definition describing its
// input contract, output schema and validation behavior.
type TaskType string

const (
	// TaskShipmentDelayRisk is the first real business capability implemented in
	// Sprint 01. More capabilities can be added later without changing the
	// generic CompleteCommand contract.
	TaskShipmentDelayRisk TaskType = "shipment_delay_risk"
)

// EntityType identifies the business subject on which a capability operates.
// The gateway stores references rather than domain entities so it does not
// become the owner of shipment or customer state.
type EntityType string

const (
	EntityShipment EntityType = "shipment"

	//future: EntityCustomer EntityType = "customer"
	//future: EntityCarrier EntityType = "carrier"
)

// EntityReference is an explicit reference to a business subject.
//
// Important: ID here is intentionally generic because the reference could be
// a shipment, customer, carrier, or another future subject. Durable evidence
// is different and therefore gets its own strongly named EvidenceReference.
type EntityReference struct {
	Type EntityType `json:"type"`
	ID   string     `json:"id"`
}

// EvidenceReference points at durable business evidence owned by the evidence
// subsystem. The gateway must never replace EvidenceID with a generic ID:
// evidence_id, request_id and shipment_id have different lifecycles and
// different security/audit meanings.
type EvidenceReference struct {
	EvidenceID string `json:"evidence_id"`
}

// ExecutionStatus is the stable outcome vocabulary used by the application
// and usage/audit events. Transport adapters may map these statuses to HTTP or
// gRPC-specific codes, but domain code should not know those protocols.
type ExecutionStatus string

const (
	ExecutionSucceeded        ExecutionStatus = "succeeded"
	ExecutionFailed           ExecutionStatus = "failed"
	ExecutionProviderFailed   ExecutionStatus = "provider_failed"
	ExecutionValidationFailed ExecutionStatus = "validation_failed"
	ExecutionDeadlineExceeded ExecutionStatus = "deadline_exceeded"
	ExecutionRequestCancelled ExecutionStatus = "request_cancelled"
)

// ValidationStatus tells us whether provider output crossed the trust boundary.
// A provider call can succeed operationally while validation still fails.
type ValidationStatus string

const (
	ValidationNotRun ValidationStatus = "not_run"
	ValidationPassed ValidationStatus = "passed"
	ValidationFailed ValidationStatus = "failed"
)

// ValidateReference performs only structural validation. It does not check a
// database because the gateway does not own those business entities.
func (r EntityReference) Validate() error {
	if strings.TrimSpace(string(r.Type)) == "" {
		return fmt.Errorf("entity type must not be empty")
	}
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("entity id must not be empty")
	}
	return nil
}

// Validate performs the structural validation for a durable evidence reference.
// Existence and tenant ownership remain responsibilities of the evidence/RAG
// boundary that supplied the reference.
func (r EvidenceReference) Validate() error {
	if strings.TrimSpace(r.EvidenceID) == "" {
		return fmt.Errorf("evidence_id must not be empty")
	}
	return nil
}
