#!/usr/bin/env bash
# Operator lookups against a running dev environment — the commands behind the
# "Verify and operate" sections of docs/features/*.md.
#
# Read-only: every subcommand is a GET (or a Firestore runQuery, also a read).
# Output is safe to paste into a published log: the project ID is replaced by
# PROJECT_ID (a project number by PROJECT_NUMBER), and secret-bearing values
# (otp_mac, refresh/prev token hashes, the log-only OTP code, bearer tokens) are
# cut to their first 8 characters, which is enough to tell two values apart and
# not enough to use one.
#
# Usage (from the repo root, on the host — it re-runs itself in the toolbox):
#   PROJECT_ID=my-project [FIRESTORE_DATABASE=messaging-dev] scripts/inspect.sh <cmd> ...
#
#   doc   <collection>/<id>             one document, typed values flattened
#   where <collection> <field> <value>  documents with field == value (max 20)
#   otp   <+E.164 phone>                otp_requests/{sha256(phone)}
#   phone <+E.164 phone>                phone_index/{sha256(phone)}, the uniqueness sentinel
#   pair  <user-id> <user-id>           direct_chats/{min}__{max}
#   prom  '<PromQL>'                    Managed Prometheus instant query
#   trace '<Cloud Trace v1 filter>'     the latest matching trace, as a span tree (SINCE=<RFC3339>, default 6h ago)
#   logs  '<Cloud Logging filter>' [n]  the newest n (default 5) matching lines, last 24h
set -euo pipefail

: "${PROJECT_ID:?set PROJECT_ID}"
DATABASE="${FIRESTORE_DATABASE:-messaging-dev}"

if [[ -z "${IN_TOOLBOX:-}" ]]; then
  docker compose run --rm -T -e IN_TOOLBOX=1 -e PROJECT_ID="${PROJECT_ID}" \
    -e FIRESTORE_DATABASE="${DATABASE}" toolbox bash scripts/inspect.sh "$@" 2>&1 \
    | grep -v '^ *Container .*-run-' \
    | sed -E -e "s/${PROJECT_ID}/PROJECT_ID/g" \
             -e 's/(project_number:|projects\/)[0-9]{6,}/\1PROJECT_NUMBER/g'
  exit "${PIPESTATUS[0]}"
fi

TOKEN="$(gcloud auth print-access-token)"
FS="https://firestore.googleapis.com/v1/projects/${PROJECT_ID}/databases/${DATABASE}/documents"
SECRET_KEYS='^(otp_mac|refresh_token_hash|prev_token_hash|otp|token|access_token|refresh_token)$'

# get retries a 429: the Cloud Trace read quota (300/min/project) is shared with
# the Console's trace explorer, which polls it while a tab is open.
get() {
  local out attempt
  for attempt in 1 2 3 4; do
    out="$(curl -sS -H "Authorization: Bearer ${TOKEN}" "$@")"
    [[ "$(jq -r '(.error.code // 0) | tostring' <<<"${out}" 2>/dev/null)" == "429" && ${attempt} -lt 4 ]] || break
    echo "  (429 rate-limited; retrying in 20s, attempt ${attempt}/3)" >&2
    sleep 20
  done
  printf '%s\n' "${out}"
}

