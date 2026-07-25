# Makefile for Realtime Messaging Platform
# All targets delegate to Docker containers per ADR-014 (PR0-INV-1).
# No Go, buf, or lint tools are invoked directly on the host.

.PHONY: all dev down logs lint fmt test test-integration proto proto-lint proto-breaking build docker ci-local ci-fast check-no-aws clean help \
	terraform-fmt terraform-fmt-fix terraform-validate terraform-lint terraform-security

# Default target
all: ci-local

# ============================================================================
# Development
# ============================================================================

## Start the health-only services with hot reload
dev:
	docker compose up --build

## Stop all services and remove volumes
down:
	docker compose down -v

## View logs (use SERVICE=name to filter)
logs:
	docker compose logs -f $(SERVICE)

# ============================================================================
# Code Quality (Docker-only per PR0-INV-1)
# ============================================================================

## Run linters (golangci-lint)
lint:
	docker compose run --rm toolbox \
		golangci-lint run ./...

## Run gofmt (format all Go files in-place)
fmt:
	docker compose run --rm toolbox \
		golangci-lint fmt

## Run architectural linting (go-arch-lint)
lint-arch:
	docker compose run --rm toolbox \
		arch-go

## Run all linters
lint-all: lint lint-arch

# ============================================================================
# Testing (Docker-only per PR0-INV-1)
# ============================================================================

## Run unit tests with race detection (excludes cmd/ and gen/ from coverage)
test:
	docker compose run --rm toolbox \
		sh -c 'go test -race -v $$(go list ./... | grep -v -E "cmd/|gen/")'

## Run unit tests with coverage (excludes cmd/ and gen/ from coverage)
test-coverage:
	docker compose run --rm toolbox \
		sh -c 'go test -race -coverprofile=coverage.txt -covermode=atomic $$(go list ./... | grep -v -E "cmd/|gen/")'

## Run integration tests (requires infrastructure up)
test-integration:
	docker compose run --rm toolbox \
		go test -race -tags=integration -v ./...

# ============================================================================
# Proto (Docker-only per PR0-INV-1)
# ============================================================================

## Generate Go code and OpenAPI spec from proto files
proto:
	docker compose run --rm toolbox \
		sh -c "cd proto && buf dep update && buf generate && buf generate --template buf.gen.openapi.yaml --path messaging/v1/chatmgmt.proto"

## Lint proto files
proto-lint:
	docker compose run --rm toolbox \
		sh -c "cd proto && buf lint"

## Check for breaking changes against main branch
proto-breaking:
	docker compose run --rm toolbox \
		sh -c "cd proto && buf breaking --against '../.git#branch=main,subdir=proto'"

# ============================================================================
# Build (Docker-only per PR0-INV-1)
# ============================================================================

## Build all service binaries
build:
	docker compose run --rm toolbox \
		go build -v ./cmd/...

## Build production Docker images
docker:
	docker build -f docker/gateway.Dockerfile -t messaging-gateway:latest .
	docker build -f docker/ingest.Dockerfile -t messaging-ingest:latest .
	docker build -f docker/fanout.Dockerfile -t messaging-fanout:latest .
	docker build -f docker/chatmgmt.Dockerfile -t messaging-chatmgmt:latest .

# ============================================================================
# CI (Docker-only per PR0-INV-1)
# ============================================================================

## Run full CI pipeline locally
ci-local: check-no-aws proto-lint lint test build docker
	@echo "✅ CI pipeline passed"

## Run CI pipeline without Docker build (faster)
ci-fast: check-no-aws proto-lint lint test
	@echo "✅ Fast CI passed"

## Gate: no AWS SDK imports may remain (M0.1, ADR-021 substrate migration)
check-no-aws:
	@if grep -rn "aws-sdk-go" --include="*.go" . ; then \
		echo "❌ AWS SDK imports found — M0.1 requires none"; exit 1; \
	else \
		echo "✅ no AWS SDK imports"; \
	fi

# ============================================================================
# Terraform (Docker-only per PR0-INV-1)
# ============================================================================

