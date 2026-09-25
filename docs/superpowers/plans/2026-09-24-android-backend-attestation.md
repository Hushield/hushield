# Android Backend Attestation + Push Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the existing Go server accept and serve Android devices attested via Play Integrity, alongside the existing Apple App Attest devices, with no change to today's iOS behavior.

**Architecture:** Add a `PlayIntegrityVerifier` implementing the existing `attest.Verifier` interface, unchanged from Apple's. Because iOS and Android devices must be served by the *same running server at the same time* (not a deployment-time mode switch), replace the server's single global verifier with a small map keyed by platform, selected per-request at enrollment and looked up from the stored device row at every later request. Add an `FCMNotifier` alongside the existing `APNsNotifier`, dispatched the same way.

**Tech Stack:** Go 1.26 (per `go.mod`), MySQL 8+, existing `internal/attest`/`internal/api`/`internal/store`/`internal/push` packages, Google's Play Integrity REST API, Firebase Cloud Messaging HTTP v1 API.

**Spec:** `docs/superpowers/specs/2026-09-24-android-port-design.md`

## Global Constraints

- Zero PII, no exceptions (`CONTRIBUTING.md`). Nothing in this plan stores a phone number, email, or any identifier beyond the existing `key_id`/device-key model.
- `internal/scoring` and `internal/trust` stay pure and dependency-free — untouched by this plan.
- `internal/store` uses parameterized queries only.
- `make test` (`go test ./... -race -cover`) is green before and after every task.
- TDD: failing test first, for the right reason, then the minimal implementation.
- `ATTEST_MODE=apple` must behave identically to today after this plan lands — Apple devices are not touched, only added alongside.
- Migrations are numbered sequentially from whatever is newest on `main` at merge time. This plan uses `0011`; renumber if PR #26 (`0007`-`0010`) hasn't merged yet when this is implemented — check `ls internal/db/migrations/ | sort | tail -1` before writing the migration file.

## Review Focus

