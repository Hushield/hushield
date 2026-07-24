# Roadmap: SpamFilter

## Overview

SpamFilter is delivered as a community backend first (the iOS app has nothing to
block until the data service exists), then the native iOS client, then production
hardening. Phases 1–2 (the Go + MySQL backend, Specs 1 and 2a) are **complete and
merged to `main`**. Phase 3 (the iOS app, Spec 2b) is the current focus. Phase 4
carries the production-gating follow-ups surfaced during backend reviews.

## Phases

- [x] **Phase 1: Community Backend** — Go + MySQL service: App Attest identity, trust-weighted decaying scoring, reporting, blocklist delta, admin overrides, seeding, recompute cron.
- [x] **Phase 2: Backend for the iOS Client** — Backend surfaces the app needs: assertion token refresh, removal tombstones, single-number lookup, APNs silent push.
- [ ] **Phase 3: iOS App** — Native Swift client: Call Directory Extension, SMS Filter Extension, reporting UI, shared App-Group sync framework. **← current focus**
- [ ] **Phase 4: Production Hardening & Go-Live** — Real Apple App Attest on device, shared challenge store, scheduled recompute, prod deploy.

## Phase Details

### Phase 1: Community Backend — COMPLETE (merged 2026-07-19)
**Goal**: Stand up the community reputation service every client will depend on.
**Depends on**: Nothing (first phase)
**Requirements**: REQ-01, REQ-02, REQ-03 (base delta), REQ-04
**Success Criteria**:
  1. A device enrolls via App Attest (challenge → verify) and receives a signed device token. ✅
  2. A report changes a number's cached status via the trust-weighted, time-decayed scoring engine. ✅
  3. A device pulls a keyset-paginated blocklist delta, widened by neighbor-spoof prefix, with crowd caller-ID names. ✅
  4. An admin override forces allow/block and wins over the community score. ✅
  5. FTC/FCC public data seeds the number set via synthetic seed devices; a recompute cron decays and re-scores. ✅

### Phase 2: Backend for the iOS Client — COMPLETE (merged 2026-07-21)
**Goal**: Add the backend surfaces the iOS client needs before the app is built (found via anti-drift audit).
**Depends on**: Phase 1
**Requirements**: REQ-05, REQ-03 (tombstones), REQ-06, REQ-07
**Success Criteria**:
  1. A device refreshes its token from an App Attest assertion (no re-attestation), with strictly-increasing counter replay protection. ✅
  2. The blocklist delta emits `action:"unblock"` tombstones when a number falls back below blockable. ✅
  3. `GET /api/v1/numbers/{e164}` returns a single number's reputation on demand. ✅
  4. A registered device can be nudged by a silent APNs push to refresh; recompute emits a true delta (bumps `updated_at` only on real change). ✅

### Phase 3: iOS App — NOT STARTED (current focus)
**Goal**: A native iOS app that blocks spam calls and filters spam texts from the synced community list, and lets users report numbers.
**Depends on**: Phase 2
**Requirements**: REQ-08, REQ-09, REQ-10, REQ-11
**Needs from user to start**: Apple **Team ID + bundle ID** (for `APP_ID`/signing); optional APNs `.p8` for push.
**Success Criteria** (goal-backward):
  1. A Call Directory Extension blocks and labels incoming calls from the locally-synced list and removes numbers on `unblock` tombstones.
  2. An SMS Filter Extension classifies a known-spam message offline (no network at filter time).
  3. A shared App-Group framework enrolls the device via App Attest, refreshes the token via assertion, and syncs the blocklist delta by cursor.
  4. A user reports a number (spam/not-spam) and optionally a caller-ID name from the app, and the report reaches the backend.
  5. The app builds, signs, and runs on a physical device via the paid developer account.

### Phase 4: Production Hardening & Go-Live — NOT STARTED
**Goal**: Everything required to run the service in production for real users.
**Depends on**: Phase 3 (real device attestations exist)
**Requirements**: REQ-12
**Success Criteria**:
  1. Real Apple App Attest (`ATTEST_MODE=apple`) validated end-to-end against a genuine physical-device attestation.
  2. Challenge store is shared/durable (e.g. Redis) so a multi-instance deploy is safe (replaces process-local `MemoryChallengeStore`).
  3. Recompute/decay runs on a real schedule (launchd/cron/hosted scheduler), not a manual one-shot invocation.
  4. Backend deployed with production config (secrets, admin token, DSN, APNs credentials) and health-checked.
