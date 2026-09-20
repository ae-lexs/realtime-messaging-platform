#!/usr/bin/env bash
# The M1.3 flow gate. Runs INSIDE the toolbox container (invoked by
# scripts/chat.sh flow), because the port-forward and the requests that use it
# must share a network namespace.
#
# #27 shipped three RPCs — CreateChat, GetChat, ListChats — as unit tests
# against fakes only (the PR's own "Not done" section: "Not yet run against
# live infrastructure"). This is that run. It drives the deployed ChatMgmt
# through registration (reusing the M1.2 OTP flow to get real bearer tokens,
# since every chat route is authenticated) and then:
#
#   - create a direct chat, and replay the same create to prove idempotency
#     (CreateChatResponse.is_existing)
#   - read it back as a member, with my_membership populated
#   - the two refusals AuthenticatedChatServer and ChatService exist to
#     produce: a non-member reading a real chat (403, ErrNotMember) versus a
#     member reading a chat that doesn't exist (404, ErrNotFound) — the PR's
#     stated reason these must stay distinct
#   - list chats and find it
#   - an unauthenticated call, to prove the decorator is actually on the wire
#     and not just in the unit tests
set -euo pipefail

NAMESPACE="${NAMESPACE:-messaging}"
PHONE_A="${PHONE_A:?set PHONE_A}"
PHONE_B="${PHONE_B:?set PHONE_B}"
PHONE_C="${PHONE_C:?set PHONE_C}"
PORT="${PORT:-18084}"
BASE="http://127.0.0.1:${PORT}/v1"

fail() { echo "❌ $*" >&2; exit 1; }
ok() { echo "   ✅ $*"; }

# ---------------------------------------------------------------------------
# Port-forward
# ---------------------------------------------------------------------------

echo "==> port-forwarding svc/chatmgmt"
kubectl -n "${NAMESPACE}" port-forward svc/chatmgmt "${PORT}:80" >/tmp/port-forward-chat.log 2>&1 &
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

req() {
  local method="$1" path="$2" body="${3:-}"
  shift 3
  if [[ -n "${body}" ]]; then
    curl -sS -o /tmp/chat-response.json -w '%{http_code}' \
      -X "${method}" "${BASE}${path}" \
      -H 'Content-Type: application/json' "$@" -d "${body}"
  else
    curl -sS -o /tmp/chat-response.json -w '%{http_code}' \
      -X "${method}" "${BASE}${path}" "$@"
  fi
}

# field reads one JSON field. See scripts/auth-flow.sh on why `//` fallbacks
# between camel/snake spellings are avoided — jq treats `false` as empty.
field() { jq -r "$1" /tmp/chat-response.json; }

#
# LogSMSProvider masks the phone number in the log (last 4 digits only), so
# filtering the grep by the full E.164 number would never match. Registration
# happens sequentially with no concurrent requests, so — as in
# scripts/auth-flow.sh — the latest matching line is always this call's OTP.
read_otp() {
  kubectl -n "${NAMESPACE}" logs deployment/chatmgmt --since=2m \
    | grep '"msg":"otp delivery (log-only)"' \
    | tail -1 \
    | jq -r '.otp'
}

# register <phone> <device> — the M1.2 flow, trimmed to what M1.3 needs: a
# real access token and the registered user_id. Echoes "user_id access_token".
register() {
  local phone="$1" device="$2"
  local code
  code="$(req POST /auth/otp/request "{\"phone_number\":\"${phone}\"}")"
  [[ "${code}" == "200" ]] || fail "request-otp(${phone}) returned ${code}: $(cat /tmp/chat-response.json)"
  sleep 2
  local otp
  otp="$(read_otp)"
  [[ -n "${otp}" && "${otp}" != "null" ]] || fail "no OTP found in the pod log for ${phone}"

  code="$(req POST /auth/otp/verify "{\"phone_number\":\"${phone}\",\"otp\":\"${otp}\",\"device_id\":\"${device}\"}")"
  [[ "${code}" == "200" ]] || fail "verify-otp(${phone}) returned ${code}: $(cat /tmp/chat-response.json)"
  local user_id access_token
  user_id="$(field '.user.userId')"
  access_token="$(field '.accessToken')"
  [[ -n "${user_id}" && "${user_id}" != "null" ]] || fail "no user_id for ${phone}"
  echo "${user_id} ${access_token}"
}

# ---------------------------------------------------------------------------
# 1. Register three users
# ---------------------------------------------------------------------------

echo "==> 1. register three users"
read -r USER_A TOKEN_A <<<"$(register "${PHONE_A}" flow-a)"
ok "user A registered (${USER_A})"
read -r USER_B TOKEN_B <<<"$(register "${PHONE_B}" flow-b)"
ok "user B registered (${USER_B})"
read -r USER_C TOKEN_C <<<"$(register "${PHONE_C}" flow-c)"
ok "user C registered (${USER_C}, not a member of anything A creates)"

# ---------------------------------------------------------------------------
# 2. Create a direct chat, A -> B
# ---------------------------------------------------------------------------

