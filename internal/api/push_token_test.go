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

func TestPushTokenEndpoint_NoDeviceAuth_401(t *testing.T) {
	database := dbtest.SetupDB(t)
	cfg := config.Config{AttestMode: "mock", DeviceTokenSecret: "s", DeviceTokenTTL: time.Hour, ChallengeTTL: time.Minute}
	router := NewRouter(database, cfg)

	rec := postPushToken(router, "", `{"push_token":"a1b2","environment":"production"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without device token; body=%s", rec.Code, rec.Body)
	}
}
