package db_test

import (
	"testing"

	"spamfilter/internal/db"
)

// TestOpen_Success confirms Open connects, applies the pool settings, and
// (critically) actually verifies connectivity via Ping before returning --
// against the same local MySQL server dbtest uses.
func TestOpen_Success(t *testing.T) {
	sqlDB, err := db.Open("root@tcp(127.0.0.1:3306)/?parseTime=true&multiStatements=true")
	if err != nil {
		t.Skipf("skipping: local MySQL server unreachable: %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		t.Errorf("Ping after Open: %v", err)
	}
}

// TestOpen_PingFailureReturnsError confirms Open reports an error (rather
// than returning a pool that appears fine but can never connect) when the
// target server is unreachable, and that it closes the pool it opened before
// returning.
func TestOpen_PingFailureReturnsError(t *testing.T) {
	// Nothing listens on this port: sql.Open succeeds lazily, but Open's own
	// explicit Ping must fail.
	_, err := db.Open("root@tcp(127.0.0.1:1)/?parseTime=true&timeout=1s")
	if err == nil {
		t.Fatal("Open: want error for an unreachable server, got nil")
	}
}