echo "==> 2. create a direct chat (A, B)"
code="$(req POST /chats \
  "{\"chatType\":\"CHAT_TYPE_DIRECT\",\"memberIds\":[\"${USER_B}\"]}" \
  -H "Authorization: Bearer ${TOKEN_A}")"
[[ "${code}" == "200" || "${code}" == "201" ]] || fail "create-chat returned ${code}: $(cat /tmp/chat-response.json)"
CHAT_ID="$(field '.chat.chatId')"
[[ -n "${CHAT_ID}" && "${CHAT_ID}" != "null" ]] || fail "no chat_id in create-chat response"
[[ "$(field '.chat.chatType')" == "CHAT_TYPE_DIRECT" ]] || fail "chat_type mismatch: $(field '.chat.chatType')"
[[ "$(field '.members | length')" == "2" ]] || fail "expected 2 members, got $(field '.members | length')"
[[ "$(field '.isExisting')" == "false" ]] || fail "expected is_existing=false on first create"
ok "direct chat created (${CHAT_ID}), HTTP ${code}"

# ---------------------------------------------------------------------------
# 3. Replay the same create — must return the same chat, not a duplicate
# ---------------------------------------------------------------------------

echo "==> 3. replay create-chat for the same pair"
code="$(req POST /chats \
  "{\"chatType\":\"CHAT_TYPE_DIRECT\",\"memberIds\":[\"${USER_B}\"]}" \
  -H "Authorization: Bearer ${TOKEN_A}")"
[[ "${code}" == "200" ]] || fail "replayed create-chat returned ${code}, expected 200: $(cat /tmp/chat-response.json)"
[[ "$(field '.chat.chatId')" == "${CHAT_ID}" ]] || fail "replay returned a different chat_id — pair uniqueness did not hold"
[[ "$(field '.isExisting')" == "true" ]] || fail "expected is_existing=true on replay"
ok "same chat returned, is_existing=true"
# The proto comment on CreateChatResponse.is_existing claims REST also sets
# X-Idempotent-Replay: true on this path. It doesn't — grep the codebase and
# the header is set nowhere. Documented separately; not re-asserted as a
# failure here so this gate measures what ships, not what the comment intends.

# ---------------------------------------------------------------------------
# 4. Read it back as a member
# ---------------------------------------------------------------------------

echo "==> 4. get-chat as a member (B)"
code="$(req GET "/chats/${CHAT_ID}" "" -H "Authorization: Bearer ${TOKEN_B}")"
[[ "${code}" == "200" ]] || fail "get-chat(member) returned ${code}: $(cat /tmp/chat-response.json)"
[[ "$(field '.chat.chatId')" == "${CHAT_ID}" ]] || fail "get-chat returned the wrong chat"
[[ "$(field '.myMembership.userId')" == "${USER_B}" ]] || fail "my_membership.user_id is not the caller"
ok "member read succeeds, my_membership resolves to the caller"

# ---------------------------------------------------------------------------
# 5. The not-member / not-found distinction
# ---------------------------------------------------------------------------

echo "==> 5a. get-chat as a non-member (C) on a real chat -> 403 NOT_MEMBER"
code="$(req GET "/chats/${CHAT_ID}" "" -H "Authorization: Bearer ${TOKEN_C}")"
[[ "${code}" == "403" ]] || fail "expected 403 for a non-member, got ${code}: $(cat /tmp/chat-response.json)"
ok "non-member refused with 403"

echo "==> 5b. get-chat on a chat that does not exist -> 404 NOT_FOUND"
code="$(req GET "/chats/00000000-0000-0000-0000-000000000000" "" -H "Authorization: Bearer ${TOKEN_A}")"
[[ "${code}" == "404" ]] || fail "expected 404 for a missing chat, got ${code}: $(cat /tmp/chat-response.json)"
ok "missing chat answers 404, distinct from the non-member's 403"

# ---------------------------------------------------------------------------
# 6. List chats
# ---------------------------------------------------------------------------

echo "==> 6. list-chats as A"
code="$(req GET "/chats" "" -H "Authorization: Bearer ${TOKEN_A}")"
[[ "${code}" == "200" ]] || fail "list-chats returned ${code}: $(cat /tmp/chat-response.json)"
FOUND="$(jq -r --arg id "${CHAT_ID}" '[.chats[]?.chatId] | index($id) != null' /tmp/chat-response.json)"
[[ "${FOUND}" == "true" ]] || fail "created chat not present in list-chats"
ok "list-chats includes the created chat"

# ---------------------------------------------------------------------------
# 7. Unauthenticated call — proves the decorator is on the wire, not just
#    covered by the unit tests
# ---------------------------------------------------------------------------

echo "==> 7. unauthenticated create-chat -> 401"
code="$(req POST /chats "{\"chatType\":\"CHAT_TYPE_DIRECT\",\"memberIds\":[\"${USER_B}\"]}")"
[[ "${code}" == "401" ]] || fail "expected 401 unauthenticated, got ${code}: $(cat /tmp/chat-response.json)"
ok "unauthenticated request refused"

echo
echo "✅ M1.3 flow gate passed: register -> create (+ idempotent replay) -> read -> not-member/not-found -> list -> unauthenticated refusal"
