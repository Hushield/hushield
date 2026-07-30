# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-30)

**Core value:** Community-driven, privacy-first (App Attest, no PII) spam call/text filtering for iOS.
**Current focus:** Everything is deployed and live. Remaining work is two on-device
observations that need a second phone line to call from.

## Current Position

Phase: **All four phases deployed and live.** Remaining work is two on-device
  observations that need a second phone line, plus optional hardening.
Status: Backend live at **https://api.hushield.com** (`ATTEST_MODE=apple`, MySQL 8.4,
  nginx + Let's Encrypt, systemd, recompute on a 15-min timer). Marketing site live at
  **https://hushield.com**. Code public at **github.com/Hushield/hushield** (Apache-2.0,
  10 open issues, v0.1.0). iOS build 2 on TestFlight, `IN_BETA_TESTING`.
Last activity: 2026-07-30 — verified the report → score → blocklist → tombstone state
  machine against production; promoted REQ-10/11/12; shipped build 2 with three fixes
  found during device testing.

Progress: [██████████] ~97%

**Verified against production on real hardware:**
- Real Apple App Attest (Apple-issued 3,975-byte receipt stored; fabricated attestations 401)
- Report submission from device (201, caller name stored)
- Blocklist delta sync + CallKit reload
- Admin override → `overridden_block` → survives recompute
- Unblock tombstone: `allowlisted` while retaining `was_blockable=1`

**Deliberately NOT claimed — needs a second phone line to call from:**
- An actual incoming call being blocked (REQ-08)
- SMS classification in airplane mode (REQ-09)
- `+12025550143` is staged as `overridden_block` and ready for that test.

**Optional / deferred:**
- APNs `.p8` absent, so silent push is a documented no-op (REQ-07)
- Website runs with no `GITHUB_TOKEN` (60 req/hr unauthenticated; ETag 304s are free,
  so it works, but shows the "stale" notice under load). Needs a fine-grained
  public-repo read-only PAT in `/opt/hushield-web/.env`.
- `core.hooksPath` was removed to unblock git; `make hooks` restores the pre-push gate
  but re-trips the tooling's dangerous-config guard.
- App Store listing name is "Hushield"; the brand and in-app display name are "HuShield".
  Editable until first App Review submission.

## Accumulated Context

### Decisions

