// services/llm-gateway/infrastructure/provider/fake.go
package provider

import (
	"context"
	"errors"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
)

// FakeProvider is deterministic infrastructure used by Sprint 01 tests. It is
// deliberately shaped like a real provider adapter: it observes context
// cancellation, returns raw output, and reports adapter metadata.
type FakeProvider struct {
	ProviderName string
	ModelName    string
	Response     string
	Err          error
	Delay        time.Duration
	InputTokens  int64
	OutputTokens int64
	EstimatedUSD float64
	Calls        int
}

var _ application.Provider = (*FakeProvider)(nil)

func NewFakeProvider(name, response string) *FakeProvider {
	return &FakeProvider{ProviderName: name, Response: response}
}

func (f *FakeProvider) Name() string {
	if f.ProviderName == "" {
		return "fake"
	}
	return f.ProviderName
}

func (f *FakeProvider) Complete(ctx context.Context, _ application.ProviderRequest) (application.ProviderResponse, error) {
	f.Calls++
	if err := ctx.Err(); err != nil {
		return application.ProviderResponse{}, err
	}

	if f.Delay > 0 {
		timer := time.NewTimer(f.Delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return application.ProviderResponse{}, ctx.Err()
		}
	}

	if f.Err != nil {
		return application.ProviderResponse{}, f.Err
	}

	return application.ProviderResponse{
		RawOutput:    f.Response,
		Provider:     f.Name(),
		Model:        f.ModelName,
		InputTokens:  f.InputTokens,
		OutputTokens: f.OutputTokens,
		TotalTokens:  f.InputTokens + f.OutputTokens,
		EstimatedUSD: f.EstimatedUSD,
		Attempts:     1,
	}, nil
}

// ErrProviderUnavailable is useful for deterministic fallback tests. The
// router test wraps it in an application.ProviderError so the router can make a
// policy decision without importing infrastructure packages.
var ErrProviderUnavailable = errors.New("fake provider unavailable")
