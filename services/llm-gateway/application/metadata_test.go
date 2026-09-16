// services/llm-gateway/application/metadata_test.go

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
        "shipment_id": "ship-123",
        "risk": "high_risk",
        "confidence": 0.94,
        "reasons": ["Customs clearance delay detected"]
    }`

	svc := setupTestService(provider.NewFakeProvider(response))
	cmd := validCompleteCommand()

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
	fake := provider.NewFakeProvider("")
	fake.Err = baseErr // Adjust if FakeProvider handles errors differently

	svc := setupTestService(fake)
	cmd := validCompleteCommand()

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
	invalidResponse := `{"shipment_id":"ship-123","risk":"high_risk","confidence":99.0}`
	svc := setupTestService(provider.NewFakeProvider(invalidResponse))
	cmd := validCompleteCommand()

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
	svc := setupTestService(provider.NewFakeProvider("{}"))
	cmd := validCompleteCommand()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

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

func setupTestService(p application.Provider) *application.Service {
	// Initialize the shipment risk task definition and register it
	taskDef := application.NewShipmentRiskTaskDefinition()

	// NewTaskRegistry returns (*TaskRegistry, error), so we handle or ignore the error in tests
	registry, err := application.NewTaskRegistry(taskDef)
	if err != nil {
		panic(err) // Safe for tests if setup fails
	}

	policy := domain.ExecutionPolicy{} // Use your actual initialization method if needed
	svc, err := application.NewService(p, registry, policy, nil, nil)
	if err != nil {
		panic(err)
	}
	return svc
}

func validCompleteCommand() application.CompleteCommand {
	return application.CompleteCommand{
		RequestID:           "req-abc-123",
		TenantID:            "tenant-xyz",
		ContractVersion:     "v1",
		TaskType:            domain.TaskShipmentDelayRisk, // Fixed: matches your registry.go task type
		TaskSchemaVersion:   "v1",                         // Required by ShipmentRiskTaskDefinition.ValidateCommand
		OutputSchemaVersion: "v1",                         // Required by ShipmentRiskTaskDefinition.ValidateCommand
		Subjects: []domain.EntityReference{ // Required by ShipmentRiskTaskDefinition.ValidateCommand
			{
				Type: domain.EntityShipment,
				ID:   "ship-123",
			},
		},
		Deadline: time.Now().Add(5 * time.Second),
	}
}
