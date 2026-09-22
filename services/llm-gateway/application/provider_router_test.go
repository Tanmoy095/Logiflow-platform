package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/infrastructure/provider"
)

// newRouter builds a ProviderRouter with test-friendly settings:
//   - 2 attempts per provider  (small, fast, still exercises retry)
//   - 20ms per-attempt timeout (short enough to trigger with slow fakes)
//   - zero backoff             (tests must not actually sleep)
//   - fallback enabled         (so we can observe failover)
//
// Passing 0 for backoff is critical: it makes every retry instant while
// still exercising the retry code path in the router.
func newRouter(t *testing.T, providers ...application.Provider) *application.ProviderRouter {
	t.Helper()

	router, err := application.NewProviderRouter(
		providers,
		application.RetryPolicy{
			MaxAttemptsPerProvider: 2,
			AttemptTimeout:         20 * time.Millisecond,
			// Zero backoff: keeps tests deterministic and fast.
			Backoff: func(int) time.Duration { return 0 },
		},
		application.FallbackPolicy{
			Enabled:      true,
			MaxProviders: len(providers),
		},
		application.DefaultSleeper,
	)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	return router
}

// -----------------------------------------------------------------------------
// Test 1: Happy path — the primary provider succeeds on the first try.
//
// Expected:
//   - No retry is issued.
//   - No fallback provider is contacted.
//   - Attempts reported = 1.
//
// -----------------------------------------------------------------------------
func TestProviderRouterPrimarySuccessDoesNotCallFallback(t *testing.T) {
	primary := provider.NewFakeProvider("primary", "model-a", `{"ok":true}`)
	fallback := provider.NewFakeProvider("fallback", "model-b", `{"ok":true}`)

	router := newRouter(t, primary, fallback)

	response, err := router.Complete(context.Background(), application.ProviderRequest{})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	if response.Provider != "primary" {
		t.Fatalf("provider = %q, want primary", response.Provider)
	}
	if response.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", response.Attempts)
	}
	if primary.Calls != 1 || fallback.Calls != 0 {
		t.Fatalf("calls: primary=%d fallback=%d", primary.Calls, fallback.Calls)
	}
}

// -----------------------------------------------------------------------------
// Test 2: Retryable failure → retry the SAME provider → succeed.
//
// The flakyProvider is scripted:
//
//	call #1 → ProviderFailureUnavailable  (retryable)
//	call #2 → success response
//
// Expected:
//   - Router retries the same provider.
//   - Total attempts = 2.
//   - Final response comes from that provider.
//
// -----------------------------------------------------------------------------
func TestProviderRouterRetriesRetryableFailureThenSucceeds(t *testing.T) {
	primary := provider.NewFakeProvider("primary", "model-a", `{"ok":true}`)

	// Scripted fake: behavior changes across calls.
	flaky := &flakyProvider{
		name: "primary",
		results: []flakyResult{
			{err: &application.ProviderError{Kind: application.ProviderFailureUnavailable}},
			{response: application.ProviderResponse{
				RawOutput: `{"ok":true}`,
				Provider:  "primary",
				Model:     "model-a",
			}},
		},
	}

	_ = primary // intentionally kept to document the "single-provider" setup
	router := newRouter(t, flaky)

	response, err := router.Complete(context.Background(), application.ProviderRequest{})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if response.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", response.Attempts)
	}
	if flaky.calls != 2 {
		t.Fatalf("calls = %d, want 2", flaky.calls)
	}
}

// -----------------------------------------------------------------------------
// Test 3: Per-attempt timeout → retry → exhaust budget → fall back.
//
// Setup:
//
//	slow provider sleeps 100ms per call.
//	Router's AttemptTimeout is 20ms → every slow attempt fails with a timeout.
//	MaxAttemptsPerProvider = 2 → 2 attempts against slow, then fall back.
//
// Expected:
//   - slow.Calls = 2
//   - fast.Calls = 1
//   - Total Attempts = 3
//   - Response.Provider = "fast"
//
// -----------------------------------------------------------------------------
func TestProviderRouterTimeoutRetriesThenFallsBack(t *testing.T) {
	slow := provider.NewFakeProvider("slow", "model-a", `{"ok":true}`)
	slow.Delay = 100 * time.Millisecond // > 20ms AttemptTimeout

	fast := provider.NewFakeProvider("fast", "model-b", `{"ok":true}`)

	router := newRouter(t, slow, fast)

	response, err := router.Complete(context.Background(), application.ProviderRequest{})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	if response.Provider != "fast" {
		t.Fatalf("provider = %q, want fast", response.Provider)
	}
	if response.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", response.Attempts)
	}
	if slow.Calls != 2 || fast.Calls != 1 {
		t.Fatalf("calls: slow=%d fast=%d", slow.Calls, fast.Calls)
	}
}

