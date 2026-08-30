//services/llm-gateway/infrastructure/provider/fake.go

package provider

import (
	"context"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

// Compile-time assertion.
//
// If FakeProvider ever stops implementing application.Provider,
// the project fails to compile.
var _ application.Provider = (*FakeProvider)(nil) //The compiler must prove that FakeProvider implements application.Provider.

// FakeProvider is a deterministic test double for an external AI provider.
//
// It intentionally returns raw provider output so that application tests
// exercise the same trust boundary a real provider adapter will use later.
type FakeProvider struct {
	Response string
	Err      error
	Delay    time.Duration // if > 0, wait before returning
}

// NewFakeProvider creates a fake provider that returns a fixed raw response.
func NewFakeProvider(response string) *FakeProvider {
	return &FakeProvider{
		Response: response,
	}
}

// Complete implements application.Provider.
//
// The fake is context-aware so future timeout/cancellation tests can use
// the same contract as a real provider.
// Complete implements application.Provider.
func (f *FakeProvider) Complete(
	ctx context.Context,
	_ domain.Request,
) (string, error) {
	// If a delay is configured, wait for it or until cancellation.
	if f.Delay > 0 {
		select {
		case <-time.After(f.Delay):
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
