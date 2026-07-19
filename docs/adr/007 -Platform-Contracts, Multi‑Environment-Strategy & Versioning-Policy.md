# ADR-007 — Platform Contracts, Multi‑Environment Strategy, Versioning & Platform Maturity

**Date:** 2026-07-19  
**Status:** Accepted  
**Author:** Aunmoy Dey Tanmoy  
**Decision Owner:** Aunmoy Dey Tanmoy  
**Project:** LogiFlow  
**Replaces:** ADR-006 (superseded by this broader contract)

---

## 1. Context

With the shared Helm library chart (`logiflow-service`) now providing a single source of truth for security contexts, labels, probes, and resource defaults, LogiFlow has successfully eliminated duplicated Kubernetes manifests. However, as the number of services and deployment environments grows, several structural gaps became apparent during Week 2:

1. **No explicit contract** – Service teams do not know which values they **must** provide, which are **optional** with safe defaults, and which are **forbidden** because the platform owns them. This ambiguity leads to deployment failures (e.g., missing required fields) or accidental overrides of security settings.

2. **Single‑environment configuration** – Using a single `values.yaml` file per service cannot express the differences between **development**, **staging**, and **production** without resorting to fragile command‑line `--set` overrides or maintaining multiple copies of the entire file. This creates auditability gaps and configuration drift.

3. **Uncontrolled library evolution** – Without a versioning strategy and a backward‑compatibility commitment, a single change to the library chart (e.g., removing a helper, altering a default) could break every consuming service simultaneously. Service teams need the ability to adopt new library versions independently and safely.

4. **No automated enforcement of the contract** – While `helm template` can be run manually, there is no guarantee that every deployment passes through the validation gate. In a growing team, human discipline alone is insufficient.

These gaps are the exact challenges that cause large‑scale platform initiatives to fail. Solving them systematically is what distinguishes a mature internal platform from a collection of shared scripts.

---

## 2. Decisions

I made five interrelated decisions that together form a production‑grade platform governance model.

### 2.1 Explicit Platform Contract

Every LogiFlow service adheres to a published **contract** that defines the boundary between the platform and the application. The contract categorises all configuration values into three groups:

| Category | Examples | Enforcement |
|----------|----------|-------------|
| **Required** | `image.repository`, `image.tag`, `service.port` | Enforced with Helm’s `required` function, e.g., `{{ required "image.repository is required" .Values.image.repository }}`. Missing a required value causes `helm template` to fail with a clear, human‑readable error message – never a cryptic nil‑pointer. |
| **Optional** | Probe paths, resource limits, custom environment variables | These use the `default` function. If omitted, a safe platform‑wide default is applied automatically. Example: `{{ .Values.resources.requests.cpu \| default "100m" }}`. |
| **Forbidden** | Security contexts (`runAsNonRoot`, `seccompProfile`, `capabilities`), labels, selectors, probe structure | These are **not** configurable in the service’s `values.yaml`. They are injected exclusively by the library chart’s named templates. Any attempt to alter them requires modifying the chart’s templates themselves, which is prohibited by the Golden Path. |

**Why `required` instead of nil‑pointer:** Good platforms don’t just fail—they fail beautifully. The `required` function transforms a generic template panic into an actionable instruction, dramatically improving developer experience.

### 2.2 Multi‑Environment Strategy via Overlay Files

Each service maintains a single base configuration file (`values-dev.yaml` or simply `values.yaml`) and a set of **environment‑specific override files**. The base file contains every key the templates expect; the override files contain only the differences for a given environment.

| File | Purpose | Committed to Git |
|------|---------|------------------|
| `values.yaml` (or `values-dev.yaml`) | Complete specification for local development | ✅ Yes |
| `values-staging.yaml` | Overrides for pre‑production testing (e.g., different Kafka brokers, fewer replicas) | ✅ Yes |
| `values-prod.yaml` | Overrides for production (higher resource limits, immutable image tags) | ✅ Yes |
| `values-prod-secrets.yaml` | Sensitive production values (passwords, tokens) | ❌ Never – added to `.gitignore` |

Deployment commands follow the pattern:

