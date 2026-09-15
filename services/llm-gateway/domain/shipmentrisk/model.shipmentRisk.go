package shipmentrisk

import "github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"

// TaskType is kept in the shared domain package; this capability owns the
// business meaning of the result returned for that task.
const TaskSchemaVersion = "shipment_delay_risk.v1"
const ResultSchemaVersion = "shipment_delay_risk.result.v1"

// Risk is a controlled business classification. The provider cannot introduce
// a new risk level without an explicit domain change and corresponding tests.
type Risk string

const (
	RiskNoRisk     Risk = "no_risk"
	RiskMediumRisk Risk = "medium_risk"
	RiskHighRisk   Risk = "high_risk"
)

// Result is trusted only after Policy.Validate succeeds. It should never be
// constructed and returned directly from a provider adapter.
type ShipmentRiskResult struct {
	ShipmentID string   `json:"shipment_id"`
	Risk       Risk     `json:"risk"`
	Confidence float64  `json:"confidence"`
	Reasons    []string `json:"reasons"`
}

// Whenever anyone asks a shipmentrisk.Result object: 'Hey, what kind of AI task created you?',
// it will always answer: 'I was created by shipment_delay_risk'."

func (r ShipmentRiskResult) TaskType() domain.TaskType { return domain.TaskShipmentDelayRisk }
