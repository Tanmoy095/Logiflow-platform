package application_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/infrastructure/provider"
)

// validRequest returns the smallest request that satisfies the application
// request contract.
//
// Keeping this helper centralized makes individual tests focus on the
// behavior being tested rather than repeating unrelated setup.
func validRequest() domain.Request {
	return domain.Request{
		ShipmentID: "ship-123",
		Prompt:     "Analyze shipment risk",
	}
}

// assertNoTrustedResult verifies a critical failure-path invariant:
//
//	When Complete returns an error, it must not return a partially populated
//	CompletionResult that a caller could accidentally treat as trusted.
//
// CompletionResult contains a []string, so the struct itself is not directly
// comparable with == or != in Go. We therefore check the semantic zero state
// of each field explicitly.
func assertNoTrustedResult(
	t *testing.T,
	result domain.CompletionResult,
) {
	t.Helper()

	if result.ShipmentID != "" {
		t.Fatalf(
			"error path returned trusted shipment_id %q",
			result.ShipmentID,
		)
	}

	if result.Risk != "" {
		t.Fatalf(
			"error path returned trusted risk %q",
			result.Risk,
		)
	}

	if result.Confidence != 0 {
		t.Fatalf(
			"error path returned trusted confidence %v",
			result.Confidence,
		)
	}

	if len(result.Reasons) != 0 {
		t.Fatalf(
			"error path returned trusted reasons %v",
			result.Reasons,
		)
	}
}

// newServiceWithResponse constructs the application using the deterministic
// fake provider.
//
// This is Dependency Injection in practice: the application receives a
// Provider implementation rather than constructing one itself.
func newServiceWithResponse(response string) *application.Service {
	fake := provider.NewFakeProvider(response)

	return application.NewService(fake)
}

func TestServiceComplete_ValidResponse(t *testing.T) {
	const response = `{
		"shipment_id": "ship-123",
		"risk": "high_risk",
		"confidence": 0.94,
		"reasons": [
			"Customs clearance delay detected"
		]
	}`

	service := newServiceWithResponse(response)

	result, err := service.Complete(
		context.Background(),
		validRequest(),
	)

	if err != nil {
		t.Fatalf(
			"Complete() returned unexpected error: %v",
			err,
		)
	}

	// A result is trusted only after syntax, schema, identity, and
	// domain validation have all succeeded.
	if result.ShipmentID != "ship-123" {
		t.Fatalf(
			"ShipmentID = %q, want %q",
			result.ShipmentID,
			"ship-123",
		)
	}

	if result.Risk != domain.RiskHighRisk {
		t.Fatalf(
			"Risk = %q, want %q",
			result.Risk,
			domain.RiskHighRisk,
		)
	}

	if result.Confidence != 0.94 {
		t.Fatalf(
			"Confidence = %v, want %v",
			result.Confidence,
			0.94,
		)
	}

	if len(result.Reasons) != 1 {
		t.Fatalf(
			"Reasons length = %d, want 1",
			len(result.Reasons),
		)
	}

	if result.Reasons[0] != "Customs clearance delay detected" {
		t.Fatalf(
			"Reasons[0] = %q, want %q",
			result.Reasons[0],
			"Customs clearance delay detected",
		)
	}
}

func TestServiceComplete_MalformedJSON(t *testing.T) {
	// Deliberately malformed provider output.
	//
	// This proves that raw provider output is never trusted directly.
	service := newServiceWithResponse(`{risk:`)

	result, err := service.Complete(
		context.Background(),
		validRequest(),
	)

	if err == nil {
		t.Fatal(
			"Complete() error = nil, want malformed JSON error",
		)
	}

	// Typed ParseError is intentionally deferred to Wednesday.
	if !strings.Contains(err.Error(), "parse provider response") {
		t.Fatalf(
			"error = %q, want parse provider response error",
			err,
		)
	}

	assertNoTrustedResult(t, result)
}

func TestServiceComplete_ProviderError(t *testing.T) {
	expectedErr := errors.New("provider unavailable")

	fake := &provider.FakeProvider{
		Err: expectedErr,
	}

	service := application.NewService(fake)

	result, err := service.Complete(
		context.Background(),
		validRequest(),
	)

	if err == nil {
		t.Fatal(
			"Complete() error = nil, want provider error",
		)
	}

	// Preserve provider error identity so later typed/wrapped errors can
	// still be classified with errors.Is/errors.As.
	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"Complete() error = %v, want provider error %v",
			err,
			expectedErr,
		)
	}

	assertNoTrustedResult(t, result)
}

func TestServiceComplete_RequestValidation(t *testing.T) {
	tests := []struct {
		name string
		req  domain.Request
	}{
		{
			name: "missing shipment id",
			req: domain.Request{
				Prompt: "Analyze shipment",
			},
		},
		{
			name: "missing prompt",
			req: domain.Request{
				ShipmentID: "ship-123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The provider response is valid, but request validation must
			// reject the call before provider execution.
			service := newServiceWithResponse(`{
				"shipment_id": "ship-123",
				"risk": "high_risk",
				"confidence": 0.95,
				"reasons": ["delay"]
			}`)

			result, err := service.Complete(
				context.Background(),
				tt.req,
			)

			if err == nil {
				t.Fatal(
					"Complete() error = nil, want request validation error",
				)
			}

			assertNoTrustedResult(t, result)
		})
	}
}

