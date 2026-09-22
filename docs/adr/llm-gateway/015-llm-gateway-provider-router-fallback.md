# ADR-015 — Provider Router, Bounded Retry, Timeout and Fallback

## Status

Accepted for Module 4 implementation

## Decision

Implement `application.ProviderRouter` as an implementation of the existing application `Provider` interface.

The router will:

1. use an explicit ordered provider list;
2. apply a bounded retry policy per provider;
3. apply a router-owned per-attempt timeout;
4. never extend the caller's context deadline;
5. retry only classified transient operational failures;
6. fall back only when the failure is eligible and parent context remains alive;
7. return normalized `ProviderResponse` data;
8. preserve the actual selected provider in response metadata;
9. never perform semantic validation or automatically fallback on validation/domain failures.

This matches the existing accepted Module 4 decision.

## Business need

External AI providers can fail through:

```text
timeouts
429 rate limiting
5xx responses
network failures
temporary capacity pressure
```

A single-provider architecture would allow one external dependency to stall or degrade internal logistics workflows.

LogiFlow therefore needs a controlled reliability boundary without exposing vendor-specific recovery logic to every caller.

## Reliability model

```text
                 ProviderRouter
                      │
          ┌───────────┼───────────┐
          ▼           ▼           ▼
       attempt      classify     budget
          │           │           │
          ▼           ▼           ▼
       retry       fallback    deadline
```

## Retry policy

Default retry candidates:

```text
provider unavailable
rate limited
server error
router-owned attempt timeout
```

Default non-retryable:

```text
caller cancellation
caller deadline
authentication
invalid request
```

## Timeout policy

Two time budgets exist:

```text
parent request deadline
        +
router attempt timeout
```

The effective execution window is always bounded by the parent.

Example:

```text
Parent deadline: 2s
Attempt timeout: 400ms

attempt 1 -> 400ms
attempt 2 -> 400ms
fallback  -> remaining budget
```

If the parent expires:

```text
stop immediately
no fallback
no more retries
```

The tracking design explicitly distinguishes these boundaries.

## Why a typed attempt timeout exists

`context.DeadlineExceeded` can mean either:

```text
router-owned attempt timeout
```

or:

```text
caller-owned overall deadline
```

These must produce different recovery decisions.

Therefore the router creates `AttemptTimeoutError` for its own timeout and checks the parent context separately.

## The `attemptCtx.Err()` ordering invariant

Inside the inner retry loop, the router reads the attempt context's error
before calling the cancel function:

```go
attemptCtx, cancel := context.WithTimeout(parent, attemptTimeout)
response, err := provider.Complete(attemptCtx, req)

attemptErr := attemptCtx.Err() // read first
cancel()                       // then release

if parent.Err() == nil && errors.Is(attemptErr, context.DeadlineExceeded) {
   err = &AttemptTimeoutError{Cause: err}
}
```

Calling `cancel()` first forces `ctx.Err()` to become `context.Canceled`, even
when the deadline actually fired. Reading first preserves the distinction
between the router's own per-attempt budget (`context.DeadlineExceeded`,
retryable) and caller cancellation (`context.Canceled`, terminal). This
ordering is load-bearing: swapping the lines silently disables timeout retry
behavior and must not be changed in a refactor.

## ProviderFailureKind lives in the application package

The normalized classification types are `application.ProviderFailureKind` and
`application.ProviderError`. `ProviderError` wraps a `Kind` and optional
`Cause`, and supports `errors.As` / `errors.Unwrap`.

```text
unavailable
rate_limited
server_error
authentication
invalid_request
```

Real adapters construct `ProviderError` values at their boundary. The router
classifies them without string matching:

```go
var perr *application.ProviderError
if errors.As(err, &perr) {
   switch perr.Kind {
   case application.ProviderFailureUnavailable,
      application.ProviderFailureRateLimited,
      application.ProviderFailureServerError:
      return true // retryable
   }
}
return false
```

The fake adapter in `infrastructure/provider/fake.go` exposes typed sentinels
with these kinds, so tests never guess at classification.

## Attempts propagation

