// services/llm-gateway/application/service_test.go

package application_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/infrastructure/provider"
)

// validRequest returns the smallest request that satisfies the application
// request contract. Keeping this helper centralised makes individual tests
// focus on the behaviour being tested rather than repeating setup.
func validRequest() domain.Request {
	return domain.Request{
		ShipmentID:    "ship-123",
		Prompt:        "Analyze shipment risk",
		PromptVersion: "v1",
	}
}

// assertNoTrustedResult verifies a critical failure‑path invariant:
//
//	When Complete returns an error, it must not return a partially populated
//	CompletionResult that a caller could accidentally treat as trusted.
//
// CompletionResult contains a []string, so the struct itself is not directly
// comparable with == or != in Go. We therefore check the semantic zero state
// of each field explicitly.
func assertNoTrustedResult(t *testing.T, result domain.CompletionResult) {
	t.Helper()

	if result.ShipmentID != "" {
		t.Fatalf("error path returned trusted shipment_id %q", result.ShipmentID)
	}
	if result.Risk != "" {
		t.Fatalf("error path returned trusted risk %q", result.Risk)
	}
	if result.Confidence != 0 {
		t.Fatalf("error path returned trusted confidence %v", result.Confidence)
	}
	if len(result.Reasons) != 0 {
		t.Fatalf("error path returned trusted reasons %v", result.Reasons)
	}
}

// newServiceWithResponse constructs the application service with a fake
// provider that immediately returns the given raw JSON response.
//
// This is Dependency Injection in practice: the application receives a
// Provider implementation rather than constructing one itself.
func newServiceWithResponse(response string) *application.Service {
	fake := provider.NewFakeProvider(response)
	return application.NewService(fake)
}

// --- Monday / Tuesday tests ---

// TestServiceComplete_ValidResponse verifies that a well‑formed provider
// response passes all validation stages and yields a trusted result.
func TestServiceComplete_ValidResponse(t *testing.T) {
	const response = `{
		"shipment_id": "ship-123",
		"risk": "high_risk",
		"confidence": 0.94,
		"reasons": ["Customs clearance delay detected"]
	}`

	service := newServiceWithResponse(response)
	result, err := service.Complete(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Complete() returned unexpected error: %v", err)
	}

	// Verify that the trusted result contains the expected values.
	if result.ShipmentID != "ship-123" {
		t.Fatalf("ShipmentID = %q, want %q", result.ShipmentID, "ship-123")
	}
	if result.Risk != domain.RiskHighRisk {
		t.Fatalf("Risk = %q, want %q", result.Risk, domain.RiskHighRisk)
	}
	if result.Confidence != 0.94 {
		t.Fatalf("Confidence = %v, want 0.94", result.Confidence)
	}
	if len(result.Reasons) != 1 || result.Reasons[0] != "Customs clearance delay detected" {
		t.Fatalf("Reasons = %v, want exactly one expected reason", result.Reasons)
	}
}

// TestServiceComplete_MalformedJSON ensures that syntactically invalid
// provider output is rejected as a validation failure, not a provider error.
func TestServiceComplete_MalformedJSON(t *testing.T) {
	service := newServiceWithResponse(`{risk:`)
	result, err := service.Complete(context.Background(), validRequest())

	if err == nil {
		t.Fatal("Complete() error = nil, want malformed JSON error")
	}

	// The error must be a *domain.DomainError with KindValidationFailed.
	var domainErr *domain.DomainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("error = %v, want DomainError", err)
	}
	if domainErr.Kind != domain.KindValidationFailed {
		t.Fatalf("error kind = %q, want %q", domainErr.Kind, domain.KindValidationFailed)
	}

	assertNoTrustedResult(t, result)
}

