# ADR-003: Deployment Strategy – Self-Healing, Zero-Downtime, Secure Pods

- **Status:** Accepted
- **Date:** 2026-06-27
- **Supersedes:** None
- **Relates to:** ADR-002 – Service Networking
- **Author:** Aunmoy Dey Tanmoy  
- **Decision Owner:** Aunmoy Dey Tanmoy  
- **Project:** LogiFlow  


---

# Business Problem

The `hello` service is the first stateless workload in the LogiFlow platform.

Running the application as a standalone Pod is suitable for experimentation but insufficient for production because it cannot provide operational guarantees.

Specifically, a standalone Pod introduces several risks:

- No automatic recovery when a Pod crashes.
- No declarative scaling mechanism.
- No controlled rollout strategy for application updates.
- No consistent enforcement of runtime security policies.
- No revision history for rollback after failed deployments.

As LogiFlow evolves into a platform composed of API services, background workers, event consumers, and AI inference services, every workload requires a consistent deployment model that prioritizes reliability, availability, and operational simplicity.

---

# Technical Context

Pods are ephemeral Kubernetes objects.

If a Pod crashes, is deleted, or is rescheduled onto another node, Kubernetes creates a completely new Pod with a different identity.

Simply creating Pods manually transfers operational responsibility to engineers.

Kubernetes already provides a higher-level controller—the **Deployment**—which continuously reconciles the desired application state with the actual cluster state.

Deployment also introduces ReplicaSets, rolling updates, revision history, and declarative scaling without requiring custom automation.

---

# Options Considered

| Approach                | Description                                          | Rejected Because                                                                              |
| ----------------------- | ---------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| Bare Pod                | Directly run the application as a Pod                | No self-healing, no scaling, no rollout strategy                                              |
| ReplicaSet Only         | Maintain replica count                               | Does not provide rolling updates or revision history                                          |
| StatefulSet             | Stable identities and ordered deployment             | Intended for stateful applications; unnecessary complexity                                    |
| DaemonSet               | One Pod per node                                     | Does not match the scaling requirements of stateless services                                 |
| HorizontalPodAutoscaler | Automatic scaling based on metrics                   | Deferred until platform observability is introduced; manual replica count is sufficient today |
| **Deployment**          | Kubernetes-native controller for stateless workloads | **Chosen**                                                                                    |

---

# Decision

Every stateless LogiFlow service will be deployed using a **Kubernetes Deployment**.

The Deployment becomes the authoritative description of the application's desired state.

It is responsible for:

- Maintaining the desired number of replicas
- Replacing failed Pods automatically
- Performing rolling updates
- Preserving revision history for rollback
- Enforcing runtime security configuration
- Providing a foundation for future autoscaling

The Deployment owns a ReplicaSet, which in turn manages the lifecycle of individual Pods.

This ownership hierarchy becomes the standard deployment pattern across the LogiFlow platform.

---

# Architecture

```text
                    Deployment
                         │
             manages desired state
             rollout strategy
             revision history
                         │
                         ▼
                    ReplicaSet
                         │
            maintains desired replicas
                         │
           ┌─────────────┴─────────────┐
           ▼                           ▼
        Pod A                       Pod B
           │                           │
           └─────────────┬─────────────┘
                         ▼
                    Service (ADR-002)
                         │
                Stable DNS / ClusterIP
                         │
                      Clients
```

The Deployment is responsible for lifecycle management.

The ReplicaSet ensures the requested number of Pods always exists.

The Service provides a stable network identity independent of individual Pods.

Each component has a single responsibility, allowing the platform to evolve without changing application code.

---

# Implementation

The Deployment template is located at:

```text
deployment/helm/services/hello/templates/deployment.yaml
```

The current implementation includes:

| Configuration     | Value                       | Purpose                                                      |
| ----------------- | --------------------------- | ------------------------------------------------------------ |
| Replicas          | 2                           | Provides redundancy and availability                         |
| Update Strategy   | RollingUpdate               | Enables controlled deployments                               |
| maxSurge          | 1                           | Allows one additional Pod during updates                     |
| maxUnavailable    | 0                           | Avoids intentional capacity reduction during normal rollouts |
| Security Context  | Non-root (`runAsUser:1000`) | Defense in depth                                             |
| Liveness Probe    | `/healthz`                  | Detects hung processes                                       |
| Readiness Probe   | `/ready`                    | Controls traffic eligibility                                 |
| Resource Requests | 100m CPU / 64Mi Memory      | Scheduler guarantees                                         |
| Resource Limits   | 200m CPU /128Mi Memory      | Prevents resource starvation                                 |
| Grace Period      | 30 seconds                  | Supports graceful shutdown                                   |