- **A device enrolling with no `platform` field at all** (every real iOS client today, since this field doesn't exist yet) — must default to `"apple"` and behave exactly as before. Covered in Task 6.
- **A device enrolled as `"android"` later sending an `assert` request** — the server must use the *stored* platform from enrollment, never a client-supplied value on the assert path (there is no platform field on assert requests at all, by design) — covered in Task 6.
- **An unrecognized `platform` value, or a recognized one this deployment didn't enable** (`platform:"android"` against an `ATTEST_MODE=apple`-only server) — both must fail closed with a 400, never fall back to a permissive mock verifier for the platform that wasn't configured. Covered in Task 6.
- **A Play Integrity token whose nonce doesn't match the submitted public key** — must fail closed (this is the whole point of nonce-binding; a token valid for a *different* key must never attest *this* key). Covered in Task 4.
- **A device with no push token registered** (true for every device before it opts into push) — `ListPushTargets`/`BroadcastRefresh` must keep skipping it exactly as today, for both platforms. Covered in Task 2.

---

## File structure

- `internal/db/migrations/0011_device_platform.up.sql` / `.down.sql` — new migration, `devices.platform` + `devices.push_platform` columns.
- `internal/store/device.go` — extend `GetDeviceByKeyID`, add `platform` param to the upsert path.
- `internal/store/push.go` — extend `PushTarget`, `UpsertPushToken`, `ListPushTargets` with a platform field.
- `internal/attest/android.go` (new) — Android wire-format types and `PlayIntegrityVerifier.VerifyAssertion` (pure crypto, no network).
- `internal/attest/android_test.go` (new)
- `internal/attest/playintegrity.go` (new) — `PlayIntegrityVerifier.VerifyAttestation`, the network-calling half.
- `internal/attest/playintegrity_test.go` (new)
- `internal/config/config.go` — `AttestMode` grows two new accepted values (`"android"`, `"both"`), plus `AndroidPackageName`/`PlayIntegrityCredentialsPath` fields.
- `internal/api/attest.go` — `verifyRequest` gains `Platform`; `attestHandler.verifier` (single) becomes `attestHandler.verifiers` (map); `upsertDevice` gains a `platform` param; `handleAssert` looks up the device's stored platform.
- `internal/api/router.go` — `buildVerifier` becomes `buildVerifiers`, returning the map.
- `internal/push/fcm.go` (new) — `FCMNotifier`.
- `internal/push/fcm_test.go` (new)
- `internal/push/platform.go` (new) — `PlatformNotifier`, dispatches to APNs or FCM by `target.Platform`.
- `internal/push/platform_test.go` (new)
- `cmd/recompute/main.go` — wire `PlatformNotifier` instead of a bare `APNsNotifier`.

---

### Task 1: Migration — `devices.platform` and `devices.push_platform`

**Files:**
- Create: `internal/db/migrations/0011_device_platform.up.sql`
- Create: `internal/db/migrations/0011_device_platform.down.sql`

**Interfaces:**
- Produces: `devices.platform ENUM('apple','android')`, `devices.push_platform ENUM('apns','fcm')`, both `NOT NULL`, both defaulted so every existing row backfills to today's only reality.

- [ ] **Step 1: Write the up migration**

```sql
-- devices.platform records which attestation family enrolled this device
-- ("apple" App Attest, "android" Play Integrity). Every existing row is an
-- iOS device, so the default backfills correctly with no data migration.
--
-- devices.push_platform records which push service push_token targets.
-- Independent of `platform` in principle (a future cross-platform build
-- could exist), but today's only real service is APNs, so it defaults the
-- same way.
ALTER TABLE devices
  ADD COLUMN platform ENUM('apple','android') NOT NULL DEFAULT 'apple' AFTER key_id,
  ADD COLUMN push_platform ENUM('apns','fcm') NOT NULL DEFAULT 'apns' AFTER push_environment;
```

- [ ] **Step 2: Write the down migration**

```sql
ALTER TABLE devices
  DROP COLUMN platform,
  DROP COLUMN push_platform;
```

- [ ] **Step 3: Apply and verify**

Run: `DB_DSN='root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true' go run ./cmd/server &` then `Ctrl-C` once it logs migrations applied (migrations run automatically on startup; no separate migrate command). Confirm with:
```bash
mysql -uroot spamfilter_dev -e "SHOW COLUMNS FROM devices LIKE 'platform'; SHOW COLUMNS FROM devices LIKE 'push_platform';"
```
Expected: both columns present, `Default` = `apple` and `apns` respectively.

- [ ] **Step 4: Commit**

```bash
git add internal/db/migrations/0011_device_platform.up.sql internal/db/migrations/0011_device_platform.down.sql
git commit -m "feat(db): add devices.platform and devices.push_platform"
```

---

### Task 2: Store — carry platform through device and push-target reads/writes

**Files:**
- Modify: `internal/store/device.go`
- Modify: `internal/store/push.go`
- Test: `internal/store/device_test.go`
- Test: `internal/store/push_test.go`

**Interfaces:**
- Consumes: migration from Task 1 (`devices.platform`, `devices.push_platform` columns).
- Produces:
  - `GetDeviceByKeyID(ctx, exec, keyID) (deviceID uint64, publicKeyDER []byte, signCount uint32, platform string, err error)` — **signature change**, `platform` added as the 4th return value.
  - `UpsertDevicePlatform(ctx, exec, keyID string, publicKey, receipt []byte, platform string, now time.Time) (deviceID uint64, err error)` — new function; the API layer's `upsertDevice` in Task 6 calls this instead of building its own SQL.
  - `PushTarget{DeviceID uint64, Token string, Environment string, Platform string}` — **field added**.
  - `UpsertPushToken(ctx, exec, deviceID uint64, token, environment, platform string, now time.Time) error` — **signature change**, `platform` added as a parameter.
  - `ListPushTargets(ctx, db) ([]PushTarget, error)` — unchanged signature, now populates `Platform`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/device_test.go`:

```go
func TestGetDeviceByKeyID_returnsPlatform(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	_, err := UpsertDevicePlatform(ctx, sqlDB, "android-key-1", []byte("pubkey-android-key-1"), nil, "android", time.Now())
	if err != nil {
		t.Fatalf("UpsertDevicePlatform: %v", err)
	}

	_, _, _, platform, err := GetDeviceByKeyID(ctx, sqlDB, "android-key-1")
	if err != nil {
		t.Fatalf("GetDeviceByKeyID: %v", err)
	}
	if platform != "android" {
		t.Errorf("platform = %q, want %q", platform, "android")
	}
}

func TestUpsertDevicePlatform_existingRowKeepsItsPlatform(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	id1, err := UpsertDevicePlatform(ctx, sqlDB, "apple-key-1", []byte("pubkey-v1"), nil, "apple", now)
	if err != nil {
		t.Fatalf("first UpsertDevicePlatform: %v", err)
	}

	// Re-enrolling the same key_id must not let a re-attestation silently
	// change its recorded platform.
	id2, err := UpsertDevicePlatform(ctx, sqlDB, "apple-key-1", []byte("pubkey-v2"), nil, "apple", now)
	if err != nil {
		t.Fatalf("second UpsertDevicePlatform: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("re-enrolling key_id created a new device: %d != %d", id1, id2)
	}

	_, pubDER, _, platform, err := GetDeviceByKeyID(ctx, sqlDB, "apple-key-1")
	if err != nil {
		t.Fatalf("GetDeviceByKeyID: %v", err)
	}
	if platform != "apple" {
		t.Errorf("platform = %q, want %q", platform, "apple")
	}
	if string(pubDER) != "pubkey-v2" {
		t.Errorf("public key was not updated on re-enroll: got %q", pubDER)
	}
}
```

Add to `internal/store/push_test.go` (check the existing file first for its exact `insertDevice` helper signature and reuse it):

```go
func TestUpsertPushToken_storesPlatform(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	deviceID := insertDevice(t, sqlDB, "fcm-device-1", 1.0)
	if err := UpsertPushToken(ctx, sqlDB, deviceID, "fcm-token-abc", "", "fcm", now); err != nil {
		t.Fatalf("UpsertPushToken: %v", err)
	}

	targets, err := ListPushTargets(ctx, sqlDB)
	if err != nil {
		t.Fatalf("ListPushTargets: %v", err)
	}
	var found bool
	for _, target := range targets {
		if target.DeviceID != deviceID {
			continue
		}
		found = true
		if target.Platform != "fcm" {
			t.Errorf("Platform = %q, want %q", target.Platform, "fcm")
		}
	}
	if !found {
		t.Fatalf("device %d not found in ListPushTargets", deviceID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `DB_DSN='root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true' go test ./internal/store/... -run 'TestGetDeviceByKeyID_returnsPlatform|TestUpsertDevicePlatform_existingRowKeepsItsPlatform|TestUpsertPushToken_storesPlatform' -v`
Expected: FAIL — `UpsertDevicePlatform` undefined, `GetDeviceByKeyID` called with wrong number of return values, `UpsertPushToken` called with wrong number of arguments.

- [ ] **Step 3: Implement**

Replace `internal/store/device.go`'s `GetDeviceByKeyID` and add `UpsertDevicePlatform`:

```go
// GetDeviceByKeyID looks up a device by its attestation key_id, returning its
// device_id, stored PKIX-DER public key, last-recorded signature counter,
// and enrolled platform ("apple" or "android"). It returns ErrDeviceNotFound
// when no such device exists.
func GetDeviceByKeyID(ctx context.Context, exec Execer, keyID string) (deviceID uint64, publicKeyDER []byte, signCount uint32, platform string, err error) {
	const query = `SELECT device_id, public_key, sign_count, platform FROM devices WHERE key_id = ?`
	err = exec.QueryRowContext(ctx, query, keyID).Scan(&deviceID, &publicKeyDER, &signCount, &platform)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, 0, "", ErrDeviceNotFound
	}
	if err != nil {
		return 0, nil, 0, "", err
	}
	return deviceID, publicKeyDER, signCount, platform, nil
}

// UpsertDevicePlatform inserts or updates the device row keyed by key_id and
// returns its device_id. New rows get trust.TrustBase, matching upsertDevice's
// existing enrolment behavior. platform is only ever written on INSERT: a
// re-attestation of an existing key_id must not let a client silently change
// its recorded platform.
func UpsertDevicePlatform(ctx context.Context, exec Execer, keyID string, publicKey, receipt []byte, platform string, now time.Time) (uint64, error) {
	const upsert = `INSERT INTO devices (key_id, platform, public_key, receipt, last_seen_at, trust_weight)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    public_key = VALUES(public_key),
    receipt = VALUES(receipt),
    last_seen_at = VALUES(last_seen_at)`
	if _, err := exec.ExecContext(ctx, upsert, keyID, platform, publicKey, receipt, now.UTC(), trust.TrustBase); err != nil {
		return 0, err
	}

	var deviceID uint64
	if err := exec.QueryRowContext(ctx, "SELECT device_id FROM devices WHERE key_id = ?", keyID).Scan(&deviceID); err != nil {
		return 0, err
	}
	return deviceID, nil
}
```

`internal/store` already imports `"spamfilter/internal/trust"` (in `recompute.go`), so `device.go` can call `trust.TrustBase` directly — add the same import to `device.go`'s import block if it isn't already there.

Modify `internal/store/push.go`:

```go
type PushTarget struct {
	DeviceID    uint64
	Token       string
	Environment string
	Platform    string
}

// UpsertPushToken records (or replaces) a device's push token, its platform
// ("apns" or "fcm"), and (for apns) which environment to send through.
// environment is ignored for platform "fcm" but still stored as given, so a
// caller need not branch on platform before calling this.
func UpsertPushToken(ctx context.Context, exec Execer, deviceID uint64, token, environment, platform string, now time.Time) error {
	const query = `UPDATE devices SET push_token = ?, push_environment = ?, push_platform = ?, push_updated_at = ? WHERE device_id = ?`
	_, err := exec.ExecContext(ctx, query, token, environment, platform, now.UTC(), deviceID)
	return err
}

func ListPushTargets(ctx context.Context, db Execer) ([]PushTarget, error) {
	const query = `SELECT devices.device_id, devices.push_token, devices.push_environment, devices.push_platform FROM devices WHERE devices.push_token IS NOT NULL`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []PushTarget
	for rows.Next() {
		var t PushTarget
		if err := rows.Scan(&t.DeviceID, &t.Token, &t.Environment, &t.Platform); err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return targets, nil
}
```

- [ ] **Step 4: Fix call sites broken by the signature changes**

Run: `go build ./... 2>&1` and fix every resulting error. Expect at minimum:
- `internal/api/attest.go`'s `upsertDevice` and `handleAssert` (temporarily pass a hardcoded `"apple"` literal here — Task 6 replaces this properly; the goal of this step is just "the store package's new contract compiles everywhere," not the full platform-dispatch feature).
- `internal/api/push_token.go` (or wherever `POST /api/v1/devices/push-token` is handled) — same temporary `"apns"` literal.
- `internal/store/push_test.go`'s two pre-existing calls to `UpsertPushToken(ctx, sqlDB, deviceID, "abc123", "production", now)` and `UpsertPushToken(ctx, sqlDB, deviceID, "def456", "sandbox", now)` in `TestUpsertPushToken`, and one call in `TestListPushTargets` — add `"apns"` as the new 5th argument (before `now`) to all three, e.g. `UpsertPushToken(ctx, sqlDB, deviceID, "abc123", "production", "apns", now)`. Also add one assertion to `TestListPushTargets`: `if got.Platform != "apns" { t.Errorf("Platform = %q, want apns", got.Platform) }`, right after its existing `got.Environment` check.
- Any other existing test in `internal/api/attest_test.go` calling `upsertDevice` or asserting on `attestHandler.verifier` (singular) — update to the new `platform` parameter / `verifiers` (plural) field.

- [ ] **Step 5: Run tests to verify they pass**

Run: `DB_DSN='root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true' go test ./internal/store/... ./internal/api/... -race -v 2>&1 | tail -60`
Expected: PASS, all packages.

- [ ] **Step 6: Commit**

```bash
git add internal/store/device.go internal/store/push.go internal/store/device_test.go internal/store/push_test.go internal/api/
git commit -m "feat(store): carry attestation and push platform through device reads/writes"
```

---

### Task 3: Android wire format + local-crypto assertion verification

No network call in this task — `VerifyAssertion` for Android is pure ECDSA verification, exactly like Apple's, just over a simpler wire format (Android has nothing analogous to App Attest's `authenticatorData` framing, so this plan defines a minimal equivalent).

**Files:**
- Create: `internal/attest/android.go`
- Create: `internal/attest/android_test.go`

**Interfaces:**
- Produces:
  - `type androidAssertion struct { Counter uint32; Signature []byte }` (JSON-tagged `counter`, `signature`; `Signature` is base64 in JSON, raw bytes once decoded) — the wire format the Android client and this file agree on.
  - `PlayIntegrityVerifier.VerifyAssertion(ctx, publicKeyDER, assertion, clientDataHash []byte, prevCounter uint32) (newCounter uint32, err error)` — implements half of `attest.Verifier`; the `PlayIntegrityVerifier` struct itself and its `VerifyAttestation` half are added in Task 4.

- [ ] **Step 1: Write the failing tests**

```go
package attest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func generateTestKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	return priv, pubDER
}

func signTestAssertion(t *testing.T, priv *ecdsa.PrivateKey, clientDataHash []byte, counter uint32) []byte {
	t.Helper()
	var counterBytes [4]byte
	binary.BigEndian.PutUint32(counterBytes[:], counter)
	h := sha256.New()
	h.Write(clientDataHash)
	h.Write(counterBytes[:])
	message := h.Sum(nil)

	sig, err := ecdsa.SignASN1(rand.Reader, priv, message)
	if err != nil {
		t.Fatalf("ecdsa.SignASN1: %v", err)
	}
	body, err := json.Marshal(androidAssertion{Counter: counter, Signature: sig})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return body
}

func TestPlayIntegrityVerifier_VerifyAssertion_valid(t *testing.T) {
	priv, pubDER := generateTestKey(t)
	clientDataHash := sha256.Sum256([]byte("challenge-bytes"))
	assertion := signTestAssertion(t, priv, clientDataHash[:], 5)

	v := &PlayIntegrityVerifier{}
	newCounter, err := v.VerifyAssertion(t.Context(), pubDER, assertion, clientDataHash[:], 4)
	if err != nil {
		t.Fatalf("VerifyAssertion: %v", err)
	}
	if newCounter != 5 {
		t.Errorf("newCounter = %d, want 5", newCounter)
	}
}

func TestPlayIntegrityVerifier_VerifyAssertion_counterNotIncreasing(t *testing.T) {
	priv, pubDER := generateTestKey(t)
	clientDataHash := sha256.Sum256([]byte("challenge-bytes"))
	assertion := signTestAssertion(t, priv, clientDataHash[:], 4)

	v := &PlayIntegrityVerifier{}
	if _, err := v.VerifyAssertion(t.Context(), pubDER, assertion, clientDataHash[:], 4); err == nil {
		t.Fatal("VerifyAssertion accepted counter 4 after prevCounter 4; replay protection is broken")
	}
}

func TestPlayIntegrityVerifier_VerifyAssertion_wrongKeySignature(t *testing.T) {
	_, pubDER := generateTestKey(t)
	otherPriv, _ := generateTestKey(t)
	clientDataHash := sha256.Sum256([]byte("challenge-bytes"))
	assertion := signTestAssertion(t, otherPriv, clientDataHash[:], 5) // signed by the WRONG key

	v := &PlayIntegrityVerifier{}
	if _, err := v.VerifyAssertion(t.Context(), pubDER, assertion, clientDataHash[:], 4); err == nil {
		t.Fatal("VerifyAssertion accepted a signature from a key that does not match publicKeyDER")
	}
}

func TestPlayIntegrityVerifier_VerifyAssertion_malformedJSON(t *testing.T) {
	_, pubDER := generateTestKey(t)
	clientDataHash := sha256.Sum256([]byte("challenge-bytes"))

	v := &PlayIntegrityVerifier{}
	if _, err := v.VerifyAssertion(t.Context(), pubDER, []byte("not json"), clientDataHash[:], 4); err == nil {
		t.Fatal("VerifyAssertion accepted malformed assertion bytes")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/attest/... -run TestPlayIntegrityVerifier_VerifyAssertion -v`
Expected: FAIL — `PlayIntegrityVerifier` and `androidAssertion` undefined.

- [ ] **Step 3: Implement**

```go
package attest

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

// PlayIntegrityVerifier verifies Android devices attested via the Play
// Integrity API. It implements Verifier, fails closed on any deviation, and
// mirrors AppleVerifier's structure: VerifyAttestation (see playintegrity.go)
// establishes trust in a device-generated key once; VerifyAssertion (here)
// checks that key's signature on every later request.
//
// Unlike App Attest, Android has no OS-level per-request signed-assertion
// format, so this file defines a minimal one both this server and the
// Android client agree on: a JSON envelope carrying a strictly-increasing
// counter and an ECDSA-P256 signature over
// SHA256(clientDataHash || big-endian-uint32(counter)).
type PlayIntegrityVerifier struct{}

// androidAssertion is the wire format signTestAssertion in this file's tests
// constructs and the real Android client must produce identically.
type androidAssertion struct {
	Counter   uint32 `json:"counter"`
	Signature []byte `json:"signature"`
}

// VerifyAssertion implements Verifier for Android's assertion format.
func (v *PlayIntegrityVerifier) VerifyAssertion(ctx context.Context, publicKeyDER, assertion, clientDataHash []byte, prevCounter uint32) (uint32, error) {
	var obj androidAssertion
	if err := json.Unmarshal(assertion, &obj); err != nil {
		return 0, fmt.Errorf("%w: decode assertion: %v", ErrAttestationInvalid, err)
	}
	if len(obj.Signature) == 0 {
		return 0, fmt.Errorf("%w: assertion missing signature", ErrAttestationInvalid)
	}
	if obj.Counter <= prevCounter {
		return 0, fmt.Errorf("%w: sign counter %d not greater than previous %d", ErrAttestationInvalid, obj.Counter, prevCounter)
	}

	pub, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return 0, fmt.Errorf("%w: parse public key: %v", ErrAttestationInvalid, err)
	}
	ecdsaPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return 0, fmt.Errorf("%w: stored public key is not ECDSA", ErrAttestationInvalid)
	}

	var counterBytes [4]byte
	binary.BigEndian.PutUint32(counterBytes[:], obj.Counter)
	h := sha256.New()
	h.Write(clientDataHash)
	h.Write(counterBytes[:])
	message := h.Sum(nil)

	if !ecdsa.VerifyASN1(ecdsaPub, message, obj.Signature) {
		return 0, fmt.Errorf("%w: assertion signature invalid", ErrAttestationInvalid)
	}

	return obj.Counter, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/attest/... -run TestPlayIntegrityVerifier_VerifyAssertion -v`
Expected: PASS, all four cases.

- [ ] **Step 5: Commit**

```bash
git add internal/attest/android.go internal/attest/android_test.go
git commit -m "feat(attest): add Android assertion wire format and verification"
```

---

### Task 4: Play Integrity token verification (the network-calling half)

**Files:**
- Create: `internal/attest/playintegrity.go`
- Create: `internal/attest/playintegrity_test.go`
- Modify: `internal/attest/android.go` (add fields to `PlayIntegrityVerifier`, constructed by `NewPlayIntegrityVerifier`)

**Interfaces:**
- Consumes: `androidAssertion` types are unaffected; this task only adds the attestation half.
- Produces:
  - `NewPlayIntegrityVerifier(packageName string, decoder integrityTokenDecoder) *PlayIntegrityVerifier`
  - `PlayIntegrityVerifier.VerifyAttestation(ctx, keyID string, attestationEnvelope, challenge []byte) (publicKeyDER, receipt []byte, err error)`
  - `type integrityTokenDecoder interface { Decode(ctx context.Context, integrityToken string) (*integrityVerdict, error) }` — the seam that lets tests fake Google's API without a real network call. The production implementation (`googlePlayIntegrityDecoder`, calling `playintegrity.googleapis.com`) is real but only exercised by an integration-style test gated the same way `SPAMFILTER_INTEGRATION_BASE_URL` gates existing integration tests — see Step 6.
  - `type integrityVerdict struct { PackageName string; RequestHash string; AppRecognitionVerdict string; DeviceRecognitionVerdicts []string }` — the subset of Google's decoded-token fields this verifier checks.

- [ ] **Step 1: Write the failing tests**

```go
package attest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
)

// fakeDecoder is a integrityTokenDecoder test double: it returns whatever
// verdict was configured, keyed by the token string it was handed, so a test
// can simulate Google returning a verdict for one token and an error for
// another within the same test.
type fakeDecoder struct {
	verdicts map[string]*integrityVerdict
	err      error
}

func (f *fakeDecoder) Decode(ctx context.Context, integrityToken string) (*integrityVerdict, error) {
	if f.err != nil {
		return nil, f.err
	}
	v, ok := f.verdicts[integrityToken]
	if !ok {
		return nil, errors.New("fakeDecoder: no verdict configured for this token")
	}
	return v, nil
}

func androidAttestationEnvelope(t *testing.T, integrityToken string, pubDER []byte) []byte {
	t.Helper()
	body, err := json.Marshal(struct {
		IntegrityToken string `json:"integrity_token"`
		PublicKeyDER   string `json:"public_key_der"`
	}{
		IntegrityToken: integrityToken,
		PublicKeyDER:   base64.StdEncoding.EncodeToString(pubDER),
	})
	if err != nil {
		t.Fatalf("json.Marshal envelope: %v", err)
	}
	return body
}

func TestPlayIntegrityVerifier_VerifyAttestation_valid(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	challenge := []byte("server-challenge-bytes")
	wantNonce := sha256.Sum256(append(append([]byte{}, challenge...), pubDER...))

	decoder := &fakeDecoder{verdicts: map[string]*integrityVerdict{
		"good-token": {
			PackageName:               "com.hushield.android",
			RequestHash:               base64.StdEncoding.EncodeToString(wantNonce[:]),
			AppRecognitionVerdict:     "PLAY_RECOGNIZED",
			DeviceRecognitionVerdicts: []string{"MEETS_DEVICE_INTEGRITY"},
		},
	}}
	v := NewPlayIntegrityVerifier("com.hushield.android", decoder)

	gotPubDER, _, err := v.VerifyAttestation(t.Context(), "unused-keyid", androidAttestationEnvelope(t, "good-token", pubDER), challenge)
	if err != nil {
		t.Fatalf("VerifyAttestation: %v", err)
	}
	if string(gotPubDER) != string(pubDER) {
		t.Errorf("returned public key does not match the one submitted")
	}
}

func TestPlayIntegrityVerifier_VerifyAttestation_nonceMismatch(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	wrongNonce := sha256.Sum256([]byte("this does not match the challenge+key"))

	decoder := &fakeDecoder{verdicts: map[string]*integrityVerdict{
		"bad-token": {
			PackageName:               "com.hushield.android",
			RequestHash:               base64.StdEncoding.EncodeToString(wrongNonce[:]),
			AppRecognitionVerdict:     "PLAY_RECOGNIZED",
			DeviceRecognitionVerdicts: []string{"MEETS_DEVICE_INTEGRITY"},
		},
	}}
	v := NewPlayIntegrityVerifier("com.hushield.android", decoder)

	_, _, err := v.VerifyAttestation(t.Context(), "unused-keyid", androidAttestationEnvelope(t, "bad-token", pubDER), []byte("server-challenge-bytes"))
	if err == nil {
		t.Fatal("VerifyAttestation accepted a token whose nonce does not bind to the submitted public key")
	}
}

func TestPlayIntegrityVerifier_VerifyAttestation_wrongPackageName(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	challenge := []byte("server-challenge-bytes")
	nonce := sha256.Sum256(append(append([]byte{}, challenge...), pubDER...))

	decoder := &fakeDecoder{verdicts: map[string]*integrityVerdict{
		"impostor-token": {
			PackageName:               "com.attacker.evil",
			RequestHash:               base64.StdEncoding.EncodeToString(nonce[:]),
			AppRecognitionVerdict:     "PLAY_RECOGNIZED",
			DeviceRecognitionVerdicts: []string{"MEETS_DEVICE_INTEGRITY"},
		},
	}}
	v := NewPlayIntegrityVerifier("com.hushield.android", decoder)

	_, _, err := v.VerifyAttestation(t.Context(), "unused-keyid", androidAttestationEnvelope(t, "impostor-token", pubDER), challenge)
	if err == nil {
		t.Fatal("VerifyAttestation accepted a token issued for a different package name")
	}
}

func TestPlayIntegrityVerifier_VerifyAttestation_weakIntegrityVerdict(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	challenge := []byte("server-challenge-bytes")
	nonce := sha256.Sum256(append(append([]byte{}, challenge...), pubDER...))

	decoder := &fakeDecoder{verdicts: map[string]*integrityVerdict{
		"rooted-device-token": {
			PackageName:               "com.hushield.android",
			RequestHash:               base64.StdEncoding.EncodeToString(nonce[:]),
			AppRecognitionVerdict:     "PLAY_RECOGNIZED",
			DeviceRecognitionVerdicts: []string{}, // no integrity claim at all -- e.g. a rooted/tampered device
		},
	}}
	v := NewPlayIntegrityVerifier("com.hushield.android", decoder)

	_, _, err := v.VerifyAttestation(t.Context(), "unused-keyid", androidAttestationEnvelope(t, "rooted-device-token", pubDER), challenge)
	if err == nil {
		t.Fatal("VerifyAttestation accepted a token with no MEETS_*_INTEGRITY verdict")
	}
}

func TestPlayIntegrityVerifier_VerifyAttestation_decoderError(t *testing.T) {
	decoder := &fakeDecoder{err: errors.New("google api: token expired")}
	v := NewPlayIntegrityVerifier("com.hushield.android", decoder)

	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)

	_, _, err := v.VerifyAttestation(t.Context(), "unused-keyid", androidAttestationEnvelope(t, "expired-token", pubDER), []byte("challenge"))
	if err == nil {
		t.Fatal("VerifyAttestation swallowed a decoder error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/attest/... -run TestPlayIntegrityVerifier_VerifyAttestation -v`
Expected: FAIL — `NewPlayIntegrityVerifier`, `integrityVerdict`, `integrityTokenDecoder` undefined.

- [ ] **Step 3: Add the decoder interface and verdict type, and wire the struct fields**

Modify `internal/attest/android.go`'s `PlayIntegrityVerifier` struct and add a constructor:

```go
// PlayIntegrityVerifier verifies Android devices attested via the Play
// Integrity API. See VerifyAttestation in playintegrity.go and
// VerifyAssertion above.
type PlayIntegrityVerifier struct {
	packageName string
	decoder     integrityTokenDecoder
}

// NewPlayIntegrityVerifier constructs a PlayIntegrityVerifier for the given
// Android package name, decoding tokens via decoder (the real
// googlePlayIntegrityDecoder in production, a fake in tests).
func NewPlayIntegrityVerifier(packageName string, decoder integrityTokenDecoder) *PlayIntegrityVerifier {
	return &PlayIntegrityVerifier{packageName: packageName, decoder: decoder}
}
```

(`VerifyAssertion`'s body from Task 3 is unaffected by this struct now having fields; it never referenced `v.packageName` or `v.decoder`.)

- [ ] **Step 4: Implement `playintegrity.go`**

```go
package attest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// integrityVerdict is the subset of Google's decoded Play Integrity token
// this verifier checks. See
// https://developer.android.com/google/play/integrity/verdict for the full
// shape; fields not needed for a pass/fail decision here are omitted.
type integrityVerdict struct {
	PackageName               string
	RequestHash               string
	AppRecognitionVerdict     string
	DeviceRecognitionVerdicts []string
}

// integrityTokenDecoder decodes a raw Play Integrity token into its verdict.
// The seam that lets tests avoid a real call to Google's API.
type integrityTokenDecoder interface {
	Decode(ctx context.Context, integrityToken string) (*integrityVerdict, error)
}

// androidAttestationEnvelope is what the Android client sends as
// VerifyAttestation's attestationEnvelope argument: the raw Play Integrity
// token plus the client's own public key, base64-encoded. Google's token has
// no way to carry an arbitrary application-defined public key itself, so this
// envelope is this server's own wire format, not Google's.
type androidAttestationEnvelope struct {
	IntegrityToken string `json:"integrity_token"`
	PublicKeyDER   string `json:"public_key_der"`
}

// hasIntegrityVerdict reports whether verdicts contains any recognized
// "meets integrity" value. Google documents several tiers
// (MEETS_DEVICE_INTEGRITY, MEETS_BASIC_INTEGRITY, MEETS_STRONG_INTEGRITY,
// MEETS_VIRTUAL_INTEGRITY); this verifier accepts any non-empty verdict list
// as a v1 floor and defers tightening to a specific tier to a follow-up once
// real device data shows what legitimate users actually present.
func hasIntegrityVerdict(verdicts []string) bool {
	return len(verdicts) > 0
}

// VerifyAttestation implements Verifier for Android's Play Integrity flow.
func (v *PlayIntegrityVerifier) VerifyAttestation(ctx context.Context, keyID string, attestationEnvelope, challenge []byte) ([]byte, []byte, error) {
	var envelope androidAttestationEnvelope
	if err := json.Unmarshal(attestationEnvelope, &envelope); err != nil {
		return nil, nil, fmt.Errorf("%w: decode attestation envelope: %v", ErrAttestationInvalid, err)
	}

	pubDER, err := base64.StdEncoding.DecodeString(envelope.PublicKeyDER)
	if err != nil || len(pubDER) == 0 {
		return nil, nil, fmt.Errorf("%w: public_key_der must be valid non-empty base64", ErrAttestationInvalid)
	}

	verdict, err := v.decoder.Decode(ctx, envelope.IntegrityToken)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: decode integrity token: %v", ErrAttestationInvalid, err)
	}

	if verdict.PackageName != v.packageName {
		return nil, nil, fmt.Errorf("%w: token package name %q does not match %q", ErrAttestationInvalid, verdict.PackageName, v.packageName)
	}

	wantNonce := sha256.Sum256(append(append([]byte{}, challenge...), pubDER...))
	gotNonce, err := base64.StdEncoding.DecodeString(verdict.RequestHash)
	if err != nil || !bytes.Equal(gotNonce, wantNonce[:]) {
		return nil, nil, fmt.Errorf("%w: token nonce does not bind to challenge and public key", ErrAttestationInvalid)
	}

	if verdict.AppRecognitionVerdict != "PLAY_RECOGNIZED" {
		return nil, nil, fmt.Errorf("%w: app recognition verdict %q", ErrAttestationInvalid, verdict.AppRecognitionVerdict)
	}
	if !hasIntegrityVerdict(verdict.DeviceRecognitionVerdicts) {
		return nil, nil, fmt.Errorf("%w: device recognition verdict does not meet any integrity tier", ErrAttestationInvalid)
	}

	// The verified token itself is the receipt, mirroring what AppleVerifier
	// stores from obj.AttStmt.Receipt -- an auditable record of what was
	// verified, without being PII (it describes the app/device, not a person).
	return pubDER, attestationEnvelope, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/attest/... -run TestPlayIntegrityVerifier -v`
Expected: PASS, all cases from both Task 3 and this task.

- [ ] **Step 6: Add the real Google-calling decoder (network path, not unit-tested here)**

Add to `playintegrity.go`:

```go
// googlePlayIntegrityDecoder calls Google's Play Integrity API to decode a
// token. This talks to a real Google endpoint and is exercised by a manual
// or integration test gated on a live service-account credential, mirroring
// how SpamFilterKitTests/IntegrationTests self-skips without
// SPAMFILTER_INTEGRATION_BASE_URL -- there is no way to unit-test a real
// Google API call, and faking the HTTP layer would only test the fake.
type googlePlayIntegrityDecoder struct {
	httpClient *http.Client
	tokenSource oauth2.TokenSource
}

func newGooglePlayIntegrityDecoder(credentialsPath string) (*googlePlayIntegrityDecoder, error) {
	ctx := context.Background()
	creds, err := google.CredentialsFromJSONWithParams(ctx, mustReadFile(credentialsPath), google.CredentialsParams{
		Scopes: []string{"https://www.googleapis.com/auth/playintegrity"},
	})
	if err != nil {
		return nil, fmt.Errorf("attest: loading Play Integrity credentials: %w", err)
	}
	return &googlePlayIntegrityDecoder{httpClient: http.DefaultClient, tokenSource: creds.TokenSource}, nil
}

func (d *googlePlayIntegrityDecoder) Decode(ctx context.Context, integrityToken string) (*integrityVerdict, error) {
	// Full implementation: POST to
	// https://playintegrity.googleapis.com/v1/{packageName}:decodeIntegrityToken
	// with {"integrity_token": integrityToken}, authenticated via
	// d.tokenSource, and unmarshal the tokenPayloadExternal response shape
	// into integrityVerdict. Left for the implementer to fill in against
	// Google's current API reference at implementation time -- this plan's
	// job is the verification LOGIC (Steps 1-5 above, which are fully
	// specified and tested), not transcribing an HTTP client against docs
	// that may have shifted by the time this is built.
	return nil, fmt.Errorf("attest: googlePlayIntegrityDecoder.Decode not yet implemented")
}
```

This step is the one deliberate exception to "no placeholders" in this plan: it is calling a live third-party API whose exact request/response shape should be pulled from Google's current documentation at implementation time (via the `context7` MCP tool if available, or Google's published API reference), not transcribed from training data that may be stale. Steps 1-5 (the logic this server controls and can test) are fully specified; this step is infrastructure around them. Do not ship `ATTEST_MODE=android` or `both` to production until this is filled in and manually verified against a real Play Integrity token.

- [ ] **Step 7: Commit**

```bash
git add internal/attest/playintegrity.go internal/attest/playintegrity_test.go internal/attest/android.go
git commit -m "feat(attest): verify Android Play Integrity attestation tokens"
```

---

### Task 5: Config — accept `ATTEST_MODE=android` and `both`

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `Config.AttestMode` now accepts `"mock"`, `"apple"`, `"android"`, `"both"`.
  - `Config.AndroidPackageName string` (env `ANDROID_PACKAGE_NAME`), required when `AttestMode` is `"android"` or `"both"`.
  - `Config.PlayIntegrityCredentialsPath string` (env `PLAY_INTEGRITY_CREDENTIALS_PATH`), required when `AttestMode` is `"android"` or `"both"`.

- [ ] **Step 1: Write the failing tests**

Check `internal/config/config_test.go`'s existing test structure for `ATTEST_MODE=apple` validation first (`grep -n "AttestMode\|APP_ID" internal/config/config_test.go`) and match its style, including its `strongSecret`/`strongAdminToken` test constants (already defined near the top of that file) for `DEVICE_TOKEN_SECRET`/`ADMIN_TOKEN`. Add:

```go
func TestLoad_attestModeAndroid_requiresPackageNameAndCredentials(t *testing.T) {
	t.Setenv("ATTEST_MODE", "android")
	t.Setenv("ANDROID_PACKAGE_NAME", "")
	t.Setenv("PLAY_INTEGRITY_CREDENTIALS_PATH", "")
	t.Setenv("DEVICE_TOKEN_SECRET", strongSecret)
	t.Setenv("ADMIN_TOKEN", strongAdminToken)
	if _, err := Load(); err == nil {
		t.Fatal("Load succeeded with ATTEST_MODE=android but no ANDROID_PACKAGE_NAME/PLAY_INTEGRITY_CREDENTIALS_PATH")
	}
}

func TestLoad_attestModeAndroid_valid(t *testing.T) {
	t.Setenv("ATTEST_MODE", "android")
	t.Setenv("ANDROID_PACKAGE_NAME", "com.hushield.android")
	t.Setenv("PLAY_INTEGRITY_CREDENTIALS_PATH", "/tmp/creds.json")
	t.Setenv("DEVICE_TOKEN_SECRET", strongSecret)
	t.Setenv("ADMIN_TOKEN", strongAdminToken)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AttestMode != "android" {
		t.Errorf("AttestMode = %q, want %q", cfg.AttestMode, "android")
	}
}

func TestLoad_attestModeAndroid_stillRequiresDeviceTokenSecret(t *testing.T) {
	t.Setenv("ATTEST_MODE", "android")
	t.Setenv("ANDROID_PACKAGE_NAME", "com.hushield.android")
	t.Setenv("PLAY_INTEGRITY_CREDENTIALS_PATH", "/tmp/creds.json")
	t.Setenv("DEVICE_TOKEN_SECRET", "")
	t.Setenv("ADMIN_TOKEN", strongAdminToken)
	if _, err := Load(); err == nil {
		t.Fatal("Load succeeded with ATTEST_MODE=android and no DEVICE_TOKEN_SECRET -- production secret checks must not be apple-only")
	}
}

func TestLoad_attestModeBoth_requiresBothApplePlatformsConfigured(t *testing.T) {
	t.Setenv("ATTEST_MODE", "both")
	t.Setenv("APP_ID", "")
	t.Setenv("ANDROID_PACKAGE_NAME", "com.hushield.android")
	t.Setenv("PLAY_INTEGRITY_CREDENTIALS_PATH", "/tmp/creds.json")
	t.Setenv("DEVICE_TOKEN_SECRET", strongSecret)
	t.Setenv("ADMIN_TOKEN", strongAdminToken)
	if _, err := Load(); err == nil {
		t.Fatal("Load succeeded with ATTEST_MODE=both but no APP_ID")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/... -run 'TestLoad_attestModeAndroid|TestLoad_attestModeBoth' -v`
Expected: FAIL — `ATTEST_MODE=android` is rejected by `validAttestModes` today.

- [ ] **Step 3: Implement**

In `internal/config/config.go`:

```go
var validAttestModes = map[string]bool{
	"mock":    true,
	"apple":   true,
	"android": true,
	"both":    true,
}
```

Add fields to `Config`:

```go
	// AndroidPackageName is the Android application ID Play Integrity tokens
	// must be issued for. Required when AttestMode is "android" or "both".
	AndroidPackageName string
	// PlayIntegrityCredentialsPath is a path to a Google service-account JSON
	// credentials file scoped for the Play Integrity API. Required when
	// AttestMode is "android" or "both".
	PlayIntegrityCredentialsPath string
```

Load them the same way `AppID` is loaded (find the exact `getEnv` call site for `AppID` and mirror it):

```go
		AndroidPackageName:           getEnv("ANDROID_PACKAGE_NAME", ""),
		PlayIntegrityCredentialsPath: getEnv("PLAY_INTEGRITY_CREDENTIALS_PATH", ""),
```

The existing validation block is:

```go
	if cfg.AttestMode == "apple" {
		if cfg.AppID == "" {
			return Config{}, fmt.Errorf("config: APP_ID is required when ATTEST_MODE=apple")
		}
		if err := validateSecret("DEVICE_TOKEN_SECRET", cfg.DeviceTokenSecret); err != nil {
			return Config{}, err
		}
		if err := validateSecret("ADMIN_TOKEN", cfg.AdminToken); err != nil {
			return Config{}, err
		}
	}
```

Note the `validateSecret` calls: they gate production secret hardening on "a real verifier is active," not specifically on "apple." An `android`-only deployment needs those same checks — skipping them because the mode string isn't literally `"apple"` would be a real security regression, not a cosmetic gap. Replace the whole block with:

```go
	if cfg.AttestMode == "apple" || cfg.AttestMode == "both" {
		if cfg.AppID == "" {
			return Config{}, fmt.Errorf("config: ATTEST_MODE=%q requires APP_ID", cfg.AttestMode)
		}
	}
	if cfg.AttestMode == "android" || cfg.AttestMode == "both" {
		if cfg.AndroidPackageName == "" {
			return Config{}, fmt.Errorf("config: ATTEST_MODE=%q requires ANDROID_PACKAGE_NAME", cfg.AttestMode)
		}
		if cfg.PlayIntegrityCredentialsPath == "" {
			return Config{}, fmt.Errorf("config: ATTEST_MODE=%q requires PLAY_INTEGRITY_CREDENTIALS_PATH", cfg.AttestMode)
		}
	}
	if cfg.AttestMode != "mock" {
		if err := validateSecret("DEVICE_TOKEN_SECRET", cfg.DeviceTokenSecret); err != nil {
			return Config{}, err
		}
		if err := validateSecret("ADMIN_TOKEN", cfg.AdminToken); err != nil {
			return Config{}, err
		}
	}
```

`validateSecret`'s own error strings hardcode `"when ATTEST_MODE=apple"` (see its definition, `internal/config/config.go:110-121`) — now misleading when triggered from `android`-only mode. Change both occurrences of `"when ATTEST_MODE=apple"` inside `validateSecret` to `"when ATTEST_MODE is not mock"`.

Update the error message in the `!validAttestModes[cfg.AttestMode]` branch to list all four accepted values instead of just `"mock"`/`"apple"`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS, including every pre-existing test in this package.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): accept ATTEST_MODE=android and ATTEST_MODE=both"
```

---

### Task 6: API — per-request platform dispatch for enrollment, stored-platform dispatch for assertion

This is the task that makes iOS and Android devices coexist against one running server, per this plan's Architecture section — not a deployment-time mode switch, a per-request/per-device lookup.

**Files:**
- Modify: `internal/api/router.go`
- Modify: `internal/api/attest.go`
- Test: `internal/api/attest_test.go`

**Interfaces:**
- Consumes: `store.GetDeviceByKeyID` (4 return values, Task 2), `store.UpsertDevicePlatform` (Task 2), `config.Config.AndroidPackageName`/`PlayIntegrityCredentialsPath` (Task 5), `attest.NewPlayIntegrityVerifier` (Task 4).
- Produces:
  - `buildVerifiers(cfg config.Config) map[string]attest.Verifier` — replaces `buildVerifier`. Keys are exactly `"apple"` and `"android"`.
  - `attestHandler.verifiers map[string]attest.Verifier` — replaces the single `verifier` field.
  - `verifyRequest.Platform string` (json tag `platform`) — new field; empty string means `"apple"` (back-compat with every iOS client shipped before this change, which never sends this field).

- [ ] **Step 1: Write the failing tests**

Check `internal/api/attest_test.go`'s existing `TestHandleVerify_*` tests for how `attestHandler` is constructed in tests (`grep -n "attestHandler{" internal/api/attest_test.go`) and match that setup exactly. Add:

```go
func TestHandleVerify_defaultsToApplePlatformWhenFieldOmitted(t *testing.T) {
	// Construct attestHandler as the existing tests do, but with
	// verifiers: map[string]attest.Verifier{
	//     "apple":   attest.NewMockVerifier([]byte("mock-apple-pubkey"), nil),
	//     "android": attest.NewMockVerifier([]byte("mock-android-pubkey"), nil),
	// }
	// POST /api/v1/attest/verify with a body that has NO "platform" field at
	// all (marshal a struct without that field, or a raw JSON literal), and
	// assert: 200 OK, and the persisted device's platform (read back via
	// store.GetDeviceByKeyID) is "apple".
}

func TestHandleVerify_androidPlatformSelectsAndroidVerifier(t *testing.T) {
	// Same setup. POST with {"platform":"android", ...}. Assert: the
	// "android" MockVerifier was used (its returned public key,
	// "mock-android-pubkey", is what got persisted -- NOT the apple one),
	// and the persisted device's platform is "android".
}

func TestHandleVerify_unknownPlatformRejected(t *testing.T) {
	// POST with {"platform":"windows", ...}. Assert: 400, and no device row
	// was created (query store.GetDeviceByKeyID for the submitted key_id and
	// confirm store.ErrDeviceNotFound).
}

func TestHandleAssert_usesDevicesStoredPlatformNotAnyClientClaim(t *testing.T) {
	// Enroll a device as "android" via handleVerify first (so a real row
	// with platform="android" and the android MockVerifier's public key
	// exists). Then POST /api/v1/attest/assert for that key_id -- assertRequest
	// has no platform field at all, by design, so there is nothing for a
	// client to spoof here. Assert: the ANDROID verifier's VerifyAssertion
	// was called (set a distinguishing NewCounter on each MockVerifier and
	// check the response's implied counter, or use a spy Verifier that
	// records which one was invoked), not the apple one.
}
```

(These are written as structured comments describing the exact assertion, not runnable code, because they depend on this test file's specific existing helpers for constructing requests/responses and inspecting `MockVerifier` calls -- read `attest_test.go`'s current tests for `handleVerify`/`handleAssert` first and write these four using the same helpers, same style, same level of detail as the existing tests in that file.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/... -run 'TestHandleVerify_default|TestHandleVerify_android|TestHandleVerify_unknown|TestHandleAssert_usesDevicesStoredPlatform' -v`
Expected: FAIL to compile — `verifyRequest` has no `Platform` field yet, `attestHandler` has no `verifiers` field yet.

- [ ] **Step 3: Implement `internal/api/attest.go`**

```go
type attestHandler struct {
	db           *sql.DB
	store        attest.ChallengeStore
	verifiers    map[string]attest.Verifier
	signer       *token.Signer
	challengeTTL time.Duration
	tokenTTL     time.Duration
	now          func() time.Time
}
```

```go
type verifyRequest struct {
	KeyID       string `json:"key_id"`
	Attestation string `json:"attestation"`
	Challenge   string `json:"challenge"`
	// Platform selects which Verifier checks Attestation: "apple" or
	// "android". Empty defaults to "apple" -- every iOS client shipped
	// before this field existed never sends it, and must keep working
	// identically.
	Platform string `json:"platform"`
}
```

In `handleVerify`, after the existing `key_id`/`seed:` checks and before decoding `attBytes`:

```go
	platform := body.Platform
	if platform == "" {
		platform = "apple"
	}
	verifier, ok := h.verifiers[platform]
	if !ok {
		WriteError(w, http.StatusBadRequest, requestID,
			APIError{Field: "platform", Message: fmt.Sprintf("unknown platform %q", platform), Code: "bad_request"})
		return
	}
```

Change the `h.verifier.VerifyAttestation(...)` call to `verifier.VerifyAttestation(...)`.

Change the `upsertDevice(...)` call to pass `platform` through, and change `upsertDevice` itself to call the new store function instead of building SQL inline:

```go
func upsertDevice(ctx context.Context, db *sql.DB, keyID string, publicKey, receipt []byte, platform string, now time.Time) (uint64, error) {
	if db == nil {
		return 0, errors.New("api: nil database handle")
	}
	return store.UpsertDevicePlatform(ctx, db, keyID, publicKey, receipt, platform, now)
}
```

(This makes `upsertDevice` a thin pass-through. Consider deleting it entirely and calling `store.UpsertDevicePlatform` directly from `handleVerify`, matching how `handleAssert` already calls `store.GetDeviceByKeyID` and `store.UpdateDeviceSignCount` directly with no wrapper -- check whether `upsertDevice` is called from anywhere else in this package first; if not, delete it and call the store function directly for consistency with the rest of the file.)

In `handleAssert`, the existing call:

```go
	deviceID, pubDER, signCount, err := store.GetDeviceByKeyID(r.Context(), h.db, body.KeyID)
```

becomes:

```go
	deviceID, pubDER, signCount, platform, err := store.GetDeviceByKeyID(r.Context(), h.db, body.KeyID)
```

and immediately after the existing `ErrDeviceNotFound`/generic-error handling for that call, before the `clientDataHash`/`VerifyAssertion` block:

```go
	verifier, ok := h.verifiers[platform]
	if !ok {
		// A device's stored platform not matching any configured verifier
		// means server config changed after this device enrolled (e.g. the
		// "android" verifier was removed from ATTEST_MODE). Fail closed
		// rather than silently picking a verifier the device never attested
		// against.
		logInternalError(requestID, "assert", fmt.Errorf("device %d has unconfigured platform %q", deviceID, platform))
		WriteError(w, http.StatusInternalServerError, requestID,
			APIError{Message: "device platform not available", Code: "internal_error"})
		return
	}
```

Change the `h.verifier.VerifyAssertion(...)` call to `verifier.VerifyAssertion(...)`.

- [ ] **Step 4: Implement `internal/api/router.go`**

```go
// buildVerifiers selects the App Attest and Play Integrity verifiers per
// config, keyed by platform. Each entry independently falls back to a
// MockVerifier when its real mode isn't enabled, so ATTEST_MODE=apple
// behaves exactly as it did before Android support existed: "apple" is real,
// "android" is mock (and simply unreachable in practice, since no real
// client will send platform:"android" against a server that never issued it
// that option).
func buildVerifiers(cfg config.Config) map[string]attest.Verifier {
	// ATTEST_MODE=mock is the one case where "accepts anything" is the
	// point (local dev/tests). Every other mode must fail closed: a
	// platform this deployment did not enable must be ABSENT from the map,
	// not backed by a mock. attestHandler already 400s a platform key it
	// doesn't find (see handleVerify's `verifiers[platform]` lookup) --
	// that "unknown platform" rejection is exactly the behavior an
	// unconfigured platform needs. Falling back to a permissive
	// MockVerifier here instead would mean an ATTEST_MODE=android
	// production deployment still accepts a forged platform:"apple"
	// attestation, since the mock verifier accepts anything -- the
	// opposite of "fails closed."
	if cfg.AttestMode == "mock" {
		return map[string]attest.Verifier{
			"apple":   attest.NewMockVerifier([]byte("mock-public-key-der"), nil),
			"android": attest.NewMockVerifier([]byte("mock-public-key-der"), nil),
		}
	}

	verifiers := map[string]attest.Verifier{}
	if cfg.AttestMode == "apple" || cfg.AttestMode == "both" {
		verifiers["apple"] = attest.NewAppleVerifier(cfg.AppID, attest.DefaultAppleRoots())
	}
	if cfg.AttestMode == "android" || cfg.AttestMode == "both" {
		decoder, err := newGooglePlayIntegrityDecoder(cfg.PlayIntegrityCredentialsPath)
		if err != nil {
			panic(fmt.Sprintf("config: ATTEST_MODE=%q but Play Integrity credentials failed to load: %v", cfg.AttestMode, err))
		}
		verifiers["android"] = attest.NewPlayIntegrityVerifier(cfg.AndroidPackageName, decoder)
	}
	return verifiers
}
```

Delete `buildVerifier` (singular) and change its one call site in `NewRouter` from `verifier: buildVerifier(cfg),` to `verifiers: buildVerifiers(cfg),`.

Add one more test to Step 1 (before Step 2's test run) covering exactly this:

```go
func TestHandleVerify_androidPlatformRejectedWhenAttestModeIsAppleOnly(t *testing.T) {
	// Construct attestHandler with verifiers built by buildVerifiers(cfg)
	// where cfg.AttestMode == "apple" (so verifiers == {"apple": <real or
	// test AppleVerifier>} with no "android" key at all). POST
	// {"platform":"android", ...}. Assert: 400, same as
	// TestHandleVerify_unknownPlatformRejected -- an android request against
	// an apple-only deployment must be rejected, not silently accepted by a
	// permissive fallback.
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/... -race -v 2>&1 | tail -80`
Expected: PASS — every existing test in this package (which all implicitly use `ATTEST_MODE` unset, i.e. mock, and never send `platform`) plus the four new ones.

- [ ] **Step 6: Run the full suite**

Run: `DB_DSN='root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true' go test ./... -race -cover 2>&1 | tail -40`
Expected: every package still green, matching the baseline before this plan started.

- [ ] **Step 7: Commit**

```bash
git add internal/api/attest.go internal/api/router.go internal/api/attest_test.go
git commit -m "feat(api): dispatch attestation verification by device platform"
```

---

### Task 7: FCM push sender and platform-dispatching notifier

**Files:**
- Create: `internal/push/fcm.go`
- Create: `internal/push/fcm_test.go`
- Create: `internal/push/platform.go`
- Create: `internal/push/platform_test.go`
- Modify: `internal/config/config.go` (one new field)
- Modify: `cmd/recompute/main.go`

**Interfaces:**
- Consumes: `store.PushTarget.Platform` (Task 2).
- Produces:
  - `FCMNotifier` implementing `push.Notifier` (`SendSilentRefresh(ctx, target store.PushTarget) error`).
  - `PlatformNotifier{APNs push.Notifier; FCM push.Notifier}` implementing `push.Notifier`, dispatching on `target.Platform`.

- [ ] **Step 1: Read the existing APNs sender and its tests for the pattern to mirror**

Run: `cat internal/push/apns.go internal/push/apns_test.go` before writing anything — `FCMNotifier` should mirror `APNsNotifier`'s shape (a struct holding an `http.Client` and credentials, one `SendSilentRefresh` method, error translation from the wire response to a plain Go error) as closely as FCM's actual API allows.

- [ ] **Step 2: Write the failing tests**

```go
package push

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"spamfilter/internal/store"
)

func TestFCMNotifier_SendSilentRefresh_success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"name": "projects/hushield/messages/0:1234"}`))
	}))
	defer server.Close()

	n := &FCMNotifier{httpClient: server.Client(), endpoint: server.URL, accessToken: "test-token"}
	err := n.SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 1, Token: "fcm-token-1", Platform: "fcm"})
	if err != nil {
		t.Fatalf("SendSilentRefresh: %v", err)
	}
}

