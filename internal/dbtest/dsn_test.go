package dbtest

import (
	"strings"
	"testing"
)

// These pin the two behaviours that decide whether a CI run means anything.
//
// PR #12 set DB_DSN and assumed a wrong variable name would "surface as an
// immediate, clear CI failure rather than a silent gap." The opposite was true:
// SetupDB read only TEST_DB_DSN and SKIPPED when it could not connect, so a
// wrong name produced a green run that exercised no SQL whatsoever.

func TestBaseDSN_PrefersTestDBDSN(t *testing.T) {
	t.Setenv("TEST_DB_DSN", "root@tcp(host-a:3306)/?parseTime=true")
	t.Setenv("DB_DSN", "root@tcp(host-b:3306)/db?parseTime=true")

	if got := baseDSN(); !strings.Contains(got, "host-a") {
		t.Errorf("baseDSN() = %q, want the TEST_DB_DSN value to win", got)
	}
}

// DB_DSN is what the server, seed, and recompute commands use, and the variable
// a CI author reaches for first. Honouring it removes a foot-gun.
func TestBaseDSN_FallsBackToDBDSN(t *testing.T) {
	t.Setenv("TEST_DB_DSN", "")
	t.Setenv("DB_DSN", "root@tcp(host-b:3306)/spamfilter_test?parseTime=true")

	got := baseDSN()
	if !strings.Contains(got, "host-b") {
		t.Errorf("baseDSN() = %q, want the DB_DSN value when TEST_DB_DSN is unset", got)
	}
}

func TestBaseDSN_FallsBackToDefault(t *testing.T) {
	t.Setenv("TEST_DB_DSN", "")
	t.Setenv("DB_DSN", "")

	if got := baseDSN(); got != defaultBaseDSN {
		t.Errorf("baseDSN() = %q, want defaultBaseDSN", got)
	}
}

// A DSN naming a database is fine: SetupDB overrides DBName with its own
// per-call database, so the name in the DSN is irrelevant. This is why accepting
// DB_DSN is safe rather than merely convenient.
func TestSetupDB_AcceptsADSNThatNamesADatabase(t *testing.T) {
	t.Setenv("TEST_DB_DSN", "")
	// Deliberately names a database that does not exist.
	t.Setenv("DB_DSN", "root@tcp(127.0.0.1:3306)/definitely_not_a_real_db?parseTime=true&multiStatements=true")

	sqlDB := SetupDB(t)

	var name string
	if err := sqlDB.QueryRow("SELECT DATABASE()").Scan(&name); err != nil {
		t.Fatalf("SELECT DATABASE(): %v", err)
	}
	if !strings.HasPrefix(name, "spamfilter_test_") {
		t.Errorf("connected to %q, want SetupDB's own spamfilter_test_* database", name)
	}
	if name == "definitely_not_a_real_db" {
		t.Error("SetupDB used the database from the DSN instead of creating its own")
	}
}

// Locally, an unreachable server SKIPS -- so the suite still runs on a machine
// with no MySQL. Verified via a subtest so the skip does not stop this test.
func TestUnreachableDB_SkipsWhenNotCI(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("TEST_DB_DSN", "root@tcp(127.0.0.1:1)/?timeout=1s")

	var reached bool
	ok := t.Run("inner", func(t *testing.T) {
		reached = true
		SetupDB(t) // expected to Skipf
		t.Error("SetupDB returned instead of skipping")
	})
	if !reached {
		t.Fatal("subtest never ran")
	}
	if !ok {
		t.Error("subtest failed; locally an unreachable database must SKIP, not fail")
	}
}

// Under CI an unreachable database must FAIL rather than skip -- a green CI run
// has to mean the SQL tests actually executed. Tested through the predicate:
// exercising unreachableDB directly would trigger a real t.Fatalf, and a failed
// subtest fails its parent, so the test asserting "this fails" would fail the
// package.
func TestFailNotSkip_TrueOnlyUnderCI(t *testing.T) {
	t.Setenv("CI", "")
	if failNotSkip() {
		t.Error("failNotSkip() = true with CI unset; locally an unreachable database should skip")
	}

	for _, v := range []string{"true", "1", "yes"} {
		t.Setenv("CI", v)
		if !failNotSkip() {
			t.Errorf("failNotSkip() = false with CI=%q; any non-empty CI must force a failure", v)
		}
	}
}
