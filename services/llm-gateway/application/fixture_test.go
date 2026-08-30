// services/llm-gateway/application/fixture_test.go

package application_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/application"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/infrastructure/provider"
)

// shipmentEvidenceFixture mirrors the structure of the test fixture JSON.
// It exists only in test code to load external representation.
// It is NOT a domain type; domain.Request is the normalized application input.
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

// loadShipmentFixture loads the deterministic shipment evidence fixture
// from the stream-ingestion testdata directory.
//
// NOTE: The path is relative to the package directory. For a more robust
// CI setup, this could be resolved using runtime.Caller or a configurable
// fixture root, but that is intentionally deferred for now.
func loadShipmentFixture(t *testing.T) shipmentEvidenceFixture {
	t.Helper()

	path := filepath.Join(
		"..",
		"..",
		"stream-ingestion",
		"testdata",
		"shipment_evidence_001.json",
	)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var fixture shipmentEvidenceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	return fixture
}

// fixtureToRequest is an Adapter: it translates the test fixture
// representation into the domain.Request shape expected by the application.
// This keeps domain/application code decoupled from the fixture format.
func fixtureToRequest(f shipmentEvidenceFixture) domain.Request {
	return domain.Request{
		ShipmentID:    f.ShipmentID,
		Prompt:        buildRiskPrompt(f),
		PromptVersion: "v1",
	}
}

// buildRiskPrompt constructs a deterministic prompt from shipment evidence.
// In a production system this would be a proper prompt builder/registry.
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

// validFixtureProviderResponse is a deterministic fake provider response
// that matches the fixture's shipment_id and passes validation.
const validFixtureProviderResponse = `{
	"shipment_id": "ship-123",
	"risk": "high_risk",
	"confidence": 0.94,
	"reasons": [
		"Customs clearance delay detected",
		"Expected delivery date has been exceeded"
	]
}`

// TestServiceComplete_ShipmentEvidenceFixture verifies that a realistic
// shipment evidence fixture can be mapped to a request, passed through the
// gateway, and produce a trusted result with correct identity.
func TestServiceComplete_ShipmentEvidenceFixture(t *testing.T) {
	fixture := loadShipmentFixture(t)

	req := fixtureToRequest(fixture)

	// Explicitly confirm that the adapter preserves shipment identity.
	if req.ShipmentID != fixture.ShipmentID {
		t.Fatalf(
			"adapter changed shipment identity: fixture = %q, request = %q",
			fixture.ShipmentID,
			req.ShipmentID,
		)
	}

	fake := provider.NewFakeProvider(validFixtureProviderResponse)
	service := application.NewService(fake)

	result, err := service.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() returned unexpected error: %v", err)
	}

	if result.ShipmentID != fixture.ShipmentID {
		t.Fatalf("result shipment_id = %q, want %q", result.ShipmentID, fixture.ShipmentID)
	}

	if result.Risk != domain.RiskHighRisk {
		t.Fatalf("result risk = %q, want %q", result.Risk, domain.RiskHighRisk)
	}

	if result.Confidence <= 0 || result.Confidence > 1 {
		t.Fatalf("result confidence = %v, outside [0,1]", result.Confidence)
	}

	if len(result.Reasons) == 0 {
		t.Fatal("expected at least one reason")
	}
}

// TestServiceComplete_ShipmentEvidenceFixture_MissingShipmentID verifies that
// a missing shipment identity is rejected at the application boundary
// and no trusted result is fabricated.
func TestServiceComplete_ShipmentEvidenceFixture_MissingShipmentID(t *testing.T) {
	fixture := loadShipmentFixture(t)
	fixture.ShipmentID = "" // simulate broken fixture / missing identity

	req := fixtureToRequest(fixture)

	fake := provider.NewFakeProvider(validFixtureProviderResponse)
	service := application.NewService(fake)

	result, err := service.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("Complete() error = nil, want request validation error")
	}

	assertNoTrustedResult(t, result)
}
