# Smoke Testing for LogiFlow

A smoke test is a fast, minimal validation of the critical path of a service. It answers one question: is the system alive enough to be useful?

For a Kubernetes service, the critical path usually includes:

- Docker image builds successfully
- Helm chart lints and renders correctly
- The service deploys and reaches Ready
- The health endpoint responds successfully

If these checks pass, the service is generally safe to move to deeper validation, such as integration tests, load tests, or security testing.

---

## Why This Matters

A smoke test is not a full test suite. It is a quick operational gate.

It gives you a fast answer to whether the application actually works after a code or config change. This matters because a deployment can look successful in Kubernetes while still being broken in real behavior.

Examples:

- The container image builds, but the pod crashes on startup
- The Deployment exists, but readiness probes never pass
- The application starts, but `/healthz` returns an error or hangs
- Helm manifests render, but the Service never reaches an endpoint

A smoke test catches the most common deployment failures before they become bigger issues.

---

## Smoke Test vs. `dev-up.sh`

They are related, but they are not the same thing.

| Aspect           | `dev-up.sh`                                     | `smoke-k8s.sh`                                                |
| ---------------- | ----------------------------------------------- | ------------------------------------------------------------- |
| Goal             | Set up a complete local development environment | Validate that a service is healthy after deploy               |
| Cluster behavior | Creates or initializes Kind if needed           | Assumes the cluster is already available                      |
| Health check     | Port-forwards and curls from laptop             | Runs a temporary curl pod inside the cluster                  |
| Exit behavior    | Stays live while the environment is active      | Exits after success or failure                                |
| Typical use      | Daily local engineering workflow                | CI validation, deployment safety checks, post-sync validation |

In short:

- `make dev-up` is for local development
- `./scripts/dev/smoke-k8s.sh` is for quick verification and automation

Both are useful, but they solve different problems.

---

## What the Smoke Test Actually Does

The smoke test is a deployment validation tool, not just a cluster utility. It performs the following steps:

1. Preflight checks
   - Confirms Docker, Helm, kubectl, and Kind are available
2. Builds the Docker image
   - Verifies the service can compile and package successfully
3. Loads the image into Kind
   - Makes the target image available to the local cluster
4. Updates Helm dependencies
   - Ensures the chart dependencies are current
5. Lints and renders the chart
   - Validates YAML and templates before deployment
6. Deploys or upgrades the service
   - Uses `helm upgrade --install` for idempotent installation
7. Waits for rollout completion
   - Ensures the Deployment reaches Ready state
8. Checks the health endpoint
   - Calls the service health route and expects success

This is a real end-to-end verification flow.

---

## Smoke Test and GitOps: Why Both Matter

GitOps ensures the cluster matches the desired state declared in Git. Argo CD continuously reconciles the cluster to the Git repository.

However, GitOps by itself does not guarantee that the application works.

A GitOps sync can succeed even if:

- the container crashes on startup
- the readiness probe is wrong
- the service never answers health checks
- the app starts but returns runtime failures

A smoke test fills that gap by validating actual runtime behavior after deployment.

The correct pattern is:

1. Developer changes code and pushes to Git
2. CI validates the chart and the code
3. Argo CD syncs the cluster to the Git-defined state
4. A smoke test validates the service is actually healthy
5. If the smoke test fails, rollback or alerting is triggered

This makes GitOps and smoke testing complementary, not competing.

> Git is the source of truth for desired state. A smoke test is the source of truth for actual health.

---

## Local Development Flow

### When you change application code

If you change application code such as `services/llm-gateway/application`, you do not need to manually rebuild, deploy, or re-run Helm commands yourself. The smoke script handles the full flow:

- build image
- load into Kind
- update Helm dependencies
- lint and render
- deploy or upgrade
- wait for rollout
- verify health

Use:

```bash
SERVICE=llm-gateway NAMESPACE=logiflow-dev ./scripts/dev/smoke-k8s.sh
```

That is the full local validation loop.

---

## When Argo CD Is Used

Argo CD is not for your laptop. It is for shared environments such as staging and production.

| Environment                 | Typical workflow                              |
| --------------------------- | --------------------------------------------- |
| Local machine               | `make dev-up` or `./scripts/dev/smoke-k8s.sh` |
| Shared staging / production | Push Git changes; Argo CD syncs automatically |

You would not normally run the smoke test directly against production just for local engineering. The smoke test is best used for local validation or CI gating before merge and deployment.

---

## Corrected Test Matrix

### 1. Run from the repo root

```bash
cd /path/to/LogiFlow-AI-Powered-Operational-Intelligence
./scripts/dev/smoke-k8s.sh
```

Expected result: all steps pass and the script ends with a success message.

### 2. Run from a subdirectory

```bash
cd deployment/helm/services/llm-gateway
../../../../scripts/dev/smoke-k8s.sh
```

Or the most robust option:

```bash
$(git rev-parse --show-toplevel)/scripts/dev/smoke-k8s.sh
```

### 3. Run the llm-gateway service specifically

