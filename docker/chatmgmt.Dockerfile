# Production Dockerfile for Chat Management service
# Multi-stage build: builder -> scratch with non-root user (PR0-INV-2)

# Builder stage
# Pin the builder to the native build platform so the Go toolchain runs without
# QEMU emulation and cross-compiles to the target arch (GOARCH below).
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Copy go.mod first for better caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with security flags
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o /chatmgmt \
    ./cmd/chatmgmt

# Production stage - scratch base (PR0-INV-2)
FROM scratch

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary
COPY --from=builder /chatmgmt /chatmgmt

# Use non-root user (PR0-INV-2)
USER 65534:65534

# Health check endpoint
EXPOSE 8083

# gRPC endpoint
EXPOSE 9093

ENTRYPOINT ["/chatmgmt"]