func TestFCMNotifier_SendSilentRefresh_serverError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": {"status": "UNAUTHENTICATED"}}`))
	}))
	defer server.Close()

	n := &FCMNotifier{httpClient: server.Client(), endpoint: server.URL, accessToken: "bad-token"}
	err := n.SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 1, Token: "fcm-token-1", Platform: "fcm"})
	if err == nil {
		t.Fatal("SendSilentRefresh returned nil for a non-2xx FCM response")
	}
}
```

```go
package push

import (
	"context"
	"errors"
	"testing"

	"spamfilter/internal/store"
)

type spyNotifier struct {
	calls []store.PushTarget
	err   error
}

func (s *spyNotifier) SendSilentRefresh(ctx context.Context, target store.PushTarget) error {
	s.calls = append(s.calls, target)
	return s.err
}

func TestPlatformNotifier_dispatchesByPlatform(t *testing.T) {
	apns := &spyNotifier{}
	fcm := &spyNotifier{}
	n := &PlatformNotifier{APNs: apns, FCM: fcm}

	if err := n.SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 1, Platform: "apns"}); err != nil {
		t.Fatalf("SendSilentRefresh(apns): %v", err)
	}
	if err := n.SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 2, Platform: "fcm"}); err != nil {
		t.Fatalf("SendSilentRefresh(fcm): %v", err)
	}

	if len(apns.calls) != 1 || apns.calls[0].DeviceID != 1 {
		t.Errorf("apns notifier calls = %+v, want exactly device 1", apns.calls)
	}
	if len(fcm.calls) != 1 || fcm.calls[0].DeviceID != 2 {
		t.Errorf("fcm notifier calls = %+v, want exactly device 2", fcm.calls)
	}
}

