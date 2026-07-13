#!/usr/bin/env bash
# =========================================================================
# LogiFlow dev-up.sh — One Command Local Development Environment
# =========================================================================
# Usage:
#   SERVICE=query-api ./scripts/dev/dev-up.sh
#   SERVICE=query-api make dev-up          # recommended
#
# Design:
#   - Delegates build/lint/template/deploy to the Makefile.
#   - Owns cluster creation, image loading, readiness checks, port-forward,
#     and health verification.
#   - Reads SERVICE from environment (default: hello) to support multi‑service.
# =========================================================================

set -euo pipefail

# ----------------------------------------------------------------------
# Cluster Configuration (not covered by Makefile)
# ----------------------------------------------------------------------
readonly KIND_CLUSTER_NAME="logiflow-dev"
readonly KIND_CONFIG="dev/kind/kind-config.yaml"

# ----------------------------------------------------------------------
# Service Identity (driven by SERVICE env var, like Makefile)
# ----------------------------------------------------------------------
SERVICE_NAME="${SERVICE:-hello}"          # read $SERVICE, default 'hello'
readonly SERVICE_NAME
readonly IMAGE_TAG="local"
readonly APP_IMAGE="logiflow/${SERVICE_NAME}:${IMAGE_TAG}"

# ----------------------------------------------------------------------
# Kubernetes & network
# ----------------------------------------------------------------------
readonly NAMESPACE="logiflow"
readonly LOCAL_PORT="8080"
readonly SERVICE_PORT="8080"

# ----------------------------------------------------------------------
# Health check
# ----------------------------------------------------------------------
readonly HEALTH_ENDPOINT="/healthz"
readonly MAX_RETRIES=10
readonly RETRY_DELAY=2

# --- Pretty-printing ---
GREEN='\033[0;32m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m'

log_info() { echo -e "${BOLD}[*]${NC} $1"; }
log_ok()  { echo -e "  ${GREEN}✓${NC} $1"; }
log_fail(){ echo -e "  ${RED}✗${NC} $1"; exit 1; }

# --- Cleanup on exit (kill port-forward) ---
cleanup() {
    if [[ -n "${PF_PID:-}" ]]; then
        kill "$PF_PID" 2>/dev/null || true
        wait "$PF_PID" 2>/dev/null || true
        log_info "Port-forward cleaned up."
    fi
}
trap cleanup EXIT

# ======================================================================
# 1. Validate local toolchain (no jq dependency)
# ======================================================================
validate_environment() {
    log_info "Validating environment..."
    local deps=("docker" "kind" "kubectl" "helm")
    for cmd in "${deps[@]}"; do
        if ! command -v "$cmd" &>/dev/null; then
            log_fail "Missing command: $cmd. Please install it."
        fi
    done

    local docker_ver
    docker_ver=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "unknown")

    local kubectl_ver
    kubectl_ver=$(kubectl version --client --short 2>/dev/null || kubectl version --client)

    log_ok "docker ${docker_ver}, kind $(kind version -q), ${kubectl_ver}, helm $(helm version --short)"
}

# ======================================================================
# 2. Ensure Kind cluster exists (idempotent)
# ======================================================================
ensure_cluster() {
    log_info "Ensuring Kind cluster '${KIND_CLUSTER_NAME}' exists..."
    if kind get clusters 2>/dev/null | grep -q "^${KIND_CLUSTER_NAME}$"; then
        log_ok "Cluster '${KIND_CLUSTER_NAME}' already present"
    else
        kind create cluster --name "$KIND_CLUSTER_NAME" --config "$KIND_CONFIG"
        log_ok "Cluster '${KIND_CLUSTER_NAME}' created"
    fi
    kubectl config use-context "kind-${KIND_CLUSTER_NAME}" &>/dev/null
}

# ======================================================================
# 3. Build image (delegates to Makefile – inherits SERVICE from env)
# ======================================================================
build_image() {
    log_info "Building Docker image via Makefile..."
    make build
    log_ok "Image '${APP_IMAGE}' built"
}

