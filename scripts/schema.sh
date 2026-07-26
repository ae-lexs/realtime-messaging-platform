#!/usr/bin/env bash
# Manage the Managed Kafka schema registry and the events/v1 schemas in it.
#
# The registry has no Terraform resource in either Google provider, so it is
# created here and deleted in teardown rather than by `terraform apply`. It also
# cannot exist before its cluster: with no cluster in the region, creation fails
# with FAILED_PRECONDITION — which is why the cluster is provisioned in M0.3
# (terraform/modules/kafka) rather than with the first producer.
#
# Usage:
#   PROJECT_ID=my-project [REGION=us-central1] [SCHEMA_REGISTRY_ID=messaging-dev] \
#     scripts/schema.sh {create|register|verify|test|delete}
set -euo pipefail

: "${PROJECT_ID:?set PROJECT_ID}"
REGION="${REGION:-us-central1}"
REGISTRY_ID="${SCHEMA_REGISTRY_ID:-messaging-dev}"
REGISTRY_URL="https://managedkafka.googleapis.com/v1/projects/${PROJECT_ID}/locations/${REGION}/schemaRegistries/${REGISTRY_ID}"

tb() { docker compose run --rm -T toolbox "$@"; }

# schemactl needs a bearer token; the toolbox holds the gcloud credentials.
schemactl() {
  tb sh -c "SCHEMA_REGISTRY_URL='${REGISTRY_URL}' SR_TOKEN=\$(gcloud auth print-access-token) go run ./cmd/schemactl $1"
}

case "${1:-}" in
  create)
    if tb gcloud beta managed-kafka schema-registries describe "${REGISTRY_ID}" \
        --location="${REGION}" --project="${PROJECT_ID}" >/dev/null 2>&1; then
      echo "==> schema registry ${REGISTRY_ID} already exists"
    else
      echo "==> creating schema registry ${REGISTRY_ID} in ${REGION}"
      tb gcloud beta managed-kafka schema-registries create "${REGISTRY_ID}" \
        --location="${REGION}" --project="${PROJECT_ID}"
    fi
    ;;

  register)
    echo "==> registering proto/events/v1 in ${REGISTRY_ID}"
    schemactl register
    ;;

  verify)
    schemactl verify
    ;;

  test)
    # The M0.3 gate: encode -> register -> decode against the live registry.
    echo "==> integration round-trip against ${REGISTRY_ID}"
    tb sh -c "SCHEMA_REGISTRY_URL='${REGISTRY_URL}' SR_TOKEN=\$(gcloud auth print-access-token) \
      go test -race -tags=integration -v ./internal/events/..."
    ;;

  delete)
    echo "==> deleting schema registry ${REGISTRY_ID}"
    tb gcloud beta managed-kafka schema-registries delete "${REGISTRY_ID}" \
      --location="${REGION}" --project="${PROJECT_ID}" --quiet || true
    ;;

  *)
    echo "usage: PROJECT_ID=... scripts/schema.sh {create|register|verify|test|delete}" >&2
    exit 2
    ;;
esac
