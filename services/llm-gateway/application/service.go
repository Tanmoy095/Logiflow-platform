package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

// Service orchestrates the LLM gateway pipeline. It acts as a pure workflow controller
// that coordinates domain policies, registry resolution, external providers, and telemetry.
// Service is the core orchestrator of the gateway execution pipeline.
type Service struct {
	provider        Provider
	registry        *TaskRegistry
	executionPolicy domain.ExecutionPolicy
	usagePublisher  UsagePublisher
	clock           Clock
	eventID         func() (string, error) // Injectable function field for generating unique event IDs
}

// NewService constructs a Service with the given provider and a default
// validation chain. The provider is injected, allowing different
// implementations (fake, OpenAI, Gemini) without changing this type.
func NewService(
	provider Provider,
	registry *TaskRegistry,
	executionPolicy domain.ExecutionPolicy,
	usagePublisher UsagePublisher,
	clock Clock,
) (*Service, error) {
	if provider == nil {
		return nil, fmt.Errorf("llm-gateway: nil provider")
	}
	if registry == nil {
		return nil, fmt.Errorf("llm-gateway: nil task registry")
	}
	if clock == nil {
		clock = systemClock{} // Default fallback to system clock
	}
	return &Service{
		provider:        provider,
		registry:        registry,
		executionPolicy: executionPolicy,
		usagePublisher:  usagePublisher,
		clock:           clock,
		eventID:         newEventID, // Assign default random generator function
	}, nil
}

// Complete handles the synchronous execution pipeline from request intake to domain result.
func (s *Service) Complete(
	ctx context.Context,
	cmd CompleteCommand,
) (TaskResult, ExecutionMetadata, error) {

	//  Re-introduced missing clock capture required by publishAttempt and Step 9
	startedAt := s.clock.Now()

	// Step 0: Initialize baseline metadata assuming failure until proven successful ,
	// why : because we want to ensure that any early exit due to validation or provider errors is captured in the metadata.
	metadata := ExecutionMetadata{
		RequestID:        cmd.RequestID,
		TenantID:         cmd.TenantID,
		ContractVersion:  cmd.ContractVersion,
		TaskType:         cmd.TaskType,
		Status:           domain.ExecutionFailed,
		ValidationStatus: domain.ValidationNotRun,
	}

	// publishAttempt captures execution metrics and dispatches non-blocking telemetry.
	// AI response delivery takes priority; event emission runs asynchronously using a
	// detached context to ensure client HTTP disconnections do not abort telemetry.
	//
	// Future Architectural Note: The gateway remains strictly stateless (no local DB).
	// If hard zero-loss financial billing is required in the future, the Transactional
	// Outbox Pattern should be implemented in the downstream Billing Service or
	// infrastructure layer, keeping core service execution clean and decoupled.
	publishAttempt := func(status domain.ExecutionStatus) {
		metadata.Status = status
		metadata.TotalGatewayLatencyMs = elapsedMilliseconds(startedAt, s.clock.Now())
		s.publishUsageEvent(ctx, cmd, metadata)
	}

	// Step 1: Validate input command syntax and structure
	if err := cmd.Validate(); err != nil {
		// Record the specific error classification for telemetry metadata
		metadata.ErrorKind = domain.KindInvalidArgument

		// Publish a failure telemetry event asynchronously before returning early
		publishAttempt(domain.ExecutionFailed)

		// Return a domain-specific invalid argument error to the caller
		return nil, metadata, domain.NewInvalidArgumentError(err.Error())
	}

	// Step 2: Validate global domain execution policy (contract version, tenant checks)
	if err := s.executionPolicy.ValidateCommandContext(
		cmd.ContractVersion,
		cmd.TenantID,
		cmd.RequestID,
		cmd.TaskType,
		cmd.EvidenceRefs,
	); err != nil {
		metadata.ErrorKind = domain.KindContractMismatch
		publishAttempt(domain.ExecutionFailed)
		return nil, metadata, domain.NewContractMismatchError(err.Error())
	}

	// Step 3: Resolve task definition handler from registry
	task, err := s.registry.Resolve(cmd.TaskType)
	if err != nil {
		metadata.ErrorKind = domain.KindUnsupportedTask
		publishAttempt(domain.ExecutionFailed)
		return nil, metadata, err
	}

	// Step 4: Validate command against task-specific requirements (e.g., subject count & type)
	if err := task.ValidateCommand(cmd); err != nil {
		metadata.ErrorKind = domain.KindContractMismatch
		publishAttempt(domain.ExecutionFailed)
		return nil, metadata, domain.NewContractMismatchError(err.Error())
	}

	// Step 5: Pre-flight context check (abort before calling LLM if context already canceled/expired)
	//why ? because we want to avoid unnecessary provider calls if the request context has already been canceled or timed out.
	callCtx, cancel := s.effectiveContext(ctx, cmd.Deadline)
	defer cancel()

	if err := callCtx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			metadata.ErrorKind = domain.KindRequestCanceled
			publishAttempt(domain.ExecutionRequestCancelled)
			return nil, metadata, domain.NewRequestCanceledError("request canceled before provider invocation", err)
		}
		// If the context has already exceeded its deadline, we classify it as a deadline exceeded error.
		metadata.ErrorKind = domain.KindDeadlineExceeded
		publishAttempt(domain.ExecutionDeadlineExceeded)
		return nil, metadata, domain.NewDeadlineExceededError("request deadline already expired", err)
	}

	// Step 6: Invoke external LLM provider & measure provider network latency
	providerStartedAt := s.clock.Now()

	providerResponse, err := s.provider.Complete(
		callCtx, task.BuildProviderRequest(cmd),
	)

	metadata.ProviderLatencyMs = elapsedMilliseconds(providerStartedAt, s.clock.Now())

	// Record provider-reported operational details
	metadata.Provider = providerResponse.Provider
	if metadata.Provider == "" { // Fallback to configured provider name if not reported
		metadata.Provider = s.provider.Name()
	}
	metadata.Model = providerResponse.Model
	metadata.InputTokens = providerResponse.InputTokens
	metadata.OutputTokens = providerResponse.OutputTokens
	metadata.TotalTokens = providerResponse.TotalTokens
	metadata.EstimatedCostUSD = providerResponse.EstimatedUSD
	metadata.Attempts = providerResponse.Attempts

	// Step 7: Handle external provider invocation failures
	if err != nil {
		if errors.Is(err, context.Canceled) {
			metadata.ErrorKind = domain.KindRequestCanceled
			publishAttempt(domain.ExecutionRequestCancelled)
			return nil, metadata, domain.NewRequestCanceledError("operation canceled by caller", err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			metadata.ErrorKind = domain.KindProviderTimeout
			publishAttempt(domain.ExecutionDeadlineExceeded)
			return nil, metadata, domain.NewProviderTimeoutError("provider routing exhausted before the effective deadline", err)
		}
		metadata.ErrorKind = domain.KindProviderUnavailable
		publishAttempt(domain.ExecutionProviderFailed)
		return nil, metadata, domain.NewProviderUnavailableError("provider routing exhausted or provider call failed", err)
	}

	// Step 8: Parse untrusted output & run task schema and domain validation
	validationStartedAt := s.clock.Now()
	result, err := task.DecodeAndValidate(callCtx, cmd, providerResponse.RawOutput)
	metadata.ValidationLatencyMs = elapsedMilliseconds(validationStartedAt, s.clock.Now())

	if err != nil {
		metadata.ErrorKind = domain.KindValidationFailed
		metadata.ValidationStatus = domain.ValidationFailed
		publishAttempt(domain.ExecutionValidationFailed)
		return nil, metadata, domain.NewValidationFailedError("provider output failed task validation", err)
	}

	// Step 9: Finalize metadata for successful execution and publish telemetry
	metadata.ValidationStatus = domain.ValidationPassed
	metadata.Status = domain.ExecutionSucceeded
	metadata.TotalGatewayLatencyMs = elapsedMilliseconds(startedAt, s.clock.Now())

	s.publishUsageEvent(ctx, cmd, metadata)
	return result, metadata, nil
}

