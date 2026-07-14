# ADR-005 — Platform Standardization: Helm Library Chart & Developer Workflow

- **Author:** Aunmoy Dey Tanmoy  
- **Decision Owner:** Aunmoy Dey Tanmoy  
- **Status:** Accepted  
- **Date:** 2026-07-14  
- **Project:** LogiFlow  

---

## 1. Context

LogiFlow is expected to grow from a single `hello` service to 20+ microservices. Two structural problems emerged immediately during Week 2 development.

### 1.1 Fragile, single‑service developer workflow
The Makefile, `dev‑up.sh`, Docker build commands, and Helm paths were hard‑coded for the `hello` service. Adding another service required duplicating scripts or manually editing paths and image names. The `SERVICE` variable was not exported, so nested calls (e.g., `make build` invoked from `dev‑up.sh`) could not see the intended service. This led to duplicated configuration, a fragile environment, and increased cognitive load.

### 1.2 Copy‑paste Kubernetes manifests
Every microservice would need a `deployment.yaml` and a `service.yaml` containing nearly identical labels, selectors, security contexts, probes, and resource limits. A single policy change — for example, enforcing `readOnlyRootFilesystem: true` — would require editing every service’s template. This is a classic recipe for configuration drift, security gaps, and a maintenance burden that grows linearly with the number of services.

Both problems are symptoms of a missing reusable platform layer: one at the developer tooling level, the other at the infrastructure definition level.

---

## 2. Decisions

I made two interdependent decisions that together form the foundation of a scalable internal platform.

### 2.1 Parameterised, Makefile‑driven developer workflow
The Makefile becomes the single source of truth for all build, lint, template, and deploy operations. `dev‑up.sh` no longer duplicates these commands; it delegates to `make build`, `make lint`, `make template`, and `make deploy`. The `SERVICE` environment variable is the primary configuration parameter:

- Exported from the Makefile so that shell scripts and nested `make` invocations inherit it.
- Image names, Dockerfile paths, Helm chart paths, and resource names are all derived from `SERVICE`.

Key implementation details:

- `SERVICE ?= hello` at the top of the Makefile, with `export SERVICE`.
- `IMAGE_NAME = logiflow/$(SERVICE)`, `DOCKERFILE = build/Dockerfile.$(SERVICE)`, `CHART_PATH = deployment/helm/services/$(SERVICE)`.
- `dev‑up.sh` reads `SERVICE` from the environment, falling back to `hello`.
- Removed the `jq` dependency by using `kubectl version --client --short` for portable version detection.

Result: `make dev‑up` (or `SERVICE=shipment make dev‑up`) becomes the universal entry point for every microservice, on any developer machine.

### 2.2 Helm library chart for Kubernetes manifest standardisation
A dedicated Helm library chart (`logiflow-service`, `type: library`) defines reusable named templates for all common Kubernetes resource definitions. Every microservice chart declares it as a dependency and replaces hard‑coded blocks with `{{ include "logiflow.<template>" . }}` calls.

The library chart lives at `deployment/helm/library/logiflow-service/` and provides these templates:

| Template | Purpose |
|----------|---------|
| `logiflow.labels` | Standard Kubernetes recommended labels |
| `logiflow.selectorLabels` | Selector that must match between Deployment and Service |
| `logiflow.name` | Safe, single‑line resource name (uses release name) |
| `logiflow.podSecurityContext` | `runAsNonRoot: true`, `fsGroup: 1000` |
| `logiflow.containerSecurityContext` | Full hardening: `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `runAsNonRoot: true`, `runAsUser: 1000`, `capabilities: drop: ALL`, `seccompProfile: type: RuntimeDefault` |
| `logiflow.resources` | Configurable defaults (100m CPU, 128Mi memory) with `default` function |
| `logiflow.readinessProbe` | HTTP GET `/healthz` with sensible timings |
| `logiflow.livenessProbe` | HTTP GET `/live` with longer thresholds |

#### 2.2.1 Security hardening – specific engineering choices

| Setting | Decision | Rationale |
|---------|----------|-----------|
| `runAsNonRoot: true` (pod + container) | Enforced at both levels | Defence in depth. If one level is missed, the other catches it. Prevents any container from running as root. |
| `readOnlyRootFilesystem: true` | Enforced globally | Prevents writes to the container’s filesystem. Attackers cannot modify binaries or inject code. Services that need writable directories must explicitly override this – an intentional security exception. |
| `allowPrivilegeEscalation: false` | Mandatory | Closes an entire class of privilege‑escalation exploits. No process can gain more capabilities than its parent. |
| `capabilities: drop: ["ALL"]` | Start with zero capabilities | Implements least privilege. Services that need a specific capability must add it individually. |
| `seccompProfile: type: RuntimeDefault` | Added after initial iteration | The container runtime’s default seccomp profile blocks many dangerous system calls (e.g., `reboot`, `mount`). This is now considered Kubernetes security baseline (Restricted Pod Security Standard) and adds zero performance overhead. |
| `runAsUser: 1000` | Explicitly set | Ensures a consistent non‑root UID across all services. Matches the `fsGroup` so mounted volumes are writable. |

These decisions turn the library into an enforceable security policy, not just a convenience template.

#### 2.2.2 Configurable defaults – the escape hatch
Probe paths and resource limits use the `default` function, allowing per‑service overrides via `values.yaml`. This means a service that needs a larger resource footprint or a custom health endpoint can tune itself without forking the library. The platform provides a golden path; deviations are possible but explicit.

---

## 3. Alternatives Considered

### 3.1 Developer workflow

| Alternative | Why rejected |
|-------------|--------------|
| Hard‑coded per‑service scripts | Unmaintainable beyond two services; leads to script duplication. |
| Separate shell script per service | Same problem; no shared logic. |
| Auto‑detecting task runner | Adds magic; a simple `SERVICE` variable is transparent, testable, and easy to debug. |

### 3.2 Manifest standardisation

| Alternative | Why rejected |
|-------------|--------------|
| Copy‑paste templates | Configuration drift, security risk, impossible to audit at scale. |
| Kustomize overlays | Operates post‑render; cannot set defaults inside Helm templates and cannot enforce reuse of template structure. |
| Jsonnet / CUE | Powerful but steep learning curve; not native to Helm, which is already our packaging tool. |
| Custom Go CLI scaffolding | Over‑engineered for current scale. The library chart is a Helm‑native solution that I can later wrap in a CLI if needed. |

---

## 4. Consequences

### 4.1 Positive

- Single source of truth for build logic, Kubernetes labels, security contexts, probes, and resource defaults.
- Policy changes propagate instantly — editing one line in `_helpers.tpl` updates every service on the next deploy.
- Zero‑cost onboarding — a new microservice requires only two template files (which are identical for all services) and a `values.yaml` with its specific configuration.
- Idempotent, CI‑friendly developer workflow that works identically on Linux, macOS, and Windows.
- AI‑agent compatible — an AI can scaffold a new service by filling only a values file, dramatically reducing the risk of generating insecure or misconfigured manifests.
- Auditability — a single file proves that all services comply with security and operational standards.
- Operational clarity — resource names, labels, and selectors follow a deterministic convention, simplifying monitoring, logging, and debugging.

### 4.2 Negative / Risks

- All services must remain Helm charts; a future migration to a different packaging tool would require replacing the library.
- Library changes must be backward‑compatible or carefully coordinated to avoid breaking every service at once.
- Developers must understand Helm’s `include` syntax and the library’s interface. This is a one‑time learning cost that pays for itself as the service count grows.

---

## 5. Validation & Debugging

The entire pipeline was verified end‑to‑end with:

```bash
helm dependency update deployment/helm/services/hello
helm lint deployment/helm/services/hello --namespace logiflow
helm template hello deployment/helm/services/hello \
  --namespace logiflow \
  --set image.repository=logiflow/hello \
  --set image.tag=local \
  --set service.port=8080
make dev-up

During development, I intentionally triggered and resolved the following failures:

| Error encountered | Root cause | Resolution | Lesson |
|-------------------|------------|------------|--------|
| `nil pointer evaluating interface {}.helloMsg` | `values.yaml` missing `config.helloMsg` | Added the missing key | All `.Values` paths used in templates must exist or be guarded with `default`. |
| Invalid resource name (multi‑line) | `logiflow.selectorLabels` (two lines) used as a resource name | Created dedicated `logiflow.name` helper | Templates producing multi‑line output cannot be used as simple values. |
| `no template "logiflow.name" associated` | Library dependency not downloaded | Ran `helm dependency update` | Dependencies must be explicitly fetched before rendering. |
| YAML parse error `could not find expected ':'` | Unquoted `{{ }}` in YAML scalar position | Wrapped expression in double quotes | Always quote template directives in value positions. |

After all fixes, `make dev‑up` consistently:

- Validates the environment
- Creates or reuses a Kind cluster
- Builds and loads the container image
- Lints and renders the Helm chart without errors
- Deploys the `hello` release
- Waits for pod readiness
- Establishes a port‑forward
- Passes a health check

## 6. Success Metrics

- A new microservice can be scaffolded and running locally in under 10 minutes.
- `make dev‑up` succeeds on repeated executions (idempotent).
- A security policy change can be applied to all services by editing one file and redeploying.
- Onboarding a new engineer to the platform takes one command (`make dev‑up`), not a set of written instructions.

## 7. Future Evolution

- Extend the library chart with `_configmap.tpl` and `_secret.tpl` for configuration and secrets management.
- Add PodDisruptionBudget and ServiceMonitor templates to the library.
- Package service charts and host them in a private Helm repository for GitOps‑driven deployments (e.g., via ArgoCD).
- Evolve the Bash‑based `dev‑up.sh` into a Go CLI when the number of services and workflow complexity justify it, while retaining the same Makefile/library principles.

## 8. References

- Library chart source: `deployment/helm/library/logiflow-service/`
- Refactored service chart: `deployment/helm/services/hello/`
- Developer workflow script: `scripts/dev/dev-up.sh`
- Makefile: `Makefile`