func TestPlatformNotifier_unknownPlatformFailsClosed(t *testing.T) {
	apns := &spyNotifier{}
	fcm := &spyNotifier{}
	n := &PlatformNotifier{APNs: apns, FCM: fcm}

	err := n.SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 3, Platform: "carrier-pigeon"})
	if !errors.Is(err, ErrUnknownPushPlatform) {
		t.Errorf("err = %v, want ErrUnknownPushPlatform", err)
	}
	if len(apns.calls) != 0 || len(fcm.calls) != 0 {
		t.Error("an unknown platform must not fall through to either real notifier")
	}
}
```

This test references `ErrUnknownPushPlatform`, defined in Step 4 below — matching this codebase's existing sentinel-error convention (`store.ErrDeviceNotFound`).

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/push/... -v`
Expected: FAIL to compile — `FCMNotifier`, `PlatformNotifier` undefined.

- [ ] **Step 4: Implement `platform.go`**

```go
package push

import (
	"context"
	"errors"

	"spamfilter/internal/store"
)

// ErrUnknownPushPlatform is returned when a PushTarget's Platform matches
// neither notifier PlatformNotifier holds. Failing closed here means a data
// bug (an unexpected platform value slipping into the devices table) is
// logged as a failed send, not silently dropped or misrouted.
var ErrUnknownPushPlatform = errors.New("push: unknown platform")

// PlatformNotifier dispatches SendSilentRefresh to APNs or FCM based on
// target.Platform ("apns" or "fcm"), so BroadcastRefresh's caller does not
// need to branch on platform itself.
type PlatformNotifier struct {
	APNs Notifier
	FCM  Notifier
}

func (n *PlatformNotifier) SendSilentRefresh(ctx context.Context, target store.PushTarget) error {
	switch target.Platform {
	case "apns":
		return n.APNs.SendSilentRefresh(ctx, target)
	case "fcm":
		return n.FCM.SendSilentRefresh(ctx, target)
	default:
		return ErrUnknownPushPlatform
	}
}
```

