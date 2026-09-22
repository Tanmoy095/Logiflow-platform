// Package application contains the LLM Gateway's use-case orchestration layer.
//
// This file implements Service, the workflow controller that wires together:
//
//   - command validation          (preflight trust boundary #1)
//   - execution policy            (tenant/contract rules)
//   - task registry               (capability resolution)
//   - provider invocation         (external I/O, possibly via ProviderRouter)
//   - output validation           (preflight trust boundary #2)
//   - usage/audit telemetry       (non-blocking, best-effort)
//
// The Service knows the ORDER of work. It does NOT know:
//
//   - HTTP details (they live in infrastructure/providers)
//   - retry/fallback policy (it lives in ProviderRouter)
//   - how vendor errors map to ProviderFailureKind (also the router)
//
// That separation keeps this package stable as vendors, transports, and
// reliability policies evolve underneath it.
package application

import (
	"context"
	"errors"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

// EventIDGenerator produces a unique identifier for every usage event.
//
// It is injected rather than hardcoded so that:
//
//   - production can use UUIDs or KSUIDs,
//   - tests can return deterministic IDs and assert on published events,
//   - alternative deployments (e.g. deterministic replay) can swap it out.
//
// The error return allows a generator backed by a fallible source (e.g. a
// hardware RNG or a snowflake-ID node) to report failure without panicking.
type EventIDGenerator func() (string, error)

// Service is the application use-case orchestrator for the LLM gateway.
//
// It is intentionally stateless: every field is a dependency, and Complete is
// a pure function of (context, command) modulo I/O side effects. That makes it
// safe to share across goroutines and trivial to reason about under load.
//
// executionPolicy is a VALUE (not a pointer) because policy enforcement is a
// security control, not an optional feature. There is no such thing as a
// "gateway without policy". The zero-value policy already fails validation
// (empty CurrentContractVersion), so a misconfigured policy fails fast at
// either construction time (see NewService) or at the first request.
type Service struct {
	provider        Provider
	registry        *TaskRegistry
	executionPolicy domain.ExecutionPolicy
	usagePublisher  UsagePublisher
	clock           Clock
	newEventID      EventIDGenerator
}

// NewService validates its dependency graph at composition time.
//
// Rationale for fail-fast validation:
//
//   - A missing dependency is a wiring bug, not a runtime condition. Better
//     to crash the process at startup than to serve 500s at 3am.
//   - The old silent `clock = systemClock{}` fallback made tests that forgot
//     to inject a fake clock non-deterministic in subtle ways. Rejecting nil
//     forces the caller to be explicit.
//   - The execution policy is checked for non-empty configuration here, so a
//     forgotten policy fails at boot rather than at the first user request.
//
// usagePublisher may be nil — that is a legitimate choice for minimal
// deployments where telemetry is disabled. Every other dependency is
// required.
func NewService(
	provider Provider,
	registry *TaskRegistry,
	executionPolicy domain.ExecutionPolicy,
	usagePublisher UsagePublisher,
	clock Clock,
	newEventID EventIDGenerator,
) (*Service, error) {
	if provider == nil {
		return nil, errors.New("llm-gateway: provider must not be nil")
	}
	if registry == nil {
		return nil, errors.New("llm-gateway: task registry must not be nil")
	}
	if clock == nil {
		return nil, errors.New("llm-gateway: clock must not be nil")
	}
	if newEventID == nil {
		return nil, errors.New("llm-gateway: event id generator must not be nil")
	}

	// The policy is a value type so we cannot check for nil — but we can
	// (and should) check that it is actually configured. A zero-value
	// policy is unusable: it will reject every request with "execution
	// policy current contract version must not be empty". Catching that
	// here turns a subtle 3am incident into an obvious boot failure.
	if executionPolicy.CurrentContractVersion == "" {
		return nil, errors.New(
			"llm-gateway: execution policy must be configured " +
				"(CurrentContractVersion is empty)",
		)
	}

	return &Service{
		provider:        provider,
		registry:        registry,
		executionPolicy: executionPolicy,
		usagePublisher:  usagePublisher, // nil = telemetry disabled
		clock:           clock,
		newEventID:      newEventID,
	}, nil
}

// Complete executes the full gateway pipeline for a single request.
//
// The stages are ordered cheapest-first so we never spend money on a provider
// call we could have rejected for free:
//
//  1. Command shape validation          (in-process)
//  2. Domain execution policy           (in-process, may hit tenant cache)
//  3. Task resolution from registry     (in-process)
//  4. Task-specific contract validation (in-process)
//  5. Caller context pre-flight         (in-process)
//  6. Provider invocation               (NETWORK — expensive)
//  7. Output decode + validation        (in-process, trust boundary)
//  8. Success/telemetry finalization
//
// Every early return funnels through the fail() closure so metadata invariants
// (status, error kind, total latency) are always populated consistently.
func (s *Service) Complete(
	ctx context.Context,
	cmd CompleteCommand,
) (TaskResult, ExecutionMetadata, error) {
	startedAt := s.clock.Now()

	// Baseline metadata. We fill in fields as we learn them; on any failure,
	// whatever we've captured so far (provider, tokens, latency) is preserved
	// for audit and billing.
	//
	// NOTE: metadata.TaskType is domain.TaskType (not string), matching the
	// ExecutionMetadata struct. Assigning cmd.TaskType directly keeps the
	// strong type all the way through.
	metadata := ExecutionMetadata{
		RequestID:           cmd.RequestID,
		TenantID:            cmd.TenantID,
		ContractVersion:     cmd.ContractVersion,
		TaskType:            cmd.TaskType,
		TaskSchemaVersion:   cmd.TaskSchemaVersion,
		OutputSchemaVersion: cmd.OutputSchemaVersion,
		PromptVersion:       cmd.PromptVersion,
		Status:              domain.ExecutionPreflightFailed,
		ValidationStatus:    domain.ValidationNotRun,
	}

	// fail centralizes the failure path so every early exit:
	//   - sets a specific status (not a catch-all "Failed"),
	//   - sets a specific error kind for analytics,
	//   - records total latency up to the point of failure.
	//
	// NOTE: this does NOT publish a usage event. Usage events represent
	// provider work that was actually performed; preflight rejections
	// consumed no tokens and therefore have nothing to bill. Request-level
	// telemetry for rejections belongs to a separate lifecycle event.
	fail := func(
		kind domain.Kind,
		status domain.ExecutionStatus,
		err error,
	) (TaskResult, ExecutionMetadata, error) {
		metadata.Status = status
		metadata.ErrorKind = kind
		metadata.TotalGatewayLatencyMs = millisecondsSince(startedAt, s.clock)
		return nil, metadata, err
	}

	// -------------------------------------------------------------------------
	// Stage 1: Command shape validation
	// -------------------------------------------------------------------------
	// Cheap in-process check. Runs before anything that could cost money or
	// touch external state.
	if err := cmd.Validate(); err != nil {
		return fail(
			domain.KindInvalidArgument,
			domain.ExecutionPreflightFailed,
			domain.NewInvalidArgumentError(err.Error()),
		)
	}

	// -------------------------------------------------------------------------
	// Stage 2: Domain execution policy
	// -------------------------------------------------------------------------
	// Contract version, tenant eligibility, evidence requirements, and any
	// time-sensitive rules. The policy sees the same clock the rest of the
	// service uses so "now" is consistent across decisions.
	//
	// The policy is a required dependency (validated in NewService), so this
	// call is unconditional. If you ever want to disable policy enforcement
	// for a specific deployment, do it by injecting a permissive policy — not
	// by making the field nullable, which risks silently shipping a gateway
	// with no guardrails to production.
	if err := s.executionPolicy.ValidateExecutionContext(domain.ExecutionContext{
		ContractVersion: cmd.ContractVersion,
		TenantID:        cmd.TenantID,
		RequestID:       cmd.RequestID,
		TaskType:        cmd.TaskType,
		EvidenceRefs:    cmd.EvidenceRefs,
		Now:             s.clock.Now(),
	}); err != nil {
		return fail(
			domain.KindContractMismatch,
			domain.ExecutionPreflightFailed,
			domain.NewContractMismatchError(err.Error()),
		)
	}

	// -------------------------------------------------------------------------
	// Stage 3: Resolve capability from the registry
	// -------------------------------------------------------------------------
	// The registry maps TaskType -> Task implementation. Unknown tasks are
	// rejected here, before any expensive work.
	//
	// The registry already returns a domain.NewUnsupportedTaskError, so we
	// pass it through unchanged — wrapping it again would double-nest the
	// error and confuse error-matching callers.
	task, err := s.registry.Resolve(cmd.TaskType)
	if err != nil {
		return fail(
			domain.KindUnsupportedTask,
			domain.ExecutionUnsupportedTask,
			err,
		)
	}

	// -------------------------------------------------------------------------
	// Stage 4: Capability-specific contract validation
	// -------------------------------------------------------------------------
	// Each Task may impose its own constraints (e.g. "subject must be a
	// shipment", "output_schema_version must match"). This runs before the
	// provider call so we never pay for input the task will reject.
	if err := task.ValidateCommand(cmd); err != nil {
		return fail(
			domain.KindContractMismatch,
			domain.ExecutionPreflightFailed,
			domain.NewContractMismatchError(err.Error()),
		)
	}

	// -------------------------------------------------------------------------
	// Stage 5: Reconcile caller context with the command's explicit deadline
	// -------------------------------------------------------------------------
	// If the command carries a deadline, the effective deadline is the earlier
	// of (caller context deadline, command deadline). We avoid allocating a
	// child context when the parent is already tighter — cheaper and keeps the
	// context hierarchy honest.
	callCtx, cancel := withEffectiveDeadline(ctx, cmd.Deadline)
	if cancel != nil {
		defer cancel()
	}

	if err := callCtx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return fail(
				domain.KindRequestCanceled,
				domain.ExecutionRequestCanceled,
				domain.NewRequestCanceledError(
					"request canceled before provider invocation", err),
			)
		}
		return fail(
			domain.KindDeadlineExceeded,
			domain.ExecutionDeadlineExceeded,
			domain.NewDeadlineExceededError(
				"request deadline expired before provider invocation", err),
		)
	}

	// -------------------------------------------------------------------------
	// Stage 6: Provider invocation
	// -------------------------------------------------------------------------
	// The concrete Provider MAY be a raw adapter (OpenAI, Anthropic) or a
	// ProviderRouter that hides retries and fallback. The Service is agnostic
	// — it reads only the response's public fields.
	//
	// Narrow the command into a provider-facing request. Sensitive internal
	// metadata (tenant policy hints, audit trails) never crosses this seam.
	providerRequest := task.BuildProviderRequest(cmd)

	providerStarted := s.clock.Now()
	providerResponse, err := s.provider.Complete(callCtx, providerRequest)
	metadata.ProviderLatencyMs = millisecondsSince(providerStarted, s.clock)

	// Copy provider-reported metadata BEFORE checking err. Rationale:
	//
	//   - A 503 from a vendor may still have burned input tokens.
	//   - A validation failure on the *output* still consumed real usage.
	//   - The router reports the AGGREGATE attempt count even on failure
	//     paths that produced partial output.
	//
	// Capturing this now means downstream billing and analytics always see
	// ground truth, regardless of where we eventually terminate.
	metadata.Provider = providerResponse.Provider
	metadata.Model = providerResponse.Model
	metadata.InputTokens = providerResponse.InputTokens
	metadata.OutputTokens = providerResponse.OutputTokens
	metadata.TotalTokens = providerResponse.TotalTokens
	metadata.EstimatedCostUSD = providerResponse.EstimatedCostUSD
	metadata.Attempts = providerResponse.Attempts

	if err != nil {
		// Caller-driven cancellation takes priority over any provider error:
		// the caller no longer cares about the reason, they want the request
		// to stop. This also ensures we don't accidentally bill for a request
		// the client abandoned.
		if errors.Is(err, context.Canceled) {
			return fail(
				domain.KindRequestCanceled,
				domain.ExecutionRequestCanceled,
				domain.NewRequestCanceledError("provider operation canceled", err),
			)
		}

		// Deadline exceeded maps to the SAME domain error whether it fired
		// before or during the provider call. Callers see a consistent shape.
		//
		// NOTE: the ProviderRouter may internally retry per-attempt timeouts
		// and only surface this when the CALLER's deadline died. Internal
		// per-attempt timeouts never bubble up as context.DeadlineExceeded
		// because the router wraps them in AttemptTimeoutError.
		if errors.Is(err, context.DeadlineExceeded) {
			return fail(
				domain.KindDeadlineExceeded,
				domain.ExecutionDeadlineExceeded,
				domain.NewDeadlineExceededError(
					"provider operation exceeded caller deadline", err),
			)
		}

		// Everything else collapses to a stable gateway-level kind. The
		// Service deliberately does not decode ProviderFailureKind — that
		// detail belongs to the router layer. Keeping the mapping here would
		// couple the application layer to infrastructure taxonomy.
		return fail(
			domain.KindProviderUnavailable,
			domain.ExecutionProviderFailed,
			domain.NewProviderUnavailableError(
				"provider routing exhausted", err),
		)
	}

	// -------------------------------------------------------------------------
	// Stage 7: Trust boundary — decode and validate provider output
	// -------------------------------------------------------------------------
	// Everything above this line is trusted (our own code). Everything below
	// treats the provider's output as untrusted user input: parse, validate
	// against schema, validate against domain rules.
	validationStarted := s.clock.Now()
	result, validationErr := task.DecodeAndValidate(
		callCtx, cmd, providerResponse.RawOutput,
	)
	metadata.ValidationLatencyMs = millisecondsSince(validationStarted, s.clock)

	if validationErr != nil {
		// A validation failure is NOT free: the provider was invoked and
		// tokens were spent. Publish usage so billing/analytics capture it.
		metadata.ValidationStatus = domain.ValidationFailed
		metadata.Status = domain.ExecutionValidationFailed
		metadata.ErrorKind = domain.KindValidationFailed
		metadata.TotalGatewayLatencyMs = millisecondsSince(startedAt, s.clock)

		s.publishUsageEventAsync(callCtx, cmd, metadata)

		return nil, metadata, domain.NewValidationFailedError(
			"provider output failed capability validation",
			validationErr,
		)
	}

	// -------------------------------------------------------------------------
	// Stage 8: Success finalization + telemetry
	// -------------------------------------------------------------------------
	metadata.ValidationStatus = domain.ValidationPassed
	metadata.Status = domain.ExecutionSucceeded
	metadata.ErrorKind = ""
	metadata.TotalGatewayLatencyMs = millisecondsSince(startedAt, s.clock)

	s.publishUsageEventAsync(callCtx, cmd, metadata)

	return result, metadata, nil
}

