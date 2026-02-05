# Makefile for Realtime Messaging Platform
# All targets delegate to Docker containers per ADR-014 (PR0-INV-1).
# No Go, buf, or lint tools are invoked directly on the host.

.PHONY: all dev up down logs lint fmt test test-integration proto proto-lint proto-breaking build docker ci-local clean help

# Default target
all: ci-local

# ============================================================================
# Development
# ============================================================================

## Start development environment with hot reload
dev:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml up --build

## Start infrastructure only (LocalStack, Redpanda, Redis)
up:
	docker compose up -d

## Stop all services
down:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml down -v

## View logs (use SERVICE=name to filter)
logs:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml logs -f $(SERVICE)

# ============================================================================
# Code Quality (Docker-only per PR0-INV-1)
# ============================================================================

## Run linters (golangci-lint)
lint:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox \
		golangci-lint run ./...

## Run gofmt (format all Go files in-place)
fmt:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox \
		golangci-lint fmt

## Run architectural linting (go-arch-lint)
lint-arch:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox \
		arch-go

## Run all linters
lint-all: lint lint-arch

# ============================================================================
# Testing (Docker-only per PR0-INV-1)
# ============================================================================

## Run unit tests with race detection
test:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox \
		go test -race -v ./...

## Run unit tests with coverage
test-coverage:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox \
		go test -race -coverprofile=coverage.txt -covermode=atomic ./...

## Run integration tests (requires infrastructure up)
test-integration:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox \
		go test -race -tags=integration -v ./...

# ============================================================================
# Proto (Docker-only per PR0-INV-1)
# ============================================================================

## Generate Go code from proto files
proto:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox \
		sh -c "cd proto && buf dep update && buf generate"

## Lint proto files
proto-lint:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox \
		sh -c "cd proto && buf lint"

## Check for breaking changes against main branch
proto-breaking:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox \
		sh -c "cd proto && buf breaking --against '../.git#branch=main,subdir=proto'"

# ============================================================================
# Build (Docker-only per PR0-INV-1)
# ============================================================================

## Build all service binaries
build:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox \
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
ci-local: proto-lint lint test build docker
	@echo "✅ CI pipeline passed"

## Run CI pipeline without Docker build (faster)
ci-fast: proto-lint lint test
	@echo "✅ Fast CI passed"

# ============================================================================
# Utilities
# ============================================================================

## Run a command in the toolbox container
toolbox:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox $(CMD)

## Download Go dependencies
deps:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox \
		go mod download

## Tidy Go modules
tidy:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml run --rm toolbox \
		go mod tidy

## Clean build artifacts and caches
clean:
	docker compose -f docker-compose.yaml -f docker-compose.dev.yaml down -v
	rm -rf tmp/ gen/ coverage.txt

## Display help
help:
	@echo "Realtime Messaging Platform - Makefile targets"
	@echo ""
	@echo "Development:"
	@echo "  make dev              Start development environment with hot reload"
	@echo "  make up               Start infrastructure only"
	@echo "  make down             Stop all services"
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
	@echo ""
	@echo "Utilities:"
	@echo "  make toolbox CMD=...  Run command in toolbox"
	@echo "  make deps             Download Go dependencies"
	@echo "  make tidy             Tidy Go modules"
	@echo "  make clean            Clean artifacts and caches"
