#!/usr/bin/env bash
#
# testflight-release.sh -- take HuShield from source to a TestFlight build.
#
# Four setup steps in this pipeline CANNOT be automated, because the App Store
# Connect API either does not expose the resource or forbids creating it:
#
#   * App Groups          -- no public API at all (/v1/appGroups returns 404)
#   * App Attest          -- absent from the API's capabilityType enum
#   * App Group binding   -- same missing API
#   * The app record      -- POST /v1/apps returns 403 "does not allow CREATE"
#
# So this script does the next best thing: it verifies each of those was done,
# names precisely which one is missing when the build fails, and then runs the
# whole archive -> export -> upload chain unattended once they are all in place.
#
# It is safe to run repeatedly. Run it, fix whatever it names, run it again.
#
# Usage:
#   ./scripts/testflight-release.sh              # preflight, archive, export, upload
#   ./scripts/testflight-release.sh --check      # preflight only, change nothing
#   ./scripts/testflight-release.sh --no-upload  # build and export, skip the upload
#
# Environment overrides:
# Required environment (App Store Connect API credentials):
#   ASC_KEY_ID     App Store Connect key id
#   ASC_ISSUER_ID  App Store Connect issuer id
#   ASC_KEY_PATH   path to the .p8 (optional; auto-discovered from the usual
#                  locations using ASC_KEY_ID)
#
# These are deliberately NOT defaulted in this file. They are two of the three
# components of ASC API authentication, and this repository is public -- the
# third component (the .p8 private key) is all that would be missing. Keep them
# in a gitignored local file and source it, e.g.:
#
#   # ~/.config/hushield/asc.env   (chmod 600, outside the repo)
#   export ASC_KEY_ID=XXXXXXXXXX
#   export ASC_ISSUER_ID=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
#
#   source ~/.config/hushield/asc.env && ./scripts/testflight-release.sh

set -euo pipefail

# ---------------------------------------------------------------- configuration

TEAM_ID="997DW79YCR"
APP_BUNDLE_ID="com.brahy.hushield"
EXT_BUNDLE_IDS=("com.brahy.hushield.CallDirectory" "com.brahy.hushield.MessageFilter")
APP_GROUP="group.com.brahy.hushield"
APP_NAME="HuShield"
EXPECTED_API_BASE="https://api.hushield.com"

ASC_KEY_ID="${ASC_KEY_ID:-}"
ASC_ISSUER_ID="${ASC_ISSUER_ID:-}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IOS_DIR="$REPO_ROOT/ios"
BUILD_DIR="$IOS_DIR/build"
ARCHIVE="$BUILD_DIR/HuShield.xcarchive"
EXPORT_DIR="$BUILD_DIR/export"

MODE="full"
case "${1:-}" in
	--check)     MODE="check" ;;
	--no-upload) MODE="no-upload" ;;
	"")          ;;
	*) echo "Unknown option: $1" >&2; exit 2 ;;
esac

# ------------------------------------------------------------------- output help

bold=$'\033[1m'; red=$'\033[31m'; grn=$'\033[32m'; ylw=$'\033[33m'; rst=$'\033[0m'
ok()   { printf "  ${grn}✓${rst} %s\n" "$*"; }
bad()  { printf "  ${red}✗${rst} %s\n" "$*"; }
warn() { printf "  ${ylw}!${rst} %s\n" "$*"; }
head1() { printf "\n${bold}%s${rst}\n" "$*"; }
die()  { printf "\n${red}%s${rst}\n" "$*" >&2; exit 1; }

# Fail early and usefully rather than surfacing a confusing auth error later.
# These are intentionally not stored in the repo: it is public, and they are two
# of the three parts of App Store Connect API authentication.
if [ -z "$ASC_KEY_ID" ] || [ -z "$ASC_ISSUER_ID" ]; then
	die "ASC_KEY_ID and ASC_ISSUER_ID must be set.

