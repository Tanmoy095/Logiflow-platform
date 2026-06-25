#!/usr/bin/env bash
# doctor.sh – LogiFlow environment pre-flight check
# Run with: make doctor or ./scripts/dev/doctor.sh
# Validates all assumptions a developer needs before starting work.
# - Hard failures (exit 1) = Missing critical dependencies (stops the script immediately).
# - Warnings = Project configuration issues (alerts the user but allows the script to finish).

set -euo pipefail   # Exit immediately on error, undefined variable, or pipe failure

# ------------------------------------------------------------
# Formatting & Colors
# ------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

pass() { echo -e "${GREEN}✓${NC} $1"; }
fail() { echo -e "${RED}✗${NC} $1"; exit 1; }  # exit 1 kills the script immediately
warn() { echo -e "${YELLOW}⚠${NC} $1"; }

# ------------------------------------------------------------
# 1. Toolchain & Daemons (Hard Failures)
# ------------------------------------------------------------

check_go() {
    if ! command -v go &>/dev/null; then
        fail "Go is not installed"
    fi
    local version
    version=$(go version | awk '{print $3}' | sed 's/go//')
    # Checks if version is NOT 1.22+ or 1.30+
    if [[ "$version" != 1.2[2-9]* ]] && [[ "$version" != 1.[3-9]* ]]; then
        fail "Go version $version – need ≥ 1.22"
    fi
    pass "Go $version"
}

check_git() {
    command -v git &>/dev/null || fail "Git is not installed"
    pass "Git $(git --version | awk '{print $3}')"
}

check_docker() {
    command -v docker &>/dev/null || fail "Docker is not installed"
    pass "Docker $(docker --version | awk '{print $3}' | sed 's/,//')"
}

check_docker_daemon() {
    if ! docker info &>/dev/null; then
        fail "Docker daemon is not running. Start Docker Desktop."
    fi
    pass "Docker daemon running"
}
check_kind() {
    command -v kind &>/dev/null || fail "Kind is not installed"
    pass "Kind $(kind version -q)"
}

check_helm() {
    command -v helm &>/dev/null || fail "Helm is not installed"
    pass "Helm $(helm version --short | head -1)"
}

check_make() {
    command -v make &>/dev/null || fail "Make is not installed"
    pass "Make found"
}

check_git_repo() {
    # Must be in a git repo for the branch/clean checks to work
    if ! git rev-parse --is-inside-work-tree &>/dev/null; then
        fail "Not inside a Git repository"
    fi
    pass "Inside Git repository"
}

# ------------------------------------------------------------
# 2. Environment & State (Warnings & Project Config)
# ------------------------------------------------------------

check_git_branch() {
    local branch
    branch=$(git branch --show-current)
    if [[ "$branch" == "main" || "$branch" == "master" ]]; then
        warn "On protected branch '$branch' – consider using a feature branch"
    else
        pass "Branch: $branch"
    fi
}

check_git_clean() {
    if ! git diff --quiet || ! git diff --cached --quiet; then
        warn "Working tree has uncommitted changes"
    else
        pass "Working tree clean"
    fi
}

check_port() {
    local port=${1:-8080}
    # First try lsof
    if command -v lsof &>/dev/null; then
        if lsof -i :"$port" -sTCP:LISTEN &>/dev/null; then
            fail "Port $port is in use – stop the process using it"
        else
            pass "Port $port available"
        fi
    # Fallback to ss if lsof is missing
    elif command -v ss &>/dev/null; then
        if ss -ltn | awk '{print $4}' | grep -q ":$port$"; then
            fail "Port $port is in use – stop the process using it"
        else
            pass "Port $port available"
        fi
    else
        warn "Cannot check port availability (neither lsof nor ss found)"
    fi
}

check_env_vars() {
    for var in PORT SERVICE_NAME; do
        if [ -z "${!var:-}" ]; then
            # We warn here so the dev knows, but we don't return an error code 
            # to prevent 'set -e' from crashing the script (e.g. make Error 1).
            warn "Environment variable $var is not set (using defaults)"
        else
            pass "Env $var = ${!var}"
            
            # Kubernetes compliance check: SERVICE_NAME must be strictly formatted
            if [ "$var" == "SERVICE_NAME" ]; then
                if [[ ! "${!var}" =~ ^[a-z0-9-]+$ ]]; then
                    fail "SERVICE_NAME must be lowercase letters, numbers, and hyphens only (K8s compliant)"
                fi
            fi
        fi
    done
}

check_disk_space() {
    local available_kb
    local available_gb
    available_kb=$(df . | tail -1 | awk '{print $4}')
    available_gb=$((available_kb / 1024 / 1024))
    
    if [ "$available_gb" -lt 1 ]; then
        warn "Less than 1 GB free disk space – builds may fail"
    else
        pass "Disk space ~${available_gb} GB available"
    fi
}

# ------------------------------------------------------------
# 3. Main Execution
# ------------------------------------------------------------
echo "=== LogiFlow Doctor ==="
echo ""

# Run hard failures first (will kill script instantly if they fail)
check_go
check_git
check_docker
check_docker_daemon
check_make
check_git_repo
check_kind
check_helm

# Run environment checks (will warn, but allow script to continue)
check_port 8080
check_env_vars
check_git_branch
check_git_clean
check_disk_space

echo ""
echo -e "${GREEN}=== All checks completed successfully. You're ready to build! ===${NC}"