The Deployment labels Pods with:

```yaml
app: hello
```

which matches the Service selector defined in ADR-002.

This allows Kubernetes to automatically register Ready Pods as Service endpoints.

---

# Rolling Update Behavior

During an application update:

1. A new ReplicaSet is created using the updated Pod template.
2. The Deployment creates one additional Pod (`maxSurge`).
3. Kubernetes waits until the new Pod passes its readiness probe.
4. One Pod from the previous ReplicaSet is removed.
5. The process repeats until the rollout completes.

The configured rolling update strategy is intended to maintain service availability during normal deployments by avoiding intentional reductions in serving capacity.

---

# Consequences

## Positive

### Self-Healing

Failed Pods are automatically recreated until the desired replica count is restored.

### Controlled Deployments

Rolling updates reduce deployment risk while supporting incremental replacement of running Pods.

### Security

Runtime security policies are enforced independently of the container image.

### Scalability

Replica count can be adjusted manually today and automatically through HorizontalPodAutoscaler in the future.

### Standardization

Every future stateless service follows the same deployment pattern, simplifying operations and reducing cognitive load.

### Operational Visibility

Health probes provide meaningful signals for both Kubernetes controllers and engineers during troubleshooting.

---

## Negative / Trade-offs

### Increased Configuration

Deployments require more configuration than standalone Pods.

The additional complexity is justified by improved operational reliability.

### Probe Configuration

Incorrect probe configuration can unintentionally restart healthy applications or prevent traffic routing.

Probe behavior must therefore remain simple and well documented.

### Resource Planning

Improper CPU or memory requests may reduce scheduling efficiency.

Resource values should evolve as production telemetry becomes available.

---

# Operational Metrics

This decision is considered successful when the platform consistently demonstrates:

- Deployment availability near 100%
- Replica count matches desired state
- Low unexpected Pod restart count
- Successful rolling updates
- Minimal readiness probe failures
- Predictable rollout duration
- CPU utilization within configured requests and limits
- Memory utilization within configured limits

These metrics will later be collected through Prometheus and visualized using Grafana dashboards.

---

# Operational Notes

When debugging Deployment issues, investigate in the following order.

## 1. Deployment Status

```bash
kubectl get deployment hello
```

Verify the desired and available replica counts.

---

## 2. ReplicaSets

```bash
kubectl get replicaset
```

Confirm that the expected ReplicaSet has been created.

---

## 3. Pods

```bash
kubectl get pods
```

Verify Pod readiness and restart counts.

---

## 4. Deployment Description

```bash
kubectl describe deployment hello
```

Inspect rollout events and scheduling failures.

---

## 5. Pod Details

```bash
kubectl describe pod <pod-name>
```

Review probe failures, scheduling issues, and container lifecycle events.

---

## 6. Logs

```bash
kubectl logs <pod-name>
```

Inspect application-level failures.

This investigation sequence resolves the majority of Deployment-related operational issues without guesswork.

---

# Future Impact

This ADR establishes the standard deployment strategy for every stateless workload within LogiFlow, including:

- API services
- Gateway services
- Event consumers
- Background workers
- AI inference services
- Model gateways

Future platform capabilities—including HorizontalPodAutoscaler, PodDisruptionBudgets, topology spread constraints, and progressive delivery—will extend this Deployment model rather than replace it.

By standardizing on Deployments early, every new service inherits consistent operational behavior, predictable deployment semantics, and a common troubleshooting workflow.

---

# References

- Kubernetes Deployments Documentation
- `deployment/helm/services/hello/templates/deployment.yaml`
- ADR-002: Service Networking
- `docs/debug-playbook.md`

---

> **Design Principle:** Applications describe the desired state. Kubernetes continuously reconciles reality to match that state.
