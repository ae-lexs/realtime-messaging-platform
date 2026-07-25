# Toolbox image — the GCP-targeting development workspace.
#
# This image backs the `toolbox` compose service, which runs every `make`
# target (lint, test, proto, build) and the GCP deploy-and-destroy loop.
# Per ADR-021 Axis F, nothing is installed on the host: gcloud, terraform,
# kubectl, buf, and the Go toolchain all live here.
#
# Debian base (not alpine): the Google Cloud CLI is not supported on musl,
# and Debian gives us a reliable apt path for gcloud plus glibc for the
# race detector (CGO).

FROM golang:1.26-bookworm AS base

ARG TERRAFORM_VERSION=1.15.8

# Base OS tooling. gnupg/apt-transport-https are needed to add the Google and
# HashiCorp apt repositories; python3 is a gcloud runtime dependency.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        make \
        gnupg \
        apt-transport-https \
        unzip \
        python3 \
    && rm -rf /var/lib/apt/lists/*

# Google Cloud CLI (gcloud) — via the official apt repository.
RUN curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg \
        | gpg --dearmor -o /usr/share/keyrings/cloud.google.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" \
        > /etc/apt/sources.list.d/google-cloud-sdk.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends google-cloud-cli \
    && rm -rf /var/lib/apt/lists/*

# kubectl — pinned to the current stable release for the host architecture.
RUN ARCH="$(dpkg --print-architecture)" \
    && KUBECTL_VERSION="$(curl -fsSL https://dl.k8s.io/release/stable.txt)" \
    && curl -fsSL -o /usr/local/bin/kubectl \
        "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl" \
    && chmod +x /usr/local/bin/kubectl

# Terraform — pinned via TERRAFORM_VERSION (matches .github/workflows/ci.yaml).
RUN ARCH="$(dpkg --print-architecture)" \
    && curl -fsSL -o /tmp/terraform.zip \
        "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_${ARCH}.zip" \
    && unzip -q /tmp/terraform.zip -d /usr/local/bin \
    && rm /tmp/terraform.zip \
    && chmod +x /usr/local/bin/terraform

# Go-based tooling: buf + protoc plugins (proto gen), golangci-lint v2 (built
# from source to match Go 1.26), arch-go (architectural linting).
RUN go install github.com/bufbuild/buf/cmd/buf@latest && \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest && \
    go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest && \
    go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@latest && \
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest && \
    go install github.com/arch-go/arch-go@latest

WORKDIR /app

# Prime the module cache for faster first run (source is bind-mounted at runtime).
COPY go.mod go.sum* ./
RUN go mod download

CMD ["sleep", "infinity"]
