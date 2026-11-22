#!/usr/bin/env bash
# M1.2 OTP-reissue probe — an EXPERIMENT, not a gate. Runs INSIDE the toolbox
# (invoked by `scripts/auth.sh otp-reissue`), so the port-forward and the
# requests share a network namespace.
#
# Question: does reissuing an OTP reset the failed-attempt count, and does a
# lockout or rate limit still stop guessing across the reissue? NIST SP 800-63B
# (Rev. 4): "Generating a new authentication secret SHALL NOT reset the failed
# authentication count."
#
# Two counters are in play, and the probe observes both:
#   - the OTP document's attempt_count (read directly from Firestore), which a
#     reissue may reset, and which is what sets the 15-minute lockout at 5;
#   - the Redis per-phone verify limit (5 per 15 minutes, keyed by phone, not by
#     OTP), which cannot be read from outside the VPC and is inferred from the
#     HTTP responses.
#
# Per the evidence README's distinction, an experiment reports the quantity
# under test and asserts only that the harness is valid. Nothing here asserts
# what the counters should do.
set -euo pipefail

NAMESPACE="${NAMESPACE:-messaging}"
PROJECT_ID="${PROJECT_ID:?set PROJECT_ID}"
DATABASE="${FIRESTORE_DATABASE:-messaging-dev}"
PHONE="${PHONE:?set PHONE}"
DEVICE="${DEVICE:-probe-device}"
PORT="${PORT:-18084}"
BASE="http://127.0.0.1:${PORT}/v1/auth"
WRONG_BEFORE_REISSUE="${WRONG_BEFORE_REISSUE:-4}" # one below MaxOTPVerifyAttempts (5)
WRONG_AFTER_REISSUE="${WRONG_AFTER_REISSUE:-6}"

fail() { echo "❌ harness: $*" >&2; exit 1; }
ok() { echo "   ✅ $*"; }

echo "==> port-forwarding svc/chatmgmt"
kubectl -n "${NAMESPACE}" port-forward svc/chatmgmt "${PORT}:80" >/tmp/port-forward.log 2>&1 &
PF_PID=$!
trap 'kill "${PF_PID}" 2>/dev/null || true' EXIT
for _ in $(seq 1 30); do
  curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1 && break
  sleep 1
done
curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null || fail "port-forward never became reachable"
ok "port-forward up"

post() {
  curl -sS -o /tmp/response.json -w '%{http_code}' -X POST "${BASE}$1" \
    -H 'Content-Type: application/json' -d "$2"
}
err_code() { jq -r '.error.code // .code // .message // "-"' /tmp/response.json 2>/dev/null || echo "-"; }

read_otp() {
  kubectl -n "${NAMESPACE}" logs deployment/chatmgmt --since=10m \
    | grep '"msg":"otp delivery (log-only)"' | tail -1 | jq -r '.otp'
}

PHONE_HASH="$(printf '%s' "${PHONE}" | sha256sum | cut -d' ' -f1)"
TOKEN="$(gcloud auth print-access-token)"
OTP_DOC_URL="https://firestore.googleapis.com/v1/projects/${PROJECT_ID}/databases/${DATABASE}/documents/otp_requests/${PHONE_HASH}"

# otp_doc prints the OTP document's attempt_count, status and expiry, read
# straight from Firestore, so the reset (or not) is observed, not inferred.
otp_doc() {
  curl -sS -H "Authorization: Bearer ${TOKEN}" "${OTP_DOC_URL}" \
    | jq -r '"attempt_count=\(.fields.attempt_count.integerValue // "absent") status=\(.fields.status.stringValue // "absent") expires_at=\(.fields.expires_at.timestampValue // "absent")"'
}

# wrong_code returns a 6-digit code guaranteed to differ from the real one.
wrong_code() { if [[ "$1" == "000000" ]]; then echo "111111"; else echo "000000"; fi; }

guess() {
  local label="$1" code
  code="$(post /otp/verify "{\"phone_number\":\"${PHONE}\",\"otp\":\"${WRONG}\",\"device_id\":\"${DEVICE}\"}")"
  printf '   %-22s HTTP %s  error=%-28s  %s\n' "${label}" "${code}" "$(err_code)" "$(otp_doc)"
}

echo "==> 1. request OTP #1"
code="$(post /otp/request "{\"phone_number\":\"${PHONE}\"}")"
[[ "${code}" == "200" ]] || fail "request-otp returned ${code}: $(cat /tmp/response.json)"
sleep 2
OTP1="$(read_otp)"
[[ -n "${OTP1}" && "${OTP1}" != "null" ]] || fail "no OTP in the pod log"
WRONG="$(wrong_code "${OTP1}")"
ok "OTP #1 issued; $(otp_doc)"

echo "==> 2. ${WRONG_BEFORE_REISSUE} wrong guesses against OTP #1"
for i in $(seq 1 "${WRONG_BEFORE_REISSUE}"); do guess "wrong #${i} (otp 1)"; done

echo "==> 3. reissue while OTP #1 is still live"
code="$(post /otp/request "{\"phone_number\":\"${PHONE}\"}")"
printf '   reissue-while-live     HTTP %s  error=%-28s  %s\n' "${code}" "$(err_code)" "$(otp_doc)"

EXP_RAW="$(curl -sS -H "Authorization: Bearer ${TOKEN}" "${OTP_DOC_URL}" | jq -r '.fields.expires_at.timestampValue')"
EXP_S="$(date -d "${EXP_RAW}" +%s)"
WAIT=$(( EXP_S - $(date +%s) + 5 ))
if (( WAIT > 0 )); then
  echo "==> 4. waiting ${WAIT}s for OTP #1 to expire (a live OTP cannot be replaced)"
  sleep "${WAIT}"
fi

echo "==> 5. request OTP #2 (the reissue)"
code="$(post /otp/request "{\"phone_number\":\"${PHONE}\"}")"
printf '   reissue-after-expiry   HTTP %s  error=%-28s  %s\n' "${code}" "$(err_code)" "$(otp_doc)"
if [[ "${code}" == "200" ]]; then
  sleep 2
  OTP2="$(read_otp)"
  WRONG="$(wrong_code "${OTP2}")"
fi

echo "==> 6. ${WRONG_AFTER_REISSUE} more wrong guesses"
for i in $(seq 1 "${WRONG_AFTER_REISSUE}"); do guess "wrong #${i} (after reissue)"; done

echo
echo "==> result (reported, not asserted)"
echo "   Read the attempt_count column across step 5 for whether the reissue reset it,"
echo "   and the HTTP/error columns in step 6 for which mechanism, if any, stopped guessing."
