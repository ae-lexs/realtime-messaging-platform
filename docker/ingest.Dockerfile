# Production Dockerfile for Ingest service
# Multi-stage build: builder -> scratch with non-root user (PR0-INV-2)

# Builder stage
FROM golang:1.25-alpine AS builder

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
    -o /ingest \
    ./cmd/ingest

# Production stage - scratch base (PR0-INV-2)
FROM scratch

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary
COPY --from=builder /ingest /ingest

# Use non-root user (PR0-INV-2)
USER 65534:65534

# Health check endpoint
EXPOSE 8081

# gRPC endpoint
EXPOSE 9091

ENTRYPOINT ["/ingest"]
