#!/usr/bin/env bash
#
# smoke-prod.sh -- verify a PRODUCTION (ATTEST_MODE=apple) deployment.
#
# scripts/smoke.sh cannot do this. It fabricates base64 attestation blobs, which
# only the mock verifier accepts, so against a real apple-mode server every one
# of its attest steps correctly fails. That left production with no automated
# verification at all.
#
# This script checks everything that CAN be checked without a genuine device
# attestation -- and treats rejection as the passing result where rejection is
# the correct behaviour. It deliberately proves NEGATIVES: that the server
# refuses what it should refuse. A deployment accidentally left in mock mode
# passes smoke.sh and fails this.
#
# Usage:
#   BASE=https://api.hushield.com ./scripts/smoke-prod.sh
#   BASE=https://api.hushield.com ADMIN_TOKEN=... ./scripts/smoke-prod.sh
#
#   ADMIN_TOKEN is optional. Without it, admin routes are only checked for
#   correct rejection; with it, a full authenticated round-trip is exercised.
#
# Exit status is non-zero if any check fails, so this is usable as a post-deploy
# gate in CI or from scripts/deploy.sh.

set -uo pipefail

BASE="${BASE:-https://api.hushield.com}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
CURL=(curl -sS --max-time 20)

pass=0
fail=0

ok()   { printf "  \033[32mPASS\033[0m %s\n" "$*"; pass=$((pass+1)); }
bad()  { printf "  \033[31mFAIL\033[0m %s\n" "$*"; fail=$((fail+1)); }
head1() { printf "\n\033[1m%s\033[0m\n" "$*"; }

# status <method> <path> [data] -- echoes the HTTP status code.
status() {
	local method="$1" path="$2" data="${3:-}"
	if [ -n "$data" ]; then
		"${CURL[@]}" -o /dev/null -w '%{http_code}' -X "$method" "$BASE$path" \
			-H 'Content-Type: application/json' -d "$data" 2>/dev/null
	else
		"${CURL[@]}" -o /dev/null -w '%{http_code}' -X "$method" "$BASE$path" 2>/dev/null
	fi
}

echo "base: $BASE"

# ---------------------------------------------------------------- reachability

head1 "1. Liveness and transport"

code="$(status GET /healthz)"
if [ "$code" = "200" ]; then ok "/healthz returned 200"; else bad "/healthz returned $code, want 200"; fi

body="$("${CURL[@]}" "$BASE/healthz" 2>/dev/null)"
if printf '%s' "$body" | grep -q '"success":true'; then
	ok "/healthz uses the standard response envelope"
else
	bad "/healthz body is not the standard envelope: $(printf '%s' "$body" | head -c 120)"
fi
if printf '%s' "$body" | grep -q '"request_id"'; then
	ok "responses carry a request_id (middleware wired)"
else
	bad "no request_id in the response; the request-id middleware is not wired"
fi

case "$BASE" in
https://*)
	# --fail-with-body would mask a TLS problem as an HTTP one; check the handshake
	# explicitly so an expiring or mismatched certificate is unambiguous.
	if "${CURL[@]}" -o /dev/null "$BASE/healthz" 2>/dev/null; then
		ok "TLS handshake and certificate validate"
	else
		bad "TLS verification failed (expired, wrong host, or incomplete chain)"
	fi

	plain="${BASE/https:/http:}"
	rcode="$("${CURL[@]}" -o /dev/null -w '%{http_code}' "$plain/healthz" 2>/dev/null)"
	if [ "$rcode" = "301" ] || [ "$rcode" = "308" ]; then
		ok "HTTP redirects to HTTPS ($rcode)"
	else
		bad "HTTP returned $rcode, want a 301/308 redirect to HTTPS"
	fi
	;;
*)
	printf "  \033[33mSKIP\033[0m %s\n" "not an https base URL; TLS checks skipped"
	;;
esac

# ------------------------------------------------------------ authn / authz

head1 "2. Authentication is enforced"

for route in "/api/v1/blocklist" "/api/v1/numbers/+14155551234"; do
	code="$(status GET "$route")"
	if [ "$code" = "401" ]; then
		ok "GET $route without a token -> 401"
	else
		bad "GET $route without a token -> $code, want 401"
	fi
done

code="$(status POST /api/v1/reports '{"number":"+14155551234","vote":"spam"}')"
if [ "$code" = "401" ]; then
	ok "POST /api/v1/reports without a token -> 401"
else
	bad "POST /api/v1/reports without a token -> $code, want 401"
fi

code="$("${CURL[@]}" -o /dev/null -w '%{http_code}' -X GET "$BASE/api/v1/blocklist" \
	-H 'Authorization: Bearer not-a-real-token' 2>/dev/null)"
if [ "$code" = "401" ]; then
	ok "a forged device token -> 401"
else
	bad "a forged device token -> $code, want 401"
fi

head1 "3. Admin routes are protected"

code="$(status POST /api/v1/admin/overrides '{"number":"+14155551234","mode":"block"}')"
if [ "$code" = "401" ] || [ "$code" = "503" ]; then
	ok "admin override without a token -> $code (401 protected, 503 disabled)"
else
	bad "admin override without a token -> $code, want 401 or 503"
fi

code="$("${CURL[@]}" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/admin/overrides" \
	-H 'Authorization: Bearer wrong-admin-token' -H 'Content-Type: application/json' \
	-d '{"number":"+14155551234","mode":"block"}' 2>/dev/null)"
