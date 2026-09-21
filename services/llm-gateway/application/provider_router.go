package application

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Retry — If one provider fails transiently, try it again (inner loop).
// Fallback / Failover — If one provider keeps failing, move to the next provider (outer loop).
// So the router acts as a proxy that transparently hides flaky vendors from the rest of the app.

// RetryPolicy governs retry behavior against a SINGLE vendor instance.
type RetryPolicy struct {
	MaxAttemptsPerProvider int // Max retries (e.g., 3 attempts)

	AttemptTimeout time.Duration // Sub-budget per HTTP attempt (e.g., 2s)
	// backoff can be exponential or fixed
	Backoff func(attempt int) time.Duration // Dynamic backoff calculator, backoff needs because some providers (like OpenAI) have rate limits that are not per-request, but per-minute. So if we retry too quickly, we will hit the rate limit again.

	Retryable func(error) bool // Decision function for error retryability
}

// while backoff means "wait and try the same provider again," fallback means "give up on this provider and switch to a completely different provider."

// FallbackPolicy governs when to switch to the NEXT vendor in the sequence.
type FallbackPolicy struct {
	// Enabled is the master On/Off switch for backup routing.
	// If true, severe failures trigger an automatic failover to the next vendor.
	// If false, any primary vendor failure instantly bubbles up to the user.
	Enabled bool

	MaxProviders int // Circuit breaker limit on total vendors tried per request
	// Allowed is the gatekeeper function. It inspects the error to decide if
	// switching vendors will actually help (e.g., true for 429 Rate Limits or 503 Outages;
	// false for 400 Bad Requests where changing the provider won't fix your broken code).
	Allowed func(error) bool // Decision function for fallback eligibility
}

// Sleeper intercepts time delays to allow instant unit testing and immediate cancellation via context.
type Sleeper func(context.Context, time.Duration) error

// AttemptTimeoutError indicates a local, router-enforced timeout fired for a single attempt.
// It separates individual vendor slowness from a global request deadline exhaustion,
// telling the execution engine exactly when it is safe to trigger a backoff or fallback.
type AttemptTimeoutError struct {
	Cause error
}

func (e *AttemptTimeoutError) Error() string {
	if e.Cause == nil {
		return "provider attempt timeout"
	}
	return "provider attempt timeout: " + e.Cause.Error()
}

func (e *AttemptTimeoutError) Unwrap() error { return e.Cause }

type ProviderRouter struct {
	providers []Provider
	retry     RetryPolicy
	fallback  FallbackPolicy
	sleep     Sleeper
}

// Enforce interface compliance at compile-time.
var _ Provider = (*ProviderRouter)(nil)

// NewProviderRouter initializes and sanitizes the router with safe defaults.
func NewProviderRouter(
	providers []Provider,
	retry RetryPolicy,
	fallback FallbackPolicy,
	sleep Sleeper,
) (*ProviderRouter, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("provider router requires at least one provider")
	}

	for i, provider := range providers {
		if provider == nil {
			return nil, fmt.Errorf("provider[%d] must not be nil", i)
		}
	}

	// Defensive Fallback Defaults
	if retry.MaxAttemptsPerProvider <= 0 { // if the user has not set any value for MaxAttemptsPerProvider, we will set it to 1. This means that the router will make at least one attempt to call the provider before giving up.
		retry.MaxAttemptsPerProvider = 1
	}
	if retry.Backoff == nil { // if the user has not set any value for Backoff, we will set it to DefaultBackoff. This means that the router will use a default backoff strategy to wait before retrying the provider.
		retry.Backoff = DefaultBackoff
	}

	if retry.Retryable == nil {
		retry.Retryable = DefaultRetryableProviderError
	}
	if fallback.MaxProviders <= 0 || fallback.MaxProviders > len(providers) {
		fallback.MaxProviders = len(providers)
	}
	if fallback.Allowed == nil {
		fallback.Allowed = DefaultFallbackAllowed
	}

	if sleep == nil {
		sleep = DefaultSleeper
	}

	// Defensive Slice Copy: Clones the slice into a fresh memory location
	// so external runtime changes to the original list cannot corrupt the router.
	providerCopy := append([]Provider(nil), providers...)

	return &ProviderRouter{
		providers: providerCopy,
		retry:     retry,
		fallback:  fallback,
		sleep:     sleep,
	}, nil
}