// TestServiceComplete_ProviderError verifies that an operational failure
// (e.g., connection refused) is classified as ProviderUnavailable and the
// original error remains reachable via errors.Is.
func TestServiceComplete_ProviderError(t *testing.T) {
	expectedErr := errors.New("provider unavailable")
	fake := &provider.FakeProvider{Err: expectedErr}
	service := application.NewService(fake)

	result, err := service.Complete(context.Background(), validRequest())
	if err == nil {
		t.Fatal("Complete() error = nil, want provider error")
	}

	// The error must be a *domain.DomainError with kind ProviderUnavailable.
	var domainErr *domain.DomainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("error = %v, want DomainError", err)
	}
	if domainErr.Kind != domain.KindProviderUnavailable {
		t.Fatalf("error kind = %q, want %q", domainErr.Kind, domain.KindProviderUnavailable)
	}

	// The original cause must be preserved so callers can still match it.
	if !errors.Is(err, expectedErr) {
		t.Fatalf("error does not wrap original provider error: %v", err)
	}

	assertNoTrustedResult(t, result)
}

// TestServiceComplete_RequestValidation ensures that invalid requests
// (missing shipment ID or prompt) are rejected before the provider is called.
func TestServiceComplete_RequestValidation(t *testing.T) {
	tests := []struct {
		name string
		req  domain.Request
	}{
		{
			name: "missing shipment id",
			req:  domain.Request{Prompt: "Analyze shipment", PromptVersion: "v1"},
		},
		{
			name: "missing prompt",
			req:  domain.Request{ShipmentID: "ship-123", PromptVersion: "v1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The provider response is valid, but request validation must
			// reject the call before any provider interaction.
			service := newServiceWithResponse(`{"shipment_id":"ship-123","risk":"high_risk","confidence":0.95,"reasons":["delay"]}`)
			result, err := service.Complete(context.Background(), tt.req)
			if err == nil {
				t.Fatal("Complete() error = nil, want request validation error")
			}

			var domainErr *domain.DomainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("error = %v, want DomainError", err)
			}
			if domainErr.Kind != domain.KindInvalidArgument {
				t.Fatalf("error kind = %q, want %q", domainErr.Kind, domain.KindInvalidArgument)
			}

			assertNoTrustedResult(t, result)
		})
	}
}

// TestServiceComplete_SchemaValidation verifies that missing required fields
// in the provider response are classified as validation failures.
func TestServiceComplete_SchemaValidation(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{"missing shipment_id", `{"risk":"high_risk","confidence":0.95,"reasons":["delay"]}`},
		{"missing risk", `{"shipment_id":"ship-123","confidence":0.95,"reasons":["delay"]}`},
		{"missing confidence", `{"shipment_id":"ship-123","risk":"high_risk","reasons":["delay"]}`},
		{"missing reasons", `{"shipment_id":"ship-123","risk":"high_risk","confidence":0.95}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newServiceWithResponse(tt.response)
			result, err := service.Complete(context.Background(), validRequest())
			if err == nil {
				t.Fatal("Complete() error = nil, want schema validation error")
			}

			var domainErr *domain.DomainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("error = %v, want DomainError", err)
			}
			if domainErr.Kind != domain.KindValidationFailed {
				t.Fatalf("error kind = %q, want %q", domainErr.Kind, domain.KindValidationFailed)
			}

			assertNoTrustedResult(t, result)
		})
	}
}

// TestServiceComplete_DomainValidation ensures that syntactically valid
// responses that violate business rules (confidence bounds, risk enum,
// high‑risk reasons) are rejected as validation failures.
func TestServiceComplete_DomainValidation(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{"confidence above upper bound", `{"shipment_id":"ship-123","risk":"high_risk","confidence":1.7,"reasons":["invalid confidence"]}`},
		{"confidence below lower bound", `{"shipment_id":"ship-123","risk":"high_risk","confidence":-0.1,"reasons":["invalid confidence"]}`},
		{"unsupported risk", `{"shipment_id":"ship-123","risk":"banana","confidence":0.95,"reasons":["unsupported classification"]}`},
		{"high risk with empty reasons", `{"shipment_id":"ship-123","risk":"high_risk","confidence":0.95,"reasons":[]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newServiceWithResponse(tt.response)
			result, err := service.Complete(context.Background(), validRequest())
			if err == nil {
				t.Fatal("Complete() error = nil, want domain validation error")
			}

			var domainErr *domain.DomainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("error = %v, want DomainError", err)
			}
			if domainErr.Kind != domain.KindValidationFailed {
				t.Fatalf("error kind = %q, want %q", domainErr.Kind, domain.KindValidationFailed)
			}

			assertNoTrustedResult(t, result)
		})
	}
}

