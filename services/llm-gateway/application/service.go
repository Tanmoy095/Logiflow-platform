// services/llm-gateway/application/service.go

package application

import (
	"context"
	"errors"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

// Service orchestrates the AI completion workflow.
//
// It depends on a Provider abstraction (for external AI calls) and a
// ValidationChain (for syntax, schema, and domain validation). All
// operational and business rules are kept outside the Service, making it
// a thin orchestration layer that coordinates the use case.
type Service struct {
	provider        Provider
	validationChain *ValidationChain
}

// NewService constructs a Service with the given provider and a default
// validation chain. The provider is injected, allowing different
// implementations (fake, OpenAI, Gemini) without changing this type.
func NewService(provider Provider) *Service {
	return &Service{
		provider:        provider,
		validationChain: NewValidationChain(),
	}
}

// Complete executes the LogiFlow AI‑completion use case.
//
// It returns:
//   - a trusted domain.CompletionResult (zero value on failure)
//   - ExecutionMetadata containing latency decomposition and error kind
//   - an error (nil on success)
//
// The metadata is deliberately separate from the domain result. It carries
// operational context (provider, prompt version, latencies, status) so that
// the caller or an observability layer can understand what happened without
// polluting business data.
//
// The context is passed unchanged to the provider, so the provider shares
// the same lifetime as this operation.
func (s *Service) Complete(
	ctx context.Context,
	req domain.Request,
) (domain.CompletionResult, ExecutionMetadata, error) {

	// Start total gateway timer.
	startTotal := time.Now()
	metadata := ExecutionMetadata{
		RequestID:     newRequestID(),
		Provider:      s.provider.Name(),
		PromptVersion: req.PromptVersion,
	}

	// Step 1: Validate the request before doing any external work.
	// This is a domain-level check, so failure is classified as invalid_argument.
	if err := req.Validate(); err != nil {
		metadata.Status = "invalid_argument"
		metadata.ErrorKind = string(domain.KindInvalidArgument)
		metadata.TotalGatewayLatencyMs = millisecondsSince(startTotal)
		return domain.CompletionResult{}, metadata, domain.NewInvalidArgumentError(err.Error())
	}

	// Step 2: Call the provider, measuring exactly how long it takes.
	// This latency will help distinguish provider slowness from internal
	// validation slowness later.
	providerStart := time.Now()
	raw, err := s.provider.Complete(ctx, req)
	metadata.ProviderLatencyMs = millisecondsSince(providerStart)

	if err != nil {
		// Step 3: Classify the operational failure and set metadata accordingly.
		metadata.TotalGatewayLatencyMs = millisecondsSince(startTotal)
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			metadata.Status = "provider_timeout"
			metadata.ErrorKind = string(domain.KindProviderTimeout)
			return domain.CompletionResult{}, metadata, domain.NewProviderTimeoutError(
				"provider did not respond before deadline",
				err,
			)
		case errors.Is(err, context.Canceled):
			metadata.Status = "request_canceled"
			metadata.ErrorKind = string(domain.KindRequestCanceled)
			return domain.CompletionResult{}, metadata, domain.NewRequestCanceledError(
				"operation canceled by caller",
				err,
			)
		default:
			metadata.Status = "provider_unavailable"
			metadata.ErrorKind = string(domain.KindProviderUnavailable)
			return domain.CompletionResult{}, metadata, domain.NewProviderUnavailableError(
				"provider call failed",
				err,
			)
		}
	}

	// Step 4: Run the validation chain (syntax → schema → domain).
	// Measure validation latency separately from provider latency.
	validationStart := time.Now()
	input := &ValidationInput{
		Request:           req,
		RawProviderOutput: raw,
	}

	if err := s.validationChain.Run(ctx, input); err != nil {
		metadata.ValidationLatencyMs = millisecondsSince(validationStart)
		metadata.TotalGatewayLatencyMs = millisecondsSince(startTotal)
		metadata.Status = "validation_failed"
		metadata.ErrorKind = string(domain.KindValidationFailed)
		return domain.CompletionResult{}, metadata, domain.NewValidationFailedError(err.Error())
	}

	// Step 5: Success. The candidate has passed all stages and is now trusted.
	metadata.ValidationLatencyMs = millisecondsSince(validationStart)
	metadata.TotalGatewayLatencyMs = millisecondsSince(startTotal)
	metadata.Status = "success"
	metadata.ErrorKind = "" // clear any stale value

	return *input.Candidate, metadata, nil
}

// millisecondsSince returns the elapsed time in milliseconds as a float64.
// It uses float64 to allow sub‑millisecond precision if needed.
func millisecondsSince(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000.0
}