// -----------------------------------------------------------------------------
// Context helpers
// -----------------------------------------------------------------------------

// withEffectiveDeadline reconciles the caller's context deadline with an
// optional command-level deadline, returning whichever fires first.
//
// Returns (ctx, nil) when no NEW context is created. This is deliberate:
// a non-nil cancel func should mean "there is a resource to release."
// Callers then write:
//
//	callCtx, cancel := withEffectiveDeadline(ctx, deadline)
//	if cancel != nil { defer cancel() }
//
// rather than blindly calling a no-op cancel function.
func withEffectiveDeadline(
	ctx context.Context,
	commandDeadline time.Time,
) (context.Context, context.CancelFunc) {
	// No command deadline → reuse the caller's context as-is.
	if commandDeadline.IsZero() {
		return ctx, nil
	}

	parentDeadline, hasParent := ctx.Deadline()

	// Parent already has an equal-or-earlier deadline; creating a child
	// would just add a timer for no benefit. Reuse.
	if hasParent && !commandDeadline.Before(parentDeadline) {
		return ctx, nil
	}

	// Command deadline is stricter → allocate a child context.
	return context.WithDeadline(ctx, commandDeadline)
}

// -----------------------------------------------------------------------------
// Clock helpers
// -----------------------------------------------------------------------------