# flat turns Firestore's typed values ({"stringValue": "x"}) into plain JSON and
# masks secret-bearing fields.
JQ_FLAT='
  def flat: if type == "object" then
      if has("stringValue") then .stringValue
      elif has("integerValue") then (.integerValue | tonumber)
      elif has("booleanValue") then .booleanValue
      elif has("timestampValue") then .timestampValue
      elif has("doubleValue") then .doubleValue
      elif has("nullValue") then null
      elif has("arrayValue") then [(.arrayValue.values // [])[] | flat]
      elif has("mapValue") then ((.mapValue.fields // {}) | map_values(flat))
      else . end
    else . end;
  def mask: with_entries(if (.key | test($k)) and (.value | type) == "string" and (.value | length) > 0
                         then .value = (.value[0:8] + "…") else . end);
  def doc: {id: (.name | sub(".*/documents/"; "")), updateTime, fields: ((.fields // {}) | map_values(flat) | mask)};'

show_doc() {
  jq --arg k "${SECRET_KEYS}" "${JQ_FLAT}"' if .error then .error else doc end'
}

case "${1:-}" in
  doc)
    get "${FS}/${2:?collection/id}" | show_doc
    ;;

  where)
    body="$(jq -n --arg c "${2:?collection}" --arg f "${3:?field}" --arg v "${4:?value}" \
      '{structuredQuery: {from: [{collectionId: $c}], limit: 20,
        where: {fieldFilter: {field: {fieldPath: $f}, op: "EQUAL", value: {stringValue: $v}}}}}')"
    get -X POST -H "Content-Type: application/json" "${FS}:runQuery" -d "${body}" \
      | jq --arg k "${SECRET_KEYS}" "${JQ_FLAT}"' .[] | select(.document) | .document | doc'
    ;;

  otp)
    hash="$(printf '%s' "${2:?phone}" | sha256sum | cut -d' ' -f1)"
    get "${FS}/otp_requests/${hash}" | show_doc
    ;;

  phone)
    hash="$(printf '%s' "${2:?phone}" | sha256sum | cut -d' ' -f1)"
    get "${FS}/phone_index/${hash}" | show_doc
    ;;

  pair)
    lo="${2:?user-id}" hi="${3:?user-id}"
    [[ "${hi}" < "${lo}" ]] && { t="${lo}"; lo="${hi}"; hi="${t}"; }
    get "${FS}/direct_chats/${lo}__${hi}" | show_doc
    ;;

  prom)
    get -G "https://monitoring.googleapis.com/v1/projects/${PROJECT_ID}/location/global/prometheus/api/v1/query" \
      --data-urlencode "query=${2:?PromQL}" \
      | jq -c 'if .status != "success" then . else .data.result[] | {metric, value: .value[1]} end'
    ;;

  trace)
    since="${SINCE:-$(date -u -d '-6 hours' +%Y-%m-%dT%H:%M:%SZ)}"
    raw="$(get -G "https://cloudtrace.googleapis.com/v1/projects/${PROJECT_ID}/traces" \
      --data-urlencode "filter=${2:?filter}" --data-urlencode "view=COMPLETE" \
      --data-urlencode "startTime=${since}" \
      --data-urlencode "orderBy=start desc" --data-urlencode "pageSize=1")"
    # An empty or error response is printed raw: hiding it once cost a round trip.
    if [[ "$(jq '(.traces // []) | length' <<<"${raw}")" == "0" ]]; then
      echo "no trace matched since ${since} — raw response:"
      jq -c . <<<"${raw}"
      exit 1
    fi
    jq -r '
        def ms($a; $b): ((($b | sub("\\.[0-9]+Z$"; "Z") | fromdate) - ($a | sub("\\.[0-9]+Z$"; "Z") | fromdate)) * 1000
                         + (((([$b | capture("\\.(?<f>[0-9]+)Z$").f][0] // "") + "000000")[0:6] | tonumber)
                          - ((([$a | capture("\\.(?<f>[0-9]+)Z$").f][0] // "") + "000000")[0:6] | tonumber)) / 1000);
        .traces[0] as $t | ($t.spans // []) as $s | ($s | map(.spanId)) as $ids
        | def kids($id): [$s[] | select(.parentSpanId == $id)] | sort_by(.startTime);
          def walk($sp; $d): ("  " * $d) + "- " + $sp.name + "  " + (ms($sp.startTime; $sp.endTime) * 10 | round / 10 | tostring) + " ms",
                             (kids($sp.spanId)[] | walk(.; $d + 1));
          "trace_id: \($t.traceId)",
          ($s[] | select((.parentSpanId // "") as $p | ($ids | index($p)) == null) | walk(.; 0))' <<<"${raw}"
    ;;

  logs)
    gcloud logging read "${2:?filter}" --project="${PROJECT_ID}" --freshness=1d \
      --limit="${3:-5}" --format=json \
      | jq -c --arg k "${SECRET_KEYS}" \
        'def mask: with_entries(if (.key | test($k)) and (.value | type) == "string" and (.value | length) > 0
                                then .value = (.value[0:8] + "…") else . end);
         .[] | {timestamp, severity} + ((.jsonPayload // {textPayload}) | mask)'
    ;;

  *)
    sed -n '2,22p' "$0" >&2
    exit 2
    ;;
esac
