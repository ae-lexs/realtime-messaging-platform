#!/usr/bin/env bash
# The M1.2 flow gate. Runs INSIDE the toolbox container (invoked by
# scripts/auth.sh flow), because the port-forward and the requests that use it
# must share a network namespace.
#
# It drives the deployed ChatMgmt through the whole of ADR-015: request an OTP,
# read the code out of the pod's log, verify it as a new user, verify again from
# a second device as a returning user, refresh, replay the spent refresh token
# to trigger reuse detection, log out, and finally exhaust the per-phone OTP
# rate limit — which is the step that proves Memorystore is genuinely on the
# path and not quietly bypassed.
set -euo pipefail

NAMESPACE="${NAMESPACE:-messaging}"
PHONE="${PHONE:?set PHONE}"
DEVICE="${DEVICE:-gate-device-1}"
DEVICE_2="${DEVICE}-second"
PORT="${PORT:-18083}"
BASE="http://127.0.0.1:${PORT}/v1/auth"

fail() { echo "❌ $*" >&2; exit 1; }
ok() { echo "   ✅ $*"; }

# ---------------------------------------------------------------------------
# Port-forward
# ---------------------------------------------------------------------------
# chatmgmt is a ClusterIP service; the auth endpoints are deliberately not on
# the external ingress, which only fronts the Gateway.

echo "==> port-forwarding svc/chatmgmt"
kubectl -n "${NAMESPACE}" port-forward svc/chatmgmt "${PORT}:80" >/tmp/port-forward.log 2>&1 &
PF_PID=$!
trap 'kill "${PF_PID}" 2>/dev/null || true' EXIT

for _ in $(seq 1 30); do
  if curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null || fail "port-forward never became reachable"
ok "port-forward up"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

post() {
  local path="$1" body="$2"
  shift 2
  curl -sS -o /tmp/response.json -w '%{http_code}' \
    -X POST "${BASE}${path}" \
    -H 'Content-Type: application/json' \
    "$@" \
    -d "${body}"
}

# field reads one JSON field. Note the absence of `//` fallbacks between camel
# and snake spellings: jq's alternative operator treats `false` as empty, so
# `.isNewUser // .is_new_user` silently yields null for a returning user — which
# cost a gate run to find. protojson emits lowerCamelCase, and the mux is
# configured with EmitUnpopulated (see port.ServeMuxOptions), so every declared
# field is present under exactly one name.
field() { jq -r "$1" /tmp/response.json; }

# read_otp pulls the code out of the pod's structured log. LogSMSProvider is the
# only SMS provider in the stack (ADR-015 v1.1), so this is where an OTP goes.
# Reading it here rather than fixing the generator to 000000 keeps the real
# crypto/rand path and the real MAC under test.
read_otp() {
  kubectl -n "${NAMESPACE}" logs deployment/chatmgmt --since=2m \
    | grep '"msg":"otp delivery (log-only)"' \
    | tail -1 \
    | jq -r '.otp'
}

# ---------------------------------------------------------------------------
# 1. Request an OTP
# ---------------------------------------------------------------------------

echo "==> 1. request OTP"
code="$(post /otp/request "{\"phone_number\":\"${PHONE}\"}")"
[[ "${code}" == "200" ]] || fail "request-otp returned ${code}: $(cat /tmp/response.json)"
[[ "$(field '.expiresAt.millis')" != "null" ]] || fail "no expires_at in response"
ok "OTP issued"

sleep 2
OTP="$(read_otp)"
[[ -n "${OTP}" && "${OTP}" != "null" ]] || fail "no OTP found in the pod log"
ok "OTP read from the pod log"

# ---------------------------------------------------------------------------
# 2. Verify as a new user
# ---------------------------------------------------------------------------

echo "==> 2. verify OTP (registration)"
code="$(post /otp/verify "{\"phone_number\":\"${PHONE}\",\"otp\":\"${OTP}\",\"device_id\":\"${DEVICE}\"}")"
[[ "${code}" == "200" ]] || fail "verify-otp returned ${code}: $(cat /tmp/response.json)"

IS_NEW="$(field '.isNewUser')"
ACCESS="$(field '.accessToken')"
REFRESH="$(field '.refreshToken')"
[[ "${IS_NEW}" == "true" ]] || fail "expected is_new_user=true on first verify, got ${IS_NEW}"
[[ -n "${ACCESS}" && "${ACCESS}" != "null" ]] || fail "no access token"
ok "registered; tokens issued"

# A replay of the consumed OTP must be refused (ADR-015 §1.3).
code="$(post /otp/verify "{\"phone_number\":\"${PHONE}\",\"otp\":\"${OTP}\",\"device_id\":\"${DEVICE}\"}")"
[[ "${code}" == "401" ]] || fail "a consumed OTP was accepted again (${code})"
ok "consumed OTP refused on replay"

