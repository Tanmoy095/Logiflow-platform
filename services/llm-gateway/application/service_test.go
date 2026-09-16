package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/infrastructure/provider"
)

func newTestService(t *testing.T, p application.Provider) *application.Service {
	t.Helper()
	registry, err := application.NewTaskRegistry(application.NewShipmentRiskTaskDefinition())
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	service, err := application.NewService(
		p,
		registry,
		domain.ExecutionPolicy{
			CurrentContractVersion: application.CurrentContractVersion,
			MaxEvidenceReferences:  32,
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	return service
}

func validCommand() application.CompleteCommand {
	return application.CompleteCommand{
		ContractVersion:     application.CurrentContractVersion,
		TenantID:            "tenant-1",
		RequestID:           "req-1",
		TaskType:            domain.TaskShipmentDelayRisk,
		TaskSchemaVersion:   "shipment_delay_risk.v1",
		Prompt:              "Classify shipment delay risk.",
		PromptVersion:       "shipment-delay-risk.prompt.v1",
		OutputSchemaVersion: "shipment_delay_risk.result.v1",
		MaxTokens:           256,
		Subjects: []domain.EntityReference{{
			Type: domain.EntityShipment,
			ID:   "SH-123",
		}},
		EvidenceRefs: []domain.EvidenceReference{{EvidenceID: "EV-001"}},
		Deadline:     time.Now().Add(5 * time.Second),
	}
}

func TestCompleteBuildsTrustedShipmentRiskResult(t *testing.T) {
	p := provider.NewFakeProvider("fake", `{"shipment_id":"SH-123","risk":"high_risk","confidence":0.91,"reasons":["port congestion"]}`)
	service := newTestService(t, p)

	result, metadata, err := service.Complete(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Domain and validation assertions
	if metadata.ValidationStatus != domain.ValidationPassed {
		t.Fatalf("validation status = %q, expected %q", metadata.ValidationStatus, domain.ValidationPassed)
	}

	// Telemetry & Metadata assertions
	if metadata.Status != domain.ExecutionSucceeded {
		t.Fatalf("execution status = %q, expected %q", metadata.Status, domain.ExecutionSucceeded)
	}
	if metadata.Provider != "fake" {
		t.Fatalf("provider = %q, expected %q", metadata.Provider, "fake")
	}
	if metadata.TotalGatewayLatencyMs <= 0 {
		t.Fatalf("total latency expected to be > 0, got %f", metadata.TotalGatewayLatencyMs)
	}
	if metadata.ErrorKind != "" {
		t.Fatalf("expected empty error kind on success, got %q", metadata.ErrorKind)
	}
}

func TestCompleteRejectsMalformedJSON(t *testing.T) {
	p := provider.NewFakeProvider("fake", `{not-json}`)
	service := newTestService(t, p)

	_, metadata, err := service.Complete(context.Background(), validCommand())
	if err == nil {
		t.Fatal("expected validation error")
	}
	if metadata.ErrorKind != domain.KindValidationFailed {
		t.Fatalf("error kind = %q, expected %q", metadata.ErrorKind, domain.KindValidationFailed)
	}
	if metadata.Status != domain.ExecutionValidationFailed {
		t.Fatalf("execution status = %q, expected %q", metadata.Status, domain.ExecutionValidationFailed)
	}
	if metadata.ValidationStatus != domain.ValidationFailed {
		t.Fatalf("validation status = %q, expected %q", metadata.ValidationStatus, domain.ValidationFailed)
	}
}

func TestCompleteRejectsHighRiskWithoutReasons(t *testing.T) {
	p := provider.NewFakeProvider("fake", `{"shipment_id":"SH-123","risk":"high_risk","confidence":0.91,"reasons":[]}`)
	service := newTestService(t, p)

	_, metadata, err := service.Complete(context.Background(), validCommand())
	if err == nil {
		t.Fatal("expected domain validation failure")
	}
	if metadata.ErrorKind != domain.KindValidationFailed {
		t.Fatalf("error kind = %q, expected %q", metadata.ErrorKind, domain.KindValidationFailed)
	}
}

func TestCompleteRejectsWrongShipmentReturnedByProvider(t *testing.T) {
	p := provider.NewFakeProvider("fake", `{"shipment_id":"SH-999","risk":"no_risk","confidence":0.99,"reasons":[]}`)
	service := newTestService(t, p)

	_, metadata, err := service.Complete(context.Background(), validCommand())
	if err == nil {
		t.Fatal("expected shipment identity validation failure")
	}
	if metadata.ErrorKind != domain.KindValidationFailed {
		t.Fatalf("error kind = %q, expected %q", metadata.ErrorKind, domain.KindValidationFailed)
	}
}

func TestCompletePropagatesProviderCancellation(t *testing.T) {
	p := provider.NewFakeProvider("fake", `{"shipment_id":"SH-123","risk":"no_risk","confidence":0.9,"reasons":[]}`)
	p.Delay = 100 * time.Millisecond
	service := newTestService(t, p)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, metadata, err := service.Complete(ctx, validCommand())
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
	if metadata.ErrorKind != domain.KindProviderTimeout && metadata.ErrorKind != domain.KindRequestCanceled {
		t.Fatalf("unexpected metadata error kind: %q", metadata.ErrorKind)
	}
	if metadata.Status != domain.ExecutionDeadlineExceeded && metadata.Status != domain.ExecutionRequestCancelled {
		t.Fatalf("unexpected metadata execution status: %q", metadata.Status)
	}
}
