# LogiFlow - AI-Powered Operational Intelligence Platform

[![Go Version](https://img.shields.io/badge/go-1.25%2B-blue.svg)](https://golang.org)
[![Kubernetes](https://img.shields.io/badge/kubernetes-v1.29%2B-blue.svg)](https://kubernetes.io)
[![Helm](https://img.shields.io/badge/helm-v3.14%2B-blue.svg)](https://helm.sh)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

**LogiFlow is a production-grade internal developer platform for multi-tenant AI workloads.**
It provides a **Golden Path** for building, deploying, and operating microservices on Kubernetes, from a single-command local environment to a GitOps-driven release model in [deployment/gitops/argocd/README.md](deployment/gitops/argocd/README.md). Every decision in this repository is guided by **Domain-Driven Design (DDD)**, platform engineering, and deploy-first principles, ensuring that the platform scales from 2 to 200 services without structural drift.

> **For recruiters & hiring managers:** This project demonstrates the ability to design and implement a complete internal platform, including Helm library charts, opinionated health-check policies, multi-environment strategies, automated service scaffolding, DDD-enforced code skeletons, and CI-ready workflows.

## What Has Been Delivered

The latest implementation work moves the LLM Gateway from a runtime scaffold toward a testable, production-shaped policy boundary:

- **Typed failure taxonomy:** invalid requests, provider timeouts, provider unavailability, caller cancellation, validation failures, and unexpected internal errors are represented as stable domain error kinds.
- **Cancellation and deadline propagation:** the gateway passes the request context to the provider and classifies timeout and cancellation outcomes without confusing them with generic provider failures.
- **Trust-oriented validation:** provider output is treated as untrusted and must pass syntax, schema, and domain-invariant validation before becoming a trusted `CompletionResult`.
- **Explicit refusal behavior:** malformed or semantically invalid model output is rejected rather than silently repaired or passed to automation.
- **Execution metadata:** each completion records a correlation ID, provider, prompt version, status, error kind, provider latency, validation latency, and total gateway latency.
- **Environment-aware delivery:** the gateway now has dev, staging, and production Helm overlays, secure provider-key injection, standard probes and resources, and Argo CD child Applications managed through the app-of-apps pattern.

These capabilities are covered by focused Go tests and operational guidance in [docs/runbooks/llm-gateway.md](docs/runbooks/llm-gateway.md). Provider adapters, shared budget enforcement, fallback routing, and asynchronous usage accounting remain planned integrations rather than claims of the current runtime.

## High-Level Architecture

```mermaid
flowchart TB
	dev[Developer / AI Agent] -->|make dev-up| platform[Platform Automation]
	dev -->|SERVICE=foo make generate-service| generator[Service Template + Generator]
	platform --> cluster[Kubernetes Cluster]
	generator --> cluster

	cluster --> helm[Helm Library Chart]
	cluster --> serviceCharts[Per-service Helm Charts]
	cluster --> services[Go Services]

	services --> cmd[cmd/<service>/main.go\nComposition Root]
	services --> domain[services/<service>/domain/\nPure Business Rules]
	services --> application[services/<service>/application/\nUse Cases & Ports]
	services --> interfaces[services/<service>/interfaces/\nHTTP / gRPC / Kafka / MCP]
	services --> infrastructure[services/<service>/infrastructure/\nDB / Cache / Provider]
```

**Key design principles:**

- DDD boundaries are visible in the filesystem. Every service folder tree shows exactly what layer a piece of code belongs to.
- Platform owns infrastructure; developers own business logic. The Helm library chart and Go service skeleton enforce this contract.
- Deploy-first. Every service has probes, resource limits, security contexts, and a local Kind smoke test.
- AI-agent compatible. Standardized templates and generation scripts let AI agents safely scaffold new services.

## LLM Gateway Overview

The `llm-gateway` is LogiFlow's internal AI control plane and anti-corruption layer. It sits between trusted internal services and untrusted external model providers such as OpenAI, Gemini, and Anthropic. Its purpose is to give the rest of the platform one stable, provider-neutral contract for controlled AI execution.

The gateway prevents every downstream service from independently implementing provider SDKs, prompt handling, tenant budgets, retries, validation, cost accounting, and outage behavior. This creates one policy boundary that can evolve independently from `stream-ingestion`, `knowledge-pipeline`, and future consumers.

### Gateway Responsibilities

The target gateway owns the following decisions:

- Accept a typed completion request from an internal service, including tenant and tracing context.
- Enforce tenant authorization, rate limits, and token or cost budgets before spending money with a provider.
- Select a provider through a stable internal policy rather than exposing vendor-specific APIs upstream.
- Apply strict context deadlines, bounded retries, circuit breaking, and fallback routing for operational failures.
- Treat every model response as untrusted until syntax, schema, and domain-invariant validation succeeds.
- Return a trusted `CompletionResult` or an explicit refusal that can be routed to human review.
- Publish usage and cost events asynchronously so billing does not block the completion response.
- Emit provider, latency, token, validation, and trace metadata for operational diagnosis.

### End-To-End Architecture

```mermaid
flowchart TB
	client[knowledge-pipeline\ninternal caller] -->|gRPC completion request\ntenant_id + trace context| gateway[llm-gateway\nAI policy and trust boundary]

	subgraph controls[Gateway controls]
		identity[Authorize tenant\nand request]
		budget[Redis budget and\nrate-limit check]
		cache[Tenant-scoped\nsemantic cache]
		policy[Provider strategy\nand prompt policy]
		identity --> budget --> cache --> policy
	end

	gateway --> controls
	policy --> primary[OpenAI\nprimary adapter]
	policy --> secondary[Gemini / Anthropic\nfallback adapters]
	primary --> response[Raw model response]
	secondary --> response
	response --> syntax[1. Syntax\nJSON parsing]
	syntax --> schema[2. Schema\nfield and type checks]
	schema --> domain[3. Domain\ninvariant checks]
	domain --> trusted[Trusted CompletionResult]
	domain --> refusal[Validation refusal\nReviewRequired]

	trusted --> client
	trusted --> usage[UsageCostEvent]
	usage --> kafka[Kafka]
	kafka --> billing[billing service]
	billing --> ledger[(PostgreSQL\nauthoritative ledger)]
```

### Request Lifecycle

The intended completion path is deliberately ordered:

1. `knowledge-pipeline` sends a synchronous gRPC request because it needs the AI result before it can continue chunking, embedding, and indexing a document.
2. The gateway authenticates the service and extracts `tenant_id`, request identity, and trace metadata.
3. Redis performs an atomic tenant budget and rate-limit decision. An exhausted budget is rejected before any provider call.
4. The gateway checks a tenant-scoped semantic cache. A cache hit is returned only if the cached result is still valid for the tenant and prompt version.
5. On a cache miss, a provider strategy selects OpenAI or another configured provider and invokes it with a strict deadline.
6. Operational failures such as timeouts, HTTP 429, or selected HTTP 5xx responses use bounded retry or fallback behavior.
7. The response passes syntax validation, schema validation, and domain-invariant validation in that order.
8. A valid result is returned to the caller, and token usage and cost metadata are emitted asynchronously to Kafka.
9. The billing service consumes the usage event and writes the durable financial record to PostgreSQL.
10. Invalid or semantically unsafe output is refused and marked for review; it is never silently repaired or passed to automation.

### Why The Flow Uses Both gRPC And Kafka

These protocols solve different problems. gRPC is the synchronous request-response boundary between `knowledge-pipeline` and the gateway: it provides typed Protobuf contracts, HTTP/2 multiplexing, deadline propagation, and standard status codes. Kafka is the asynchronous event boundary for `UsageCostEvent`: it allows billing to consume, retry, replay, and reconcile usage without adding database latency to the AI request.

```mermaid
flowchart LR
	upload[Client uploads evidence] --> ingestion[stream-ingestion]
	ingestion -->|persist receipt| postgres[(PostgreSQL)]
	ingestion -->|RawEvidenceReceived| events[Kafka]
	events --> worker[knowledge-pipeline]
	worker -->|synchronous gRPC| gateway[llm-gateway]
	gateway -->|validated result| worker
	gateway -->|asynchronous UsageCostEvent| events2[Kafka]
	events2 --> billing[billing]
```

Keeping the ingestion path asynchronous means a slow provider does not prevent a file from being durably accepted. Keeping the gateway call synchronous means the worker receives a typed result or failure before it writes derived vectors and citations.

### Trust Boundary And Output Safety

The gateway is an anti-corruption layer because provider output is not trusted merely because the provider returned HTTP 200. The target risk-assessment contract contains `shipment_id`, `risk`, `confidence`, and `reasons`.

```mermaid
flowchart TD
	raw[Provider output] --> parse{Valid JSON?}
	parse -->|No| parseError[ParseError\nrefuse]
	parse -->|Yes| shape{Required fields\nand types?}
	shape -->|No| schemaError[SchemaError\nrefuse]
	shape -->|Yes| rules{Business rules\nsatisfied?}
	rules -->|No| review[ReviewRequired\nstop automated action]
	rules -->|Yes| result[Trusted result\nreturn to worker]
```

Schema validation can verify that `confidence` is a number. Domain validation must additionally verify that:

$$0.0 \leq \mathrm{confidence} \leq 1.0$$

It must also reject a `high_risk` result with no evidence in `reasons`. The gateway must not clamp an invalid value such as `1.7` to `1.0` or invent a reason. Silent correction hides model degradation and can trigger unsafe operational or financial automation.

### Failure Classification

The gateway treats operational failures and data-integrity failures differently:

```mermaid
flowchart LR
	failure[Provider outcome] --> classify{Failure type}
	classify -->|Timeout / 429 / 5xx / network| operational[Operational failure]
	operational --> control[Deadline + bounded retry\ncircuit breaker]
	control --> fallback[Fallback provider\nor trusted cache]
	classify -->|Malformed / wrong schema /\ndomain invariant violation| integrity[Integrity failure]
	integrity --> refuse[Refuse result\nReviewRequired]
```

Fallback is appropriate when the provider could not safely produce a result. It is not a substitute for validation when a provider returns a logically invalid result. Retrying semantic failures wastes tokens and can produce a plausible but unsafe answer.

### Budget, Consistency, And Cost Control

The gateway is stateless at the application instance level. Shared tenant state belongs in Redis so any replica can enforce the same budget and rate policy. A Redis Lua script or equivalent atomic operation prevents race conditions between concurrent replicas.

Kafka carries usage events to the billing service, while PostgreSQL remains the authoritative ledger. This write-behind design avoids synchronous database locks and connection-pool exhaustion on the completion path, at the cost of eventual consistency and a reconciliation dependency.

Budget enforcement is more important than availability: if the authoritative Redis write path is unavailable, the production policy should fail closed rather than permit unbounded provider spend. A fail-open or AP policy may be acceptable for non-critical, read-only semantic cache data, but not for financial budget decrements.

### Resilience And Observability

Each provider has an independent circuit breaker. A failing OpenAI circuit must not disable healthy Gemini or Anthropic capacity. Strict deadlines stop waiting for slow calls; bounded retries prevent retry storms; half-open probes test recovery; and fallback results pass the same validation pipeline as primary results.

The gateway should expose telemetry that separates external provider latency from internal processing:

- `provider_latency_ms`: provider round-trip time.
- `validation_latency_ms`: syntax, schema, and domain validation time.
- `total_gateway_latency_ms`: complete gateway request time.
- `model_provider`, model, prompt version, token usage, estimated cost, cache outcome, retry count, and circuit state.
- W3C `traceparent` propagated through Kafka headers and gRPC metadata.

### Current Implementation And Plan

The current implementation is intentionally deploy-first rather than feature-complete. It provides a Go process that listens on `PORT` (default `8080`), serves `/healthz`, `/startupz`, and `/live`, and shuts down gracefully on `SIGINT` or `SIGTERM`. Its application service now validates requests before provider work, propagates cancellation, classifies provider failures with typed errors, runs the syntax/schema/domain validation chain, refuses invalid results, and returns execution metadata with decomposed latency measurements. It is packaged with `build/Dockerfile.llm-gateway` and deployed through `deployment/helm/services/llm-gateway/`.

Provider adapters, Redis budget and rate-limit enforcement, Kafka usage events, gRPC handlers, circuit breakers, fallback routing, and telemetry export are the next implementation stages. The full design explains the intended contracts and trade-offs without presenting planned behavior as already implemented.

For the complete LLM Gateway system design, including DDD boundaries, request flows, CAP and Redis decisions, timeout and circuit-breaker behavior, fallback-versus-refusal policy, security, observability, deployment, testing, and decision trade-offs, see [services/llm-gateway/System_Design.md](services/llm-gateway/System_Design.md). The original HLD source is available at [services/llm-gateway/LogiFlow\_ LLM_GATEWAY_HLD.pdf](services/llm-gateway/LogiFlow_%20LLM_GATEWAY_HLD.pdf).

## GitOps and Release Flow

LogiFlow keeps release orchestration in Git. The repository includes Argo CD parent Applications for dev, staging, and production under [deployment/gitops/argocd/](deployment/gitops/argocd/), and the detailed operating model is documented in [deployment/gitops/argocd/README.md](deployment/gitops/argocd/README.md).

```mermaid
flowchart TB
	dev[Developer or AI Agent] --> pr[Pull Request]
	pr --> ci[CI Validation]
	ci --> merge[Merge to main]
	merge --> git[Git repository]
	git --> parent[Parent Application]
	parent --> children[Child Applications in apps-* folders]
	children --> helm[Render Helm chart with environment values]
	helm --> reconcile[Compare desired vs live state]
	reconcile --> cluster[Kubernetes Cluster]
	cluster --> heal[Self-heal and drift correction]
	values[Chart or values change] --> git
	app[New or changed child Application YAML] --> git
	app -. parent creates or updates child .-> children
```

The practical result is simple: developers change Git, CI validates the change, and Argo CD reconciles the cluster. A chart or values change is handled by the existing child Application. A new or changed child Application manifest is handled by the parent first. Manual production edits are treated as drift, not as the source of truth. See the [complete Helm and Argo CD workflow](deployment/gitops/argocd/README.md) for local simulation, bootstrap, secrets, and operating details.

## Repository Structure

```text
.
├── cmd/ # Composition roots (one binary per service)
│   ├── hello/main.go
│   ├── llm-gateway/main.go
│   ├── stream-ingestion/main.go
│   └── template/ # Skeleton for new services
├── services/ # Business logic (DDD layers)
│   ├── llm-gateway/
│   │   ├── domain/ # Gateway domain model and policies
│   │   ├── application/ # Gateway use cases and ports
│   │   ├── interfaces/ # HTTP, gRPC, Kafka, MCP, Temporal adapters
│   │   └── infrastructure/ # PostgreSQL, Redis, Kafka, provider clients
│   ├── stream-ingestion/
│   │   ├── domain/ # Entities, value objects, domain events
│   │   ├── application/ # Use cases, ports (interfaces), services
│   │   ├── interfaces/ # HTTP, gRPC, Kafka, MCP, Temporal adapters
│   │   └── infrastructure/ # PostgreSQL, Redis, Kafka, provider clients
│   └── (future services ...)
├── service-template/ # Reusable DDD skeleton (copied by generator)
├── pkg/ # Platform SDK (shared across services)
│   ├── foundation/ # Pure helpers (config, retry, clock, ids)
│   ├── technical/ # Reusable technical adapters (logging, metrics, server, gRPC, Kafka)
│   └── shared/ # Cross-service contracts (tenant, auth, events)
├── deployment/ # Kubernetes manifests & GitOps
│   └── helm/
│       ├── library/logiflow-service/ # Opinionated platform library chart
│       ├── services/llm-gateway/ # LLM gateway chart and environment overlays
│       └── services/<service>/ # Per-service charts (values overlays)
├── dev/ # Local development environment
│   └── kind/kind-config.yaml # Kind cluster definition
├── scripts/ # Automation & developer tooling
│   └── dev/
│       ├── doctor.sh # Pre-flight environment validation
│       ├── dev-up.sh # One-command local environment
│       └── generate-service.sh # Service generator
├── docs/ # Engineering evidence
│   ├── adr/ # Architecture Decision Records
│   └── runbooks/ # Debugging playbooks
├── Makefile # Unified developer interface
└── README.md
```

### Detailed Breakdown

#### cmd/<service>/main.go - Composition Roots

The single entry point for each microservice. Its only job is to load configuration, initialize observability, create infrastructure adapters, instantiate application services, and start servers. It contains zero business logic.

The llm-gateway composition root starts an HTTP server on port 8080 by default, supports the `PORT` environment variable, exposes `/healthz`, `/startupz`, and `/live` for Kubernetes probes, and performs graceful shutdown on SIGINT and SIGTERM.

#### services/<service>/ - DDD Layered Architecture

Every service follows the same four-layer structure, enforced by the service-template/:

- domain/ - Pure business rules. Imports only the Go standard library and pkg/foundation. Never touches databases, HTTP, or messaging.
- application/ - Use cases and ports. Defines interfaces that infrastructure must implement. Orchestrates domain entities.
- interfaces/ - Adapters that convert external requests (HTTP, gRPC, Kafka messages, Temporal activities) into application commands.
- infrastructure/ - Concrete implementations of application ports: PostgreSQL repositories, Redis caches, Kafka producers, third-party API clients.

This structure physically prevents common architectural mistakes, such as SQL queries inside domain entities.

#### service-template/ and scripts/dev/generate-service.sh

Adding a new service is a single command:

```bash
SERVICE=fraud-detection make generate-service
```

The script copies the DDD skeleton, sets up the Go module with correct import paths, and creates the composition root, all in seconds. AI coding agents can execute the same command, ensuring they never invent inconsistent folder structures.

#### pkg/ - Platform SDK

- foundation - Pure, business-agnostic utilities safe for even domain to import, such as config parsers, retry backoffs, and ULID generators.
- technical - Reusable infrastructure adapters that know about protocols but not business rules, such as structured logging, Prometheus metrics, gRPC interceptors, Kafka wrappers, and HTTP server helpers.
- shared - Cross-service contracts that must be identical everywhere, such as tenant context propagation, JWT auth helpers, and the canonical event envelope.

#### deployment/helm/library/logiflow-service/ - Helm Library Chart

A single source of truth for Kubernetes standards. Every service chart imports this library to inherit:

- Standard labels and selectors
- Opinionated security contexts: runAsNonRoot, readOnlyRootFilesystem, seccompProfile, and dropped capabilities
- Startup, readiness, and liveness probes with fixed timings, with service-specific paths only
- Default resource requests and limits
- PodDisruptionBudget and ServiceMonitor placeholders

This means a security policy update is a one-line change that propagates to all services on their next deploy.

#### deployment/helm/services/<service>/ - Per-Service Charts

Service charts are thin wrappers that only provide a values.yaml with the container image, port, and environment variables. All infrastructure is inherited from the library. Environment-specific overrides, such as values-staging.yaml and values-prod.yaml, plus secrets like values-prod-secrets.yaml, follow the same pattern.

The llm-gateway chart is located at `deployment/helm/services/llm-gateway/`. Its container image is built with `build/Dockerfile.llm-gateway`, and its chart inherits the platform labels, security contexts, resources, and probe timings from the library chart.

The complete llm-gateway architecture, including bounded-context boundaries, provider fallback, budget enforcement, validation, observability, deployment flows, and design trade-offs, is documented in [services/llm-gateway/System_Design.md](services/llm-gateway/System_Design.md). The low-level implementation contract, including SOLID principles, object composition, interfaces, concurrency, design patterns, error handling, and test strategy, is documented in [services/llm-gateway/LLD_System_Design.md](services/llm-gateway/LLD_System_Design.md). The original high-level design source is retained at [services/llm-gateway/LogiFlow\_ LLM_GATEWAY_HLD.pdf](services/llm-gateway/LogiFlow_%20LLM_GATEWAY_HLD.pdf).

#### deployment/gitops/argocd/ - GitOps Control Plane

This directory defines the Argo CD application structure for GitOps delivery. Parent Applications point at environment folders, and child Applications define service-specific Helm releases. The llm-gateway now has child Applications for dev, staging, and production, while the parent Applications manage discovery through the app-of-apps pattern. Production provider secrets remain external to Git.

#### dev/ and scripts/ - Local Developer Experience

- doctor.sh - Pre-flight check that validates Docker, Kind, kubectl, Helm, Go versions, branch names, and port availability.
- dev-up.sh - One command that creates a Kind cluster, builds the Docker image, loads it into Kind, lints and templates the Helm chart, deploys, waits for readiness, port-forwards, and runs a health check.
- Makefile - The universal entry point: make dev-up, make lint, make template, make generate-service, make status, and make logs.

#### docs/ - Engineering Evidence

Contains Architecture Decision Records for every major design choice, production debugging runbooks, and portfolio evidence. This is the why behind the code.

## The Golden Path: Creating a New Service

### Generate the skeleton

```bash
SERVICE=my-service make generate-service
```

This creates cmd/my-service/main.go and services/my-service/ with all DDD layers.

### Configure the chart

Copy deployment/helm/services/hello/ to deployment/helm/services/my-service/, then edit values-dev.yaml and the staging/prod overrides to set the image, port, and any custom environment variables. The deployment.yaml template remains identical because it only contains include calls to the library.

### Implement business logic

Fill in domain/, application/, interfaces/, and infrastructure/ with real code. The platform provides the deployment contracts and Helm probe configuration; each service composition root must start its server and expose the configured health endpoints.

### Deploy locally

```bash
SERVICE=my-service make dev-up
```

This builds the image, loads it into Kind, lints the Helm chart, deploys, and starts a port-forward. The service is reachable at http://localhost:8080/healthz.

### Deploy the llm-gateway locally

```bash
# Build the gateway image from the repository root
docker build -f build/Dockerfile.llm-gateway -t logiflow/llm-gateway:local .

# Load the image into the local Kind cluster
kind load docker-image logiflow/llm-gateway:local --name logiflow-dev

# Deploy development values
helm upgrade --install llm-gateway-dev deployment/helm/services/llm-gateway \
	-f deployment/helm/services/llm-gateway/values-dev.yaml \
	--set image.tag=local \
	--namespace logiflow-dev --create-namespace

# Verify the deployment
kubectl get pods -n logiflow-dev
```

The staging and production overlays are `values-staging.yaml` and `values-prod.yaml`. Production secrets belong in `values-prod-secrets.yaml` and must never be committed.

## Platform Engineering Highlights

### Opinionated Health-Check Strategy

Every LogiFlow container gets a startup, readiness, and liveness probe from a single logiflow.probes template. Developers only configure the probe paths; the platform owns all timing parameters. This prevents unsafe manual overrides and eliminates CrashLoopBackOff for slow-loading AI models.

### Secure by Default

All containers run as non-root, with a read-only root filesystem, all capabilities dropped, and seccompProfile: RuntimeDefault applied. These settings are enforced by the library chart and cannot be overridden by individual services.

### Multi-Environment Strategy

One chart, many environments. Base configuration in values-dev.yaml is overridden by environment-specific files such as values-staging.yaml and values-prod.yaml, plus secrets in values-prod-secrets.yaml, which is never committed. This keeps the configuration DRY and prevents drift.

### Production Debugging Playbook

A structured runbook in docs/runbooks/debug-playbook.md documents common failure modes such as ImagePullBackOff, CrashLoopBackOff, OOMKilled, selector mismatch, and probe failures, with exact investigation commands and recovery steps. It follows a consistent flow: describe -> events -> logs -> endpoints.

### Platform Contracts and Versioning

The library chart and Go service skeleton are governed by explicit contracts in ADR-007 and ADR-009. Services pin a specific library version and can upgrade independently. Backward compatibility is guaranteed within the same major version.

### AI-Native Platform Design

This platform is explicitly designed for a world where AI coding agents contribute to production code. The service-template/ and generate-service.sh script act as guardrails: an AI agent can scaffold a secure, correctly structured service by running the same commands a human would. The agent only needs to generate business logic; the platform provides everything else.

## Getting Started

Prerequisites: Docker, Kind, kubectl, Helm, Go 1.25+, and Make.

```bash
# Clone the repository
git clone https://github.com/Tanmoy095/LogiFlow-Platform.git
cd LogiFlow-Platform

# Run the pre-flight check
make doctor

# Start the full local environment for the hello service
make dev-up

# Verify everything works
curl http://localhost:8080/healthz

# Generate a new service
SERVICE=my-service make generate-service
```

## Tech Stack

| Layer                        | Technologies                        |
| ---------------------------- | ----------------------------------- |
| Language                     | Go 1.25+                            |
| Container Orchestration      | Kubernetes (Kind for local dev)     |
| Package Manager              | Helm 3+                             |
| Service Mesh / Observability | Prometheus, OpenTelemetry (planned) |
| Message Broker               | Apache Kafka (planned)              |
| Database                     | PostgreSQL, pgvector (planned)      |
| Workflow Engine              | Temporal (planned)                  |
| GitOps                       | Argo CD                             |
| Security                     | Kyverno, Keycloak (planned)         |

## Roadmap

- [x] Helm library chart with standardized probes, security, and resources
- [x] Multi-environment values strategy
- [x] One-command local development environment
- [x] DDD-based service skeleton and generation pipeline
- [x] Production debugging playbook
- [x] Platform contracts and versioning policies
- [ ] Real platform SDK implementations (pkg/technical, pkg/shared)
- [ ] Kafka and Temporal integration
- [x] Argo CD app-of-apps structure with environment-specific llm-gateway Applications
- [x] LLM gateway runtime scaffold, health endpoints, container build, and Helm deployment
- [x] LLM gateway typed errors, cancellation handling, validation/refusal policy, and execution metadata
- [ ] AI governance and LLM gateway completion pipeline

## License

This project is licensed under the Apache License 2.0. See LICENSE for details.
