// services/llm-gateway/application/fixture_test.go
//
// Fixture-driven integration tests for Service.Complete.
//
// Unlike the pure unit tests in service_test.go, these tests exercise the
// full pipeline using a realistic shipment-evidence fixture that is shared
// with the stream-ingestion service. The fixture is the canonical example
// of what an upstream pipeline produces; the gateway must be able to
// consume it without losing identity or fabricating results.
//
// Two guarantees are verified here:
//
//  1. A well-formed fixture flows all the way through the 8-stage pipeline
//     and produces a trusted ShipmentRiskResult whose identity matches the
//     input.
//
//  2. A broken fixture (missing shipment identity) is rejected at the
//     application preflight boundary — the gateway never fabricates a
//     trusted result from incomplete input.
//
// The fixture adapter below deliberately keeps the JSON shape and the
// application contract decoupled: if the fixture evolves, only the adapter
// changes, not the Service or the domain.
package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain/shipmentrisk"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/infrastructure/provider"
)

// =============================================================================
// Fixture representation
// =============================================================================
//
// shipmentEvidenceFixture mirrors the JSON structure owned by the
// stream-ingestion service. It exists only in test code to deserialize the
// fixture file. It is NOT a domain type — the normalized representation
// the application consumes is application.CompleteCommand.
type shipmentEvidenceFixture struct {
	ShipmentID           string `json:"shipment_id"`
	Carrier              string `json:"carrier"`
	Origin               string `json:"origin"`
	Destination          string `json:"destination"`
	ShipmentStatus       string `json:"shipment_status"`
	ExpectedDeliveryDate string `json:"expected_delivery_date"`

	Invoice struct {
		InvoiceID string  `json:"invoice_id"`
		AmountUSD float64 `json:"amount_usd"`
		Currency  string  `json:"currency"`
	} `json:"invoice"`

	Events []struct {
		Timestamp   string `json:"timestamp"`
		Type        string `json:"type"`
		Description string `json:"description"`
	} `json:"events"`

	Evidence []string `json:"evidence"`
}

// =============================================================================
// Fixture loading
// =============================================================================

// loadShipmentFixture loads the deterministic shipment-evidence fixture from
// the stream-ingestion testdata directory.
//
// The path is resolved using runtime.Caller rather than a relative path.
// A relative path like ../../stream-ingestion/... depends on the working
// directory at test time, which breaks if the test package is ever moved
// or if the test runner changes its working directory (e.g. in some CI
// sandboxes). runtime.Caller gives us the source file location, so the
// path is stable regardless of cwd.
func loadShipmentFixture(t *testing.T) shipmentEvidenceFixture {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot resolve fixture path")
	}

	// thisFile = .../services/llm-gateway/application/fixture_test.go
	// We want   .../services/stream-ingestion/testdata/shipment_evidence_001.json
	// So from the package dir we go up two levels to services/, then into
	// stream-ingestion/testdata.
	path := filepath.Join(
		filepath.Dir(thisFile),
		"..", "..",
		"stream-ingestion",
		"testdata",
		"shipment_evidence_001.json",
	)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}

	var fixture shipmentEvidenceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode fixture %s: %v", path, err)
	}
	return fixture
}

// =============================================================================
// Fixture → application adapter
// =============================================================================

// fixtureToCommand is an anti-corruption adapter.
//
// It translates the raw fixture representation into the transport-neutral
// application.CompleteCommand. Keeping this translation in the test file
// (rather than in production code) ensures:
//
//   - the domain and application layers never depend on the fixture format;
//   - a fixture schema change requires only an adapter change;
//   - the adapter itself is a place to catch identity drift (see the
//     assertion in TestServiceComplete_ShipmentEvidenceFixture below).
func fixtureToCommand(f shipmentEvidenceFixture) application.CompleteCommand {
	return application.CompleteCommand{
		ContractVersion: application.CurrentContractVersion,
		TenantID:        "tenant-fixture-001",
		RequestID:       "req-fixture-001",

		TaskType:            domain.TaskShipmentDelayRisk,
		TaskSchemaVersion:   shipmentrisk.TaskSchemaVersion,
		OutputSchemaVersion: shipmentrisk.ResultSchemaVersion,

		Prompt:        buildRiskPrompt(f),
		PromptVersion: "shipment-delay-risk.prompt.v1",
		MaxTokens:     256,

		// Subjects carry the business identity the capability must
		// evaluate. The adapter preserves it verbatim so the
		// identity-correlation check inside DecodeAndValidate has
		// something trustworthy to compare against.
		Subjects: []domain.EntityReference{{
			Type: domain.EntityShipment,
			ID:   f.ShipmentID,
		}},

		EvidenceRefs: []domain.EvidenceReference{{
			EvidenceID: "EV-FIXTURE-001",
		}},

		Deadline: time.Now().Add(5 * time.Second),
	}
}

