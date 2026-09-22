# LLM Gateway — Architecture Decision Records

This directory contains the Architecture Decision Records (ADRs) for the
LogiFlow LLM Gateway. Each ADR captures one decision, the business need it
serves, the alternatives considered, and the trade-offs accepted.

The ADRs are the durable record of _why_ the gateway is shaped the way it is.
The code shows _what_; the ADRs show _why_ and _what else was on the table_.

---

## Index

| ADR                                                                 | Decision                                                    | Status   | Module                 |
| ------------------------------------------------------------------- | ----------------------------------------------------------- | -------- | ---------------------- |
| [ADR-011](./011-llm-gateway-generic-completion-command.md)          | Generic governed completion command and capability dispatch | Accepted | Module 3 — implemented |
| [ADR-012](./012-llm-gateway-explicit-identity-vocabulary.md)        | Explicit identity vocabulary and evidence references        | Accepted | Module 3 — implemented |
| [ADR-013](./013-llm-gateway-domain-policy-and-events.md)            | Domain policy and privacy-safe usage events                 | Accepted | Module 3 — implemented |
| [ADR-014](./014-llm-gateway-capability-specific-domain-packages.md) | Capability-specific domain packages                         | Accepted | Module 3 — implemented |
| [ADR-015](./015-llm-gateway-provider-router-fallback.md)            | Provider router, bounded retry, timeout and fallback        | Accepted | Module 4 — implemented |

---

## What each ADR covers

- **ADR-011 — Contract scalability.** Why the gateway exposes one governed
  `CompleteCommand` instead of a growing family of typed commands, and how
  `TaskType` + `TaskRegistry` + `TaskDefinition` keep the orchestration layer
  capability-agnostic.

- **ADR-012 — Identity and security semantics.** Why `tenant_id`,
  `evidence_id`, `shipment_id`, `request_id`, `idempotency_key`, and
  `event_id` are distinct concepts, and why the compiler enforces the
  distinction instead of relying on developer discipline.

- **ADR-013 — Domain governance and FinOps/privacy.** Why the domain owns
  gateway-wide policy and the `AIUsageEvent` shape, why the event is
  metadata-only, and why publication is an application port rather than a
  domain responsibility.

- **ADR-014 — DDD extensibility.** Why business invariants that belong to a
  specific capability live in `domain/<capability>/` rather than the root
  domain package, and when a new capability actually deserves a domain
  package.

- **ADR-015 — Distributed-systems reliability.** Why retry, per-attempt
  timeouts, and fallback live behind a `ProviderRouter` that implements the
  same `Provider` interface as a raw adapter, and why validation failures
  never trigger fallback.

---

## Reading paths

### If you're new to the codebase

Read in this order:
ADR-011 → contract scalability (what the gateway accepts)
ADR-012 → identity/security semantics (how it names things)
ADR-013 → domain governance + FinOps (what it guarantees)
ADR-014 → DDD extensibility (how capabilities grow)
ADR-015 → distributed reliability (how it survives vendors)

Together these tell one story: _a governed, testable, capability-extensible
AI execution boundary that treats external providers as unreliable
dependencies while keeping business truth deterministic._

### If you're reviewing a specific change

- Changing `CompleteCommand` shape or adding a capability → **ADR-011, ADR-014**
- Touching identity fields, evidence refs, or tenant scoping → **ADR-012**
- Modifying `AIUsageEvent`, policy, or telemetry → **ADR-013**
- Adding or reshaping a capability package → **ADR-014**
- Changing retry, timeout, fallback, or provider classification → **ADR-015**

### If you're evaluating the engineering approach

Start with **ADR-011** (why one command) and finish with **ADR-015** (why
bounded reliability instead of optimistic retries). The five together cover
the full decision surface of the gateway.

---

## Evidence trail

Each ADR links to the corresponding implementation artifacts. The broader
architectural tracking file records the commits and business rationale for
the domain foundation, the shipment-risk subdomain, the application
command/ports, the task registry, the orchestration pipeline, metadata
extraction, the provider router, and the deterministic fake-provider test
fixtures.

---

## Adding a new ADR

1. Copy the structure of the closest existing ADR.
2. Number sequentially: `016-...`, `017-...`, etc.
3. Include: Status, Decision, Business need, Alternatives considered,
   Trade-offs, Consequences, and an Interview question.
4. Add a row to the index above.
5. If the ADR changes an earlier decision, mark the earlier one
   `Superseded by ADR-0NN` rather than deleting it — the trail is the point.

---

## Status legend

| Status                | Meaning                                       |
| --------------------- | --------------------------------------------- |
| Proposed              | Under discussion; do not build against it yet |
| Accepted              | Current authoritative decision                |
| Accepted for Module N | Scoped to a specific delivery stage           |
| Superseded by ADR-NNN | Replaced; kept for historical context         |
| Deprecated            | No longer recommended; migration pending      |
