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

// TestServiceComplete_MetadataSuccess verifies that a successful pipeline execution
// captures complete operational telemetry—including tenant context, token counts,
// estimated costs, fine-grained latencies, and success status flags.
func TestServiceComplete_MetadataSuccess(t *testing.T) {
	response := `{
		"shipment_id": "SH-123",
		"risk": "high_risk",
		"confidence": 0.94,
		"reasons": ["Customs clearance delay detected"]
	}`

	svc := setupMetadataTestService(t, provider.NewFakeProvider("fake", response))
	cmd := validMetadataCompleteCommand()

	result, meta, err := svc.Complete(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Complete() returned unexpected error: %v", err)
	}

	// 1. Context & Routing Identity Verification
	if meta.RequestID != cmd.RequestID {
		t.Errorf("RequestID = %q, want %q", meta.RequestID, cmd.RequestID)
	}
	if meta.TenantID != cmd.TenantID {
		t.Errorf("TenantID = %q, want %q", meta.TenantID, cmd.TenantID)
	}
	if meta.ContractVersion != cmd.ContractVersion {
		t.Errorf("ContractVersion = %q, want %q", meta.ContractVersion, cmd.ContractVersion)
	}
	if meta.TaskType != cmd.TaskType {
		t.Errorf("TaskType = %v, want %v", meta.TaskType, cmd.TaskType)
	}

	// 2. Strongly-Typed Execution & Validation Status Checks
	if meta.Status != domain.ExecutionSucceeded {
		t.Errorf("Status = %v, want %v", meta.Status, domain.ExecutionSucceeded)
	}
	if meta.ValidationStatus != domain.ValidationPassed {
		t.Errorf("ValidationStatus = %v, want %v", meta.ValidationStatus, domain.ValidationPassed)
	}
	if meta.ErrorKind != "" {
		t.Errorf("ErrorKind = %v, want empty (no error)", meta.ErrorKind)
	}

	// 3. Operational & Telemetry Metrics Verification
	if meta.Provider == "" {
		t.Error("Provider is empty")
	}
	if meta.ProviderLatencyMs < 0 {
		t.Errorf("ProviderLatencyMs = %v, want >= 0", meta.ProviderLatencyMs)
	}
	if meta.ValidationLatencyMs < 0 {
		t.Errorf("ValidationLatencyMs = %v, want >= 0", meta.ValidationLatencyMs)
	}
	if meta.TotalGatewayLatencyMs < 0 {
		t.Errorf("TotalGatewayLatencyMs = %v, want >= 0", meta.TotalGatewayLatencyMs)
	}

	// 4. FinOps Token & Cost Metrics Validation
	if meta.TotalTokens != (meta.InputTokens + meta.OutputTokens) {
		t.Errorf("TotalTokens (%d) != InputTokens (%d) + OutputTokens (%d)",
			meta.TotalTokens, meta.InputTokens, meta.OutputTokens)
	}
	if meta.EstimatedCostUSD < 0 {
		t.Errorf("EstimatedCostUSD = %v, want >= 0", meta.EstimatedCostUSD)
	}

	// 5. Verify domain result payload integrity
	if result == nil {
		t.Fatal("TaskResult is nil on success")
	}
}

// TestServiceComplete_MetadataProviderError verifies that upstream provider failures
// record the exact classification, set execution status, and capture latency.
func TestServiceComplete_MetadataProviderError(t *testing.T) {
	baseErr := errors.New("upstream connection refused")
	fake := provider.NewFakeProvider("fake", "")
	fake.Err = baseErr

	svc := setupMetadataTestService(t, fake)
	cmd := validMetadataCompleteCommand()

	_, meta, err := svc.Complete(context.Background(), cmd)
	if err == nil {
		t.Fatal("Complete() expected error, got nil")
	}

	if meta.Status != domain.ExecutionProviderFailed {
		t.Errorf("Status = %v, want %v", meta.Status, domain.ExecutionProviderFailed)
	}
	if meta.ValidationStatus != domain.ValidationNotRun {
		t.Errorf("ValidationStatus = %v, want %v", meta.ValidationStatus, domain.ValidationNotRun)
	}
	if meta.ErrorKind != domain.KindProviderUnavailable {
		t.Errorf("ErrorKind = %v, want %v", meta.ErrorKind, domain.KindProviderUnavailable)
	}
	if meta.ProviderLatencyMs < 0 {
		t.Errorf("ProviderLatencyMs = %v, want >= 0", meta.ProviderLatencyMs)
	}
	if meta.TotalGatewayLatencyMs < 0 {
		t.Errorf("TotalGatewayLatencyMs = %v, want >= 0", meta.TotalGatewayLatencyMs)
	}
}

