# ADR-012 — Load Balancer Strategy for LogiFlow

**Date:** 2026-09-01  
**Status:** Accepted  
**Author:** Aunmoy Dey Tanmoy  
**Decision Owner:** Aunmoy Dey Tanmoy  
**Project:** LogiFlow  
**Related:** ADR-005 (Platform Standardization), ADR-007 (Contracts and Versioning), ADR-011 (GitOps Deployment Strategy)

---

## Context

LogiFlow consists of multiple microservices (`hello`, `stream-ingestion`, `llm-gateway`, and soon many more), all deployed on Kubernetes. These services must communicate with each other reliably and, eventually, with external clients through a public API. The platform needs a clear, scalable strategy for routing traffic to the correct service and the correct healthy pod.

Kubernetes provides several load‑balancing options:

- **ClusterIP** – internal L4 load balancing using iptables/IPVS.
- **NodePort** – exposes a service on a static port on each node.
- **LoadBalancer** – provisions an external cloud load balancer.
- **Ingress / Gateway API** – L7 routing that understands HTTP paths, headers, and advanced traffic policies.

Each option carries different performance, security, and operational trade‑offs. Without a standardised strategy, teams might pick the wrong tool: exposing internal services through cloud load balancers, hard‑coding NodePorts, or creating path‑based routing with raw L4 solutions that cannot support it.

---

## Problem

How should LogiFlow route traffic:

1. Between internal microservices?
2. From external clients to the correct service, especially when multiple services share a single public entry point?
3. During progressive delivery (canary, blue‑green) where traffic must be split based on HTTP headers, cookies, or weight percentages?

A consistent load‑balancer strategy is required to ensure security, cost efficiency, and operational simplicity across all environments.

---

## Decision

LogiFlow adopts a **layered load‑balancing strategy**:

### 1. Internal service‑to‑service communication uses ClusterIP (L4)

All microservices communicate with each other via Kubernetes **ClusterIP Services**. This is the default Service type and provides:

- A stable virtual IP and DNS name.
- Kernel‑level L4 load balancing with iptables/IPVS.
- Low latency (microseconds) and high throughput.
- No external exposure, which reduces the attack surface.

**Example:**  
`llm-gateway` calls `stream-ingestion` using `stream-ingestion.logiflow.svc.cluster.local:8080`.

**Why L4?**  
Internal traffic does not need path‑based routing or header inspection. It only needs reliable, fast, and secure forwarding to ready pods. Layer 4 is the correct abstraction for this use case.

### 2. External client traffic uses L7 Ingress (or Gateway API) when public exposure is needed

For external clients or path‑based routing across multiple services, LogiFlow will use an **Ingress controller** (e.g., NGINX, Traefik) or the Kubernetes Gateway API. This L7 layer provides:

- Host and path‑based routing (`/embeddings` → embedding-service, `/generate` → llm-gateway).
- TLS termination.
- Header/cookie inspection.
- Weighted traffic splitting for canary releases.
- Rate limiting and authentication policies at the edge.

**Current status:** LogiFlow is currently internal‑only. No public API exists yet. Therefore, no Ingress controller is deployed in this iteration. When external access or advanced routing is required, the platform will introduce an L7 Ingress without modifying existing internal ClusterIP Services.

### 3. Progressive delivery uses L7 features when required

Canary and blue‑green deployments that require percentage‑based traffic splitting cannot be achieved with pure L4. LogiFlow will use the Ingress layer’s canary annotations or a service mesh (Istio/Linkerd) when such release strategies are needed. Until then, rolling updates with `maxUnavailable: 0` and `maxSurge: 1` provide sufficient safety.

### 4. Health checks protect traffic routing at all layers

Readiness probes are the universal health signal. Kubernetes uses them to add or remove pods from Service endpoints. The L7 Ingress also respects the same readiness state. A pod that fails readiness is automatically excluded from both L4 and L7 load balancing.

The smoke test (`scripts/dev/smoke-k8s.sh`) validates that the service responds correctly to a real HTTP request after deployment. This dynamic check complements the static reconciliation provided by GitOps (ArgoCD).

