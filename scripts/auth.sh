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

EVIDENCE_DIR="docs/artifacts/evidence"

# capture <name> <command...> — run a gate and keep its output.
#
# The infrastructure a gate runs against does not survive the session: teardown
# is mandatory (ADR-021), so a run cannot be repeated later to satisfy a reader,
# and the log is the only durable evidence that it happened. Output is written
# as well as shown, and committed deliberately — it appears in `git status`
# rather than being staged by a script.
#
# The project ID is redacted because these logs are published (docs/artifacts).
# `set -o pipefail` is already in force, so a failing gate still fails the
# script despite the pipe.
capture() {
  local name="$1"
  shift

  mkdir -p "${EVIDENCE_DIR}"
  local out="${EVIDENCE_DIR}/${name}.log"

  {
    echo "# ${name} — captured gate output"
    echo "# captured:  $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "# commit:    $(git rev-parse HEAD)"
    echo "# database:  ${DATABASE} (${REGION})"
    echo "# project:   redacted"
    echo "#"
    echo "# Re-running requires re-provisioning — the infrastructure this ran"
    echo "# against was destroyed at end of session. See docs/artifacts/README.md."
    echo
  } >"${out}"

  "$@" 2>&1 | sed -e "s/${PROJECT_ID}/PROJECT_ID/g" | tee -a "${out}"
}

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
    capture m1.2-store \
      docker compose run --rm -T \
      -e FIRESTORE_PROJECT="${PROJECT_ID}" \
      -e FIRESTORE_DATABASE="${DATABASE}" \
      toolbox go test -race -count=1 -tags=integration -v ./internal/firestore/...
    ;;

  flow)
    echo "==> full auth flow against the deployed chatmgmt in ${NAMESPACE}"
    # Everything runs inside one toolbox container so the port-forward and the
    # curls share a network namespace.
    capture m1.2-flow \
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
