# ADR-011 — GitOps Deployment Strategy with ArgoCD

**Date:** 2026-07-28  
**Status:** Accepted  
**Author:** Aunmoy Dey Tanmoy  
**Decision Owner:** Aunmoy Dey Tanmoy  
**Project:** LogiFlow  
**Supersedes:** None  
**Related:** ADR-005 (Platform Standardization), ADR-007 (Contracts & Versioning)

---

## 1. Context

LogiFlow is growing toward 15+ microservices, each requiring deployments to multiple environments (development, staging, production). The current deployment model relies on developers running `make dev-up` locally for dev, and manually executing `helm upgrade --install` for staging and production. This imperative approach creates several operational risks:

- **No single source of truth** – the live cluster state can drift from what is defined in Git.
- **No audit trail** – it is impossible to determine who changed a deployment parameter, when, or why.
- **Manual overhead** – every configuration change requires a human to execute a command, increasing toil and the chance of error.
- **Inconsistent environments** – dev, staging, and production can diverge because different people run different commands.
- **Security exposure** – multiple team members need direct `kubectl` access to production clusters.

As the platform evolves toward an AI‑native internal developer platform, it is critical that all deployment operations become deterministic, auditable, and safe for both human and AI agents.

---

## 2. Problem

How should LogiFlow manage the lifecycle of applications across multiple environments in a way that guarantees the cluster state always matches the intended state defined in version control, while eliminating direct human access to production clusters and enabling safe AI‑assisted changes?

---

## 3. Decision

**LogiFlow adopts a GitOps deployment strategy using ArgoCD as the reconciliation engine.**

### 3.1 Git as the Single Source of Truth
The desired state of every application in every environment is declared in Git – not in the cluster, not in a CI pipeline script, and not in an engineer’s terminal. Git becomes the authoritative record of what *should* be running.

### 3.2 ArgoCD as the Reconciliation Engine
ArgoCD is a Kubernetes-native controller that continuously:
1. Watches the Git repository.
2. Renders Helm charts (or Kustomize overlays, plain YAML, etc.) from the declared source.
3. Compares the rendered desired state with the live cluster state.
4. Automatically synchronizes (if `syncPolicy.automated` is set) to eliminate any drift.

ArgoCD itself is configured via Git (the “app of apps” pattern), so the entire deployment infrastructure is itself Git-managed.

### 3.3 Application of Apps for Environment Separation
A single parent Application watches a directory of child Application manifests. Each environment (dev, staging, prod) has its own parent Application and its own directory of child Application YAMLs. Adding a new service or changing an existing one requires only editing or adding the appropriate child Application file and pushing to Git. ArgoCD detects the change and applies it to the correct environment automatically.

**Repository structure:**
deployment/gitops/argocd/
├── parent-dev.yaml # watches apps-dev/
├── parent-staging.yaml # watches apps-staging/
├── parent-prod.yaml # watches apps-prod/
├── apps-dev/
│ ├── hello-app.yaml
│ ├── stream-ingestion-app.yaml
│ └── ...
├── apps-staging/
│ └── ...
└── apps-prod/
└── ...

text

Each child Application specifies:
- The Git repository and path to the Helm chart.
- The environment-specific values file (e.g., `values-dev.yaml`, `values-prod.yaml`).
- The destination cluster and namespace.
- The sync policy (automated, with `prune` and `selfHeal` enabled).

### 3.4 Sync Policy: Automated, Prune, Self‑Heal
All LogiFlow applications are configured with:
- `automated` – changes in Git are applied without manual intervention.
- `prune: true` – resources removed from Git are automatically deleted from the cluster.
- `selfHeal: true` – any manual `kubectl` edit in production is automatically reverted to match Git within minutes.

### 3.5 CI Role: Validate, Never Deploy
CI pipelines (GitHub Actions) will lint and render (`helm template`) the charts to validate correctness, but they will never execute `helm install` or `kubectl apply` against a cluster. Deployment is solely the responsibility of the GitOps controller running inside the cluster. This separates the validation concern (CI) from the delivery concern (ArgoCD), following the principle of least privilege.

### 3.6 Local Development Remains Imperative
For fast iteration on a developer’s laptop, `make dev-up` and `helm upgrade --install` remain the recommended workflow. GitOps governs shared environments (staging and production), not personal development clusters.

---

## 4. Consequences

### 4.1 Positive