---

## Rationale

This layered approach is the industry standard for Kubernetes platforms:

- **L4 for east‑west traffic** keeps internal communication fast and simple.
- **L7 for north‑south traffic** provides the intelligent routing needed at the edge.
- **No premature complexity** – L7 is introduced only when external or path‑based routing becomes a real requirement.

It also aligns with the platform’s GitOps and AI‑safety principles:

- The load balancer configuration is declared in Git and applied by ArgoCD.
- AI agents cannot accidentally create a `LoadBalancer` service and expose a service to the internet because the platform does not include such manifests by default.
- The contract enforces that all services use `ClusterIP` unless an explicit exception is approved.

---

## Consequences

### Positive

- **Security:** Internal services are never exposed to the internet by accident.
- **Performance:** L4 ClusterIP provides fast, low‑latency east‑west communication.
- **Simplicity:** Developers only need to know the service DNS name. No special routing logic.
- **Cost control:** No cloud load balancers are provisioned for internal traffic.
- **Future‑proof:** The platform can adopt L7 Ingress or a service mesh for canary and path‑based routing without re‑architecting existing services.
- **AI‑agent safety:** The default service type is ClusterIP. AI‑generated service skeletons inherit this safe default. An AI cannot accidentally create an external `LoadBalancer` unless explicitly instructed.

### Negative / Risks

- **External access requires extra infrastructure:** An Ingress controller and DNS configuration will be needed when a public API is exposed.
- **Canary requires L7 or service mesh:** Advanced release strategies are not available today without additional tooling.
- **Limited by L4 for internal traffic:** Services that require HTTP header‑based routing internally would need an internal Ingress or service mesh. This is not currently required.

---

## Alternatives Considered

| Alternative                                               | Why Rejected                                                                                                                                        |
| --------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| Use `LoadBalancer` for every service                      | Expensive; exposes internal services to the internet; unnecessary for east‑west traffic.                                                            |
| Use NodePort for all services                             | Port management nightmare; insecure; not suitable for production.                                                                                   |
| Use Ingress for internal service‑to‑service communication | Adds latency and complexity; L4 ClusterIP is the right tool for internal traffic.                                                                   |
| Use a service mesh (Istio/Linkerd) from day one           | Overkill for current scale; introduces operational overhead without clear need. Revisit when canary, mTLS, or observability requirements demand it. |
| Use plain DNS without Kubernetes Services                 | Pod IPs are ephemeral; no load balancing or health checking.                                                                                        |

---

## AI Platform Alignment

The platform is designed to be AI‑native. In this context, load balancing decisions are part of the **platform contract** that AI agents must follow:

- The default service type in the Helm library chart is `ClusterIP`. AI‑generated service manifests inherit this safe default.
- AI agents can propose new services and values, but they cannot change the load balancer type without going through the standard GitOps review process.
- The validation pipeline (CI smoke test) ensures that the deployed service is healthy behind its L4 Service before the change is merged.
- As the platform evolves, L7 Ingress and canary configurations will be templated in the library chart, so AI agents can also generate advanced routing safely.

---

## Implementation Status

- [x] All current services use `ClusterIP` Services (L4) for internal communication.
- [x] Health checks (startup, readiness, liveness) are standardised in the library chart.
- [x] Smoke test script (`scripts/dev/smoke-k8s.sh`) validates that the service responds to HTTP requests after deployment.
- [x] GitOps (ArgoCD) manages the deployment lifecycle, including service manifests.
- [ ] No Ingress controller deployed (not required until external API is needed).
- [ ] No service mesh deployed (revisit for canary, mTLS, or advanced observability).

---

## References

- Helm library chart: `deployment/helm/library/logiflow-service/`
- Example service manifest: `deployment/helm/services/llm-gateway/templates/service.yaml`
- Smoke test script: `scripts/dev/smoke-k8s.sh`
- GitOps configuration: `deployment/gitops/argocd/`
- ADR-011: GitOps Deployment Strategy with Argo CD
- ADR-005: Platform Standardization with Helm Library Charts
- ADR-007: Platform Contracts and Versioning
