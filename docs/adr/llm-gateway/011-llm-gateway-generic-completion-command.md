# ADR-011 — Generic Governed Completion Command and Capability Dispatch

## Status

Accepted

## Decision

Use one transport-neutral `application.CompleteCommand` as the stable application entry point for controlled AI execution.

Resolve `CompleteCommand.TaskType` through an application-owned `TaskRegistry` into a capability-specific `TaskDefinition`.

The command carries the shared execution contract:

- `contract_version`
- `tenant_id`
- `request_id`
- `idempotency_key`
- `task_type`
- `task_schema_version`
- `prompt`
- `prompt_version`
- `output_schema_version`
- `max_tokens`
- `deadline`
- `subjects`
- explicit `evidence_id` references

Capability-specific requirements stay in the resolved `TaskDefinition`.

The generic command is deliberately **not** forwarded to the provider. Each
capability narrows it into a smaller vendor-facing contract:

```go
type ProviderRequest struct {
       TaskType            domain.TaskType
       Prompt              string
       PromptVersion       string
       OutputSchemaVersion string
       MaxTokens            int
}
```

Tenant identity, request identity, idempotency key, evidence references, and
business subjects are application concerns and are never sent to a vendor.
This narrowing is a privacy boundary, not merely a convenience.

## TaskDefinition as the capability seam

Every capability implements the application-owned interface:

```go
type TaskDefinition interface {
       TaskType() domain.TaskType
       TaskSchemaVersion() string
       OutputSchemaVersion() string
       ValidateCommand(CompleteCommand) error
       BuildProviderRequest(CompleteCommand) ProviderRequest
       DecodeAndValidate(context.Context, CompleteCommand, string) (TaskResult, error)
}
```

The task definition owns capability-specific validation, provider-request
construction, and trust-boundary decoding. Adding a capability does not
require editing `Service.Complete`; the orchestration algorithm is
capability-agnostic by construction.

## Business need

LogiFlow will eventually have multiple AI operations: shipment delay risk, customs analysis, invoice extraction, document analysis, and future agent/MCP tools.

A separate command type for every capability would force every transport adapter and application service to grow with the product.

The system therefore needs:

```text
many capabilities
       ↓
one governed application boundary
       ↓
TaskType + version
       ↓
specific TaskDefinition
```

This matches the Module 3 requirement to use a small generic command with controlled task/schema semantics.

## Alternatives considered

### A. One command type per capability

Example:

```text
ShipmentRiskCommand
CustomsSummaryCommand
InvoiceExtractionCommand
```

Rejected for the first gateway foundation because application and transport contracts would grow linearly with capability count.

### B. Arbitrary `POST /chat`

Rejected because the gateway would not know which schema, policy, budget, evaluation or business rules apply.

The architecture explicitly rejects a fully arbitrary prompt gateway.

### C. Large central switch

Rejected because every new capability changes the same orchestration file and couples unrelated business rules. The tracking document identifies this as the giant-switch anti-pattern.

## Trade-offs

### Benefits

- stable transport/application seam;
- capability extensibility;
- centralized governance;
- fewer transport contracts;
- easier testing;
- clean future gRPC/MCP adapters.

### Costs

- generic command contains fields unused by some capabilities;
- task definitions need capability-specific validation;
- registry correctness becomes a startup concern;
- generic contracts can become too broad if allowed to grow without discipline.

## Consequences

Adding a new meaningful capability should normally require:

```text
domain/<capability>/model.go
domain/<capability>/policy.go
application/<capability>_task.go
registry registration
tests
```

and should not require changing the orchestration algorithm in `service.go`.

## Evidence

The implementation tracking records the generic completion command, TaskRegistry, explicit identity references and capability adapters as completed Module 3 work.

## Interview question

> Why did you choose a generic command instead of typed commands?

Expected answer:

> I wanted one stable application boundary for multiple AI capabilities. Type safety is preserved by controlling `TaskType`, schema versions, registry resolution, capability validation and trusted result types rather than creating a new transport command for every feature.
