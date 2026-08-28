package domain

import "fmt"

/**
What business concepts must exist even if OpenAI disappears tomorrow?

Answer:the Business domains of llm-gateway are:

Request
CompletionResult
Risk
confidence
shipment identity
reasons

domain owns:

shipment identity
risk validity
confidence validity
reasons requirement

*/

type Risk string

const (
	RiskNoRisk     Risk = "no_risk"
	RiskMediumRisk Risk = "medium_risk"
	RiskHighRisk   Risk = "high_risk"
)

type Request struct {
	ShipmentID string
	Prompt     string
}

type CompletionResult struct {
	ShipmentID string   `json:"shipment_id"`
	Risk       Risk     `json:"risk"`
	Confidence float64  `json:"confidence"`
	Reasons    []string `json:"reasons"`
}

func (r *Request) Validate() error {
	if r.ShipmentID == "" {
		return fmt.Errorf("shipment_id must not be empty")
	}

	if r.Prompt == "" {
		return fmt.Errorf("prompt must not be empty")
	}

	return nil
}

func (c *CompletionResult) Validate() error {

	if c.ShipmentID == "" {
		return fmt.Errorf("shipment_id must not be empty")
	}

	switch c.Risk {
	case RiskNoRisk, RiskMediumRisk, RiskHighRisk:
		// valid risk values
	default:
		return fmt.Errorf("invalid risk value: %s", c.Risk)
	}

	if c.Confidence < 0 || c.Confidence > 1 {
		return fmt.Errorf(
			"confidence must be between 0 and 1",
		)
	}

	if len(c.Reasons) == 0 {
		return fmt.Errorf("reasons must not be empty")
	}

	return nil

}
