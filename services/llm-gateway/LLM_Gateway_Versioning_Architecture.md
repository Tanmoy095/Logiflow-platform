# LogiFlow AI Engineering — Versioning

> **Mental model:** version the boundary that changes. Do not use one global `v1` for API protocol, task inputs, prompts, outputs, and Kafka events.

## 1. The most important idea: who creates the prompt?

There are two different layers of input:

- **Dynamic input:** supplied by the user, client, stream-ingestion service, documents, telemetry, or retrieved context.
- **Application-controlled instructions:** the system prompt / task instructions owned by the application and identified by `PromptVersion`.

For an enterprise AI task, the application usually constructs the final model input rather than blindly forwarding raw text.

```text
User / Stream / RAG
      │
      │ dynamic data / question / context
      ▼
┌──────────────────────────────┐
│ LLM Gateway                  │
│                              │
│ PromptVersion ──► template   │
│                 +            │
│                 dynamic data │
└──────────────┬───────────────┘
               │
               ▼
        Final model request
               │
               ▼
       OpenAI / Gemini / Claude
```

This distinction matters even for ChatGPT-style applications: the user controls the query, while the application can still control system instructions, persona, boundaries, tools, output requirements, and other policy.

---

## 2. The five versions

| Version                 | Example                         | Protects                 | Main question                                       |
| ----------------------- | ------------------------------- | ------------------------ | --------------------------------------------------- |
| **ContractVersion**     | `ai-completion.v1`              | Gateway envelope         | “Can I understand this API request?”                |
| **TaskSchemaVersion**   | `shipment_delay_risk.v1`        | Task input contract      | “Do I understand this capability’s inputs?”         |
| **PromptVersion**       | `shipment-delay-risk.prompt.v1` | AI instructions/behavior | “Which exact instructions should the model follow?” |
| **OutputSchemaVersion** | `shipment_delay_risk.result.v1` | Result contract          | “What result shape should I return/validate?”       |
| **EventVersion**        | `llm-gateway.usage.v1`          | Kafka event contract     | “What event format do internal consumers receive?”  |

```text
                    AI REQUEST
                        │
                        ▼
             ContractVersion
              API envelope
                        │
                        ▼
             TaskSchemaVersion
               task inputs
                        │
                        ▼
                Task Registry
             resolve capability
                        │
                        ▼
                PromptVersion
              system instructions
                        │
                        ▼
                  LLM call
                        │
                        ▼
            OutputSchemaVersion
             decode + validate
                        │
                        ▼
                EventVersion
               audit / billing
```

### Rule

```text
Prompt changed?          → PromptVersion
Task input changed?      → TaskSchemaVersion
Task output changed?     → OutputSchemaVersion
Gateway envelope changed?→ ContractVersion
Kafka event changed?     → EventVersion
```

---

# 3. PromptVersion in plain English

`PromptVersion` is a stable identifier for a specific, version-controlled set of instructions sent to the model.

Example:

```text
shipment-delay-risk.prompt.v1
shipment-delay-risk.prompt.v2
```

A prompt is not “just text” in an AI product. Changing one instruction can change classification, hallucination behavior, formatting, or quality. Therefore prompt changes deserve the same discipline as application changes.

### Prompt formula

```text
Final model input
    = Application-controlled instructions
    + Dynamic user/client input
    + Retrieved context (when using RAG)
    + Other request context/tools as applicable
```

A simplified task template can look like:

```text
You are a senior logistics analyst.
Always return JSON matching the requested output schema.
Do not assume risk without evidence.

Input Data:
{{.UserInput}}
```

---

# 4. Why not pass raw user input directly?

Suppose the incoming data is:

```text
Shipment SH-9912 delayed at Chittagong port.
```

Without application-controlled instructions, the model may answer in natural language, while the Go service may expect a machine-readable result.

With a versioned task prompt:

```text
shipment-delay-risk.prompt.v1
        │
        ▼
┌──────────────────────────────────────────────┐
│ System instructions                          │
│ - classify risk                               │
│ - use evidence                                │
│ - return JSON                                 │
│ - obey business constraints                  │
└──────────────────────┬───────────────────────┘
                       +
                       ▼
┌──────────────────────────────────────────────┐
│ Dynamic shipment evidence                    │
└──────────────────────┬───────────────────────┘
                       │
                       ▼
              Final model request
```

The application therefore gets a much more predictable integration boundary. The model still needs validation after it responds; prompt instructions are not a substitute for domain validation.

---

# 5. Prompt registry

A practical approach is to keep versioned prompt templates and resolve them by ID.

```text
prompts/
├── shipment_risk_v1.txt
└── shipment_risk_v2.txt
```

Conceptually:

```go
type PromptRegistry struct {
    templates map[string]*template.Template
}

func (r *PromptRegistry) Render(
    promptVersion string,
    userInput string,
) (string, error)
```

Lookup must be explicit:

```text
shipment-delay-risk.prompt.v2
            │
            ▼
       prompt registry
            │
            ▼
      render template
            │
            ▼
      final model input
```