// TestServiceComplete_NoRiskMayHaveEmptyReasons confirms that the business
// rule “high_risk => at least one reason” does not incorrectly require
// reasons for no_risk results.
func TestServiceComplete_NoRiskMayHaveEmptyReasons(t *testing.T) {
	service := newServiceWithResponse(`{"shipment_id":"ship-123","risk":"no_risk","confidence":0.99,"reasons":[]}`)
	result, err := service.Complete(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Complete() returned unexpected error: %v", err)
	}

	if result.Risk != domain.RiskNoRisk {
		t.Fatalf("Risk = %q, want %q", result.Risk, domain.RiskNoRisk)
	}
	if len(result.Reasons) != 0 {
		t.Fatalf("Reasons = %v, want empty", result.Reasons)
	}
}

// TestCompletionResultValidate_RejectsNonFiniteConfidence directly tests the
// domain validation rule that confidence must be a finite number.
func TestCompletionResultValidate_RejectsNonFiniteConfidence(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
	}{
		{"NaN", math.NaN()},
		{"positive infinity", math.Inf(1)},
		{"negative infinity", math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := domain.CompletionResult{
				ShipmentID: "ship-123",
				Risk:       domain.RiskNoRisk,
				Confidence: tt.confidence,
				Reasons:    []string{},
			}
			err := result.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want non-finite confidence error")
			}
			if !strings.Contains(err.Error(), "confidence must be a finite number") {
				t.Fatalf("error = %q, want finite-confidence validation error", err)
			}
		})
	}
}

// TestServiceComplete_ShipmentIdentityMismatch ensures that a provider
// response for a different shipment is rejected even if everything else
// is valid.
func TestServiceComplete_ShipmentIdentityMismatch(t *testing.T) {
	service := newServiceWithResponse(`{"shipment_id":"ship-999","risk":"high_risk","confidence":0.95,"reasons":["delay"]}`)
	result, err := service.Complete(context.Background(), validRequest())
	if err == nil {
		t.Fatal("Complete() error = nil, want shipment identity mismatch")
	}

	var domainErr *domain.DomainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("error = %v, want DomainError", err)
	}
	if domainErr.Kind != domain.KindValidationFailed {
		t.Fatalf("error kind = %q, want %q", domainErr.Kind, domain.KindValidationFailed)
	}

	assertNoTrustedResult(t, result)
}

// --- ADDED tests: context & typed errors ---

// TestServiceComplete_ContextTimeout verifies that when the caller's context
// deadline expires, the provider cancels the operation and the service
// returns a typed ProviderTimeout error without a trusted result.
//
// The fake provider is configured to wait longer than the deadline, so the
// test deterministically triggers a deadline exceeded scenario.
func TestServiceComplete_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	fake := &provider.FakeProvider{
		Response: validFixtureProviderResponse, // defined in fixture_test.go
		Delay:    200 * time.Millisecond,       // exceeds deadline
	}
	service := application.NewService(fake)

	result, err := service.Complete(ctx, validRequest())
	if err == nil {
		t.Fatal("Complete() error = nil, want timeout error")
	}

	// Error must be a *domain.DomainError with kind ProviderTimeout.
	var domainErr *domain.DomainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("error = %v, want DomainError", err)
	}
	if domainErr.Kind != domain.KindProviderTimeout {
		t.Fatalf("error kind = %q, want %q", domainErr.Kind, domain.KindProviderTimeout)
	}

	// The underlying cause should be context.DeadlineExceeded, so callers
	// can still use errors.Is with the standard context error.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error does not wrap context.DeadlineExceeded: %v", err)
	}

	assertNoTrustedResult(t, result)
}

