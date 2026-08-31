# LLM Gateway Runbook

This runbook helps engineers diagnose common operational issues.

## Table of Contents

- [LLM Gateway Runbook](#llm-gateway-runbook)
  - [Table of Contents](#table-of-contents)
    - [Symptom: High Gateway Latency](#symptom-high-gateway-latency)
    - [Symptom: Provider Errors](#symptom-provider-errors)
    - [Symptom: Validation Failures](#symptom-validation-failures)
    - [Liveness vs Readiness](#liveness-vs-readiness)

---

### Symptom: High Gateway Latency

**Signals**

- `total_gateway_latency_p95` exceeds threshold (e.g., > 2s)
- Customer reports slow document processing

**Diagnosis**

1. Check `provider_latency_ms` vs `validation_latency_ms` in logs.
2. If `provider_latency_ms` is high (e.g., > 10s), the external AI provider is likely slow.
3. If `validation_latency_ms` is high (e.g., > 1s), internal validation is bottleneck.

**Recovery**

- **Provider slow**: Check provider health dashboard, consider fallback if implemented. Do **not** restart gateway pods.
- **Validation slow**: Profile the validation pipeline; look for memory issues, CPU contention, or unexpected blocking calls.

---

### Symptom: Provider Errors

**Signals**

- Metrics show increase in `provider_unavailable` or `provider_timeout`
- `error_kind` in logs is `provider_unavailable` or `provider_timeout`

**Diagnosis**

1. Check provider status page.
2. Verify network connectivity from gateway to provider.
3. Examine recent changes to prompt versions (may cause timeouts).

**Recovery**

- If provider is down, enable fallback provider if available.
- Adjust context deadlines if they are too aggressive.
- Contact provider support.

---

### Symptom: Validation Failures

**Signals**

- Increase in `validation_failed` error kind.
- Logs show high `validation_latency_ms` or specific domain rule violations.

**Diagnosis**

1. Look at the specific validation error messages in logs (e.g., confidence > 1, missing reasons).
2. Check recent prompt changes—model may be returning malformed output.
3. Verify that the output contract (schema) has not changed.

**Recovery**

- If the model is producing invalid output, consider prompt adjustment or model version rollback.
- Do **not** automatically retry validation failures; they are semantic, not operational.
- Route such failures to human review if needed.

---

### Liveness vs Readiness

- **Liveness**: process is alive. Must **not** depend on external provider health.
- **Readiness**: instance can serve traffic. May include provider health check, but should be separate from liveness.
