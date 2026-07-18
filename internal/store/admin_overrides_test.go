package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"spamfilter/internal/dbtest"
)

func TestUpsertOverride_InsertsAndUpdatesSingleRow(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	numberID, err := UpsertPhoneNumber(ctx, sqlDB, "+14155559101", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}

	if err := UpsertOverride(ctx, sqlDB, numberID, "block", "known scammer", "admin-1", now); err != nil {
		t.Fatalf("UpsertOverride (insert): %v", err)
	}

	var mode, reason, admin string
	if err := sqlDB.QueryRow(
		"SELECT mode, reason, admin FROM admin_overrides WHERE phone_number_id = ?", numberID,
	).Scan(&mode, &reason, &admin); err != nil {
		t.Fatalf("select admin_overrides: %v", err)
	}
	if mode != "block" || reason != "known scammer" || admin != "admin-1" {
		t.Errorf("got mode=%q reason=%q admin=%q, want block/known scammer/admin-1", mode, reason, admin)
	}

	// Re-upserting for the same phone_number_id (different mode/reason/admin)
	// must update the existing row, not create a second one.
	if err := UpsertOverride(ctx, sqlDB, numberID, "allow", "", "admin-2", now); err != nil {
		t.Fatalf("UpsertOverride (update): %v", err)
	}

	var count int
	if err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM admin_overrides WHERE phone_number_id = ?", numberID,
	).Scan(&count); err != nil {
		t.Fatalf("count admin_overrides: %v", err)
	}
	if count != 1 {
		t.Errorf("admin_overrides row count = %d, want 1", count)
	}

	var mode2, admin2 string
	var reason2 sql.NullString
	if err := sqlDB.QueryRow(
		"SELECT mode, reason, admin FROM admin_overrides WHERE phone_number_id = ?", numberID,
	).Scan(&mode2, &reason2, &admin2); err != nil {
		t.Fatalf("select admin_overrides (after update): %v", err)
	}
	if mode2 != "allow" || reason2.Valid || admin2 != "admin-2" {
		t.Errorf("got mode=%q reason=%+v admin=%q, want allow/NULL/admin-2", mode2, reason2, admin2)
	}
}
