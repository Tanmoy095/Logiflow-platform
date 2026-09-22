#!/usr/bin/env bash
# scripts/test/test-llm-gateway.sh
#
# Run tests for the LLM gateway service.
#
# Optional argument: test name filter (substring matched against test names
# across all llm-gateway packages).
#
# Examples:
#   ./scripts/test/test-llm-gateway.sh
#   ./scripts/test/test-llm-gateway.sh TestServiceComplete_MetadataSuccess
#   ./scripts/test/test-llm-gateway.sh TestProviderRouter
#   ./scripts/test/test-llm-gateway.sh TestNewService
#
# Flags are deliberately explicit:
#   -race       : the Service spawns goroutines and FakeProvider guards shared
#                 state with a mutex. Race detection must be on by default.
#   -count=1    : disable Go's test result cache so every run actually
#                 re-executes the tests. Without this, a passing cache can
#                 hide a real regression.
#   -timeout    : cap the total run so a hung test fails the CI job instead of
#                 blocking it until the runner's global timeout.
#   -v          : verbose output for CI logs and local debugging.

set -euo pipefail

# Resolve repository root (two levels up from scripts/test/)
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

TEST_FILTER="${1:-}"
GATEWAY_PKG="./services/llm-gateway/..."

if [[ -n "$TEST_FILTER" ]]; then
    echo "Running llm-gateway tests matching: $TEST_FILTER"
    go test \
        -race \
        -count=1 \
        -timeout 5m \
        -v \
        "$GATEWAY_PKG" \
        -run "$TEST_FILTER"
else
    echo "Running all llm-gateway tests"
    go test \
        -race \
        -count=1 \
        -timeout 5m \
        -v \
        "$GATEWAY_PKG"
fi