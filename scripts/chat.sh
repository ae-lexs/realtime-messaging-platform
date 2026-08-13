#!/usr/bin/env bash
# The M1.3 apparatus — chat lifecycle and membership.
#
# It starts as one subcommand, because the first thing M1.3 does is measure a
# premise rather than build on it.
#
#   store         The M1.3 gate — chat creation, membership mutation, the
#                 direct-pair uniqueness invariant under concurrency.
#
#   direct-pair   The direct-chat negative control. ADR-023 v1.3 justifies the
#                 `direct_chats` sentinel with "a query matching nothing locks
#                 nothing", which RTM-04 measured false on the registration
#                 path. Direct chats have no OTP document to confound the
#                 result, so this is the clean instance of the same question.
#                 Three arms: the naive in-transaction query, its no-query
#                 positive control, and the sentinel with the refusing
#                 mechanism recorded.
#
# Usage:
#   PROJECT_ID=my-project BILLING_ACCOUNT_ID=XXXXXX-XXXXXX-XXXXXX \
#     [REGION=us-central1] [FIRESTORE_DATABASE=messaging-dev] \
#     [REPS=5] [LOG_SUFFIX=-optimistic] \
#     scripts/chat.sh {apply|store|direct-pair|destroy}
#
# `apply` and `destroy` target Firestore only. The experiment needs no cluster,
# no Redis and no secrets — standing up the rest would cost provisioning time
# for nothing. The concurrency mode is read and set with `scripts/auth.sh mode`,
# which is shared apparatus rather than duplicated here.
set -euo pipefail

: "${PROJECT_ID:?set PROJECT_ID}"
REGION="${REGION:-us-central1}"
DATABASE="${FIRESTORE_DATABASE:-messaging-dev}"
BUCKET="${TF_STATE_BUCKET:-${PROJECT_ID}-tf-state}"
ENV_DIR="terraform/environments/dev"

tb() { docker compose run --rm -T toolbox "$@"; }

EVIDENCE_DIR="docs/artifacts/evidence"

# concurrency_mode reports the database's mode, which the log must carry.
#
# The measured result depends on it — server clients use the database-level
# setting, and Standard and Enterprise editions default differently — so a log
# that does not say which mode produced it is not evidence for either
# (RTM-04 C8, the correction that cost a claim).
concurrency_mode() {
  tb gcloud firestore databases describe --database="${DATABASE}" \
    --project="${PROJECT_ID}" --format="value(concurrencyMode)" 2>/dev/null | tr -d '\r' || echo "unknown"
}

# capture <name> <command...> — run an experiment and keep its output.
#
# The infrastructure does not survive the session: teardown is mandatory
# (ADR-021), so a run cannot be repeated later to satisfy a reader, and the log
# is the only durable record that it happened. The project ID is redacted
# because these logs are published (docs/artifacts).
capture() {
  local name="$1"
  shift

  mkdir -p "${EVIDENCE_DIR}"
  local out="${EVIDENCE_DIR}/${name}.log"
  local mode
  mode="$(concurrency_mode)"

  {
    echo "# ${name} — captured experiment output"
    echo "# captured:  $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "# commit:    $(git rev-parse HEAD)"
    echo "# database:  ${DATABASE} (${REGION})"
    echo "# mode:      ${mode}"
    echo "# reps:      ${REPS:-1}"
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
    -var="project_id=${PROJECT_ID}" \
    -var="billing_account_id=${BILLING_ACCOUNT_ID}" \
    -var="region=${REGION}"
}

case "${1:-}" in
  apply)
    echo "==> applying Firestore in ${PROJECT_ID} (${REGION})"
    terraform_cmd apply
    ;;

  store)
    # The M1.3 store gate: chat creation and membership mutation against a live
    # database. Unlike `direct-pair` next to it, every outcome here is specified
    # by ADR-006 §4 and ADR-016, so the tests assert rather than report — with
    # one exception, the concurrency gate, which asserts the invariant AND
    # records which mechanism refused each loser (RTM-04 C7).
    #
    # -count=1 disables Go's test cache: a cached "ok" would report success
    # without touching Firestore, which is worthless for a live gate.
    echo "==> M1.3 store gate against ${PROJECT_ID}/${DATABASE} (${REGION})"
    capture "m1.3-store${LOG_SUFFIX:-}" \
      docker compose run --rm -T \
      -e FIRESTORE_PROJECT="${PROJECT_ID}" \
      -e FIRESTORE_DATABASE="${DATABASE}" \
      toolbox go test -race -count=1 -tags=integration -v ./internal/firestore/... \
      -run 'TestCreateDirect|TestCreateGroup|TestAddMember|TestDirectChatMembershipIsImmutable|TestTheOwnerCannot|TestLeave|TestRemoveMember|TestSetRole|TestSetMute|TestSetName|TestMutationsOnAMissingChat|TestConcurrentCreateDirect'
    ;;

  direct-pair)
    # REPS repeats every arm. A concurrency result from one run is an anecdote;
    # the user pair and every document ID are freshly generated per repetition,
    # so repetitions do not contaminate one another.
    #
    # LOG_SUFFIX keeps a second run — under the other concurrency mode, say —
    # from overwriting the first.
    #
    # -count=1 is deliberately not used: -count="${REPS}" both disables the
    # test cache and is the repetition mechanism. A cached "ok" would report
    # success without touching Firestore, which is worthless for a live run.
    echo "==> M1.3 direct-pair negative control against ${PROJECT_ID}/${DATABASE} (${REGION}), REPS=${REPS:-1}"
    capture "m1.3-direct-pair${LOG_SUFFIX:-}" \
      docker compose run --rm -T \
      -e FIRESTORE_PROJECT="${PROJECT_ID}" \
      -e FIRESTORE_DATABASE="${DATABASE}" \
      toolbox go test -race -count="${REPS:-1}" -tags=integration -v ./internal/firestore/... \
      -run 'TestConcurrentDirectChatsBehindAnEmptyQuery|TestConcurrentDirectChatsWithNoQueryAtAll|TestConcurrentDirectChatsBehindTheSentinel'
    ;;

  destroy)
    # Expect ~6 minutes: deleting the TTL fields dominates this teardown.
    echo "==> destroying Firestore (the TTL fields take ~6 min)"
    terraform_cmd destroy
    ;;

  *)
    echo "usage: PROJECT_ID=... scripts/chat.sh {apply|store|direct-pair|destroy}" >&2
    exit 2
    ;;
esac
