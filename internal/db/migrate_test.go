package db_test

import (
	"testing"

	"spamfilter/internal/db"
	"spamfilter/internal/dbtest"
)

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

	const wantMigrations = 3 // 0001_init, 0002_drop_duplicate_number_index, 0003_device_sign_count

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
