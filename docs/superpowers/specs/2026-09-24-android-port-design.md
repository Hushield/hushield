# HuShield for Android — Design

## Purpose and constraints

Port HuShield to Android with full feature parity with the iOS app: call blocking and SMS
filtering, backed by the same Go/MySQL server. The non-negotiable constraint carries over
unchanged from `CONTRIBUTING.md`: **zero PII, anywhere.** The only identity in the system is a
hardware-attested device key. No accounts, no phone numbers on reporters, no analytics
identifiers, no linkage between a report and a person.

**Decisions made during scoping (see below for reasoning):**
- Full parity for v1: call blocking **and** SMS filtering, not call-blocking-only.
- Native Kotlin, not Flutter — this app lives in OS integration points (Keystore,
  `CallScreeningService`, the default-SMS-app role) that Flutter would reach through a native
  plugin layer anyway, so cross-platform UI code buys nothing here.
- Devices without Google Play Services (GrapheneOS-style) are out of scope for v1. Play
  Integrity requires GMS. This is a real gap against the app's own privacy-conscious audience,
  accepted knowingly rather than diluted around with a weaker fallback attestation path.

## Why this is architectural, not bounded

This is a new native client plus a new attestation-verification backend, not a change to an
existing flow. It restructures how a whole platform's identity model plugs into the existing
`attest.Verifier` interface and adds a genuinely new subsystem (a minimal SMS app) with no iOS
analog.

## Backend reuse audit

Confirmed by reading the code, not assumed:

| Package | Apple-specific? | Android impact |
|---|---|---|
| `internal/scoring` | No | None. Pure, deterministic, keyed on abstract device trust — untouched. |
| `internal/trust` | No | None. Same reasoning. |
| `internal/store` (blocklist, reports, devices) | No | None. Everything is keyed on `phone_number_id`/`device_id`, not platform. |
| `internal/api` | Only `router.go`'s verifier selection | Add one `case` for `ATTEST_MODE=android`. |
| `internal/attest` | `apple.go`, `verifier.go`'s `Verifier` interface is already platform-neutral | Add `PlayIntegrityVerifier` implementing the existing interface. `challenge.go`/`challenge_redis.go` (the `ChallengeStore`) are reused unchanged. |
| `internal/store/push.go` | Yes — APNs-only, `push_environment` enum has no Android value | Add an FCM sender path; migration needed (see Data model changes). |

**Net: the backend is roughly 90% reusable as-is.** The two seams that need new code
(`PlayIntegrityVerifier`, an FCM sender) are additions beside the existing Apple-specific code,
not changes to it — `ATTEST_MODE=apple` and `ATTEST_MODE=android` coexist, same as
`push_environment` gains a value rather than being redefined.

## Component design

### 1. Enrollment / attestation

Mirrors the iOS flow (`EnrollmentService` + `AttestationProvider`) with Play Integrity standing
in for App Attest, using the device-binding pattern Play Integrity is designed for:

1. App generates an ECDSA key pair in Android Keystore, requesting StrongBox backing where
   available and falling back to TEE-backed keys otherwise (StrongBox isn't universal below
   API 28/certain OEMs).
2. App requests a challenge from the existing `POST /attest/challenge` — **no server change**,
   this endpoint is already platform-neutral.
3. App calls the Play Integrity API with `nonce = SHA-256(challenge || public_key_der)`, binding
   the attestation to this specific key rather than to the app install in general.
4. App sends the resulting signed token plus its public key to `POST /attest/enroll`.
5. Server's `PlayIntegrityVerifier.VerifyAttestation` (new, implements the existing `Verifier`
   interface) calls Google's token-verification endpoint, checks the nonce matches, checks the
   device/app integrity verdicts meet a minimum bar, and returns the public key — same return
   shape `apple.go`'s `VerifyAttestation` already produces.
6. From here on, identical to iOS: the device signs each request locally with the Keystore key,
   a strictly-increasing counter provides replay protection, `VerifyAssertion` validates it. No
   new backend concept — `internal/token`, `devices.sign_count`, the whole assertion-verification
   contract carries over untouched.

**Key recovery**, mirroring PR #24's iOS fix: a Keystore key becomes unusable if the app is
reinstalled or if Keystore's key material is invalidated (e.g. biometric enrollment changes, if
we ever gate the key on that — not planned for v1, noted so a future change doesn't reintroduce
this bug blind). The Android equivalent of `AttestationProviderError.keyUnusable` is catching
`android.security.keystore.KeyPermanentlyInvalidatedException`, clearing the stored identity, and
re-enrolling once — same one-shot-retry shape as the iOS fix, not a loop.

