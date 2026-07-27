# Morning Device Checklist

Everything below this line requires a physical iPhone, a chosen deploy host, or both — it's
the only part of Phase 3/4 that couldn't be finished in the dev environment. Budget ~20–30 min
for the device session (steps 1–7); the deploy section (step 8) is a separate, longer task once
a host is chosen.

Identity/signing values used throughout (from `.planning/STATE.md`):

- Apple Team ID: `997DW79YCR`
- Bundle ID: `com.brahy.hushield`
- App Attest `APP_ID`: `997DW79YCR.com.brahy.hushield`

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