// buildRiskPrompt constructs a deterministic prompt from the fixture.
//
// In production this would be a versioned prompt-builder registered per
// capability. Here it is inline for test simplicity, but its two
// properties matter:
//
//   - it must be deterministic (so tests are reproducible),
//   - it must contain the shipment identity (so the model has what it
//     needs to answer the correlation question correctly).
func buildRiskPrompt(f shipmentEvidenceFixture) string {
	prompt := "Analyze the shipment risk using the following evidence.\n\n" +
		"Shipment ID: " + f.ShipmentID + "\n" +
		"Carrier: " + f.Carrier + "\n" +
		"Origin: " + f.Origin + "\n" +
		"Destination: " + f.Destination + "\n" +
		"Status: " + f.ShipmentStatus + "\n" +
		"Expected delivery: " + f.ExpectedDeliveryDate + "\n\n" +
		"Evidence:\n"

	for _, e := range f.Evidence {
		prompt += "- " + e + "\n"
	}

	prompt += "\nReturn structured shipment risk output.\n" +
		"Do not invent shipment identity."

	return prompt
}

// =============================================================================
// Deterministic provider responses
// =============================================================================

// validFixtureProviderResponse is a deterministic FakeProvider response whose
// shipment_id matches the fixture (ship-123). The pipeline should accept it
// without any modification.
const validFixtureProviderResponse = `{
	"shipment_id": "ship-123",
	"risk": "high_risk",
	"confidence": 0.94,
	"reasons": [
		"Customs clearance delay detected",
		"Expected delivery date has been exceeded"
	]
}`

// =============================================================================
// Test doubles (mirrors service_test.go)
// =============================================================================
//
// These mirror the helpers in service_test.go. If the fixture test grows to
// share more helpers, promote them to a testutil package. For now, keeping
// them local avoids a cross-package dependency for two small helpers.

type fixtureTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFixtureTestClock() *fixtureTestClock {
	return &fixtureTestClock{
		now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func (c *fixtureTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Millisecond)
	return c.now
}

func newFixtureTestEventIDGen() application.EventIDGenerator {
	var n int64
	var mu sync.Mutex
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		return "evt-fixture-" + time.Duration(n).String(), nil
	}
}

