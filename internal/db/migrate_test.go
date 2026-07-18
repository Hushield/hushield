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

	var migrationRowCount int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationRowCount); err != nil {
		t.Fatalf("failed to count schema_migrations rows: %v", err)
	}
	if migrationRowCount != 1 {
		t.Errorf("schema_migrations row count = %d, want 1 after first Migrate()", migrationRowCount)
	}

	// Second run must be a no-op: no error, no duplicate rows.
	if err := db.Migrate(sqlDB); err != nil {
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
