#!/usr/bin/env bash
# The ListChats membership-fields gate (M1.3 open item 1, ADR-006 §4.2), against
# the DEPLOYED chatmgmt. Runs INSIDE the toolbox (invoked by
# `scripts/chat.sh listchats`), so the port-forward and the requests share a
# network namespace.
#
# A builds a page that mixes a muted group and two direct chats — one A opened,
# one opened by its peer — then lists chats and asserts, item by item:
#   - myMembership on every item (role: OWNER for the group, MEMBER for directs);
#   - otherMember on every direct item, naming the right peer; none on the group;
#   - mutedUntil on the muted group.
# Users registered through OTP have no display name yet (no profile endpoint),
# so otherMember.displayName is printed, not asserted.
#
# The read-cost half ("at most three Firestore round trips per page") cannot be
# seen from the client: it is measured from the chat.list span tree in Cloud
# Trace, which scripts/telemetry-proof.sh appends to this log.
set -euo pipefail

NAMESPACE="${NAMESPACE:-messaging}"
PHONE_A="${PHONE_A:?}" PHONE_B="${PHONE_B:?}" PHONE_C="${PHONE_C:?}"
PORT="${PORT:-18085}"
BASE="http://127.0.0.1:${PORT}/v1"

fail() { echo "❌ $*" >&2; exit 1; }
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

req() {
  local method="$1" path="$2" body="${3:-}"
  shift 3
  if [[ -n "${body}" ]]; then
    curl -sS -o /tmp/lc.json -w '%{http_code}' -X "${method}" "${BASE}${path}" \
      -H 'Content-Type: application/json' "$@" -d "${body}"
  else
    curl -sS -o /tmp/lc.json -w '%{http_code}' -X "${method}" "${BASE}${path}" "$@"
  fi
}
field() { jq -r "$1" /tmp/lc.json; }
read_otp() {
  kubectl -n "${NAMESPACE}" logs deployment/chatmgmt --since=2m \
    | grep '"msg":"otp delivery (log-only)"' | tail -1 | jq -r '.otp'
}
register() {
  local phone="$1" device="$2" code otp
  code="$(req POST /auth/otp/request "{\"phone_number\":\"${phone}\"}")"
  [[ "${code}" == "200" ]] || fail "request-otp(${phone}) returned ${code}: $(cat /tmp/lc.json)"
  sleep 2
  otp="$(read_otp)"
  code="$(req POST /auth/otp/verify "{\"phone_number\":\"${phone}\",\"otp\":\"${otp}\",\"device_id\":\"${device}\"}")"
  [[ "${code}" == "200" ]] || fail "verify-otp(${phone}) returned ${code}: $(cat /tmp/lc.json)"
  echo "$(field '.user.userId') $(field '.accessToken')"
}

echo "==> 1. register A, B, C"
read -r USER_A TOKEN_A <<<"$(register "${PHONE_A}" lc-a)"
read -r USER_B TOKEN_B <<<"$(register "${PHONE_B}" lc-b)"
read -r USER_C TOKEN_C <<<"$(register "${PHONE_C}" lc-c)"
ok "registered A=${USER_A} B=${USER_B} C=${USER_C}"

