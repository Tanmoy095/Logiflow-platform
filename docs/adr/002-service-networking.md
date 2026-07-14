# ADR-002: Service Networking – Stable Pod Addressability

- **Status:** Accepted
- **Date:** 2026-06-26
- **Supersedes:** None
- **Author:** Aunmoy Dey Tanmoy  
- **Decision Owner:** Aunmoy Dey Tanmoy  
- **Status:** Accepted  
- **Project:** LogiFlow  

---

# Business Problem

Internal LogiFlow services must communicate reliably even when workloads restart, scale, or are rescheduled across cluster nodes.

If callers depend on the network addresses of individual pods, any lifecycle event (crash, rolling update, or scale-up) breaks connectivity and forces every consumer to implement its own service discovery and retry logic.

We require a simple, platform-native abstraction that decouples service identity from pod lifetimes while enabling seamless horizontal scaling.

---

# Technical Context

Pods in Kubernetes are **ephemeral**. Every restart creates a new Pod with a new IP address.

Hardcoding Pod IPs is therefore not a viable networking strategy.

Building a custom service-discovery registry would duplicate Kubernetes functionality while adding unnecessary operational complexity.

Kubernetes already provides the required building blocks through the **Service** resource, which offers:

- Stable virtual IP
- Stable DNS name
- Automatic endpoint discovery
- Built-in load balancing

---

# Options Considered

| Approach                                | Description                                                                        | Rejected Because                                                                         |
| --------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| Hardcoded Pod IP                        | Clients connect directly to Pod IPs                                                | IP changes after restart; single point of failure; impossible to scale                   |
| DNS directly to Pods (Headless Service) | Returns all Pod IPs to clients for client-side load balancing                      | Pushes health checking and balancing into every application                              |
| Custom Service Registry                 | External registry (Consul, etcd, etc.)                                             | Additional infrastructure and operational complexity; duplicates Kubernetes capabilities |
| LoadBalancer / NodePort                 | Exposes services externally                                                        | Internal-only communication does not require external exposure; increases attack surface |
| **ClusterIP Service**                   | Kubernetes-native virtual IP with automatic endpoint management and load balancing | **Chosen**                                                                               |

---

# Decision

We will expose every internal LogiFlow service through a **Kubernetes Service** of type **ClusterIP**.

The Service becomes the permanent network identity of the application, while Pods remain replaceable implementation details.

Consumers communicate using the Service DNS name (for example, `hello`) rather than Pod IP addresses.

The Service's label selector dynamically discovers healthy Pods, while Kubernetes automatically routes traffic to available backends.

Applications remain completely unaware of Pod lifecycle events.

---

# Consequences

## Positive

### Stable DNS

The Service name (`hello`) always resolves regardless of Pod restarts, rolling updates, or scaling events.

### Automatic Load Balancing

Traffic is automatically distributed across all Ready Pods without requiring application changes.

### Simplicity

The same networking pattern applies to every future stateless workload, including:

- API services
- Event consumers
- Background workers
- AI inference services

### Security

ClusterIP Services are reachable only from inside the Kubernetes cluster, reducing the attack surface.

### Future-Proof

Today's implementation may use kube-proxy with iptables or IPVS.

Future clusters may use eBPF-based implementations such as Cilium.

The application architecture remains unchanged.

---

## Negative / Trade-offs

### Internal Only

ClusterIP Services cannot be accessed from outside the cluster.

External clients require either:

- An Ingress
- A LoadBalancer Service

This separation of concerns is intentional.

### Small Latency Overhead

Traffic traverses one additional networking hop (iptables/IPVS/eBPF).

The overhead is negligible for typical microservice workloads.

### Debugging Requires Cluster Access

Troubleshooting requires Kubernetes tooling such as:

- `kubectl get svc`
- `kubectl get endpoints`
- `kubectl describe pod`
- `kubectl logs`

---

# Operational Notes

When troubleshooting Service connectivity, investigate in the following order.

## 1. Does the Service exist?

```bash
kubectl get svc <service-name>
```

Verify that a ClusterIP has been assigned.

---

## 2. Are Endpoints populated?

```bash
kubectl get endpoints <service-name>
```

Expected output:

```
10.42.0.5:8080
10.42.0.7:8080
```

If the Endpoints list is empty, no Ready Pods currently match the Service selector.

---

## 3. Are Pods healthy?

```bash
kubectl get pods -l <selector-labels>
```

Verify that all Pods report:

```
READY   STATUS
1/1     Running
```

---

## 4. Do labels match?

Compare:

Service selector

```yaml
spec:
  selector:
    app: hello
```

Pod labels

```yaml
metadata:
  labels:
    app: hello
```

A selector mismatch silently prevents Endpoint creation.

---

## 5. Is the application listening?

Confirm that the container is listening on the configured `targetPort`.

Useful commands:

```bash
kubectl logs <pod>
```

```bash
kubectl exec -it <pod> -- sh
```

This investigation workflow resolves the majority of Service connectivity issues without guesswork.

---

# Architecture Diagram

```text
          Client (internal)
                 │
                 ▼
      +-------------------------+
      |  Service (ClusterIP)    |
      |  hello:8080             |
      +-------------------------+
                 │
                 ▼
      +-------------------------+
      | Endpoint Controller     |
      | (watches Pods and       |
      | updates Endpoints)      |
      +-------------------------+
                 │
         ┌───────┴────────┐
         ▼                ▼
   +-------------+   +-------------+
   |   Pod A     |   |   Pod B     |
   | IP: 10.x.x.x|   | IP: 10.x.x.x|
   | :8080       |   | :8080       |
   +-------------+   +-------------+
```

The Service provides a single, permanent entry point.

The Endpoint Controller dynamically updates the backend Pod list as Pods are created, destroyed, or rescheduled.

Clients never need to know individual Pod IP addresses.

---

# Future Impact

This ADR establishes the default networking pattern for every stateless workload within LogiFlow, including:

- API services
- Event consumers
- Background workers
- AI inference services

Future platform components should expose a stable **ClusterIP Service** unless there is a specific requirement for:

- External access
- Headless discovery
- Stateful workloads

When LogiFlow evolves to multi-namespace or multi-cluster deployments, the Service abstraction remains unchanged.

Only the DNS suffix changes.

---

# References

- Kubernetes Services Documentation
- `deployment/helm/services/hello/`
- `docs/debug-playbook.md`
- ADR-001: Hello Scaling (future)

---

> **Design Principle:** Pods are ephemeral implementation details. Services provide the stable network identity that applications depend upon.
