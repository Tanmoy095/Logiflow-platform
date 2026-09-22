# ADR-012 — Explicit Identity Vocabulary and Evidence References

## Status

Accepted

## Decision

Never use a generic `ID` field where the lifecycle or ownership meaning matters.

Use explicit names:

```text
tenant_id
evidence_id
shipment_id
request_id
idempotency_key
event_id
```

Use `EntityReference` for business subjects and `EvidenceReference` with explicit `EvidenceID` for durable evidence.

## Type-enforced vocabulary

The vocabulary is enforced by types, not by convention:

```text
domain.EntityReference      { Type EntityType; ID string }
domain.EvidenceReference    { EvidenceID string }
application.CompleteCommand { TenantID, RequestID, IdempotencyKey string }
```

The compiler prevents assigning one identity kind to a field that expects
another. A future integrator cannot accidentally pass a shipment ID as an
evidence ID because these types do not unify.

## Business need

These identifiers do different jobs.

```text
tenant_id
→ security / ownership boundary

evidence_id
→ durable business-memory identity

shipment_id
→ optional business subject identity

request_id
→ one execution/correlation identity

idempotency_key
→ same logical operation

event_id
→ event occurrence identity
```

Conflating them can corrupt audit history, break RAG lookups, or accidentally make shipment identity look like authorization.

The tracking document explicitly records `evidence_id != request_id` and preserves this lifecycle separation.

## Security implication

Do not assume:

```text
shipment_id → tenant authorization
```

Authorization belongs to the authenticated tenant context.

Evidence retrieval must be tenant-filtered before AI context reaches the model.

## Alternatives considered

### Generic `ID`

Rejected because it is easy to assign the wrong identifier to the wrong semantic role.

### Universal `shipment_id` on every AI command

Rejected because many governed AI tasks are tenant-level or evidence-level and do not concern a shipment.

### Embedding full domain entities

Rejected because the gateway should reference business entities rather than own their database state.

## Trade-offs

### Benefits

- self-documenting APIs;
- safer refactoring;
- better observability;
- correct audit semantics;
- clean multi-tenant reasoning.

### Costs

- slightly more verbose structs;
- more explicit mapper code;
- developers must learn the identity vocabulary.

## Consequences

A PDF can exist as:

```text
tenant_id = T1
evidence_id = EV-1
shipment_id = null
```

and later become associated with one or more shipments without re-uploading the evidence.

## Interview question

> Why not just use `ID`?

Expected answer:

> Because an evidence object, shipment, request and event have different lifecycles. Explicit identifiers prevent accidental semantic substitution and make tenant isolation and audit behavior easier to reason about.