echo "==> 2. build a mixed page for A"
code="$(req POST /chats "{\"chatType\":\"CHAT_TYPE_GROUP\",\"name\":\"listchats gate\",\"memberIds\":[\"${USER_B}\",\"${USER_C}\"]}" -H "Authorization: Bearer ${TOKEN_A}")"
[[ "${code}" =~ ^20[01]$ ]] || fail "create group returned ${code}: $(cat /tmp/lc.json)"
GROUP_ID="$(field '.chat.chatId')"
code="$(req POST "/chats/${GROUP_ID}/mute" "{\"durationHours\":24}" -H "Authorization: Bearer ${TOKEN_A}")"
[[ "${code}" == "200" ]] || fail "mute group returned ${code}: $(cat /tmp/lc.json)"
code="$(req POST /chats "{\"chatType\":\"CHAT_TYPE_DIRECT\",\"memberIds\":[\"${USER_B}\"]}" -H "Authorization: Bearer ${TOKEN_A}")"
[[ "${code}" =~ ^20[01]$ ]] || fail "create direct A-B returned ${code}"
DIRECT_AB="$(field '.chat.chatId')"
code="$(req POST /chats "{\"chatType\":\"CHAT_TYPE_DIRECT\",\"memberIds\":[\"${USER_A}\"]}" -H "Authorization: Bearer ${TOKEN_C}")"
[[ "${code}" =~ ^20[01]$ ]] || fail "create direct C-A (opened by C) returned ${code}"
DIRECT_CA="$(field '.chat.chatId')"
ok "group ${GROUP_ID} (A owner, muted 24h), direct A-B ${DIRECT_AB} (opened by A), direct C-A ${DIRECT_CA} (opened by C)"

echo "==> 3. list chats as A"
LIST_STARTED="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
code="$(req GET /chats "" -H "Authorization: Bearer ${TOKEN_A}")"
[[ "${code}" == "200" ]] || fail "list-chats returned ${code}: $(cat /tmp/lc.json)"
echo "   list_started_at=${LIST_STARTED}"
echo "   raw items:"
jq -c '.items[]' /tmp/lc.json | sed 's/^/     /'

item() { jq -c --arg id "$1" '.items[] | select(.chatId == $id)' /tmp/lc.json; }
[[ "$(jq '.items | length' /tmp/lc.json)" == "3" ]] || fail "expected 3 items, got $(jq '.items | length' /tmp/lc.json)"
ok "3 items"

G="$(item "${GROUP_ID}")"
[[ "$(jq -r '.myMembership.role' <<<"${G}")" == "MEMBER_ROLE_OWNER" ]] || fail "group myMembership.role: ${G}"
[[ "$(jq -r '.myMembership.mutedUntil.millis // empty' <<<"${G}")" != "" ]] || fail "muted group has no mutedUntil: ${G}"
[[ "$(jq -r '.otherMember' <<<"${G}")" == "null" ]] || fail "group carries an otherMember: ${G}"
ok "group: myMembership.role=OWNER, mutedUntil present, otherMember null"

for pair in "${DIRECT_AB}:${USER_B}:opened by A" "${DIRECT_CA}:${USER_C}:opened by C"; do
  id="${pair%%:*}"; rest="${pair#*:}"; peer="${rest%%:*}"; how="${rest#*:}"
  D="$(item "${id}")"
  [[ "$(jq -r '.myMembership.role' <<<"${D}")" == "MEMBER_ROLE_MEMBER" ]] || fail "direct ${id} myMembership.role: ${D}"
  [[ "$(jq -r '.otherMember.userId' <<<"${D}")" == "${peer}" ]] || fail "direct ${id} otherMember is not ${peer}: ${D}"
  ok "direct (${how}): myMembership.role=MEMBER, otherMember.userId=${peer}, displayName=\"$(jq -r '.otherMember.displayName' <<<"${D}")\""
done

while IFS= read -r item_json; do
  [[ "$(jq -r '.myMembership.joinedAt.millis // empty' <<<"${item_json}")" != "" ]] || fail "an item lacks myMembership.joinedAt"
done < <(jq -c '.items[]' /tmp/lc.json)
ok "every item carries myMembership.joinedAt"

# The deprecated chats field is still populated, in the same order (non-breaking).
[[ "$(jq -c '[.chats[].chatId]' /tmp/lc.json)" == "$(jq -c '[.items[].chatId]' /tmp/lc.json)" ]] \
  || fail "deprecated chats is not populated in the same order as items"
ok "deprecated chats still populated, same order as items"

echo
echo "✅ ListChats API half passed. Read-cost half: see the chat.list span tree appended below."
