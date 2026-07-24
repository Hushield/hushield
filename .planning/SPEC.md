# Project Specification: SpamFilter

## Overview

SpamFilter is a community spam-reporting and filtering system in the spirit of
Hiya/Truecaller. iOS devices — identified only by an Apple App Attest key, with no
user accounts or PII — report phone numbers as spam or not-spam. Reports fold into a
decaying, trust-weighted score per number, producing a `blocked`/`suspected`
classification, and any attested device pulls the resulting blocklist as an
incremental delta. The system is two halves: a Go + MySQL community backend (built)
and a native iOS client with Call Directory and SMS Filter extensions (next).

## Goals

- Filter spam calls and texts on iOS using crowd-sourced, decaying reputation data.
- Preserve privacy: the only identity in the system is an App Attest device key.
- Resist brigading and staleness via per-device trust weighting and time decay.
- Ship a client that blocks calls and filters SMS **offline**, syncing deltas when online.
- Bootstrap coverage from public FTC/FCC complaint data so the network is useful on day one.

## Target Users

- iPhone owners who want fewer spam/scam calls and texts without handing over a contact list or phone number.
- The community itself — every report improves the shared blocklist.

## Core Features

- **App Attest identity** — challenge → verify → device token, refreshed via signed assertions (no PII, no accounts).
- **Trust-weighted, time-decaying scoring** — `blocked`/`suspected` tiers; admin overrides win; per-device trust from tenure, volume, and disagreement.
- **Blocklist delta API** — keyset-paginated block/label/unblock stream, widened by neighbor-spoof prefix, carrying crowd-agreed caller-ID names.
- **Single-number lookup** — on-demand reputation for one E.164 number.
- **Silent push refresh** — server nudges devices via APNs background push to pull fresh deltas.
- **Cold-start seeding** — one-shot import of FTC/FCC public complaint data as synthetic seed-device reports.
- **iOS Call Directory Extension** — blocks and labels incoming calls from the synced list, honoring removal tombstones.
- **iOS SMS Filter Extension** — classifies incoming messages offline.
- **iOS reporting UI + shared sync framework** — report numbers, contribute caller-ID names, and keep the App-Group-shared blocklist in sync.

## Scope

- Go + MySQL backend: identity, scoring/trust, reporting, blocklist delta, lookup, push, admin overrides, seeding, recompute cron. **(Built — Phases 1–2.)**
- Native iOS app: Call Directory Extension, SMS Filter Extension, reporting UI, shared App-Group sync framework. **(Phase 3.)**
- Production hardening: real Apple App Attest, shared challenge store, scheduled recompute. **(Phase 4.)**

## Non-Goals

- User accounts, social features, or any storage of PII.
- Android or other non-iOS clients.
- Carrier-level or SS7 blocking — this is on-device filtering plus a shared reputation service.
- A general contact/dialer replacement.

## Constraints

- Backend: Go + MySQL 8+, parameterized queries only, `/api/v1` response envelope.
- Client: native Swift; Call Directory + SMS Filter are OS-provided app extensions with hard memory/runtime limits.
- Identity is App Attest only; the backend never sees a user, name, or phone-owner mapping.
- SMS filtering must function offline; call blocking runs off a locally-synced Call Directory.
- Paid Apple Developer account available; Team ID + bundle ID required before iOS signing.

## Measurable Success Criteria

- A device can enroll via App Attest and refresh its token via assertion without re-attesting. **(Met.)**
- Reports change a number's cached status through the trust-weighted, time-decayed scoring engine. **(Met.)**
- A device syncs an incremental blocklist delta (block/label/unblock) and never re-fetches the full set. **(Met.)**
- A number can be looked up on demand and a silent push can nudge a device to refresh. **(Met.)**
- An iPhone blocks a known-spam call and filters a known-spam SMS from the synced list, offline. **(Phase 3.)**
- A user reports a number from the app and sees it reflected in the community score. **(Phase 3.)**
- Real Apple App Attest (`apple` mode) is validated end-to-end against a physical device. **(Phase 4.)**