// -----------------------------------------------------------------------------
// Test 4: Authentication errors are terminal — neither retried nor fallbacked.
//
// Rationale:
//
//	A 401 means the credentials are wrong. Retrying won't help, and switching
//	to another vendor would only waste a call (their auth is independent).
//
// Expected:
//   - primary.Calls = 1  (no retry)
//   - fallback.Calls = 0 (no fallback)
//   - err != nil
//
// -----------------------------------------------------------------------------
func TestProviderRouterAuthenticationDoesNotRetryOrFallback(t *testing.T) {
	primary := provider.NewFakeProvider("primary", "model-a", "")
	primary.Err = provider.ErrAuthentication

	fallback := provider.NewFakeProvider("fallback", "model-b", `{"ok":true}`)

	router := newRouter(t, primary, fallback)

	_, err := router.Complete(context.Background(), application.ProviderRequest{})
	if err == nil {
		t.Fatal("expected error")
	}

	if primary.Calls != 1 {
		t.Fatalf("primary calls = %d, want 1", primary.Calls)
	}
	if fallback.Calls != 0 {
		t.Fatalf("fallback calls = %d, want 0", fallback.Calls)
	}
}

// -----------------------------------------------------------------------------
// Test 5: Caller's global deadline wins over fallback logic.
//
// Setup:
//
//	Caller gives 5ms total.
//	slow provider needs 100ms (would take 20ms per attempt, but caller dies first).
//	Fallback is configured but must NOT be tried.
//
// Expected:
//   - err == context.DeadlineExceeded
//   - fallback.Calls = 0
//
// -----------------------------------------------------------------------------
func TestProviderRouterCallerDeadlinePreventsFallback(t *testing.T) {
	slow := provider.NewFakeProvider("slow", "model-a", "")
	slow.Delay = 100 * time.Millisecond

	fallback := provider.NewFakeProvider("fallback", "model-b", `{"ok":true}`)

	router := newRouter(t, slow, fallback)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, err := router.Complete(ctx, application.ProviderRequest{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if fallback.Calls != 0 {
		t.Fatalf("fallback calls = %d, want 0", fallback.Calls)
	}
}

// -----------------------------------------------------------------------------
// Test 6: Pre-canceled context → no provider is contacted at all.
//
// The router's pre-flight check must reject the call before dispatching
// any HTTP request, avoiding wasted vendor calls.
//
// Expected:
//   - err == context.Canceled
//   - primary.Calls = 0
//   - fallback.Calls = 0
//
// -----------------------------------------------------------------------------
func TestProviderRouterCancellationDoesNotStartAnyProvider(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel BEFORE calling Complete

	primary := provider.NewFakeProvider("primary", "model-a", `{"ok":true}`)
	fallback := provider.NewFakeProvider("fallback", "model-b", `{"ok":true}`)

	router := newRouter(t, primary, fallback)

	_, err := router.Complete(ctx, application.ProviderRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if primary.Calls != 0 || fallback.Calls != 0 {
		t.Fatalf("calls: primary=%d fallback=%d", primary.Calls, fallback.Calls)
	}
}

// -----------------------------------------------------------------------------
// flakyProvider — a scripted provider used to test retry behavior.
//
// Each call returns the next entry from `results`. If more calls arrive
// than there are scripted results, it fails loudly with "unexpected extra
// call" so the test can catch an over-retrying router.
// -----------------------------------------------------------------------------
type flakyResult struct {
	response application.ProviderResponse
	err      error
}

type flakyProvider struct {
	name    string
	results []flakyResult
	calls   int
}

func (f *flakyProvider) Name() string { return f.name }

func (f *flakyProvider) Complete(_ context.Context, _ application.ProviderRequest) (application.ProviderResponse, error) {
	// Guard: router called us more times than the script defines.
	if f.calls >= len(f.results) {
		return application.ProviderResponse{Provider: f.name, Model: "model-a"},
			errors.New("unexpected extra call")
	}

	result := f.results[f.calls]
	f.calls++
	if result.err != nil {
		return application.ProviderResponse{Provider: f.name, Model: "model-a"}, result.err
	}
	return result.response, nil
}
