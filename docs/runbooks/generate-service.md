# Runbook: Generating a New LogiFlow Service

This runbook explains how to create a new microservice from the platform's standard DDD skeleton. The scaffolding process is fully automated and takes less than five seconds, producing a compilable Go service structure that enforces the project's architectural contracts.

## 1. Prerequisites

Before generating a new service, verify that the following files and directories exist at the repository root:

```bash
# The service template (business layers only, no cmd/)
ls service-template/domain/
ls service-template/application/
ls service-template/interfaces/
ls service-template/infrastructure/

# The composition root skeleton
ls cmd/template/main.go

# The generation script (must be executable)
ls -l scripts/dev/generate-service.sh
```

If any of these are missing, generation will fail. Re-create them according to the docs/folderstructure.md contract before proceeding.

## 2. Generate the Service

Use the Makefile target to invoke the generator. The target expects the SERVICE environment variable to contain the desired service name (lowercase, hyphens allowed).

```bash
SERVICE=<service-name> make generate-service
```

Example for creating a fraud-detection service:

```bash
SERVICE=fraud-detection make generate-service
```

What the script does internally:

- Copies service-template/ to services/<service-name>/.
- Copies cmd/template/ to cmd/<service-name>/.
- Replaces the Go module path in the new service's go.mod from the template placeholder to the actual service module path.
- Replaces all import paths in the generated Go files to match the new module.
- Prints a summary of the created directories and next steps.

The script is idempotent. It will refuse to overwrite an existing services/<name> or cmd/<name> directory.

## 3. What the Generation Produces

After running the command, you will have:

```text
cmd/<service-name>/
  main.go                          # composition root (wiring only)

services/<service-name>/
  domain/
    model.go                       # entities and value objects
    errors.go                      # domain-specific errors
    policy.go                      # business rules / policies
    events.go                      # domain events
  application/
    commands.go                    # command types (write operations)
    queries.go                     # query types (read operations)
    ports.go                       # interfaces for infrastructure
    service.go                     # application service struct
    service_test.go                # table-driven test skeleton
  interfaces/
    http/handler.go                # HTTP adapters
    grpc/handler.go                # gRPC adapters
    kafka/handler.go               # Kafka consumers
    mcp/handler.go                 # Model Context Protocol adapters
    temporal/handler.go            # Temporal activities / workflows
  infrastructure/
    postgres/repository.go         # PostgreSQL implementations
    redis/repository.go             # Redis implementations
    kafka/repository.go            # Kafka producer implementations
    provider/repository.go         # third-party API clients
    keycloak/repository.go         # Keycloak / IAM clients
  migrations/
    0001_init.sql                  # database migration placeholder
  go.mod                           # module: .../services/<service-name>
  README.md                        # service-specific documentation
```

Every file contains a minimal package declaration and a TODO comment, so the project compiles immediately. The composition root at cmd/<service-name>/main.go contains a commented-out wiring sequence that follows the standard lifecycle: config -> logging -> infrastructure -> application -> server -> graceful shutdown.

## 4. Verify the Skeleton

### 4.1 Compile the business layers

The service packages (domain, application, and so on) should compile without errors as soon as generation finishes.

```bash
cd services/<service-name>
go build ./...
cd -
```

### 4.2 Check the command syntax

The composition root is not yet wired because it contains commented-out code, but you can still run go vet to catch syntax errors:

```bash
cd cmd/<service-name>
go vet ./...
cd -
```

### 4.3 Verify the directory structure

Run tree or find to confirm that every layer and adapter folder exists:

```bash
tree services/<service-name>
tree cmd/<service-name>
```

If any folder is missing, the script may have failed silently. Re-run generation after fixing any missing source templates.

### 4.4 Inspect the import paths

Open a few .go files inside services/<service-name>/interfaces/ and infrastructure/ and confirm that the import paths point to the correct service module, for example github.com/Tanmoy095/LogiFlow-Platform/services/fraud-detection.

If they still reference service-template, the generation script's sed replacement may have failed because of a different base module path. Adjust MODULE_BASE in scripts/dev/generate-service.sh accordingly and re-run.

## 5. Next Steps: Making the Service Real

At this point the service is a hollow shell that enforces the DDD contract. To make it functional, follow these steps in order.

### 5.1 Wire the composition root

Edit cmd/<service-name>/main.go and uncomment the placeholder blocks. Replace the empty import statements with the real packages:

```go
import (
    "github.com/Tanmoy095/LogiFlow-Platform/pkg/technical/observability"
    "github.com/Tanmoy095/LogiFlow-Platform/pkg/technical/server"
    "github.com/Tanmoy095/LogiFlow-Platform/services/fraud-detection/application"
    servicehttp "github.com/Tanmoy095/LogiFlow-Platform/services/fraud-detection/interfaces/http"
    // ... additional imports for infrastructure
)
```

Then fill in each wiring step with actual code: load config, create repositories, instantiate the application service, build routes, and start the server. Refer to docs/runbooks/wiring-composition-root.md once it is created for the canonical pattern.

### 5.2 Implement business logic

- Domain: define entities, value objects, and domain errors.
- Application: define ports (interfaces) and implement use-case orchestration.
- Interfaces: implement HTTP handlers and gRPC servers that call the application layer.
- Infrastructure: implement the ports, such as PostgreSQL repositories, Redis caches, and Kafka producers.

The TODO comments in each placeholder file serve as inline guidance.

### 5.3 Add the Helm chart

Copy an existing service chart, such as deployment/helm/services/hello/, to deployment/helm/services/<service-name>/. Edit values-dev.yaml and the staging/prod overrides to set the image name, port, and any custom environment variables. The deployment.yaml and service.yaml templates require no changes because they only contain include calls to the library chart.

```bash
cp -r deployment/helm/services/hello deployment/helm/services/<service-name>
# edit values-dev.yaml, values-staging.yaml, values-prod.yaml
# add values-prod-secrets.yaml (DO NOT COMMIT)
```

### 5.4 Build the Docker image

Create build/Dockerfile.<service-name> that builds the binary from ./cmd/<service-name>:

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /<service-name> ./cmd/<service-name>

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /<service-name> /<service-name>
USER 1000
EXPOSE 8080
ENTRYPOINT ["/<service-name>"]
```

### 5.5 Deploy locally

```bash
SERVICE=<service-name> make dev-up
```

This builds the image, loads it into Kind, lints and templates the Helm chart, deploys, waits for readiness, and sets up a port-forward.

## 6. Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| make generate-service fails with "already exists" | A service with that name already exists | Choose a different name or remove the existing service directory |
| go build fails with import path errors | The module path in go.mod or import statements is wrong | Verify MODULE_BASE in the generation script and re-run |
| helm lint fails after copying the chart | The chart references hello-specific config | Replace hello references in values.yaml with the new service name |
| make dev-up fails with ImagePullBackOff | The Docker image has not been built | Run docker build -f build/Dockerfile.<service> -t logiflow/<service>:local . and kind load docker-image logiflow/<service>:local --name logiflow-dev |

## 7. Reference

- Service template: service-template/
- Command skeleton: cmd/template/main.go
- Generator script: scripts/dev/generate-service.sh
- Helm library chart: deployment/helm/library/logiflow-service/
- Example service: deployment/helm/services/hello/
- Debug playbook: docs/runbooks/debug-playbook.md