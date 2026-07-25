# Platform Engineer’s Runbook — Everyday Operations

## Golden Rule: After Changing the Library Chart
Every time you modify `_helpers.tpl`, re-package the library for all services.

```bash
# Single service
cd deployment/helm/services/<service-name> && helm dependency update && cd -

# All services (loop)
for svc in hello stream-ingestion; do
  cd deployment/helm/services/$svc && helm dependency update && cd -
done

# Platform Engineer’s Runbook — Creating a New Service (Golden Path)



SERVICE=<service-name>

# 1. Copy the hello chart
cp -r deployment/helm/services/hello deployment/helm/services/$SERVICE

# 2. Clean old dependencies
cd deployment/helm/services/$SERVICE && rm -rf charts/ Chart.lock && cd -

# 3. Edit Chart.yaml → change name & description
# 4. Edit values.yaml → set image, port, config
# 5. Edit templates/deployment.yaml → replace env block

# 6. Fetch library
cd deployment/helm/services/$SERVICE && helm dependency update && cd -

# 7. Validate
helm lint deployment/helm/services/$SERVICE --namespace logiflow
helm template dev deployment/helm/services/$SERVICE \
  -f deployment/helm/services/$SERVICE/values-dev.yaml \
  --namespace logiflow > /tmp/$SERVICE-manifest.yaml
# Open file and inspect security contexts, probes, labels, env vars

# 8. Deploy locally
SERVICE=$SERVICE make dev-up

# Platform Engineer’s Runbook — Multi‑Environment Deployments
Create namespaces once:


kubectl create namespace logiflow-dev
kubectl create namespace logiflow-staging
kubectl create namespace logiflow-prod
# Platform Engineer’s Runbook — Multi‑Environment Deployments Deploy to each environment

# Dev
helm upgrade --install <svc>-dev deployment/helm/services/<svc> \
  -f deployment/helm/services/<svc>/values-dev.yaml \
  --namespace logiflow-dev

# Staging
helm upgrade --install <svc>-staging deployment/helm/services/<svc> \
  -f deployment/helm/services/<svc>/values-dev.yaml \
  -f deployment/helm/services/<svc>/values-staging.yaml \
  --namespace logiflow-staging

# Production
helm upgrade --install <svc>-prod deployment/helm/services/<svc> \
  -f deployment/helm/services/<svc>/values-dev.yaml \
  -f deployment/helm/services/<svc>/values-prod.yaml \
  -f deployment/helm/services/<svc>/values-prod-secrets.yaml \
  --namespace logiflow-prod

#Check all environments:

kubectl get pods -A | grep <service-name>


# Platform Engineer’s Runbook — Helm Operations

# Lint
helm lint deployment/helm/services/<service> --namespace <namespace>

# Render locally
helm template <release> deployment/helm/services/<service> \
  --namespace <namespace> \
  -f deployment/helm/services/<service>/values-dev.yaml \
  > /tmp/manifest.yaml

# View release history
helm list -n <namespace>
helm history <release> -n <namespace>

# Rollback
helm rollback <release> <revision> -n <namespace>

# Uninstall
helm uninstall <release> -n <namespace>







Image Issues
bash
kubectl describe pod <pod> -n <namespace>   # look for "Failed to pull image"
docker build -t logiflow/<service>:<tag> .
kind load docker-image logiflow/<service>:<tag> --name logiflow-dev
Selector Mismatch (Endpoints Empty)
bash
kubectl get endpoints <release> -n <namespace>
kubectl get pods -n <namespace> --show-labels
kubectl describe svc <release> -n <namespace>  # compare Selector with pod labels
One‑Command Dev Loop
bash
SERVICE=<service> make dev-up
make dev-down   # tear down Kind cluster
text

---

### 2. `runbook-debugging-probes.md`
```markdown
# Debugging Probes — The 2 AM Playbook

## Quick Status Check
```bash
kubectl get pods -n <namespace> -l app.kubernetes.io/name=<service>
kubectl get endpoints <release> -n <namespace>
kubectl get events -n <namespace> --sort-by='.lastTimestamp' | tail -20
Probe Configuration on a Running Pod
bash
kubectl get pod <pod> -n <namespace> -o yaml | grep -A10 'startupProbe\|readinessProbe\|livenessProbe'
Startup Probe Failure → CrashLoopBackOff
bash
kubectl describe pod <pod> -n <namespace>
# Look for: "Startup probe failed: HTTP probe failed …"
kubectl logs <pod> -n <namespace> --previous
Common fixes:

Probe path wrong → fix probes.startup.path in values file.

App genuinely crashes → investigate kubectl logs.

Not enough time → increase failureThreshold or periodSeconds.

Readiness Probe Failure → Pod Not Ready (0/1), No Traffic
bash
kubectl describe pod <pod> -n <namespace>
# Look for: "Readiness probe failed: …"
kubectl logs <pod> -n <namespace>
Impact: Pod removed from Service endpoints – no traffic. Container not restarted.
Fix: Correct path or restore dependent service (DB, Kafka).

Liveness Probe Failure → Container Restart
bash
kubectl describe pod <pod> -n <namespace>
# Look for: "Liveness probe failed: …" then "Killing container …"
Impact: Container killed and restarted.
Fix: Check for deadlock, infinite loop, or incorrect probe path.

Compare Desired vs Live State
bash
kubectl get deployment <release> -n <namespace> -o yaml > /tmp/live.yaml
diff /tmp/manifest.yaml /tmp/live.yaml
text

---

### 3. `runbook-ci-validation.md`
```markdown
# Pre‑Push CI Validation Checklist

Run this sequence locally before committing.

```bash
# 1. Update all library dependencies (if _helpers.tpl changed)
for svc in hello stream-ingestion; do
  (cd deployment/helm/services/$svc && helm dependency update)
done

# 2. Lint every service
helm lint deployment/helm/services/hello --namespace logiflow
helm lint deployment/helm/services/stream-ingestion --namespace logiflow

# 3. Render all services × all environments (must succeed)
echo "=== hello ==="
helm template hello-dev deployment/helm/services/hello -f deployment/helm/services/hello/values-dev.yaml --namespace logiflow > /dev/null || exit 1
helm template hello-staging deployment/helm/services/hello -f deployment/helm/services/hello/values-dev.yaml -f deployment/helm/services/hello/values-staging.yaml --namespace logiflow-staging > /dev/null || exit 1
helm template hello-prod deployment/helm/services/hello -f deployment/helm/services/hello/values-dev.yaml -f deployment/helm/services/hello/values-prod.yaml --namespace logiflow-prod > /dev/null || exit 1

echo "=== stream-ingestion ==="
helm template stream-ingestion-dev deployment/helm/services/stream-ingestion -f deployment/helm/services/stream-ingestion/values-dev.yaml --namespace logiflow > /dev/null || exit 1
helm template stream-ingestion-staging deployment/helm/services/stream-ingestion -f deployment/helm/services/stream-ingestion/values-dev.yaml -f deployment/helm/services/stream-ingestion/values-staging.yaml --namespace logiflow-staging > /dev/null || exit 1
helm template stream-ingestion-prod deployment/helm/services/stream-ingestion -f deployment/helm/services/stream-ingestion/values-dev.yaml -f deployment/helm/services/stream-ingestion/values-prod.yaml --namespace logiflow-prod > /dev/null || exit 1

echo "All validations passed."
If any of these fail, fix before pushing. This is the same pipeline that CI will run.

text

---

You now have three precise, senior‑engineer‑grade runbooks that cover everything you’ve learned. Keep them inside `docs/runbooks/` and consult them daily.
