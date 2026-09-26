package api

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"spamfilter/internal/attest"
	"spamfilter/internal/config"
	"spamfilter/internal/dbtest"
	"spamfilter/internal/store"
	"spamfilter/internal/token"
	"spamfilter/internal/trust"
)

func decodeEnvelope(t *testing.T, body []byte) (success bool, data json.RawMessage) {
	t.Helper()
	var env struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v; body=%s", err, body)
	}
	return env.Success, env.Data
}

func newTestHandler(store attest.ChallengeStore, verifier attest.Verifier, database *sql.DB) *attestHandler {
	return &attestHandler{
		db:    database,
		store: store,
		verifiers: map[string]attest.Verifier{
			"apple":   verifier,
			"android": verifier,
		},
		signer:       token.NewSigner([]byte("test-secret")),
		challengeTTL: 5 * time.Minute,
		tokenTTL:     time.Hour,
	}
}

func TestChallengeEndpoint_ReturnsChallenge(t *testing.T) {
	h := newTestHandler(attest.NewMemoryChallengeStore(), attest.NewMockVerifier(nil, nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attest/challenge", nil)
	req = req.WithContext(reqCtx())
	rec := httptest.NewRecorder()
	h.handleChallenge(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	success, data := decodeEnvelope(t, rec.Body.Bytes())
	if !success {
		t.Errorf("success = false, want true")
	}
	var payload challengeResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(payload.Challenge)
	if err != nil {
		t.Fatalf("challenge not base64: %v", err)
	}
	if len(raw) != 32 {
		t.Errorf("challenge length = %d, want 32", len(raw))
	}
	if _, err := time.Parse(time.RFC3339, payload.ExpiresAt); err != nil {
		t.Errorf("expires_at not RFC3339: %v", err)
	}
}

// TestAttestHandler_CustomClock confirms handleChallenge uses the handler's
// injected clock (rather than time.Now) when one is set, by asserting the
// returned expires_at matches challengeTTL added to the injected time exactly.
func TestAttestHandler_CustomClock(t *testing.T) {
	fixed := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	h := &attestHandler{
		store:        attest.NewMemoryChallengeStore(),
		verifiers:    map[string]attest.Verifier{"apple": attest.NewMockVerifier(nil, nil)},
		signer:       token.NewSigner([]byte("clock-test-secret")),
		challengeTTL: 5 * time.Minute,
		now:          func() time.Time { return fixed },
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attest/challenge", nil)
	req = req.WithContext(reqCtx())
	rec := httptest.NewRecorder()
	h.handleChallenge(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var payload challengeResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := fixed.Add(5 * time.Minute).UTC().Format(time.RFC3339)
	if payload.ExpiresAt != want {
		t.Errorf("expires_at = %q, want %q (derived from the injected clock)", payload.ExpiresAt, want)
	}
}

// errorChallengeStore is a ChallengeStore whose Issue always fails, used to
// exercise handleChallenge's internal-error branch.
type errorChallengeStore struct{ attest.ChallengeStore }

func (errorChallengeStore) Issue(now time.Time, ttl time.Duration) ([]byte, error) {
	return nil, errors.New("boom: store unavailable")
}

func TestChallengeEndpoint_StoreError_500(t *testing.T) {
	h := newTestHandler(errorChallengeStore{}, attest.NewMockVerifier(nil, nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attest/challenge", nil)
	req = req.WithContext(reqCtx())
	rec := httptest.NewRecorder()
	h.handleChallenge(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body)
	}
}

func TestVerifyEndpoint_EmptyKeyID(t *testing.T) {
	h := newTestHandler(attest.NewMemoryChallengeStore(), attest.NewMockVerifier(nil, nil), nil)

	body := verifyRequest{
		KeyID:       "",
		Attestation: base64.StdEncoding.EncodeToString([]byte("attestation")),
		Challenge:   base64.StdEncoding.EncodeToString([]byte("challenge")),
	}
	rec := doVerify(t, h, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body)
	}
	success, errs := decodeEnvelopeErrors(t, rec.Body.Bytes())
	if success {
		t.Error("success = true, want false")
	}
	if len(errs) != 1 || errs[0].Field != "key_id" {
		t.Errorf("errors = %+v, want single error on field=key_id", errs)
	}
}

func TestVerifyEndpoint_BadBase64Attestation(t *testing.T) {
	h := newTestHandler(attest.NewMemoryChallengeStore(), attest.NewMockVerifier(nil, nil), nil)

	body := verifyRequest{
		KeyID:       "somekey",
		Attestation: "not-valid-base64!!!",
		Challenge:   base64.StdEncoding.EncodeToString([]byte("challenge")),
	}
	rec := doVerify(t, h, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body)
	}
	success, errs := decodeEnvelopeErrors(t, rec.Body.Bytes())
	if success {
		t.Error("success = true, want false")
	}
	if len(errs) != 1 || errs[0].Field != "attestation" {
		t.Errorf("errors = %+v, want single error on field=attestation", errs)
	}
}

func TestVerifyEndpoint_EmptyAttestation(t *testing.T) {
	h := newTestHandler(attest.NewMemoryChallengeStore(), attest.NewMockVerifier(nil, nil), nil)

	body := verifyRequest{
		KeyID:       "somekey",
		Attestation: "",
		Challenge:   base64.StdEncoding.EncodeToString([]byte("challenge")),
	}
	rec := doVerify(t, h, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body)
	}
}

func TestVerifyEndpoint_BadBase64Challenge(t *testing.T) {
	h := newTestHandler(attest.NewMemoryChallengeStore(), attest.NewMockVerifier(nil, nil), nil)

	body := verifyRequest{
		KeyID:       "somekey",
		Attestation: base64.StdEncoding.EncodeToString([]byte("attestation")),
		Challenge:   "not-valid-base64!!!",
	}
	rec := doVerify(t, h, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body)
	}
	success, errs := decodeEnvelopeErrors(t, rec.Body.Bytes())
	if success {
		t.Error("success = true, want false")
	}
	if len(errs) != 1 || errs[0].Field != "challenge" {
		t.Errorf("errors = %+v, want single error on field=challenge", errs)
	}
}

func TestVerifyEndpoint_BadBody(t *testing.T) {
	h := newTestHandler(attest.NewMemoryChallengeStore(), attest.NewMockVerifier(nil, nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attest/verify", strings.NewReader("{not-json"))
	req = req.WithContext(reqCtx())
	rec := httptest.NewRecorder()
	h.handleVerify(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body)
	}
}

func TestVerifyEndpoint_InvalidChallenge(t *testing.T) {
	h := newTestHandler(attest.NewMemoryChallengeStore(), attest.NewMockVerifier(nil, nil), nil)

	// Never issued this challenge -> Consume fails -> 401.
	body := verifyRequest{
		KeyID:       "somekey",
		Attestation: base64.StdEncoding.EncodeToString([]byte("attestation")),
		Challenge:   base64.StdEncoding.EncodeToString([]byte("never-issued-challenge-bytes-xxx")),
	}
	rec := doVerify(t, h, body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body)
	}
}

func TestVerifyEndpoint_VerifierFailure(t *testing.T) {
	store := attest.NewMemoryChallengeStore()
	now := time.Now()
	ch, err := store.Issue(now, 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	// Verifier configured to fail; challenge is valid and gets consumed first.
	verifier := &attest.MockVerifier{Err: attest.ErrAttestationInvalid}
	h := newTestHandler(store, verifier, nil)

	body := verifyRequest{
		KeyID:       "somekey",
		Attestation: base64.StdEncoding.EncodeToString([]byte("attestation")),
		Challenge:   base64.StdEncoding.EncodeToString(ch),
	}
	rec := doVerify(t, h, body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body)
	}
}

func TestVerifyEndpoint_RejectsReservedKeyID(t *testing.T) {
	store := attest.NewMemoryChallengeStore()
	now := time.Now()
	ch, err := store.Issue(now, 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	h := newTestHandler(store, attest.NewMockVerifier(nil, nil), nil)

	// A client-supplied key_id in the reserved "seed:" namespace must be
	// rejected before any device is persisted, so a mock-mode client cannot
	// claim a seed device's fixed high trust_weight.
	body := verifyRequest{
		KeyID:       "seed:ftc",
		Attestation: base64.StdEncoding.EncodeToString([]byte("attestation")),
		Challenge:   base64.StdEncoding.EncodeToString(ch),
	}
	rec := doVerify(t, h, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body)
	}
	success, errs := decodeEnvelopeErrors(t, rec.Body.Bytes())
	if success {
		t.Error("success = true, want false")
	}
	if len(errs) != 1 || errs[0].Field != "key_id" {
		t.Errorf("errors = %+v, want single error on field=key_id", errs)
	}
}

// TestVerifyEndpoint_HappyPath_DB exercises the full challenge -> verify ->
// token flow through the router against the live test DB, asserting a device
// row is created and the returned token parses to that device_id.
func TestVerifyEndpoint_HappyPath_DB(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "router-test-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
	}
	router := NewRouter(database, cfg)

	// 1. Request a challenge.
	chReq := httptest.NewRequest(http.MethodPost, "/api/v1/attest/challenge", nil)
	chRec := httptest.NewRecorder()
	router.ServeHTTP(chRec, chReq)
	if chRec.Code != http.StatusOK {
		t.Fatalf("challenge status = %d, want 200; body=%s", chRec.Code, chRec.Body)
	}
	_, chData := decodeEnvelope(t, chRec.Body.Bytes())
	var chPayload challengeResponse
	if err := json.Unmarshal(chData, &chPayload); err != nil {
		t.Fatalf("unmarshal challenge: %v", err)
	}

	// 2. Verify with that challenge.
	keyID := "device-key-" + base64.RawURLEncoding.EncodeToString([]byte(time.Now().String()))
	body := verifyRequest{
		KeyID:       keyID,
		Attestation: base64.StdEncoding.EncodeToString([]byte("mock-attestation")),
		Challenge:   chPayload.Challenge,
	}
	bodyJSON, _ := json.Marshal(body)
	vReq := httptest.NewRequest(http.MethodPost, "/api/v1/attest/verify", strings.NewReader(string(bodyJSON)))
	vRec := httptest.NewRecorder()
	router.ServeHTTP(vRec, vReq)
	if vRec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200; body=%s", vRec.Code, vRec.Body)
	}

	_, vData := decodeEnvelope(t, vRec.Body.Bytes())
	var vPayload verifyResponse
	if err := json.Unmarshal(vData, &vPayload); err != nil {
		t.Fatalf("unmarshal verify: %v", err)
	}
	if vPayload.DeviceToken == "" {
		t.Fatal("device_token is empty")
	}

	// 3. Assert a device row exists for this key_id at TrustBase (issue #15).
	var deviceID uint64
	var trustWeight float64
	if err := database.QueryRow("SELECT device_id, trust_weight FROM devices WHERE key_id = ?", keyID).Scan(&deviceID, &trustWeight); err != nil {
		t.Fatalf("device row not found: %v", err)
	}
	if trustWeight != trust.TrustBase {
		t.Errorf("enrolment trust_weight = %v, want trust.TrustBase (%v)", trustWeight, trust.TrustBase)
	}

	// 4. Token parses to the same device_id.
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))
	parsedID, err := signer.Parse(vPayload.DeviceToken, time.Now())
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if parsedID != deviceID {
		t.Errorf("token device_id = %d, want %d", parsedID, deviceID)
	}
}