// TestServiceComplete_MetadataValidationError verifies validation parsing errors.
func TestServiceComplete_MetadataValidationError(t *testing.T) {
	// Confidence > 1.0 triggers domain policy validation error
	invalidResponse := `{"shipment_id":"SH-123","risk":"high_risk","confidence":99.0,"reasons":["invalid confidence"]}`
	svc := setupMetadataTestService(t, provider.NewFakeProvider("fake", invalidResponse))
	cmd := validMetadataCompleteCommand()

	_, meta, err := svc.Complete(context.Background(), cmd)
	if err == nil {
		t.Fatal("Complete() expected validation error, got nil")
	}

	if meta.Status != domain.ExecutionValidationFailed {
		t.Errorf("Status = %v, want %v", meta.Status, domain.ExecutionValidationFailed)
	}
	if meta.ValidationStatus != domain.ValidationFailed {
		t.Errorf("ValidationStatus = %v, want %v", meta.ValidationStatus, domain.ValidationFailed)
	}
	if meta.ErrorKind != domain.KindValidationFailed {
		t.Errorf("ErrorKind = %v, want %v", meta.ErrorKind, domain.KindValidationFailed)
	}
	if meta.ValidationLatencyMs < 0 {
		t.Errorf("ValidationLatencyMs = %v, want >= 0", meta.ValidationLatencyMs)
	}
}

// TestServiceComplete_MetadataTimeout verifies timeout and cancellation handling.
func TestServiceComplete_MetadataTimeout(t *testing.T) {
	svc := setupMetadataTestService(t, provider.NewFakeProvider("fake", "{}"))
	cmd := validMetadataCompleteCommand()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Immediately cancel context to trigger pre-flight check

	_, meta, err := svc.Complete(ctx, cmd)
	if err == nil {
		t.Fatal("Complete() expected cancellation error, got nil")
	}

	if meta.Status != domain.ExecutionRequestCancelled {
		t.Errorf("Status = %v, want %v", meta.Status, domain.ExecutionRequestCancelled)
	}
	if meta.ErrorKind != domain.KindRequestCanceled {
		t.Errorf("ErrorKind = %v, want %v", meta.ErrorKind, domain.KindRequestCanceled)
	}
	if meta.ValidationStatus != domain.ValidationNotRun {
		t.Errorf("ValidationStatus = %v, want %v", meta.ValidationStatus, domain.ValidationNotRun)
	}
}

// --- Test Helpers ---

func setupMetadataTestService(t *testing.T, p application.Provider) *application.Service {
	t.Helper()
	taskDef := application.NewShipmentRiskTaskDefinition()
	registry, err := application.NewTaskRegistry(taskDef)
	if err != nil {
		t.Fatalf("failed to create task registry: %v", err)
	}

	policy := domain.ExecutionPolicy{
		CurrentContractVersion: application.CurrentContractVersion,
		MaxEvidenceReferences:  32,
	}

	svc, err := application.NewService(p, registry, policy, nil, nil)
	if err != nil {
		t.Fatalf("failed to create application service: %v", err)
	}
	return svc
}

func validMetadataCompleteCommand() application.CompleteCommand {
	return application.CompleteCommand{
		ContractVersion:     application.CurrentContractVersion,
		TenantID:            "tenant-xyz",
		RequestID:           "req-abc-123",
		TaskType:            domain.TaskShipmentDelayRisk,
		TaskSchemaVersion:   "shipment_delay_risk.v1",
		Prompt:              "Classify shipment delay risk.",
		PromptVersion:       "shipment-delay-risk.prompt.v1",
		OutputSchemaVersion: "shipment_delay_risk.result.v1",
		MaxTokens:           256,
		Subjects: []domain.EntityReference{
			{
				Type: domain.EntityShipment,
				ID:   "SH-123",
			},
		},
		EvidenceRefs: []domain.EvidenceReference{
			{EvidenceID: "EV-001"},
		},
		Deadline: time.Now().Add(5 * time.Second),
	}
}
