package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain/shipmentrisk"
)

// TaskResult is the polymorphic interface implemented by all domain result
// models.
//
// It lets service.go handle domain results without knowing their underlying
// concrete types, and gives each capability result a way to identify which
// AI task produced it.
type TaskResult interface {
	TaskType() domain.TaskType
}

// TaskDefinition is the capability adapter interface.
//
// Each AI task (Shipment Risk, Port Risk, Customs Summary) implements this
// interface to plug directly into the gateway's execution pipeline. It owns
// the translation between the generic application contract and one governed
// AI capability.
type TaskDefinition interface {
	TaskType() domain.TaskType
	TaskSchemaVersion() string
	OutputSchemaVersion() string
	ValidateCommand(CompleteCommand) error
	BuildProviderRequest(CompleteCommand) ProviderRequest
	DecodeAndValidate(context.Context, CompleteCommand, string) (TaskResult, error)
}

// TaskRegistry manages the map of supported AI capabilities. It is populated
// once at application startup and then read-only for the lifetime of the
// process, making it safe to share across goroutines without locking.
type TaskRegistry struct {
	definitions map[domain.TaskType]TaskDefinition
}

// NewTaskRegistry builds the O(1) lookup table of capabilities at server boot.
//
// It enforces two startup invariants:
//
//   - No nil definitions (a nil would panic on first Resolve).
//   - No duplicate TaskTypes (a duplicate would silently shadow an earlier
//     registration, which is almost always a wiring bug).
//
// The registry must contain at least one definition; an empty registry
// indicates the composition root forgot to wire capabilities.
func NewTaskRegistry(definitions ...TaskDefinition) (*TaskRegistry, error) {
	definitionsByType := make(map[domain.TaskType]TaskDefinition, len(definitions))

	for i, definition := range definitions {
		if definition == nil {
			return nil, fmt.Errorf("task definition %d must not be nil", i)
		}

		taskType := definition.TaskType()
		if strings.TrimSpace(string(taskType)) == "" {
			return nil, fmt.Errorf("task definition %d has empty task type", i)
		}

		if _, exists := definitionsByType[taskType]; exists {
			return nil, fmt.Errorf("duplicate task registration: %q", taskType)
		}

		definitionsByType[taskType] = definition
	}

	if len(definitionsByType) == 0 {
		return nil, fmt.Errorf("task registry requires at least one definition")
	}

	return &TaskRegistry{definitions: definitionsByType}, nil
}

// Resolve looks up the requested task handler at runtime during an active API
// call. Unknown task types are returned as a domain error so the interface
// layer can map them to an appropriate transport response without knowing
// the registry implementation.
func (r *TaskRegistry) Resolve(taskType domain.TaskType) (TaskDefinition, error) {
	definition, ok := r.definitions[taskType]
	if !ok {
		return nil, domain.NewUnsupportedTaskError(
			fmt.Sprintf("unsupported task_type %q", taskType),
		)
	}
	return definition, nil
}

// -----------------------------------------------------------------------------
// Shipment Risk capability adapter
// -----------------------------------------------------------------------------

// ShipmentRiskTaskDefinition translates the generic gateway contract into the
// shipment-risk capability.
//
// It intentionally lives in application (not domain) because it is
// orchestration and translation code. The actual shipment business invariants
// live inside domain/shipmentrisk. Keeping them separate lets the domain
// package stay free of HTTP, JSON, and provider concerns.
type ShipmentRiskTaskDefinition struct {
	policy shipmentrisk.Policy
}

// NewShipmentRiskTaskDefinition constructs the adapter with its stateless
// policy. The policy has no fields today, but keeping it as an explicit
// dependency leaves room to inject policy configuration later without
// changing this signature.
func NewShipmentRiskTaskDefinition() ShipmentRiskTaskDefinition {
	return ShipmentRiskTaskDefinition{
		policy: shipmentrisk.Policy{},
	}
}

func (t ShipmentRiskTaskDefinition) TaskType() domain.TaskType {
	return domain.TaskShipmentDelayRisk
}

func (t ShipmentRiskTaskDefinition) TaskSchemaVersion() string {
	return shipmentrisk.TaskSchemaVersion
}

func (t ShipmentRiskTaskDefinition) OutputSchemaVersion() string {
	return shipmentrisk.ResultSchemaVersion
}

// ValidateCommand ensures incoming request parameters meet shipment-risk
// business rules before any provider call happens.
//
// This is the capability-specific half of the validation contract: the
// generic shape check lives in CompleteCommand.Validate; this method enforces
// what "a valid shipment risk request" specifically means.
func (t ShipmentRiskTaskDefinition) ValidateCommand(cmd CompleteCommand) error {
	if cmd.TaskSchemaVersion != t.TaskSchemaVersion() {
		return fmt.Errorf(
			"task_schema_version %q does not match supported version %q",
			cmd.TaskSchemaVersion,
			t.TaskSchemaVersion(),
		)
	}
	if cmd.OutputSchemaVersion != t.OutputSchemaVersion() {
		return fmt.Errorf(
			"output_schema_version %q does not match supported version %q",
			cmd.OutputSchemaVersion,
			t.OutputSchemaVersion(),
		)
	}

	// Today this capability analyzes exactly one shipment. The generic
	// command remains flexible so a future capability can accept zero, one,
	// or many subjects — only this adapter cares that shipment-risk needs
	// exactly one.
	if len(cmd.Subjects) != 1 {
		return fmt.Errorf("shipment_delay_risk requires exactly one shipment subject")
	}
	if cmd.Subjects[0].Type != domain.EntityShipment {
		return fmt.Errorf("shipment_delay_risk requires a shipment subject")
	}

	return nil
}

