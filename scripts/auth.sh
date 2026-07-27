#!/usr/bin/env bash
# The M1.2 gate — auth re-homed to Firestore (OTP, tokens, sessions).
#
# It has two halves, because M1.2 is the first module whose dependencies are not
# all reachable from the workspace.
#
#   store   Firestore semantics, driven from the toolbox against the public
#           Firestore API — the M1.1 pattern. Conditional writes, transaction
#           atomicity, concurrent registration, rotation single-use.
#
#   flow    The full OTP -> token -> refresh -> logout flow against the
#           *deployed* service. Memorystore has no public endpoint, so the
#           fail-closed rate limiting and revocation of ADR-013 can only be
#           exercised by the real pod. kubectl port-forward gives the toolbox a
#           way in; the OTP itself is read from the pod's structured log, which
#           is where LogSMSProvider puts it.
#
# Usage:
#   PROJECT_ID=my-project BILLING_ACCOUNT_ID=XXXXXX-XXXXXX-XXXXXX \
#     [REGION=us-central1] [FIRESTORE_DATABASE=messaging-dev] \
#     scripts/auth.sh {apply|store|flow|destroy}
#
# `apply` and `destroy` target Firestore and its secrets only — the store half
# needs no cluster and no Redis, and standing up GKE for it would cost ~25
# minutes of Kafka provisioning for nothing. The flow half assumes a full
# `make deploy`.
set -euo pipefail

: "${PROJECT_ID:?set PROJECT_ID}"
REGION="${REGION:-us-central1}"
DATABASE="${FIRESTORE_DATABASE:-messaging-dev}"
BUCKET="${TF_STATE_BUCKET:-${PROJECT_ID}-tf-state}"
ENV_DIR="terraform/environments/dev"
NAMESPACE="messaging"
PHONE="${AUTH_TEST_PHONE:-+525599990001}"
DEVICE="${AUTH_TEST_DEVICE:-gate-device-1}"

tb() { docker compose run --rm -T toolbox "$@"; }

terraform_cmd() {
  : "${BILLING_ACCOUNT_ID:?set BILLING_ACCOUNT_ID}"

  tb terraform -chdir="${ENV_DIR}" init -input=false -reconfigure \
    -backend-config="bucket=${BUCKET}" -backend-config="prefix=dev" >/dev/null

  tb terraform -chdir="${ENV_DIR}" "$1" -input=false -auto-approve \
    -target=module.project_services \
    -target=module.firestore \
    -target=module.service_accounts \
    -target=module.secrets \
    -var="project_id=${PROJECT_ID}" \
    -var="billing_account_id=${BILLING_ACCOUNT_ID}" \
    -var="region=${REGION}"
}

case "${1:-}" in
  apply)
    echo "==> applying Firestore + auth secrets in ${PROJECT_ID} (${REGION})"
    terraform_cmd apply
    PROJECT_ID="${PROJECT_ID}" ./scripts/auth-keys.sh
    ;;

  store)
    echo "==> Firestore auth semantics against ${PROJECT_ID}/${DATABASE} (${REGION})"
    # -count=1 disables Go's test cache: a cached "ok" would report success
    # without touching Firestore, which is worthless for a live gate.
    docker compose run --rm -T \
      -e FIRESTORE_PROJECT="${PROJECT_ID}" \
      -e FIRESTORE_DATABASE="${DATABASE}" \
      toolbox go test -race -count=1 -tags=integration -v ./internal/firestore/...
    ;;

  flow)
    echo "==> full auth flow against the deployed chatmgmt in ${NAMESPACE}"
    # Everything runs inside one toolbox container so the port-forward and the
    # curls share a network namespace.
    docker compose run --rm -T \
      -e NAMESPACE="${NAMESPACE}" \
      -e PHONE="${PHONE}" \
      -e DEVICE="${DEVICE}" \
      toolbox bash scripts/auth-flow.sh
    ;;

  destroy)
    # Expect ~6 minutes: deleting the TTL fields dominates this teardown.
    echo "==> destroying Firestore + auth secrets (the TTL fields take ~6 min)"
    terraform_cmd destroy
    ;;

  *)
    echo "usage: PROJECT_ID=... scripts/auth.sh {apply|store|flow|destroy}" >&2
    exit 2
    ;;
esac
