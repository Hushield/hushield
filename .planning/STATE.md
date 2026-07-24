# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-23)

**Core value:** Community-driven, privacy-first (App Attest, no PII) spam call/text filtering for iOS.
**Current focus:** Phase 3 — the native iOS app (Spec 2b)

## Current Position

Phase: 3 of 4 (iOS App)
Plan: 0 of N in current phase — not yet planned
Status: Ready to plan — iOS project scaffolded and signed; start-blocker (Team ID + bundle ID) resolved
Last activity: 2026-07-23 — iOS app scaffold created, signed with the John Brahy team

**iOS signing (resolved 2026-07-23):**
- Apple Team ID: `997DW79YCR` (John Brahy, paid account)
- Bundle ID: `com.brahy.spamfilter`
- **App Attest `APP_ID`: `997DW79YCR.com.brahy.spamfilter`** — the exact value the backend needs for `ATTEST_MODE=apple`.

Progress: [█████░░░░░] ~50% (2 of 4 phases complete)

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

Repo health at sync time: `go build ./...` and `go vet ./...` both clean.

### Blockers/Concerns

- ~~Phase 3 needs Team ID + bundle ID before it can start~~ — **resolved 2026-07-23** (Team `997DW79YCR`, bundle `com.brahy.spamfilter`). Optional APNs `.p8` still outstanding for the push path.
- **Carried production-gating items (Phase 4):** `ATTEST_MODE` defaults to `mock` (real Apple verifier only tested against an injected test root — needs a genuine device attestation); `MemoryChallengeStore` is process-local (100k cap, lazy purge) and needs a shared store for multi-instance; `cmd/recompute` is a one-shot CLI meant for system cron/launchd (~15 min).

## Session Continuity

Last session: 2026-07-23
Stopped at: iOS app scaffolded under `ios/` (XcodeGen → `SpamFilter.xcodeproj`, SwiftUI, builds green) and signed with the John Brahy team (`997DW79YCR`)
Next step: run the Phase 3 (iOS app) brainstorm/plan cycle — decompose the Call Directory Extension, SMS Filter Extension, reporting UI, and shared App-Group sync framework
Resume file: None
