package application

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

/*
Validation pipeline:

Provider raw output (untrusted string)
        │
        ▼
 SyntaxValidator      (JSON parse)
        │
        ▼
 SchemaValidator      (required fields, correct types)
        │
        ▼
 DomainValidator      (business invariants, identity, confidence, risk, reasons)
        │
        ▼
 Trusted CompletionResult
*/

// providerResponse is the intermediate DTO used for schema validation.
// Pointers distinguish "missing" (nil) from "present zero value".
type providerResponse struct {
	ShipmentID *string   `json:"shipment_id"`
	Risk       *string   `json:"risk"`
	Confidence *float64  `json:"confidence"`
	Reasons    *[]string `json:"reasons"`
}

// ValidationInput carries data through the validation stages.
type ValidationInput struct {
	Request           domain.Request
	RawProviderOutput string

	// Parsed holds the result of syntax validation (untrusted DTO).
	Parsed *providerResponse

	// Candidate is created after schema validation.
	// It remains untrusted until domain validation succeeds.
	Candidate *domain.CompletionResult
}

// Validator is a single stage in the validation chain.
type Validator interface {
	Validate(ctx context.Context, input *ValidationInput) error
}

// SyntaxValidator checks that raw output is valid JSON.
type SyntaxValidator struct{}

func (SyntaxValidator) Validate(_ context.Context, input *ValidationInput) error {
	var parsed providerResponse
	if err := json.Unmarshal([]byte(input.RawProviderOutput), &parsed); err != nil {
		return fmt.Errorf("syntax validation: %w", err)
	}
	input.Parsed = &parsed
	return nil
}

// SchemaValidator checks required fields and constructs the candidate.
type SchemaValidator struct{}

func (SchemaValidator) Validate(_ context.Context, input *ValidationInput) error {
	if input.Parsed == nil {
		return fmt.Errorf("schema validation: parsed response is nil")
	}
	p := input.Parsed

	if p.ShipmentID == nil {
		return fmt.Errorf("schema validation: shipment_id is required")
	}
	if p.Risk == nil {
		return fmt.Errorf("schema validation: risk is required")
	}
	if p.Confidence == nil {
		return fmt.Errorf("schema validation: confidence is required")
	}
	if p.Reasons == nil {
		return fmt.Errorf("schema validation: reasons is required")
	}

	// Construct candidate. Still untrusted.
	candidate := domain.CompletionResult{
		ShipmentID: *p.ShipmentID,
		Risk:       domain.Risk(*p.Risk),
		Confidence: *p.Confidence,
		Reasons:    *p.Reasons,
	}
	input.Candidate = &candidate
	return nil
}

// DomainValidator enforces business invariants on the candidate.
type DomainValidator struct{}

func (DomainValidator) Validate(_ context.Context, input *ValidationInput) error {
	if input.Candidate == nil {
		return fmt.Errorf("domain validation: candidate is nil")
	}

	// Cross-entity identity check.
	if input.Candidate.ShipmentID != input.Request.ShipmentID {
		return fmt.Errorf("domain validation: shipment_id mismatch: requested %q, returned %q",
			input.Request.ShipmentID, input.Candidate.ShipmentID)
	}

	// Delegate remaining business rules to the domain object.
	if err := input.Candidate.Validate(); err != nil {
		return fmt.Errorf("domain validation: %w", err)
	}

	return nil
}

// ValidationChain executes validators in order.
type ValidationChain struct {
	validators []Validator
}

// NewValidationChain creates the standard chain.
func NewValidationChain() *ValidationChain {
	return &ValidationChain{
		validators: []Validator{
			SyntaxValidator{},
			SchemaValidator{},
			DomainValidator{},
		},
	}
}

// Run executes all validators, stopping at first error.
func (c *ValidationChain) Run(ctx context.Context, input *ValidationInput) error {
	for _, v := range c.validators {
		if err := v.Validate(ctx, input); err != nil {
			return err
		}
	}
	return nil
}
