// services/llm-gateway/infrastructure/provider/fake.go

package provider

import (
	"context"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

// Compile‑time assertion ensures FakeProvider always satisfies application.Provider.
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
	// It means: "the provider has started the operation and is now in the
	// blocking phase, waiting for either the delay to finish or the context
	// to be canceled."
	Started chan struct{}
}

// NewFakeProvider creates a fake that returns the given response immediately.
func NewFakeProvider(response string) *FakeProvider {
	return &FakeProvider{Response: response}
}

// Name returns the provider identifier used for observability metadata.
// In a real implementation, this would be "openai", "gemini", etc.
func (f *FakeProvider) Name() string {
	return "fake"
}

// Complete implements application.Provider.
//
// It respects the context before doing any work, then waits for the
// configured delay (if any) or for cancellation. If an error is set, it
// returns that error; otherwise it returns the fixed response.
func (f *FakeProvider) Complete(
	ctx context.Context,
	_ domain.Request,
) (string, error) {

	// Always respect the context before doing any work.
	// If the caller has already canceled or the deadline has passed,
	// return immediately. This mirrors real provider behavior.
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// If a delay is configured, wait for it or until cancellation.
	if f.Delay > 0 {
		// Signal that we have entered the blocking phase (if requested).
		if f.Started != nil {
			select {
			case f.Started <- struct{}{}:
				// Test has received the signal.
			default:
				// If the test isn't ready to receive, don't block the provider.
			}
		}

		select {
		case <-time.After(f.Delay):
			// Delay finished, continue to return response/error.
		case <-ctx.Done():
			// Context canceled or deadline exceeded; return the context error.
			return "", ctx.Err()
		}
	}

	// If an explicit error is set, return it.
	if f.Err != nil {
		return "", f.Err
	}

	// Otherwise, return the fixed response.
	return f.Response, nil
}