func TestServiceComplete_SchemaValidation(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{
			name: "missing shipment_id",
			response: `{
				"risk": "high_risk",
				"confidence": 0.95,
				"reasons": ["delay"]
			}`,
		},
		{
			name: "missing risk",
			response: `{
				"shipment_id": "ship-123",
				"confidence": 0.95,
				"reasons": ["delay"]
			}`,
		},
		{
			name: "missing confidence",
			response: `{
				"shipment_id": "ship-123",
				"risk": "high_risk",
				"reasons": ["delay"]
			}`,
		},
		{
			name: "missing reasons",
			response: `{
				"shipment_id": "ship-123",
				"risk": "high_risk",
				"confidence": 0.95
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newServiceWithResponse(tt.response)

			result, err := service.Complete(
				context.Background(),
				validRequest(),
			)

			if err == nil {
				t.Fatal(
					"Complete() error = nil, want schema validation error",
				)
			}

			if !strings.Contains(err.Error(), "schema validation") {
				t.Fatalf(
					"error = %q, want schema validation error",
					err,
				)
			}

			assertNoTrustedResult(t, result)
		})
	}
}

func TestServiceComplete_DomainValidation(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{
			name: "confidence above upper bound",
			response: `{
				"shipment_id": "ship-123",
				"risk": "high_risk",
				"confidence": 1.7,
				"reasons": ["invalid confidence"]
			}`,
		},
		{
			name: "confidence below lower bound",
			response: `{
				"shipment_id": "ship-123",
				"risk": "high_risk",
				"confidence": -0.1,
				"reasons": ["invalid confidence"]
			}`,
		},
		{
			name: "unsupported risk",
			response: `{
				"shipment_id": "ship-123",
				"risk": "banana",
				"confidence": 0.95,
				"reasons": ["unsupported classification"]
			}`,
		},
		{
			name: "high risk with empty reasons",
			response: `{
				"shipment_id": "ship-123",
				"risk": "high_risk",
				"confidence": 0.95,
				"reasons": []
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newServiceWithResponse(tt.response)

			result, err := service.Complete(
				context.Background(),
				validRequest(),
			)

			if err == nil {
				t.Fatal(
					"Complete() error = nil, want domain validation error",
				)
			}

			// All cases in this table are syntactically and structurally
			// valid. Their failure must therefore come from business rules.
			if strings.Contains(err.Error(), "schema validation") {
				t.Fatalf(
					"error = %q, want domain validation failure",
					err,
				)
			}

			assertNoTrustedResult(t, result)
		})
	}
}

func TestServiceComplete_NoRiskMayHaveEmptyReasons(t *testing.T) {
	// This test is important because the domain rule is NOT:
	//
	//	reasons must always be non-empty
	//
	// The documented rule is:
	//
	//	high_risk => at least one reason
	//
	// Therefore no_risk with an explicitly present empty reasons array
	// should be a valid result.
	service := newServiceWithResponse(`{
		"shipment_id": "ship-123",
		"risk": "no_risk",
		"confidence": 0.99,
		"reasons": []
	}`)

	result, err := service.Complete(
		context.Background(),
		validRequest(),
	)

	if err != nil {
		t.Fatalf(
			"Complete() returned unexpected error: %v",
			err,
		)
	}

	if result.Risk != domain.RiskNoRisk {
		t.Fatalf(
			"Risk = %q, want %q",
			result.Risk,
			domain.RiskNoRisk,
		)
	}

	if len(result.Reasons) != 0 {
		t.Fatalf(
			"Reasons = %v, want empty reasons",
			result.Reasons,
		)
	}
}

func TestCompletionResultValidate_RejectsNonFiniteConfidence(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
	}{
		{
			name:       "NaN",
			confidence: math.NaN(),
		},
		{
			name:       "positive infinity",
			confidence: math.Inf(1),
		},
		{
			name:       "negative infinity",
			confidence: math.Inf(-1),
		},
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
				t.Fatal(
					"Validate() error = nil, want non-finite confidence error",
				)
			}

			if !strings.Contains(
				err.Error(),
				"confidence must be a finite number",
			) {
				t.Fatalf(
					"error = %q, want finite-confidence validation error",
					err,
				)
			}
		})
	}
}

func TestServiceComplete_ShipmentIdentityMismatch(t *testing.T) {
	// The response is valid JSON, has the correct schema, and contains
	// valid domain values — but it belongs to another shipment.
	//
	// This is a cross-entity integrity failure.
	service := newServiceWithResponse(`{
		"shipment_id": "ship-999",
		"risk": "high_risk",
		"confidence": 0.95,
		"reasons": ["delay"]
	}`)

	result, err := service.Complete(
		context.Background(),
		validRequest(),
	)

	if err == nil {
		t.Fatal(
			"Complete() error = nil, want shipment identity mismatch",
		)
	}

	if !strings.Contains(
		err.Error(),
		"shipment_id mismatch",
	) {
		t.Fatalf(
			"error = %q, want shipment identity mismatch",
			err,
		)
	}

	assertNoTrustedResult(t, result)
}
