//services/llm-gateway/infrastructure/provider/fake.go

package provider

import (
	"context"

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
func (f *FakeProvider) Complete(
	ctx context.Context,
	_ domain.Request,
) (string, error) {

	select {
	case <-ctx.Done():
		return "", ctx.Err()

	default:
	}

	if f.Err != nil {
		return "", f.Err
	}

	return f.Response, nil
}
