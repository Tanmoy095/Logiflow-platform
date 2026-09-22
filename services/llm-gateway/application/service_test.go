package application_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/infrastructure/provider"
)

// =============================================================================
// Test fixtures
// =============================================================================

// testClock is a monotonic, thread-safe clock that advances by exactly 1ms on
// every Now() call.
//
// Why advance on every call? The Service uses clock.Now() to compute latencies
// (provider, validation, total gateway). If the clock returned the same
// instant forever, every latency would be 0ms and we could not assert that
// latency measurement actually works. Advancing by 1ms also keeps the tests
// fully deterministic — no wall-clock dependence, no flakes.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{
		now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Millisecond)
	return c.now
}

// newTestEventIDGen returns a deterministic generator that yields "evt-1",
// "evt-2", ... Each call to newTestEventIDGen gets its own counter so
// parallel tests don't interfere with each other.
func newTestEventIDGen() application.EventIDGenerator {
	var counter atomic.Int64
	return func() (string, error) {
		return fmt.Sprintf("evt-%d", counter.Add(1)), nil
	}
}

// chanPublisher captures published usage events on a buffered channel.
//
// The Service publishes asynchronously (fire-and-forget goroutine), so tests
// need a way to synchronize on the publish without flaky time.Sleep. A
// buffered channel gives us a deterministic "wait until the event arrives"
// primitive. The buffer is generous so the goroutine never blocks and the
// test can read at its own pace.
type chanPublisher struct {
	ch chan domain.AIUsageEvent
}

func newChanPublisher() *chanPublisher {
	return &chanPublisher{ch: make(chan domain.AIUsageEvent, 8)}
}

func (p *chanPublisher) PublishUsageEvent(_ context.Context, e domain.AIUsageEvent) error {
	p.ch <- e
	return nil
}

// waitForEvent blocks until a usage event is published or the timeout fires.
// Used by tests that need to assert on the async side effect.
func (p *chanPublisher) waitForEvent(t *testing.T) domain.AIUsageEvent {
	t.Helper()
	select {
	case e := <-p.ch:
		return e
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for usage event")
		return domain.AIUsageEvent{}
	}
}

// assertNoEvent fails the test if any usage event was published within the
// grace period. Used by preflight-rejection tests where no provider work
// happened and therefore no event should be emitted.
func (p *chanPublisher) assertNoEvent(t *testing.T) {
	t.Helper()
	select {
	case e := <-p.ch:
		t.Fatalf("unexpected usage event published: %+v", e)
	case <-time.After(50 * time.Millisecond):
		// No event — correct.
	}
}

// newTestService constructs a fully wired Service with deterministic
// dependencies. It returns the service and the publisher so tests can
// inspect asynchronous side effects.
func newTestService(t *testing.T, p application.Provider) (*application.Service, *chanPublisher) {
	t.Helper()

	registry, err := application.NewTaskRegistry(application.NewShipmentRiskTaskDefinition())
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	publisher := newChanPublisher()

	service, err := application.NewService(
		p,
		registry,
		domain.ExecutionPolicy{
			CurrentContractVersion: application.CurrentContractVersion,
			MaxEvidenceReferences:  32,
		},
		publisher,
		newTestClock(),
		newTestEventIDGen(),
	)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	return service, publisher
}

// validCommand returns a CompleteCommand that passes every preflight check.
// Individual tests mutate one field to exercise a specific failure path.
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

// =============================================================================
// Happy path
// =============================================================================

