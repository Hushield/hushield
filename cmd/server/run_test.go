package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"spamfilter/internal/dbtest"
)

// testDSN builds a DSN pointing at the throwaway database dbtest created.
// SetupDB returns only the pool, so the database name is recovered from the
// connection itself.
func testDSN(t *testing.T) string {
	t.Helper()
	sqlDB := dbtest.SetupDB(t)

	var name string
	if err := sqlDB.QueryRow("SELECT DATABASE()").Scan(&name); err != nil {
		t.Fatalf("resolving test database name: %v", err)
	}
	base := os.Getenv("TEST_DB_DSN")
	if base == "" {
		base = "root@tcp(127.0.0.1:3306)/?parseTime=true&multiStatements=true"
	}
	at := strings.Index(base, "@")
	slash := strings.Index(base, "/")
	return base[:at] + base[at:slash] + "/" + name + "?parseTime=true&multiStatements=true"
}

// A bad production config must abort startup with a message naming the
// offending variable. This is the single most likely deploy failure, and
// previously it could only be observed by running the binary.
func TestRun_ConfigValidationFailureNamesTheVariable(t *testing.T) {
	t.Setenv("ATTEST_MODE", "apple")
	t.Setenv("APP_ID", "") // required in apple mode

	err := run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected startup to fail with ATTEST_MODE=apple and no APP_ID")
	}
	if !strings.Contains(err.Error(), "loading config") {
		t.Errorf("error = %q, want it to mention loading config", err)
	}
	if !strings.Contains(err.Error(), "APP_ID") {
		t.Errorf("error = %q, want it to name APP_ID", err)
	}
}

// An unrecognized ATTEST_MODE must abort rather than silently selecting the
// mock verifier, which accepts any attestation.
func TestRun_UnknownAttestModeAborts(t *testing.T) {
	t.Setenv("ATTEST_MODE", "Apple")

	err := run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected startup to fail: ATTEST_MODE is case-sensitive")
	}
	if !strings.Contains(err.Error(), "ATTEST_MODE") {
		t.Errorf("error = %q, want it to name ATTEST_MODE", err)
	}
}

func TestRun_UnreachableDatabaseAborts(t *testing.T) {
	t.Setenv("ATTEST_MODE", "mock")
	t.Setenv("DB_DSN", "root@tcp(127.0.0.1:1)/nope?parseTime=true&timeout=1s")

	err := run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected startup to fail against an unreachable database")
	}
	if !strings.Contains(err.Error(), "database") && !strings.Contains(err.Error(), "migrations") {
		t.Errorf("error = %q, want it to mention the database or migrations", err)
	}
}

// An unusable listen address must be reported as a bind failure, not after the
// process has already claimed to be listening.
func TestRun_BindFailureIsReported(t *testing.T) {
	t.Setenv("ATTEST_MODE", "mock")
	t.Setenv("DB_DSN", testDSN(t))
	t.Setenv("ADDR", "256.256.256.256:8080")

	err := run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected a bind failure for an invalid address")
	}
	if !strings.Contains(err.Error(), "listening on") {
		t.Errorf("error = %q, want it to identify the bind as the failure", err)
	}
}

// The happy path: migrations apply, the router mounts, /healthz answers with the
// standard envelope, and cancelling the context shuts down cleanly.
func TestRun_ServesHealthzAndShutsDownGracefully(t *testing.T) {
	t.Setenv("ATTEST_MODE", "mock")
	t.Setenv("DB_DSN", testDSN(t))
	t.Setenv("ADDR", "127.0.0.1:0") // :0 -> the OS picks a free port

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, ready) }()

	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("server exited before becoming ready: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("server never became ready")
	}

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// Assert the response envelope, not just the status: every route is supposed
	// to share {success, data, errors, meta}, and a bare body would mean the
	// envelope middleware was not wired.
	var body struct {
		Success bool              `json:"success"`
		Data    map[string]string `json:"data"`
		Errors  []any             `json:"errors"`
		Meta    struct {
			RequestID string `json:"request_id"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding /healthz body: %v", err)
	}
	if !body.Success || body.Data["status"] != "ok" {
		t.Errorf("body = %+v, want success with data.status=ok", body)
	}
	if body.Meta.RequestID == "" {
		t.Error("meta.request_id is empty; the request-id middleware is not wired")
	}

	// Graceful shutdown must return nil, not an error.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("graceful shutdown returned an error: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("server did not shut down within 20s of context cancellation")
	}
}

// Device routes must reject an unauthenticated caller. Checked here at the
// wired-router level, not just in the api package's own tests.
func TestRun_DeviceRoutesRequireAuth(t *testing.T) {
	t.Setenv("ATTEST_MODE", "mock")
	t.Setenv("DB_DSN", testDSN(t))
	t.Setenv("ADDR", "127.0.0.1:0")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	go func() { _ = run(ctx, ready) }()

	var addr string
	select {
	case addr = <-ready:
	case <-time.After(30 * time.Second):
		t.Fatal("server never became ready")
	}

	resp, err := http.Get("http://" + addr + "/api/v1/blocklist")
	if err != nil {
		t.Fatalf("GET /api/v1/blocklist: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for an unauthenticated device route", resp.StatusCode)
	}
}
