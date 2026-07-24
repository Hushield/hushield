package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"spamfilter/internal/attest"
	"spamfilter/internal/config"
)

// silentRedisLogger is a no-op go-redis internal.Logging implementation, used
// below to keep a deliberately-triggered "redis unreachable" test's output
// pristine (go-redis otherwise logs its internal dial retries to stderr).
type silentRedisLogger struct{}

func (silentRedisLogger) Printf(ctx context.Context, format string, v ...any) {}

func silenceRedisLogsForUnreachableServer(t *testing.T) {
	t.Helper()
	redis.SetLogger(silentRedisLogger{})
	t.Cleanup(func() {
		redis.SetLogger(defaultRedisLoggerForTests{log: log.New(os.Stderr, "redis: ", log.LstdFlags|log.Lshortfile)})
	})
}

type defaultRedisLoggerForTests struct{ log *log.Logger }

func (l defaultRedisLoggerForTests) Printf(ctx context.Context, format string, v ...any) {
	l.log.Printf(format, v...)
}

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

func TestBuildVerifier_MockByDefault(t *testing.T) {
	v := buildVerifier(config.Config{})
	if _, ok := v.(*attest.MockVerifier); !ok {
		t.Errorf("verifier = %T, want *attest.MockVerifier for default config", v)
	}
}

func TestBuildVerifier_AppleMode(t *testing.T) {
	v := buildVerifier(config.Config{AttestMode: "apple", AppID: "TEAMID.com.example.app"})
	if _, ok := v.(*attest.AppleVerifier); !ok {
		t.Errorf("verifier = %T, want *attest.AppleVerifier when ATTEST_MODE=apple", v)
	}
}

func TestBuildChallengeStore_MemoryByDefault(t *testing.T) {
	s := buildChallengeStore(config.Config{})
	if _, ok := s.(*attest.MemoryChallengeStore); !ok {
		t.Errorf("store = %T, want *attest.MemoryChallengeStore for default config", s)
	}
}

func TestBuildChallengeStore_RedisInvalidURLPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("buildChallengeStore: want panic for unparseable REDIS_URL, got none")
		}
	}()
	buildChallengeStore(config.Config{ChallengeStore: "redis", RedisURL: "://not-a-valid-url"})
}

func TestBuildChallengeStore_RedisUnreachablePanics(t *testing.T) {
	silenceRedisLogsForUnreachableServer(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("buildChallengeStore: want panic for unreachable redis, got none")
		}
	}()
	// Nothing listens on this port; ParseURL succeeds but Ping fails.
	buildChallengeStore(config.Config{ChallengeStore: "redis", RedisURL: "redis://127.0.0.1:1/0"})
}

func TestBuildChallengeStore_RedisReachableSucceeds(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	s := buildChallengeStore(config.Config{ChallengeStore: "redis", RedisURL: "redis://" + mr.Addr()})
	if _, ok := s.(*attest.RedisChallengeStore); !ok {
		t.Fatalf("store = %T, want *attest.RedisChallengeStore", s)
	}
}
