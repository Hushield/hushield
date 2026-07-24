# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-23)

**Core value:** Community-driven, privacy-first (App Attest, no PII) spam call/text filtering for iOS.
**Current focus:** Phase 3 (iOS app) + Phase 4 (production hardening) — both substantially complete;
remaining work is device-gated (physical iPhone session) and deploy-gated (choose a host).

## Current Position

Phase: 3 and 4 of 4 — substantially complete, pending a device session and a deploy target
Plan: Tasks 1–9 + 11 implemented and merged; Task 10 (this update) is planning-doc wrap-up
Status: Code-complete for everything that doesn't require a physical device or a chosen deploy
  host. What's left is a ~20–30 min guided device session (see `docs/MORNING-CHECKLIST.md`) plus
  picking a production host.
Last activity: 2026-07-24 — Phase 3 iOS app (SpamFilterKit, extensions, SwiftUI app, full test
  suite) and Phase 4 backend hardening (Redis challenge store, scheduled recompute, prod config
  validation, container deploy scaffold, `make`-based test gate) merged to `spam-ios-phase3`.

**iOS signing (resolved 2026-07-23):**
- Apple Team ID: `997DW79YCR` (John Brahy, paid account)
- Bundle ID: `com.brahy.spamfilter`
- **App Attest `APP_ID`: `997DW79YCR.com.brahy.spamfilter`** — the exact value the backend needs for `ATTEST_MODE=apple`.

Progress: [████████░░] ~90% (Phases 1–2 complete; Phases 3–4 substantially complete — remaining
  ~10% is the device-gated App Attest validation, the two Settings toggles, and choosing +
  verifying a deploy host)

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
- **Phase 3 — iOS App: SUBSTANTIALLY COMPLETE**, merged to `spam-ios-phase3`. `SpamFilterKit` framework: `APIClient` matching the exact backend wire contract, App Attest enrollment/refresh with both a real `DeviceAttestationProvider` and a `SimulatorAttestationProvider` stub, blocklist delta sync with tombstone handling, phone E.164↔Int64 conversion, keychain-backed token store, Call Directory + SMS Filter extensions (thin OS shims wrapping tested pure logic). Polished SwiftUI app (Report / Lookup / Status / Setup tabs, design system, view-models). Test coverage: 100 `SpamFilterKitTests` unit tests, `SpamFilterAppTests` view-model tests, an env-gated `IntegrationTests` suite that drives a real local backend, and an `SpamFilterUITests` XCUITest flow. Signed and building green for iOS Simulator with Team `997DW79YCR` / bundle `com.brahy.spamfilter`. **Not yet done (device-gated):** a physical-device build/run (real `DeviceAttestationProvider` only activates on-device), and enabling the two extensions under iOS Settings.
- **Phase 4 — Production Hardening: SUBSTANTIALLY COMPLETE**, merged to `spam-ios-phase3`. Redis-backed `ChallengeStore` (config-selectable via `CHALLENGE_STORE`/`REDIS_URL`, default `memory`), `cmd/recompute -interval` continuous mode plus `launchd`/`cron` scheduler artifacts under `scripts/`, hard-fail prod config validation (`Load()` refuses to start with `ATTEST_MODE=apple` and a missing `APP_ID`/insecure `DEVICE_TOKEN_SECRET`/empty `ADMIN_TOKEN`), `.env.example` documenting every server env var, a containerized deploy scaffold (`Dockerfile`, `docker-compose.yml`, `scripts/deploy.sh`), and a local `make`-based test gate (`make test` = Go + iOS, `make deploy` depends on `make test`, `make hooks` installs a pre-push gate). Go coverage raised 76.7% → 86.7%. **Not yet done (device/deploy-gated):** real `ATTEST_MODE=apple` validated against a genuine physical-device attestation (needs Phase 3's device session first); the backend has not been deployed to a real host (`DEPLOY_TARGET` is unset — `scripts/deploy.sh` is a guarded no-op until a host is chosen); the `docker build` itself is **unverified** (no Docker daemon available in the build/dev environment that produced it).

Repo health at sync time (2026-07-24): `go build ./...` clean.

### Blockers/Concerns

- ~~Phase 3 needs Team ID + bundle ID before it can start~~ — **resolved 2026-07-23** (Team `997DW79YCR`, bundle `com.brahy.spamfilter`). Optional APNs `.p8` still outstanding for the push path.
- **Remaining device-gated item:** real `ATTEST_MODE=apple` App Attest verification has only been tested against an injected test root/CA — it needs a genuine physical-iPhone attestation to validate end-to-end. See `docs/MORNING-CHECKLIST.md` for the exact steps.
- **Remaining deploy-gated items:** no production host chosen yet (`scripts/deploy.sh` no-ops without `DEPLOY_TARGET`); the Dockerfile has never actually been built (no Docker daemon in the environment that authored it) — treat the image build as unverified until run once for real.
- **Carried, now largely addressed:** `MemoryChallengeStore` is still the *default* (dev/single-instance), but a Redis-backed alternative now exists and is config-selectable for multi-instance deploys; `cmd/recompute` now supports a continuous `-interval` mode (plus `launchd`/`cron` artifacts) in addition to the original one-shot invocation — actually scheduling it on a host is still a deploy-time step.

## Session Continuity

Last session: 2026-07-24
Stopped at: Phase 3 (iOS app) and Phase 4 (backend hardening) both merged to `spam-ios-phase3` and substantially complete; planning docs (this file, `ROADMAP.md`) and `docs/MORNING-CHECKLIST.md` brought up to date to close out the phase.
Next step: run the morning device session in `docs/MORNING-CHECKLIST.md` (physical-device build/run, real App Attest validation, enable the two Settings toggles), then pick a deploy host and run `scripts/deploy.sh` for real.
Resume file: None