func doVerify(t *testing.T, h *attestHandler, body verifyRequest) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attest/verify", strings.NewReader(string(b)))
	req = req.WithContext(reqCtx())
	rec := httptest.NewRecorder()
	h.handleVerify(rec, req)
	return rec
}

// twoPlatformHandler returns an attestHandler wired with distinct MockVerifiers
// for "apple" and "android", so a test can tell which one was actually used
// (by the distinct canned public key each returns for VerifyAttestation, or by
// asserting on the persisted device row).
func twoPlatformHandler(store attest.ChallengeStore, database *sql.DB) *attestHandler {
	return &attestHandler{
		db:    database,
		store: store,
		verifiers: map[string]attest.Verifier{
			"apple":   attest.NewMockVerifier([]byte("mock-apple-pubkey"), nil),
			"android": attest.NewMockVerifier([]byte("mock-android-pubkey"), nil),
		},
		signer:       token.NewSigner([]byte("test-secret")),
		challengeTTL: 5 * time.Minute,
		tokenTTL:     time.Hour,
	}
}

func TestHandleVerify_defaultsToApplePlatformWhenFieldOmitted(t *testing.T) {
	database := dbtest.SetupDB(t)
	chStore := attest.NewMemoryChallengeStore()
	ch, err := chStore.Issue(time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	h := twoPlatformHandler(chStore, database)

	keyID := "platform-default-key"
	body := verifyRequest{
		KeyID:       keyID,
		Attestation: base64.StdEncoding.EncodeToString([]byte("attestation")),
		Challenge:   base64.StdEncoding.EncodeToString(ch),
		// Platform intentionally omitted -- every iOS client shipped before
		// this field existed never sends it.
	}
	rec := doVerify(t, h, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}

	_, _, _, platform, err := store.GetDeviceByKeyID(reqCtx(), database, keyID)
	if err != nil {
		t.Fatalf("GetDeviceByKeyID: %v", err)
	}
	if platform != "apple" {
		t.Errorf("persisted platform = %q, want %q", platform, "apple")
	}
}

func TestHandleVerify_androidPlatformSelectsAndroidVerifier(t *testing.T) {
	database := dbtest.SetupDB(t)
	chStore := attest.NewMemoryChallengeStore()
	ch, err := chStore.Issue(time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	h := twoPlatformHandler(chStore, database)

	keyID := "platform-android-key"
	body := verifyRequest{
		KeyID:       keyID,
		Attestation: base64.StdEncoding.EncodeToString([]byte("attestation")),
		Challenge:   base64.StdEncoding.EncodeToString(ch),
		Platform:    "android",
	}
	rec := doVerify(t, h, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}

	_, pubDER, _, platform, err := store.GetDeviceByKeyID(reqCtx(), database, keyID)
	if err != nil {
		t.Fatalf("GetDeviceByKeyID: %v", err)
	}
	if platform != "android" {
		t.Errorf("persisted platform = %q, want %q", platform, "android")
	}
	// The android MockVerifier's canned public key, not the apple one, must be
	// what got persisted -- proof the android verifier (not apple's) ran.
	if string(pubDER) != "mock-android-pubkey" {
		t.Errorf("persisted public key = %q, want %q", pubDER, "mock-android-pubkey")
	}
}

func TestHandleVerify_unknownPlatformRejected(t *testing.T) {
	database := dbtest.SetupDB(t)
	chStore := attest.NewMemoryChallengeStore()
	ch, err := chStore.Issue(time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	h := twoPlatformHandler(chStore, database)

	keyID := "platform-unknown-key"
	body := verifyRequest{
		KeyID:       keyID,
		Attestation: base64.StdEncoding.EncodeToString([]byte("attestation")),
		Challenge:   base64.StdEncoding.EncodeToString(ch),
		Platform:    "windows",
	}
	rec := doVerify(t, h, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body)
	}
	success, errs := decodeEnvelopeErrors(t, rec.Body.Bytes())
	if success {
		t.Error("success = true, want false")
	}
	if len(errs) != 1 || errs[0].Field != "platform" {
		t.Errorf("errors = %+v, want single error on field=platform", errs)
	}

	if _, _, _, _, err := store.GetDeviceByKeyID(reqCtx(), database, keyID); !errors.Is(err, store.ErrDeviceNotFound) {
		t.Errorf("GetDeviceByKeyID error = %v, want store.ErrDeviceNotFound (no device row should have been created)", err)
	}
}

// TestHandleVerify_rejectsPlatformChangeOnReattestation guards against an
// attacker enrolling a NEW key under a victim's existing key_id and having
// UpsertDevicePlatform silently keep the old platform while accepting new
// key material verified under a different verifier's rules than what was
// checked at that device's original enrollment.
func TestHandleVerify_rejectsPlatformChangeOnReattestation(t *testing.T) {
	database := dbtest.SetupDB(t)
	chStore := attest.NewMemoryChallengeStore()
	h := twoPlatformHandler(chStore, database)

	keyID := "platform-switch-key"

	// 1. Enroll as "apple".
	ch1, err := chStore.Issue(time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	rec := doVerify(t, h, verifyRequest{
		KeyID:       keyID,
		Attestation: base64.StdEncoding.EncodeToString([]byte("attestation")),
		Challenge:   base64.StdEncoding.EncodeToString(ch1),
		Platform:    "apple",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("initial apple enrollment status = %d, want 200; body=%s", rec.Code, rec.Body)
	}

	// 2. Attempt to re-enroll the same key_id as "android" -- must be rejected.
	ch2, err := chStore.Issue(time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	rec = doVerify(t, h, verifyRequest{
		KeyID:       keyID,
		Attestation: base64.StdEncoding.EncodeToString([]byte("attestation")),
		Challenge:   base64.StdEncoding.EncodeToString(ch2),
		Platform:    "android",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("platform-switch re-attestation status = %d, want 400; body=%s", rec.Code, rec.Body)
	}

	// 3. The device's stored platform and public key must remain unchanged.
	_, pubDER, _, platform, err := store.GetDeviceByKeyID(reqCtx(), database, keyID)
	if err != nil {
		t.Fatalf("GetDeviceByKeyID: %v", err)
	}
	if platform != "apple" {
		t.Errorf("stored platform = %q, want %q (must not change on rejected re-attestation)", platform, "apple")
	}
	if string(pubDER) != "mock-apple-pubkey" {
		t.Errorf("stored public key = %q, want %q (must not be overwritten by the rejected android re-attestation)", pubDER, "mock-apple-pubkey")
	}
}

// TestHandleVerify_androidPlatformRejectedWhenAttestModeIsAppleOnly guards the
// fail-closed property buildVerifiers depends on: a deployment that only
// enabled ATTEST_MODE=apple must not expose any fallback that accepts a
// platform:"android" attestation, mock or otherwise.
func TestHandleVerify_androidPlatformRejectedWhenAttestModeIsAppleOnly(t *testing.T) {
	cfg := config.Config{
		AttestMode:        "apple",
		AppID:             "ABCDE12345.com.hushield.app",
		DeviceTokenSecret: "test-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
	}
	verifiers := buildVerifiers(cfg)
	if _, ok := verifiers["android"]; ok {
		t.Fatalf("buildVerifiers(AttestMode=apple) = %v, want no \"android\" entry at all", verifiers)
	}

	store := attest.NewMemoryChallengeStore()
	ch, err := store.Issue(time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	h := &attestHandler{
		store:        store,
		verifiers:    verifiers,
		signer:       token.NewSigner([]byte("test-secret")),
		challengeTTL: 5 * time.Minute,
		tokenTTL:     time.Hour,
	}

	body := verifyRequest{
		KeyID:       "somekey",
		Attestation: base64.StdEncoding.EncodeToString([]byte("attestation")),
		Challenge:   base64.StdEncoding.EncodeToString(ch),
		Platform:    "android",
	}
	rec := doVerify(t, h, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body)
	}
}

// TestHandleAssert_usesDevicesStoredPlatformNotAnyClientClaim guards the
// stored-platform dispatch at assert time: assertRequest carries no platform
// field at all (by design, so there is nothing for a client to spoof), so
// handleAssert must look up the verifier from the device's persisted platform,
// not any client-supplied value.
func TestHandleAssert_usesDevicesStoredPlatformNotAnyClientClaim(t *testing.T) {
	database := dbtest.SetupDB(t)
	store := attest.NewMemoryChallengeStore()

	// Distinguishing NewCounter per platform: whichever ends up persisted as
	// sign_count reveals which verifier actually ran.
	appleVerifier := &attest.MockVerifier{PublicKeyDER: []byte("mock-apple-pubkey"), NewCounter: 111}
	androidVerifier := &attest.MockVerifier{PublicKeyDER: []byte("mock-android-pubkey"), NewCounter: 222}
	h := &attestHandler{
		db:    database,
		store: store,
		verifiers: map[string]attest.Verifier{
			"apple":   appleVerifier,
			"android": androidVerifier,
		},
		signer:       token.NewSigner([]byte("test-secret")),
		challengeTTL: 5 * time.Minute,
		tokenTTL:     time.Hour,
	}

	// 1. Enroll as "android" via handleVerify, so a real row with
	// platform="android" and the android verifier's public key exists.
	keyID := "assert-stored-platform-key"
	verifyCh, err := store.Issue(time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	vBody := verifyRequest{
		KeyID:       keyID,
		Attestation: base64.StdEncoding.EncodeToString([]byte("attestation")),
		Challenge:   base64.StdEncoding.EncodeToString(verifyCh),
		Platform:    "android",
	}
	vRec := doVerify(t, h, vBody)
	if vRec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200; body=%s", vRec.Code, vRec.Body)
	}

	// 2. Assert for that key_id. assertRequest has no platform field to spoof.
	assertCh, err := store.Issue(time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	aBody := assertRequest{
		KeyID:     keyID,
		Assertion: base64.StdEncoding.EncodeToString([]byte("assertion")),
		Challenge: base64.StdEncoding.EncodeToString(assertCh),
	}
	aRec := doAssert(t, h, aBody)
	if aRec.Code != http.StatusOK {
		t.Fatalf("assert status = %d, want 200; body=%s", aRec.Code, aRec.Body)
	}

	// 3. The android verifier's NewCounter (222) -- not the apple verifier's
	// (111) -- must be what got persisted.
	var signCount uint32
	if err := database.QueryRow("SELECT sign_count FROM devices WHERE key_id = ?", keyID).Scan(&signCount); err != nil {
		t.Fatalf("select sign_count: %v", err)
	}
	if signCount != androidVerifier.NewCounter {
		t.Errorf("sign_count = %d, want %d (the android verifier's counter, not apple's %d)", signCount, androidVerifier.NewCounter, appleVerifier.NewCounter)
	}
}
