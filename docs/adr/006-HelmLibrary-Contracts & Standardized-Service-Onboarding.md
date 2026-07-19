# ADR-006 — Helm Library Contracts & Standardized Service Onboarding

**Date:** 2026-07-14  
**Status:** Accepted  
**Author:** Aunmoy Dey Tanmoy  
**Decision Owner:** Aunmoy Dey Tanmoy  
**Project:** LogiFlow

---

## 1. Context

LogiFlow is growing beyond a single `hello` service. Two operational challenges emerged immediately during Week 2:

1. **Trust in generated configuration** – Helm templates produce Kubernetes manifests, but without a rendering step there is no way to inspect what will actually be sent to the cluster. Deploying directly with `helm install` means trusting a black box. A platform engineer must be able to verify the **contract** between the application’s `values.yaml` and the infrastructure templates *before* any resource reaches the API server.

2. **Cost of onboarding a new microservice** – Without a standardized pattern, each new service would copy hundreds of lines of infrastructure YAML (labels, probes, security contexts, resource limits). This leads to configuration drift, security gaps, and a maintenance burden that scales linearly with the number of services. The goal is to make adding a new service a matter of changing only business‑specific configuration – not rewriting infrastructure.

Both problems are rooted in the same missing platform primitive: a **single source of truth for infrastructure definitions**, combined with a **clear contract** between the platform team (which owns the templates) and the application teams (which own the values).

---

## 2. Decisions

I made two interconnected decisions that together form the foundation of a scalable, trustable internal platform.

### 2.1 Helm Template Rendering as a Contract

Helm is treated as a **compiler**, not merely a YAML generator. The rendering pipeline is:

values.yaml + --set overrides → merged Values
↓
templates (with library includes) → Go template engine → final Kubernetes YAML


- **`helm template` is mandatory before every deployment.** It renders the chart locally, producing the exact YAML that Kubernetes will receive. This rendered output is the **contract** between the application team’s configuration and the platform team’s infrastructure.
- **The contract defines two types of values:**
  - **Optional values** use the `default` function in the library templates. If a value is missing, a safe, platform‑defined fallback is used (e.g., resource limits, probe timings).
  - **Required values** have no `default`. If they are missing, the template deliberately fails with a `nil pointer` error. This enforces that security‑critical or mandatory settings (like `runAsNonRoot`, `service.port`, or a required environment variable) must be explicitly provided.

This means every deployment is **transparent**: I can inspect the exact YAML that will be applied, and I can guarantee that certain security settings are never silently omitted.

### 2.2 Standardized Service Onboarding via the Library Chart

New microservices are onboarded by copying an existing service chart (such as `hello`) and changing **only** the `values.yaml` file. The `templates/` directory remains identical for every service, because all infrastructure concerns are already handled by the library chart.

- **The library chart (`logiflow-service`, type: library)** provides all standard Kubernetes definitions: labels, selectors, security contexts, probes, resource defaults, and naming helpers.
- **A new service requires:**
  1. Copy the `hello` chart directory.
  2. Update `Chart.yaml` with the new service’s name.
  3. Edit `values.yaml` to set the container image, service port, any custom environment variables, and optionally override resource limits or probe paths.
  4. Run `helm dependency update` to fetch the library.
  5. Validate with `helm lint` and `helm template`, then deploy.
- **Under no circumstances should the deployment or service templates be edited** for a standard stateless service. The platform owns the infrastructure; the service team owns the configuration. This separation enforces consistency across all services.

This transforms onboarding from a half‑day exercise in copy‑pasting YAML into a **five‑minute configuration change**.

---

## 3. Alternatives Considered

| Alternative | Why rejected |
|-------------|--------------|
| **Each team writes their own Deployment and Service from scratch** | Configuration drift, security gaps, no single source of truth for policies. |
| **Inspect live resources with `kubectl get -o yaml` instead of `helm template`** | `kubectl get` shows current state, not desired state. It cannot catch errors before deployment, and live resources may have been manually modified. |
| **No library chart – use copy‑paste with a wiki template** | Wiki templates quickly become outdated; there is no enforcement mechanism. Humans (and AI agents) will inevitably deviate. |
| **Use a custom CLI scaffolding tool instead of a library chart** | Over‑engineered for the current scale. The library chart is a Helm‑native solution that can later be wrapped in a CLI if needed. |

---

## 4. Consequences

### 4.1 Positive

