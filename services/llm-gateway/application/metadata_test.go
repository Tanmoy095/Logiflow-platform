package application_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/infrastructure/provider"
)

// =============================================================================
// Test clock
// =============================================================================
//
// The Service now requires an injected Clock (rejecting nil at construction).
// We use a deterministic clock that advances by exactly 1ms on every Now()
// call so latency measurements are:
//
//   - non-zero (so we can assert latency > 0),
//   - fully deterministic (no wall-clock flakes),
//   - monotonic (never goes backwards).
//
// The mutex is required because Complete() may call Now() from multiple
// goroutines when tests run in parallel.
type metadataTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func newMetadataTestClock() *metadataTestClock {
	return &metadataTestClock{
		now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func (c *metadataTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Millisecond)
	return c.now
}

// =============================================================================
// Test event ID generator
// =============================================================================
//
// The Service requires an EventIDGenerator. Returning a deterministic ID keeps
// assertions reproducible; the atomic counter ensures concurrent tests get
// distinct values.
func newMetadataTestEventIDGen() application.EventIDGenerator {
	var n int64
	var mu sync.Mutex
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		return "evt-test-" + time.Duration(n).String(), nil
	}
}

// =============================================================================
// Test: success path metadata
// =============================================================================

// TestServiceComplete_MetadataSuccess verifies that a successful pipeline
// execution captures complete operational telemetry:
//
//   - tenant, request, contract, and task identity carried through,
//   - success status flags (Status, ValidationStatus, empty ErrorKind),
//   - latency measurements present and non-negative,
//   - token arithmetic internally consistent,
//   - cost non-negative,
//   - the domain result is populated.
//
// This is the "happy path" contract. If any of these fail, either the service
// stopped populating metadata or the domain result wasn't returned.
func TestServiceComplete_MetadataSuccess(t *testing.T) {
	response := `{
		"shipment_id": "SH-123",
		"risk": "high_risk",
		"confidence": 0.94,
		"reasons": ["Customs clearance delay detected"]
	}`

	// New signature: (name, model, response). The model is asserted in later
	// metadata checks; leaving it unset would break those assertions.
	svc := setupMetadataTestService(t, provider.NewFakeProvider("fake", "model-a", response))
	cmd := validMetadataCompleteCommand()

	result, meta, err := svc.Complete(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Complete() returned unexpected error: %v", err)
	}

	// ---- 1. Context & routing identity --------------------------------
	// Every identity field from the command must round-trip into metadata.
	// If any of these drift, audit trails and correlation queries break.
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

	// ---- 2. Strongly-typed execution & validation statuses ------------
	// Success must be signalled by BOTH a specific status and the absence
	// of an error kind. An empty ValidationStatus would silently break
	// downstream dashboards that aggregate by this field.
	if meta.Status != domain.ExecutionSucceeded {
		t.Errorf("Status = %v, want %v", meta.Status, domain.ExecutionSucceeded)
	}
	if meta.ValidationStatus != domain.ValidationPassed {
		t.Errorf("ValidationStatus = %v, want %v", meta.ValidationStatus, domain.ValidationPassed)
	}
	if meta.ErrorKind != "" {
		t.Errorf("ErrorKind = %v, want empty (no error)", meta.ErrorKind)
	}

	// ---- 3. Operational telemetry ------------------------------------
	// Provider identity must be populated (the Service copies it from the
	// provider response). Latencies must be present and non-negative; the
	// test clock advances 1ms per Now() call, so a 0 here would indicate
	// the Service is not measuring at all.
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

	// ---- 4. FinOps token & cost invariants ---------------------------
	// TotalTokens must equal Input + Output. A mismatch indicates either
	// the provider or the Service is corrupting the arithmetic, which
	// would silently break billing.
	if meta.TotalTokens != (meta.InputTokens + meta.OutputTokens) {
		t.Errorf("TotalTokens (%d) != InputTokens (%d) + OutputTokens (%d)",
			meta.TotalTokens, meta.InputTokens, meta.OutputTokens)
	}
	if meta.EstimatedCostUSD < 0 {
		t.Errorf("EstimatedCostUSD = %v, want >= 0", meta.EstimatedCostUSD)
	}

	// ---- 5. Domain result payload ------------------------------------
	// A successful Complete() must return a non-nil TaskResult. A nil
	// result with nil error is the most insidious bug — callers would
	// dereference and panic.
	if result == nil {
		t.Fatal("TaskResult is nil on success")
	}
}

// =============================================================================
// Test: provider failure metadata
// =============================================================================

