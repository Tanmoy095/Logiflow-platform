# ============================================================
# LogiFlow Platform Developer Makefile
# ============================================================

# --- Service configuration (override with SERVICE=name) ---
SERVICE ?= hello
export SERVICE         # <-- so dev-up.sh and nested make calls inherit it

IMAGE_NAME ?= logiflow/$(SERVICE)
IMAGE_TAG  ?= local
APP_IMAGE  := $(IMAGE_NAME):$(IMAGE_TAG)

DOCKERFILE := build/Dockerfile.$(SERVICE)

# --- Cluster & deployment config ---
KIND_CLUSTER_NAME ?= logiflow-dev
NAMESPACE         ?= logiflow
CHART_PATH        ?= deployment/helm/services/$(SERVICE)
SERVICE_NAME      ?= $(SERVICE)
LOCAL_PORT        ?= 8080
SERVICE_PORT      ?= 8080

# --- Go config ---
GO_TEST_FLAGS ?= -v ./...

.DEFAULT_GOAL := help

# ============================================================
# Self-documenting help
# ============================================================
.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ============================================================
# Environment validation
# ============================================================
.PHONY: doctor
doctor: ## Run environment validation (doctor.sh)
	@bash scripts/dev/doctor.sh

# ============================================================
# Build
# ============================================================
.PHONY: build
build: ## Build Docker image
	@echo "Building service: $(SERVICE)"
	@echo "Image: $(APP_IMAGE)"
	@echo "Dockerfile: $(DOCKERFILE)"
	@test -f "$(DOCKERFILE)" || \
		(echo "ERROR: Dockerfile not found: $(DOCKERFILE)" && exit 1)
	docker build -f $(DOCKERFILE) -t $(APP_IMAGE) .

# ============================================================
# Full local development environment
# ============================================================
.PHONY: dev-up
dev-up: doctor ## Start the complete local environment (doctor + dev-up.sh)
	@bash scripts/dev/dev-up.sh

.PHONY: dev-down
dev-down: ## Tear down the local Kind cluster
	@echo "Deleting Kind cluster '$(KIND_CLUSTER_NAME)'..."
	-kind delete cluster --name $(KIND_CLUSTER_NAME)

# ============================================================
# Helm chart operations
# ============================================================
.PHONY: lint
lint: ## Lint the Helm chart
	helm lint $(CHART_PATH) --namespace $(NAMESPACE)

.PHONY: template
template: ## Render the Helm chart locally (dry-run)
	helm template $(SERVICE_NAME) $(CHART_PATH) \
		--namespace $(NAMESPACE) \
		--set image.repository=$(IMAGE_NAME) \
		--set image.tag=$(IMAGE_TAG) \
		--set service.port=$(SERVICE_PORT)

.PHONY: deploy
deploy: ## Deploy/upgrade the Helm release
	kubectl create namespace $(NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	helm upgrade --install $(SERVICE_NAME) $(CHART_PATH) \
		--namespace $(NAMESPACE) \
		--set image.repository=$(IMAGE_NAME) \
		--set image.tag=$(IMAGE_TAG) \
		--set service.port=$(SERVICE_PORT) \
		--wait --timeout 60s

# ============================================================
# Testing
# ============================================================
.PHONY: test
test: ## Run all Go unit tests
	go test $(GO_TEST_FLAGS)

# ============================================================
# Observability / Debugging
# ============================================================
.PHONY: status
status: ## Show cluster resources status
	@echo "=== Pods ==="
	kubectl get pods -n $(NAMESPACE)
	@echo ""
	@echo "=== Services ==="
	kubectl get svc -n $(NAMESPACE)
	@echo ""
	@echo "=== Endpoints ==="
	kubectl get endpoints -n $(NAMESPACE)

.PHONY: logs
logs: ## Tail logs from the hello pod
	kubectl logs -l app.kubernetes.io/name=$(SERVICE_NAME) -n $(NAMESPACE) --tail=50 -f

.PHONY: port-forward
port-forward: ## Forward local port to hello service
	@echo "Forwarding localhost:$(LOCAL_PORT) -> svc/$(SERVICE_NAME):$(SERVICE_PORT) ..."
	kubectl port-forward svc/$(SERVICE_NAME) $(LOCAL_PORT):$(SERVICE_PORT) -n $(NAMESPACE)

.PHONY: health-check
health-check: ## Quick health check of the running service
	@curl -s http://localhost:$(LOCAL_PORT)/healthz && echo "OK" || echo "FAILED"

# ============================================================
# Cleanup
# ============================================================
.PHONY: clean
clean: dev-down ## Remove generated files and cluster
	@echo "Cleaning up..."
	-docker rmi $(APP_IMAGE) 2>/dev/null
	@echo "Done."