They are deliberately not committed -- this repository is public, and these are
two of the three components of App Store Connect API auth. Keep them in a
gitignored file outside the repo and source it:

  # ~/.config/hushield/asc.env   (chmod 600)
  export ASC_KEY_ID=XXXXXXXXXX
  export ASC_ISSUER_ID=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx

  source ~/.config/hushield/asc.env && ./scripts/testflight-release.sh

Both are at https://appstoreconnect.apple.com/access/integrations/api"
fi

PROBLEMS=()
problem() { PROBLEMS+=("$1"); bad "$1"; }

# ------------------------------------------------------- App Store Connect key

find_asc_key() {
	if [ -n "${ASC_KEY_PATH:-}" ]; then
		[ -f "$ASC_KEY_PATH" ] && { echo "$ASC_KEY_PATH"; return; }
		die "ASC_KEY_PATH is set but $ASC_KEY_PATH does not exist."
	fi
	for d in "$HOME/.appstoreconnect/private_keys" "$HOME/.private_keys" \
	         "$HOME/private_keys" "$HOME/Downloads"; do
		if [ -f "$d/AuthKey_${ASC_KEY_ID}.p8" ]; then
			echo "$d/AuthKey_${ASC_KEY_ID}.p8"; return
		fi
	done
	echo ""
}

# asc GET <path> -- prints the JSON body, or nothing on failure.
asc() {
	local path="$1"
	python3 - "$ASC_KEY_ID" "$ASC_ISSUER_ID" "$ASC_KEY_PATH_RESOLVED" "$path" <<'PY' 2>/dev/null
import json, sys, time, urllib.request, urllib.error
try:
    import jwt
except ImportError:
    sys.exit(3)
key_id, issuer, key_path, path = sys.argv[1:5]
with open(key_path) as f:
    key = f.read()
now = int(time.time())
tok = jwt.encode({"iss": issuer, "iat": now, "exp": now + 600,
                  "aud": "appstoreconnect-v1"},
                 key, algorithm="ES256", headers={"kid": key_id, "typ": "JWT"})
req = urllib.request.Request("https://api.appstoreconnect.apple.com" + path)
req.add_header("Authorization", "Bearer " + tok)
try:
    with urllib.request.urlopen(req, timeout=30) as r:
        print(r.read().decode())
except urllib.error.HTTPError as e:
    sys.exit(1)
PY
}

# ------------------------------------------------------------------- preflight

head1 "1. Local toolchain"

command -v xcodebuild >/dev/null || die "xcodebuild not found. Install Xcode."
ok "xcodebuild $(xcodebuild -version | head -1 | awk '{print $2}')"

if command -v xcodegen >/dev/null; then
	ok "xcodegen present"
else
	problem "xcodegen not installed. Run: brew install xcodegen"
fi

if security find-identity -v -p codesigning 2>/dev/null | grep -q "Apple Distribution: .*($TEAM_ID)"; then
	ok "Apple Distribution certificate for team $TEAM_ID"
else
	problem "No 'Apple Distribution' certificate for team $TEAM_ID in the keychain.
      Xcode ▸ Settings ▸ Accounts ▸ Manage Certificates ▸ + ▸ Apple Distribution"
fi

# A distribution build cannot be produced without an Apple ID in Xcode; the CLI
# cannot add one (it needs a password and 2FA).
if [ -d "$HOME/Library/Developer/Xcode/UserData" ] && \
   security find-generic-password -l "Xcode-Token" >/dev/null 2>&1; then
	ok "Xcode appears to have a signed-in Apple ID"
else
	warn "Could not confirm an Apple ID is signed into Xcode."
	warn "If archiving fails with 'No Account for Team', add it:"
	warn "  Xcode ▸ Settings ▸ Accounts ▸ +"
fi

head1 "2. App Store Connect credentials"

ASC_KEY_PATH_RESOLVED="$(find_asc_key)"
if [ -z "$ASC_KEY_PATH_RESOLVED" ]; then
	problem "App Store Connect key AuthKey_${ASC_KEY_ID}.p8 not found.
      Looked in ~/.appstoreconnect/private_keys, ~/.private_keys, ~/private_keys, ~/Downloads.
      Set ASC_KEY_PATH=/path/to/AuthKey_XXXX.p8 to override."
	ASC_AVAILABLE=0
