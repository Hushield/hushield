#!/usr/bin/env bash
# scripts/smoke.sh -- drives a running spamfilter server through the full
# attest -> report -> block -> counter-report -> drop -> admin-allow -> gone
# lifecycle via curl. See README.md "Smoke test".
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

step "1. Attest 3 devices"
TOKEN1=$(attest_device "smoke-device-1-$$")
TOKEN2=$(attest_device "smoke-device-2-$$")
TOKEN3=$(attest_device "smoke-device-3-$$")
pass "attested 3 devices, got 3 device tokens"

report() {
  local token="$1" vote="$2"
  curl -sf -X POST "$BASE/api/v1/reports" \
    -H "Authorization: Bearer ${token}" \
    -H 'Content-Type: application/json' \
    -d "{\"number\":\"${NUMBER}\",\"category\":\"scam\",\"vote\":\"${vote}\"}"
}

step "2. Report $NUMBER as spam from all 3 devices"
report "$TOKEN1" spam >/dev/null
report "$TOKEN2" spam >/dev/null
report "$TOKEN3" spam >/dev/null
pass "filed 3 spam reports"

step "3. GET blocklist -- expect $NUMBER blocked"
BL1=$(curl -sf "$BASE/api/v1/blocklist?since=0" -H "Authorization: Bearer ${TOKEN1}")
ACTION1=$(entry_action "$BL1" "$NUMBER")
[ "$ACTION1" = "block" ] || fail "expected action=block for $NUMBER, got '${ACTION1}'; response=$BL1"
pass "$NUMBER action=block"

step "4. Counter-report $NUMBER as not_spam from all 3 devices"
report "$TOKEN1" not_spam >/dev/null
report "$TOKEN2" not_spam >/dev/null
report "$TOKEN3" not_spam >/dev/null
pass "filed 3 not_spam counter-reports"

step "5. GET blocklist -- expect $NUMBER dropped"
BL2=$(curl -sf "$BASE/api/v1/blocklist?since=0" -H "Authorization: Bearer ${TOKEN1}")
ACTION2=$(entry_action "$BL2" "$NUMBER")
[ -z "$ACTION2" ] || fail "expected $NUMBER absent from blocklist after counter-reports, got action='${ACTION2}'; response=$BL2"
pass "$NUMBER no longer present (dropped)"

FRESH_NUMBER="+1415555$((RANDOM % 9000 + 1000))"
step "6. Admin: allow $NUMBER, block a fresh number $FRESH_NUMBER"
curl -sf -X POST "$BASE/api/v1/admin/overrides" \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d "{\"number\":\"${NUMBER}\",\"mode\":\"allow\"}" >/dev/null
curl -sf -X POST "$BASE/api/v1/admin/overrides" \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d "{\"number\":\"${FRESH_NUMBER}\",\"mode\":\"block\"}" >/dev/null
pass "admin overrides applied"

step "7. GET blocklist -- expect $NUMBER gone (allowlisted), $FRESH_NUMBER blocked"
BL3=$(curl -sf "$BASE/api/v1/blocklist?since=0" -H "Authorization: Bearer ${TOKEN1}")
ACTION3=$(entry_action "$BL3" "$NUMBER")
[ -z "$ACTION3" ] || fail "expected $NUMBER absent (allowlisted), got action='${ACTION3}'"
pass "$NUMBER gone (allowlisted)"

ACTION4=$(entry_action "$BL3" "$FRESH_NUMBER")
[ "$ACTION4" = "block" ] || fail "expected $FRESH_NUMBER action=block, got '${ACTION4}'; response=$BL3"
pass "$FRESH_NUMBER action=block (override)"

echo
echo "ALL SMOKE STEPS PASSED"
