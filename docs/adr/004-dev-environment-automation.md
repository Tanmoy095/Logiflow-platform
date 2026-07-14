# ADR-004: Developer Environment Automation

- **Status:** Accepted
- **Date:** 2026-06-28
- **Supersedes:** None
- **Relates to:** ADR-002 – Service Networking, ADR-003 – Deployment Strategy
- **Author:** Aunmoy Dey Tanmoy  
- **Decision Owner:** Aunmoy Dey Tanmoy 
- **Project:** LogiFlow  


---

# Business Problem

Every engineer working on LogiFlow requires a fully functional local Kubernetes environment before developing, debugging, or testing platform features.

Without automation, developers must manually execute a sequence of commands:

- Create or verify a Kind cluster
- Build the application image
- Load the image into Kind
- Validate Helm charts
- Deploy the application
- Wait for Kubernetes resources to become Ready
- Verify service health
- Configure port forwarding

Although each step is individually straightforward, together they create a lengthy and error-prone setup process.

Manual setup introduces several operational risks:

- Inconsistent developer environments
- High onboarding effort
- Documentation drift
- Human error during repetitive tasks
- Different workflows between local development and CI
- Difficulty for AI coding agents to bootstrap the project consistently

As LogiFlow grows to multiple microservices and contributors, manual environment preparation becomes increasingly expensive to maintain.

---

# Technical Context

LogiFlow is built on a Kubernetes-native development workflow using:

- Docker
- Kind
- Helm
- kubectl
- Make
- Bash

Each tool performs one stage of the deployment process.

Executing these stages manually requires engineers to remember command ordering, dependency validation, and deployment verification.

Automation should orchestrate these tools while remaining transparent and reproducible.

Rather than replacing Kubernetes tooling, LogiFlow provides a thin automation layer that coordinates existing platform commands.

---

# Options Considered

| Approach                                 | Description                                       | Rejected Because                                                                     |
| ---------------------------------------- | ------------------------------------------------- | ------------------------------------------------------------------------------------ |
| README with manual commands              | Developers follow documentation step-by-step      | Documentation becomes outdated; execution varies between engineers                   |
| Individual developer scripts             | Every engineer maintains personal automation      | Creates inconsistent workflows and no shared source of truth                         |
| Remote shared development cluster        | Develop directly on a shared Kubernetes cluster   | Increased cost, network dependency, reduced isolation, difficult offline development |
| Docker Compose                           | Local container orchestration without Kubernetes  | Does not match production architecture or Kubernetes behavior                        |
| **Single automated development command** | One command orchestrates the complete local setup | **Chosen**                                                                           |

---

# Decision

LogiFlow provides a single, deterministic entry point for local platform setup:

```text
make dev-up
```

(or equivalently)

```text
scripts/dev/dev-up.sh
```

The automation is responsible for:

- Validating required developer tools
- Creating or verifying a Kind cluster
- Building the application image
- Loading images into the cluster
- Linting Helm charts
- Rendering Kubernetes manifests
- Deploying the application
- Waiting for Deployment readiness
- Verifying application health
- Reporting failures with actionable diagnostics

Developers interact with a single command rather than a sequence of platform-specific operations.

The automation becomes the canonical workflow for both local development and continuous integration.

---

# Architecture

```text
               Developer / CI / AI Agent
                         │
                  make dev-up
                         │
        ┌────────────────┴────────────────┐
        ▼                                 ▼
 Validate Toolchain              Ensure Kind Cluster
        │                                 │
        └────────────────┬────────────────┘
                         ▼
                  Build Docker Image
                         │
                         ▼
                Load Image into Kind
                         │
                         ▼
               Helm Lint & Template
                         │
                         ▼
                  Helm Install/Upgrade
                         │
                         ▼
              Wait for Deployment Ready
                         │
                         ▼
             Verify Application Health
```

Automation orchestrates existing tools rather than replacing them.

Each stage performs a single responsibility while the automation coordinates the overall workflow.

---

# Implementation

The current implementation is provided through:

```text
Makefile
scripts/dev/dev-up.sh
```

The workflow performs the following sequence:

1. Validate required dependencies (`docker`, `kind`, `kubectl`, `helm`, `make`)
2. Create the Kind cluster if it does not already exist
3. Build the Docker image
4. Load the image into the Kind cluster
5. Lint and template Helm manifests
6. Deploy or upgrade the Helm release
7. Wait until Kubernetes reports the Deployment as Ready
8. Verify the application's health endpoint
9. Report success or actionable failure messages

The automation is intentionally **idempotent**, allowing repeated execution without requiring manual cleanup.

---

# Consequences

## Positive

### Consistent Development Environment

Every engineer follows the same deployment workflow regardless of operating system or experience level.

### Faster Onboarding

New contributors can bootstrap the project with a single command.

The target onboarding time is under ten minutes.

### Shared Workflow

Developers, CI pipelines, and AI coding agents execute the same automation path, eliminating workflow drift.

### Reduced Human Error

Common validation steps—including Helm linting, manifest rendering, readiness checks, and health verification—are performed automatically.

### Reproducibility

Idempotent automation ensures repeated executions produce the same result without unnecessary manual intervention.

---

## Negative / Trade-offs

### Maintenance Cost

Automation must evolve as Kubernetes, Helm, Docker, and project dependencies change.

### Script Complexity

As additional microservices are introduced, Bash scripts become more difficult to maintain and test.

### Local Resource Usage

Running a complete Kubernetes environment locally requires additional CPU and memory compared to lightweight development approaches.

This trade-off is accepted because local development mirrors production behavior.

---

# Operational Notes

When `make dev-up` fails, investigate in the following order.

## 1. Toolchain Validation

Verify Docker, Kind, Helm, kubectl, and Make are installed and accessible.

---

## 2. Cluster Status

```bash
kind get clusters
```

Ensure the expected Kind cluster exists.

---

## 3. Kubernetes Resources

```bash
kubectl get pods
kubectl get deployments
kubectl get svc
```

Verify workloads have been created successfully.

---

## 4. Deployment Status

```bash
kubectl rollout status deployment/hello
```

Confirm the rollout completed successfully.

---

## 5. Application Logs

```bash
kubectl logs <pod-name>
```

Inspect application startup failures.

---

## 6. Health Verification

Confirm the health endpoint responds successfully after deployment.

This investigation workflow resolves the majority of local environment issues without requiring guesswork.

---

# Success Metrics

This decision is considered successful when:

- A new engineer deploys LogiFlow locally in under ten minutes.
- `make dev-up` succeeds on repeated executions without manual cleanup.
- CI pipelines execute the same automation workflow as developers.
- AI coding agents can bootstrap the development environment using a single command.
- Local environments remain consistent across contributors.

---

# Future Impact

As LogiFlow evolves into a multi-service platform, the Bash automation will be replaced by a dedicated Go CLI.

Example interface:

```text
logiflow dev up
logiflow dev down
logiflow dev status
logiflow dev doctor
```

The Go implementation will provide:

- Structured logging
- Better testability
- Cross-platform distribution
- Rich error reporting
- Plugin architecture for future platform capabilities
- Easier integration with AI development agents

The architectural principle remains unchanged: developers should interact with a single platform entry point regardless of the number of underlying services.

---

# References

- Makefile
- `scripts/dev/dev-up.sh`
- Kind Documentation
- Helm Documentation
- ADR-002: Service Networking
- ADR-003: Deployment Strategy

---

> **Design Principle:** Developer workflows should be automated, deterministic, and reproducible. Engineers—and AI agents—should focus on building software, not remembering deployment commands.