// BuildProviderRequest isolates prompt and version details for vendor
// transmission. Nothing tenant- or business-specific leaks through this
// seam — the vendor sees only what it needs to produce an answer.
func (ShipmentRiskTaskDefinition) BuildProviderRequest(cmd CompleteCommand) ProviderRequest {
	return ProviderRequest{
		TaskType:            cmd.TaskType,
		Prompt:              cmd.Prompt,
		PromptVersion:       cmd.PromptVersion,
		OutputSchemaVersion: cmd.OutputSchemaVersion,
		MaxTokens:           cmd.MaxTokens,
	}
}

// shipmentRiskProviderResponse is an anti-corruption DTO used exclusively for
// parsing raw JSON from the provider.
//
// Pointer fields are deliberate: they distinguish between an omitted JSON
// field (nil) and a present zero value ("" or 0.0). For example, missing
// confidence and confidence=0.0 are different states and must not be silently
// collapsed by encoding/json.
type shipmentRiskProviderResponse struct {
	ShipmentID *string   `json:"shipment_id"`
	Risk       *string   `json:"risk"`
	Confidence *float64  `json:"confidence"`
	Reasons    *[]string `json:"reasons"`
}

// DecodeAndValidate executes the complete three-stage validation pipeline
// inside the capability definition:
//
//	Stage 1 — Syntax: strict JSON parsing, no unknown fields, no trailing data
//	Stage 2 — Schema: mandatory fields must be present (not just zero-valued)
//	Stage 3 — Domain: identity correlation + capability business invariants
//
// The order matters: each stage is cheaper than the next, so we fail on the
// earliest possible signal instead of paying for validation we'd do anyway.
func (t ShipmentRiskTaskDefinition) DecodeAndValidate(
	_ context.Context,
	cmd CompleteCommand,
	raw string,
) (TaskResult, error) {
	// -------------------------------------------------------------------------
	// STAGE 1: Syntax validation & strict JSON parsing
	// -------------------------------------------------------------------------
	decoder := json.NewDecoder(strings.NewReader(raw))

	// Rejects unknown JSON keys returned by hallucinating LLMs. If the model
	// invents a field we don't expect, we want to see the failure instead of
	// silently ignoring it.
	decoder.DisallowUnknownFields()

	var candidate shipmentRiskProviderResponse
	if err := decoder.Decode(&candidate); err != nil {
		return nil, fmt.Errorf("syntax validation failed: %w", err)
	}

	// Rejects concatenated JSON streams (e.g. `{...}{...}`) instead of
	// silently accepting only the first object.
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return nil, fmt.Errorf("syntax validation failed: multiple JSON values returned")
	}

	// -------------------------------------------------------------------------
	// STAGE 2: Schema validation (nullability & mandatory fields)
	// -------------------------------------------------------------------------
	if candidate.ShipmentID == nil {
		return nil, fmt.Errorf("schema validation failed: shipment_id is required")
	}
	if candidate.Risk == nil {
		return nil, fmt.Errorf("schema validation failed: risk is required")
	}
	if candidate.Confidence == nil {
		return nil, fmt.Errorf("schema validation failed: confidence is required")
	}
	if candidate.Reasons == nil {
		return nil, fmt.Errorf("schema validation failed: reasons is required")
	}

	// Map the untrusted DTO to the candidate domain model. From this point on,
	// every field is a plain value — no more nil checks needed.
	result := shipmentrisk.ShipmentRiskResult{
		ShipmentID: *candidate.ShipmentID,
		Risk:       shipmentrisk.Risk(*candidate.Risk),
		Confidence: *candidate.Confidence,
		Reasons:    *candidate.Reasons,
	}

	// -------------------------------------------------------------------------
	// STAGE 3a: Identity correlation
	// -------------------------------------------------------------------------
	// JSON can be perfectly valid while still describing the wrong shipment.
	// This check prevents a semantically valid answer for SH-999 from being
	// accidentally accepted for a request about SH-123.
	//
	// It lives here (not in domain/shipmentrisk) because the domain result
	// does not know what was originally requested — only the application
	// command knows that.
	requestedShipmentID := cmd.Subjects[0].ID
	if result.ShipmentID != requestedShipmentID {
		return nil, fmt.Errorf(
			"domain validation failed: provider shipment_id %q does not match requested shipment %q",
			result.ShipmentID,
			requestedShipmentID,
		)
	}

	// -------------------------------------------------------------------------
	// STAGE 3b: Capability domain policy
	// -------------------------------------------------------------------------
	// Delegates to domain/shipmentrisk.Policy, which enforces the business
	// truth of the result: valid Risk enum, confidence range, high_risk
	// requires reasons, and so on.
	if err := t.policy.Validate(result); err != nil {
		return nil, fmt.Errorf("domain validation failed: %w", err)
	}

	// Output is now fully validated and trusted. The application layer may
	// safely expose it as a business result.
	return result, nil
}
