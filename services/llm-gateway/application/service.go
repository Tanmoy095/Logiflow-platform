//service/llm-gateway/application/service.go

package application

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

type Service struct {
	provider Provider
}

func NewService(provider Provider) *Service {
	return &Service{
		provider: provider,
	}
}

// Complete executes the LogiFlow AI-completion use case:
//
//  1. Validate request
//  2. Call Provider
//  3. Parse raw output
//  4. Validate provider schema
//  5. Construct candidate domain result
//  6. Validate business/domain invariants
//  7. Return trusted CompletionResult
//
// The provider's raw response remains untrusted until all validation passes.
func (s *Service) Complete(
	ctx context.Context,
	req domain.Request,
) (domain.CompletionResult, error) {

	// Boundary validation: can this request be executed?
	if err := req.Validate(); err != nil {
		return domain.CompletionResult{}, err
	}

	// The application depends only on the Provider abstraction.
	raw, err := s.provider.Complete(ctx, req)
	if err != nil {
		return domain.CompletionResult{}, err
	}

	// Convert raw provider data into a candidate result.
	result, err := parseProviderResponse(raw)
	if err != nil {
		return domain.CompletionResult{}, err
	}

	// Cross-entity consistency check.
	//
	// A valid JSON response for the wrong shipment is still invalid.
	if result.ShipmentID != req.ShipmentID {
		return domain.CompletionResult{}, fmt.Errorf(
			"shipment_id mismatch: requested %q, returned %q",
			req.ShipmentID,
			result.ShipmentID,
		)
	}

	// Domain validation is the final trust gate.
	if err := result.Validate(); err != nil {
		return domain.CompletionResult{}, err
	}

	// Only now does the result become trusted.
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
