# Production Dockerfile for Gateway service
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
    -o /gateway \
    ./cmd/gateway

# Production stage - scratch base (PR0-INV-2)
FROM scratch

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary
COPY --from=builder /gateway /gateway

# Use non-root user (PR0-INV-2)
# UID 65534 is "nobody" - a standard unprivileged user
USER 65534:65534

# Health check endpoint
EXPOSE 8080

# gRPC endpoint
EXPOSE 9090

ENTRYPOINT ["/gateway"]
