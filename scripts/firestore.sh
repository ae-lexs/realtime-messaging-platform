#!/usr/bin/env bash
# Stand up Firestore on its own, run the CRUD round-trip (the M1.1 gate), and
# tear it back down.
#
# Firestore is Terraform-managed and costs nothing when idle, so it is worth
# applying on its own rather than through the full `make deploy`: the Kafka
# cluster alone takes ~25 minutes to create and buys nothing here. That is what
# the -target flags below are for.
#
# There is no emulator (ADR-021 Axis F) — the Go client reads the gcloud
# Application Default Credentials mounted into the toolbox.
#
# Usage:
#   PROJECT_ID=my-project BILLING_ACCOUNT_ID=XXXXXX-XXXXXX-XXXXXX \
#     [REGION=us-central1] [FIRESTORE_DATABASE=messaging-dev] \
#     scripts/firestore.sh {apply|test|destroy}
set -euo pipefail

: "${PROJECT_ID:?set PROJECT_ID}"
REGION="${REGION:-us-central1}"
DATABASE="${FIRESTORE_DATABASE:-messaging-dev}"
BUCKET="${TF_STATE_BUCKET:-${PROJECT_ID}-tf-state}"
ENV_DIR="terraform/environments/dev"

tb() { docker compose run --rm -T toolbox "$@"; }

# Terraform validates every variable even when targeting, so the billing
# account is required for an apply or destroy but not for the test.
terraform_cmd() {
  : "${BILLING_ACCOUNT_ID:?set BILLING_ACCOUNT_ID}"

  tb terraform -chdir="${ENV_DIR}" init -input=false -reconfigure \
    -backend-config="bucket=${BUCKET}" -backend-config="prefix=dev" >/dev/null

  tb terraform -chdir="${ENV_DIR}" "$1" -input=false -auto-approve \
    -target=module.project_services -target=module.firestore \
    -var="project_id=${PROJECT_ID}" \
    -var="billing_account_id=${BILLING_ACCOUNT_ID}" \
    -var="region=${REGION}"
}

case "${1:-}" in
  apply)
    echo "==> applying module.firestore in ${PROJECT_ID} (${REGION})"
    terraform_cmd apply
    ;;

  test)
    echo "==> Firestore round-trip against ${PROJECT_ID}/${DATABASE} (${REGION})"
    # -count=1 disables Go's test cache: a cached "ok" would report success
    # without touching Firestore, which is worthless for a live gate.
    docker compose run --rm -T \
      -e FIRESTORE_PROJECT="${PROJECT_ID}" \
      -e FIRESTORE_DATABASE="${DATABASE}" \
      toolbox go test -race -count=1 -tags=integration -v ./internal/firestore/...
    ;;

  destroy)
    # Expect ~6 minutes: deleting the TTL field is a slow field operation, and
    # it dominates this teardown.
    echo "==> destroying module.firestore (the TTL field takes ~6 min)"
    terraform_cmd destroy
    ;;

  *)
    echo "usage: PROJECT_ID=... BILLING_ACCOUNT_ID=... scripts/firestore.sh {apply|test|destroy}" >&2
    exit 2
    ;;
esac
