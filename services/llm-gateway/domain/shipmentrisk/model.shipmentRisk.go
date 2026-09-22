package shipmentrisk

import (
	"fmt"
	"math"
	"strings"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
)

// Schema version constants identify the wire contract for this capability.
//
//	TaskSchemaVersion   — the shape of the input the gateway will accept
//	ResultSchemaVersion — the shape of the trusted result the gateway emits
//
// Both are versioned independently so input and output contracts can evolve
// on their own timeline.
const (
	TaskSchemaVersion   = "shipment_delay_risk.v1"
	ResultSchemaVersion = "shipment_delay_risk.result.v1"
)

// Risk is a closed business classification.
//
// The gateway never lets a raw vendor string become business truth. Provider
// output is decoded into this controlled enum and then validated. If a vendor
// invents a new category, it is rejected — it cannot silently become a new
// business meaning.
type Risk string

const (
	RiskNoRisk     Risk = "no_risk"
	RiskMediumRisk Risk = "medium_risk"
	RiskHighRisk   Risk = "high_risk"
)

// ShipmentRiskResult is the trusted shipment-risk result.
//
// "Trusted" means: this object has passed both its own Validate() and the
// capability policy. A provider adapter must never construct it as a trusted
// result by itself — the application layer owns the transition from
// untrusted provider output to trusted domain result.
type ShipmentRiskResult struct {
	ShipmentID string   `json:"shipment_id"`
	Risk       Risk     `json:"risk"`
	Confidence float64  `json:"confidence"`
	Reasons    []string `json:"reasons"`
}

// TaskType lets generic application orchestration identify which capability
// produced this result without importing shipment-specific fields.
//
// Whenever the application asks a ShipmentRiskResult: "what kind of AI task
// created you?", it always answers: shipment_delay_risk.
func (r ShipmentRiskResult) TaskType() domain.TaskType {
	return domain.TaskShipmentDelayRisk
}

// Validate enforces only the intrinsic invariants of a shipment-risk result.
//
// Deliberately out of scope here:
//   - Cross-object correlation ("does ShipmentID match what was requested?")
//     — that requires the application command and lives in the capability
//     adapter one layer up.
//   - Transport concerns, retries, provider identity, Kafka delivery.
//
// Rules enforced:
//   - ShipmentID must be non-blank.
//   - Risk must be one of the defined enum values.
//   - Confidence must be a finite number in [0, 1].
//   - High risk must carry at least one reason (business requirement).
//   - No reason may be blank.
func (r ShipmentRiskResult) Validate() error {
	if strings.TrimSpace(r.ShipmentID) == "" {
		return fmt.Errorf("shipment_id must not be empty")
	}

	switch r.Risk {
	case RiskNoRisk, RiskMediumRisk, RiskHighRisk:
		// Supported classification.
	default:
		return fmt.Errorf("invalid risk value %q", r.Risk)
	}

	// Reject NaN and Inf before the range check, because NaN compares false
	// against every bound and would silently pass <0 || >1 checks on some
	// runtimes.
	if math.IsNaN(r.Confidence) || math.IsInf(r.Confidence, 0) {
		return fmt.Errorf("confidence must be finite")
	}
	if r.Confidence < 0 || r.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}

	// High risk without a reason is not actionable for downstream consumers,
	// so it is a contract violation, not a nicety.
	if r.Risk == RiskHighRisk && len(r.Reasons) == 0 {
		return fmt.Errorf("high_risk requires at least one reason")
	}

	for i, reason := range r.Reasons {
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("reasons[%d] must not be blank", i)
		}
	}

	return nil
}
