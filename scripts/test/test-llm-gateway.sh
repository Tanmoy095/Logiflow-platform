#!/usr/bin/env bash
# scripts/test/test-llm-gateway.sh
#
# Run tests for the LLM gateway service.
# Optional argument: test name filter (substring matched against test names).
# Example:
#   ./scripts/test/test-llm-gateway.sh
#   ./scripts/test/test-llm-gateway.sh TestServiceComplete_ShipmentEvidenceFixture

set -euo pipefail

# Resolve repository root (two levels up from scripts/test/)
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

# If a test name filter is provided, use it with -run
TEST_FILTER="${1:-}"

if [[ -n "$TEST_FILTER" ]]; then
    echo "Running llm-gateway tests matching: $TEST_FILTER"
    go test -v ./services/llm-gateway/application -run "$TEST_FILTER"
else
    echo "Running all llm-gateway tests"
    go test -v ./services/llm-gateway/...
fi