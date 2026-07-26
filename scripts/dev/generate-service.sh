#!/usr/bin/env bash
# Usage: SERVICE=stream-ingestion make generate-service
set -euo pipefail

SERVICE_NAME="${SERVICE:?SERVICE env var required}"
MODULE_BASE="github.com/Tanmoy095/LogiFlow-Platform"

echo "Generating service: ${SERVICE_NAME}"

# 1. Create services directory if needed
mkdir -p services

# 2. Copy service body
if [ -d "services/${SERVICE_NAME}" ]; then
    echo "Error: services/${SERVICE_NAME} already exists."
    exit 1
fi
cp -r service-template "services/${SERVICE_NAME}"

# 3. Copy command skeleton
if [ -d "cmd/${SERVICE_NAME}" ]; then
    echo "Error: cmd/${SERVICE_NAME} already exists."
    exit 1
fi
cp -r cmd/template "cmd/${SERVICE_NAME}"

# 4. Update go.mod module path
sed -i "s|github.com/Tanmoy095/LogiFlow-Platform/service-template|${MODULE_BASE}/services/${SERVICE_NAME}|g" "services/${SERVICE_NAME}/go.mod"

# 5. Update all Go import paths inside the service
find "services/${SERVICE_NAME}" -name "*.go" -exec sed -i "s|github.com/Tanmoy095/LogiFlow-Platform/service-template|${MODULE_BASE}/services/${SERVICE_NAME}|g" {} \;

echo "Service ${SERVICE_NAME} created successfully."
echo "Next steps:"
echo "  - Implement domain entities in services/${SERVICE_NAME}/domain/"
echo "  - Implement use cases in services/${SERVICE_NAME}/application/"
echo "  - Wire up composition root in cmd/${SERVICE_NAME}/main.go"
echo "  - Build: go build ./cmd/${SERVICE_NAME}/..."