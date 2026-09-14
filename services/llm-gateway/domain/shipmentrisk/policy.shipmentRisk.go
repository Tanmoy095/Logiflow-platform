package shipmentrisk

import (
	"fmt"
	"math"
	"strings"
)

// Policy contains only rules that express shipment-risk business truth.
// Provider, transport and infrastructure concerns deliberately do not appear.
type Policy struct{}

// Validate verifies the final candidate before the application is allowed to
// expose it as a trusted business result. We reject invalid model output rather
// than silently repairing it because silent repair hides model degradation.
func (Policy) Validate(result Result) error {
	if strings.TrimSpace(result.ShipmentID) == "" {
		return fmt.Errorf("shipment_id must not be empty")
	}

	switch result.Risk {
	case RiskNoRisk, RiskMediumRisk, RiskHighRisk:
		// Supported classification.
	default:
		return fmt.Errorf("invalid risk value: %q", result.Risk)
	}

	if math.IsNaN(result.Confidence) || math.IsInf(result.Confidence, 0) {
		return fmt.Errorf("confidence must be a finite number")
	}
	if result.Confidence < 0 || result.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	if result.Risk == RiskHighRisk && len(result.Reasons) == 0 {
		return fmt.Errorf("high_risk requires at least one reason")
	}

	for i, reason := range result.Reasons {
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("reasons[%d] must not be empty", i)
		}
	}

	return nil
}
