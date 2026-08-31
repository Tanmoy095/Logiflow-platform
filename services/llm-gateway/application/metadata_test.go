// services/llm-gateway/application/metadata_test.go

package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/infrastructure/provider"
)

// TestServiceComplete_MetadataSuccess verifies that a successful completion
// populates all metadata fields correctly and does not leak any error kind.
func TestServiceComplete_MetadataSuccess(t *testing.T) {
	response := `{
		"shipment_id": "ship-123",
		"risk": "high_risk",
		"confidence": 0.94,
		"reasons": ["Customs clearance delay detected"]
	}`

	service := application.NewService(provider.NewFakeProvider(response))

	result, meta, err := service.Complete(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Complete() returned unexpected error: %v", err)
	}

	if meta.Status != "success" {
		t.Fatalf("Status = %q, want %q", meta.Status, "success")
	}
	if meta.Provider != "fake" {
		t.Fatalf("Provider = %q, want %q", meta.Provider, "fake")
	}
	if meta.PromptVersion != "v1" {
		t.Fatalf("PromptVersion = %q, want %q", meta.PromptVersion, "v1")
	}
	if meta.RequestID == "" {
		t.Fatal("RequestID is empty")
	}
	if meta.ProviderLatencyMs < 0 {
		t.Fatalf("ProviderLatencyMs = %v, want >= 0", meta.ProviderLatencyMs)
	}
	if meta.ValidationLatencyMs < 0 {
		t.Fatalf("ValidationLatencyMs = %v, want >= 0", meta.ValidationLatencyMs)
	}
	if meta.TotalGatewayLatencyMs < 0 {
		t.Fatalf("TotalGatewayLatencyMs = %v, want >= 0", meta.TotalGatewayLatencyMs)
	}
	if meta.ErrorKind != "" {
		t.Fatalf("ErrorKind = %q, want empty", meta.ErrorKind)
	}

	// Domain result still correct
	if result.Risk != domain.RiskHighRisk {
		t.Fatalf("Risk = %q, want %q", result.Risk, domain.RiskHighRisk)
	}
}

// TestServiceComplete_MetadataProviderError verifies that an operational
// provider error sets the metadata status and error kind accordingly.
func TestServiceComplete_MetadataProviderError(t *testing.T) {
	baseErr := errors.New("connection refused")
	fake := &provider.FakeProvider{Err: baseErr}
	service := application.NewService(fake)

	_, meta, err := service.Complete(context.Background(), validRequest())
	if err == nil {
		t.Fatal("Complete() error = nil, want provider error")
	}

	if meta.Status != "provider_unavailable" {
		t.Fatalf("Status = %q, want %q", meta.Status, "provider_unavailable")
	}
	if meta.ErrorKind != string(domain.KindProviderUnavailable) {
		t.Fatalf("ErrorKind = %q, want %q", meta.ErrorKind, domain.KindProviderUnavailable)
	}
	if meta.ProviderLatencyMs < 0 {
		t.Fatalf("ProviderLatencyMs = %v, want >= 0", meta.ProviderLatencyMs)
	}
	if meta.TotalGatewayLatencyMs < 0 {
		t.Fatalf("TotalGatewayLatencyMs = %v, want >= 0", meta.TotalGatewayLatencyMs)
	}
}

// TestServiceComplete_MetadataValidationError verifies that a semantic
// validation failure sets status and error kind to validation_failed.
func TestServiceComplete_MetadataValidationError(t *testing.T) {
	response := `{"shipment_id":"ship-123","risk":"high_risk","confidence":1.7,"reasons":["delay"]}`
	service := application.NewService(provider.NewFakeProvider(response))

	_, meta, err := service.Complete(context.Background(), validRequest())
	if err == nil {
		t.Fatal("Complete() error = nil, want validation error")
	}

	if meta.Status != "validation_failed" {
		t.Fatalf("Status = %q, want %q", meta.Status, "validation_failed")
	}
	if meta.ErrorKind != string(domain.KindValidationFailed) {
		t.Fatalf("ErrorKind = %q, want %q", meta.ErrorKind, domain.KindValidationFailed)
	}
	if meta.ValidationLatencyMs < 0 {
		t.Fatalf("ValidationLatencyMs = %v, want >= 0", meta.ValidationLatencyMs)
	}
}

// TestServiceComplete_MetadataTimeout verifies that a deadline exceeded