// Name satisfies the Provider interface and identifies this proxy wrapper
// inside application logs, system metrics, and debugging traces.
func (r *ProviderRouter) Name() string {
	return "provider-router"
}

// Complete handles the outer execution loop (Provider Fallback Routing).
func (r *ProviderRouter) Complete(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	var lastErr error
	var totalAttempts int
	// OUTER LOOP: Iterate over configured providers (Primary -> Secondary -> Tertiary)

	for providerIndex, provider := range r.providers {
		// 1. Safety budget: never try more than MaxProviders vendors.
		if providerIndex >= r.fallback.MaxProviders {
			break // Exceeded configured vendor safety budget
		}

		// PRE-FLIGHT CHECK: Parent context check before attempting work,If the caller already canceled the request, stop immediately.
		if err := ctx.Err(); err != nil {
			return ProviderResponse{Attempts: totalAttempts}, err
		}

		// Execute inner retry loop against current provider, Run the INNER retry loop against this provider.
		response, attempts, err := r.completeWithRetries(ctx, provider, req)

		totalAttempts += attempts

		// 4. Success → return immediately, no more providers.
		if err == nil {
			response.Attempts = totalAttempts
			return response, nil // SUCCESS: Return early
		}
		// lastErr is updated to the most recent error encountered during the provider attempts. This allows the router to keep track of the last failure reason, which can be useful for logging or returning a meaningful error message to the caller after all providers have been exhausted.
		lastErr = err

		// 5. If the outer (parent) context died mid-inner-loop, bail out.
		if parentErr := ctx.Err(); parentErr != nil {
			return ProviderResponse{Attempts: totalAttempts}, parentErr
		}

		// 6. Fallback policy check: is this error even fallback-eligible?
		//    e.g. 400 Bad Request → don't bother trying another vendor.
		//    e.g. 503 Unavailable → try the next vendor.
		if !r.fallback.Enabled || !r.fallback.Allowed(err) {
			break // Error is non-fallbackable (e.g. 401 Unauthorized or 400 Bad Request)
		}
	}

	// 7. Exhausted all providers → return the last error encountered.
	if lastErr == nil {
		lastErr = errors.New("provider router exhausted without a response")
	}

	return ProviderResponse{Attempts: totalAttempts}, lastErr
}

