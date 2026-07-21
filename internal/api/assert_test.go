package api

import (
	"encoding/base64"
	"encoding/json"
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

func doAssert(t *testing.T, h *attestHandler, body assertRequest) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attest/assert", strings.NewReader(string(b)))
	req = req.WithContext(reqCtx())
	rec := httptest.NewRecorder()
	h.handleAssert(rec, req)
	return rec
}

func TestAssertEndpoint_BadBody(t *testing.T) {
	h := newTestHandler(attest.NewMemoryChallengeStore(), attest.NewMockVerifier(nil, nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/attest/assert", strings.NewReader("{not-json"))
	req = req.WithContext(reqCtx())
	rec := httptest.NewRecorder()
	h.handleAssert(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body)
	}
}

func TestAssertEndpoint_RejectsReservedKeyID(t *testing.T) {
	store := attest.NewMemoryChallengeStore()
	ch, err := store.Issue(time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	h := newTestHandler(store, attest.NewMockVerifier(nil, nil), nil)

	body := assertRequest{
		KeyID:     "seed:ftc",
		Assertion: base64.StdEncoding.EncodeToString([]byte("assertion")),
		Challenge: base64.StdEncoding.EncodeToString(ch),
	}
	rec := doAssert(t, h, body)
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

func TestAssertEndpoint_InvalidChallenge(t *testing.T) {
	h := newTestHandler(attest.NewMemoryChallengeStore(), attest.NewMockVerifier(nil, nil), nil)

	body := assertRequest{
		KeyID:     "somekey",
		Assertion: base64.StdEncoding.EncodeToString([]byte("assertion")),
		Challenge: base64.StdEncoding.EncodeToString([]byte("never-issued-challenge-bytes-xxx")),
	}
	rec := doAssert(t, h, body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body)
	}
}

func TestAssertEndpoint_UnknownKeyID_DB(t *testing.T) {
	database := dbtest.SetupDB(t)
	store := attest.NewMemoryChallengeStore()
	ch, err := store.Issue(time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	h := newTestHandler(store, attest.NewMockVerifier(nil, nil), database)

	// Device never attested -> GetDeviceByKeyID misses -> 401.
	body := assertRequest{
		KeyID:     "device-that-never-attested",
		Assertion: base64.StdEncoding.EncodeToString([]byte("assertion")),
		Challenge: base64.StdEncoding.EncodeToString(ch),
	}
	rec := doAssert(t, h, body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body)
	}
}

func TestAssertEndpoint_VerifierFailure_DB(t *testing.T) {
	database := dbtest.SetupDB(t)
	ctx := reqCtx()

	// Seed an attested device row directly.
	keyID := "verifier-fail-key"
	if _, err := database.ExecContext(ctx, "INSERT INTO devices (key_id, public_key) VALUES (?, ?)", keyID, []byte("pub")); err != nil {
		t.Fatalf("insert device: %v", err)
	}

	store := attest.NewMemoryChallengeStore()
	ch, err := store.Issue(time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	verifier := &attest.MockVerifier{Err: attest.ErrAttestationInvalid}
	h := newTestHandler(store, verifier, database)

	body := assertRequest{
		KeyID:     keyID,
		Assertion: base64.StdEncoding.EncodeToString([]byte("assertion")),
		Challenge: base64.StdEncoding.EncodeToString(ch),
	}
	rec := doAssert(t, h, body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body)
	}
}

// TestAssertEndpoint_HappyPath_DB exercises attest -> challenge -> assert
// through the router: a device attests once, then refreshes its token via an
// assertion, and the new token parses to the same device_id. It also asserts
// the persisted sign_count advanced.
func TestAssertEndpoint_HappyPath_DB(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "assert-test-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
	}
	router := NewRouter(database, cfg)

	// 1. Attest a device (creates the device row).
	keyID := "assert-happy-" + base64.RawURLEncoding.EncodeToString([]byte(time.Now().String()))
	requestChallenge := func() string {
		chReq := httptest.NewRequest(http.MethodPost, "/api/v1/attest/challenge", nil)
		chRec := httptest.NewRecorder()
		router.ServeHTTP(chRec, chReq)
		if chRec.Code != http.StatusOK {
			t.Fatalf("challenge status = %d; body=%s", chRec.Code, chRec.Body)
		}
		_, chData := decodeEnvelope(t, chRec.Body.Bytes())
		var chPayload challengeResponse
		if err := json.Unmarshal(chData, &chPayload); err != nil {
			t.Fatalf("unmarshal challenge: %v", err)
		}
		return chPayload.Challenge
	}

	vBody := verifyRequest{
		KeyID:       keyID,
		Attestation: base64.StdEncoding.EncodeToString([]byte("mock-attestation")),
		Challenge:   requestChallenge(),
	}
	vJSON, _ := json.Marshal(vBody)
	vReq := httptest.NewRequest(http.MethodPost, "/api/v1/attest/verify", strings.NewReader(string(vJSON)))
	vRec := httptest.NewRecorder()
	router.ServeHTTP(vRec, vReq)
	if vRec.Code != http.StatusOK {
		t.Fatalf("verify status = %d; body=%s", vRec.Code, vRec.Body)
	}

	var deviceID uint64
	if err := database.QueryRow("SELECT device_id FROM devices WHERE key_id = ?", keyID).Scan(&deviceID); err != nil {
		t.Fatalf("device row not found: %v", err)
	}

	// 2. Refresh the token via an assertion.
	aBody := assertRequest{
		KeyID:     keyID,
		Assertion: base64.StdEncoding.EncodeToString([]byte("mock-assertion")),
		Challenge: requestChallenge(),
	}
	aJSON, _ := json.Marshal(aBody)
	aReq := httptest.NewRequest(http.MethodPost, "/api/v1/attest/assert", strings.NewReader(string(aJSON)))
	aRec := httptest.NewRecorder()
	router.ServeHTTP(aRec, aReq)
	if aRec.Code != http.StatusOK {
		t.Fatalf("assert status = %d; body=%s", aRec.Code, aRec.Body)
	}

	_, aData := decodeEnvelope(t, aRec.Body.Bytes())
	var aPayload verifyResponse
	if err := json.Unmarshal(aData, &aPayload); err != nil {
		t.Fatalf("unmarshal assert: %v", err)
	}
	if aPayload.DeviceToken == "" {
		t.Fatal("device_token is empty")
	}
	if _, err := time.Parse(time.RFC3339, aPayload.ExpiresAt); err != nil {
		t.Errorf("expires_at not RFC3339: %v", err)
	}

	// 3. New token parses to the same device_id.
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))
	parsedID, err := signer.Parse(aPayload.DeviceToken, time.Now())
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if parsedID != deviceID {
		t.Errorf("token device_id = %d, want %d", parsedID, deviceID)
	}

	// 4. sign_count advanced from the default 0 (MockVerifier returns prev+1).
	var signCount uint32
	if err := database.QueryRow("SELECT sign_count FROM devices WHERE device_id = ?", deviceID).Scan(&signCount); err != nil {
		t.Fatalf("select sign_count: %v", err)
	}
	if signCount != 1 {
		t.Errorf("sign_count = %d, want 1", signCount)
	}
}
