# ADR-008 — Production Health Check Strategy & Opinionated Probe Library

**Date:** 2026-07-26  
**Status:** Accepted  
**Author:** Aunmoy Dey Tanmoy  
**Decision Owner:** Aunmoy Dey Tanmoy  
**Project:** LogiFlow  
**Supersedes:** ADR-006 (Health Check Strategy – initial probe definitions)

---

## 1. Context

As LogiFlow expands toward 20+ microservices, including AI‑intensive workloads with long startup times, the platform must guarantee consistent, safe health‑check behaviour across every deployment. Two distinct problems were observed:

1. **Slow‑starting containers are killed prematurely** – Services that load large models (embedding, LLM inference) require 45‑120 seconds before they can serve traffic. Without a **startup probe**, the default liveness probe starts too early and terminates the container, causing an endless restart loop (`CrashLoopBackOff`). The application never reaches a healthy state despite being otherwise functional.

2. **Health‑check configuration drifts across services** – Even with a shared Helm library providing probe helpers, each service’s `deployment.yaml` still references three separate `include` blocks for `startupProbe`, `readinessProbe`, and `livenessProbe`. Developers must remember to include all three; they can accidentally swap paths, omit a probe, or (if allowed) override timing values with unsafe numbers. This leads to inconsistent production behaviour and increased cognitive load.

Both problems are symptoms of a missing platform‑enforced contract for health checks. The underlying need is to move from *“services own their probe definitions”* to *“the platform guarantees a complete, hardened set of health checks, and services only declare the endpoints they expose.”*

---

## 2. Decisions

I made three interrelated architectural decisions that together form an opinionated, scalable health‑check policy.

### 2.1 Three‑Probe Model as Platform Standard

Every LogiFlow container will be deployed with **startup**, **readiness**, and **liveness** probes. The responsibilities are strictly separated:

| Probe | Question answered | Failure action | Typical use |
|-------|-------------------|----------------|-------------|
| **Startup** | Has initialisation finished? | Restart container (if still failing) | Slow model loading, database migrations |
| **Readiness** | Can the pod safely receive traffic? | Remove from Service endpoints | Temporary dependency loss, overload |
| **Liveness** | Is the process still alive and responsive? | Restart container | Deadlocks, memory corruption |

The startup probe is mandatory even for fast‑starting services because it provides a uniform initialisation gate and prevents the liveness probe from interfering before the application is fully running.

### 2.2 Single, Bundled Probe Library (`logiflow.probes`)

The three individual library helpers (`logiflow.startupProbe`, `logiflow.readinessProbe`, `logiflow.livenessProbe`) are replaced with a single, combined template:

```yaml
{{- define "logiflow.probes" -}}
startupProbe:
  httpGet:
    path: {{ .Values.probes.startup.path | default "/startupz" }}
    port: {{ .Values.service.port }}
  periodSeconds: 5
  failureThreshold: 30
readinessProbe:
  httpGet:
    path: {{ .Values.probes.readiness.path | default "/healthz" }}
    port: {{ .Values.service.port }}
  initialDelaySeconds: 3
  periodSeconds: 5
  failureThreshold: 2
livenessProbe:
  httpGet:
    path: {{ .Values.probes.liveness.path | default "/live" }}
    port: {{ .Values.service.port }}
  initialDelaySeconds: 5
  periodSeconds: 10
  failureThreshold: 3
{{- end }}

### This template is placed in a dedicated file templates/_probes.tpl inside the library chart for maintainability and auditability. The deployment template now contains a single line:

{{ include "logiflow.probes" . | nindent 10 }}


The service developer can no longer omit a probe or include them partially – the platform delivers the complete, correct set.

2.3 Platform-Owned Timings, Service-Owned Paths
All probe timing parameters (periodSeconds, failureThreshold, initialDelaySeconds) are fixed by the platform and are not overridable by individual services. These values were chosen after testing with real workloads and represent a safe balance between responsiveness and tolerance.

Only the probe paths may be customised via the service’s values.yaml:

probes:
  startup:
    path: /startupz
  readiness:
    path: /healthz
  liveness:
    path: /live

If a service omits a path, the platform’s default is used. This separation ensures that:

SRE teams can tune timings globally and have the change propagate to all services instantly.

Developers only think about which endpoints their application exposes – a business concern.

# ADR-008 — Consequences, Alternatives Considered, and Validation

## 3. Alternatives Considered

| Alternative | Why Rejected |
|-------------|---------------|
| No startup probe; rely on extended `initialDelaySeconds` for liveness | Does not adapt to variable startup times; still risks premature kills if startup occasionally exceeds the fixed delay. |
| Three separate library helpers with full configurability | Leaves room for drift; developers can forget a probe or misconfigure timings. No single source of truth for the complete health policy. |
| Let developers write raw probe YAML in every service | Obvious scaling and reliability issues; inconsistent behaviour across services; high cognitive load. |
| Use TCP probes instead of HTTP | Less informative; an open port does not guarantee the application logic is healthy. |
| Hard-code probe paths in the library (no per-service override) | Too inflexible; different frameworks use different health endpoints (e.g., `/ready`, `/healthy`). Business logic should be able to declare its own paths. |

## 4. Consequences

### 4.1 Positive

- **Elimination of CrashLoopBackOff for AI workloads** – The startup probe gives model‑loading services enough time to initialise, preventing unnecessary restarts.
- **Zero‑drift health checks** – Every service inherits the exact same probe structure and timings, making monitoring, alerting, and debugging predictable.
- **Single point of change** – If the SRE team decides to add a new probe, adjust timings, or enforce a new health endpoint, they change **one file** (`_probes.tpl`). All 200+ services pick it up on their next deploy.
- **Developer experience** – A new service only needs to declare its health endpoints in `values.yaml`; it does not need to know what a “failureThreshold” is.
- **AI‑agent safety** – When an AI coding agent scaffolds a new service, it only generates a few lines of path configuration. The platform prevents the agent from inventing insecure probe settings, dramatically reducing hallucination risk.
- **Auditability** – A single file proves that every container runs with a full set of production‑grade health checks.

### 4.2 Negative / Risks

- **Timing inflexibility** – A very small number of services (e.g., extremely slow model loaders) may need longer startup probes than the platform default. This can be handled by a future extension that allows a documented exception mechanism (e.g., a `probes.startup.failureThreshold` override with an explicit approval flag), but for now we accept that edge cases will be rare.
- **Library coupling** – All services must consume the library chart. A non‑Helm service would need to replicate the probe policy manually.

## 5. Validation

The health‑check strategy was validated through a series of intentional failure experiments on a Kind‑based LogiFlow development cluster.

### 5.1 Startup probe failure → CrashLoopBackOff

```bash
helm upgrade --install hello-dev deployment/helm/services/hello \
  -f values-dev.yaml --set probes.startup.path=/nonexistent \
  --namespace logiflow-dev


  Pod entered CrashLoopBackOff.

kubectl describe pod showed Startup probe failed: HTTP probe failed with statuscode: 404.

Fix: revert path to a valid endpoint; pod recovers immediately.

5.2 Readiness probe failure → Pod removed from Service

helm upgrade --install hello-dev ... --set probes.readiness.path=/nonexistent

Pod showed READY 0/1.

kubectl get endpoints returned <none> for the service.

Container was not restarted; traffic was simply blocked.

Fix: correct the path; pod becomes Ready without restart.

Pod showed READY 0/1.

kubectl get endpoints returned <none> for the service.

Container was not restarted; traffic was simply blocked.

Fix: correct the path; pod becomes Ready without restart.

5.3 Liveness probe failure → Container restart
bash
helm upgrade --install hello-dev ... --set probes.liveness.path=/nonexistent
Container was killed and restarted.

kubectl describe pod showed liveness probe failure and subsequent container kill.

5.4 Template rendering validation
bash
helm template hello deployment/helm/services/hello \
  -f values-dev.yaml --namespace logiflow > manifest.yaml
Confirmed all three probes present with correct hard‑coded timings and overridden paths.

helm lint passed without warnings.

5.5 Multi‑service consistency
Both hello and stream-ingestion services were deployed with the identical logiflow.probes include, confirming that a single library change updates all consumers.

6. Future Evolution
Allow timing overrides via explicit exception mechanism for services that can demonstrate a genuine need (e.g., a GPU model loading for 10 minutes). This would be controlled by a flag in values.yaml and reviewed by the platform team.

Add gRPC health probes for services that use gRPC instead of HTTP.

Integrate with service mesh (e.g., Istio) to use mTLS‑aware probes.

Extend the probe library with readinessGates for conditional traffic routing based on external dependencies.

Implement automated compliance checks that verify every running pod’s probe configuration matches the platform policy, closing the gap between desired and live state.

7. References
Library chart: deployment/helm/library/logiflow-service/

Probe library: deployment/helm/library/logiflow-service/templates/_probes.tpl

Example service: deployment/helm/services/hello/

Debugging runbook: docs/runbooks/runbook-debugging-probes.md