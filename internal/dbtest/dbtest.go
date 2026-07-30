// Package dbtest provisions an isolated, throwaway MySQL database for each
// DB-gated test. go test runs different packages' tests in parallel, so
// sharing one fixed database (as the old per-package drop-and-migrate setup
// did) causes cross-package races like "Table 'devices' already exists".
// SetupDB avoids that by giving every test its own uniquely-named database.
package dbtest

import (
	"database/sql"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"spamfilter/internal/db"
)

// defaultBaseDSN connects to the MySQL server without selecting a database,
// so SetupDB can CREATE/DROP its own database per invocation.
const defaultBaseDSN = "root@tcp(127.0.0.1:3306)/?parseTime=true&multiStatements=true"

var dbCounter uint64

// baseDSN resolves the DSN SetupDB connects with, in order of precedence:
// TEST_DB_DSN, then DB_DSN, then defaultBaseDSN.
//
// DB_DSN is accepted because it is the variable the server, seed, and recompute
// commands already use, and it is the one a CI author reaches for first. Any
// database named in it is irrelevant -- SetupDB overrides DBName with its own
// per-call database -- so honouring it is safe and removes a foot-gun that
// otherwise looks like it works while doing nothing.
func baseDSN() string {
	for _, key := range []string{"TEST_DB_DSN", "DB_DSN"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return defaultBaseDSN
}

// unreachableDB reports how SetupDB should react when the server cannot be
// reached: skip locally, but FAIL under CI.
//
// Skipping is the right default on a developer machine with no MySQL running --
// it lets the rest of the suite run. In CI it is dangerous: a wrong DSN or a
// dead service container yields a GREEN run that exercised no SQL at all, which
// is indistinguishable from a genuinely passing suite. Every major CI provider,
// GitHub Actions included, sets CI=true.
func unreachableDB(t *testing.T, format string, args ...any) {
	t.Helper()
	if failNotSkip() {
		t.Fatalf("CI=%s: refusing to skip database tests -- "+format,
			append([]any{os.Getenv("CI")}, args...)...)
	}
	t.Skipf("skipping: "+format, args...)
}

// failNotSkip is the decision unreachableDB acts on, split out so it can be
// tested directly. Testing it through unreachableDB would mean triggering a real
// t.Fatalf, and a failed subtest fails its parent -- so the test for "this
// fails" would itself fail the package.
func failNotSkip() bool { return os.Getenv("CI") != "" }

// SetupDB creates a uniquely-named MySQL database, applies db.Migrate to it,
// and returns a ready connection pool scoped to that database. Each call
// gets its own database, so tests in different packages (or in the same
// package) never collide even when go test runs them in parallel. It
// registers a t.Cleanup that drops the database and closes the pool.
//
// The base DSN comes from TEST_DB_DSN, else DB_DSN, else a local root
// connection; see baseDSN. If the server is unreachable, SetupDB skips the test
// locally and FAILS it under CI; see unreachableDB.
func SetupDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := baseDSN()

	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		unreachableDB(t, "cannot open test DB server: %v", err)
	}
	if err := admin.Ping(); err != nil {
		admin.Close()
		unreachableDB(t, "test DB server unreachable: %v", err)
	}

	name := fmt.Sprintf("spamfilter_test_%d_%d_%d", os.Getpid(), time.Now().UnixNano(), atomic.AddUint64(&dbCounter, 1))

	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		admin.Close()
		t.Fatalf("create test database %s: %v", name, err)
	}

	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		admin.Close()
		t.Fatalf("parse base DSN: %v", err)
	}
	cfg.DBName = name

	sqlDB, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		admin.Close()
		t.Fatalf("open test database %s: %v", name, err)
	}
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		admin.Close()
		t.Fatalf("ping test database %s: %v", name, err)
	}

	if err := db.Migrate(sqlDB); err != nil {
		sqlDB.Close()
		admin.Close()
		t.Fatalf("migrate test database %s: %v", name, err)
	}

	t.Cleanup(func() {
		sqlDB.Close()
		if _, err := admin.Exec("DROP DATABASE IF EXISTS " + name); err != nil {
			t.Logf("cleanup: drop database %s: %v", name, err)
		}
		admin.Close()
	})

	return sqlDB
}
