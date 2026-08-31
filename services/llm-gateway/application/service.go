// services/llm-gateway/application/service.go

package application

import (
	"context"
	"errors"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

type Service struct {
	provider        Provider
	ValidationChain *ValidationChain
}

func NewService(provider Provider) *Service {
	return &Service{provider: provider, ValidationChain: NewValidationChain()}
}

// Complete executes the LogiFlow AI‑completion use case.
//
// Steps:
//  1. Validate request (domain invariants) -> KindInvalidArgument.
//  2. Call Provider with the caller's context.
//  3. Classify provider operational errors (timeout, canceled, unavailable).
//  4. Run the validation chain on the raw provider output.
//  5. Return the trusted CompletionResult if all validations pass.
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

	//Run the validation chain on the raw provider output.
	input := &ValidationInput{
		Request:           req,
		RawProviderOutput: raw,
	}

	if err := s.ValidationChain.Run(ctx, input); err != nil {
		// Any validation failure is a semantic failure -> ValidationFailed.
		return domain.CompletionResult{}, domain.NewValidationFailedError(err.Error())
	}
	//  Success – candidate has passed all stages and is now trusted.
	return *input.Candidate, nil
}