```bash
helm upgrade --install <release> <chart> \
  -f values-dev.yaml \
  -f values-<env>.yaml \
  -f values-prod-secrets.yaml \   # only for production
  --namespace logiflow-<env>

  Because Helm merges values files left to right, later files override earlier ones. This keeps the configuration DRY: the base file evolves independently, and environment files are tiny and focused.

Environments are simulated locally using Kubernetes namespaces (logiflow-dev, logiflow-staging, logiflow-prod) within a single Kind cluster. In production, these namespaces map to separate clusters or isolated virtual clusters.

### 2.3 CI‑Integrated Validation Gate
The contract is enforced by a validation pipeline that runs before any deployment. This pipeline is identical whether executed locally or in CI (e.g., GitHub Actions). The steps are:

- helm dependency update – ensure the library is available.
- helm lint – catch YAML and structural errors.
- helm template – render the full manifest. If a required value is missing, the required function stops the pipeline with a clear message. Optional values are filled with defaults; forbidden values are completely absent (as expected).
- Manual inspection (local) or automated diff (CI) of the rendered manifest to confirm correctness.
- helm upgrade --install – only after all previous steps pass.

This same pipeline will be encoded in CI workflows (GitHub Actions) for every service. The CI configuration is considered part of the platform; service teams do not write their own deployment logic.

### 2.4 Backward Compatibility Commitment
All library chart changes within the same major version are guaranteed to be backward‑compatible. This is an explicit commitment, not an aspiration:

- Additive changes only: New helpers, new optional fields, new defaults may be added freely.
- Existing helpers are never removed without a major version bump.
- Output of existing helpers will not change in a way that reduces security or functionality (e.g., removing a required label, relaxing a security setting).
- Default values may be changed only if the new default is equally safe or safer (e.g., tightening a resource limit) and only with a minor version bump accompanied by release notes.
- Deprecation notice is placed in the library README and in template comments for at least one full minor version before any removal. This gives service teams time to migrate.

This commitment ensures that service teams can upgrade the library dependency without fear of silent breakage. It is the foundation of trust between the platform team and the application teams.

### 2.5 Semantic Versioning & Independent Service Upgrades
The library chart follows Semantic Versioning 2.0.0:

- MAJOR (1.0.0) – Breaking changes: removed helpers, renamed templates, altered required values.
- MINOR (0.2.0) – New backward‑compatible helpers, new optional fields, updated defaults.
- PATCH (0.1.1) – Documentation, bug fixes, template formatting.

Each service pins a specific library version in its Chart.yaml:

```yaml
dependencies:
  - name: logiflow-service
    version: "0.1.0"
    repository: "file://../../library/logiflow-service"

## Service Upgrade Strategy

Services may upgrade independently. They can remain on an older version indefinitely, but they will miss security patches and new platform features. To encourage adoption, automated dependency update pull requests (via Renovate or Dependabot) will be added to the CI pipeline. A dashboard tracking library versions across all services will provide visibility.

## 2.6 Golden Path & Separation of Responsibilities

The Golden Path for creating any new service—human or AI‑generated—is:

1. Copy an existing service chart (e.g., `hello`) to a new directory.
2. Update `Chart.yaml` with the service name and description. The library dependency stays unchanged.
3. Edit `values.yaml` (and its environment overrides) to set the container image, port, and any custom environment variables.
4. Replace the `env` block in `templates/deployment.yaml` with service‑specific environment variables. No other template modifications are permitted.
5. Run `helm dependency update` to fetch the library.
6. Validate: `helm lint` → `helm template` → inspect.
7. Deploy with `helm upgrade --install` using the appropriate values files and namespace.

This path was successfully demonstrated with the creation of the `stream-ingestion` service, where only the image, Kafka broker, and topic values were changed. The library provided all security, resource, and labelling policies automatically.

The separation of responsibilities is clear:

- **Platform team** owns the library chart, the contract, the versioning policy, and the CI pipeline.
- **Application teams** own their service’s `values.yaml` files and the tiny service‑specific template adjustments (the `env` block). They do not need to understand or manage infrastructure.

## 3. AI Infrastructure & Agent Integration

This platform architecture is explicitly designed to work with AI coding agents (e.g., Cursor, Copilot, custom internal agents). Without a platform, an AI asked to “create an embedding service” would have to generate hundreds of lines of Kubernetes YAML, including security contexts, probes, labels, and selectors. The probability of hallucination is high.

With the LogiFlow platform:

- The AI only needs to generate a `values.yaml` containing the image, port, and service‑specific config.
- All security and operational standards are inherited from the library chart.
- The contract (required/optional/forbidden) acts as a strict schema, limiting the AI’s output to safe, valid configurations.
- The validation gate (lint + template) provides immediate feedback if the AI makes a mistake.

This dramatically reduces the risk of AI‑generated infrastructure errors and is a direct reason why companies are investing heavily in Internal Developer Platforms as guardrails for AI‑assisted development.

## 4. Platform Maturity & Evolution

The current platform (Helm library chart + values overrides + contract + CI) is stage 3 on a well‑known maturity curve:

1. Bash scripts – manual, fragile.
2. Helm charts – per‑service, duplicated.
3. **Helm library chart** – centralised standards, manual onboarding. *(You are here)*
4. **Go CLI wrapping the library** – `logiflow new-service --type ai --gpu` auto‑generates the chart and values, further reducing developer toil.
5. **Internal Developer Platform (IDP)** – a self‑service portal (Backstage, Crossplane, or custom) where a developer clicks “Create AI Service” and the platform automatically provisions the Git repo, CI/CD, Helm chart, namespace, secrets, RBAC, monitoring dashboards, and alerts.

