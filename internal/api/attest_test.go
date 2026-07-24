package api

import (
	"context"
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
	"spamfilter/internal/token"
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
		db:           database,
		store:        store,
		verifier:     verifier,
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
		verifier:     attest.NewMockVerifier(nil, nil),
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

func TestUpsertDevice_NilDB(t *testing.T) {
	if _, err := upsertDevice(context.Background(), nil, "key", []byte("pub"), []byte("receipt"), time.Now()); err == nil {
		t.Fatal("upsertDevice: want error for nil db, got nil")
	}
}

// TestUpsertDevice_ClosedDB confirms upsertDevice propagates the underlying
// ExecContext error for a real (non-nil) but closed DB, distinct from the
// nil-db guard above.
func TestUpsertDevice_ClosedDB(t *testing.T) {
	database := dbtest.SetupDB(t)
	database.Close()

	if _, err := upsertDevice(context.Background(), database, "key", []byte("pub"), []byte("receipt"), time.Now()); err == nil {
		t.Fatal("upsertDevice: want error for closed db, got nil")
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

	// 3. Assert a device row exists for this key_id.
	var deviceID uint64
	if err := database.QueryRow("SELECT device_id FROM devices WHERE key_id = ?", keyID).Scan(&deviceID); err != nil {
		t.Fatalf("device row not found: %v", err)
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
