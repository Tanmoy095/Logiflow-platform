// services/llm-gateway/application/ports.go
package application

import (
	"context"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

/*

Provider is the application-owned port for external AI completion.

The provider returns raw, untrusted output.
The application layer is responsible for parsing,
schema validation, domain validation, and creation
of the trusted domain.CompletionResult.


Why does your Provider interface return string instead of CompletionResult?”


Because provider output is untrusted external data. I deliberately keep the Provider port raw-output oriented
so the application owns parsing and schema validation, and the domain owns business invariants.
CompletionResult is created only after those checks pass, making it a trust-bearing type rather than a provider transport type.

*/

type Provider interface {
	Complete(
		ctx context.Context,
		req ProviderRequest,
	) (ProviderResponse, error)

	// Name returns a short identifier for the provider (e.g., "openai", "fake").
	// Used for observability metadata.
	Name() string
}

//Flexible Kafka/AWS Kinesis/PostgreSQL Outbox Table implementation for publishing usage
// events. The application defines what it needs; infrastructure decides how to publish.

type UsagePublisher interface {
	PublishUsageEvent(ctx context.Context, event domain.AIUsageEvent) error
}

// Clock lets deterministic tests control time without replacing time.Now
// throughout the business code. The default implementation is systemClock.
type Clock interface {
	Now() time.Time
}
type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

//--------------------------Budget_PORTS------------------------------

//------Rate Limit --> Budget Reservation --> Cache Lookup --> ProviderRouter-------------

// RateLimiter is the application port for traffic admission control.
// The implementation may use a distributed token bucket later. The use case
// depends only on the decision semantics, not the storage mechanism.
type RateLimiter interface {
	Allow(ctx context.Context, tenantID string) (RateLimitDecision, error)
}

// BudgetStore is the application-owned contract for tenant budget enforcement.
// The interface expresses a business capability instead of Redis operations.
// A future adapter can satisfy this with Redis, another datastore, or a
// managed quota service without changing application orchestration.
type BudgetStore interface {
	Reserve(ctx context.Context, reservation BudgetReservation) error
	Reconcile(ctx context.Context, reservation BudgetReservation, actualCost float64) error
}

// Cache Lookup
// CompletionCache is the application port for trusted-result caching.
//
// The `CachedCompletion` shape is deliberately typed instead of using `any` so
// cache callers cannot silently store arbitrary runtime objects.
type CompletionCache interface {
	Get(ctx context.Context, key CacheKey) (CachedCompletion, bool, error)
	Put(ctx context.Context, key CacheKey, value CachedCompletion, ttl time.Duration) error
}

// BudgetReservation represents an admission reservation against a tenant's
// shared AI spending allowance.
// A reservation is intentionally tied to request identity so later
// reconciliation can distinguish concurrent executions belonging to the same
// tenant.
type BudgetReservation struct {
	TenantID      string
	RequestID     string
	EstimatedCost float64
	ExpiresAt     time.Time
}

// RateLimitDecision is the result of a tenant admission check.
type RateLimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
	Remaining  int64
}

// CacheKey is an opaque identifier produced by application cache policy.
//
// It must never be built by concatenating raw prompt or document content in
// infrastructure code.
type CacheKey string

// CachedCompletion is a serialized trusted-result candidate.
//
// Cache infrastructure stores the serialized representation only. On a cache
// read, the application still re-establishes task/schema/domain validity before
// returning a trusted result. The cache is an optimization, not a trust bypass.
type CachedCompletion struct {
	TaskType      domain.TaskType
	SchemaVersion string
	Payload       []byte
}