The trigger to move from stage 3 to stage 4 and beyond will be when:

- The number of services exceeds ~50 and manual copy‑paste becomes a bottleneck.
- Multiple teams request self‑service provisioning.
- Auditors require proof that every service meets a certain standard, and manual verification is no longer feasible.
- AI agents become a primary interface for infrastructure changes, requiring a stricter, machine‑readable contract.

The library chart and contract remain the foundation; the higher stages add automation, discoverability, and governance on top.

## 5. Alternatives Considered

| Alternative | Why Rejected |
|-------------|---------------|
| No contract – everything optional | Leads to configuration drift; security settings can be accidentally omitted or overridden. |
| All values required, no defaults | Overwhelms service teams; forces every service to specify resource limits even when the standard is adequate. |
| Single values file per environment, no inheritance | Duplication of configuration keys; updating a common value requires editing every environment file. |
| Forcing all services to use the latest library version | Blocks independent releases; a library bug can bring down all services simultaneously. |
| Rely on nil‑pointer errors for missing required values | Poor developer experience; error messages are cryptic and waste debugging time. |
| No versioning strategy | Impossible to evolve the library safely; every change is a potential outage. |
| Skip CI validation | Without automated enforcement, the contract is merely advisory. Human discipline does not scale. |

## 6. Consequences

### 6.1 Positive

- **Clarity**: The contract documents exactly what a service team must configure. Onboarding time for a new service (like `stream-ingestion`) was reduced to minutes.
- **Safety**: Forbidden values cannot be accidentally overridden. The library guarantees a consistent security posture across all services.
- **Independence**: Services can adopt new library versions at their own pace. A critical service can remain on a stable version while others adopt experimental features.
- **Evolvability**: The platform team can confidently add new helpers and defaults, knowing existing services will not break.
- **Developer Experience**: `required` gives clear, actionable errors. The CI pipeline ensures broken charts never reach production.
- **AI‑agent compatibility**: The constrained configuration surface and strict validation make the platform safe for AI‑generated infrastructure.
- **Auditability**: A single library file proves that every container runs as non‑root, uses `seccompProfile: RuntimeDefault`, and follows the same labelling scheme.

### 6.2 Negative / Risks

- Contract documentation must be maintained alongside the library. Outdated docs lead to confusion.
- Multiple library versions in the same cluster can cause slightly different security postures. This is mitigated by automated upgrade PRs and dashboards that track library versions.
- Environment files multiply — a service with dev/staging/prod has at least three values files. However, they are all small and focused; the alternative (one giant file with conditionals) is far messier.

## 7. Validation

The contract and multi‑environment strategy were validated with the `stream-ingestion` service.

**Chart creation and dependency resolution:**
```text
cp -r hello stream-ingestion && rm -rf charts/ Chart.lock && helm dependency update

## Contract enforcement
helm lint and helm template were run for all three environments. Missing the required config.kafkaBroker value immediately produced an error with the required function, not a nil‑pointer panic.

## Multi‑environment deployment
The service was deployed into three namespaces (logiflow-dev, logiflow-staging, logiflow-prod) using base + override values files. Each deployment resulted in the expected number of replicas and correct Kafka broker addresses (verified in the rendered manifests).

## Template error handling
An initial deployment failure caused by placing the env: block at the pod spec level (instead of inside the container) was diagnosed via helm template and fixed. This proved that the validation gate catches structural errors before they reach the API server.

## Runtime behaviour
Pods correctly entered ImagePullBackOff due to missing container images, confirming that the Deployment, Service, and environment variable injection were all functional.

## Backward compatibility (designed)
A pre‑release CI check will run helm template against all service charts to confirm backward compatibility before tagging a new library version. This check is defined but not yet implemented in CI.

## Secret protection
values-prod-secrets.yaml is listed in .gitignore and is never committed. The production deployment command appends it as the final -f flag.

## 8. Future Evolution
Contract validation tool: A Go‑based linter will automatically verify rendered manifests against the contract, replacing manual inspection.

CI/CD implementation: The lint → template → deploy pipeline will be implemented as GitHub Actions workflows for every service, with secrets injected from a vault.

Library‑version dashboard: A tool to track which services are on which library version, with automated upgrade PRs (Renovate/Dependabot).

Self‑service CLI: logiflow new-service --type ai --gpu will scaffold a complete service, including environment values and CI config.

Internal Developer Platform (IDP): At ~120 services, the library and CLI will become the engine behind a web portal (Backstage‑like) for self‑service provisioning, policy enforcement, and monitoring.

## 9. References
Library chart: deployment/helm/library/logiflow-service/

Example service chart: deployment/helm/services/hello/

Onboarded service: deployment/helm/services/stream-ingestion/

Golden Path documentation: docs/module-4-golden-path.md

ADR-005: Platform Standardization with Helm Library Charts

ADR-006: Helm Library Contracts & Standardized Service Onboarding (superseded by this ADR)