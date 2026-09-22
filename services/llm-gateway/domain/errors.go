package domain

import "fmt"

// Kind is the stable failure taxonomy shared by the application layer and
// transport adapters.
//
// Keeping these values transport-neutral allows a future gRPC adapter and a
// future HTTP/MCP adapter to map the same failure into their own protocol
// without changing anything in the domain or application layers.
//
// Rules:
//   - Never put HTTP status codes here — those are transport concerns.
//   - Never rename an existing Kind without a migration plan; alerts,
//     dashboards, and SLOs key off these strings.
type Kind string

const (
	// ---- Preflight / input contract -------------------------------------

	KindInvalidArgument  Kind = "invalid_argument"
	KindUnsupportedTask  Kind = "unsupported_task"
	KindContractMismatch Kind = "contract_mismatch"

	// ---- Caller lifecycle -----------------------------------------------

	// KindRequestCanceled: the caller canceled before completion. Not
	// retryable, not billable.
	KindRequestCanceled Kind = "request_canceled"

	// KindDeadlineExceeded: the caller's deadline (or the command's
	// explicit deadline) expired. Distinct from a vendor-side timeout —
	// this one is defined by the caller's budget.
	KindDeadlineExceeded Kind = "deadline_exceeded"

	// ---- Provider / execution -------------------------------------------

	// KindProviderUnavailable: all provider attempts (including any retries
	// and fallbacks performed by a ProviderRouter) failed. The specific
	// underlying provider failure kind is intentionally NOT surfaced at
	// this layer — application code should not depend on vendor taxonomy.
	KindProviderUnavailable Kind = "provider_unavailable"

	// ---- Output validation ----------------------------------------------

	KindValidationFailed Kind = "validation_failed"

	// ---- Internal safety nets -------------------------------------------

	KindInvalidEvent Kind = "invalid_event"
	KindInternal     Kind = "internal"
)

// DomainError carries a stable machine-readable Kind while retaining an
// underlying cause for errors.Is/errors.As.
//
// The Message field must remain safe to log and safe to show to interface
// clients. Never use this error as a dumping ground for raw provider
// payloads, credentials, or evidence content — those belong in the Cause
// and only for internal logging.
type DomainError struct {
	Kind    Kind
	Message string
	Cause   error
}

// Error renders a stable string form: "<kind>: <message>[: <cause>]".
//
// Including the cause inline is deliberate: most log pipelines benefit from
// seeing it without an additional Unwrap call. Redaction belongs at the
// logger level, not here.
func (e *DomainError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("%s: %s", e.Kind, e.Message)
	}
	return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Cause)
}

// Unwrap exposes the cause to errors.Is / errors.As.
func (e *DomainError) Unwrap() error { return e.Cause }

// Is allows category-based matching:
//
//	if errors.Is(err, &domain.DomainError{Kind: domain.KindValidationFailed}) {
//	    ...
//	}
//
// This avoids declaring a package-level sentinel for every Kind while still
// giving callers a stable way to branch on the failure class.
func (e *DomainError) Is(target error) bool {
	t, ok := target.(*DomainError)
	return ok && e.Kind == t.Kind
}

// -----------------------------------------------------------------------------
// Constructors
// -----------------------------------------------------------------------------
//
// Each constructor pins the Kind and keeps call sites terse. Prefer these
// over constructing DomainError literals directly so that adding fields
// later does not require touching every call site.

func NewInvalidArgumentError(msg string) *DomainError {
	return &DomainError{Kind: KindInvalidArgument, Message: msg}
}

func NewUnsupportedTaskError(msg string) *DomainError {
	return &DomainError{Kind: KindUnsupportedTask, Message: msg}
}

func NewContractMismatchError(msg string) *DomainError {
	return &DomainError{Kind: KindContractMismatch, Message: msg}
}

func NewDeadlineExceededError(msg string, cause error) *DomainError {
	return &DomainError{Kind: KindDeadlineExceeded, Message: msg, Cause: cause}
}

func NewProviderUnavailableError(msg string, cause error) *DomainError {
	return &DomainError{Kind: KindProviderUnavailable, Message: msg, Cause: cause}
}

func NewRequestCanceledError(msg string, cause error) *DomainError {
	return &DomainError{Kind: KindRequestCanceled, Message: msg, Cause: cause}
}

func NewValidationFailedError(msg string, cause error) *DomainError {
	return &DomainError{Kind: KindValidationFailed, Message: msg, Cause: cause}
}
