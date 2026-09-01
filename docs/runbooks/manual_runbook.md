# LLM Gateway End-to-End Dev Runbook

This runbook takes the `llm-gateway` from a local Docker build to a running workload in a Kind cluster. It covers image loading, Helm validation, Kubernetes deployment, health verification, and a local simulation of the GitOps reconciliation flow.

The sequence mirrors the work performed by CI and Argo CD, but keeps each step visible so failures are easy to isolate.

> [!IMPORTANT]
> Run repository-level commands from the repository root unless a command explicitly changes directory. The Dockerfile builds `./cmd/llm-gateway` from the root Go module.

## Scope and Assumptions

| Item                    | Expected value                                         |
| ----------------------- | ------------------------------------------------------ |
| Kubernetes distribution | Kind                                                   |
| Kind cluster            | `logiflow-dev`                                         |
| Kubernetes context      | `kind-logiflow-dev`                                    |
| Namespace               | `logiflow-dev`                                         |
| Image                   | `logiflow/llm-gateway:local`                           |
| Helm values             | `deployment/helm/services/llm-gateway/values-dev.yaml` |
| Application port        | `8080`                                                 |

This is a development workflow. Never use a real provider API key in the example Secret.

## 0. Pre-flight Checks

From the repository root, verify that the cluster, context, Docker, and required tools are available:

```bash
kind get clusters
kubectl config current-context
docker version
make doctor
```

Expected results:

- `kind get clusters` includes `logiflow-dev`.
- `kubectl config current-context` returns `kind-logiflow-dev`.
- Docker responds without a daemon error.
- `make doctor` completes successfully.

Resolve any failed pre-flight check before continuing.

## 1. Build the Docker Image

Build the image from the repository root:

```bash
docker build \
  -f build/Dockerfile.llm-gateway \
  -t logiflow/llm-gateway:local \
  .
```

The multi-stage Dockerfile compiles a static Go binary and places it in a minimal `scratch` runtime image. A successful build means the image is available in Docker Desktop's local image store.

If the build fails, check:

- `build/Dockerfile.llm-gateway` exists.
- The command is being run from the repository root.
- `go build ./cmd/llm-gateway` succeeds.
- Docker Desktop is running and has enough resources.

## 2. Load the Image into Kind

Kind cannot automatically use images stored in Docker Desktop. Load the image into the `logiflow-dev` nodes:

```bash
kind load docker-image logiflow/llm-gateway:local --name logiflow-dev
```

Expected output includes a message confirming that `logiflow/llm-gateway:local` was loaded. The image name and tag must exactly match the values in `values-dev.yaml`.

## 3. Update Helm Dependencies

The service chart depends on the `logiflow-service` library chart. Refresh its dependencies when library templates have changed:

```bash
cd deployment/helm/services/llm-gateway
helm dependency update
cd -
```

Expected output includes `Saving 1 charts` or a message about removing an outdated dependency.

If Helm reports that a `logiflow.*` template cannot be found, inspect the library chart helpers and probe templates in `deployment/helm/library/logiflow-service/`.

## 4. Lint the Helm Chart

Return to the repository root and lint the chart:

```bash
helm lint deployment/helm/services/llm-gateway
```

Expected output:

```text
1 chart(s) linted, 0 chart(s) failed
```

Common causes of lint failures include invalid YAML indentation, missing chart dependencies, and incorrect library template names.

## 5. Render the Helm Manifest

Render the same development chart that Argo CD would render. Save the output so it can be inspected before anything is applied:

```bash
helm template llm-gateway deployment/helm/services/llm-gateway \
  -f deployment/helm/services/llm-gateway/values-dev.yaml \
  --namespace logiflow-dev \
  > /tmp/llm-gateway-manifest.yaml
```

Inspect `/tmp/llm-gateway-manifest.yaml` and confirm it contains:

- A `Deployment` and a `Service`.
- Image `logiflow/llm-gateway:local`.
- Non-root, read-only filesystem, and `RuntimeDefault` security settings.
- Startup, readiness, and liveness probes.
- Probe paths `/healthz` and `/live`.
- Environment variables `SERVICE_NAME`, `PORT`, `LOG_LEVEL`, `MODEL_ROUTING_POLICY`, and `PROVIDER_API_KEY`.

The API key is supplied through a Kubernetes Secret reference. The value itself should not appear in the rendered manifest.

## 6. Create the Development Secret

The current deployment template always references the Secret named `llm-gateway-secret`. Create a disposable development value before installing the chart:

```bash
kubectl create namespace logiflow-dev --dry-run=client -o yaml \
  | kubectl apply -f -

kubectl create secret generic llm-gateway-secret \
  --from-literal=providerApiKey=test-key \
  --namespace logiflow-dev \
  --dry-run=client -o yaml \
  | kubectl apply -f -
```

> [!WARNING]
> `test-key` is only a local placeholder. Do not use a real provider key in this command, commit it to Git, or place it in a shared cluster. Production secrets must be supplied through an approved secret-management mechanism.

## 7. Deploy to Kubernetes

Install or upgrade the development release:

```bash
helm upgrade --install llm-gateway deployment/helm/services/llm-gateway \
  -f deployment/helm/services/llm-gateway/values-dev.yaml \
  --namespace logiflow-dev \
  --create-namespace
```

The release uses the `logiflow-dev` namespace to match the dev Argo CD Application. `helm upgrade --install` is idempotent and can be safely rerun after changing the chart or values.

## 8. Verify the Deployment

Check the release, pods, endpoints, and recent logs:

```bash
helm status llm-gateway --namespace logiflow-dev

kubectl get pods \
  -n logiflow-dev \
  -l app.kubernetes.io/name=llm-gateway

kubectl get endpoints llm-gateway --namespace logiflow-dev

kubectl logs \
  -n logiflow-dev \
  -l app.kubernetes.io/name=llm-gateway \
  --tail=50
```

Healthy output should show:

- The pod is `Running` and `READY` is `1/1`.
- The Service has at least one endpoint address.
- The logs do not show startup or probe failures.

For an explicit readiness check:

```bash
kubectl wait \
  --for=condition=available \
  deployment/llm-gateway \
  --namespace logiflow-dev \
  --timeout=120s
```

## 9. Simulate the GitOps Sync

Argo CD renders the desired Helm output and applies it to the cluster. Simulate that process locally:

```bash
helm template llm-gateway deployment/helm/services/llm-gateway \
  -f deployment/helm/services/llm-gateway/values-dev.yaml \
  --namespace logiflow-dev \
  | kubectl apply -f -
```

Expected output reports the resources as unchanged or configured.

### Test drift correction

Change the live replica count manually, then reapply the desired state:

```bash
kubectl scale deployment/llm-gateway \
  --replicas=3 \
  --namespace logiflow-dev

kubectl get deployment/llm-gateway --namespace logiflow-dev
```

The development values declare one replica. Reapply the rendered desired state:

```bash
helm template llm-gateway deployment/helm/services/llm-gateway \
  -f deployment/helm/services/llm-gateway/values-dev.yaml \
  --namespace logiflow-dev \
  | kubectl apply -f -

kubectl get deployment/llm-gateway --namespace logiflow-dev
```

The replica count should return to `1`. This demonstrates the render-and-apply portion of reconciliation. It is not continuous Argo CD self-healing; in a shared environment, the child Application performs this reconciliation with `selfHeal: true`.

## Troubleshooting

### `ImagePullBackOff`

**Symptoms:** The pod cannot start and reports `ImagePullBackOff`.

**Diagnose:**

```bash
kubectl describe pod \
  -n logiflow-dev \
  -l app.kubernetes.io/name=llm-gateway
```

**Fix:** Rebuild the image, load it into the correct Kind cluster, and verify that the repository and tag match `values-dev.yaml`:

```bash
docker build -f build/Dockerfile.llm-gateway -t logiflow/llm-gateway:local .
kind load docker-image logiflow/llm-gateway:local --name logiflow-dev
```

### `CrashLoopBackOff` or probe failure

**Symptoms:** The pod repeatedly restarts or events show failed startup or liveness probes.

**Diagnose:**

```bash
kubectl describe pod \
  -n logiflow-dev \
  -l app.kubernetes.io/name=llm-gateway
```

**Fix:** Confirm that the application exposes `/healthz` and `/live`, and that the paths in `values-dev.yaml` match those endpoints. Review the container logs for startup errors.

### Missing Secret

**Symptoms:** The pod remains pending or events report that `llm-gateway-secret` cannot be found.

**Fix:** Recreate the development Secret from [Step 6](#6-create-the-development-secret), then restart the rollout if necessary:

```bash
kubectl rollout restart deployment/llm-gateway --namespace logiflow-dev
```

### No Service endpoints

**Symptoms:** The pod is running but `kubectl get endpoints` shows no addresses.

**Diagnose:**

```bash
kubectl describe service/llm-gateway --namespace logiflow-dev
kubectl get pods --namespace logiflow-dev --show-labels
```

**Fix:** Verify that the Deployment and Service use the same library-chart selector labels. Do not manually edit selectors; fix the chart templates or library values instead.

### Helm dependency or template error

**Symptoms:** Helm reports a missing dependency or an unknown `logiflow.*` template.

**Fix:** Run `helm dependency update` from the chart directory, then lint again. Check the helper names in `deployment/helm/library/logiflow-service/` and ensure the chart dependency is available.

## Command Summary

Run these commands from the repository root for a compact happy-path execution:

```bash
# Build and load the image
docker build -f build/Dockerfile.llm-gateway -t logiflow/llm-gateway:local .
kind load docker-image logiflow/llm-gateway:local --name logiflow-dev

# Refresh and validate the chart
cd deployment/helm/services/llm-gateway
helm dependency update
cd -
helm lint deployment/helm/services/llm-gateway
helm template llm-gateway deployment/helm/services/llm-gateway \
  -f deployment/helm/services/llm-gateway/values-dev.yaml \
  --namespace logiflow-dev \
  > /tmp/llm-gateway-manifest.yaml

# Prepare the namespace and development-only Secret
kubectl create namespace logiflow-dev --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret generic llm-gateway-secret \
  --from-literal=providerApiKey=test-key \
  --namespace logiflow-dev \
  --dry-run=client -o yaml | kubectl apply -f -

# Deploy and verify
helm upgrade --install llm-gateway deployment/helm/services/llm-gateway \
  -f deployment/helm/services/llm-gateway/values-dev.yaml \
  --namespace logiflow-dev \
  --create-namespace
kubectl wait --for=condition=available deployment/llm-gateway \
  --namespace logiflow-dev --timeout=120s
kubectl get pods -n logiflow-dev
kubectl get endpoints llm-gateway -n logiflow-dev

# Simulate a GitOps sync
helm template llm-gateway deployment/helm/services/llm-gateway \
  -f deployment/helm/services/llm-gateway/values-dev.yaml \
  --namespace logiflow-dev \
  | kubectl apply -f -
```

## Completion Criteria

The manual validation is complete when:

- The image builds and loads into `logiflow-dev` without errors.
- Helm dependency update and lint succeed.
- The rendered manifest contains the expected workload, security settings, probes, and environment variables.
- The deployment becomes available with a ready pod and Service endpoint.
- Reapplying the desired manifest restores the declared replica count after manual drift.

At that point, the chart, values files, and Argo CD Application manifests are ready for review in Git. Argo CD automates the reconciliation steps; it does not replace the need for CI validation and human review.

## LLM Gateway Smoke Test

Run from repo root:

    SERVICE=llm-gateway NAMESPACE=logiflow-dev ./scripts/dev/smoke-k8s.sh

If it fails, follow the failure scenarios above.