// TestServiceComplete_ExplicitCancellation verifies that when the caller
// cancels the context while the provider is in‑flight, the service returns
// a typed RequestCanceled error and no trusted result.
//
// This test synchronizes with the provider using the Started channel,
// ensuring the provider is actually blocked before cancellation occurs.
func TestServiceComplete_ExplicitCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan struct{})
	fake := &provider.FakeProvider{
		Response: validFixtureProviderResponse,
		Delay:    time.Second, // long enough to be canceled before completion
		Started:  started,
	}
	service := application.NewService(fake)

	type result struct {
		completion domain.CompletionResult
		err        error
	}
	resultCh := make(chan result, 1)

	go func() {
		r, err := service.Complete(ctx, validRequest())
		resultCh <- result{r, err}
	}()

	// Wait until the provider signals it has started waiting.
	select {
	case <-started:
		// Provider is now blocked; cancel it.
		cancel()
	case <-time.After(200 * time.Millisecond):
		t.Fatal("provider did not start within expected time")
	}

	res := <-resultCh

	if res.err == nil {
		t.Fatal("Complete() error = nil, want cancellation error")
	}

	var domainErr *domain.DomainError
	if !errors.As(res.err, &domainErr) {
		t.Fatalf("error = %v, want DomainError", res.err)
	}
	if domainErr.Kind != domain.KindRequestCanceled {
		t.Fatalf("error kind = %q, want %q", domainErr.Kind, domain.KindRequestCanceled)
	}

	if !errors.Is(res.err, context.Canceled) {
		t.Fatalf("error does not wrap context.Canceled: %v", res.err)
	}

	assertNoTrustedResult(t, res.completion)
}

// TestServiceComplete_ContextAlreadyCanceled verifies that if the context
// is canceled before the provider is called, the service returns a typed
// RequestCanceled error and no trusted result.
func TestServiceComplete_ContextAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	fake := provider.NewFakeProvider(validFixtureProviderResponse)
	service := application.NewService(fake)

	result, err := service.Complete(ctx, validRequest())
	if err == nil {
		t.Fatal("Complete() error = nil, want cancellation error")
	}

	var domainErr *domain.DomainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("error = %v, want DomainError", err)
	}
	if domainErr.Kind != domain.KindRequestCanceled {
		t.Fatalf("error kind = %q, want %q", domainErr.Kind, domain.KindRequestCanceled)
	}

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error does not wrap context.Canceled: %v", err)
	}

	assertNoTrustedResult(t, result)
}

// TestServiceComplete_ProviderErrorWrapped verifies that an operational
// provider error is classified as ProviderUnavailable and that the original
// error remains reachable via errors.Is. This is important because future
// resilience policies (retry, fallback) will inspect these typed errors.
func TestServiceComplete_ProviderErrorWrapped(t *testing.T) {
	baseErr := errors.New("connection refused")
	fake := &provider.FakeProvider{Err: baseErr}
	service := application.NewService(fake)

	result, err := service.Complete(context.Background(), validRequest())
	if err == nil {
		t.Fatal("Complete() error = nil, want provider error")
	}

	var domainErr *domain.DomainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("error = %v, want DomainError", err)
	}
	if domainErr.Kind != domain.KindProviderUnavailable {
		t.Fatalf("error kind = %q, want %q", domainErr.Kind, domain.KindProviderUnavailable)
	}

	if !errors.Is(err, baseErr) {
		t.Fatalf("error does not wrap original provider error: %v", err)
	}

	assertNoTrustedResult(t, result)
}
