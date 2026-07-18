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

// SetupDB creates a uniquely-named MySQL database, applies db.Migrate to it,
// and returns a ready connection pool scoped to that database. Each call
// gets its own database, so tests in different packages (or in the same
// package) never collide even when go test runs them in parallel. It
// registers a t.Cleanup that drops the database and closes the pool.
//
// The base DSN (no database selected) comes from the TEST_DB_DSN env var, or
// defaults to a local root connection. If the server is unreachable, SetupDB
// skips the test.
func SetupDB(t *testing.T) *sql.DB {
	t.Helper()

	baseDSN := os.Getenv("TEST_DB_DSN")
	if baseDSN == "" {
		baseDSN = defaultBaseDSN
	}

	admin, err := sql.Open("mysql", baseDSN)
	if err != nil {
		t.Skipf("skipping: cannot open test DB server: %v", err)
	}
	if err := admin.Ping(); err != nil {
		admin.Close()
		t.Skipf("skipping: test DB server unreachable: %v", err)
	}

	name := fmt.Sprintf("spamfilter_test_%d_%d_%d", os.Getpid(), time.Now().UnixNano(), atomic.AddUint64(&dbCounter, 1))

	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		admin.Close()
		t.Fatalf("create test database %s: %v", name, err)
	}

	cfg, err := mysql.ParseDSN(baseDSN)
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