- Two-spec decomposition: community backend first, iOS app second (backend is the client's prerequisite).
- Go + MySQL backend; native Swift client (Call Directory + SMS Filter are native app extensions).
- App Attest is the sole identity — no accounts, no PII anywhere.
- Trust-weighted, time-decaying scoring instead of a raw blocklist.
- Spec 2a (assertion refresh, tombstones, lookup, APNs) added to the backend *before* the app, per an anti-drift audit.

### Completed Work (predates this PhaseFlow scaffold — no per-plan PLAN.md files)

- **Phase 1 — Community Backend:** merged to `main` 2026-07-19. App Attest challenge/verify, weighted decaying scoring with per-device trust, POST /reports, GET /blocklist delta (keyset pagination, neighbor-spoof, crowd caller-ID), admin overrides, FTC/FCC seeding, recompute cron.
- **Phase 2 — Backend for the iOS Client:** merged to `main` 2026-07-21 (HEAD `814e23e`). `POST /api/v1/attest/assert` token refresh (migration 0003 `sign_count`, replay guard), blocklist removal tombstones `action:"unblock"` (migration 0004 `was_blockable`), `GET /api/v1/numbers/{e164}` (lookup), APNs silent push (migration 0005 push cols, `POST /api/v1/devices/push-token`, `internal/push`), true-delta recompute fix. Migrations now 0001–0005.
- **Phase 3 — iOS App: SUBSTANTIALLY COMPLETE**, merged to `spam-ios-phase3`. `SpamFilterKit` framework: `APIClient` matching the exact backend wire contract, App Attest enrollment/refresh with both a real `DeviceAttestationProvider` and a `SimulatorAttestationProvider` stub, blocklist delta sync with tombstone handling, phone E.164↔Int64 conversion, keychain-backed token store, Call Directory + SMS Filter extensions (thin OS shims wrapping tested pure logic). Polished SwiftUI app (Report / Lookup / Status / Setup tabs, design system, view-models). Test coverage: 100 `SpamFilterKitTests` unit tests, `SpamFilterAppTests` view-model tests, an env-gated `IntegrationTests` suite that drives a real local backend, and an `SpamFilterUITests` XCUITest flow. Signed and building green for iOS Simulator with Team `997DW79YCR` / bundle `com.brahy.hushield`. **COMPLETED 2026-07-28:** shipped to TestFlight (build 2), installed on a physical iPhone, both extensions enabled under iOS Settings, real App Attest verified.
- **Phase 4 — Production Hardening: SUBSTANTIALLY COMPLETE**, merged to `spam-ios-phase3`. Redis-backed `ChallengeStore` (config-selectable via `CHALLENGE_STORE`/`REDIS_URL`, default `memory`), `cmd/recompute -interval` continuous mode plus `launchd`/`cron` scheduler artifacts under `scripts/`, hard-fail prod config validation (`Load()` refuses to start with `ATTEST_MODE=apple` and a missing `APP_ID`/insecure `DEVICE_TOKEN_SECRET`/empty `ADMIN_TOKEN`), `.env.example` documenting every server env var, a containerized deploy scaffold (`Dockerfile`, `docker-compose.yml`, `scripts/deploy.sh`), and a local `make`-based test gate (`make test` = Go + iOS, `make deploy` depends on `make test`, `make hooks` installs a pre-push gate). Go coverage raised 76.7% → 86.7%. **COMPLETED 2026-07-27/28:** deployed to `pm-prod-spamfilter` (10.30.1.244, arm64) at `https://api.hushield.com` under systemd behind nginx with a Let's Encrypt cert; real `ATTEST_MODE=apple` verified. `scripts/deploy.sh` was rewritten from a no-op into a working scp + systemctl deploy. The **Docker path was abandoned** — the image was never built, sets no `GOARCH`, and `docker-compose.yml` is dev-only with an empty MySQL root password; tracked as issue #1 to fix or delete.

Repo health 2026-07-30: `go build ./...` clean; full iOS suite green (112 kit + 16 app
+ 6 UI); Go coverage 86.7% with `internal/config` at 100%.

### Blockers/Concerns

- ~~Phase 3 needs Team ID + bundle ID before it can start~~ — **resolved 2026-07-23** (Team `997DW79YCR`, bundle `com.brahy.hushield`). Optional APNs `.p8` still outstanding for the push path.
- ~~**Remaining device-gated item:** real `ATTEST_MODE=apple` App Attest verification has only been tested against an injected test root/CA~~ — **RESOLVED 2026-07-28 18:08 PDT.** A genuine physical-iPhone attestation was verified end-to-end against `https://api.hushield.com` running `ATTEST_MODE=apple` with `APP_ID=997DW79YCR.com.brahy.hushield`, delivered via TestFlight build 1 (0.1.0).

  Evidence: `POST /api/v1/attest/challenge` 200 → `POST /api/v1/attest/verify` 200 → `GET /api/v1/blocklist` 200, from user-agent `SpamFilter/1 CFNetwork/3860.600.12 Darwin/25.5.0`. The stored `devices` row carries a 44-char base64 key id, a 91-byte DER `SubjectPublicKeyInfo` P-256 public key (`3059301306072A8648CE3D02…`), and a **3,975-byte Apple-issued attestation receipt** — a mock attestation produces no receipt. Negative control: a fabricated attestation posted to the same endpoint returns 401.

  What made it work: `SpamFilter.Release.entitlements` declares `appattest-environment = production`. TestFlight builds attest against Apple's production service, so the original `development` value would have produced attestations the server rejects, with nothing in the error naming the environment.

  **Still device-gated:** enabling the two extensions under iOS Settings, confirming a real call is blocked, and confirming SMS filtering classifies in airplane mode.

- **Note on enrollment timing:** nothing enrolls on app launch. `EnrollmentService.validToken()` is called lazily by sync, lookup, and report only — `StatusScreen.onAppear` merely reads local state. A first-run user sees "Not enrolled yet" with no obvious action; tapping **Sync now** is what triggers attestation. Worth considering as a UX issue.
- **Remaining deploy-gated items:** no production host chosen yet (`scripts/deploy.sh` no-ops without `DEPLOY_TARGET`); the Dockerfile has never actually been built (no Docker daemon in the environment that authored it) — treat the image build as unverified until run once for real.
- **Carried, now largely addressed:** `MemoryChallengeStore` is still the *default* (dev/single-instance), but a Redis-backed alternative now exists and is config-selectable for multi-instance deploys; `cmd/recompute` now supports a continuous `-interval` mode (plus `launchd`/`cron` artifacts) in addition to the original one-shot invocation — actually scheduling it on a host is still a deploy-time step.

## Session Continuity

Last session: 2026-07-24
Stopped at: Phase 3 (iOS app) and Phase 4 (backend hardening) both merged to `spam-ios-phase3` and substantially complete; planning docs (this file, `ROADMAP.md`) and `docs/MORNING-CHECKLIST.md` brought up to date to close out the phase.
Next step: run the morning device session in `docs/MORNING-CHECKLIST.md` (physical-device build/run, real App Attest validation, enable the two Settings toggles), then pick a deploy host and run `scripts/deploy.sh` for real.
Resume file: None
