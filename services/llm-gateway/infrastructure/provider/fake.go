// services/llm-gateway/infrastructure/provider/fake.go

package provider

import (
	"context"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

// Compile‑time assertion.
var _ application.Provider = (*FakeProvider)(nil)

// FakeProvider is a deterministic test double for an external AI provider.
//
// It can simulate:
//   - immediate success (Response)
//   - immediate failure (Err)
//   - delayed response (Delay + Response)
//   - delayed failure (Delay + Err)
//   - hanging until cancellation (large Delay + context cancel)
//
// All delays are cancellation‑aware: the provider stops waiting
// as soon as ctx.Done() is closed.
//
// The optional Started channel is used for test synchronization. When set,
// the provider sends a value on it immediately before entering the delay
// wait. This allows tests to deterministically know when the provider has
// started blocking, eliminating the need for time.Sleep.
type FakeProvider struct {
	Response string
	Err      error
	Delay    time.Duration

	// Started, if non‑nil, receives a signal just before the provider
	// begins waiting on the delay or on ctx.Done().
	Started chan struct{} // it means the provider has started the operation and is now in the blocking phase,blocking phase means the provider has started the operation and is now waiting for either the delay to finish or the context to be canceled.
}

// NewFakeProvider creates a fake that returns the given response immediately.
func NewFakeProvider(response string) *FakeProvider {
	return &FakeProvider{Response: response}
}

// Complete implements application.Provider.
func (f *FakeProvider) Complete(
	ctx context.Context,
	_ domain.Request,
) (string, error) {

	// Always respect the context before doing any work.
	// This mirrors real provider behavior: if the caller has already
	// canceled or the deadline has passed, return immediately.
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// If a delay is configured, we must wait for it or until cancellation.
	// Test can wait on <-fake.Started before calling cancel().

	// This proves the provider actually started the operation, not that it observed a pre‑canceled context.

	// The select with default prevents the provider from blocking if the test doesn’t read from Started.
	if f.Delay > 0 {
		// Signal that we have entered the blocking phase (if requested).
		if f.Started != nil {
			select {
			case f.Started <- struct{}{}: //blocking phase means the provider has started the operation
			default:
				// If the test isn't ready to receive, don't block the provider.
			}
		}

		select {
		case <-time.After(f.Delay): // this means the provider has completed the operation after the delay, so we can proceed
			// continue to return response/error
		case <-ctx.Done():
			// Return the context error so callers can classify it.
			return "", ctx.Err()
		}
	}

	// If an error is set, return it.
	if f.Err != nil {
		return "", f.Err
	}

	// Otherwise, return the fixed response.
	return f.Response, nil
}