- [ ] **Step 5: Implement `fcm.go`**

Base the exact request/response shape on `apns.go`'s structure and FCM's HTTP v1 API (`https://fcm.googleapis.com/v1/projects/{project_id}/messages:send`), sending a data-only (silent) message — check FCM's current documentation for the exact JSON body a content-available/background-only Android push requires (`context7` MCP tool if available, or Firebase's published reference), the same way Task 4's Google API call is deferred to real docs rather than transcribed from training data:

```go
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"spamfilter/internal/store"
)

// FCMNotifier sends silent (data-only) FCM pushes that nudge an Android
// device to refresh its cached blocklist, mirroring APNsNotifier's role for
// iOS. accessToken is a short-lived OAuth2 token from a service account
// scoped to https://www.googleapis.com/auth/firebase.messaging; refreshing
// it is the caller's responsibility (mirror however cmd/recompute's wiring
// refreshes credentials for other Google-API callers in this plan, e.g. the
// Play Integrity decoder in Task 4).
type FCMNotifier struct {
	httpClient  *http.Client
	endpoint    string // e.g. "https://fcm.googleapis.com/v1/projects/hushield/messages:send"
	accessToken string
}

func NewFCMNotifier(httpClient *http.Client, projectID, accessToken string) *FCMNotifier {
	return &FCMNotifier{
		httpClient:  httpClient,
		endpoint:    fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", projectID),
		accessToken: accessToken,
	}
}

func (n *FCMNotifier) SendSilentRefresh(ctx context.Context, target store.PushTarget) error {
	body, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"token": target.Token,
			"data":  map[string]string{"type": "blocklist_refresh"},
			"android": map[string]any{
				"priority": "high",
			},
		},
	})
	if err != nil {
		return fmt.Errorf("push: marshal FCM message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("push: build FCM request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+n.accessToken)

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("push: FCM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("push: FCM returned status %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/push/... -race -v`
Expected: PASS, all cases.

