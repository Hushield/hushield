package dbtest

import (
	"database/sql"
	"strings"
	"sync"
	"testing"
)

// Every database-backed test in this repo depends on SetupDB, so a defect here
// produces false confidence everywhere it is used. These tests pin the four
// guarantees callers actually rely on, rather than chasing coverage through
// t.Fatalf branches that can only fire when the infrastructure itself is broken.

// Guarantee 1: callers get a REAL schema. If migrations silently did not run,
// every store test would fail with confusing "no such table" errors, or worse,
// pass against a partially-migrated database.
func TestSetupDB_AppliesAllMigrations(t *testing.T) {
	sqlDB := SetupDB(t)

	var applied int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&applied); err != nil {
		t.Fatalf("querying schema_migrations: %v", err)
	}
	if applied < 5 {
		t.Errorf("schema_migrations has %d rows, want at least 5 (0001-0005)", applied)
	}

	// Spot-check the tables the rest of the suite writes to. information_schema
	// is used rather than SHOW TABLES LIKE, which does not accept placeholders.
	for _, table := range []string{"devices", "phone_numbers", "reports", "caller_names", "admin_overrides"} {
		var found string
		err := sqlDB.QueryRow(
			"SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?",
			table,
		).Scan(&found)
		if err == sql.ErrNoRows {
			t.Errorf("table %q missing; migrations did not fully apply", table)
			continue
		}
		if err != nil {
			t.Errorf("checking table %q: %v", table, err)
		}
	}
}

// Guarantee 2: each caller gets its OWN database. Tests across packages run in
// parallel, and a shared database would make them interfere in ways that look
// like flakiness rather than a helper bug.
func TestSetupDB_GivesEachCallerAnIsolatedDatabase(t *testing.T) {
	a := SetupDB(t)
	b := SetupDB(t)

	nameOf := func(db *sql.DB) string {
		t.Helper()
		var n string
		if err := db.QueryRow("SELECT DATABASE()").Scan(&n); err != nil {
			t.Fatalf("SELECT DATABASE(): %v", err)
		}
		return n
	}

	nameA, nameB := nameOf(a), nameOf(b)
	if nameA == nameB {
		t.Fatalf("both handles point at %q; SetupDB must create a distinct database per call", nameA)
	}
	if !strings.HasPrefix(nameA, "spamfilter_test_") || !strings.HasPrefix(nameB, "spamfilter_test_") {
		t.Errorf("names %q/%q do not carry the spamfilter_test_ prefix that identifies them as disposable", nameA, nameB)
	}

	// Writing through one handle must not be visible through the other.
	if _, err := a.Exec("INSERT INTO phone_numbers (number, source) VALUES ('+14155550001', 'community')"); err != nil {
		t.Fatalf("insert into A: %v", err)
	}
	var countB int
	if err := b.QueryRow("SELECT COUNT(*) FROM phone_numbers").Scan(&countB); err != nil {
		t.Fatalf("count in B: %v", err)
	}
	if countB != 0 {
		t.Errorf("B sees %d rows written to A; the databases are not isolated", countB)
	}
}

// Guarantee 3: concurrent calls do not collide. The name is built from pid +
// nanosecond timestamp + an atomic counter; without the counter, two calls in
// the same nanosecond would race to CREATE the same database.
func TestSetupDB_ConcurrentCallsDoNotCollide(t *testing.T) {
	const n = 8

	var (
		mu    sync.Mutex
		names = make(map[string]int, n)
		wg    sync.WaitGroup
	)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each subtest gets its own *testing.T so Cleanup is scoped per call.
			t.Run("", func(t *testing.T) {
				db := SetupDB(t)
				var name string
				if err := db.QueryRow("SELECT DATABASE()").Scan(&name); err != nil {
					t.Errorf("SELECT DATABASE(): %v", err)
					return
				}
				mu.Lock()
				names[name]++
				mu.Unlock()
			})
		}()
	}
	wg.Wait()

	if len(names) != n {
		t.Errorf("got %d distinct databases from %d concurrent calls, want %d", len(names), n, n)
	}
	for name, count := range names {
		if count > 1 {
			t.Errorf("database %q was handed to %d callers", name, count)
		}
	}
}

// Guarantee 4: the database is dropped afterwards. Without this a full suite run
// leaks a database per test, which eventually exhausts the server.
func TestSetupDB_CleanupDropsTheDatabase(t *testing.T) {
	var name string

	// Inner subtest so its t.Cleanup fires while the outer test can still observe.
	t.Run("inner", func(t *testing.T) {
		db := SetupDB(t)
		if err := db.QueryRow("SELECT DATABASE()").Scan(&name); err != nil {
			t.Fatalf("SELECT DATABASE(): %v", err)
		}
	})

	if name == "" {
		t.Fatal("never captured the database name")
	}

	// Reconnect independently -- the pool from the subtest is closed by now.
	admin, err := sql.Open("mysql", defaultBaseDSN)
	if err != nil {
		t.Skipf("skipping: cannot reopen admin connection: %v", err)
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		t.Skipf("skipping: admin connection unreachable: %v", err)
	}

	var found string
	err = admin.QueryRow("SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?", name).Scan(&found)
	if err == nil {
		t.Errorf("database %q still exists after cleanup; a full suite run would leak one per test", name)
	} else if err != sql.ErrNoRows {
		t.Errorf("checking for leftover database: %v", err)
	}
}

// The default is used by every other package, since none of them set the env
// var -- so it has to be a working DSN with no database selected.
func TestDefaultBaseDSN_SelectsNoDatabase(t *testing.T) {
	if strings.Contains(defaultBaseDSN, ")/?") == false {
		t.Errorf("defaultBaseDSN = %q, want no database selected (a %q separator)", defaultBaseDSN, ")/?")
	}
	for _, required := range []string{"parseTime=true", "multiStatements=true"} {
		if !strings.Contains(defaultBaseDSN, required) {
			t.Errorf("defaultBaseDSN is missing %q, which the migration runner requires", required)
		}
	}
}

// SetupDB falls back to defaultBaseDSN when TEST_DB_DSN is unset. Every other
// package relies on that branch; this is the only place it is exercised
// deliberately.
func TestSetupDB_FallsBackToDefaultWhenEnvUnset(t *testing.T) {
	t.Setenv("TEST_DB_DSN", "")

	sqlDB := SetupDB(t)
	if err := sqlDB.Ping(); err != nil {
		t.Errorf("Ping with the default base DSN: %v", err)
	}
}
