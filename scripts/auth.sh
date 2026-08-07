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

  negative-control)
    # The RTM-04 negative control (docs/artifacts/RTM-04.md, C6). Kept separate
    # from `store` for two reasons: it captures to its own log, so the run the
    # ledger already cites for C2 and C5 is not overwritten, and it is an
    # experiment rather than a gate — the arms measuring an unknown assert only
    # that the harness was valid and report the quantity under test.
    #
    # REPS repeats every arm. A concurrency result from one run is an anecdote;
    # the phone number and document IDs are freshly generated per repetition,
    # so repetitions do not contaminate one another.
    # LOG_SUFFIX keeps a second run (a different concurrency mode, say) from
    # overwriting the first. The mode itself is recorded in the log, because the
    # measured result depends on it and a log that does not say which mode
    # produced it is not evidence for either.
    echo "==> RTM-04 negative control against ${PROJECT_ID}/${DATABASE} (${REGION}), REPS=${REPS:-1}"
    capture "rtm-04-negative-control${LOG_SUFFIX:-}" \
      docker compose run --rm -T \
      -e FIRESTORE_PROJECT="${PROJECT_ID}" \
      -e FIRESTORE_DATABASE="${DATABASE}" \
      toolbox go test -race -count="${REPS:-1}" -tags=integration -v ./internal/firestore/... \
      -run 'TestConcurrentInsertsBehindAnEmptyQuery|TestConcurrentInsertsWithNoQueryAtAll|TestConcurrentRegistrationWithoutTheSentinel|TestConcurrentRegistrationLoserErrors'
    ;;

  mode)
    # Read or set the database's concurrency mode.
    #
    # This is the switch RTM-04 C6 turns on. Server client libraries use the
    # database-level setting: Standard edition defaults to PESSIMISTIC, and
    # Enterprise edition defaults to OPTIMISTIC, so the same code measured on
    # one is not evidence about the other. Flipping it here rather than in
    # terraform/modules/firestore is deliberate — it is experimental apparatus,
    # not a deployment choice, and the module should keep shipping whatever the
    # platform default is, since that is the condition the essay is about.
    #
    #   scripts/auth.sh mode                 # report the current mode
    #   scripts/auth.sh mode optimistic      # set it
    if [ -z "${2:-}" ]; then
      echo "==> concurrency mode of ${PROJECT_ID}/${DATABASE}:"
      tb gcloud firestore databases describe --database="${DATABASE}" \
        --project="${PROJECT_ID}" --format="value(concurrencyMode)"
    else
      echo "==> setting ${PROJECT_ID}/${DATABASE} concurrency mode to $2"
      tb gcloud firestore databases update --database="${DATABASE}" \
        --project="${PROJECT_ID}" --concurrency-mode="$2" --quiet
      tb gcloud firestore databases describe --database="${DATABASE}" \
        --project="${PROJECT_ID}" --format="value(concurrencyMode)"
    fi
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
    echo "usage: PROJECT_ID=... scripts/auth.sh {apply|store|negative-control|mode [MODE]|flow|destroy}" >&2
    exit 2
    ;;
esac