// Test: a well-formed provider response flows all the way through the pipeline
// and produces a trusted ShipmentRiskResult with success metadata.
//
// Expected:
//   - err == nil
//   - result != nil
//   - metadata.Status           == ExecutionSucceeded
//   - metadata.ValidationStatus == ValidationPassed
//   - metadata.Provider         == "fake"
//   - metadata.Model            == "model-a"
//   - metadata.ErrorKind        == "" (empty on success)
//   - metadata.TotalGatewayLatencyMs > 0
//   - a usage event is published with the same success status
func TestCompleteBuildsTrustedShipmentRiskResult(t *testing.T) {
	p := provider.NewFakeProvider(
		"fake",
		"model-a",
		`{"shipment_id":"SH-123","risk":"high_risk","confidence":0.91,"reasons":["port congestion"]}`,
	)
	service, publisher := newTestService(t, p)

	result, metadata, err := service.Complete(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if metadata.ValidationStatus != domain.ValidationPassed {
		t.Fatalf("validation status = %q, want %q", metadata.ValidationStatus, domain.ValidationPassed)
	}
	if metadata.Status != domain.ExecutionSucceeded {
		t.Fatalf("status = %q, want %q", metadata.Status, domain.ExecutionSucceeded)
	}
	if metadata.Provider != "fake" {
		t.Fatalf("provider = %q, want fake", metadata.Provider)
	}
	if metadata.Model != "model-a" {
		t.Fatalf("model = %q, want model-a", metadata.Model)
	}
	if metadata.ErrorKind != "" {
		t.Fatalf("error kind = %q, want empty", metadata.ErrorKind)
	}
	if metadata.TotalGatewayLatencyMs <= 0 {
		t.Fatalf("total gateway latency = %f, want > 0", metadata.TotalGatewayLatencyMs)
	}
	if p.Calls != 1 {
		t.Fatalf("provider calls = %d, want 1", p.Calls)
	}

	// Usage event must fire on success.
	event := publisher.waitForEvent(t)
	if event.Status != domain.ExecutionSucceeded {
		t.Fatalf("event status = %q, want %q", event.Status, domain.ExecutionSucceeded)
	}
	if event.ValidationStatus != domain.ValidationPassed {
		t.Fatalf("event validation status = %q, want %q", event.ValidationStatus, domain.ValidationPassed)
	}
	if event.Provider != "fake" {
		t.Fatalf("event provider = %q, want fake", event.Provider)
	}
}

// =============================================================================
// Preflight rejections (provider never invoked)
// =============================================================================
//
// Each of the following tests exercises a stage before the provider call.
// The shared assertions are:
//   - err != nil
//   - p.Calls == 0        (no provider money spent)
//   - no usage event      (no tokens consumed)
// =============================================================================

// Test: a command with a missing/invalid contract version is rejected at
// Stage 1 (generic command shape validation).
func TestCompleteRejectsInvalidCommandShape(t *testing.T) {
	p := provider.NewFakeProvider("fake", "model-a", `{}`)
	service, publisher := newTestService(t, p)

	cmd := validCommand()
	cmd.ContractVersion = "" // fails cmd.Validate()

	_, metadata, err := service.Complete(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected preflight error")
	}
	if metadata.ErrorKind != domain.KindInvalidArgument {
		t.Fatalf("error kind = %q, want %q", metadata.ErrorKind, domain.KindInvalidArgument)
	}
	if metadata.Status != domain.ExecutionPreflightFailed {
		t.Fatalf("status = %q, want %q", metadata.Status, domain.ExecutionPreflightFailed)
	}
	if p.Calls != 0 {
		t.Fatalf("provider calls = %d, want 0 (rejected before provider)", p.Calls)
	}
	publisher.assertNoEvent(t)
}

// Test: a command whose contract_version matches the gateway but whose policy
// rejects it is classified as KindContractMismatch. We force this by
// constructing a Service with a policy that expects a different version.
func TestCompleteRejectsPolicyViolation(t *testing.T) {
	p := provider.NewFakeProvider("fake", "model-a", `{}`)

	registry, err := application.NewTaskRegistry(application.NewShipmentRiskTaskDefinition())
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	publisher := newChanPublisher()
	service, err := application.NewService(
		p,
		registry,
		// Policy expects a different contract version than the command sends.
		domain.ExecutionPolicy{
			CurrentContractVersion: "some-other-contract.v1",
			MaxEvidenceReferences:  32,
		},
		publisher,
		newTestClock(),
		newTestEventIDGen(),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, metadata, err := service.Complete(context.Background(), validCommand())
	if err == nil {
		t.Fatal("expected policy error")
	}
	if metadata.ErrorKind != domain.KindContractMismatch {
		t.Fatalf("error kind = %q, want %q", metadata.ErrorKind, domain.KindContractMismatch)
	}
	if metadata.Status != domain.ExecutionPreflightFailed {
		t.Fatalf("status = %q, want %q", metadata.Status, domain.ExecutionPreflightFailed)
	}
	if p.Calls != 0 {
		t.Fatalf("provider calls = %d, want 0", p.Calls)
	}
	publisher.assertNoEvent(t)
}

// Test: an unknown task_type is rejected at Stage 3 by the registry.
func TestCompleteRejectsUnknownTaskType(t *testing.T) {
	p := provider.NewFakeProvider("fake", "model-a", `{}`)
	service, publisher := newTestService(t, p)

	cmd := validCommand()
	cmd.TaskType = domain.TaskType("not_a_real_task")

	_, metadata, err := service.Complete(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected unsupported task error")
	}
	if metadata.ErrorKind != domain.KindUnsupportedTask {
		t.Fatalf("error kind = %q, want %q", metadata.ErrorKind, domain.KindUnsupportedTask)
	}
	if metadata.Status != domain.ExecutionUnsupportedTask {
		t.Fatalf("status = %q, want %q", metadata.Status, domain.ExecutionUnsupportedTask)
	}
	if p.Calls != 0 {
		t.Fatalf("provider calls = %d, want 0", p.Calls)
	}
	publisher.assertNoEvent(t)
}

// Test: shipment_delay_risk requires exactly one subject. Zero subjects is
// rejected by the capability's ValidateCommand.
func TestCompleteRejectsMissingSubject(t *testing.T) {
	p := provider.NewFakeProvider("fake", "model-a", `{}`)
	service, publisher := newTestService(t, p)

	cmd := validCommand()
	cmd.Subjects = nil

	_, metadata, err := service.Complete(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected capability validation error")
	}
	if metadata.ErrorKind != domain.KindContractMismatch {
		t.Fatalf("error kind = %q, want %q", metadata.ErrorKind, domain.KindContractMismatch)
	}
	if p.Calls != 0 {
		t.Fatalf("provider calls = %d, want 0", p.Calls)
	}
	publisher.assertNoEvent(t)
}

// Test: a caller context that is already canceled before Complete is called
// must abort at Stage 5 with zero provider work.
func TestCompleteRejectsPreCanceledContext(t *testing.T) {
	p := provider.NewFakeProvider("fake", "model-a", `{}`)
	service, publisher := newTestService(t, p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel BEFORE calling Complete

	_, metadata, err := service.Complete(ctx, validCommand())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if metadata.ErrorKind != domain.KindRequestCanceled {
		t.Fatalf("error kind = %q, want %q", metadata.ErrorKind, domain.KindRequestCanceled)
	}
	if metadata.Status != domain.ExecutionRequestCanceled {
		t.Fatalf("status = %q, want %q", metadata.Status, domain.ExecutionRequestCanceled)
	}
	if p.Calls != 0 {
		t.Fatalf("provider calls = %d, want 0", p.Calls)
	}
	publisher.assertNoEvent(t)
}

// =============================================================================
// Provider-stage failures (provider WAS invoked, so usage events fire)
// =============================================================================

// Test: a caller context that expires during the provider call must be
// classified as a deadline exceeded, and the provider must have been called
// exactly once.
func TestCompletePropagatesCallerDeadlineDuringProviderCall(t *testing.T) {
	p := provider.NewFakeProvider("fake", "model-a",
		`{"shipment_id":"SH-123","risk":"no_risk","confidence":0.9,"reasons":[]}`)
	p.Delay = 100 * time.Millisecond // longer than caller timeout
	service, _ := newTestService(t, p)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	cmd := validCommand()
	cmd.Deadline = time.Time{} // clear the command deadline; caller ctx wins

	_, metadata, err := service.Complete(ctx, cmd)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if metadata.ErrorKind != domain.KindDeadlineExceeded {
		t.Fatalf("error kind = %q, want %q", metadata.ErrorKind, domain.KindDeadlineExceeded)
	}
	if metadata.Status != domain.ExecutionDeadlineExceeded {
		t.Fatalf("status = %q, want %q", metadata.Status, domain.ExecutionDeadlineExceeded)
	}
	if p.Calls != 1 {
		t.Fatalf("provider calls = %d, want 1 (provider was invoked)", p.Calls)
	}
}

// Test: any provider error that is NOT a context error collapses to
// KindProviderUnavailable at the domain boundary. The Service intentionally
// does not surface the specific ProviderFailureKind.
func TestCompleteCollapsesProviderErrorToUnavailable(t *testing.T) {
	p := provider.NewFakeProvider("fake", "model-a", "")
	p.Err = provider.ErrUnavailable // typed ProviderError from fake.go
	service, _ := newTestService(t, p)

	_, metadata, err := service.Complete(context.Background(), validCommand())
	if err == nil {
		t.Fatal("expected provider error")
	}
	if metadata.ErrorKind != domain.KindProviderUnavailable {
		t.Fatalf("error kind = %q, want %q", metadata.ErrorKind, domain.KindProviderUnavailable)
	}
	if metadata.Status != domain.ExecutionProviderFailed {
		t.Fatalf("status = %q, want %q", metadata.Status, domain.ExecutionProviderFailed)
	}
	if p.Calls != 1 {
		t.Fatalf("provider calls = %d, want 1", p.Calls)
	}
}

// =============================================================================
// Output-validation failures (provider WAS invoked, usage event still fires)
// =============================================================================

// Test: syntactically invalid JSON from the provider fails Stage 7 (decode).
// Even so, the provider was invoked — the usage event must still be
// published so billing captures the wasted tokens.
func TestCompleteRejectsMalformedJSON(t *testing.T) {
	p := provider.NewFakeProvider("fake", "model-a", `{not-json}`)
	service, publisher := newTestService(t, p)

	_, metadata, err := service.Complete(context.Background(), validCommand())
	if err == nil {
		t.Fatal("expected validation error")
	}
	if metadata.ErrorKind != domain.KindValidationFailed {
		t.Fatalf("error kind = %q, want %q", metadata.ErrorKind, domain.KindValidationFailed)
	}
	if metadata.Status != domain.ExecutionValidationFailed {
		t.Fatalf("status = %q, want %q", metadata.Status, domain.ExecutionValidationFailed)
	}
	if metadata.ValidationStatus != domain.ValidationFailed {
		t.Fatalf("validation status = %q, want %q", metadata.ValidationStatus, domain.ValidationFailed)
	}
	if p.Calls != 1 {
		t.Fatalf("provider calls = %d, want 1", p.Calls)
	}

	// Provider work happened → usage event must still fire.
	event := publisher.waitForEvent(t)
	if event.ValidationStatus != domain.ValidationFailed {
		t.Fatalf("event validation status = %q, want %q", event.ValidationStatus, domain.ValidationFailed)
	}
}

// Test: a schema-valid response that violates a business invariant
// (high_risk requires at least one reason) is rejected at Stage 3b.
func TestCompleteRejectsHighRiskWithoutReasons(t *testing.T) {
	p := provider.NewFakeProvider(
		"fake",
		"model-a",
		`{"shipment_id":"SH-123","risk":"high_risk","confidence":0.91,"reasons":[]}`,
	)
	service, _ := newTestService(t, p)

	_, metadata, err := service.Complete(context.Background(), validCommand())
	if err == nil {
		t.Fatal("expected domain validation failure")
	}
	if metadata.ErrorKind != domain.KindValidationFailed {
		t.Fatalf("error kind = %q, want %q", metadata.ErrorKind, domain.KindValidationFailed)
	}
	if p.Calls != 1 {
		t.Fatalf("provider calls = %d, want 1", p.Calls)
	}
}

// Test: a valid JSON object that refers to a different shipment than the one
// requested is rejected at Stage 3a (identity correlation).
func TestCompleteRejectsWrongShipmentReturnedByProvider(t *testing.T) {
	p := provider.NewFakeProvider(
		"fake",
		"model-a",
		`{"shipment_id":"SH-999","risk":"no_risk","confidence":0.99,"reasons":[]}`,
	)
	service, _ := newTestService(t, p)

	_, metadata, err := service.Complete(context.Background(), validCommand())
	if err == nil {
		t.Fatal("expected identity correlation failure")
	}
	if metadata.ErrorKind != domain.KindValidationFailed {
		t.Fatalf("error kind = %q, want %q", metadata.ErrorKind, domain.KindValidationFailed)
	}
}

// =============================================================================
// Constructor validation — fail-fast at composition time
// =============================================================================

// helper: builds a valid dependency tuple so each test can nil exactly one
// argument and verify the constructor rejects it.
func testDeps(t *testing.T) (
	application.Provider,
	*application.TaskRegistry,
	domain.ExecutionPolicy,
	application.UsagePublisher,
	application.Clock,
	application.EventIDGenerator,
) {
	t.Helper()
	registry, err := application.NewTaskRegistry(application.NewShipmentRiskTaskDefinition())
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return provider.NewFakeProvider("fake", "model-a", `{}`),
		registry,
		domain.ExecutionPolicy{CurrentContractVersion: application.CurrentContractVersion},
		newChanPublisher(),
		newTestClock(),
		newTestEventIDGen()
}

func TestNewServiceRejectsNilProvider(t *testing.T) {
	_, reg, pol, pub, clk, idg := testDeps(t)
	if _, err := application.NewService(nil, reg, pol, pub, clk, idg); err == nil {
		t.Fatal("expected error for nil provider")
	}
}

func TestNewServiceRejectsNilRegistry(t *testing.T) {
	prov, _, pol, pub, clk, idg := testDeps(t)
	if _, err := application.NewService(prov, nil, pol, pub, clk, idg); err == nil {
		t.Fatal("expected error for nil registry")
	}
}

func TestNewServiceRejectsNilClock(t *testing.T) {
	prov, reg, pol, pub, _, idg := testDeps(t)
	if _, err := application.NewService(prov, reg, pol, pub, nil, idg); err == nil {
		t.Fatal("expected error for nil clock")
	}
}

func TestNewServiceRejectsNilEventIDGenerator(t *testing.T) {
	prov, reg, pol, pub, clk, _ := testDeps(t)
	if _, err := application.NewService(prov, reg, pol, pub, clk, nil); err == nil {
		t.Fatal("expected error for nil event id generator")
	}
}

// The execution policy is a VALUE (not pointer), so nil-checking is not
// possible. Instead the constructor validates that the policy is actually
// configured. This test guards against a zero-value policy silently disabling
// gateway guardrails.
func TestNewServiceRejectsUnconfiguredPolicy(t *testing.T) {
	prov, reg, _, pub, clk, idg := testDeps(t)
	_, err := application.NewService(
		prov,
		reg,
		domain.ExecutionPolicy{}, // CurrentContractVersion is empty
		pub,
		clk,
		idg,
	)
	if err == nil {
		t.Fatal("expected error for unconfigured policy")
	}
}
