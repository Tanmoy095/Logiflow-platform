# ADR-013 — Gateway Domain Policy and Privacy-Safe Usage Events

## Status

Accepted

## Decision

Keep gateway-wide execution invariants in `domain/policy.go`.

Model AI usage/audit facts in `domain/events.go`.

The domain defines event data but does not publish it. `application.UsagePublisher` remains the port; Kafka/outbox implementations stay in infrastructure.

## Business need

The gateway has two business-facing responsibilities before and after provider execution:

### Before provider execution

Reject invalid work cheaply:

```text
contract version
tenant_id
request_id
task type
evidence reference bounds
```

This prevents unnecessary provider spend and protects a multi-tenant system from malformed or excessive requests.

### After execution

Record enough information for:

```text
tenant billing
FinOps
SLA analysis
provider comparison
audit
incident investigation
```

without exporting sensitive prompt/document content.

## Metadata-only event contract

`AIUsageEvent` may contain:

```text
event_id
event_version
tenant_id
request_id
task_type
provider
model
input_tokens
output_tokens
total_tokens
estimated_cost_usd
attempts
status                    (execution status)
validation status
occurred_at
```

`Attempts` carries the `ProviderRouter` aggregate across all retries and all
providers for the request. It is not a per-provider attempt count. Billing
uses it to reflect true vendor-call economics: a request that succeeded on
attempt 3 cost more than one that succeeded on attempt 1.

It must not contain:

```text
raw prompt
full completion
document content
retrieved chunks
provider credentials
secrets
```

The tracking document explicitly describes the event as an itemized, privacy-safe usage receipt and records `llm-gateway.usage.v1`.

## Event timing decision

The gateway records usage metadata whenever the provider returned a usable `ProviderResponse`, even when downstream validation later fails.

Reason:

```text
provider executed
   ↓
provider may have consumed tokens
   ↓
validation failed
```

A validation failure must not erase evidence that provider resources were consumed.

The event's `validation_status` therefore describes the trust outcome, while token fields describe actual provider usage.

The event contract is enforced rather than advisory:

```text
total_tokens                 == input_tokens + output_tokens
input_tokens/output_tokens/total_tokens  non-negative
estimated_cost_usd            finite (not NaN/Inf), non-negative
attempts                      non-negative
all required strings          non-blank after TrimSpace
occurred_at                   non-zero
```

The total-token arithmetic check catches integration bugs where a provider or
the router corrupts token accounting. The finite-value check on
`estimated_cost_usd` prevents `NaN` or `Inf` from silently corrupting billing.

## Async publication decision

Publishing usage events must not block the critical completion response.

The current application design therefore exposes an asynchronous `UsagePublisher` port. A durable outbox/Kafka implementation remains a future infrastructure concern.

This preserves stateless application execution and separates operational telemetry from the synchronous AI result path.

## Async publication context

The publication goroutine runs with:

```go
publishCtx := context.WithoutCancel(parentCtx)
```

This preserves values from the caller's context, including OpenTelemetry span
IDs, tenant scoping, and request tracing, while stripping cancellation and
deadlines. It is the correct primitive:

- `context.Background()` would sever distributed tracing and make the publish
  appear as an orphan span;
- the plain caller context would abort publication on client disconnect,
  silently dropping billing events.

The user's connection lifetime must not gate internal accounting.

## Two error vocabularies

The domain `Kind` vocabulary is caller-facing and stable:

```text
invalid_argument
unsupported_task
contract_mismatch
request_canceled
deadline_exceeded
provider_unavailable
validation_failed
invalid_event
internal
```

The router's `ProviderFailureKind` vocabulary is adapter-emitted and routing-facing:

```text
unavailable
rate_limited
server_error
authentication
invalid_request
```

The `Service` deliberately collapses every `ProviderFailureKind` into
`KindProviderUnavailable` at Stage 6, or into `KindDeadlineExceeded` /
`KindRequestCanceled` when the failure is context-driven. Callers never need
to reason about vendor-specific distinctions.

## Alternatives considered

### Synchronous PostgreSQL billing write

Rejected for the hot path because database latency and connection pressure become coupled to every model request.

### Raw prompt in Kafka

Rejected because internal event streams can be consumed by analytics, logging and billing systems; sensitive user data should not spread across those systems.

### Domain package publishing directly to Kafka

Rejected because it violates dependency direction.

## Trade-offs

### Benefits

- privacy-safe usage telemetry;
- tenant-attributed costs;
- replayable event shape;
- domain remains framework independent;
- billing can scale independently.

### Costs

- asynchronous event delivery creates eventual consistency;
- best-effort goroutines are not a durable queue;
- production implementation eventually needs Kafka/outbox durability and reconciliation.

## Evidence

Module 3 tracking records the domain event implementation, validation and metadata-only privacy rule.

## Interview question

> Why is `AIUsageEvent` in domain if Kafka is infrastructure?

Expected answer:

> The domain owns the business fact — AI usage occurred for a tenant and has a measurable cost and outcome. Kafka is only one mechanism for transporting that fact, so publication remains an application port implemented by infrastructure.
