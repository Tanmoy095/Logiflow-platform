#!/usr/bin/env bash
# =========================================================================
# LogiFlow Smoke Test – verifies the critical path of a deployed service.
#
# Usage:
#   ./scripts/dev/smoke-k8s.sh                 # tests default service: hello
#   SERVICE=llm-gateway ./scripts/dev/smoke-k8s.sh
#   SERVICE=llm-gateway NAMESPACE=logiflow-dev ./scripts/dev/smoke-k8s.sh
#
# What it does:
#   1. Preflight checks (docker, kubectl, helm, kind, cluster)
#   2. Builds the Docker image (if needed)
#   3. Loads the image into Kind
#   4. Updates Helm dependencies
#   5. Lints and renders the Helm chart (dry-run)
#   6. Deploys with helm upgrade --install (idempotent)
#   7. Waits for rollout to complete
#   8. Verifies health via internal DNS and /healthz
#
# Exit codes:
#   0 – success
#   1 – any failure (immediate exit with error message)
# =========================================================================

set -euo pipefail

# ---------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------
SERVICE="${SERVICE:-hello}"
NAMESPACE="${NAMESPACE:-logiflow-dev}"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-logiflow-dev}"
APP_IMAGE="logiflow/${SERVICE}:local"
CHART_PATH="deployment/helm/services/${SERVICE}"
DOCKERFILE="build/Dockerfile.${SERVICE}"
LOCAL_PORT="8080"
SERVICE_PORT="8080"
HEALTH_ENDPOINT="/healthz"

# Get repo root (works from any subdirectory)
REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

# ---------------------------------------------------------------------
# Pretty printing
# ---------------------------------------------------------------------
GREEN='\033[0;32m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m'

log_info() { echo -e "${BOLD}[${1}]${NC} $2"; }
log_ok()   { echo -e "  ${GREEN}✓${NC} $1"; }
log_fail() { echo -e "  ${RED}✗${NC} $1"; exit 1; }

step=0
total=8

step_start() {
  step=$((step + 1))
  log_info "${step}/${total}" "$1"
}

# ---------------------------------------------------------------------
# Cleanup – kill any background port-forward
# ---------------------------------------------------------------------
cleanup() {
  if [[ -n "${PF_PID:-}" ]]; then
    kill "$PF_PID" 2>/dev/null || true
    wait "$PF_PID" 2>/dev/null || true
    log_info "cleanup" "Port-forward cleaned up."
  fi
}
trap cleanup EXIT

# ---------------------------------------------------------------------
# 1. Preflight checks
# ---------------------------------------------------------------------
preflight() {
  step_start "Running preflight checks"
  for cmd in docker kubectl helm kind; do
    command -v "$cmd" >/dev/null 2>&1 || log_fail "Missing command: $cmd"
  done
  if ! kind get clusters | grep -q "^${KIND_CLUSTER_NAME}$"; then
    log_fail "Kind cluster '${KIND_CLUSTER_NAME}' not found. Run make dev-up first."
  fi
  log_ok "All dependencies present"
}

# ---------------------------------------------------------------------
# 2. Build Docker image
# ---------------------------------------------------------------------
build_image() {
  step_start "Building Docker image"
  if [[ ! -f "$DOCKERFILE" ]]; then
    log_fail "Dockerfile not found: $DOCKERFILE"
  fi
  docker build -f "$DOCKERFILE" -t "$APP_IMAGE" .
  log_ok "Image built: $APP_IMAGE"
}

# ---------------------------------------------------------------------
# 3. Load image into Kind
# ---------------------------------------------------------------------
load_image() {
  step_start "Loading image into Kind"
  kind load docker-image "$APP_IMAGE" --name "$KIND_CLUSTER_NAME"
  log_ok "Image loaded"
}

# ---------------------------------------------------------------------
# 4. Update Helm dependencies
# ---------------------------------------------------------------------
update_deps() {
  step_start "Updating Helm dependencies"
  if [[ -d "$CHART_PATH" ]]; then
    (cd "$CHART_PATH" && helm dependency update)
  else
    log_fail "Chart path not found: $CHART_PATH"
  fi
  log_ok "Dependencies updated"
}

# ---------------------------------------------------------------------
# 5. Lint and template
# ---------------------------------------------------------------------
validate_chart() {
  step_start "Validating Helm chart (lint + template)"
  helm lint "$CHART_PATH" --namespace "$NAMESPACE" || log_fail "Helm lint failed"
  helm template "$SERVICE" "$CHART_PATH" \
    -f "$CHART_PATH/values-dev.yaml" \
    --namespace "$NAMESPACE" > /dev/null || log_fail "Helm template failed"
  log_ok "Chart validated"
}

# ---------------------------------------------------------------------
# 6. Deploy / upgrade
# ---------------------------------------------------------------------
deploy() {
  step_start "Deploying service with Helm"
  kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  helm upgrade --install "$SERVICE" "$CHART_PATH" \
    -f "$CHART_PATH/values-dev.yaml" \
    --namespace "$NAMESPACE" \
    --wait --timeout 120s || log_fail "Helm deployment failed"
  log_ok "Release '$SERVICE' deployed"
}

# ---------------------------------------------------------------------
# 7. Wait for rollout
# ---------------------------------------------------------------------
wait_ready() {
  step_start "Waiting for rollout to complete"
  kubectl rollout status deployment "$SERVICE" -n "$NAMESPACE" --timeout=120s || log_fail "Rollout failed"
  log_ok "Pod is ready"
}

# ---------------------------------------------------------------------
# 8. Health check via internal DNS
# ---------------------------------------------------------------------

verify_health() {
  step_start "Verifying health endpoint"
  local svc_fqdn="${SERVICE}.${NAMESPACE}.svc.cluster.local:${SERVICE_PORT}"
  kubectl run smoke-test-$$ \
    -n "$NAMESPACE" \
    --rm -i --restart=Never --image=curlimages/curl -- \
    curl -fsS "http://${svc_fqdn}${HEALTH_ENDPOINT}" \
    && log_ok "Health check passed" \
    || log_fail "Health check failed"
}

# ---------------------------------------------------------------------
# Main execution
# ---------------------------------------------------------------------
main() {
  echo -e "${BOLD}LogiFlow Smoke Test – ${SERVICE}${NC}\n"
  preflight
  build_image
  load_image
  update_deps
  validate_chart
  deploy
  wait_ready
  verify_health
  echo -e "\n${GREEN}${BOLD}Smoke test passed.${NC}"
}

main "$@"