- **Single source of truth** for all infrastructure policies. A security update (e.g., adding `seccompProfile: RuntimeDefault` or flipping `readOnlyRootFilesystem`) is made once in the library chart and propagates to every service on its next deploy.
- **Zero‑cost onboarding** – a new developer can scaffold a production‑ready, secure microservice by editing one file.
- **Auditability** – a single `_helpers.tpl` file proves that every container runs as non‑root, drops all capabilities, and uses sensible probes.
- **AI‑agent compatible** – when an AI coding agent generates a new service, it only needs to fill a `values.yaml`. The library prevents it from hallucinating insecure or misconfigured manifests.
- **Fast debugging** – the `helm template` command lets me reproduce the exact desired state without touching a cluster. Combined with `helm lint`, most structural problems are caught in seconds.
- **Idempotent, deterministic deployments** – the same `values.yaml` always produces the same manifest.

### 4.2 Negative / Risks

- **All services must remain Helm charts.** A future migration to a different packaging tool would require replacing the library.
- **Library changes are global.** A breaking change in a helper template (e.g., renaming a template or altering a required value) will break the rendering of every service. Therefore, library updates must be backward‑compatible or carefully coordinated.
- **Learning curve** – developers must understand the library’s interface (which values are defaulted, which are required). This is documented in the chart’s `values.yaml` and can be mitigated with inline comments.

---

## 5. Validation & Debugging (Intentional Break‑Fix)

The rendering contract and the onboarding process were validated through deliberate failures that simulate real‑world mistakes. These exercises confirmed that the library behaves predictably and that problems can be diagnosed quickly.

### 5.1 Missing required value (nil pointer)
- **Break:** Removed `config.logLevel` from `values.yaml` while the deployment template referenced it directly.
- **Observation:** `helm template` failed with `nil pointer evaluating interface {}.logLevel`.
- **Fix:** Added the missing key or changed the template to use `default "info"`.
- **Lesson:** Required values must be documented; the template must fail loudly if they are missing.

### 5.2 Deleted library template
- **Break:** Removed the `logiflow.resources` template from `_helpers.tpl`.
- **Observation:** `helm template` errored: `template: no template "logiflow.resources" associated`.
- **Fix:** Restored the template. This proved that every service template has an explicit dependency on the library.
- **Lesson:** The library is a contract; removing a named template breaks all consumers.

### 5.3 Wrong container image (ImagePullBackOff)
- **Break:** Deployed the shipping service with an image tag that did not exist.
- **Observation:** Pod entered `ImagePullBackOff`. `kubectl describe pod` showed pull errors in the events.
- **Fix:** Built the correct image or pointed to a valid tag.
- **Lesson:** Image errors are visible in the pod events; always check `kubectl describe` before investigating deeper.

### 5.4 Selector mismatch
- **Break:** Changed the Service’s selector to a value not matching the pod labels.
- **Observation:** `kubectl get endpoints <service>` returned `<none>`. Pods were running but could not be reached.
- **Fix:** Reverted to the library’s `logiflow.selectorLabels` helper.
- **Lesson:** The library must supply both the Deployment’s pod template labels and the Service’s selector to prevent mismatches.

### 5.5 Incorrect probe path
- **Break:** Set `probes.readiness.path` to a non‑existent endpoint.
- **Observation:** Pod was `Running` but `READY 0/1`. `kubectl describe pod` showed readiness probe failures (HTTP 404).
- **Fix:** Corrected the path to the actual health endpoint.
- **Lesson:** Readiness probes directly control whether a pod receives traffic; a misconfiguration causes an outage even if the process is running.

All failures were resolved using the debugging order: `helm lint` → `helm template` → `kubectl apply` → `kubectl describe pod` / `kubectl logs`.

---

## 6. Future Evolution

- **Extend the library** with `_configmap.tpl` and `_secret.tpl` to standardise configuration and secrets management.
- **Add PodDisruptionBudget and ServiceMonitor templates** for production‑grade resilience and observability.
- **Package service charts** and host them in a private Helm repository for GitOps‑driven deployments (e.g., ArgoCD).
- **Evolve the Bash‑based `dev‑up.sh` into a Go CLI** when the number of services justifies it, while keeping the Makefile/library principles.
- **GPU and AI support:** The resources helper will be extended to include `nvidia.com/gpu` requests, and startup probes will be added for slow‑loading model servers. The same library will serve both standard microservices and AI inference services.

---

## 7. References

- Library chart: `deployment/helm/library/logiflow-service/`
- Standardized service template: `deployment/helm/services/hello/`
- Developer workflow: `scripts/dev/dev-up.sh` and `Makefile`