package redis

// TODO: implement port

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
)

// ErrNotImplemented explicitly communicates the Sprint 01 maturity boundary.
//
// This adapter is a compile-safe seam, not evidence of a working distributed
// Redis implementation. The first real adapter belongs to the later Redis
// sprint defined by the project plan.
var ErrNotImplemented = errors.New("redis adapter not implemented")

type Config struct {
	Address  string
	Username string
	Password string
	Database int
	Timeout  time.Duration
}

type Repository struct {
	cfg Config
}

func NewRepository(cfg Config) (*Repository, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("redis address is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Second
	}
	return &Repository{cfg: cfg}, nil
}

// Expected future adapter contracts:
//
// var _ application.BudgetStore = (*Repository)(nil)
// var _ application.RateLimiter = (*Repository)(nil)
// var _ application.CompletionCache = (*Repository)(nil)
//
// These assertions remain commented because the runtime methods intentionally
// return ErrNotImplemented in Sprint 01.

func (r *Repository) Reserve(
	_ context.Context,
	_ application.BudgetReservation,
) error {
	return ErrNotImplemented
}

func (r *Repository) Reconcile(
	_ context.Context,
	_ application.BudgetReservation,
	_ float64,
) error {
	return ErrNotImplemented
}

func (r *Repository) Allow(
	_ context.Context,
	_ string,
) (application.RateLimitDecision, error) {
	return application.RateLimitDecision{}, ErrNotImplemented
}

func (r *Repository) Get(
	_ context.Context,
	_ application.CacheKey,
) (application.CachedCompletion, bool, error) {
	return application.CachedCompletion{}, false, ErrNotImplemented
}

func (r *Repository) Put(
	_ context.Context,
	_ application.CacheKey,
	_ application.CachedCompletion,
	_ time.Duration,
) error {
	return ErrNotImplemented
}
