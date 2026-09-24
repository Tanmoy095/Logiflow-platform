//services/llm-gateway/application/budgetPolicy.go

package application

/*

  Standard microservices interact with deterministic software dependencies, whereas
  Large Language Models (LLMs) are probabilistic and inherently unpredictable. Calling
  an LLM provider directly without deterministic guardrails exposes the platform to
  severe financial and operational risks:

    - Unbounded Financial Spend: A buggy retry loop, malformed upstream request, or
      recursive service call can dispatch thousands of requests in seconds, consuming
      a tenant's monthly budget in minutes.

    - Resource Starvation: An aggressive caller or noisy tenant can overwhelm shared
      provider rate limits, starving other business tenants of capacity.

    - Overspend Race Conditions: When concurrent requests check remaining balances
     non-atomically, parallel calls can pass balance checks simultaneously, resulting
      in negative tenant balances.

  budget_policy.go mitigates these risks by placing deterministic cost and admission
  guardrails in front of the probabilistic external provider, converting the gateway
  from an API proxy into an authoritative financial and operational control plane.

*/

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// These errors are application-policy outcomes. They deliberately do not
// mention Redis, Kafka, or another infrastructure product.
var (
	ErrBudgetDenied                   = errors.New("tenant budget denied")
	ErrBudgetDependencyUnavailable    = errors.New("budget dependency unavailable")
	ErrRateLimited                    = errors.New("tenant rate limited")
	ErrRateLimitDependencyUnavailable = errors.New("rate-limit dependency unavailable")
)

// BudgetPolicy owns deterministic admission decisions.
// It does NOT own provider execution, vendor SDKs, Redis clients, HTTP
// handlers, or database transactions. Those concerns remain behind ports.

type BudgetPolicy struct {
	Budget BudgetStore
	Limit  RateLimiter
	Clock  func() time.Time

	// ReservationTTL bounds the lifetime of an estimated spend reservation.
	// The production value will eventually be policy/configuration driven.
	ReservationTTL time.Duration
}

// NewBudgetPolicy constructs the cost/admission policy.
func NewBudgetPolicy(
	budget BudgetStore,
	limiter RateLimiter,
	now func() time.Time,
) (*BudgetPolicy, error) {
	if budget == nil {
		return nil, fmt.Errorf("budget store is required")
	}
	if limiter == nil {
		return nil, fmt.Errorf("rate limiter is required")
	}
	if now == nil {
		now = time.Now
	}

	return &BudgetPolicy{
		Budget:         budget,
		Limit:          limiter,
		Clock:          now,
		ReservationTTL: 60 * time.Second, //default value; can be overridden by configuration
	}, nil
}

// Admit performs the two authoritative pre-provider controls:
//
//  1. rate limit;
//  2. tenant budget reservation.
//
// The reservation is returned to the caller because actual provider usage may
// later need to reconcile the estimate.
//
// A dependency failure is fail-closed: uncertainty must not become permission to spend money

func (p *BudgetPolicy) Admit(
	ctx context.Context,
	tenantID string,
	requestID string, //requestID is used for logging and tracing; it does not affect the budget or rate limit decision
	estimatedCost float64,
) (BudgetReservation, error) {

	if tenantID == "" {
		return BudgetReservation{}, fmt.Errorf("%w: tenant_id is required", ErrBudgetDenied)
	}
	if requestID == "" {
		return BudgetReservation{}, fmt.Errorf("%w: request_id is required", ErrBudgetDenied)
	}
	if estimatedCost < 0 {
		return BudgetReservation{}, fmt.Errorf("%w: estimated cost must not be negative", ErrBudgetDenied)
	}
	// The rate limiter is the first authoritative check. It is intentionally
	// placed before the budget check to prevent unnecessary budget reservations.
	decision, err := p.Limit.Allow(ctx, tenantID)

	if err != nil {
		return BudgetReservation{}, fmt.Errorf("%w: %v", ErrRateLimitDependencyUnavailable, err)
	}
	if !decision.Allowed {
		return BudgetReservation{}, fmt.Errorf(
			"%w: retry_after=%s",
			ErrRateLimited,
			decision.RetryAfter,
		)
	}
	// The reservation is stored in the budget store for later reconciliation.
	reservation := BudgetReservation{
		TenantID:      tenantID,
		RequestID:     requestID,
		EstimatedCost: estimatedCost,
		ExpiresAt:     p.Clock().Add(p.ReservationTTL),
	}
	// The reservation is stored in the budget store for later reconciliation.
	if err := p.Budget.Reserve(ctx, reservation); err != nil {
		return BudgetReservation{}, fmt.Errorf("%w: %v", ErrBudgetDependencyUnavailable, err)
	}

	return reservation, nil

}

// Reconcile replaces the reservation estimate with actual observed provider
// cost. The interface exists now so later durable billing/reconciliation can be
// added without changing the completion contract.
func (p *BudgetPolicy) Reconcile(
	ctx context.Context,
	reservation BudgetReservation,
	actualCost float64,
) error {
	if actualCost < 0 {
		return fmt.Errorf("actual cost must not be negative")
	}
	if err := p.Budget.Reconcile(ctx, reservation, actualCost); err != nil {
		return fmt.Errorf("budget reconciliation failed: %w", err)
	}
	return nil
}

// IsFailClosed reports whether an admission error should stop provider spend.
func IsFailClosed(err error) bool {
	return errors.Is(err, ErrBudgetDenied) ||
		errors.Is(err, ErrBudgetDependencyUnavailable) ||
		errors.Is(err, ErrRateLimited) ||
		errors.Is(err, ErrRateLimitDependencyUnavailable)
}
