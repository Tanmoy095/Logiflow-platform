# ADR-009 — Standardized DDD Service Skeleton & Automated Generation Pipeline

**Date:** 2026-07-26  
**Status:** Accepted  
**Author:** Aunmoy Dey Tanmoy  
**Decision Owner:** Aunmoy Dey Tanmoy  
**Project:** LogiFlow  
**Module:** 3 — Platform Architecture & Composition Root

---

## 1. Context

LogiFlow is projected to grow to 15+ microservices spanning multiple bounded contexts—`stream‑ingestion`, `knowledge‑pipeline`, `llm‑gateway`, `query‑api`, `billing`, `workflow‑engine`, and several AI‑specialised services. Each service must follow the same DDD‑based layered architecture (`domain`, `application`, `interfaces`, `infrastructure`) and share a common composition root (`cmd/<service>/main.go`). Without automation, every new service would require a developer (or an AI agent) to manually create dozens of directories, placeholders, Go module files, and import paths. This manual process is error‑prone, time‑consuming, and leads to inconsistent structures across services—exactly the type of drift we eliminated at the infrastructure level with Helm library charts.

The core problem is that **the architecture contract exists only in documentation**, not in a machine‑enforceable form. Without a physical template, every developer interprets the contract differently, and every AI agent must be painstakingly prompted with the full directory layout.

---

## 2. Decision

I designed and implemented a **standardized service skeleton** (`service‑template/`) and an **automated generation pipeline** (`scripts/dev/generate-service.sh` + `make generate-service`).

### 2.1 Standardized Skeleton

The skeleton enforces the DDD layer structure defined in the project’s low‑level design:


services/<service>/
domain/
application/
interfaces/
http/
grpc/
kafka/
mcp/
temporal/
infrastructure/
postgres/
redis/
kafka/
provider/
keycloak/
migrations/



Each folder contains a minimal placeholder file that declares the package and a `TODO` comment explaining its purpose. The skeleton contains **no business logic**—it is purely a structural contract.

### 2.2 Separate Composition Root

The composition root is kept separate from the business layers. It lives at `cmd/template/main.go` and is copied into `cmd/<service>/main.go` during generation. This enforces the rule that `cmd/` is the only place that wires dependencies, and it never contains business logic.

### 2.3 Automated Generation Pipeline

A bash script (`scripts/dev/generate-service.sh`) performs the following steps:
1. Copies the business layers from `service‑template/` into `services/<service>/`.
2. Copies the composition root from `cmd/template/` to `cmd/<service>/`.
3. Updates the Go module path in the new service’s `go.mod` to the correct repository path.
4. Updates all Go import paths in the generated service files to match the new module.

A Makefile target (`make generate-service SERVICE=<name>`) invokes the script, making generation a single command.

### 2.4 Platform Package Stubs

To support the skeleton, placeholder modules were created for `pkg/foundation`, `pkg/technical`, and `pkg/shared`. These stubs will be filled with real implementations in later modules; their existence now ensures that services can import them without compilation errors from day one.

---

## 3. Alternatives Considered

| Alternative | Why Rejected |
|-------------|---------------|
| **Manual copy‑paste for each new service** | Error‑prone, inconsistent, does not scale. Leads to the same duplication problem we solved at the Helm level. |
| **A “golden repository” that developers fork** | Requires manual renaming and path updates. Forks diverge over time; there is no central enforcement of the architecture. |
| **A Go CLI that generates code from a template** | Over‑engineered for the current scale. A bash script is simpler, easier to maintain, and sufficient for this stage. A Go CLI will be a natural evolution when the platform matures. |
| **Placing the composition root inside the service directory** | Violates the separation of concerns. `cmd/` is a top‑level concern that may import from multiple services; burying it inside a single service creates confusion and makes it harder to add additional binaries per service later (e.g., `cmd/<service>/worker.go`). |
| **No template at all—rely on documentation** | Documentation drifts. The filesystem itself is a better enforcement mechanism. A template that must be physically copied ensures the structure is always correct. |

---

## 4. Consequences

### 4.1 Positive

