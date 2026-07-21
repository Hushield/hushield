#!/usr/bin/env bash
# scripts/smoke.sh -- drives a running spamfilter server through the full
# attest -> assert (token refresh) -> report -> block -> counter-report ->
# unblock -> numbers lookup -> push-token -> admin-allow lifecycle via curl.
# See README.md "Smoke test".
#
# Usage:
#   BASE=http://localhost:8080 ADMIN_TOKEN=changeme ./scripts/smoke.sh
#
# Uses jq for JSON parsing if available; otherwise falls back to a small
# grep/sed-based extractor that is good enough for this script's flat JSON
# response shapes.

set -euo pipefail

BASE="${BASE:-http://localhost:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"

if [ -z "$ADMIN_TOKEN" ]; then
  echo "ADMIN_TOKEN is required" >&2
  exit 1
fi

HAVE_JQ=0
if command -v jq >/dev/null 2>&1; then
  HAVE_JQ=1
fi

# extract_field <json> <field> extracts a top-level scalar field from the
# "data" object of an envelope response.
extract_field() {
  local json="$1" field="$2"
  if [ "$HAVE_JQ" = "1" ]; then
    echo "$json" | jq -r ".data.${field} // empty"
  else
    echo "$json" | grep -o "\"${field}\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -1 | sed -E 's/.*:[[:space:]]*"([^"]*)"/\1/'
  fi
}

# entry_action <blocklist-json> <number> prints the "action" of the
# blocklist entry for <number>, or empty if the number is not present.
entry_action() {
  local json="$1" number="$2"
  if [ "$HAVE_JQ" = "1" ]; then
    echo "$json" | jq -r --arg n "$number" '.data.entries[]? | select(.number == $n) | .action'
  else
    echo "$json" | grep -o "{[^{}]*\"number\":\"${number}\"[^{}]*}" | head -1 | grep -o '"action":"[^"]*"' | sed -E 's/.*:"([^"]*)"/\1/'
  fi
}

step() { echo; echo "== $* =="; }
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

NUMBER="+1415555$((RANDOM % 9000 + 1000))"
echo "base: $BASE"
echo "smoke number: $NUMBER"

attest_device() {
  local key_id="$1"
  local ch_resp challenge v_resp token

  ch_resp=$(curl -sf -X POST "$BASE/api/v1/attest/challenge")
  challenge=$(extract_field "$ch_resp" "challenge")
  [ -n "$challenge" ] || fail "attest challenge for $key_id returned no challenge: $ch_resp"

  v_resp=$(curl -sf -X POST "$BASE/api/v1/attest/verify" \
    -H 'Content-Type: application/json' \
    -d "{\"key_id\":\"${key_id}\",\"attestation\":\"$(printf '%s' "mock-attestation-${key_id}" | base64)\",\"challenge\":\"${challenge}\"}")
  token=$(extract_field "$v_resp" "device_token")
  [ -n "$token" ] || fail "attest verify for $key_id returned no device_token: $v_resp"

  echo "$token"
}

# assert_device <key_id> refreshes an already-attested device's token via
# /attest/assert with a fresh challenge, and prints the new device_token.
# In ATTEST_MODE=mock the MockVerifier accepts any assertion for a device
# that has already gone through /attest/challenge + /attest/verify.
assert_device() {
  local key_id="$1"
  local ch_resp challenge a_resp token

  ch_resp=$(curl -sf -X POST "$BASE/api/v1/attest/challenge")
  challenge=$(extract_field "$ch_resp" "challenge")
  [ -n "$challenge" ] || fail "assert challenge for $key_id returned no challenge: $ch_resp"

  a_resp=$(curl -sf -X POST "$BASE/api/v1/attest/assert" \
    -H 'Content-Type: application/json' \
    -d "{\"key_id\":\"${key_id}\",\"assertion\":\"$(printf '%s' "mock-assertion-${key_id}" | base64)\",\"challenge\":\"${challenge}\"}")
  token=$(extract_field "$a_resp" "device_token")
  [ -n "$token" ] || fail "assert for $key_id returned no device_token: $a_resp"

  echo "$token"
}

KEYID1="smoke-device-1-$$"
KEYID2="smoke-device-2-$$"
KEYID3="smoke-device-3-$$"

step "1. Attest 3 devices"
TOKEN1=$(attest_device "$KEYID1")
TOKEN2=$(attest_device "$KEYID2")
TOKEN3=$(attest_device "$KEYID3")
pass "attested 3 devices, got 3 device tokens"

step "2. Refresh device 1's token via /attest/assert"
# device_token bytes are a deterministic function of (device_id, expiry
# second); sleep 1s so the refreshed token's expiry second -- and therefore
# its bytes -- provably differs from the original /attest/verify token.
sleep 1
TOKEN1_REFRESHED=$(assert_device "$KEYID1")
[ -n "$TOKEN1_REFRESHED" ] || fail "assert returned an empty device_token"
[ "$TOKEN1_REFRESHED" != "$TOKEN1" ] || fail "expected a new device token from /attest/assert, got byte-identical to the /attest/verify token"
TOKEN1="$TOKEN1_REFRESHED"
pass "device 1 asserted with a fresh challenge, got a new device_token"
echo "new device_token: $TOKEN1"

report() {
  local token="$1" vote="$2"
  curl -sf -X POST "$BASE/api/v1/reports" \
    -H "Authorization: Bearer ${token}" \
    -H 'Content-Type: application/json' \
    -d "{\"number\":\"${NUMBER}\",\"category\":\"scam\",\"vote\":\"${vote}\"}"
}

step "3. Report $NUMBER as spam from all 3 devices"
report "$TOKEN1" spam >/dev/null
report "$TOKEN2" spam >/dev/null
report "$TOKEN3" spam >/dev/null
pass "filed 3 spam reports"

step "4. GET blocklist -- expect $NUMBER blocked; capture cursor"
BL1=$(curl -sf "$BASE/api/v1/blocklist?since=0" -H "Authorization: Bearer ${TOKEN1}")
ACTION1=$(entry_action "$BL1" "$NUMBER")
[ "$ACTION1" = "block" ] || fail "expected action=block for $NUMBER, got '${ACTION1}'; response=$BL1"
CURSOR_AFTER_BLOCK=$(extract_field "$BL1" "cursor")
[ -n "$CURSOR_AFTER_BLOCK" ] || fail "expected a non-empty cursor in blocklist response: $BL1"
pass "$NUMBER action=block; cursor=$CURSOR_AFTER_BLOCK"

step "5. Counter-report $NUMBER as not_spam from all 3 devices"
# The blocklist cursor is a compound (second, phone_number_id) keyset: a
# second update to the SAME row within the SAME wall-clock second as the
# cursor is not strictly greater than (cursor_sec, cursor_id) (same id), so
# it would be invisible to the delta below. Sleep past the second boundary
# so the counter-report's updated_at is unambiguously after the cursor.
sleep 1
report "$TOKEN1" not_spam >/dev/null
report "$TOKEN2" not_spam >/dev/null
report "$TOKEN3" not_spam >/dev/null
pass "filed 3 not_spam counter-reports"

step "6. GET blocklist since=\$CURSOR_AFTER_BLOCK -- true delta must show $NUMBER as action=unblock"
BL2=$(curl -sf "$BASE/api/v1/blocklist?since=${CURSOR_AFTER_BLOCK}" -H "Authorization: Bearer ${TOKEN1}")
ACTION2=$(entry_action "$BL2" "$NUMBER")
[ "$ACTION2" = "unblock" ] || fail "expected action=unblock for $NUMBER in the delta since cursor $CURSOR_AFTER_BLOCK, got '${ACTION2}'; response=$BL2"
pass "$NUMBER action=unblock in delta since cursor $CURSOR_AFTER_BLOCK"
echo "delta response: $BL2"

step "7. GET /api/v1/numbers/$NUMBER -- show current reputation"
NUM_RESP=$(curl -sf "$BASE/api/v1/numbers/${NUMBER}" -H "Authorization: Bearer ${TOKEN1}")
NUM_STATUS=$(extract_field "$NUM_RESP" "status")
NUM_ACTION=$(extract_field "$NUM_RESP" "action")
[ "$NUM_STATUS" = "unknown" ] || fail "expected status=unknown for $NUMBER after counter-reports, got '${NUM_STATUS}'; response=$NUM_RESP"
[ "$NUM_ACTION" = "none" ] || fail "expected action=none for $NUMBER after counter-reports, got '${NUM_ACTION}'; response=$NUM_RESP"
pass "$NUMBER status=$NUM_STATUS action=$NUM_ACTION"
echo "numbers response: $NUM_RESP"

step "8. POST /api/v1/devices/push-token"
PUSH_TOKEN=$(printf '%016x%016x%016x%016x' "$((RANDOM))" "$((RANDOM))" "$((RANDOM))" "$((RANDOM))")
PT_RESP=$(curl -sf -X POST "$BASE/api/v1/devices/push-token" \
  -H "Authorization: Bearer ${TOKEN1}" \
  -H 'Content-Type: application/json' \
  -d "{\"push_token\":\"${PUSH_TOKEN}\",\"environment\":\"sandbox\"}")
PT_STATUS=$(extract_field "$PT_RESP" "status")
[ "$PT_STATUS" = "registered" ] || fail "expected status=registered, got '${PT_STATUS}'; response=$PT_RESP"
pass "push token registered"
echo "push-token response: $PT_RESP"

FRESH_NUMBER="+1415555$((RANDOM % 9000 + 1000))"
step "9. Admin: allow $NUMBER, block a fresh number $FRESH_NUMBER"
curl -sf -X POST "$BASE/api/v1/admin/overrides" \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d "{\"number\":\"${NUMBER}\",\"mode\":\"allow\"}" >/dev/null
curl -sf -X POST "$BASE/api/v1/admin/overrides" \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d "{\"number\":\"${FRESH_NUMBER}\",\"mode\":\"block\"}" >/dev/null
pass "admin overrides applied"

step "10. GET blocklist -- expect $NUMBER still a tombstone (allowlisted), $FRESH_NUMBER blocked"
# NUMBER was blockable once (step 4), so it keeps surfacing as an
# action:"unblock" removal tombstone in every snapshot for as long as it
# stays outside the blockable set (unknown or, now, allowlisted) -- it does
# NOT simply vanish from the delta. See README "action:unblock semantics".
BL3=$(curl -sf "$BASE/api/v1/blocklist?since=0" -H "Authorization: Bearer ${TOKEN1}")
ACTION3=$(entry_action "$BL3" "$NUMBER")
[ "$ACTION3" = "unblock" ] || fail "expected $NUMBER action=unblock (allowlisted tombstone), got '${ACTION3}'; response=$BL3"
pass "$NUMBER action=unblock (allowlisted tombstone persists)"

ACTION4=$(entry_action "$BL3" "$FRESH_NUMBER")
[ "$ACTION4" = "block" ] || fail "expected $FRESH_NUMBER action=block, got '${ACTION4}'; response=$BL3"
pass "$FRESH_NUMBER action=block (override)"

echo
echo "ALL SMOKE STEPS PASSED"
