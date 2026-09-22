package shipmentrisk

import "fmt"

// Policy is deliberately stateless.
//
// Keeping capability-specific policy in its own package means future
// capabilities can introduce different business invariants without touching
// the generic LLM Gateway domain.
type Policy struct{}

// Validate verifies that a candidate result is a legitimate shipment-risk
// business result.
//
// It first delegates to the result's own Validate(), which covers all
// intrinsic invariants. Anything added here in the future should be a rule
// that needs context beyond the result itself — for example:
//
//   - "returned ShipmentID must match the one in the request command"
//   - "confidence must be below X for tenants on the conservative tier"
//   - "reasons must reference evidence IDs the tenant actually owns"
//
// Those rules require the application command (or tenant context), so they
// stay in a layer that has access to it. This package only sees the result.
func (Policy) Validate(result ShipmentRiskResult) error {
	if err := result.Validate(); err != nil {
		return fmt.Errorf("shipment risk policy violation: %w", err)
	}
	return nil
}
