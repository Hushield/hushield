package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"spamfilter/internal/config"
	"spamfilter/internal/dbtest"
)

// The wiring in run() is where a deploy actually breaks: a bad flag, a config
// that fails validation, an unreachable database. None of that was covered,
// because it all lived in main() behind log.Fatalf.

func TestRun_RejectsUnknownFlag(t *testing.T) {
	err := run([]string{"-nonsense"})
	if err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
	if !strings.Contains(err.Error(), "parsing flags") {
		t.Errorf("error = %q, want it to mention flag parsing", err)
	}
}

func TestRun_RejectsMalformedInterval(t *testing.T) {
	err := run([]string{"-interval", "15minutes"})
	if err == nil {
		t.Fatal("expected an error: 15minutes is not a Go duration")
	}
	if !strings.Contains(err.Error(), "parsing flags") {
		t.Errorf("error = %q, want it to mention flag parsing", err)
	}
}

// A config that cannot pass validation must surface as an error naming the
// offending variable, not as a database connection failure further down.
func TestRun_SurfacesConfigValidationError(t *testing.T) {
	t.Setenv("ATTEST_MODE", "Apple") // valid values are mock|apple, case-sensitive

	err := run(nil)
	if err == nil {
		t.Fatal("expected an error for an unrecognized ATTEST_MODE")
	}
	if !strings.Contains(err.Error(), "loading config") {
		t.Errorf("error = %q, want it to mention loading config", err)
	}
	if !strings.Contains(err.Error(), "ATTEST_MODE") {
		t.Errorf("error = %q, want it to name ATTEST_MODE so an operator can act on it", err)
	}
}

func TestRun_SurfacesDatabaseError(t *testing.T) {
	t.Setenv("ATTEST_MODE", "mock")
	t.Setenv("DB_DSN", "root@tcp(127.0.0.1:1)/nope?parseTime=true&timeout=1s")

	err := run(nil)
	if err == nil {
		t.Fatal("expected an error for an unreachable database")
	}
	// Either the connection or the migration step may report first; both are
	// legitimate, but it must not be silent.
	if !strings.Contains(err.Error(), "database") && !strings.Contains(err.Error(), "migrations") {
		t.Errorf("error = %q, want it to mention the database or migrations", err)
	}
}

// One-shot mode must complete a full cycle and return, not block.
func TestRun_OneShotCompletesAndReturns(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)

	// SetupDB creates a uniquely-named throwaway database and hands back only
	// the pool, so recover the name from the connection to build a DSN that
	// run() can use via DB_DSN.
	var dbName string
	if err := sqlDB.QueryRow("SELECT DATABASE()").Scan(&dbName); err != nil {
		t.Fatalf("resolving test database name: %v", err)
	}
	base := os.Getenv("TEST_DB_DSN")
	if base == "" {
		base = "root@tcp(127.0.0.1:3306)/?parseTime=true&multiStatements=true"
	}
	host := base[strings.Index(base, "@"):strings.Index(base, "/")]
	user := base[:strings.Index(base, "@")]
	dsn := user + host + "/" + dbName + "?parseTime=true&multiStatements=true"

	t.Setenv("ATTEST_MODE", "mock")
	t.Setenv("DB_DSN", dsn)

	done := make(chan error, 1)
	go func() { done <- run([]string{"-notify=false"}) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("one-shot run returned an error: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("one-shot mode did not return; it must not block like interval mode")
	}
}

// runCycle builds the notifier only when notify is on. A configured-but-broken
// APNs key must fail loudly rather than be swallowed, because silent push
// failure is indistinguishable from push being deliberately disabled.
func TestRunCycle_BrokenAPNsKeyIsReported(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:  "mock",
		APNSKeyPath: "/nonexistent/AuthKey_BROKEN.p8",
		APNSKeyID:   "BROKENKEY1",
		APNSTeamID:  "TEAM123456",
		APNSTopic:   "com.example.hushield",
	}

	err := runCycle(context.Background(), sqlDB, cfg, true)
	if err == nil {
		t.Fatal("expected an error: APNS_KEY_PATH points at a file that does not exist")
	}
	if !strings.Contains(err.Error(), "push notifier") {
		t.Errorf("error = %q, want it to identify the push notifier as the failure", err)
	}
}

// The same broken key must NOT fail a recompute when notify is off -- the whole
// point of the -notify guard is that a push-only misconfiguration cannot break
// a recompute that otherwise succeeded.
func TestRunCycle_BrokenAPNsKeyIgnoredWhenNotifyOff(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:  "mock",
		APNSKeyPath: "/nonexistent/AuthKey_BROKEN.p8",
		APNSKeyID:   "BROKENKEY1",
		APNSTeamID:  "TEAM123456",
		APNSTopic:   "com.example.hushield",
	}

	if err := runCycle(context.Background(), sqlDB, cfg, false); err != nil {
		t.Fatalf("notify=false must skip the notifier entirely, got: %v", err)
	}
}
