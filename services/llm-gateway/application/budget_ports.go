package application

import (
	"context"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

// BudgetReservation represents an admission reservation against a tenant's
// shared AI spending allowance.
//
// A reservation is intentionally tied to request identity so later
// reconciliation can distinguish concurrent executions belonging to the same
// tenant.
type BudgetReservation struct {
	TenantID      string
	RequestID     string
	EstimatedCost float64
	ExpiresAt     time.Time
}

// BudgetStore is the application-owned contract for tenant budget enforcement.
//
// The interface expresses a business capability instead of Redis operations.
// A future adapter can satisfy this with Redis, another datastore, or a
// managed quota service without changing application orchestration.
type BudgetStore interface {
	Reserve(ctx context.Context, reservation BudgetReservation) error
	Reconcile(ctx context.Context, reservation BudgetReservation, actualCost float64) error
}

// RateLimitDecision is the result of a tenant admission check.
type RateLimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
	Remaining  int64
}

// RateLimiter is the application port for traffic admission control.
//
// The implementation may use a distributed token bucket later. The use case
// depends only on the decision semantics, not the storage mechanism.
type RateLimiter interface {
	Allow(ctx context.Context, tenantID string) (RateLimitDecision, error)
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

// CompletionCache is the application port for trusted-result caching.
//
// The `CachedCompletion` shape is deliberately typed instead of using `any` so
// cache callers cannot silently store arbitrary runtime objects.
type CompletionCache interface {
	Get(ctx context.Context, key CacheKey) (CachedCompletion, bool, error)
	Put(ctx context.Context, key CacheKey, value CachedCompletion, ttl time.Duration) error
}