The router aggregates the total number of provider calls across retries and
providers and stores it in `ProviderResponse.Attempts`:

```text
ProviderRouter attempts
      ↓
ProviderResponse.Attempts
      ↓  copied by Service at Stage 6, before error classification
ExecutionMetadata.Attempts
      ↓
AIUsageEvent.Attempts
```

A raw provider leaves `Attempts` at zero. It makes one call and has no
business knowing about retries. Only the router owns the aggregate and fills
it in.

## Parent-context death is terminal

When the parent context dies during an attempt (`parent.Err() != nil`), the
router must not wrap the error in `AttemptTimeoutError`, must return the parent
error immediately, and must not retry or fall back. The caller has already
given up; further work would be waste and could be billed without a consumer.

## Fallback policy

Fallback is permitted only for operational failures.

```text
primary timeout
   ↓
retry primary
   ↓
retry exhausted
   ↓
secondary eligible?
   ↓
secondary attempt
```

Fallback is forbidden by default for:

```text
malformed JSON
schema failure
domain invariant violation
wrong shipment identity
```

because these failures mean the provider returned untrusted content, not that the network dependency was unavailable.

## Alternatives considered

### Direct calls from `Service`

Rejected because retries/fallback would become coupled to the main use-case orchestrator.

### Retry every error

Rejected because permanent errors would waste time and money and can amplify outages.

### Unbounded retry

Rejected because it can create retry storms and violate request deadlines.

### Fallback after semantic validation failure

Rejected because it doubles cost/latency and can hide systematic model/prompt defects.

### Full distributed circuit breaker in this module

Deferred.

Circuit state needs telemetry, thresholds, cooldown policy, concurrency control and often shared state. The current staged project plan keeps stronger circuit-breaker work as a later maturity layer.

## Trade-offs

### Availability benefit

A transient failure can be absorbed without forcing upstream retries.

### Cost risk

A retry is another provider call and may generate additional provider cost.

### Latency risk

Fallback adds serial network attempts.

### Quality risk

Different providers can produce different outputs.

Therefore reliability policy must remain bounded and observable.

## Observability requirements

The router should expose enough metadata to answer:

```text
Which provider actually served the request?
How many attempts occurred?
How many retries were used?
Was fallback invoked?
How much provider latency was spent?
Was the failure operational or semantic?
```

The existing tracking design already records provider/model/token/cost/attempt concepts.

## Testing requirements

The implemented router tests are:

```text
TestProviderRouterPrimarySuccessDoesNotCallFallback
TestProviderRouterRetriesRetryableFailureThenSucceeds
TestProviderRouterTimeoutRetriesThenFallsBack
TestProviderRouterAuthenticationDoesNotRetryOrFallback
TestProviderRouterCallerDeadlinePreventsFallback
TestProviderRouterCancellationDoesNotStartAnyProvider
```

## Consequences

Positive:

- provider failure semantics are centralized;
- upstream services keep one stable contract;
- deterministic tests are possible;
- retry behavior is bounded;
- deadline behavior is explicit.

Negative:

- more orchestration code;
- fallback can increase cost and latency;
- provider quality may differ;
- stronger resilience requires later circuit and budget mechanisms.

## Migration path

Module 4 evolves naturally toward:

```text
ProviderRouter
   +
CircuitBreaker
   +
Redis budget/rate policy
   +
real provider adapters
   +
OpenTelemetry
   +
quality/evaluation signals
```

without changing `CompleteCommand` or shipment-risk domain rules.

## Recommended commits

```text
feat(llm-gateway/application): classify provider failures for reliability policy
feat(llm-gateway/application): add bounded provider retry and deadline-aware fallback
test(llm-gateway/application): cover provider router reliability semantics
refactor(llm-gateway): route provider execution through reliability boundary
```

## Interview question

> How do you stop a failing LLM provider from taking down the gateway?

Expected answer:

> The gateway treats the provider as an unreliable dependency. A ProviderRouter applies bounded per-provider retries, short attempt deadlines, parent-context cancellation and controlled fallback for transient operational failures. It does not retry semantic validation failures, because those represent untrusted model output rather than provider availability.
