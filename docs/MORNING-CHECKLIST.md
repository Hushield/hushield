# Morning Device Checklist

**Status 2026-07-30:** the backend is deployed, the app is on TestFlight, and real App Attest is
verified. What remains needs a **second phone line to call from**: confirming an actual incoming
call is blocked, and confirming SMS classification works in airplane mode. `+12025550143` is
already staged server-side as `overridden_block` for that test.

Sections 0–3 below describe the original *local* device session against a Mac-hosted server. They
are superseded for normal use — the TestFlight build talks to `https://api.hushield.com` directly —
but kept for local debugging without a deploy.

Identity/signing values used throughout (from `.planning/STATE.md`):

- Apple Team ID: `997DW79YCR`
- Bundle ID: `com.brahy.hushield`
- App Attest `APP_ID`: `997DW79YCR.com.brahy.hushield`
- Backend: **live at `https://api.hushield.com`** in `ATTEST_MODE=apple`

---

## Step A — ✅ DONE (2026-07-28). Four web-UI actions that blocked the TestFlight archive

> All four were completed and the archive now builds. `scripts/testflight-release.sh`
> automates the whole pipeline from here: preflight → archive → export → upload, and
> it sets export compliance and attaches the build to the internal TestFlight group.
> Kept below for the record and for anyone reproducing the setup on a fresh account.

These four could not be scripted: the App Store Connect API either does not expose the resource or
forbids creating it. All are now complete.

Already done via the API, so skip these: bundle IDs `com.brahy.hushield` (`PBA3MXGGU7`),
`com.brahy.hushield.CallDirectory` (`ZHW6JNSNP8`), `com.brahy.hushield.MessageFilter`
(`W27J8UFYYJ`), plus the App Groups and Push Notifications capabilities.

### A1. Create the App Group — [Developer Portal ▸ Identifiers ▸ App Groups](https://developer.apple.com/account/resources/identifiers/list/applicationGroup)

Create `group.com.brahy.hushield`, description "HuShield Shared Group".

There is no public API for App Groups (`/v1/appGroups` returns 404). Without this the profile cannot
carry the entitlement, and all three targets fail with *"doesn't support the group.com.brahy.hushield
App Group"*.

### A2. Enable App Attest on the app identifier — [Developer Portal ▸ Identifiers](https://developer.apple.com/account/resources/identifiers/list)

Open `com.brahy.hushield` and tick **App Attest**. While there, confirm **App Groups** is ticked and
associated with `group.com.brahy.hushield`.

App Attest is *not* in the API's `capabilityType` enum — the valid values are ICLOUD,
IN_APP_PURCHASE, GAME_CENTER, PUSH_NOTIFICATIONS, WALLET, APP_GROUPS, and ~20 others, none of which
is App Attest. The archive fails with *"Provisioning profile doesn't include the App Attest
capability"* until this is ticked by hand.

### A3. Associate the App Group with both extensions — same screen

Open `com.brahy.hushield.CallDirectory` and `com.brahy.hushield.MessageFilter` and associate each
with `group.com.brahy.hushield`. Both extensions read the synced blocklist through the shared
container, so both need it.

### A4. Create the app record — [App Store Connect ▸ Apps ▸ +](https://appstoreconnect.apple.com/apps)

- Platform: iOS · Name: **HuShield** · Primary language: English (U.S.)
- Bundle ID: `com.brahy.hushield` · SKU: `HUSHIELD-IOS-001`

The API refuses this outright: `POST /v1/apps` returns
`403 The resource 'apps' does not allow 'CREATE'`. Note the App Store name must be globally unique —
the listing was created as **"Hushield"** (lowercase s), which differs from the brand and the
in-app display name "HuShield". Editable in App Store Connect until first App Review submission.

### Then the archive should build

```sh
cd ios && xcodegen generate
xcodebuild archive \
  -project SpamFilter.xcodeproj -scheme SpamFilter -configuration Release \
  -destination 'generic/platform=iOS' -archivePath build/HuShield.xcarchive \
  -allowProvisioningUpdates
```

Run it from Xcode (Product ▸ Archive) if the CLI still reports *"Authentication failed: Make sure a
bearer token was provided"* — that is xcodebuild failing to use the ASC key for provisioning, and the
GUI path uses your signed-in account instead.

Upload with Xcode ▸ Organizer ▸ Distribute App ▸ TestFlight, or:

```sh
xcodebuild -exportArchive -archivePath build/HuShield.xcarchive \
  -exportPath build/export -exportOptionsPlist ExportOptions.plist
# ASC_KEY_ID and ASC_ISSUER_ID are kept out of this repo on purpose -- it is
# public, and they are two of the three parts of ASC API auth. Source them from
# a gitignored file, e.g. ~/.config/hushield/asc.env
source ~/.config/hushield/asc.env
xcrun altool --upload-app -f build/export/HuShield.ipa -t ios \
  --apiKey "$ASC_KEY_ID" --apiIssuer "$ASC_ISSUER_ID"
```

Export compliance: the app uses only standard HTTPS/TLS, no custom or proprietary cryptography.

**Internal TestFlight testing needs no App Review**, so the build is installable within minutes of
processing. External testing would add a review cycle.

---

## 0. Start the local backend

From the repo root (`SpamFilter/`):

```sh
# Create the dev database (idempotent).
mysql -h 127.0.0.1 -u root -e 'CREATE DATABASE IF NOT EXISTS spamfilter_dev'

# Run the server in mock attest mode (migrations auto-run on startup).
ATTEST_MODE=mock \
DB_DSN='root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true' \
ADMIN_TOKEN=changeme \
ADDR=':8080' \
go run ./cmd/server
```

Leave this running in its own terminal/tab for the rest of the session.

## 1. Plug in the iPhone and resolve signing

1. Connect the iPhone via cable (or same-network Wi-Fi debugging if already paired) and unlock it.
2. Open `ios/SpamFilter.xcodeproj` in Xcode (or run `cd ios && xcodegen generate` first if you
   edited `project.yml`, e.g. for step 2 below).
3. Select the device as the run destination for the `SpamFilter` scheme.
4. Confirm Signing & Capabilities resolves automatically (`CODE_SIGN_STYLE: Automatic`,
   `DEVELOPMENT_TEAM: 997DW79YCR`) for all three targets that ship to device:
   - `SpamFilter` (`com.brahy.hushield`)
   - `CallDirectoryExtension` (`com.brahy.hushield.CallDirectory`)
   - `MessageFilterExtension` (`com.brahy.hushield.MessageFilter`)
   Fix any "no signing certificate"/"no matching provisioning profile" errors in Xcode before
   continuing (should auto-resolve with the paid account already on this Team ID).

## 2. Point the app at the Mac's LAN IP

The Simulator can reach `localhost:8080` directly, but a physical device on the same Wi-Fi
network needs the Mac's LAN IP instead.

```sh
# Find the Mac's LAN IP (adjust en0 -> en1 if you're on Ethernet/a different interface).
ipconfig getifaddr en0
```

Edit `ios/project.yml`, under the `SpamFilter` target's `settings.base`:

```yaml
SPAMFILTER_API_BASE_URL: http://<mac-lan-ip>:8080
```

Then regenerate the Xcode project so the change takes effect:

```sh
cd ios && xcodegen generate
```

(`ADDR=:8080` in step 0 already binds on all interfaces, not just localhost, so no server-side
change is needed — just make sure macOS's firewall isn't blocking incoming connections on 8080.)

Revert this line back to `http://localhost:8080` afterward if you don't want it committed —
it's a real change to a tracked file, not a local override.

## 3. Build and run on device

Build/run the `SpamFilter` scheme onto the device from Xcode (⌘R), or:

```sh
cd ios && xcodegen generate && xcodebuild \
  -project SpamFilter.xcodeproj -scheme SpamFilter \
  -destination 'generic/platform=iOS' \
  -allowProvisioningUpdates
```

then install/launch via Xcode's device run (device destinations need a live Xcode session to
install, not just a build). On device, the app's `AppEnvironment` now activates the real
`DeviceAttestationProvider` instead of `SimulatorAttestationProvider` — this is the thing that
was previously untestable off-device.

Use the app's Setup tab to enroll. This exercises the real App Attest key generation +
attestation flow for the first time.

## 4. Validate real App Attest (`ATTEST_MODE=apple`)

Stop the mock-mode server from step 0, then restart it in `apple` mode. `Load()` hard-fails apple
mode unless `APP_ID`, a non-default `DEVICE_TOKEN_SECRET`, and `ADMIN_TOKEN` are all set:

```sh
ATTEST_MODE=apple \
APP_ID=997DW79YCR.com.brahy.hushield \
DEVICE_TOKEN_SECRET=$(openssl rand -hex 32) \
ADMIN_TOKEN=$(openssl rand -hex 32) \
DB_DSN='root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true' \
ADDR=':8080' \
go run ./cmd/server
```

Re-run Setup/enroll in the app against this server. A successful enroll + verify here is the
one thing that could not be exercised until now — it's the actual completion of Phase 4
criterion 1 (real Apple App Attest end-to-end against a genuine physical-device attestation).

## 5. Enable the two extensions in iOS Settings

Neither extension activates just by installing the app — both need an explicit opt-in:

- **Settings → Phone → Call Blocking & Identification** → enable **SpamFilter**.
- **Settings → Messages → Unknown & Spam** (wording may vary slightly by iOS version) →
  select **SpamFilter** as the message filter.

## 6. Seed a test number and verify call/SMS filtering

Seed a number as blocked via the admin override endpoint (works against either server instance
from steps 0/4 — use whichever is currently running and its matching `ADMIN_TOKEN`):

```sh
curl -sf -X POST http://<mac-lan-ip-or-localhost>:8080/api/v1/admin/overrides \
  -H "Authorization: Bearer <ADMIN_TOKEN>" \
  -H 'Content-Type: application/json' \
  -d '{"number":"+14155551234","mode":"block"}'
```

Then, in the app, trigger a blocklist sync (Status tab) so the Call Directory / SMS filter
extensions pick up the new entry. Place a test call and send a test SMS from `+14155551234`
(e.g. via a second device, a VoIP test number, or a carrier test line) and confirm:

- The call is identified/blocked by Call Blocking & Identification.
- The SMS is filtered into the Unknown & Spam / Junk tab in Messages.

## 7. Run the integration test suite (note the `TEST_RUNNER_` gotcha)

The env-gated `IntegrationTests` in `SpamFilterKitTests` drive the real backend over HTTP but
are skipped unless `SPAMFILTER_INTEGRATION_BASE_URL` reaches the Simulator's test process. Full
details in `ios/SpamFilterKitTests/INTEGRATION.md`; the short version:

```sh
# Server must be running (mock mode is fine for this).
cd ios
xcodegen generate
# IMPORTANT: xcodebuild only forwards env vars to the Simulator test host when
# they're `export`ed under a TEST_RUNNER_ prefix -- a plain prefix on the
# command line does NOT reach the test binary.
export TEST_RUNNER_SPAMFILTER_INTEGRATION_BASE_URL=http://localhost:8080
xcodebuild \
  -project SpamFilter.xcodeproj -scheme SpamFilter -sdk iphonesimulator \
  -destination 'id=<simulator UDID, from `xcrun simctl list devices available`>' \
  test -only-testing:SpamFilterKitTests/IntegrationTests \
  CODE_SIGNING_ALLOWED=NO
unset TEST_RUNNER_SPAMFILTER_INTEGRATION_BASE_URL
```

## 8. Run the automated test gate before any push/deploy

```sh
make test     # go test ./... -race -cover  +  iOS xcodebuild test on an available Simulator
make deploy   # runs `make test` first (hard prerequisite), then scripts/deploy.sh
make hooks    # one-time: installs the pre-push gate (make test) via core.hooksPath
```

## 9. Go-live: choosing a deploy host (not yet done)

`scripts/deploy.sh` builds and tags the `spamfilter-server` Docker image locally, then no-ops
(exit 0) until `DEPLOY_TARGET` is set to `registry`, `ssh`, or `paas` — each of those blocks is a
TODO stub in the script, not implemented. Before this is usable:

1. Pick a host (a plain box over SSH, a container registry + orchestrator, or a PaaS like
   Fly/Railway/Render).
2. Fill in the matching block in `scripts/deploy.sh`.
3. Provide production secrets (`DEVICE_TOKEN_SECRET`, `ADMIN_TOKEN`, DB `DSN`, optionally
   `APNS_*`) per `.env.example` — do not reuse the dev/test values from steps 0/4 above.
4. Run `make deploy`.

**Known-unverified item:** the `docker build` in `scripts/deploy.sh`/`Dockerfile` has never
actually been run — there was no Docker daemon available in the environment that authored it.
Treat the image build itself as unverified until it's run once for real (`docker build -t
spamfilter-server:latest .` from the repo root) as part of picking a host.
