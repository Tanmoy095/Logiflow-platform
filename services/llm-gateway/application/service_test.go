package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/infrastructure/provider"
)

// validRequest returns the smallest valid request used by most tests.
//
// Keeping the minimum valid request in one place makes the tests easier
// to read and keeps changes to the request contract centralized.
func validRequest() domain.Request {
	return domain.Request{
		ShipmentID: "ship-123",
		Prompt:     "Analyze shipment risk",
	}
}

// assertNoTrustedResult verifies the most important failure-path invariant:
//
//	An application error must never be accompanied by a CompletionResult
//	that a caller could accidentally treat as trusted business data.
//
// We intentionally do not compare CompletionResult structs directly because
// the type contains []string, and slices are not comparable in Go.
//
// Instead, we explicitly verify that every field remains at its zero value.
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

// newServiceWithResponse constructs the application service with the
// deterministic FakeProvider.
//
// This is Dependency Injection in practice:
// the application receives its Provider from the outside instead of
// constructing a concrete provider itself.
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

	// A trusted result is allowed to exist here because the provider output
	// successfully crossed the parsing, schema, and domain validation
	// boundaries.
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
	// Deliberately broken provider output.
	//
	// This is the Monday "break" scenario:
	// valid provider call -> malformed raw data -> parser must reject it.
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

	// Monday intentionally does not require the final typed ParseError
	// taxonomy. That richer error model is introduced later.
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

	// Construct the fake directly because this test is intentionally
	// controlling the provider's operational failure behavior.
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

	// errors.Is preserves the underlying error identity even when
	// application layers wrap or classify it later.
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
			// The provider returns valid data, but the application should
			// reject the request before invoking the provider.
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
			name: "empty reasons",
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

			// These cases have valid JSON and valid field shapes.
			// Therefore, the failure should occur at the domain layer,
			// not the schema layer.
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

func TestServiceComplete_ShipmentIdentityMismatch(t *testing.T) {
	// The JSON is structurally valid and the domain values are otherwise
	// valid, but the result belongs to a different shipment.
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

	if !strings.Contains(err.Error(), "shipment_id mismatch") {
		t.Fatalf(
			"error = %q, want shipment_id mismatch",
			err,
		)
	}

	assertNoTrustedResult(t, result)
}
