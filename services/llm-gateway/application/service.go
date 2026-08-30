// services/llm-gateway/application/service.go

package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

type Service struct {
	provider Provider
}

func NewService(provider Provider) *Service {
	return &Service{provider: provider}
}

// Complete executes the LogiFlow AI‑completion use case.
//
// Steps:
//  1. Validate request (domain invariants).
//  2. Call Provider with the caller's context.
//  3. Classify provider errors (timeout, canceled, unavailable).
//  4. Parse and validate provider output.
//  5. Check shipment identity.
//  6. Validate business rules.
//  7. Return trusted CompletionResult or a typed DomainError.
//
// The context is passed unchanged to the provider, so the provider
// shares the same lifetime as this operation.

func (s *Service) Complete(
	ctx context.Context,
	req domain.Request,
) (domain.CompletionResult, error) {

	// Boundary validation: can this request be executed?
	//Request validation → invalid argument.
	if err := req.Validate(); err != nil {
		return domain.CompletionResult{}, domain.NewInvalidArgumentError(err.Error())
	}

	// The application depends only on the Provider abstraction.

	// Call provider with the same context.
	raw, err := s.provider.Complete(ctx, req)
	if err != nil {
		//classify operational errors
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return domain.CompletionResult{}, domain.NewProviderTimeoutError(
				"provider did not respond before deadline",
				err,
			)
		case errors.Is(err, context.Canceled):
			return domain.CompletionResult{}, domain.NewRequestCanceledError(
				"operation canceled by caller",
				err,
			)
		default:
			return domain.CompletionResult{}, domain.NewProviderUnavailableError(
				"provider call failed",
				err,
			)
		}
	}

	// Convert raw provider data into a candidate result.
	result, err := parseProviderResponse(raw)
	if err != nil {
		// Parsing/schema errors are validation failures, not operational.
		return domain.CompletionResult{}, domain.NewValidationFailedError(
			err.Error(),
		)
	}

	// Cross-entity consistency check.
	//
	// A valid JSON response for the wrong shipment is still invalid.
	if result.ShipmentID != req.ShipmentID {
		return domain.CompletionResult{}, domain.NewValidationFailedError(
			fmt.Sprintf("shipment_id mismatch: requested %q, returned %q",
				req.ShipmentID, result.ShipmentID),
		)
	}

	if err := result.Validate(); err != nil {
		return domain.CompletionResult{}, domain.NewValidationFailedError(err.Error())
	}

	// Step 7: success – trusted result.
	return result, nil
}

// providerResponse is an intermediate representation of untrusted provider data.
//
// Pointers allow us to distinguish:
//   - field missing / null -> nil
//   - field present with zero value -> pointer to zero value
//
// This is important for schema validation.
type providerResponse struct {
	ShipmentID *string   `json:"shipment_id"`
	Risk       *string   `json:"risk"`
	Confidence *float64  `json:"confidence"`
	Reasons    *[]string `json:"reasons"`
}

// parseProviderResponse performs syntax + schema validation.
//
// It does NOT perform business/domain validation.
func parseProviderResponse(
	raw string,
) (domain.CompletionResult, error) {

	var response providerResponse

	// Stage 1: Syntax validation.
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return domain.CompletionResult{}, fmt.Errorf(
			"parse provider response: %w",
			err,
		)
	}

	// Stage 2: Schema validation.
	if response.ShipmentID == nil {
		return domain.CompletionResult{}, fmt.Errorf(
			"schema validation: shipment_id is required",
		)
	}

	if response.Risk == nil {
		return domain.CompletionResult{}, fmt.Errorf(
			"schema validation: risk is required",
		)
	}

	if response.Confidence == nil {
		return domain.CompletionResult{}, fmt.Errorf(
			"schema validation: confidence is required",
		)
	}

	if response.Reasons == nil {
		return domain.CompletionResult{}, fmt.Errorf(
			"schema validation: reasons is required",
		)
	}

	// Candidate construction.
	//
	// This is NOT trusted merely because construction succeeded.
	// Domain validation happens immediately after this.
	return domain.CompletionResult{
		ShipmentID: *response.ShipmentID,
		Risk:       domain.Risk(*response.Risk),
		Confidence: *response.Confidence,
		Reasons:    *response.Reasons,
	}, nil
}