### 2. Call blocking

`CallScreeningService`, registered as a role independent of default-dialer status — unlike SMS,
this needs no default-app trade with the user. `onScreenCall` must resolve synchronously and
fast (Android imposes a response deadline), so it can only ever read a local cache, never the
network — same constraint `CallDirectoryExtension` has on iOS, same answer: a local SQLite/Room
table kept in sync by a periodic delta fetch against `/api/v1/blocklist`, mirroring
`SyncService`.

### 3. SMS filtering — the one piece with no iOS analog

This is the single biggest net-new scope item in the whole port, and worth stating plainly: iOS's
`MessageFilterExtension` is an opt-in OS hook with no user-facing trade-off. Android has no
equivalent for a third party. The only way to actually block (not just flag) an incoming SMS
before it reaches the user's inbox is:

1. Hold the default-SMS-app role (`RoleManager.ROLE_SMS`).
2. Register `SmsReceiver` for `SMS_RECEIVED_ACTION` and call `abortBroadcast()` on anything the
   local blocklist flags, before the system's own SMS app (now this app) ever writes it to the
   inbox.

Holding that role is not optional cosmetics — Android and Play Store policy require a genuinely
usable SMS app behind it (view threads, compose, send), not a thin shell around a filter. That
means this phase includes building real, if minimal, messaging UI: a thread list, a conversation
view, and a compose flow. This is real product surface that didn't exist as a decision on iOS,
and it's the most likely place a rough estimate turns out optimistic.

**Rejected alternative**: a `NotificationListenerService` reading the default SMS app's own
notifications and dismissing spam ones. Rejected because (a) it can only hide a notification, not
prevent the message landing in the inbox — a materially weaker guarantee than what "filtering"
means on iOS or what "blocking" means for calls in this same app, and (b) reading notification
content is itself a sensitive permission Play Store scrutinizes, without buying a stronger result
in exchange.

### 4. Data layer

A Room database plays the same role `BlocklistDatabase.swift` plays on iOS: local cache of the
blocklist delta (`phone_number_id`, `number`, `cached_score`, `status`, `updated_at`), synced via
the same keyset-delta protocol PR #26 optimized (`/api/v1/blocklist?since_sec=&since_id=`) — no
new server-side sync protocol, the Android client is just another consumer of the existing one.

### 5. Push notifications

`internal/store/push.go` gains an FCM sender alongside its existing APNs sender. Requires a
migration adding a platform discriminator: today's `push_environment` column is an APNs-only
enum (`sandbox`/`production`); this becomes either a separate `push_platform` column
(`apns`/`fcm`) or the enum grows Android-specific values, decided at implementation-plan time
based on which reads more clearly in `push.go`'s dispatch logic.

### 6. UI

Jetpack Compose screens mirroring the SwiftUI app's Report / Lookup / Status / Setup flows, plus
the minimal messaging UI required by item 3.

## What's explicitly out of scope for v1

- Devices without Google Play Services (no fallback attestation path).
- Any weakening of the "hardware-attested key is the only identity" guarantee to accommodate the
  above.
- Feature work beyond parity with the current iOS app — no Android-only features in this pass.

## Testing approach

Following the existing repo convention (TDD, `internal/dbtest` helpers, table-driven tests for
pure logic):

- `PlayIntegrityVerifier`: unit tests against Google's documented token format/error codes,
  mirroring `apple_test.go`'s structure (valid token, expired, tampered nonce, failed integrity
  verdict).
- FCM sender: unit tests mirroring the existing APNs sender's tests in `internal/store/push_test.go`.
- Android client: instrumented tests for `CallScreeningService`/`SmsReceiver` against a fake
  local blocklist, plus enrollment-recovery tests mirroring iOS's `EnrollmentRecoveryTests`
  (`KeyPermanentlyInvalidatedException` → clear-and-re-enroll-once).
- No changes anticipated to `internal/scoring`/`internal/trust` test suites — they're
  platform-agnostic and untouched by this work.

## Sequencing note

The backend seams (`PlayIntegrityVerifier`, FCM sender, migration) can land and ship independently
of the Android client — they're additive, don't change existing `ATTEST_MODE=apple` behavior, and
can be verified against Google's test tokens before any Kotlin code exists. Recommend that as
phase 1 of the implementation plan, so the highest-uncertainty piece (attestation) gets verified
against a real Google API early rather than discovered late inside client work.