if [ "$code" = "401" ] || [ "$code" = "503" ]; then
	ok "a wrong admin token -> $code"
else
	bad "a wrong admin token -> $code, want 401 or 503"
fi

# --------------------------------------------------- apple mode, proven by refusal

head1 "4. Real App Attest is enforced (the mock verifier would accept these)"

# Challenge issuance is intentionally public -- it is pre-authentication, and a
# challenge is useless without a valid attestation over it.
ch_body="$("${CURL[@]}" -X POST "$BASE/api/v1/attest/challenge" \
	-H 'Content-Type: application/json' -d '{"key_id":"smoke-prod"}' 2>/dev/null)"
challenge="$(printf '%s' "$ch_body" | sed -n 's/.*"challenge":"\([^"]*\)".*/\1/p')"
if [ -n "$challenge" ]; then
	ok "challenge issued (public by design; useless without a real attestation)"
else
	bad "no challenge issued: $(printf '%s' "$ch_body" | head -c 140)"
fi

# THE decisive check. A fabricated attestation is what smoke.sh sends and what
# the mock verifier accepts. In apple mode it must be refused. If this returns
# 2xx, the deployment is in mock mode and is accepting forged devices.
code="$("${CURL[@]}" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/attest/verify" \
	-H 'Content-Type: application/json' \
	-d "{\"key_id\":\"c21va2U=\",\"attestation\":\"Zm9yZ2VkLWF0dGVzdGF0aW9u\",\"challenge\":\"${challenge:-x}\"}" 2>/dev/null)"
case "$code" in
	401|400|422)
		ok "a FORGED attestation was rejected ($code) -- server is genuinely in apple mode" ;;
	2*)
		bad "a FORGED attestation was ACCEPTED ($code) -- this deployment is in MOCK mode and will accept any device" ;;
	*)
		bad "forged attestation -> unexpected $code (want 400/401/422)" ;;
esac

code="$("${CURL[@]}" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/attest/assert" \
	-H 'Content-Type: application/json' \
	-d '{"key_id":"c21va2U=","assertion":"Zm9yZ2VkLWFzc2VydGlvbg==","challenge":"x"}' 2>/dev/null)"
case "$code" in
	401|400|422) ok "a forged assertion was rejected ($code)" ;;
	2*)          bad "a forged assertion was ACCEPTED ($code)" ;;
	*)           bad "forged assertion -> unexpected $code" ;;
esac

# ------------------------------------------------------------ input validation

head1 "5. Input validation"

# Aimed at /verify, not /challenge: handleChallenge deliberately never reads the
# request body -- it just issues a challenge -- so malformed JSON there is a 200
# by design, not a bug. /verify does decode a body.
code="$(status POST /api/v1/attest/verify '{"key_id":')"
case "$code" in
	400|422) ok "malformed JSON to /attest/verify -> $code" ;;
	5*)      bad "malformed JSON -> $code; a truncated body must not 500" ;;
	*)       bad "malformed JSON -> $code, want 400/422" ;;
esac

# A body that parses but omits required fields must also be refused.
code="$(status POST /api/v1/attest/verify '{}')"
case "$code" in
	400|401|422) ok "an empty verify body -> $code" ;;
	2*)          bad "an empty verify body was ACCEPTED ($code)" ;;
	*)           bad "an empty verify body -> unexpected $code" ;;
esac

code="$(status GET "/api/v1/numbers/not-a-phone-number")"
case "$code" in
	400|401|404|422) ok "an invalid E.164 path parameter -> $code (not a 500)" ;;
	5*)              bad "an invalid E.164 path parameter -> $code; malformed input must not 500" ;;
	*)               bad "an invalid E.164 path parameter -> unexpected $code" ;;
esac

# ------------------------------------------------ optional authenticated admin

if [ -n "$ADMIN_TOKEN" ]; then
	head1 "6. Authenticated admin round-trip"

	# +1 202 555 01xx is the reserved fictional range -- safe to touch.
	num="+12025550199"
	resp="$("${CURL[@]}" -X POST "$BASE/api/v1/admin/overrides" \
		-H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
		-d "{\"number\":\"$num\",\"mode\":\"block\",\"reason\":\"smoke-prod\",\"admin\":\"smoke\"}" 2>/dev/null)"
	if printf '%s' "$resp" | grep -q 'overridden_block'; then
		ok "admin block override applied -> overridden_block"
	else
		bad "admin block override: $(printf '%s' "$resp" | head -c 160)"
	fi

	resp="$("${CURL[@]}" -X POST "$BASE/api/v1/admin/overrides" \
		-H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
		-d "{\"number\":\"$num\",\"mode\":\"allow\",\"reason\":\"smoke-prod cleanup\",\"admin\":\"smoke\"}" 2>/dev/null)"
	if printf '%s' "$resp" | grep -q 'allowlisted'; then
		ok "flipping to allow -> allowlisted (tombstone path reachable)"
	else
		bad "admin allow override: $(printf '%s' "$resp" | head -c 160)"
	fi
else
	printf "\n  \033[33mSKIP\033[0m %s\n" "ADMIN_TOKEN unset; authenticated admin round-trip skipped"
fi

# ------------------------------------------------------------------- summary

printf "\n\033[1m%d passed, %d failed\033[0m\n" "$pass" "$fail"
if [ "$fail" -gt 0 ]; then
	echo "PRODUCTION SMOKE FAILED"
	exit 1
fi
echo "ALL PRODUCTION SMOKE CHECKS PASSED"
