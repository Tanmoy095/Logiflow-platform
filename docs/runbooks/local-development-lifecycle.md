# Runbook: Local Development Lifecycle for LogiFlow Services

This runbook describes the complete workflow for building, deploying, and debugging LogiFlow microservices in a local Kind-based Kubernetes cluster. It covers manual steps for understanding the flow and the automated `make dev-up` pipeline, plus how to update the shared Helm library chart and verify everything works.

**Prerequisites:**

- `doctor.sh` passes. Run `make doctor` to check Docker, Kind, kubectl, Helm, and Go.
- A running Kind cluster. `kind get clusters` should show `logiflow-dev`.
- The repository root is your working directory.

## 1. Quick Decision: Manual vs. Automated

| Scenario | Recommended approach |
| --- | --- |
| I changed **Go code** and want a full, fresh deployment. | `SERVICE=<name> LOCAL_PORT=<port> make dev-up` |
| I changed **only Helm values** (no code change). | `helm upgrade --install <name> deployment/helm/services/<name> -f values-dev.yaml --namespace logiflow` |
| I changed the **Helm library chart** (`_helpers.tpl`, `_probes.tpl`). | Follow Section 4, then redeploy. |
| I want to understand the pipeline or debug step-by-step. | Use the manual workflow in Section 2. |

The manual workflow explains why each step exists; the automated command encapsulates them all.

## 2. Manual Workflow - After Code or Configuration Changes

### 2.1 Build the new Docker image

```bash
docker build -f build/Dockerfile.<service> -t logiflow/<service>:local .
```

Example for stream-ingestion:

```bash
docker build -f build/Dockerfile.stream-ingestion -t logiflow/stream-ingestion:local .
```

Why: Your Go source has changed; the binary inside the container must reflect the new code. The image is tagged `:local` to avoid collisions with remote registries.

### 2.2 Load the image into the Kind cluster

```bash
kind load docker-image logiflow/<service>:local --name logiflow-dev
```

Why: Kind nodes run a separate container runtime. Images built on the host are not automatically visible inside the cluster. This command copies the image into the node's containerd storage.

Verify the image is present inside the node (optional):

```bash
docker exec -it logiflow-dev-control-plane crictl images | grep <service>
```

### 2.3 Deploy or upgrade the Helm release

```bash
helm upgrade --install <release-name> deployment/helm/services/<service> \
  -f deployment/helm/services/<service>/values-dev.yaml \
  --namespace logiflow
```

Example for stream-ingestion:

```bash
helm upgrade --install stream-ingestion deployment/helm/services/stream-ingestion \
  -f deployment/helm/services/stream-ingestion/values-dev.yaml \
  --namespace logiflow
```

Why: `helm upgrade --install` is idempotent. It creates the release if it does not exist, otherwise it updates it. The `-f` flag loads the development values file, which overrides any `values.yaml` defaults in the chart. The release name usually matches the service name.

### 2.4 Wait for readiness and test

```bash
kubectl get pods -n logiflow -w   # watch until READY 1/1

# Start a port-forward if not already running
kubectl port-forward svc/<service> <local-port>:8080 -n logiflow &

# Hit the probes
curl http://localhost:<local-port>/startupz
curl http://localhost:<local-port>/healthz
curl http://localhost:<local-port>/live
```

## 3. Automated Workflow - `make dev-up`

The Makefile and `dev-up.sh` automate the entire manual pipeline plus pre-flight checks. Use this when you want a complete reset or when you're starting work on a service.

### 3.1 Basic invocation

```bash
SERVICE=<name> make dev-up
```

Example for hello:

```bash
SERVICE=hello make dev-up
```

This single command:

- Runs `doctor.sh` to validate your toolchain.
- Ensures the Kind cluster exists, creating it if needed.
- Builds the Docker image (`make build`).
- Loads the image into Kind.
- Lints and templates the Helm chart.
- Deploys the release.
- Waits for the pod to be ready.
- Starts a port-forward on port 8080.
- Verifies the health endpoint.

### 3.2 Avoiding port conflicts with multiple services

The default `LOCAL_PORT` is 8080. When running more than one service locally, assign each its own port:

```bash
SERVICE=hello LOCAL_PORT=8080 make dev-up
SERVICE=stream-ingestion LOCAL_PORT=8081 make dev-up
```

Now you can curl `http://localhost:8080/healthz` for hello and `http://localhost:8081/healthz` for stream-ingestion.

How it works: The Makefile and `dev-up.sh` both honour the `LOCAL_PORT` variable. The port-forward uses the value you provide, and the health check targets that same port.

## 4. Updating the Helm Library Chart

The library chart at deployment/helm/library/logiflow-service/ provides all standard templates (`_helpers.tpl`, `_probes.tpl`, and so on). When you change any of its files, you must re-package it for every service that depends on it.

### 4.1 Update the library dependency for a specific service

```bash
cd deployment/helm/services/<service>
helm dependency update
cd -
```

Example for hello:

```bash
cd deployment/helm/services/hello
helm dependency update
cd -
```

Why: Each service caches the library chart as a `.tgz` file inside its `charts/` directory. `helm dependency update` re-fetches the library and updates the lock file. Without this, the service still uses the old version.

### 4.2 After updating all services, redeploy

Use `helm upgrade --install` manually or `SERVICE=<name> make dev-up` automatically for every affected service. A loop works well:

```bash
for svc in hello stream-ingestion; do
  helm upgrade --install $svc deployment/helm/services/$svc \
    -f deployment/helm/services/$svc/values-dev.yaml \
    --namespace logiflow
done
```

## 5. Cluster Inspection Commands

Use these to verify the overall health of your local environment.

### 5.1 Pods, Services, Endpoints

```bash
# All pods in the application namespace
kubectl get pods -n logiflow

# All pods across all namespaces
kubectl get pods -A

# Services and their endpoints
kubectl get svc -n logiflow
kubectl get endpoints -n logiflow
```

### 5.2 Resource details

```bash
kubectl describe pod <pod-name> -n logiflow   # Events, probes, resource usage
kubectl describe svc <service-name> -n logiflow  # Selector, ports
kubectl describe node logiflow-dev-control-plane   # Node capacity, conditions
```

### 5.3 Logs

```bash
kubectl logs <pod-name> -n logiflow
kubectl logs <pod-name> -n logiflow --previous   # logs from previous crashed container
```

### 5.4 Events timeline

```bash
kubectl get events -n logiflow --sort-by='.lastTimestamp'
```

## 6. Troubleshooting Common Issues

| Symptom | Likely cause | Action |
| --- | --- | --- |
| ImagePullBackOff | Image not loaded into Kind, or tag mismatch. | Run `kind load docker-image ...`; verify the image tag matches `values.yaml`. |
| CrashLoopBackOff | Startup or liveness probe failing. | `kubectl describe pod` and check probe paths; ensure endpoints `/startupz` and `/live` exist in the app. |
| Pod Running but 0/1 Ready | Readiness probe failing. | `kubectl describe pod` and check readiness probe; verify `/healthz` returns 200. |
| helm upgrade fails with “nil pointer evaluating interface” | A required value is missing in `values-dev.yaml`. | Add the missing key (for example, `config.helloMsg`) or use a default in the template. |
| helm template error “no template … associated” | Library dependency not updated. | Run `helm dependency update` inside the service chart directory. |
| Port-forward “cannot listen on port” | Another process is using that port. | Kill the existing port-forward or use a different `LOCAL_PORT`. |
| make dev-up fails after library change | The library's `.tgz` is stale. | Re-run `helm dependency update` for the service, then retry. |
| Multiple services, port conflict | Both try to forward to the same `LOCAL_PORT`. | Always assign unique `LOCAL_PORT` values per service. |

## 7. Appendix - Working with Multiple Services Simultaneously

To run hello and stream-ingestion side-by-side:

```bash
# Terminal 1 - hello on 8080
SERVICE=hello LOCAL_PORT=8080 make dev-up

# Terminal 2 - stream-ingestion on 8081
SERVICE=stream-ingestion LOCAL_PORT=8081 make dev-up
```

To tear down everything and start fresh:

```bash
make dev-down   # deletes the Kind cluster
SERVICE=hello make dev-up
SERVICE=stream-ingestion LOCAL_PORT=8081 make dev-up
```

Now you can develop and test both services independently without port clashes.