# ======================================================================
# 4. Load image into Kind
# ======================================================================
load_image() {
    log_info "Loading image '${APP_IMAGE}' into cluster '${KIND_CLUSTER_NAME}'..."
    kind load docker-image "$APP_IMAGE" --name "$KIND_CLUSTER_NAME"
    log_ok "Image '${APP_IMAGE}' loaded into '${KIND_CLUSTER_NAME}'"
}

# ======================================================================
# 5. Helm lint (delegates to Makefile)
# ======================================================================
lint_chart() {
    log_info "Linting Helm chart via Makefile..."
    make lint
    log_ok "Chart lint passed"
}

# ======================================================================
# 6. Helm template (delegates to Makefile)
# ======================================================================
template_chart() {
    log_info "Templating Helm chart via Makefile..."
    make template
    log_ok "Template rendered successfully"
}

# ======================================================================
# 7. Deploy release (delegates to Makefile)
# ======================================================================
deploy_release() {
    log_info "Deploying Helm release via Makefile..."
    make deploy
    log_ok "Release '${SERVICE_NAME}' deployed"
}

# ======================================================================
# 8. Wait for pod readiness
# ======================================================================
wait_for_readiness() {
    log_info "Waiting for pod with label app.kubernetes.io/name=${SERVICE_NAME}..."
    if ! kubectl wait --for=condition=ready pod \
        -l "app.kubernetes.io/name=${SERVICE_NAME}" \
        -n "$NAMESPACE" \
        --timeout=120s; then
        log_fail "Pod readiness timeout. Debug: kubectl describe pod, kubectl logs, kubectl get events"
    fi
    local pod=$(
        kubectl get pods \
            -l "app.kubernetes.io/name=${SERVICE_NAME}" \
            -n "$NAMESPACE" \
            -o jsonpath='{.items[0].metadata.name}'
    )
    log_ok "Pod '${pod}' is ready"
}

# ======================================================================
# 9. Start port-forward
# ======================================================================
start_port_forward() {
    log_info "Starting port-forward: localhost:${LOCAL_PORT} -> svc/${SERVICE_NAME}:${SERVICE_PORT} in ${NAMESPACE}..."
    kubectl port-forward "svc/$SERVICE_NAME" "$LOCAL_PORT:$SERVICE_PORT" -n "$NAMESPACE" &
    PF_PID=$!
    sleep 1
    if ! kill -0 $PF_PID 2>/dev/null; then
        log_fail "Port-forward failed to start"
    fi
    log_ok "Port-forward running (pid $PF_PID)"
}

# ======================================================================
# 10. Health verification with retry
# ======================================================================
verify_health() {
    log_info "Verifying health at http://localhost:${LOCAL_PORT}${HEALTH_ENDPOINT}..."
    local url="http://localhost:${LOCAL_PORT}${HEALTH_ENDPOINT}"
    local attempt=1
    while [ $attempt -le $MAX_RETRIES ]; do
        if curl -s --max-time 2 "$url" > /dev/null; then
            log_ok "Health check passed (attempt $attempt)"
            return 0
        fi
        log_info "Health check attempt $attempt failed, retrying in ${RETRY_DELAY}s..."
        sleep $RETRY_DELAY
        attempt=$((attempt + 1))
    done
    log_fail "Health check failed after $MAX_RETRIES attempts. Check pod logs."
}

# ======================================================================
# Main
# ======================================================================
main() {
    echo -e "${BOLD}LogiFlow Dev Environment – one command to rule them all${NC}\n"
    validate_environment
    ensure_cluster
    build_image
    load_image
    lint_chart
    template_chart
    deploy_release
    wait_for_readiness
    start_port_forward
    verify_health

    echo ""
    echo -e "${GREEN}${BOLD}Environment Ready 🚀${NC}"
    echo -e "Service accessible at ${BOLD}http://localhost:${LOCAL_PORT}${HEALTH_ENDPOINT}${NC}"
    echo "Press Ctrl+C to stop port-forward and exit."
    wait $PF_PID
}

main "$@"