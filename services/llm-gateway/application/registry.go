package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain"
	"github.com/Tanmoy095/LogiFlow-Platform/services/llm-gateway/domain/shipmentrisk"
)

// Registry is a container for all the application's dependencies.
// TaskResult is the common read-only shape returned by the task registry.
type TaskResult interface {
	TaskType() domain.TaskType
}

//TaskDefinition is the application-side capability adapter.
//
// It connects the generic CompleteCommand to a specific domain capability while
// avoiding a giant switch in Service.Complete. Adding a future capability means
// registering another TaskDefinition rather than editing the core use case.

type TaskDefinition interface {
	TaskType() domain.TaskType
	TaskSchemaVersion() string
	OutputSchemaVersion() string
	ValidateCommand(CompleteCommand) error
	BuildProviderRequest(CompleteCommand) ProviderRequest
	DecodeAndValidate(context.Context, CompleteCommand, string) (TaskResult, error)
}

type TaskRegistry struct {
	definitions map[domain.TaskType]TaskDefinition
}

func NewTaskRegistry(definitions ...TaskDefinition) (*TaskRegistry, error) {

	definitionsByType := make(map[domain.TaskType]TaskDefinition, len(definitions))
	for _, definition := range definitions {
		if definition == nil {
			return nil, fmt.Errorf("task registry cannot contain nil definition")
		}
		taskType := definition.TaskType()
		if strings.TrimSpace(string(taskType)) == "" {
			return nil, fmt.Errorf("task definition has empty task type")
		}
		if _, exists := definitionsByType[taskType]; exists {
			return nil, fmt.Errorf("duplicate task definition: %q", taskType)
		}
		definitionsByType[taskType] = definition

	}
	// Initialize the task registry with the provided definitions, eg.shipment risk task definition.
	return &TaskRegistry{definitions: definitionsByType}, nil
}

// Resolve returns the TaskDefinition for the given taskType or an error if not found.
func (r *TaskRegistry) Resolve(taskType domain.TaskType) (TaskDefinition, error) {
	definition, ok := r.definitions[taskType]
	if !ok {
		return nil, domain.NewUnsupportedTaskError(fmt.Sprintf("unsupported task_type: %q", taskType))
	}
	return definition, nil
}

// ShipmentRiskTaskDefinition translates the generic gateway contract into the
// shipment-risk capability. It intentionally lives in application because it is
// orchestration/translation code; the actual shipment business invariants live
// inside domain/shipmentrisk.
type ShipmentRiskTaskDefinition struct {
	policy shipmentrisk.Policy
}

func NewShipmentRiskTaskDefinition() ShipmentRiskTaskDefinition {
	return ShipmentRiskTaskDefinition{policy: shipmentrisk.Policy{}}
}

func (ShipmentRiskTaskDefinition) TaskType() domain.TaskType {
	return domain.TaskShipmentDelayRisk
}

func (ShipmentRiskTaskDefinition) TaskSchemaVersion() string {
	return shipmentrisk.TaskSchemaVersion
}

func (ShipmentRiskTaskDefinition) OutputSchemaVersion() string {
	return shipmentrisk.ResultSchemaVersion
}

func (t ShipmentRiskTaskDefinition) ValidateCommand(cmd CompleteCommand) error {
	if cmd.TaskSchemaVersion != t.TaskSchemaVersion() {
		return fmt.Errorf("task_schema_version %q does not match supported version %q", cmd.TaskSchemaVersion, t.TaskSchemaVersion())
	}
	if cmd.OutputSchemaVersion != t.OutputSchemaVersion() {
		return fmt.Errorf("output_schema_version %q does not match supported version %q", cmd.OutputSchemaVersion, t.OutputSchemaVersion())
	}
	if len(cmd.Subjects) != 1 {
		return fmt.Errorf("shipment_delay_risk requires exactly one subject")
	}
	if cmd.Subjects[0].Type != domain.EntityShipment {
		return fmt.Errorf("shipment_delay_risk requires a shipment subject")
	}
	return nil
}

func (ShipmentRiskTaskDefinition) BuildProviderRequest(cmd CompleteCommand) ProviderRequest {
	return ProviderRequest{
		TaskType:            cmd.TaskType,
		Prompt:              cmd.Prompt,
		PromptVersion:       cmd.PromptVersion,
		OutputSchemaVersion: cmd.OutputSchemaVersion,
		MaxTokens:           cmd.MaxTokens,
	}
}

type shipmentRiskProviderResponse struct {
	ShipmentID *string   `json:"shipment_id"`
	Risk       *string   `json:"risk"`
	Confidence *float64  `json:"confidence"`
	Reasons    *[]string `json:"reasons"`
}

func (t ShipmentRiskTaskDefinition) DecodeAndValidate(_ context.Context, cmd CompleteCommand, raw string) (TaskResult, error) {
	// DisallowUnknownFields prevents a provider from silently returning a shape
	// that is broader or different from the contract the caller requested.
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()

	var candidate shipmentRiskProviderResponse
	if err := decoder.Decode(&candidate); err != nil {
		return nil, fmt.Errorf("syntax validation failed: %w", err)
	}

	// Reject concatenated JSON documents such as `{...}{...}` rather than
	// accepting the first value and ignoring trailing provider output.
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return nil, fmt.Errorf("syntax validation failed: multiple JSON values returned")
	}

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

	result := shipmentrisk.ShipmentRiskResult{
		ShipmentID: *candidate.ShipmentID,
		Risk:       shipmentrisk.Risk(*candidate.Risk),
		Confidence: *candidate.Confidence,
		Reasons:    *candidate.Reasons,
	}

	// Domain validation must also ensure the provider answered about the same
	// business subject the request asked us to analyze. This is stronger than
	// merely checking that shipment_id is non-empty.
	requestedShipmentID := cmd.Subjects[0].ID
	if result.ShipmentID != requestedShipmentID {
		return nil, fmt.Errorf("domain validation failed: provider shipment_id %q does not match requested shipment %q", result.ShipmentID, requestedShipmentID)
	}

	if err := t.policy.Validate(result); err != nil {
		return nil, fmt.Errorf("domain validation failed: %w", err)
	}

	return result, nil
}