// millisecondsSince returns the elapsed wall-clock milliseconds between
// `start` and the current clock time.
//
// Guards against negative durations because synthetic clocks (used in tests
// to simulate clock skew or rollback) may report earlier times than `start`.
// Returning 0 in that case keeps metadata sane rather than surfacing a
// nonsensical negative latency to dashboards.
func millisecondsSince(start time.Time, clock Clock) float64 {
	elapsed := clock.Now().Sub(start)
	if elapsed < 0 {
		return 0
	}
	return float64(elapsed.Microseconds()) / 1000.0
}

// -----------------------------------------------------------------------------
// Usage event publishing
// -----------------------------------------------------------------------------

// publishUsageEventAsync emits a best-effort, non-blocking usage/audit event.
//
// Design contract:
//
//   - This function NEVER returns an error to the caller. Observability must
//     not fail a business request. If the event can't be built or validated,
//     we drop it silently and let metrics/alerting surface the anomaly.
//
//   - The publish runs in a goroutine with a context detached from the
//     caller's cancellation. Rationale: once the result is computed, a
//     client disconnect must not cancel the billing event. The event is a
//     side effect of completion, not of the connection's lifetime.
//
//   - The parent context is used ONLY for its VALUES (trace IDs, tenant
//     scoping, request metadata). Cancellation and deadlines are stripped
//     by context.WithoutCancel.
//
//   - This is a SEAM, not a durability guarantee. Production requires an
//     outbox / Kafka producer with at-least-once semantics. The goroutine
//     here is the minimal async bridge; a future module replaces it.
func (s *Service) publishUsageEventAsync(
	parentCtx context.Context,
	cmd CompleteCommand,
	metadata ExecutionMetadata,
) {
	if s.usagePublisher == nil {
		return // telemetry disabled for this deployment
	}

	eventID, err := s.newEventID()
	if err != nil {
		// Event ID generation failure is an observability incident, not a
		// business failure. Swallow here; alert via metrics elsewhere.
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
		InputTokens:      metadata.InputTokens,
		OutputTokens:     metadata.OutputTokens,
		TotalTokens:      metadata.TotalTokens,
		EstimatedCostUSD: metadata.EstimatedCostUSD,
		// Attempts is the router's aggregate: 1 for a first-try success,
		// N for a request that succeeded after retries or fallback. Billing
		// uses this to reflect true vendor call economics.
		Attempts:         metadata.Attempts,
		Status:           metadata.Status,
		ValidationStatus: metadata.ValidationStatus,
		OccurredAt:       s.clock.Now(),
	}

	if err := event.Validate(); err != nil {
		// A malformed event is a bug in our own contract, not a user-facing
		// failure. Drop it and let the metrics layer surface the anomaly.
		return
	}

	// context.WithoutCancel preserves VALUES from the caller's context
	// (OpenTelemetry span IDs, tenant scoping, request tracing) while
	// stripping CANCELLATION and DEADLINES. This is the correct primitive:
	//
	//   - Background() would lose trace context and break distributed
	//     tracing — the publish would appear as an orphan span.
	//   - Plain parentCtx would abort the publish on client disconnect,
	//     dropping billing events.
	//
	// We strip cancellation because the user's connection lifetime should
	// not gate our internal accounting.
	publishCtx := context.WithoutCancel(parentCtx)

	// Fire-and-forget. The goroutine's lifetime is independent of the caller.
	// If this becomes a bottleneck, swap for a bounded worker pool or a
	// real message broker client (Kafka producer with internal batching).
	go func() {
		_ = s.usagePublisher.PublishUsageEvent(publishCtx, event)
	}()
}
