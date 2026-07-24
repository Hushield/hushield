package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"spamfilter/internal/config"
	"spamfilter/internal/dbtest"
	"spamfilter/internal/token"
)

func postPushToken(router http.Handler, tok, rawBody string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/push-token", strings.NewReader(rawBody))
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestPushTokenEndpoint_HappyPath_DB(t *testing.T) {
	database := dbtest.SetupDB(t)
	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "push-token-test-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
	}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))

	deviceID, tok := mintDeviceToken(t, database, signer, 1.0)

	rec := postPushToken(router, tok, `{"push_token":"a1b2c3d4","environment":"production"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if resp.Status != "registered" {
		t.Errorf("status = %q, want registered", resp.Status)
	}

	var token, env string
	if err := database.QueryRow(
		"SELECT push_token, push_environment FROM devices WHERE device_id = ?", deviceID,
	).Scan(&token, &env); err != nil {
		t.Fatalf("select push columns: %v", err)
	}
	if token != "a1b2c3d4" || env != "production" {
		t.Errorf("row got token=%q env=%q, want a1b2c3d4/production", token, env)
	}
}

func TestPushTokenEndpoint_BadJSON_400(t *testing.T) {
	database := dbtest.SetupDB(t)
	cfg := config.Config{AttestMode: "mock", DeviceTokenSecret: "s", DeviceTokenTTL: time.Hour, ChallengeTTL: time.Minute}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))
	_, tok := mintDeviceToken(t, database, signer, 1.0)

	rec := postPushToken(router, tok, `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for bad JSON; body=%s", rec.Code, rec.Body)
	}
}

func TestPushTokenEndpoint_BadEnvironment_422(t *testing.T) {
	database := dbtest.SetupDB(t)
	cfg := config.Config{AttestMode: "mock", DeviceTokenSecret: "s", DeviceTokenTTL: time.Hour, ChallengeTTL: time.Minute}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))
	_, tok := mintDeviceToken(t, database, signer, 1.0)

	rec := postPushToken(router, tok, `{"push_token":"a1b2","environment":"staging"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 for bad environment; body=%s", rec.Code, rec.Body)
	}
}

func TestPushTokenEndpoint_MissingToken_422(t *testing.T) {
	database := dbtest.SetupDB(t)
	cfg := config.Config{AttestMode: "mock", DeviceTokenSecret: "s", DeviceTokenTTL: time.Hour, ChallengeTTL: time.Minute}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))
	_, tok := mintDeviceToken(t, database, signer, 1.0)

	rec := postPushToken(router, tok, `{"push_token":"","environment":"production"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 for missing push_token; body=%s", rec.Code, rec.Body)
	}
}

func TestPushTokenEndpoint_NonHexToken_422(t *testing.T) {
	database := dbtest.SetupDB(t)
	cfg := config.Config{AttestMode: "mock", DeviceTokenSecret: "s", DeviceTokenTTL: time.Hour, ChallengeTTL: time.Minute}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))
	_, tok := mintDeviceToken(t, database, signer, 1.0)

	rec := postPushToken(router, tok, `{"push_token":"not-hex-zzz","environment":"production"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 for non-hex push_token; body=%s", rec.Code, rec.Body)
	}
	success, errs := decodeEnvelopeErrors(t, rec.Body.Bytes())
	if success {
		t.Error("success = true, want false")
	}
	if len(errs) != 1 || errs[0].Field != "push_token" {
		t.Errorf("errors = %+v, want single error on field=push_token", errs)
	}
}

func TestPushTokenEndpoint_TooLongToken_422(t *testing.T) {
	database := dbtest.SetupDB(t)
	cfg := config.Config{AttestMode: "mock", DeviceTokenSecret: "s", DeviceTokenTTL: time.Hour, ChallengeTTL: time.Minute}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))
	_, tok := mintDeviceToken(t, database, signer, 1.0)

	longToken := strings.Repeat("a", maxPushTokenLen+1)
	rec := postPushToken(router, tok, `{"push_token":"`+longToken+`","environment":"production"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 for over-length push_token; body=%s", rec.Code, rec.Body)
	}
}

// TestPushTokenEndpoint_CustomClock confirms handleRegister uses the
// handler's injected clock (rather than time.Now) when one is set, by
// asserting the persisted push_updated_at matches the injected time exactly.
func TestPushTokenEndpoint_CustomClock(t *testing.T) {
	database := dbtest.SetupDB(t)
	signer := token.NewSigner([]byte("push-clock-secret"))
	deviceID, tok := mintDeviceToken(t, database, signer, 1.0)

	fixed := time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC)
	h := &pushTokenHandler{db: database, now: func() time.Time { return fixed }}
	deviceAuth := NewDeviceAuth(signer)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/push-token", strings.NewReader(`{"push_token":"a1b2","environment":"production"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req = req.WithContext(reqCtx())
	rec := httptest.NewRecorder()
	deviceAuth.RequireDevice(http.HandlerFunc(h.handleRegister)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	var pushUpdatedAt time.Time
	if err := database.QueryRow("SELECT push_updated_at FROM devices WHERE device_id = ?", deviceID).Scan(&pushUpdatedAt); err != nil {
		t.Fatalf("select push_updated_at: %v", err)
	}
	if !pushUpdatedAt.Equal(fixed) {
		t.Errorf("push_updated_at = %v, want %v (the injected clock)", pushUpdatedAt, fixed)
	}
}

// TestPushTokenEndpoint_StoreError_500 confirms handleRegister surfaces
// store.UpsertPushToken's error (a closed DB) as a 500.
func TestPushTokenEndpoint_StoreError_500(t *testing.T) {
	database := dbtest.SetupDB(t)
	database.Close()

	h := &pushTokenHandler{db: database}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/push-token", strings.NewReader(`{"push_token":"a1b2","environment":"production"}`))
	req = req.WithContext(deviceCtx(1))
	rec := httptest.NewRecorder()
	h.handleRegister(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body)
	}
}

func TestPushTokenEndpoint_NoDeviceAuth_401(t *testing.T) {
	database := dbtest.SetupDB(t)
	cfg := config.Config{AttestMode: "mock", DeviceTokenSecret: "s", DeviceTokenTTL: time.Hour, ChallengeTTL: time.Minute}
	router := NewRouter(database, cfg)

	rec := postPushToken(router, "", `{"push_token":"a1b2","environment":"production"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without device token; body=%s", rec.Code, rec.Body)
	}
}