// setupFixtureService wires a Service with a FakeProvider and deterministic
// clock/ID generator. usagePublisher is intentionally nil — these fixture
// tests focus on the trusted-result contract, not telemetry.
func setupFixtureService(t *testing.T, p application.Provider) *application.Service {
	t.Helper()

	registry, err := application.NewTaskRegistry(application.NewShipmentRiskTaskDefinition())
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	svc, err := application.NewService(
		p,
		registry,
		domain.ExecutionPolicy{
			CurrentContractVersion: application.CurrentContractVersion,
			MaxEvidenceReferences:  32,
		},
		nil, // telemetry disabled for this test
		newFixtureTestClock(),
		newFixtureTestEventIDGen(),
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc
}

// =============================================================================
// Test 1 — happy path through the pipeline
// =============================================================================

// TestServiceComplete_ShipmentEvidenceFixture verifies that a realistic
// shipment-evidence fixture can be:
//
//  1. loaded from disk,
//  2. adapted into the application command,
//  3. passed through the full 8-stage pipeline,
//  4. promoted into a trusted ShipmentRiskResult,
//  5. returned with identity that matches the original fixture.
//
// The adapter assertion ("adapter preserved shipment identity") is
// deliberately placed BEFORE the service call. If the adapter itself is
// buggy, we want the failure to point at the adapter — not at the service.
func TestServiceComplete_ShipmentEvidenceFixture(t *testing.T) {
	fixture := loadShipmentFixture(t)
	cmd := fixtureToCommand(fixture)

	// Sanity check: the adapter must not silently drop or rewrite the
	// shipment identity. If this fails, every downstream assertion becomes
	// meaningless because the pipeline was fed the wrong subject.
	if cmd.Subjects[0].ID != fixture.ShipmentID {
		t.Fatalf(
			"adapter changed shipment identity: fixture = %q, command = %q",
			fixture.ShipmentID,
			cmd.Subjects[0].ID,
		)
	}

	fake := provider.NewFakeProvider("fixture-fake", "model-fixture", validFixtureProviderResponse)
	svc := setupFixtureService(t, fake)

	result, metadata, err := svc.Complete(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if result == nil {
		t.Fatal("Complete() returned nil result with nil error")
	}

	// ---- Success metadata --------------------------------------------
	if metadata.Status != domain.ExecutionSucceeded {
		t.Fatalf("metadata.Status = %q, want %q", metadata.Status, domain.ExecutionSucceeded)
	}
	if metadata.ValidationStatus != domain.ValidationPassed {
		t.Fatalf("metadata.ValidationStatus = %q, want %q", metadata.ValidationStatus, domain.ValidationPassed)
	}

	// ---- Type-assert the generic TaskResult to the concrete domain type
	// The Service returns the interface; only the caller knows which
	// capability it invoked, so the assertion belongs here.
	shipmentResult, ok := result.(shipmentrisk.ShipmentRiskResult)
	if !ok {
		t.Fatalf("result type = %T, want shipmentrisk.ShipmentRiskResult", result)
	}

	// ---- Trusted-result invariants -----------------------------------
	if shipmentResult.ShipmentID != fixture.ShipmentID {
		t.Fatalf(
			"result.ShipmentID = %q, want %q",
			shipmentResult.ShipmentID, fixture.ShipmentID,
		)
	}
	if shipmentResult.Risk != shipmentrisk.RiskHighRisk {
		t.Fatalf(
			"result.Risk = %q, want %q",
			shipmentResult.Risk, shipmentrisk.RiskHighRisk,
		)
	}
	if shipmentResult.Confidence <= 0 || shipmentResult.Confidence > 1 {
		t.Fatalf("result.Confidence = %v, outside (0, 1]", shipmentResult.Confidence)
	}
	if len(shipmentResult.Reasons) == 0 {
		t.Fatal("expected at least one reason")
	}

	// ---- TaskResult identity seam ------------------------------------
	// The generic application contract must be able to identify which
	// capability produced this result without importing capability-
	// specific fields.
	if shipmentResult.TaskType() != domain.TaskShipmentDelayRisk {
		t.Fatalf(
			"result.TaskType() = %q, want %q",
			shipmentResult.TaskType(), domain.TaskShipmentDelayRisk,
		)
	}
}

// =============================================================================
// Test 2 — preflight rejection of missing identity
// =============================================================================

// TestServiceComplete_ShipmentEvidenceFixture_MissingShipmentID verifies
// that a broken fixture is rejected at the application preflight boundary.
//
// Where the rejection happens: the adapter sets Subjects[0].ID = "". The
// generic cmd.Validate() calls EntityReference.Validate() which rejects an
// empty ID at Stage 1 (command shape validation). The Service therefore
// returns a typed DomainError with Kind = KindInvalidArgument, and no
// provider work is performed.
//
// This proves the invariant: "the gateway never fabricates a trusted
// result from incomplete input." A missing identity is caught before a
// vendor is ever called.
func TestServiceComplete_ShipmentEvidenceFixture_MissingShipmentID(t *testing.T) {
	fixture := loadShipmentFixture(t)
	fixture.ShipmentID = "" // simulate broken fixture / missing identity

	cmd := fixtureToCommand(fixture)

	// Track provider invocations to prove no vendor spend occurred.
	fake := provider.NewFakeProvider("fixture-fake", "model-fixture", validFixtureProviderResponse)
	svc := setupFixtureService(t, fake)

	result, metadata, err := svc.Complete(context.Background(), cmd)
	if err == nil {
		t.Fatal("Complete() error = nil, want preflight validation error")
	}

	// ---- Typed error assertion ---------------------------------------
	// The Service wraps preflight failures as *domain.DomainError so
	// callers can branch on Kind without parsing strings.
	var domainErr *domain.DomainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("error = %v (%T), want *domain.DomainError", err, err)
	}
	if domainErr.Kind != domain.KindInvalidArgument {
		t.Fatalf("error.Kind = %q, want %q", domainErr.Kind, domain.KindInvalidArgument)
	}

	// ---- No trusted result fabricated -------------------------------
	if result != nil {
		t.Fatalf("result = %v, want nil (no trusted result on failure)", result)
	}

	// ---- Failure metadata is still populated ------------------------
	// Even on preflight rejection, the Service must set status fields so
	// dashboards can classify the failure.
	if metadata.Status != domain.ExecutionPreflightFailed {
		t.Fatalf("metadata.Status = %q, want %q", metadata.Status, domain.ExecutionPreflightFailed)
	}
	if metadata.ErrorKind != domain.KindInvalidArgument {
		t.Fatalf("metadata.ErrorKind = %q, want %q", metadata.ErrorKind, domain.KindInvalidArgument)
	}

	// ---- Billing invariant: no provider call, no tokens spent -------
	if fake.Calls != 0 {
		t.Fatalf("provider calls = %d, want 0 (preflight rejection must not call provider)", fake.Calls)
	}
}