// effectiveContext reconciles the parent request context with an optional explicit command deadline.
// It ensures that whichever deadline comes first (the parent's or the command's) wins.
func (s *Service) effectiveContext(parent context.Context, requestedDeadline time.Time) (context.Context, context.CancelFunc) {

	// Check 1: If no specific deadline was provided in the command,
	// just use the parent context as-is. (func() {} is a safe "do-nothing" cancel function)
	if requestedDeadline.IsZero() {
		return parent, func() {}
	}

	// Check 2: If the parent context already has a deadline, and that parent deadline
	// happens BEFORE or AT THE SAME TIME as the requested command deadline,
	// the parent context will time out first anyway. We stick with the parent context.
	if parentDeadline, ok := parent.Deadline(); ok && !requestedDeadline.Before(parentDeadline) {
		return parent, func() {}
	}

	// Check 3: The requested command deadline is stricter (happens sooner) than the parent context.
	// We create a new context with this tighter deadline and return it alongside its cancel function.
	return context.WithDeadline(parent, requestedDeadline)
}

// publishUsageEvent constructs and emits an audit/billing event to the Kafka publisher port.
// publishUsageEvent emits telemetry non-blockingly using a detached background context.
func (s *Service) publishUsageEvent(ctx context.Context, cmd CompleteCommand, metadata ExecutionMetadata) {
	if s.usagePublisher == nil {
		return
	}

	eventID, err := s.eventID()
	if err != nil {
		return
	}

	event := domain.AIUsageEvent{
		EventID:          eventID,
		EventVersion:     domain.CurrentUsageEventVersion,
		TenantID:         cmd.TenantID,
		RequestID:        cmd.RequestID,
		TaskType:         cmd.TaskType,
		Provider:         metadata.Provider,
		Model:            metadata.Model,
		Status:           metadata.Status,
		ValidationStatus: metadata.ValidationStatus,
		InputTokens:      metadata.InputTokens,
		OutputTokens:     metadata.OutputTokens,
		TotalTokens:      metadata.TotalTokens,
		EstimatedCostUSD: metadata.EstimatedCostUSD,
		OccurredAt:       s.clock.Now().UTC(),
	}

	if err := event.Validate(); err != nil {
		return
	}

	// 1. CONTEXT DETACHMENT:
	// context.WithoutCancel takes the parent context (retaining all values like
	// OpenTelemetry tracing IDs, tenant info, etc.) but strips away client
	// cancellation/timeout signals. This prevents dropped billing events if a user
	// disconnects right when the request completes.
	pubCtx := context.WithoutCancel(ctx)

	// 2. ASYNCHRONOUS BACKGROUND EXECUTION:
	// Wrapping the Publish call in `go func()` sends the event in a background
	// goroutine. This eliminates IO wait penalties, ensuring the user gets their
	// response immediately without waiting for network ACK confirmations from
	// message brokers like Kafka.
	go func() {
		_ = s.usagePublisher.PublishUsageEvent(pubCtx, event)
	}()
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }
