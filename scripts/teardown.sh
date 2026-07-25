#!/usr/bin/env bash
# Tear down everything M0.2 stood up — the mandatory end-of-session step
# (ADR-021 Deployment Req 4). Deletes the k8s namespace first (so the external
# load balancer is released before the VPC is destroyed), then terraform destroy.
#
# Usage:
#   PROJECT_ID=my-project BILLING_ACCOUNT_ID=XXXXXX-XXXXXX-XXXXXX \
#     [REGION=us-central1] scripts/teardown.sh
set -euo pipefail

: "${PROJECT_ID:?set PROJECT_ID}"
: "${BILLING_ACCOUNT_ID:?set BILLING_ACCOUNT_ID}"
REGION="${REGION:-us-central1}"
BUCKET="${TF_STATE_BUCKET:-${PROJECT_ID}-tf-state}"
ENV_DIR="terraform/environments/dev"

tb() { docker compose run --rm -T toolbox "$@"; }

# 1. Delete workloads + Ingress so the LB and its forwarding rules are released.
#    Waiting on the namespace lets the Ingress finalizer clean up the LB first;
#    an orphaned LB would otherwise block VPC/subnet destruction.
echo "==> deleting namespace messaging (releases the external load balancer)"
tb kubectl delete namespace messaging --ignore-not-found=true --wait=true || true

# 2. Destroy the infrastructure.
echo "==> terraform destroy"
tb terraform -chdir="${ENV_DIR}" init -input=false -reconfigure \
  -backend-config="bucket=${BUCKET}" -backend-config="prefix=dev"
tb terraform -chdir="${ENV_DIR}" destroy -input=false -auto-approve \
  -var="project_id=${PROJECT_ID}" \
  -var="billing_account_id=${BILLING_ACCOUNT_ID}" \
  -var="region=${REGION}"

echo "==> Torn down. Verify zero billable resources in the GCP console."
