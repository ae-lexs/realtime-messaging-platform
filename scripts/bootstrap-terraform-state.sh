#!/usr/bin/env bash
# Bootstrap the GCS bucket that holds Terraform remote state. Idempotent.
#
# GCS provides native object-level locking, so no separate lock table is
# provisioned or needed.
#
# Runs gcloud inside the toolbox container (nothing on the host but Docker).
# Authenticate first with: make gcp-auth
#
# Usage:
#   PROJECT_ID=my-project [REGION=us-central1] scripts/bootstrap-terraform-state.sh
set -euo pipefail

: "${PROJECT_ID:?set PROJECT_ID to your GCP project id}"
REGION="${REGION:-us-central1}"
BUCKET="${TF_STATE_BUCKET:-${PROJECT_ID}-tf-state}"

tb() { docker compose run --rm -T toolbox "$@"; }

echo "==> Ensuring state bucket gs://${BUCKET} (${REGION}, project ${PROJECT_ID})"
if tb gcloud storage buckets describe "gs://${BUCKET}" --project "${PROJECT_ID}" >/dev/null 2>&1; then
  echo "    bucket already exists — skipping create"
else
  tb gcloud storage buckets create "gs://${BUCKET}" \
    --project "${PROJECT_ID}" \
    --location "${REGION}" \
    --uniform-bucket-level-access \
    --public-access-prevention
fi

echo "==> Enabling object versioning (state recovery)"
tb gcloud storage buckets update "gs://${BUCKET}" --versioning

echo "==> Done. Terraform init uses:"
echo "      -backend-config=\"bucket=${BUCKET}\" -backend-config=\"prefix=dev\""