// completeWithRetries handles the inner execution loop (Retries against ONE Provider).
func (r *ProviderRouter) completeWithRetries(parent context.Context, provider Provider, req ProviderRequest) (ProviderResponse, int, error) {
	var (
		attempts int
		lastErr  error
	)
	// INNER LOOP: up to MaxAttemptsPerProvider attempts against ONE vendor.

	for attempt := 1; attempt <= r.retry.MaxAttemptsPerProvider; attempt++ {
		attempts = attempt

		// 1: Build a per-attempt context with its own sub-timeout.
		attemptCtx := parent // Start with the parent context, which may have its own deadline or cancellation signal.
		cancel := func() {}  // no-op cancel by default
		// Enforce local per-attempt sub-timeout if configured
		if r.retry.AttemptTimeout > 0 {
			// e.g. parent has 10s budget, but each attempt gets 2s max.
			attemptCtx, cancel = context.WithTimeout(parent, r.retry.AttemptTimeout)
		}

		// STEP 2: Dispatch the actual HTTP call to the vendor.
		response, err := provider.Complete(attemptCtx, req)

		// STEP 3: Read attemptCtx.Err() BEFORE calling cancel()!
		//
		//   Why? cancel() forces ctx.Err() to return context.Canceled,
		//   which erases the fact that the context was actually a
		//   DeadlineExceeded. We need that evidence to tell the
		//   difference between:
		//     (a) "the attempt timed out" (retryable)
		//     (b) "the caller canceled" (not retryable)
		// ------------------------------------------------------------
		attemptErr := attemptCtx.Err()
		cancel() // always release the context's resources

		// STEP 4: Success → return immediately.
		if err == nil {
			return response, attempts, nil
		}

		// STEP 5: Timeout discrimination.
		//
		//   If OUR sub-timeout fired (DeadlineExceeded) but the
		//   parent context is still alive, wrap the error so that
		//   downstream policies know "this was a transient per-attempt
		//   timeout", not a real vendor failure.
		// ------------------------------------------------------------
		if parent.Err() == nil && errors.Is(attemptErr, context.DeadlineExceeded) {
			err = &AttemptTimeoutError{Cause: err}
		}

		lastErr = err

		// ------------------------------------------------------------
		// STEP 6: If the CALLER canceled, do not retry — propagate.
		// ------------------------------------------------------------
		if parentErr := parent.Err(); parentErr != nil {
			return ProviderResponse{}, attempts, parentErr
		}

		// ------------------------------------------------------------
		// STEP 7: Stop if error is non-retryable OR budget is spent.
		//   e.g. 401 Unauthorized → do not retry
		//   e.g. 429 Rate Limited  → retry
		// ------------------------------------------------------------
		if !r.retry.Retryable(err) || attempt == r.retry.MaxAttemptsPerProvider {
			break
		}

		// ------------------------------------------------------------
		// STEP 8: Backoff with cancellation awareness.
		//   DefaultBackoff: 50ms, 100ms, 200ms, 400ms, ... capped 1s.
		//   Sleep is interruptible: if the caller cancels during the
		//   sleep, we return immediately instead of blocking.
		// Execute cancellation-aware backoff pause
		if err := r.sleep(parent, r.retry.Backoff(attempt)); err != nil {
			return ProviderResponse{}, attempts, err // Context canceled during backoff sleep
		}
	}
	return ProviderResponse{}, attempts, lastErr
}

// DefaultRetryableProviderError retries only known transient operational
// failures and router-owned attempt timeouts.
func DefaultRetryableProviderError(err error) bool {
	if err == nil {
		return false
	}

	var attemptTimeout *AttemptTimeoutError
	if errors.As(err, &attemptTimeout) {
		return true
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	return IsRetryableProviderError(err)
}

// DefaultFallbackAllowed follows the same operational-only rule.
//
// Semantic validation failures never reach this function because the provider
// router returns before validation starts. The application service decides what
// happens after validation.
func DefaultFallbackAllowed(err error) bool {
	if err == nil {
		return false
	}

	var attemptTimeout *AttemptTimeoutError
	if errors.As(err, &attemptTimeout) {
		return true
	}

	return IsFallbackEligibleProviderError(err)
}

// DefaultBackoff calculates binary exponential backoff with a hard cap.
func DefaultBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	const (
		base = 50 * time.Millisecond
		max  = 1 * time.Second
	)

	// Shift overflow protection (guards against attempt >= 64 bit shift integer wraparound)
	if attempt >= 6 {
		return max
	}

	// Bitwise left shift: base * 2^(attempt-1)
	// Attempt 1: 50ms << 0 = 50ms
	// Attempt 2: 50ms << 1 = 100ms
	// Attempt 3: 50ms << 2 = 200ms
	// Attempt 4: 50ms << 3 = 400ms
	return base << (attempt - 1)
}

// DefaultSleeper performs a cancellation-aware sleep using a non-blocking select statement.
func DefaultSleeper(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop() // Prevent memory leaks by cleaning up the timer channel

	select {
	case <-timer.C:
		return nil // Backoff delay completed successfully
	case <-ctx.Done():
		return ctx.Err() // Client canceled request during sleep delay -> Unblock immediately!
	}
}
