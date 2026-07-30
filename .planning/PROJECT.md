# Project: SpamFilter

## Core Value

A community-driven spam call/text filter for iOS — devices report bad numbers,
reports fold into a decaying weighted score, and every device pulls the resulting
blocklist. Identity is an Apple App Attest key only: **no accounts, no PII.**

## Requirements

- [x] REQ-01: Community reporting of spam numbers with device-only identity (App Attest, no PII)
- [x] REQ-02: Weighted, time-decaying per-number scoring → `blocked`/`suspected` tiers; admin override wins
- [x] REQ-03: Devices pull an efficient blocklist delta (keyset pagination, neighbor-spoof widening, crowd caller-ID, removal tombstones)
- [x] REQ-04: Cold-start seeding from FTC/FCC public complaint data via synthetic seed devices
- [x] REQ-05: Device-token lifecycle — attest → verify → assert-refresh — with strictly-increasing counter replay protection
- [x] REQ-06: On-demand single-number reputation lookup (`GET /api/v1/numbers/{e164}`)
- [x] REQ-07: Server-initiated silent APNs push nudging clients to refresh their blocklist
- [~] REQ-08: iOS Call Directory Extension — block + label incoming calls, honoring removal tombstones. *Extension built, signed, and enabled on a physical device; CallKit reload succeeds. Not yet proven by an actual blocked call.*
- [~] REQ-09: iOS SMS Filter Extension — offline classification of incoming messages. *Extension enabled on device; offline (airplane-mode) classification not yet exercised.*
- [x] REQ-10: iOS reporting UI — report spam/not-spam and contribute a community caller-ID name. *Verified 2026-07-28: a report submitted from a physical device returned 201 and stored the caller name.*
- [x] REQ-11: Shared App-Group sync framework — App Attest enroll/refresh + blocklist delta sync across extensions. *Verified 2026-07-28 on device: enrollment, delta sync, and CallKit reload all succeed.*
- [x] REQ-12: Production hardening — real Apple App Attest validated on a physical device, shared (non-process-local) challenge store, scheduled recompute cron. *Verified 2026-07-28: real App Attest confirmed by an Apple-issued receipt; Redis store selectable; recompute on a systemd timer every 15 min.*

## Constraints

- **Stack:** Go + MySQL 8+ backend (per CLAUDE.md defaults); native iOS (Swift) client with Call Directory + SMS Filter app extensions.
- **No PII:** the only identity anywhere in the system is an App Attest `key_id`. No user accounts.
- **Apple toolchain available:** paid Apple Developer account — real-device builds and TestFlight are in scope. Team ID + bundle ID needed to start iOS signing; APNs `.p8` optional for push.
- **Offline-capable client:** SMS filtering must work with no network; call blocking runs off a locally-synced Call Directory.
- `ATTEST_MODE` defaults to `mock` in dev — real `apple` mode only validated once a genuine device attestation exists (Phase 3).

## Key Decisions

| Date | Decision | Rationale |
|------|----------|-----------|
| 2026-07-19 | Two-spec decomposition: build the community backend first, iOS app second | The iOS app has nothing to block until the community data service exists |
| 2026-07-19 | Go + MySQL for the backend | User's CLAUDE.md default stack for backend APIs |
| 2026-07-19 | App Attest as the sole identity; no accounts/PII | Privacy-first; a device key is enough to weight and de-duplicate reports |
| 2026-07-19 | Weighted time-decay scoring with per-device trust, not a raw blocklist | "Amazing," Hiya-grade quality over a bare list; resists brigading and stale data |
| 2026-07-21 | Spec 2a: add assertion refresh, tombstones, lookup, and APNs *before* the app | Anti-drift audit found the client would need these backend surfaces; cheaper to add now |
| 2026-07-23 | Native Swift for the iOS client | Call Directory + SMS Filter are first-class native app extensions; Flutter can't host them |
| 2026-07-23 | Bundle ID `com.brahy.hushield`, Team `997DW79YCR` → App Attest `APP_ID` `997DW79YCR.com.brahy.hushield` | Signed the scaffold with John's paid account; this is the exact identity the backend's `apple` attest mode must trust |
