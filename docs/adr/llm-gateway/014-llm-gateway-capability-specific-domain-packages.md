# ADR-014 — Capability-Specific Domain Packages

## Status

Accepted

## Decision

Keep business models and policies that belong to a specific AI capability under:

```text
domain/<capability>/
```

Current implementation:

```text
domain/shipmentrisk/
   model.go      — ShipmentRiskResult, Risk enum, TaskSchemaVersion, ResultSchemaVersion
   policy.go     — Policy.Validate(ShipmentRiskResult) error
```

Future capabilities may add:

```text
domain/customssummary/
domain/invoiceextraction/
domain/containerdamagedetection/
```

only when those capabilities contain meaningful business invariants.

## Business need

The gateway is a platform boundary, not a shipment-risk service.

Shipment risk is the first implemented governed capability. Its rules include:

```text
risk enum
confidence range
high-risk explanation requirement
shipment identity correlation
```

Those rules should not pollute a generic root domain model.

The tracking document explicitly establishes the two-file capability pattern and future examples.

## Why the root domain remains small

Root domain:

```text
TaskType
EntityReference
EvidenceReference
ExecutionPolicy
AIUsageEvent
typed errors
```

Capability domain:

```text
Risk
Confidence
Reasons
ShipmentRiskResult
shipment-risk policy
```

This makes business ownership obvious.

## Validation layering

Intrinsic invariants live on the result type itself:

```text
ShipmentRiskResult.Validate() error
   - shipment_id non-blank
   - risk is one of the enum values
   - confidence finite, in [0, 1]
   - high_risk requires at least one reason
   - no reason is blank
```

Cross-object rules live in the capability policy:

```text
shipmentrisk.Policy.Validate(ShipmentRiskResult) error
   - currently delegates to ShipmentRiskResult.Validate()
   - extension point for rules needing context beyond the result
```

Rules requiring the application command, such as verifying that the returned
`shipment_id` matches the requested subject, belong one layer up in the
capability adapter's `DecodeAndValidate`, because only that layer has access
to the command.

## Alternatives considered

### Put every capability in `domain/model.go`

Rejected because the root package becomes a large mixed-purpose file.

### One domain package per transport

Rejected because business meaning should not depend on HTTP/gRPC/MCP.

### No domain capability package

Rejected because important business invariants would end up in application or adapters.

## Trade-offs

### Benefits

- strong bounded-context boundaries;
- easier independent testing;
- low coupling between AI capabilities;
- clear onboarding path for future teams.

### Costs

- more packages;
- capability developers must understand application registration;
- some small capabilities may not deserve a full domain package.

## Important DDD rule

DDD does **not** mean every prompt gets a domain model.

Use a capability domain package when there are business truths that must always hold.

A generic text rewrite operation may need only application-level validation.

A financial invoice extraction capability has mathematical invariants and therefore deserves a domain model/policy.

This principle is explicitly documented in the architectural Q&A.

## Consequences

Adding a capability should not require rewriting the provider abstraction or core orchestration.

The intended flow is:

```text
CompleteCommand
   ↓
TaskRegistry
   ↓
Capability TaskDefinition
   ↓
capability decoder
   ↓
capability domain policy
   ↓
trusted capability result
```

## Interview question

> Why does shipment risk have its own domain package?

Expected answer:

> Because risk, confidence and explainability are business semantics. Keeping them isolated prevents the generic gateway domain from becoming coupled to one current logistics use case and gives future capabilities an explicit place for their own invariants.