- **Elimination of repetitive scaffolding**: Creating a new, fully structured service now takes **seconds** instead of hours. A developer runs `make generate-service SERVICE=<name>` and immediately has a working skeleton.
- **Architectural enforcement by construction**: The folder structure physically prevents common mistakes. `domain/` cannot contain infrastructure imports because it only contains placeholder files that declare a pure package. The composition root is the only place that can wire dependencies.
- **Consistency across all services**: Every service, from `stream‑ingestion` to `llm‑gateway`, shares the exact same layout. Code reviews are faster, onboarding is smoother, and platform‑wide changes (like adding a new shared package) can be rolled out uniformly.
- **AI‑agent readiness**: An AI coding agent asked to “create a new fraud‑detection service” only needs to generate the business logic files (domain entities, use cases, repository implementations). The agent runs the same `make generate-service` command to scaffold the structure, then places the generated business files into the correct directories. This dramatically reduces the prompt complexity and the risk of hallucinated folder structures.
- **Platform SDK foundation**: The `pkg/` stubs provide the import targets that the generated services expect. Filling them with real implementations (logging, metrics, events, tenant context) will instantly upgrade all services without structural changes.
- **Developer experience**: The `TODO` comments in each placeholder act as built‑in documentation, telling a developer exactly what goes where. The generation script also prints clear next‑step instructions.

### 4.2 Negative / Risks

- **Template maintenance**: If the architecture evolves, the template and script must be updated. However, this is a one‑time cost per change, and all future services automatically benefit.
- **Rigidity**: The skeleton enforces a specific layer structure. A service that genuinely needs a different structure (e.g., a small CLI tool without a domain) would need to deviate from the template. In such cases, the team can manually create the service and document the exception.
- **Module path coupling**: The script relies on a hard‑coded base module path. If the repository is renamed or moved, the path must be updated in the script and the template.

---

## 5. Validation

The pipeline was validated by generating the `stream‑ingestion` service from scratch:

1. `SERVICE=stream-ingestion make generate-service` – completed successfully.
2. The resulting directory structure was verified to match the standard service tree exactly.
3. `go build ./...` inside `services/stream‑ingestion/` compiled without errors, confirming that all Go packages are valid and import paths are correct.
4. The composition root at `cmd/stream‑ingestion/main.go` was present and syntactically correct (verified with `go vet`).

Any future service can be generated with the same command, and the same validation steps will apply.

---

## 6. Future Evolution

- **Fill the `pkg/` packages** with real implementations (config loaders, structured loggers, Prometheus metrics, event envelope, tenant propagation). The generated services will then import these packages and become production‑ready.
- **Extend the generation script** to optionally create the Helm chart, Dockerfile, and CI pipeline alongside the Go code, creating a truly self‑service platform.
- **Evolve the bash script into a Go CLI** (`logiflow new-service --name fraud-detection --type ai --gpu`) that wraps the same template logic but offers a richer interface, interactive prompts, and validation.
- **Add a `service-template-test/`** that contains a minimal service with real business logic, used as an integration test to verify that the skeleton remains compatible with the platform SDK.

---

## 7. How This Reduces Repetitive Tasks (Developer & AI Perspective)

- **Before this module**: Create 25 folders, write 12 placeholder files, manually set up `go.mod`, and fix import paths. Repeat for every service. ~30 minutes of manual, error‑prone work per service.
- **After this module**: `SERVICE=<name> make generate-service`. <5 seconds. Zero manual path editing.

For AI agents, the improvement is even more dramatic. Without the template, an agent must be prompted with the full directory tree and exact import paths, and it must reproduce them without error. With the template, the agent simply executes the generation command and then fills in the business‑specific files. The platform provides the **guardrails**; the agent provides the **business logic**.

---

## 8. References

- Service template: `service‑template/`
- Command skeleton: `cmd/template/main.go`
- Generation script: `scripts/dev/generate-service.sh`
- Makefile target: `make generate-service`
- Platform package stubs: `pkg/foundation/`, `pkg/technical/`, `pkg/shared/`
- Generated service: `services/stream‑ingestion/` and `cmd/stream‑ingestion/main.go`
- Architecture contract: `docs/folderstructure.md` and `docs/low‑level-design.md`