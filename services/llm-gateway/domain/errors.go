//services/llm-gateway/domain/errors.go

package domain

import "fmt"

// Kind is the stable machine-readable error category consumed by application
// and interface layers. Do not put HTTP status codes here.
type Kind string

const (
	KindInvalidArgument     Kind = "invalid_argument"
	KindUnsupportedTask     Kind = "unsupported_task"
	KindContractMismatch    Kind = "contract_mismatch"
	KindDeadlineExceeded    Kind = "deadline_exceeded"
	KindProviderTimeout     Kind = "provider_timeout"
	KindProviderUnavailable Kind = "provider_unavailable"
	KindRequestCanceled     Kind = "request_canceled"
	KindValidationFailed    Kind = "validation_failed"
	KindInvalidEvent        Kind = "invalid_event"
	KindInternal            Kind = "internal"
)

// DomainError preserves the category while allowing the original error to be
// retained for logs and errors.Is/errors.As checks. The public message should
// be safe for interface clients; sensitive provider payloads must not be placed
// inside it.
type DomainError struct {
	Kind    Kind
	Message string
	Cause   error
}

func (e *DomainError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *DomainError) Unwrap() error { return e.Cause }

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
func NewProviderTimeoutError(msg string, cause error) *DomainError {
	return &DomainError{Kind: KindProviderTimeout, Message: msg, Cause: cause}
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