# ---------------------------------------------------------------------------
# 3. Verify again as a returning user, from a second device
# ---------------------------------------------------------------------------

echo "==> 3. verify OTP (login, second device)"
code="$(post /otp/request "{\"phone_number\":\"${PHONE}\"}")"
[[ "${code}" == "200" ]] || fail "second request-otp returned ${code}"
sleep 2
OTP_2="$(read_otp)"
[[ -n "${OTP_2}" && "${OTP_2}" != "null" ]] || fail "no second OTP in the pod log"

code="$(post /otp/verify "{\"phone_number\":\"${PHONE}\",\"otp\":\"${OTP_2}\",\"device_id\":\"${DEVICE_2}\"}")"
[[ "${code}" == "200" ]] || fail "login verify returned ${code}: $(cat /tmp/response.json)"
IS_NEW="$(field '.isNewUser')"
[[ "${IS_NEW}" == "false" ]] || fail "expected is_new_user=false for a known phone, got ${IS_NEW}"
ok "returning user recognised — the phone lookup resolved to the existing user"

# ---------------------------------------------------------------------------
# 4. Refresh
# ---------------------------------------------------------------------------
# The device ID and bearer token travel as headers, which is exactly what the
# custom grpc-gateway header matcher exists to forward. If it regressed, this
# step fails with DEVICE_MISMATCH and nothing else would.

echo "==> 4. refresh tokens"
code="$(post /tokens/refresh "{\"refresh_token\":\"${REFRESH}\"}" \
  -H "Authorization: Bearer ${ACCESS}" -H "X-Device-Id: ${DEVICE}")"
[[ "${code}" == "200" ]] || fail "refresh returned ${code}: $(cat /tmp/response.json)"
NEW_ACCESS="$(field '.accessToken')"
NEW_REFRESH="$(field '.refreshToken')"
[[ -n "${NEW_ACCESS}" && "${NEW_ACCESS}" != "null" ]] || fail "no rotated access token"
[[ "${NEW_REFRESH}" != "${REFRESH}" ]] || fail "refresh returned the same token — no rotation happened"
ok "tokens rotated"

# ---------------------------------------------------------------------------
# 5. Reuse detection
# ---------------------------------------------------------------------------

echo "==> 5. replay the spent refresh token"
code="$(post /tokens/refresh "{\"refresh_token\":\"${REFRESH}\"}" \
  -H "Authorization: Bearer ${NEW_ACCESS}" -H "X-Device-Id: ${DEVICE}")"
[[ "${code}" == "401" ]] || fail "a spent refresh token was accepted (${code})"
ok "reuse detected and refused"

# Reuse detection revokes the whole session, it does not merely reject the
# replayed token (ADR-015 §4.2) — so the *current, legitimate* refresh token
# must stop working too. This is the assertion that distinguishes "we detected
# theft" from "we said no once".
code="$(post /tokens/refresh "{\"refresh_token\":\"${NEW_REFRESH}\"}" \
  -H "Authorization: Bearer ${NEW_ACCESS}" -H "X-Device-Id: ${DEVICE}")"
[[ "${code}" == "401" ]] || fail "the session survived reuse detection (${code})"
ok "session revoked, not just the replayed token"

# ---------------------------------------------------------------------------
# 6. Logout
# ---------------------------------------------------------------------------

echo "==> 6. logout"
code="$(post /logout '{}' -H "Authorization: Bearer ${NEW_ACCESS}")"
[[ "${code}" == "200" ]] || fail "logout returned ${code}: $(cat /tmp/response.json)"
ok "logged out"

# ---------------------------------------------------------------------------
# 7. Rate limit — the proof Memorystore is on the path
# ---------------------------------------------------------------------------
# OTPRequestRateLimitPerPhone is 3 per 15 minutes. Two requests have already
# been made above, so the limit lands within a few more. If Redis were absent
# or silently bypassed, every one of these would return 200.

echo "==> 7. exhaust the per-phone OTP rate limit"
LIMITED=0
for _ in $(seq 1 5); do
  code="$(post /otp/request "{\"phone_number\":\"${PHONE}\"}")"
  if [[ "${code}" == "429" ]]; then
    LIMITED=1
    break
  fi
done
[[ "${LIMITED}" == "1" ]] || fail "the per-phone OTP rate limit never triggered — is Memorystore reachable?"
ok "rate limit enforced via Memorystore"

echo
echo "✅ M1.2 flow gate passed: OTP -> token -> refresh -> reuse -> logout -> rate limit"
