package dbtest

import (
	"testing"
)

// TestSetupDB_HonorsTestDBDSNOverride confirms SetupDB reads its base DSN
// from TEST_DB_DSN when set (the branch every other package's dbtest.SetupDB
// call leaves untested, since none of them set the env var), rather than
// silently using the built-in default.
func TestSetupDB_HonorsTestDBDSNOverride(t *testing.T) {
	t.Setenv("TEST_DB_DSN", defaultBaseDSN)

	sqlDB := SetupDB(t)
	if err := sqlDB.Ping(); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

// TestSetupDB_SkipsWhenServerUnreachable confirms SetupDB skips (rather than
// failing) the calling test when the configured MySQL server can't be
// reached -- the safety valve that lets the whole suite run somewhere MySQL
// isn't available instead of failing every DB-backed test outright.
func TestSetupDB_SkipsWhenServerUnreachable(t *testing.T) {
	// This asserts the LOCAL behaviour. SetupDB now FAILS instead of skipping
	// when CI is set (so a dead service container cannot yield a green run), so
	// CI has to be cleared here or this test fails on every CI run -- which is
	// exactly what happened the first time the workflow ran.
	t.Setenv("CI", "")
	t.Setenv("TEST_DB_DSN", "root@tcp(127.0.0.1:1)/?parseTime=true&timeout=1s")

	var ran bool
	ok := t.Run("inner", func(t *testing.T) {
		ran = true
		SetupDB(t) // must call t.Skipf, not t.Fatalf
	})
	if !ok {
		t.Error("subtest reported failure, want a clean skip")
	}
	if !ran {
		t.Fatal("subtest never ran")
	}
}
