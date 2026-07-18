package db

import (
	"database/sql"
	"testing"
)

const testDSN = "root@tcp(127.0.0.1:3306)/spamfilter_test?parseTime=true&multiStatements=true"

// connectTestDB opens a connection to the test database, skipping the test
// if the DB is unreachable.
func connectTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("mysql", testDSN)
	if err != nil {
		t.Skipf("skipping: cannot open test DB: %v", err)
	}
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		t.Skipf("skipping: test DB unreachable: %v", err)
	}
	return sqlDB
}

func dropAllTables(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	tables := []string{
		"admin_overrides",
		"caller_names",
		"reports",
		"phone_numbers",
		"devices",
		"schema_migrations",
	}
	if _, err := sqlDB.Exec("SET FOREIGN_KEY_CHECKS=0"); err != nil {
		t.Fatalf("failed to disable FK checks: %v", err)
	}
	defer sqlDB.Exec("SET FOREIGN_KEY_CHECKS=1")
	for _, table := range tables {
		if _, err := sqlDB.Exec("DROP TABLE IF EXISTS " + table); err != nil {
			t.Fatalf("failed to drop table %s: %v", table, err)
		}
	}
}

func TestMigrate_CreatesAllFiveTablesAndIsIdempotent(t *testing.T) {
	sqlDB := connectTestDB(t)
	defer sqlDB.Close()

	dropAllTables(t, sqlDB)
	defer dropAllTables(t, sqlDB)

	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("first Migrate() failed: %v", err)
	}

	wantTables := []string{"devices", "phone_numbers", "reports", "caller_names", "admin_overrides"}
	for _, table := range wantTables {
		var name string
		err := sqlDB.QueryRow(
			"SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
			table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found after Migrate(): %v", table, err)
		}
	}

	var migrationRowCount int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationRowCount); err != nil {
		t.Fatalf("failed to count schema_migrations rows: %v", err)
	}
	if migrationRowCount != 1 {
		t.Errorf("schema_migrations row count = %d, want 1 after first Migrate()", migrationRowCount)
	}

	// Second run must be a no-op: no error, no duplicate rows.
	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("second Migrate() failed: %v", err)
	}

	var migrationRowCountAfter int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationRowCountAfter); err != nil {
		t.Fatalf("failed to count schema_migrations rows after second run: %v", err)
	}
	if migrationRowCountAfter != 1 {
		t.Errorf("schema_migrations row count after second Migrate() = %d, want 1 (idempotent)", migrationRowCountAfter)
	}
}
