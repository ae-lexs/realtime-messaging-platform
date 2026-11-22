#!/usr/bin/env bash
# Telemetry proof — re-checkable evidence that spans reach Cloud Trace and
# metrics reach Managed Service for Prometheus (seed doubt 3 of the Module 1
# Feature Dossier: until now this was asserted only in PR #28's prose).
# Runs INSIDE the toolbox (invoked by `scripts/chat.sh telemetry` /
# `listchats-cost`), using the operator's gcloud credentials.
#
# It prints RAW API responses, not descriptions: one complete auth.verify_otp
# trace from the Cloud Trace v1 API (plus its span tree rendered from that same
# JSON), and the instant-query responses from the Managed Prometheus HTTP API
# for auth_otp_requests_total and chat_created_total.
#
#   MODE=telemetry       the proof above (default)
#   MODE=listchats-cost  the chat.list span tree and its count of Firestore
#                        child spans: the read-cost half of the ListChats gate
set -euo pipefail

PROJECT_ID="${PROJECT_ID:?set PROJECT_ID}"
MODE="${MODE:-telemetry}"
SINCE="${SINCE:-$(date -u -d '-2 hours' +%Y-%m-%dT%H:%M:%SZ)}"
TOKEN="$(gcloud auth print-access-token)"
TRACE_API="https://cloudtrace.googleapis.com/v1/projects/${PROJECT_ID}/traces"
PROM_API="https://monitoring.googleapis.com/v1/projects/${PROJECT_ID}/location/global/prometheus/api/v1/query"

# latest_trace <root span name> — the most recent complete trace whose root span
# has that name, as raw JSON.
latest_trace() {
  curl -sS -G -H "Authorization: Bearer ${TOKEN}" "${TRACE_API}" \
    --data-urlencode "filter=root:$1" \
    --data-urlencode "view=COMPLETE" \
    --data-urlencode "startTime=${SINCE}" \
    --data-urlencode "orderBy=start desc" \
    --data-urlencode "pageSize=1"
}

# tree renders the span hierarchy of the first trace in a v1 traces response.
tree() {
  jq -r '
    .traces[0] as $t
    | ($t.spans // []) as $s
    | ($s | map(.spanId)) as $ids
    | def kids($id): [$s[] | select(.parentSpanId == $id)] | sort_by(.startTime);
      def walk($sp; $d): ("  " * $d) + "- " + $sp.name + "  [" + $sp.startTime + " → " + $sp.endTime + "]",
                         (kids($sp.spanId)[] | walk(.; $d + 1));
      "trace_id: \($t.traceId)",
      ($s[] | select((.parentSpanId // "") as $p | ($ids | index($p)) == null) | walk(.; 0))'
}

prom() {
  curl -sS -G -H "Authorization: Bearer ${TOKEN}" "${PROM_API}" --data-urlencode "query=$1"
}

case "${MODE}" in
  telemetry)
    echo "==> Cloud Trace v1: latest trace rooted at auth.verify_otp (since ${SINCE})"
    RAW="$(latest_trace auth.verify_otp)"
    [[ "$(jq '.traces | length' <<<"${RAW}")" -ge 1 ]] || { echo "❌ no auth.verify_otp trace found"; echo "${RAW}"; exit 1; }
    echo "--- span tree (rendered from the raw response below)"
    tree <<<"${RAW}"
    echo "--- raw response"
    jq . <<<"${RAW}"
    echo
    for q in auth_otp_requests_total chat_created_total; do
      echo "==> Managed Prometheus instant query: ${q}"
      prom "${q}" | jq .
      echo
    done
    echo "==> Managed Prometheus instant query: sum by (status) (auth_otp_requests_total)"
    prom 'sum by (status) (auth_otp_requests_total)' | jq .
    ;;

  listchats-cost)
    echo
    echo "# ---- appended: read cost of one ListChats page, from Cloud Trace ($(date -u +%Y-%m-%dT%H:%M:%SZ)) ----"
    RAW="$(latest_trace chat.list)"
    [[ "$(jq '.traces | length' <<<"${RAW}")" -ge 1 ]] || { echo "❌ no chat.list trace found"; echo "${RAW}"; exit 1; }
    tree <<<"${RAW}"
    N="$(jq '[.traces[0].spans[] | select(.name | startswith("firestore."))] | length' <<<"${RAW}")"
    NAMES="$(jq -r '[.traces[0].spans[] | select(.name | startswith("firestore.")) | .name] | join(", ")' <<<"${RAW}")"
    echo "firestore spans under chat.list: ${N} (${NAMES})"
    if (( N <= 3 )); then
      echo "   ✅ at most three Firestore round trips for this page"
    else
      echo "❌ ${N} Firestore round trips — more than three"
      exit 1
    fi
    echo "--- raw response"
    jq . <<<"${RAW}"
    ;;

  *) echo "usage: MODE={telemetry|listchats-cost}" >&2; exit 2 ;;
esac
