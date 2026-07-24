package db_test

import (
	"strings"
	"testing"

	"spamfilter/internal/db"
	"spamfilter/internal/dbtest"
)

// TestMigrate_ClosedDBReturnsError confirms Migrate propagates the
// underlying error (rather than panicking) when the DB connection can't even
// create the schema_migrations bookkeeping table.
func TestMigrate_ClosedDBReturnsError(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	sqlDB.Close()

	if err := db.Migrate(sqlDB); err == nil {
		t.Fatal("Migrate: want error on a closed DB, got nil")
	}
}

// TestMigrate_IncompatibleExistingTableReturnsError confirms Migrate
// propagates an error when schema_migrations already exists but with a
// shape appliedVersions can't query (here, missing the "version" column) --
// distinct from the schema_migrations-table-creation failure above, since
// CREATE TABLE IF NOT EXISTS is a no-op against the pre-existing table and
// the failure instead comes from the subsequent SELECT.
func TestMigrate_IncompatibleExistingTableReturnsError(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)

	if _, err := sqlDB.Exec("DROP TABLE schema_migrations"); err != nil {
		t.Fatalf("drop schema_migrations: %v", err)
	}
	if _, err := sqlDB.Exec("CREATE TABLE schema_migrations (id INT PRIMARY KEY)"); err != nil {
		t.Fatalf("create incompatible schema_migrations: %v", err)
	}

	err := db.Migrate(sqlDB)
	if err == nil {
		t.Fatal("Migrate: want error for an incompatible pre-existing schema_migrations table, got nil")
	}
	if !strings.Contains(err.Error(), "reading applied migrations") {
		t.Errorf("error = %v, want it to mention 'reading applied migrations'", err)
	}
}

func TestMigrate_CreatesAllFiveTablesAndIsIdempotent(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)

	if err := db.Migrate(sqlDB); err != nil {
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

	const wantMigrations = 5 // 0001_init, 0002_drop_duplicate_number_index, 0003_device_sign_count, 0004_was_blockable, 0005_push_tokens

	var migrationRowCount int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationRowCount); err != nil {
		t.Fatalf("failed to count schema_migrations rows: %v", err)
	}
	if migrationRowCount != wantMigrations {
		t.Errorf("schema_migrations row count = %d, want %d after first Migrate()", migrationRowCount, wantMigrations)
	}

	// 0002 must have dropped the redundant non-unique index on number while
	// leaving the UNIQUE key intact.
	var dupIndexCount int
	if err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'phone_numbers' AND index_name = 'idx_phone_numbers_number'",
	).Scan(&dupIndexCount); err != nil {
		t.Fatalf("failed to check for duplicate index: %v", err)
	}
	if dupIndexCount != 0 {
		t.Errorf("idx_phone_numbers_number still present after 0002, want dropped")
	}
	var uniqueIndexCount int
	if err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'phone_numbers' AND index_name = 'uq_phone_numbers_number'",
	).Scan(&uniqueIndexCount); err != nil {
		t.Fatalf("failed to check for unique index: %v", err)
	}
	if uniqueIndexCount != 1 {
		t.Errorf("uq_phone_numbers_number count = %d, want 1 (must survive 0002)", uniqueIndexCount)
	}

	// 0003 must have added the sign_count column to devices.
	var signCountColCount int
	if err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'devices' AND column_name = 'sign_count'",
	).Scan(&signCountColCount); err != nil {
		t.Fatalf("failed to check for devices.sign_count column: %v", err)
	}
	if signCountColCount != 1 {
		t.Errorf("devices.sign_count column count = %d, want 1 (must be added by 0003)", signCountColCount)
	}

	// 0004 must have added the was_blockable column to phone_numbers.
	var wasBlockableColCount int
	if err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'phone_numbers' AND column_name = 'was_blockable'",
	).Scan(&wasBlockableColCount); err != nil {
		t.Fatalf("failed to check for phone_numbers.was_blockable column: %v", err)
	}
	if wasBlockableColCount != 1 {
		t.Errorf("phone_numbers.was_blockable column count = %d, want 1 (must be added by 0004)", wasBlockableColCount)
	}

	// 0005 must have added the three push columns to devices.
	for _, col := range []string{"push_token", "push_environment", "push_updated_at"} {
		var pushColCount int
		if err := sqlDB.QueryRow(
			"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'devices' AND column_name = ?",
			col,
		).Scan(&pushColCount); err != nil {
			t.Fatalf("failed to check for devices.%s column: %v", col, err)
		}
		if pushColCount != 1 {
			t.Errorf("devices.%s column count = %d, want 1 (must be added by 0005)", col, pushColCount)
		}
	}

	// Second run must be a no-op: no error, no duplicate rows.
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("second Migrate() failed: %v", err)
	}

	var migrationRowCountAfter int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationRowCountAfter); err != nil {
		t.Fatalf("failed to count schema_migrations rows after second run: %v", err)
	}
	if migrationRowCountAfter != wantMigrations {
		t.Errorf("schema_migrations row count after second Migrate() = %d, want %d (idempotent)", migrationRowCountAfter, wantMigrations)
	}
}
