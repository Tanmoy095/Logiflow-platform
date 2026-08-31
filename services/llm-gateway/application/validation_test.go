// services/llm-gateway/application/validation_test.go

package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/infrastructure/provider"
)

// TestValidationMatrix is the Thursday correctness matrix.
//
// It proves that the validation pipeline correctly distinguishes
// syntax, schema, and domain failures. Every case expects the
// final error to be a *domain.DomainError with KindValidationFailed
// and no trusted result.
func TestValidationMatrix(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		// ── Syntax failures ────────────────────────────────────────────
		{
			name:     "malformed JSON",
			response: `{risk:`,
		},
		// ── Schema failures ────────────────────────────────────────────
		{
			name:     "missing shipment_id",
			response: `{"risk":"high_risk","confidence":0.95,"reasons":["delay"]}`,
		},
		{
			name:     "missing risk",
			response: `{"shipment_id":"ship-123","confidence":0.95,"reasons":["delay"]}`,
		},
		{
			name:     "missing confidence",
			response: `{"shipment_id":"ship-123","risk":"high_risk","reasons":["delay"]}`,
		},
		{
			name:     "missing reasons",
			response: `{"shipment_id":"ship-123","risk":"high_risk","confidence":0.95}`,
		},
		{
			name:     "wrong confidence type",
			response: `{"shipment_id":"ship-123","risk":"high_risk","confidence":"very-high","reasons":["delay"]}`,
		},
		// ── Domain failures ────────────────────────────────────────────
		{
			name:     "confidence above upper bound",
			response: `{"shipment_id":"ship-123","risk":"high_risk","confidence":1.7,"reasons":["delay"]}`,
		},
		{
			name:     "confidence below lower bound",
			response: `{"shipment_id":"ship-123","risk":"high_risk","confidence":-0.1,"reasons":["delay"]}`,
		},
		{
			name:     "unsupported risk",
			response: `{"shipment_id":"ship-123","risk":"banana","confidence":0.95,"reasons":["delay"]}`,
		},
		{
			name:     "high risk with empty reasons",
			response: `{"shipment_id":"ship-123","risk":"high_risk","confidence":0.95,"reasons":[]}`,
		},
		{
			name:     "shipment identity mismatch",
			response: `{"shipment_id":"ship-999","risk":"high_risk","confidence":0.95,"reasons":["delay"]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := application.NewService(
				provider.NewFakeProvider(tt.response),
			)

			result, err := service.Complete(
				context.Background(),
				validRequest(),
			)

			if err == nil {
				t.Fatal("Complete() error = nil, want validation error")
			}

			var domainErr *domain.DomainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("error = %v, want DomainError", err)
			}

			if domainErr.Kind != domain.KindValidationFailed {
				t.Fatalf(
					"error kind = %q, want %q",
					domainErr.Kind,
					domain.KindValidationFailed,
				)
			}

			assertNoTrustedResult(t, result)
		})
	}
}

// TestValidationSuccess_LowRiskEmptyReasons ensures that a valid low‑risk
// response with empty reasons passes all stages and becomes trusted.
func TestValidationSuccess_LowRiskEmptyReasons(t *testing.T) {
	response := `{
		"shipment_id": "ship-123",
		"risk": "no_risk",
		"confidence": 0.99,
		"reasons": []
	}`

	service := application.NewService(
		provider.NewFakeProvider(response),
	)

	result, err := service.Complete(
		context.Background(),
		validRequest(),
	)
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
