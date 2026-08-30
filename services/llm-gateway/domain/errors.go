package domain

// services/llm-gateway/domain/errors.go

import (
	"fmt"
)

// Kind classifies a domain-level failure.
//
// This is the stable, machine-readable category that callers and
// resilience policies can use to decide what to do next.
// It is intentionally decoupled from any transport protocol.
type Kind string

const (
	// KindInvalidArgument indicates the request failed its own validation.
	KindInvalidArgument Kind = "invalid_argument"

	// KindProviderTimeout indicates the external AI provider did not
	// respond within the allowed deadline.
	KindProviderTimeout Kind = "provider_timeout"

	// KindProviderUnavailable indicates the provider could not be reached
	// or returned an operational error (e.g., 5xx).
	KindProviderUnavailable Kind = "provider_unavailable"

	// KindRequestCanceled indicates the caller explicitly stopped the
	// operation before it completed.
	KindRequestCanceled Kind = "request_canceled"

	// KindValidationFailed indicates the provider returned a well‑formed
	// response that violated business rules (e.g., confidence > 1).
	KindValidationFailed Kind = "validation_failed"

	// KindInternal indicates an unexpected error inside the gateway itself.
	KindInternal Kind = "internal"
)

// DomainError is the gateway's typed error object.
//
// It carries three pieces of information:
//   - Kind: the stable failure category (machine‑readable).
//   - Message: human‑readable context.
//   - Cause: the underlying error that triggered this failure (may be nil).
//
// This design allows callers to use errors.Is/errors.As to both:
//   - check for a specific sentinel error (via Is)
//   - extract the typed error and its Kind (via As)
//
// The struct is immutable after construction – no shared mutable state.
type DomainError struct {
	Kind    Kind
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *DomainError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

// Unwrap enables errors.Is / errors.As to traverse the cause chain.
func (e *DomainError) Unwrap() error {
	return e.Cause
}

// Is allows direct comparison with a sentinel error of the same Kind.
//
// Example:
//
//	var ErrTimeout = &DomainError{Kind: KindProviderTimeout}
//	errors.Is(err, ErrTimeout) // true if err's Kind matches

//Unwrap gives Go the keys to open the box and look inside.

// Is tells Go to stop comparing the whole box, and just compare the label (Kind) on the box instead.
func (e *DomainError) Is(target error) bool {
	t, ok := target.(*DomainError)
	if !ok {
		return false
	}
	return e.Kind == t.Kind
}

// Helper constructors make error creation concise and intent‑revealing.
func NewInvalidArgumentError(msg string) *DomainError {
	return &DomainError{Kind: KindInvalidArgument, Message: msg}
}

func NewProviderTimeoutError(msg string, cause error) *DomainError {
	return &DomainError{Kind: KindProviderTimeout, Message: msg, Cause: cause}
}

func NewProviderUnavailableError(msg string, cause error) *DomainError {
	return &DomainError{Kind: KindProviderUnavailable, Message: msg, Cause: cause}
}

func NewRequestCanceledError(msg string, cause error) *DomainError {
	return &DomainError{Kind: KindRequestCanceled, Message: msg, Cause: cause}
}

func NewValidationFailedError(msg string) *DomainError {
	return &DomainError{Kind: KindValidationFailed, Message: msg}
}

func NewInternalError(msg string, cause error) *DomainError {
	return &DomainError{Kind: KindInternal, Message: msg, Cause: cause}
}