```bash
SERVICE=llm-gateway NAMESPACE=logiflow-dev ./scripts/dev/smoke-k8s.sh
```

Expected result: the gateway image builds, the chart renders, the release deploys, and the health probe passes.

### 4. Failure case: missing Dockerfile

```bash
SERVICE=nonexistent ./scripts/dev/smoke-k8s.sh
```

Expected behavior: the script exits early with a clear error such as a missing Dockerfile.

### 5. Failure case: bad image tag

Temporarily edit the service values to use a nonexistent tag, then run:

```bash
SERVICE=llm-gateway NAMESPACE=logiflow-dev ./scripts/dev/smoke-k8s.sh
```

Expected behavior: rollout fails because the image cannot be pulled.

### 6. Failure case: wrong readiness path

Temporarily set a broken readiness path like `/broken`, then rerun the smoke script.

Expected behavior: the Deployment never becomes Ready and the script fails at rollout or health verification.

### 7. Failure case: missing chart dependency

Delete or break the chart dependency, then rerun the script.

Expected behavior: Helm dependency or templating steps fail with a clear error message.

---

## Important Output Notes

### Namespace warning

You may see a warning like this:

```text
Warning: resource namespaces/logiflow-dev is missing the kubectl.kubernetes.io/last-applied-configuration annotation...
```

This is generally harmless. It means the namespace was created without a stored last-applied configuration and Kubernetes is merely patching metadata. It does not indicate a broken deployment.

### Pod attach warning

You may also see:

```text
warning: couldn't attach to pod/... falling back to streaming logs...
```

This happens when the temporary validation pod is not created in the same namespace as the target service. It is not a deployment failure. The check still works, but it is cleaner to ensure the pod is always created in the target namespace.

The successful health response should look like this:

```json
{ "status": "ok" }
```

That is the proof that the service health endpoint is responding correctly.

---

## Recommended Script Improvement

The health-check step is more robust when the temporary pod is created in the same namespace as the service. A better pattern is:

```bash
verify_health() {
  step_start "Verifying health endpoint"
  local svc_fqdn="${SERVICE}.${NAMESPACE}.svc.cluster.local:${SERVICE_PORT}"

  kubectl run smoke-test-$$ \
    -n "$NAMESPACE" \
    --rm -i \
    --restart=Never \
    --image=curlimages/curl -- \
    curl -fsS "http://${svc_fqdn}${HEALTH_ENDPOINT}"
}
```

This removes the cross-namespace warning and makes the verification cleaner.

---

## What to Run in Practice

### Local developer workflow

```bash
SERVICE=llm-gateway NAMESPACE=logiflow-dev ./scripts/dev/smoke-k8s.sh
```

### CI validation flow

```bash
cd services/llm-gateway
go test ./...
go vet ./...
cd ../..
SERVICE=llm-gateway NAMESPACE=logiflow-dev ./scripts/dev/smoke-k8s.sh
```

This is a strong local plus CI pipeline: code correctness first, end-to-end health validation second.

---

## Failure Testing and Recovery

### 1. Wrong image tag

**Trigger:** image tag points to a nonexistent image.

**Expected result:** pod cannot start; rollout fails.

**Recovery:** revert the tag to the correct development image and rerun the smoke test.

### 2. Wrong readiness probe path

**Trigger:** readiness probe points to `/broken` instead of `/healthz`.

**Expected result:** Deployment never reaches Ready, health verification fails.

**Recovery:** restore the correct probe path and rerun the smoke test.

### 3. Broken Service selector

**Trigger:** Service selector does not match pod labels.

**Expected result:** service has no endpoints; health check fails.

**Recovery:** fix the selector or reapply the desired Helm state.

### 4. Missing Helm dependency

**Trigger:** `charts/` and `Chart.lock` are missing or broken.

**Expected result:** dependency or render step fails.

**Recovery:** run Helm dependency update and re-run the script.

---

## CI and Argo CD Integration

A robust pipeline should do the following:

```yaml
- name: Run Go validation
  run: |
    cd services/llm-gateway
    go test ./...
    go vet ./...

- name: Run smoke test
  run: |
    SERVICE=llm-gateway NAMESPACE=logiflow-dev ./scripts/dev/smoke-k8s.sh
```

This gives a good separation:

- Go tests validate correctness
- Helm lint/template checks configuration validity
- Smoke test validates runtime health
- Argo CD handles Git-driven deployment in shared environments

---

## Completion Criteria

The smoke test is considered successful when all of the following are true:

- Docker image builds successfully
- Helm chart lints and renders without errors
- The Deployment becomes Ready
- The Service has endpoints
- The health endpoint returns success
- The script ends without failure

When this happens, the application is healthy enough to move on to the next phase of validation.

---

## Final Takeaway

This smoke test is one of the most important operational guardrails in the platform.

It gives you a fast answer to the question that matters most in deployment engineering: “Does the service actually work after it is deployed?”

Use it locally after every meaningful change, and use it in CI before release. It is a simple, practical, and high-value safety check for any production-grade platform.
