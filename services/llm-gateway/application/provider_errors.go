//services/llm-gateway/application/provider_errors.go

package application

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ProviderFailureKind is the normalized operational failure vocabulary used
// by the router.
//
// Real infrastructure adapters will map vendor-specific HTTP/SDK failures
// into these kinds. The router must not parse vendor strings or HTTP codes.
type ProviderFailureKind string

const (
	ProviderFailureUnavailable    ProviderFailureKind = "unavailable"
	ProviderFailureRateLimited    ProviderFailureKind = "rate_limited"
	ProviderFailureServerError    ProviderFailureKind = "server_error"
	ProviderFailureAuthentication ProviderFailureKind = "authentication"
	ProviderFailureInvalidRequest ProviderFailureKind = "invalid_request"
)

// ProviderError is the application-level representation of an external
// provider failure.
//
// RetryAfter is a hint only. The router still enforces its own retry/deadline
// policy, so a provider cannot force the gateway to sleep beyond the caller's
// request budget.
type ProviderError struct {
	Kind       ProviderFailureKind
	RetryAfter time.Duration
	Cause      error
}

func (e *ProviderError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("provider failure (%s)", e.Kind)
	}
	return fmt.Sprintf("provider failure (%s): %v", e.Kind, e.Cause)
}

func (e *ProviderError) Unwrap() error {
	return e.Cause
}

// IsRetryableProviderError returns true if the error is a ProviderError
// with a kind that is considered retryable, or if the error is a context
// cancellation or deadline exceeded error.

func IsRetryableProviderError(err error) bool {
	if err == nil {
		return false
	}
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		switch providerErr.Kind {
		case ProviderFailureUnavailable,
			ProviderFailureRateLimited,
			ProviderFailureServerError:
			return true
		default:
			return false
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return false
}

//IsFallbackEligibleProviderError: Extends retryability rules to include AttemptTimeoutError. If an individual attempt to OpenAI times out, but the caller's overall connection is still healthy, the error is marked safe to hand over to a fallback provider (like Gemini).

func IsFallbackEligibleProviderError(err error) bool {
	if err == nil {
		return false
	}

	// AttemptTimeoutError is router-owned and therefore fallback-eligible
	// only when the parent context is still alive. The router performs the
	// parent check separately.
	var attemptTimeout *AttemptTimeoutError
	if errors.As(err, &attemptTimeout) {
		return true
	}

	return IsRetryableProviderError(err)
}