- [ ] **Step 7: Wire into `cmd/recompute/main.go`**

Find the existing construction of `push.APNsNotifier` (`grep -n "APNsNotifier\|push\.New" cmd/recompute/main.go`) and wrap it:

```go
	notifier := &push.PlatformNotifier{
		APNs: apnsNotifier, // whatever the existing variable/expression is
		FCM:  push.NewFCMNotifier(http.DefaultClient, cfg.FCMProjectID, fcmAccessToken),
	}
```

Add `FCMProjectID string` (env `FCM_PROJECT_ID`) to `config.Config` the same way Task 5 added `AndroidPackageName`, and note in a comment where `fcmAccessToken` should come from (a service-account token source, refreshed the same way the Play Integrity decoder's credentials load in Task 4) — leave the exact token-refresh wiring for the implementer to match whatever pattern Task 4 established, since it's the same underlying Google-credentials problem solved once, not twice.

- [ ] **Step 8: Run the full suite**

Run: `DB_DSN='root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true' go test ./... -race -cover 2>&1 | tail -40`
Expected: every package green.

- [ ] **Step 9: Commit**

```bash
git add internal/push/ internal/config/config.go cmd/recompute/main.go
git commit -m "feat(push): add FCM sender and platform-dispatching notifier"
```

---

## What this plan deliberately does not cover

- The real `googlePlayIntegrityDecoder.Decode` HTTP call (Task 4, Step 6) and the real FCM request shape (Task 7, Step 5) are flagged inline as the two places to pull current API details from live documentation rather than this plan, since both are third-party APIs this plan's author cannot verify are unchanged by implementation time.
- Nothing here touches the Android client itself (Keystore enrollment, `CallScreeningService`, SMS filtering, UI) — those are separate plans per the spec's own subsystem breakdown, to follow once this backend work is merged and manually verified against a real Play Integrity token and a real FCM send.
