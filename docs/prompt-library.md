# LogiFlow Prompt Library

## Shipment Risk Analysis

### Version

v1

### Purpose

Analyze shipment evidence and classify operational risk.

### Input

- shipment identity
- shipment status
- carrier events
- commercial evidence
- operational evidence

### Output Contract

The model must return:

- `shipment_id` (must match the requested shipment)
- `risk` (one of: `no_risk`, `medium_risk`, `high_risk`)
- `confidence` (float between 0.0 and 1.0)
- `reasons` (list of strings; required for `high_risk`)

### Business Rules

1. `shipment_id` must identify the requested shipment.
2. `risk` must be one of the supported values.
3. `confidence` must be between 0.0 and 1.0.
4. `high_risk` requires at least one reason.
5. Missing shipment identity must never be fabricated.

### Versioning Rule

Prompt changes require a new prompt version.

Examples:

- `v1` → initial shipment risk analysis
- `v2` → adds customs-specific context

---

## Validation Pipeline

The LLM gateway treats **all provider output as untrusted** until it passes three
ordered validation stages:

1. **Syntax validation**
   - Ensures the raw response is valid JSON.
   - Failures are classified as `validation_failed`.

2. **Schema validation**
   - Ensures required fields are present and of the correct type.
   - Required fields: `shipment_id`, `risk`, `confidence`, `reasons`.
   - Failures are classified as `validation_failed`.

3. **Domain validation**
   - Enforces business invariants:
     - shipment identity must match the request
     - `risk` must be one of the supported values
     - `confidence` must be finite and within `[0.0, 1.0]`
     - `high_risk` output must include at least one reason
   - Failures are classified as `validation_failed`.

Only after all three stages succeed does the gateway expose a **trusted
CompletionResult**.

---

## Fallback vs Refusal Policy

- **Operational failures** (`provider_timeout`, `provider_unavailable`,
  `request_canceled`) are candidates for bounded retry or fallback to another
  provider. They indicate the provider did not return a usable answer.

- **Semantic validation failures** (`validation_failed`) mean the provider
  returned a response, but it violates the contract or business rules.
  These are **refusal** or **human review** candidates, not automatic retry
  candidates. Blindly retrying semantic failures can hide systematic prompt
  or model degradation and waste tokens.

This policy will be implemented in future sprints as a **Strategy** pattern.
