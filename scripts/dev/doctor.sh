#!/usr/bin/env bash
# doctor.sh – LogiFlow environment pre-flight check
# Run with: make doctor or ./scripts/dev/doctor.sh
# Fails fast if any core assumption is broken.

set -euo pipefail   # Exit on error, undefined variable, or pipe failure

# ---- Colors (nice output) ----
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

pass() { echo -e "${GREEN}✓${NC} $1"; }
fail() { echo -e "${RED}✗${NC} $1"; exit 1; }
warn() { echo -e "${YELLOW}⚠${NC} $1"; }

echo "=== LogiFlow doctor.sh ==="

# ------------------------------------------------------------
# 1. TOOLCHAIN CHECKS – are required CLIs present?
# ------------------------------------------------------------
echo "[toolchain]"

# Go check
if command -v go &>/dev/null; then
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    REQUIRED_MAJOR=1
REQUIRED_MINOR=22

GO_MAJOR=$(echo "$GO_VERSION" | cut -d. -f1)
GO_MINOR=$(echo "$GO_VERSION" | cut -d. -f2)

if (( GO_MAJOR > REQUIRED_MAJOR )) || (( GO_MAJOR == REQUIRED_MAJOR && GO_MINOR >= REQUIRED_MINOR )); then
    pass "Go $GO_VERSION"
else
    fail "Go version $GO_VERSION – need ${REQUIRED_MAJOR}.${REQUIRED_MINOR}+"
fi
else
    fail "go is not installed"
fi

# Docker check
if command -v docker &>/dev/null; then
    pass "docker CLI found"
else
    fail "docker CLI not found"
fi

# Git check (you already use Git)
command -v git &>/dev/null && pass "git CLI found" || fail "git not found"

# ------------------------------------------------------------
# 2. RUNTIME DAEMONS – are the engines running?
# ------------------------------------------------------------
echo "[daemons]"

# Docker daemon must be alive
if docker info &>/dev/null; then
    pass "Docker daemon running"
else
    fail "Docker daemon is not running – start Docker Desktop"
fi

# (Optional) Kubernetes check – if kubectl is installed, check cluster
if command -v kubectl &>/dev/null; then
    if kubectl cluster-info &>/dev/null; then
        pass "Kubernetes cluster reachable"
    else
        warn "kubectl installed but no cluster reachable (ignore if not using K8s yet)"
    fi
fi

# ------------------------------------------------------------
# 3. ENVIRONMENT & PROJECT STATE – is the repo in good shape?
# ------------------------------------------------------------
echo "[environment]"

# Current branch
CURRENT_BRANCH=$(git branch --show-current)
if [ "$CURRENT_BRANCH" = "main" ] || [ "$CURRENT_BRANCH" = "master" ]; then
    warn "You are on '$CURRENT_BRANCH' – consider using a feature branch"
else
    pass "branch '$CURRENT_BRANCH'"
fi

# Dirty worktree (uncommitted changes)
if git diff --quiet && git diff --cached --quiet; then
    pass "working tree clean"
else
    warn "uncommitted changes present"
fi

# Check required environment variables (customize for your services)
REQUIRED_VARS=("PORT" "SERVICE_NAME")   # add more as your project grows
for var in "${REQUIRED_VARS[@]}"; do
    if [ -z "${!var:-}" ]; then
        warn "env var $var is not set (using default in code if any)"
    else
        pass "env var $var = ${!var}"
        #KuberNetes compliance check 

        if ["$var" == "SERVICE_NAME" ]; then
            if [[ ! "$SERVICE_NAME" =~ ^[a-z0-9-]+$ ]]; then
                fail "SERVICE_NAME must be lowercase letters, numbers, and hyphens only (kubernetes compliant)"
            fi
        fi
    fi
done

# Check if the target port is available
check_target_port() {

    local target_port="${1:-8080}"  # default to 8080 if not provided
    if command -v lsof &>/dev/null; then
        if lsof -i :"$target_port" -sTCP:LISTEN &>/dev/null; then
            fail "Port $target_port is in use – stop the process using it"
        else
            pass "Port $target_port is available"
        fi
    elif command -v ss &>/dev/null; then
        if ss -ltn | awk '{print $4}' | grep -q ":$target_port$"; then
            fail "Port $target_port is in use – stop the process using it"
        else
            pass "Port $target_port is available"
        fi
    else
        warn "Neither lsof nor ss found – cannot check port availability"
    fi
}

#call the function to check port 8080
check_target_port 8080
# ------------------------------------------------------------
# 4. DISK SPACE (basic) – enough room to build
# ------------------------------------------------------------
echo "[resources]"
AVAILABLE_KB=$(df . | tail -1 | awk '{print $4}')
AVAILABLE_GB=$((AVAILABLE_KB / 1024 / 1024))
if [ "$AVAILABLE_GB" -lt 1 ]; then
    warn "Less than 1 GB free disk space – builds may fail"
else
    pass "disk space ~${AVAILABLE_GB} GB available"
fi

echo -e "\n${GREEN}All essential checks passed.${NC} You're ready to build!"