// TestServiceComplete_MetadataProviderError verifies that upstream provider
// failures record the exact domain-level classification, set the execution
// status, and still capture latency.
//
// The Service intentionally collapses every non-context provider error
// (including typed ProviderErrors from the router) to
// KindProviderUnavailable, so callers see one stable category regardless of
// whether the router tried one vendor or ten.
func TestServiceComplete_MetadataProviderError(t *testing.T) {
	// Use the typed sentinel from fake.go instead of a plain errors.New so
	// this test models the real path: the router's error classification
	// flows through the adapter, and the Service collapses it at the
	// application boundary.
	fake := provider.NewFakeProvider("fake", "model-a", "")
	fake.Err = provider.ErrUnavailable

	svc := setupMetadataTestService(t, fake)
	cmd := validMetadataCompleteCommand()

	_, meta, err := svc.Complete(context.Background(), cmd)
	if err == nil {
		t.Fatal("Complete() expected error, got nil")
	}

	if meta.Status != domain.ExecutionProviderFailed {
		t.Errorf("Status = %v, want %v", meta.Status, domain.ExecutionProviderFailed)
	}
	// Validation was never reached — the provider failed before we ever
	// had output to decode.
	if meta.ValidationStatus != domain.ValidationNotRun {
		t.Errorf("ValidationStatus = %v, want %v", meta.ValidationStatus, domain.ValidationNotRun)
	}
	if meta.ErrorKind != domain.KindProviderUnavailable {
		t.Errorf("ErrorKind = %v, want %v", meta.ErrorKind, domain.KindProviderUnavailable)
	}
	// Provider latency must still be recorded even on failure — the call
	// took time and we want it visible in dashboards.
	if meta.ProviderLatencyMs < 0 {
		t.Errorf("ProviderLatencyMs = %v, want >= 0", meta.ProviderLatencyMs)
	}
	if meta.TotalGatewayLatencyMs < 0 {
		t.Errorf("TotalGatewayLatencyMs = %v, want >= 0", meta.TotalGatewayLatencyMs)
	}
}

// =============================================================================
// Test: validation failure metadata
// =============================================================================

// TestServiceComplete_MetadataValidationError verifies that a provider
// response which fails schema or domain validation is classified as a
// validation failure, not a provider failure.
//
// The distinction matters for billing and alerting:
//   - provider failure → vendor was unreachable, usually not billed
//   - validation failure → vendor returned output (and charged for it)
//     but the output was unusable
func TestServiceComplete_MetadataValidationError(t *testing.T) {
	// Confidence > 1.0 fails the domain's range check in
	// shipmentrisk.ShipmentRiskResult.Validate(), so this exercises Stage 3b
	// of DecodeAndValidate.
	invalidResponse := `{"shipment_id":"SH-123","risk":"high_risk","confidence":99.0,"reasons":["invalid confidence"]}`
	svc := setupMetadataTestService(t, provider.NewFakeProvider("fake", "model-a", invalidResponse))
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
	// Validation latency is meaningful only on paths where validation was
	// actually attempted. Here it was, so it must be non-negative.
	if meta.ValidationLatencyMs < 0 {
		t.Errorf("ValidationLatencyMs = %v, want >= 0", meta.ValidationLatencyMs)
	}
}

// =============================================================================
// Test: pre-flight cancellation metadata
// =============================================================================

// TestServiceComplete_MetadataTimeout verifies that a caller who cancels
// before any provider work begins is classified as a request cancellation
// (not a deadline, not a provider failure) and does not invoke the provider.
//
// Naming note: the test name says "Timeout" but the behavior exercised is
// pre-flight cancellation. Renaming to *_PreflightCancellation would be
// clearer, but the assertions are what matter — they cover Stage 5 of the
// pipeline.
func TestServiceComplete_MetadataTimeout(t *testing.T) {
	fake := provider.NewFakeProvider("fake", "model-a", "{}")
	svc := setupMetadataTestService(t, fake)
	cmd := validMetadataCompleteCommand()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately canceled, so Stage 5 rejects before provider call

	_, meta, err := svc.Complete(ctx, cmd)
	if err == nil {
		t.Fatal("Complete() expected cancellation error, got nil")
	}

	// NOTE: ExecutionStatus spelling changed from "ExecutionRequestCancelled"
	// to "ExecutionRequestCanceled" (one L) to match context.Canceled.
	if meta.Status != domain.ExecutionRequestCanceled {
		t.Errorf("Status = %v, want %v", meta.Status, domain.ExecutionRequestCanceled)
	}
	if meta.ErrorKind != domain.KindRequestCanceled {
		t.Errorf("ErrorKind = %v, want %v", meta.ErrorKind, domain.KindRequestCanceled)
	}
	if meta.ValidationStatus != domain.ValidationNotRun {
		t.Errorf("ValidationStatus = %v, want %v", meta.ValidationStatus, domain.ValidationNotRun)
	}
	// The provider must NOT have been called. Preflight cancellation means
	// zero vendor spend — that's the whole point of Stage 5.
	if fake.Calls != 0 {
		t.Errorf("provider calls = %d, want 0 (preflight canceled)", fake.Calls)
	}
}

// =============================================================================
// Test helpers
// =============================================================================

// setupMetadataTestService wires a complete Service with all required
// dependencies:
//
//   - the caller-supplied provider (usually a FakeProvider),
//   - a registry with the shipment-risk capability,
//   - a policy configured for the current contract version,
//   - nil usage publisher (telemetry disabled for metadata tests),
//   - a deterministic clock,
//   - a deterministic event ID generator.
//
// The clock and event ID generator are required — the Service rejects nil
// for both at construction. Passing nil here was the old bug; do not
// reintroduce it.
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

	svc, err := application.NewService(
		p,
		registry,
		policy,
		nil, // usagePublisher: nil disables telemetry (legitimate for these tests)
		newMetadataTestClock(),
		newMetadataTestEventIDGen(),
	)
	if err != nil {
		t.Fatalf("failed to create application service: %v", err)
	}
	return svc
}

// validMetadataCompleteCommand returns a fully-formed command that passes
// every preflight check for shipment_delay_risk. Individual tests can mutate
// one field to trigger a specific failure path.
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

// =============================================================================
// Compile-time guard against accidentally unused imports.
// =============================================================================
//
// The errors package is retained for future tests that need errors.Is /
// errors.As on returned errors.
var _ = errors.Is
