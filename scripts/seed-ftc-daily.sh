#!/usr/bin/env bash
#
# seed-ftc-daily.sh -- import the FTC's daily Do Not Call complaint CSV as
# seed reports, so the blocklist carries publicly-reported spam numbers
# between community reports.
#
# Invoked by hushield-seed-ftc.timer on the deploy host. Safe to run by hand.
#
# WHY A LOOKBACK WINDOW, NOT JUST TODAY:
# The FTC publishes one file per weekday, named by publication date, by about
# noon Eastern. There is no file for weekends or federal holidays, and dates
# here are UTC, so "today" is routinely a date that does not exist yet. The
# window finds the newest file that actually exists instead of guessing.
#
# It must be wide enough that a legitimately quiet stretch never looks like a
# failure: a Sunday-evening UTC run with a 3-day window sees only Mon/Sun/Sat
# and finds nothing, and a Monday federal holiday pushes that further. Seven
# days always spans a published weekday, even across Thanksgiving or Christmas.
#
# Only the newest file is imported, not every file in the window. Importing is
# idempotent, so re-importing would be harmless, but each run of the seeder
# ends in a full RecomputeAllNumbers -- so N files means N recomputes for no
# benefit. The cost of this choice is that a run missed entirely (host down)
# never backfills that day's numbers. At roughly 10k numbers a day against a
# ~350k steady state, that gap is about 3% and decays out on its own.
#
# WHY THE USER AGENT:
# www.ftc.gov answers curl's default agent with HTTP 403. A browser agent is
# required to retrieve the file at all.
#
# WHY TRUST 3.0:
# score = BaseWeight(1.0) * trust * decay * categoryMultiplier. At trust 3.0
# with category robocall (1.5) a fresh seeded number scores 4.5: above
# SuspectThreshold (2.0) so it is LABELLED, below BlockThreshold (5.0) so it is
# never auto-blocked. With a 30-day half-life it stays above the suspect line
# for roughly 35 days. That is deliberate: the FTC states plainly that none of
# the reported-call data is verified, and a single unverified complaint -- on a
# caller ID that may itself be spoofed -- must not block a real call.
#
# Environment:
#   SEED_BIN        path to the seed binary   (default /opt/hushield/bin/hushield-seed)
#   SEED_TRUST      seed device trust_weight  (default 3.0)
#   SEED_CATEGORY   scoring category          (default robocall)
#   LOOKBACK_DAYS   days back to search       (default 7)

set -euo pipefail

SEED_BIN="${SEED_BIN:-/opt/hushield/bin/hushield-seed}"
SEED_TRUST="${SEED_TRUST:-3.0}"
SEED_CATEGORY="${SEED_CATEGORY:-robocall}"
LOOKBACK_DAYS="${LOOKBACK_DAYS:-7}"

BASE_URL="https://www.ftc.gov/sites/default/files"
UA="Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36"

die() { echo "ERROR: $*" >&2; exit 1; }

[ -x "$SEED_BIN" ] || die "seed binary not found or not executable: $SEED_BIN"
command -v curl >/dev/null || die "curl is required"

# GNU date on the deploy host; BSD date when run by hand on macOS.
days_ago() {
	if date -u -d "$1 days ago" +%F >/dev/null 2>&1; then
		date -u -d "$1 days ago" +%F
	else
		date -u -v-"$1"d +%F
	fi
}

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

found=0
for i in $(seq 0 "$((LOOKBACK_DAYS - 1))"); do
	day="$(days_ago "$i")"
	file="DNC_Complaint_Numbers_${day}.csv"
	dest="${WORKDIR}/${file}"

	status="$(curl -sS -L --max-time 120 -A "$UA" -o "$dest" \
		-w '%{http_code}' "${BASE_URL}/${file}" || echo 000)"

	if [ "$status" != "200" ]; then
		echo "    ${day}: not published (HTTP ${status})"
		rm -f "$dest"
		continue
	fi

	# A 200 that is not a CSV means the URL pattern moved and we are staring at
	# an HTML error page. Importing that would silently skip every row.
	if ! head -1 "$dest" | grep -q 'Company_Phone_Number'; then
		die "${file} downloaded but its header lacks Company_Phone_Number -- the FTC export format changed, check scripts/seed-ftc-daily.sh"
	fi

	rows="$(( $(wc -l < "$dest") - 1 ))"
	echo "==> ${day}: ${rows} rows, importing at trust ${SEED_TRUST}"
	"$SEED_BIN" -source ftc -file "$dest" -trust "$SEED_TRUST" -category "$SEED_CATEGORY"

	found=$((found + 1))
	rm -f "$dest"
	break
done

# Zero files across a seven-day window is not a quiet weekend -- that span
# always contains a published weekday. It means the URL pattern changed or the
# host has no egress to ftc.gov, and it must fail loudly rather than look like
# a successful no-op run forever.
[ "$found" -gt 0 ] || die "no FTC CSV found in the last ${LOOKBACK_DAYS} days -- URL pattern changed, or no network egress to www.ftc.gov"
