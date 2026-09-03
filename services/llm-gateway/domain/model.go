// 	services/llm-gateway/domain/model.go

package domain

import (
	"fmt"
	"math"
)

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

// Risk represents the business classification assigned to a shipment.
type Risk string

const (
	RiskNoRisk     Risk = "no_risk"
	RiskMediumRisk Risk = "medium_risk"
	RiskHighRisk   Risk = "high_risk"
)

// Request represents the business request to perform a controlled
// AI completion for a specific shipment.
type Request struct {
	ShipmentID    string
	Prompt        string
	PromptVersion string // Version of the prompt template (behavior contract).
}

// CompletionResult is the trusted representation of an AI completion.
type CompletionResult struct {
	ShipmentID string   `json:"shipment_id"`
	Risk       Risk     `json:"risk"`
	Confidence float64  `json:"confidence"`
	Reasons    []string `json:"reasons"`
}

func (r Request) Validate() error {
	if r.ShipmentID == "" {
		return fmt.Errorf("shipment_id must not be empty")
	}

	if r.Prompt == "" {
		return fmt.Errorf("prompt must not be empty")
	}

	if r.PromptVersion == "" {
		return fmt.Errorf("prompt_version must not be empty")
	}

	return nil
}

func (c CompletionResult) Validate() error {
	if c.ShipmentID == "" {
		return fmt.Errorf(
			"shipment_id must not be empty",
		)
	}

	switch c.Risk {
	case RiskNoRisk, RiskMediumRisk, RiskHighRisk:
		// Supported business value.
	default:
		return fmt.Errorf(
			"invalid risk value: %q",
			c.Risk,
		)
	}

	// Protect the domain invariant independently of the transport/parser.
	//
	// JSON itself does not normally encode NaN or +/-Inf, but this domain
	// object may be constructed from Go code or another future adapter.
	if math.IsNaN(c.Confidence) || math.IsInf(c.Confidence, 0) {
		return fmt.Errorf(
			"confidence must be a finite number",
		)
	}

	if c.Confidence < 0 || c.Confidence > 1 {
		return fmt.Errorf(
			"confidence must be between 0 and 1",
		)
	}

	// Cross-field business invariant:
	//
	// High-risk classifications require evidence explaining why the
	// shipment was classified as high risk.
	if c.Risk == RiskHighRisk && len(c.Reasons) == 0 {
		return fmt.Errorf(
			"high_risk requires at least one reason",
		)
	}

	return nil
}