- **Complete auditability** – every deployment is a Git commit with author, timestamp, and message.
- **Instant rollback** – `git revert` and push. ArgoCD returns the cluster to the previous state without any direct cluster interaction.
- **Self‑healing** – manual changes in production are automatically reverted. The cluster always converges to Git.
- **Reduced attack surface** – developers and CI pipelines only need Git access; they never need `kubectl` credentials for production clusters.
- **Environment consistency** – the same mechanism (app of apps) deploys to dev, staging, and prod, eliminating environment drift.
- **AI‑agent safety** – AI agents can propose changes via pull requests; they never touch the cluster directly. Git serves as the safety boundary.
- **Operational simplicity** – adding a new service requires only a Helm chart and three child Application YAML files. A single commit deploys to all environments.

### 4.2 Negative / Risks

- **Initial setup complexity** – ArgoCD must be installed and the parent Applications bootstrapped. This is a one‑time cost.
- **ArgoCD becomes critical infrastructure** – if ArgoCD is unavailable, automatic deployments stop. However, the cluster continues to run; only changes are paused. High availability of ArgoCD is a future consideration.
- **Secrets management** – sensitive values (passwords, tokens) cannot be stored in Git. An external secret management solution (Sealed Secrets, External Secrets Operator, or a vault) must be adopted alongside GitOps.
- **Learning curve** – developers must learn to work with Git as the deployment interface, and understand the GitOps reconciliation loop.

---

## 5. Alternatives Considered

| Alternative | Why Rejected |
|-------------|---------------|
| **Push‑based CI/CD (CI runs `helm upgrade`)** | CI requires direct cluster access, creating a security risk. The CI pipeline becomes the deployment bottleneck; if CI is down, deployments halt. No self‑healing or continuous reconciliation. |
| **Manual `helm install` / `kubectl apply`** | No audit trail, high risk of configuration drift, unscalable beyond a handful of services. |
| **Using only `make dev-up` for all environments** | Not suitable for production; requires local tools and cluster access. No separation between dev and prod. |
| **Kustomize with a push‑based model** | Similar security and drift issues; Kustomize does not provide a reconciliation loop. |
| **FluxCD (alternative GitOps controller)** | FluxCD is a valid alternative, but ArgoCD has a richer UI, more mature application‑centric model, and is the industry‑standard choice for the use case. The underlying GitOps principles are identical; switching between them is a configuration change, not an architectural one. |

---

## 6. Why GitOps Aligns with LogiFlow’s Long‑Term AI Platform Vision

LogiFlow is designed from the ground up to be an AI‑native internal developer platform. In that future:

- **AI coding agents** will generate service scaffolds, Helm values, and even infrastructure changes.
- Those agents cannot be trusted with direct `kubectl` access to production clusters—the blast radius of a bad AI‑generated command is too high.
- Git acts as the **safety boundary**: an AI agent proposes a change via a pull request. CI validates the Helm chart (`helm lint`, `helm template`). A human reviews and merges. ArgoCD deploys automatically.
- If the deployed change causes an incident, a `git revert` instantly rolls back the entire change across all affected environments.
- The reconciliation loop ensures that even if an operator manually patches something during an outage, the system will converge back to the audited Git state once the emergency is resolved.

This pattern—**AI proposes, Git gates, ArgoCD deploys**—is the same model used by leading AI infrastructure companies to scale safely. It turns Git into the control plane for both human and AI‑driven operations, making the platform resilient, auditable, and secure as automation increases.

---

## 7. Implementation Status

- [x] Parent Applications for `logiflow-dev`, `logiflow-staging`, and `logiflow-prod` defined.
- [x] Child Application manifests for `hello` and `stream‑ingestion` created in all three environment directories.
- [x] GitOps flow manually simulated using `helm template | kubectl apply` to validate the concept.
- [ ] ArgoCD controller installed on the target cluster(s).
- [ ] Parent Applications deployed and bootstrapped.
- [ ] External secrets management integrated for production secrets.

---

## 8. References

- GitOps configuration: `deployment/gitops/argocd/`
- Parent Applications: `parent-dev.yaml`, `parent-staging.yaml`, `parent-prod.yaml`
- Child Applications: `apps-dev/`, `apps-staging/`, `apps-prod/`
- GitOps README: `deployment/gitops/argocd/README.md`
- Helm library chart (used by all services): `deployment/helm/library/logiflow-service/`
- ADR-005: Platform Standardization with Helm Library Charts
- ADR-007: Platform Contracts & Versioning