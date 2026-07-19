package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"spamfilter/internal/config"
)

func TestHealthz_ReturnsOKEnvelope(t *testing.T) {
	router := NewRouter(nil, config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}

	var success bool
	if err := json.Unmarshal(raw["success"], &success); err != nil {
		t.Fatalf("failed to unmarshal success: %v", err)
	}
	if !success {
		t.Errorf("success = false, want true")
	}

	var data struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		t.Fatalf("failed to unmarshal data: %v", err)
	}
	if data.Status != "ok" {
		t.Errorf("data.status = %q, want ok", data.Status)
	}

	if rec.Header().Get("X-Request-Id") == "" {
		t.Errorf("response missing X-Request-Id header (router should apply RequestIDMiddleware)")
	}
}

func TestHealthz_ReusesRequestID(t *testing.T) {
	router := NewRouter(nil, config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "test-req-id")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != "test-req-id" {
		t.Errorf("X-Request-Id = %q, want test-req-id", got)
	}
}