TF_IMAGE := hashicorp/terraform:1.14
TF_DOCKER := docker run --rm -v "$(CURDIR)/terraform:/terraform" -w /terraform
TFLINT_IMAGE := ghcr.io/terraform-linters/tflint:v0.55.1
TRIVY_IMAGE := aquasec/trivy:0.59.1

## Check Terraform formatting
terraform-fmt:
	$(TF_DOCKER) $(TF_IMAGE) fmt -check -recursive

## Fix Terraform formatting
terraform-fmt-fix:
	$(TF_DOCKER) $(TF_IMAGE) fmt -recursive

## Validate Terraform configurations (per environment)
terraform-validate:
	@for env in environments/dev environments/prod; do \
		echo "==> Validating $$env"; \
		$(TF_DOCKER) $(TF_IMAGE) -chdir=$$env init -backend=false > /dev/null 2>&1 && \
		$(TF_DOCKER) $(TF_IMAGE) -chdir=$$env validate || exit 1; \
	done

## Lint Terraform with tflint
terraform-lint:
	docker run --rm -v "$(CURDIR)/terraform:/terraform" -w /terraform --entrypoint sh $(TFLINT_IMAGE) \
		-c "tflint --init --config /terraform/.tflint.hcl && tflint --recursive --config /terraform/.tflint.hcl"

## Security scan Terraform with trivy
terraform-security:
	docker run --rm -v "$(CURDIR)/terraform:/terraform" $(TRIVY_IMAGE) \
		config --severity HIGH,CRITICAL --exit-code 1 /terraform

# ============================================================================
# Utilities
# ============================================================================

## Run a command in the toolbox container
toolbox:
	docker compose run --rm toolbox $(CMD)

## Download Go dependencies
deps:
	docker compose run --rm toolbox \
		go mod download

## Tidy Go modules
tidy:
	docker compose run --rm toolbox \
		go mod tidy

## Clean build artifacts and caches
clean:
	docker compose down -v
	rm -rf tmp/ gen/ coverage.txt

## Display help
help:
	@echo "Realtime Messaging Platform - Makefile targets"
	@echo ""
	@echo "Development:"
	@echo "  make dev              Start the health-only services with hot reload"
	@echo "  make down             Stop all services and remove volumes"
	@echo "  make logs             View logs (SERVICE=name to filter)"
	@echo ""
	@echo "Code Quality:"
	@echo "  make lint             Run golangci-lint"
	@echo "  make fmt              Run gofmt on all Go files"
	@echo "  make lint-arch        Run architectural linting"
	@echo "  make lint-all         Run all linters"
	@echo ""
	@echo "Testing:"
	@echo "  make test             Run unit tests"
	@echo "  make test-coverage    Run tests with coverage"
	@echo "  make test-integration Run integration tests"
	@echo ""
	@echo "Proto:"
	@echo "  make proto            Generate Go code from protos"
	@echo "  make proto-lint       Lint proto files"
	@echo "  make proto-breaking   Check for breaking changes"
	@echo ""
	@echo "Build:"
	@echo "  make build            Build service binaries"
	@echo "  make docker           Build production Docker images"
	@echo ""
	@echo "CI:"
	@echo "  make ci-local         Run full CI pipeline locally"
	@echo "  make ci-fast          Run fast CI (no Docker build)"
	@echo "  make check-no-aws     Assert no AWS SDK imports remain"
	@echo ""
	@echo "Terraform:"
	@echo "  make terraform-fmt      Check Terraform formatting"
	@echo "  make terraform-fmt-fix  Fix Terraform formatting"
	@echo "  make terraform-validate Validate Terraform configurations"
	@echo "  make terraform-lint     Lint with tflint"
	@echo "  make terraform-security Security scan with trivy"
	@echo ""
	@echo "Utilities:"
	@echo "  make toolbox CMD=...  Run command in toolbox"
	@echo "  make deps             Download Go dependencies"
	@echo "  make tidy             Tidy Go modules"
	@echo "  make clean            Clean artifacts and caches"