An unknown prompt version should fail clearly instead of silently selecting another prompt.

> **Important:** a Go `map` makes lookup convenient; the architectural decision is the **registry boundary**, not “using a hashmap because hashmaps never break.”

---

# 6. Prompt execution flow

```text
[Microservice / Stream Ingestion]
          │
          │ task + dynamic data
          │ prompt_version = shipment-delay-risk.prompt.v2
          ▼
[Gateway Application]
          │
          ├─ validate request/versions
          ├─ resolve task definition
          ├─ resolve PromptVersion
          ├─ render template + dynamic data
          └─ call provider
          ▼
[LLM Provider]
          │
          ├─ process input tokens
          ├─ generate output tokens
          └─ return output + usage metadata
          ▼
[Gateway]
          │
          ├─ decode output
          ├─ apply domain/schema validation
          └─ emit usage/audit event
          ▼
[Kafka → Billing / Analytics / Audit]
```

---

# 7. Why multiple versions instead of one `v1`?

Different boundaries evolve at different speeds:

```text
Contract       ─────────── slow
Task schema    ──────── medium
Prompt         ── fast
Output schema  ──────── medium
Event schema   ─────────── slow
```

If the prompt changes weekly, that should not force every API client to migrate. If a task adds a result field, that should not force a global gateway protocol upgrade.

**Architecture principle:** independent velocity of change.

---

# 8. Version validation: where and why?

## Gate 1 — ContractVersion

At the gateway boundary, reject unsupported protocol versions early.

```text
ai-completion.v1  → supported → continue
unknown            → reject
```

## Gate 2 — TaskSchemaVersion

Resolve the requested capability and version:

```text
(TaskType + TaskSchemaVersion)
            │
            ▼
      Task Registry
```

Unknown combinations are rejected.

## Gate 3 — PromptVersion

The prompt registry must contain the requested prompt version.

```text
unknown prompt version → reject / explicit error
```

Do not silently fall back to an unrelated prompt; that makes AI behavior difficult to reason about and audit.

## Gate 4 — OutputSchemaVersion

The returned model output is decoded and validated against the expected result contract.

```text
valid JSON ≠ automatically valid business result
```

## Gate 5 — EventVersion

Usage/audit facts are emitted with their event schema version so Kafka consumers know which event contract they received.

---

# 9. Real production evolution

### Scenario A — Prompt tuning

```text
shipment-delay-risk.prompt.v1
              │
              ▼
shipment-delay-risk.prompt.v2
```

The task/API contract can remain unchanged.

A common rollout pattern is:

```text
V1 ──► 90%
V2 ──► 10% canary
        │
        ├─ compare quality
        ├─ compare failures
        ├─ compare latency
        └─ compare token usage/cost
```

Once validated, V2 becomes the default. Keeping V1 available makes rollback straightforward.

### Scenario B — New output field

```text
shipment_delay_risk.result.v1
              │
              ▼
shipment_delay_risk.result.v2
```

Support V1 and V2 during migration rather than forcing all consumers to upgrade together.

### Scenario C — New gateway protocol

```text
ai-completion.v1
        │
        ▼
ai-completion.v2
```

This is a global contract change. A compatibility path may support both versions until V1 is deprecated.

---

# 10. Versioning and auditability

For a production AI request, useful metadata includes:

```text
contract_version
 task_type
 task_schema_version
 prompt_version
 output_schema_version
 provider
 model
 usage metrics
 status
```

This lets engineering teams ask:

> “Which versioned instructions and contracts produced this result?”

That is valuable for debugging, evaluation, rollout analysis, cost analysis, and incident investigation.

---

# 11. Registry vs giant switch

A small `switch` is not inherently wrong. The problem is a growing central switch that becomes a knowledge dump for every task/version.

Prefer:

```text
              Registry
                 │
      ┌──────────┼──────────┐
      ▼          ▼          ▼
   task V1    task V2    task V1
```

with an explicit lookup key such as:

```text
TaskType + Version
```

The registry keeps version resolution additive and localized.

---

# 12. What the map does NOT guarantee

A registry does **not** make old code unbreakable.

Compatibility still depends on keeping the old contract and implementation supported.

```text
Registry
   + retained old versions
   + stable old contracts
   + explicit compatibility policy
   = controlled evolution
```

You can still break V1 by deleting it, changing its meaning without changing the version, or removing assets it depends on.

---

# 13. Interview answer

> **“We version independent boundaries because an AI system has different contracts that evolve at different rates. ContractVersion protects the gateway envelope, TaskSchemaVersion protects task inputs, PromptVersion tracks AI instructions and behavior, OutputSchemaVersion protects result compatibility, and EventVersion protects Kafka consumers. This lets us tune prompts, evolve individual capabilities, and migrate consumers without turning every small AI change into a global API breaking change.”**

### One-line memory aid

> **Envelope → Input → Instructions → Output → Events.**

That is the order to remember the five version boundaries.
