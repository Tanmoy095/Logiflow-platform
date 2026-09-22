// services/llm-gateway/infrastructure/provider/fake.go
package provider

import (
	"context"
	"sync"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
)

// Compile-time assertion: FakeProvider must satisfy application.Provider.
// If someone changes the Provider interface and forgets to update this
// fixture, the build breaks immediately — before any test runs.
var _ application.Provider = (*FakeProvider)(nil)

// FakeProvider is a deterministic, thread-safe infrastructure adapter used
// exclusively by unit tests. It stands in for a real vendor SDK (OpenAI,
// Anthropic, etc.) without requiring credentials or network access.
//
// The fixture is shaped to mimic real providers in these ways:
//
//  1. It observes context cancellation and deadlines (like HTTP clients).
//  2. It can simulate latency via Delay.
//  3. It can return typed ProviderErrors so policies can classify them.
//  4. It reports usage metadata (tokens, cost) on both success AND failure.
//  5. It counts calls so tests can assert "this was / wasn't invoked".
//
// The mutex ensures concurrent tests (or parallel goroutines in the router)
// never race on the counters or fields.
type FakeProvider struct {
	// mu guards every field below. All reads/writes go through it.
	mu sync.Mutex

	ProviderName     string        // e.g. "openai"
	ModelName        string        // e.g. "gpt-4o-mini"
	Response         string        // raw output on success
	Err              error         // error to return (nil → success)
	Delay            time.Duration // simulated latency per call
	InputTokens      int64         // usage metadata
	OutputTokens     int64
	EstimatedCostUSD float64 // renamed from EstimatedUSD for clarity
	Calls            int     // how many times Complete() was invoked
}

// NewFakeProvider constructs a fixture with the given identity and canned
// response. Tests typically create one per provider scenario, e.g.:
//
//	primary  := provider.NewFakeProvider("openai", "gpt-4o-mini", `{"ok":true}`)
//	fallback := provider.NewFakeProvider("anthropic", "claude-3-haiku", `{"ok":true}`)
//
// Other behavior (Err, Delay, tokens, cost) is set via exported fields
// after construction, matching the "configure then use" style of the
// router tests.
func NewFakeProvider(name, model, response string) *FakeProvider {
	return &FakeProvider{
		ProviderName: name,
		ModelName:    model,
		Response:     response,
	}
}

// Name returns the provider identity, defaulting to "fake" when unset.
// It locks because tests may read Name() concurrently with Complete().
func (f *FakeProvider) Name() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.ProviderName == "" {
		return "fake"
	}
	return f.ProviderName
}

// Complete simulates one vendor call.
//
// The execution order is deliberate:
//
//  1. Snapshot configuration under the mutex (fast).
//  2. Release the mutex BEFORE doing any blocking work.
//  3. Check context (pre-flight cancellation).
//  4. Honor Delay, but abort early if the context dies.
//  5. Return either a success response or a failure response — both
//     carrying provider identity and usage metadata.
//
// Releasing the mutex before the timer is crucial: otherwise concurrent
// calls would serialize and tests like "parallel cancellation" would
// deadlock or run 10× slower than intended.
func (f *FakeProvider) Complete(ctx context.Context, _ application.ProviderRequest) (application.ProviderResponse, error) {
	// -------- Step 1: Snapshot mutable state under the lock --------
	f.mu.Lock()
	f.Calls++
	delay := f.Delay
	err := f.Err
	response := f.Response
	providerName := f.ProviderName
	modelName := f.ModelName
	inputTokens := f.InputTokens
	outputTokens := f.OutputTokens
	estimatedCost := f.EstimatedCostUSD
	f.mu.Unlock()
	// -------- End of critical section; no lock held below this line --------

	// -------- Step 2: Pre-flight context check --------
	// Mirrors what a real HTTP adapter does: if the caller already canceled
	// before we hit the network, don't even try.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return application.ProviderResponse{
			Provider: providerName,
			Model:    modelName,
		}, ctxErr
	}

	// -------- Step 3: Simulated latency, cancellable --------
	// A real HTTP client would block here until the response arrives OR
	// the context cancels. We emulate that with a timer + select.
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop() // avoid leaking the timer channel

		select {
		case <-timer.C:
			// Delay elapsed normally.
		case <-ctx.Done():
			// Caller canceled (or per-attempt timeout fired) mid-flight.
			// Return whatever identity we know so logs still attribute it.
			return application.ProviderResponse{
				Provider: providerName,
				Model:    modelName,
			}, ctx.Err()
		}
	}

	// -------- Step 4a: Failure path --------
	// Even on failure we return a fully-populated response so callers can
	// observe usage metadata (important for cost accounting on partial
	// failures — e.g. an upstream 503 that still consumed tokens).
	if err != nil {
		return application.ProviderResponse{
			Provider:     providerName,
			Model:        modelName,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  inputTokens + outputTokens,
			EstimatedUSD: estimatedCost,
		}, err
	}

	// -------- Step 4b: Success path --------
	// Note: Attempts is intentionally NOT set here. The router owns that
	// field and aggregates across retries and fallbacks. Providers only
	// report what they themselves know: output, identity, and usage.
	return application.ProviderResponse{
		RawOutput:    response,
		Provider:     providerName,
		Model:        modelName,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  inputTokens + outputTokens,
		EstimatedUSD: estimatedCost,
	}, nil
}

// -----------------------------------------------------------------------------
// Sentinels
// -----------------------------------------------------------------------------
// These are typed ProviderErrors so the router's reliability policies
// (Retryable, FallbackAllowed) can classify them by Kind without importing
// any HTTP- or SDK-specific knowledge.
//
// Real adapters should construct their own ProviderError values with the
// SAME Kind values (defined in the application package). That keeps the
// policy layer completely decoupled from infrastructure.
//
// Example usage in a test:
//
//	p := NewFakeProvider("openai", "gpt-4o-mini", "")
//	p.Err = provider.ErrRateLimited
//
// The router will then:
//  1. See Kind == ProviderFailureRateLimited.
//  2. Mark it as retryable.
//  3. Retry against the same provider (up to MaxAttemptsPerProvider).
//  4. Fall back only if retries are exhausted AND fallback allows it.
var (
	// Transient — safe to retry.
	ErrUnavailable = &application.ProviderError{Kind: application.ProviderFailureUnavailable}
	ErrRateLimited = &application.ProviderError{Kind: application.ProviderFailureRateLimited}
	ErrServerError = &application.ProviderError{Kind: application.ProviderFailureServerError}

	// Terminal — do NOT retry, do NOT fall back.
	// (Auth failure and malformed request won't be fixed by trying again.)
	ErrAuthentication = &application.ProviderError{Kind: application.ProviderFailureAuthentication}
	ErrInvalidRequest = &application.ProviderError{Kind: application.ProviderFailureInvalidRequest}
)
