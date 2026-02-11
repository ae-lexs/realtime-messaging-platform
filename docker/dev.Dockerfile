# Development Dockerfile with Go, tools, Air for hot reload, and Delve for debugging.
# Used by docker-compose.dev.yaml for local development.

FROM golang:1.25-alpine AS base

# Install build dependencies (build-base + binutils provide gcc/ld for CGO/race detector)
RUN apk add --no-cache git make curl build-base binutils-gold

# Install Air for hot reload
RUN go install github.com/air-verse/air@latest

# Install Delve for debugging
RUN go install github.com/go-delve/delve/cmd/dlv@latest

# Install golangci-lint v2 (built from source to match Go 1.25)
RUN go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

# Install buf and protoc plugins for proto generation
RUN go install github.com/bufbuild/buf/cmd/buf@latest && \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest && \
    go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest

# Install arch-go for architectural linting
RUN go install github.com/arch-go/arch-go@latest

WORKDIR /app

# Copy go.mod first for better caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Default command runs Air (overridden by docker-compose per service)
CMD ["air"]
