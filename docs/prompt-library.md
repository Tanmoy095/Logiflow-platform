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
