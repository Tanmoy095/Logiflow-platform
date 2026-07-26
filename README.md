# LogiFlow — AI‑Powered Operational Intelligence Platform

[![Go Version](https://img.shields.io/badge/go-1.22%2B-blue.svg)](https://golang.org)
[![Kubernetes](https://img.shields.io/badge/kubernetes-v1.29%2B-blue.svg)](https://kubernetes.io)
[![Helm](https://img.shields.io/badge/helm-v3.14%2B-blue.svg)](https://helm.sh)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

**LogiFlow is a production‑grade internal developer platform for multi‑tenant AI workloads.**  
It provides a **Golden Path** for building, deploying, and operating microservices on Kubernetes—from a single‑command local environment to a GitOps‑driven multi‑cluster production setup. Every decision in this repository is guided by **Domain‑Driven Design (DDD), platform engineering, and deploy‑first principles**, ensuring that the platform scales from 2 to 200 services without structural drift.

> **For recruiters & hiring managers:** This project demonstrates the ability to design and implement a complete internal platform, including Helm library charts, opinionated health‑check policies, multi‑environment strategies, automated service scaffolding, DDD‑enforced code skeletons, and CI‑ready workflows. It is built with the same engineering patterns used at Stripe, Uber, Spotify, and Netflix.

---

## 🏗️ High‑Level Architecture
┌────────────────────────────────────────────────────────────────────┐
│ Developer / AI Agent │
└─────────────┬──────────────────────────────────────────────────────┘
│ make dev-up │ SERVICE=foo make generate-service
▼ ▼
┌──────────────────────────────┐ ┌─────────────────────────────────┐
│ Platform Automation │ │ Service Template + Generator │
│ (doctor.sh, dev-up.sh, │ │ (service-template/, │
│ Makefile) │ │ cmd/template/) │
└──────────────┬───────────────┘ └──────────────┬──────────────────┘
│ │
▼ ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Kubernetes Cluster (Kind or Cloud) │
│ │
│ ┌───────────────────────────┐ ┌───────────────────────────────┐ │
│ │ Helm Library Chart │ │ Per‑service Helm Charts │ │
│ │ (labels, probes, │ │ (values.yaml overrides per │ │
│ │ security, resources) │ │ dev/staging/prod) │ │
│ └───────────────────────────┘ └───────────────────────────────┘ │
│ │
│ ┌───────────────────────────────────────────────────────────────┐ │
│ │ Services (Go) │ │
│ │ cmd/<service>/main.go ◄── Composition Root │ │
│ │ services/<service>/domain/ ◄── Pure Business Rules │ │
│ │ services/<service>/application/ ◄── Use Cases & Ports │ │
│ │ services/<service>/interfaces/ ◄── HTTP/gRPC/Kafka/MCP │ │
│ │ services/<service>/infrastructure/ ◄── DB/Cache/Provider │ │
│ └───────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────┘


**Key design principles:**
- **DDD boundaries are visible in the filesystem.** Every service’s folder tree tells you exactly what layer a piece of code belongs to.
- **Platform owns infrastructure; developers own business logic.** The Helm library chart and Go service skeleton enforce this contract.
- **Deploy‑first.** Every service has probes, resource limits, security contexts, and a local Kind smoke test.
- **AI‑agent compatible.** Standardised templates and generation scripts let AI agents safely scaffold new services.

---

## 📁 Repository Structure
.
├── cmd/ # Composition roots (one binary per service)
│ ├── hello/main.go
│ ├── stream-ingestion/main.go
│ └── template/ # Skeleton for new services
├── services/ # Business logic (DDD layers)
│ ├── stream-ingestion/
│ │ ├── domain/ # Entities, value objects, domain events
│ │ ├── application/ # Use cases, ports (interfaces), services
│ │ ├── interfaces/ # HTTP, gRPC, Kafka, MCP, Temporal adapters
│ │ └── infrastructure/ # PostgreSQL, Redis, Kafka, provider clients
│ └── (future services …)
├── service-template/ # Reusable DDD skeleton (copied by generator)
│ └── (same structure as services/<service>/)
├── pkg/ # Platform SDK (shared across services)
│ ├── foundation/ # Pure helpers (config, retry, clock, ids)
│ ├── technical/ # Reusable technical adapters (logging, metrics, server, gRPC, Kafka)
│ └── shared/ # Cross‑service contracts (tenant, auth, events)
├── deployment/ # Kubernetes manifests & GitOps
│ └── helm/
│ ├── library/logiflow-service/ # Opinionated platform library chart
│ └── services/<service>/ # Per‑service charts (values overlays)
├── dev/ # Local development environment
│ └── kind/kind-config.yaml # Kind cluster definition
├── scripts/ # Automation & developer tooling
│ ├── dev/doctor.sh # Pre‑flight environment validation
│ ├── dev/dev-up.sh # One‑command local environment
│ └── dev/generate-service.sh # Service generator
├── docs/ # Engineering evidence
│ ├── adr/ # Architecture Decision Records
│ ├── runbooks/ # Debugging playbooks
│ └── …
├── Makefile # Unified developer interface
└── README.md


### 🔍 Detailed Breakdown

#### `cmd/<service>/main.go` — Composition Roots
The single entry point for each microservice. Its **only** job is to load configuration, initialise observability, create infrastructure adapters, instantiate application services, and start servers. It contains **zero business logic**. This strict separation makes it trivial to swap databases, add new transports (gRPC, Kafka), or change logging libraries without touching business code.

#### `services/<service>/` — DDD Layered Architecture
Every service follows the same four‑layer structure, enforced by the `service‑template/`:
- **`domain/`** — Pure business rules. Imports only the Go standard library and `pkg/foundation`. Never touches databases, HTTP, or messaging.
- **`application/`** — Use cases and ports. Defines interfaces that infrastructure must implement. Orchestrates domain entities.
- **`interfaces/`** — Adapters that convert external requests (HTTP, gRPC, Kafka messages, Temporal activities) into application commands.
- **`infrastructure/`** — Concrete implementations of application ports: PostgreSQL repositories, Redis caches, Kafka producers, third‑party API clients.

This structure **physically prevents** common architectural mistakes (e.g., SQL queries inside domain entities).

#### `service-template/` & `scripts/dev/generate-service.sh`
Adding a new service is a single command:
```bash
SERVICE=fraud-detection make generate-service

The script copies the DDD skeleton, sets up the Go module with correct import paths, and creates the composition root—all in seconds. AI coding agents can execute the same command, ensuring they never invent inconsistent folder structures.

pkg/ — Platform SDK
foundation — Pure, business‑agnostic utilities safe for even domain to import (config parsers, retry backoffs, ULID generators).

technical — Reusable infrastructure adapters that know about protocols but not business rules (structured logging, Prometheus metrics, gRPC interceptors, Kafka wrappers, HTTP server helpers). These are the building blocks that make every service production‑ready.

shared — Cross‑service contracts that must be identical everywhere (tenant context propagation, JWT auth helpers, the canonical event envelope). This is what makes 15 services behave as one system.

deployment/helm/library/logiflow-service/ — Helm Library Chart
A single source of truth for Kubernetes standards. Every service chart imports this library to inherit:

Standard labels and selectors

Opinionated security contexts (runAsNonRoot, readOnlyRootFilesystem, seccompProfile, drop ALL capabilities)

Startup, readiness, and liveness probes with fixed timings (service‑specific paths only)

Default resource requests and limits

PodDisruptionBudget and ServiceMonitor placeholders

This means a security policy update is a one‑line change that propagates to all 15+ services on their next deploy.

deployment/helm/services/<service>/ — Per‑Service Charts
Service charts are thin wrappers that only provide a values.yaml with the container image, port, and environment variables. All infrastructure is inherited from the library. Environment‑specific overrides (values‑staging.yaml, values‑prod.yaml) and secrets (values‑prod‑secrets.yaml, never committed) follow the same pattern.

dev/ and scripts/ — Local Developer Experience
doctor.sh — Pre‑flight check that validates Docker, Kind, kubectl, Helm, Go versions, branch names, and port availability. It catches “works on my machine” problems before a single build runs.

dev-up.sh — One command that creates a Kind cluster, builds the Docker image, loads it into Kind, lints and templates the Helm chart, deploys, waits for readiness, port‑forwards, and runs a health check. Idempotent and deterministic.

Makefile — The universal entry point: make dev-up, make lint, make template, make generate-service, make status, make logs.

docs/ — Engineering Evidence
Contains Architecture Decision Records (ADRs) for every major design choice, production debugging runbooks, and portfolio evidence. This is the “why” behind the code.

🎯 The Golden Path — Creating a New Service
Generate the skeleton

bash
SERVICE=my-service make generate-service
This creates cmd/my-service/main.go and services/my-service/ with all DDD layers.

Configure the chart
Copy deployment/helm/services/hello/ to deployment/helm/services/my-service/, then edit values-dev.yaml (and staging/prod overrides) to set the image, port, and any custom environment variables. The deployment.yaml template remains identical—it only contains include calls to the library.

Implement business logic
Fill in domain/, application/, interfaces/, and infrastructure/ with real code. The platform guarantees that logging, metrics, health checks, and graceful shutdown are already handled.

Deploy locally

bash
SERVICE=my-service make dev-up
This builds the image, loads it into Kind, lints the Helm chart, deploys, and starts a port‑forward. The service is reachable at http://localhost:8080/healthz.

⚙️ Platform Engineering Highlights
📋 Opinionated Health‑Check Strategy
Every LogiFlow container gets a startup, readiness, and liveness probe from a single logiflow.probes template. Developers only configure the probe paths; the platform owns all timing parameters. This prevents unsafe manual overrides and eliminates CrashLoopBackOff for slow‑loading AI models.

🔐 Secure by Default
All containers run as non‑root, with a read‑only root filesystem, all capabilities dropped, and seccompProfile: RuntimeDefault applied. These settings are enforced by the library chart and cannot be overridden by individual services.

🌍 Multi‑Environment Strategy
One chart, many environments. Base configuration (values-dev.yaml) is overridden by environment‑specific files (values-staging.yaml, values-prod.yaml) and secrets (values-prod-secrets.yaml, never committed). This keeps the configuration DRY and prevents drift.

🐞 Production Debugging Playbook
A structured runbook (docs/runbooks/debug-playbook.md) documents every common failure mode (ImagePullBackOff, CrashLoopBackOff, OOMKilled, selector mismatch, probe failures) with exact investigation commands and recovery steps. It follows a consistent flow: describe → events → logs → endpoints.

🔄 Platform Contracts & Versioning
The library chart and Go service skeleton are governed by explicit contracts (ADR‑007, ADR‑009). Services pin a specific library version and can upgrade independently. Backward compatibility is guaranteed within the same major version.

🤖 AI‑Native Platform Design
This platform is explicitly designed for a world where AI coding agents contribute to production code. The service-template/ and generate-service.sh script act as guardrails: an AI agent can scaffold a secure, correctly‑structured service by running the same commands a human would. The agent only needs to generate business logic; the platform provides everything else. This dramatically reduces hallucination risk and enforces architectural consistency at scale.

🚀 Getting Started
Prerequisites: Docker, Kind, kubectl, Helm, Go 1.22+, Make

bash
# Clone the repository
git clone https://github.com/Tanmoy095/LogiFlow-Platform.git
cd LogiFlow-Platform

# Run the pre‑flight check
make doctor

# Start the full local environment for the hello service
make dev-up

# Verify everything works
curl http://localhost:8080/healthz
Generate a new service:

bash
SERVICE=my-service make generate-service
🧰 Tech Stack
Layer	Technologies
Language	Go 1.22+
Container Orchestration	Kubernetes (Kind for local dev)
Package Manager	Helm 3+
Service Mesh / Observability	Prometheus, OpenTelemetry (planned)
Message Broker	Apache Kafka (planned)
Database	PostgreSQL, pgvector (planned)
Workflow Engine	Temporal (planned)
GitOps	Argo CD (planned)
Security	Kyverno, Keycloak (planned)
🗺️ Roadmap
☑ Helm library chart with standardised probes, security, resources
☑ Multi‑environment values strategy
☑ One‑command local development environment
☑ DDD‑based service skeleton & generation pipeline
☑ Production debugging playbook
☑ Platform contracts & versioning policies
□ Real platform SDK implementations (pkg/technical, pkg/shared)
□ Kafka & Temporal integration
□ GitOps with Argo CD
□ AI governance & LLM gateway
📜 License
This project is licensed under the Apache License 2.0. See LICENSE for details.