else
	ok "API key: $(basename "$ASC_KEY_PATH_RESOLVED")"
	case "$ASC_KEY_PATH_RESOLVED" in
		"$HOME/Downloads"/*)
			warn "That key lives in ~/Downloads. Consider moving it somewhere durable:"
			warn "  mkdir -p ~/.appstoreconnect/private_keys && mv '$ASC_KEY_PATH_RESOLVED' ~/.appstoreconnect/private_keys/"
			;;
	esac
	if python3 -c "import jwt" 2>/dev/null; then
		ASC_AVAILABLE=1
	else
		warn "PyJWT is not installed, so API checks will be skipped."
		warn "  pip3 install pyjwt cryptography"
		ASC_AVAILABLE=0
	fi
fi

head1 "3. Registered identifiers (created via API — should already pass)"

if [ "${ASC_AVAILABLE:-0}" = "1" ]; then
	for bid in "$APP_BUNDLE_ID" "${EXT_BUNDLE_IDS[@]}"; do
		body="$(asc "/v1/bundleIds?filter%5Bidentifier%5D=${bid}&limit=1" || true)"
		if [ -n "$body" ] && printf '%s' "$body" | grep -q '"identifier"'; then
			ok "$bid"
		else
			problem "Bundle ID $bid is not registered.
      Developer Portal ▸ Identifiers ▸ + ▸ App IDs"
		fi
	done
else
	warn "Skipped (no working API access)."
fi

head1 "4. App Store Connect app record  ${bold}[MANUAL — needed only for upload]${rst}"

APP_RECORD_OK=0
if [ "${ASC_AVAILABLE:-0}" = "1" ]; then
	body="$(asc "/v1/apps?filter%5BbundleId%5D=${APP_BUNDLE_ID}&limit=1" || true)"
	if [ -n "$body" ] && printf '%s' "$body" | grep -q '"id"' && \
	   ! printf '%s' "$body" | grep -q '"data" *: *\[\]'; then
		app_id="$(printf '%s' "$body" | python3 -c "import json,sys; d=json.load(sys.stdin)['data']; print(d[0]['id'] if d else '')" 2>/dev/null || echo "")"
		if [ -n "$app_id" ]; then
			ok "App record exists (ASC id $app_id)"
			APP_RECORD_OK=1
		fi
	fi
	if [ "$APP_RECORD_OK" = "0" ]; then
		# Deliberately a warning, not a blocker. The app record is required to
		# UPLOAD, not to archive -- so the build can proceed and prove the
		# signing, App Group, and App Attest setup while this is still missing.
		# The upload step refuses to run without it.
		warn "No App Store Connect app record for $APP_BUNDLE_ID yet."
		warn "Not a blocker for archiving -- only for the upload at the end."
		warn "POST /v1/apps returns 403 'does not allow CREATE', so create it by hand:"
		warn "  https://appstoreconnect.apple.com/apps  ▸  +  ▸  New App"
		warn "  Platform: iOS · Name: $APP_NAME · Language: English (U.S.)"
		warn "  Bundle ID: $APP_BUNDLE_ID · SKU: HUSHIELD-IOS-001"
	fi
else
	warn "Skipped (no working API access)."
fi

head1 "5. Build settings"

cd "$IOS_DIR"
if [ "${SKIP_XCODEGEN:-0}" != "1" ] && command -v xcodegen >/dev/null; then
	xcodegen generate >/dev/null 2>&1 && ok "Regenerated SpamFilter.xcodeproj from project.yml"
fi

if [ -f SpamFilter.xcodeproj/project.pbxproj ]; then
	settings="$(xcodebuild -project SpamFilter.xcodeproj -target SpamFilter \
	            -configuration Release -showBuildSettings 2>/dev/null || true)"

	base="$(printf '%s' "$settings" | awk -F' = ' '/SPAMFILTER_API_BASE_URL/{print $2; exit}')"
	if [ "$base" = "$EXPECTED_API_BASE" ]; then
		ok "Release API base URL: $base"
	else
		problem "Release SPAMFILTER_API_BASE_URL is '${base:-unset}', expected $EXPECTED_API_BASE"
	fi

	ent="$(printf '%s' "$settings" | awk -F' = ' '/CODE_SIGN_ENTITLEMENTS/{print $2; exit}')"
	if [ -n "$ent" ] && grep -q "<string>production</string>" "$IOS_DIR/$ent" 2>/dev/null; then
		ok "Release entitlements use the PRODUCTION App Attest environment"
	else
		problem "Release entitlements ($ent) do not declare appattest-environment = production.
      TestFlight builds attest against Apple's production service; a 'development'
      value here produces attestations the server rejects, with nothing in the
      error pointing at the environment."
	fi
else
	problem "SpamFilter.xcodeproj missing and xcodegen did not run."
fi

head1 "6. App Store validation rules (checked here to avoid a slow upload failure)"

# altool rejects these server-side after the archive AND the upload have already
# run, which is a long round trip for a missing plist key. Check them up front.

APP_PLIST="$IOS_DIR/SpamFilter/Info.plist"
ICONSET="$IOS_DIR/SpamFilter/Assets.xcassets/AppIcon.appiconset"

if grep -q "CFBundleIconName" "$APP_PLIST" 2>/dev/null; then
	ok "CFBundleIconName present (upload error 90713)"
else
	problem "CFBundleIconName missing from SpamFilter/Info.plist.
      Required for any app built against the iOS 11+ SDK. Because this target
      sets GENERATE_INFOPLIST_FILE=NO, Xcode will NOT inject it automatically.
      Add:  <key>CFBundleIconName</key><string>AppIcon</string>"
fi

ICON_PNG="$(find "$ICONSET" -name '*.png' 2>/dev/null | head -1)"
if [ -z "$ICON_PNG" ]; then
	problem "No PNG in $ICONSET — the catalog declares an icon but ships none.
      Upload fails with error 90022 (missing 120x120 icon), because every size
      is derived from the 1024x1024 source."
else
	dims="$(sips -g pixelWidth -g pixelHeight "$ICON_PNG" 2>/dev/null | awk '/pixel/{print $2}' | paste -sd x -)"
	alpha="$(sips -g hasAlpha "$ICON_PNG" 2>/dev/null | awk '/hasAlpha/{print $2}')"
	if [ "$dims" = "1024x1024" ] && [ "$alpha" = "no" ]; then
		ok "App icon $(basename "$ICON_PNG") is 1024x1024 with no alpha"
	else
		problem "App icon must be exactly 1024x1024 with NO alpha channel.
      $(basename "$ICON_PNG") is ${dims:-unknown}, hasAlpha=${alpha:-unknown}.
      The App Store rejects icons with transparency."
	fi
	if ! grep -q '"filename"' "$ICONSET/Contents.json" 2>/dev/null; then
		problem "$ICONSET/Contents.json has no \"filename\" entry, so the PNG is
      present but not referenced. Xcode will build an empty icon set."
	fi
fi

for ext in CallDirectoryExtension MessageFilterExtension; do
	if grep -q "CFBundleDisplayName" "$IOS_DIR/$ext/Info.plist" 2>/dev/null; then
		ok "$ext has CFBundleDisplayName (upload error 90360)"
	else
		problem "CFBundleDisplayName missing from $ext/Info.plist.
      Required for app extensions; upload fails with error 90360. This is also
      the label shown next to the toggle in iOS Settings."
	fi
done

# ----------------------------------------------------------------- gate / report

if [ ${#PROBLEMS[@]} -gt 0 ]; then
	printf "\n${red}${bold}%d item(s) need attention before this can build.${rst}\n" "${#PROBLEMS[@]}"
	printf "Fix the items marked ${red}✗${rst} above, then run this script again.\n"
	exit 1
fi

printf "\n${grn}${bold}Preflight passed.${rst}\n"

if [ "$MODE" = "check" ]; then
	cat <<EOF

--check specified, stopping here. Two things this script cannot verify remotely,
because neither is exposed by the API -- the archive below is what proves them:

  * App Group '$APP_GROUP' exists and is associated with all three identifiers
  * App Attest is enabled on $APP_BUNDLE_ID

If the archive fails on either, the error is translated into a specific
instruction rather than left raw.
EOF
	exit 0
fi

# ---------------------------------------------------------------------- archive

head1 "7. Archiving for distribution"

mkdir -p "$BUILD_DIR"
rm -rf "$ARCHIVE"
ARCHIVE_LOG="$BUILD_DIR/archive.log"

set +e
xcodebuild archive \
	-project SpamFilter.xcodeproj \
	-scheme SpamFilter \
	-configuration Release \
	-destination 'generic/platform=iOS' \
	-archivePath "$ARCHIVE" \
	-allowProvisioningUpdates \
	> "$ARCHIVE_LOG" 2>&1
ARCHIVE_RC=$?
set -e

if [ $ARCHIVE_RC -ne 0 ]; then
	printf "\n${red}${bold}Archive failed.${rst} Translating the errors:\n\n"

	# Map each known signing failure to the exact portal action that fixes it.
	if grep -q "doesn't support the ${APP_GROUP} App Group\|doesn't include the com.apple.security.application-groups" "$ARCHIVE_LOG"; then
		cat <<EOF
${red}▸ The App Group is missing or not associated.${rst}
  There is no API for App Groups, so both steps are manual:

  1. Create it:
     https://developer.apple.com/account/resources/identifiers/list/applicationGroup
     Identifier: $APP_GROUP
     Description: HuShield Shared Group

  2. Associate it with ALL THREE identifiers (App Groups ▸ Configure on each):
     $APP_BUNDLE_ID
     ${EXT_BUNDLE_IDS[0]}
     ${EXT_BUNDLE_IDS[1]}

EOF
	fi

	if grep -q "doesn't include the App Attest capability\|appattest-environment" "$ARCHIVE_LOG"; then
		cat <<EOF
${red}▸ App Attest is not enabled on the app identifier.${rst}
  App Attest is absent from the API's capabilityType enum, so this is manual:

     https://developer.apple.com/account/resources/identifiers/list
     Open $APP_BUNDLE_ID ▸ tick "App Attest" ▸ Save

EOF
	fi

	if grep -q "No Account for Team\|Authentication failed: Make sure a bearer token" "$ARCHIVE_LOG"; then
		cat <<EOF
${ylw}▸ Command-line signing could not authenticate.${rst}
  This is a known limitation: xcodebuild often cannot use an ASC API key for
  provisioning even when the same key works against the REST API. Archive from
  the GUI instead, which uses your signed-in account:

     Xcode ▸ open ios/SpamFilter.xcodeproj ▸ Product ▸ Archive
     then Organizer ▸ Distribute App ▸ TestFlight

EOF
	fi

	printf "Full log: %s\n" "$ARCHIVE_LOG"
	printf "Last signing-related lines:\n\n"
	grep -E "error:|warning: .*sign" "$ARCHIVE_LOG" | tail -12 | sed 's/^/    /'
	exit 1
fi

ok "Archive created: $ARCHIVE"

# ----------------------------------------------------------------------- export

head1 "8. Exporting the IPA"

PLIST="$BUILD_DIR/ExportOptions.plist"
cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>method</key><string>app-store-connect</string>
	<key>teamID</key><string>$TEAM_ID</string>
	<key>uploadSymbols</key><true/>
	<key>manageAppVersionAndBuildNumber</key><true/>
	<key>destination</key><string>export</string>
</dict>
</plist>
EOF

rm -rf "$EXPORT_DIR"
set +e
xcodebuild -exportArchive \
	-archivePath "$ARCHIVE" \
	-exportPath "$EXPORT_DIR" \
	-exportOptionsPlist "$PLIST" \
	-allowProvisioningUpdates \
	> "$BUILD_DIR/export.log" 2>&1
EXPORT_RC=$?
set -e

if [ $EXPORT_RC -ne 0 ]; then
	# Older Xcode wants "app-store" rather than "app-store-connect".
	warn "Export failed; retrying with the legacy 'app-store' method."
	sed -i '' 's|<string>app-store-connect</string>|<string>app-store</string>|' "$PLIST"
	set +e
	xcodebuild -exportArchive -archivePath "$ARCHIVE" -exportPath "$EXPORT_DIR" \
		-exportOptionsPlist "$PLIST" -allowProvisioningUpdates \
		> "$BUILD_DIR/export.log" 2>&1
	EXPORT_RC=$?
	set -e
fi

[ $EXPORT_RC -eq 0 ] || { grep -E "error:" "$BUILD_DIR/export.log" | tail -10 | sed 's/^/    /'; die "Export failed. Log: $BUILD_DIR/export.log"; }

IPA="$(find "$EXPORT_DIR" -name '*.ipa' -maxdepth 1 | head -1)"
[ -n "$IPA" ] || die "Export reported success but produced no .ipa."
ok "Exported: $IPA ($(du -h "$IPA" | cut -f1))"

if [ "$MODE" = "no-upload" ]; then
	printf "\n--no-upload specified. Upload it yourself with:\n"
	printf "  xcrun altool --upload-app -f '%s' -t ios \\\\\n" "$IPA"
	printf "    --apiKey %s --apiIssuer %s\n" "$ASC_KEY_ID" "$ASC_ISSUER_ID"
	exit 0
fi

# ----------------------------------------------------------------------- upload

head1 "9. Uploading to App Store Connect"

if [ "$APP_RECORD_OK" != "1" ]; then
	die "Refusing to upload: no app record exists for $APP_BUNDLE_ID. Create it first (step 4)."
fi

# altool needs the key discoverable by ID, not by path.
mkdir -p "$HOME/.appstoreconnect/private_keys"
if [ ! -f "$HOME/.appstoreconnect/private_keys/AuthKey_${ASC_KEY_ID}.p8" ]; then
	cp "$ASC_KEY_PATH_RESOLVED" "$HOME/.appstoreconnect/private_keys/AuthKey_${ASC_KEY_ID}.p8"
	chmod 600 "$HOME/.appstoreconnect/private_keys/AuthKey_${ASC_KEY_ID}.p8"
	ok "Copied the key where altool can find it"
fi

set +e
xcrun altool --upload-app -f "$IPA" -t ios \
	--apiKey "$ASC_KEY_ID" --apiIssuer "$ASC_ISSUER_ID" \
	2>&1 | tee "$BUILD_DIR/upload.log"
UPLOAD_RC=${PIPESTATUS[0]}
set -e

if [ $UPLOAD_RC -ne 0 ]; then
	printf "\n"
	die "Upload failed. Log: $BUILD_DIR/upload.log
If it mentions authentication, upload from Xcode instead:
  Xcode ▸ Window ▸ Organizer ▸ Archives ▸ Distribute App ▸ TestFlight"
fi

cat <<EOF

${grn}${bold}Uploaded.${rst}

Apple now processes the build, usually 5-15 minutes. Then:

  1. https://appstoreconnect.apple.com  ▸  $APP_NAME  ▸  TestFlight
  2. Answer export compliance: the app uses only standard HTTPS/TLS,
     no proprietary or custom cryptography.
  3. Add yourself under Internal Testing. Internal testing needs NO App Review,
     so it becomes installable as soon as processing finishes.
  4. Install via the TestFlight app on your iPhone.

Then the device validation in docs/MORNING-CHECKLIST.md -- the part that finally
proves real App Attest against https://api.hushield.com, which has never once
been verified with a genuine device attestation.
EOF
