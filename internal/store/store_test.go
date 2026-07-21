package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"spamfilter/internal/dbtest"
	"spamfilter/internal/scoring"
)

// insertDevice inserts a device row with the given trust_weight and returns
// its device_id.
func insertDevice(t *testing.T, sqlDB *sql.DB, keyID string, trustWeight float64) uint64 {
	t.Helper()
	res, err := sqlDB.Exec(
		"INSERT INTO devices (key_id, public_key, trust_weight) VALUES (?, ?, ?)",
		keyID, []byte("pubkey-"+keyID), trustWeight,
	)
	if err != nil {
		t.Fatalf("insert device: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return uint64(id)
}

func TestUpsertPhoneNumber(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	id1, err := UpsertPhoneNumber(ctx, sqlDB, "+14155550001", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}
	if id1 == 0 {
		t.Fatal("expected nonzero phone_number_id")
	}

	// Upserting the same number again must return the same id.
	later := now.Add(time.Hour)
	id2, err := UpsertPhoneNumber(ctx, sqlDB, "+14155550001", later)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber (again): %v", err)
	}
	if id2 != id1 {
		t.Errorf("phone_number_id changed on re-upsert: got %d, want %d", id2, id1)
	}

	var lastReportedAt time.Time
	if err := sqlDB.QueryRow("SELECT last_reported_at FROM phone_numbers WHERE phone_number_id = ?", id1).Scan(&lastReportedAt); err != nil {
		t.Fatalf("select last_reported_at: %v", err)
	}
	// MySQL TIMESTAMP rounds to the nearest second, so allow +/-1s slack
	// instead of asserting exact equality.
	if diff := lastReportedAt.Sub(later.UTC()); diff < -time.Second || diff > time.Second {
		t.Errorf("last_reported_at = %v, want ~%v", lastReportedAt, later)
	}
}

func TestUpsertReport(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	deviceID := insertDevice(t, sqlDB, "device-1", 1.0)
	phoneNumberID, err := UpsertPhoneNumber(ctx, sqlDB, "+14155550002", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}

	inserted, err := UpsertReport(ctx, sqlDB, deviceID, phoneNumberID, scoring.CategoryScam, scoring.VoteSpam, now)
	if err != nil {
		t.Fatalf("UpsertReport: %v", err)
	}
	if !inserted {
		t.Error("expected inserted = true for first report")
	}

	// Same device, same number, different vote: must update, not duplicate.
	inserted, err = UpsertReport(ctx, sqlDB, deviceID, phoneNumberID, scoring.CategoryRobocall, scoring.VoteNotSpam, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("UpsertReport (update): %v", err)
	}
	if inserted {
		t.Error("expected inserted = false when updating an existing report")
	}

	var count int
	if err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM reports WHERE device_id = ? AND phone_number_id = ?",
		deviceID, phoneNumberID,
	).Scan(&count); err != nil {
		t.Fatalf("count reports: %v", err)
	}
	if count != 1 {
		t.Fatalf("reports row count = %d, want 1 (unique per device+number)", count)
	}

	var category, vote string
	if err := sqlDB.QueryRow(
		"SELECT category, vote FROM reports WHERE device_id = ? AND phone_number_id = ?",
		deviceID, phoneNumberID,
	).Scan(&category, &vote); err != nil {
		t.Fatalf("select report: %v", err)
	}
	if category != string(scoring.CategoryRobocall) || vote != string(scoring.VoteNotSpam) {
		t.Errorf("report = (%s, %s), want (%s, %s)", category, vote, scoring.CategoryRobocall, scoring.VoteNotSpam)
	}
}

func TestUpsertCallerName(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	deviceID := insertDevice(t, sqlDB, "device-1", 1.0)
	phoneNumberID, err := UpsertPhoneNumber(ctx, sqlDB, "+14155550003", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}

	if err := UpsertCallerName(ctx, sqlDB, deviceID, phoneNumberID, "Robo Caller", now); err != nil {
		t.Fatalf("UpsertCallerName: %v", err)
	}
	if err := UpsertCallerName(ctx, sqlDB, deviceID, phoneNumberID, "Updated Name", now.Add(time.Minute)); err != nil {
		t.Fatalf("UpsertCallerName (update): %v", err)
	}

	var name string
	var count int
	if err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM caller_names WHERE device_id = ? AND phone_number_id = ?",
		deviceID, phoneNumberID,
	).Scan(&count); err != nil {
		t.Fatalf("count caller_names: %v", err)
	}
	if count != 1 {
		t.Fatalf("caller_names row count = %d, want 1", count)
	}
	if err := sqlDB.QueryRow(
		"SELECT name FROM caller_names WHERE device_id = ? AND phone_number_id = ?",
		deviceID, phoneNumberID,
	).Scan(&name); err != nil {
		t.Fatalf("select name: %v", err)
	}
	if name != "Updated Name" {
		t.Errorf("name = %q, want %q", name, "Updated Name")
	}
}

func TestTouchDevice(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	deviceID := insertDevice(t, sqlDB, "device-1", 1.0)

	if err := TouchDevice(ctx, sqlDB, deviceID, true, now); err != nil {
		t.Fatalf("TouchDevice: %v", err)
	}
	if err := TouchDevice(ctx, sqlDB, deviceID, true, now.Add(time.Minute)); err != nil {
		t.Fatalf("TouchDevice (again): %v", err)
	}
	if err := TouchDevice(ctx, sqlDB, deviceID, false, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("TouchDevice (no increment): %v", err)
	}

	var reportCount int
	if err := sqlDB.QueryRow("SELECT report_count FROM devices WHERE device_id = ?", deviceID).Scan(&reportCount); err != nil {
		t.Fatalf("select report_count: %v", err)
	}
	if reportCount != 2 {
		t.Errorf("report_count = %d, want 2", reportCount)
	}
}

func TestRecomputeNumber_ScamReportsCrossThresholds(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	// Fixed instant with a sub-second fraction below .5s: MySQL's TIMESTAMP
	// columns round created_at DOWN to 10:30:00 (verified against the actual
	// server), while RecomputeNumber's own `now` still carries the .1s
	// fraction. Without RecomputeNumber's now.Truncate(time.Second) fix, that
	// leaves a nonzero positive age gap (0.1s) between now and created_at,
	// giving a decay just under 1.0 and a score just under the 2.0
	// SuspectThreshold -- flipping this from "suspected" to "unknown". Using
	// a fixed instant (instead of time.Now()) makes this rounding-down edge
	// deterministic on every run rather than depending on which side of .5s
	// the wall clock's fraction happens to land on.
	now := time.Date(2024, 3, 15, 10, 30, 0, 100_000_000, time.UTC)

	phoneNumberID, err := UpsertPhoneNumber(ctx, sqlDB, "+14155550004", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}

	device1 := insertDevice(t, sqlDB, "device-1", 1.0)
	if _, err := UpsertReport(ctx, sqlDB, device1, phoneNumberID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
		t.Fatalf("UpsertReport: %v", err)
	}

	status, err := RecomputeNumber(ctx, sqlDB, phoneNumberID, now)
	if err != nil {
		t.Fatalf("RecomputeNumber: %v", err)
	}
	if status != scoring.StatusSuspected {
		t.Fatalf("status after 1 scam report = %s, want %s", status, scoring.StatusSuspected)
	}

	device2 := insertDevice(t, sqlDB, "device-2", 1.0)
	device3 := insertDevice(t, sqlDB, "device-3", 1.0)
	if _, err := UpsertReport(ctx, sqlDB, device2, phoneNumberID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
		t.Fatalf("UpsertReport device2: %v", err)
	}
	if _, err := UpsertReport(ctx, sqlDB, device3, phoneNumberID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
		t.Fatalf("UpsertReport device3: %v", err)
	}

	status, err = RecomputeNumber(ctx, sqlDB, phoneNumberID, now)
	if err != nil {
		t.Fatalf("RecomputeNumber (3 reports): %v", err)
	}
	if status != scoring.StatusBlocked {
		t.Fatalf("status after 3 scam reports = %s, want %s", status, scoring.StatusBlocked)
	}

	var cachedScore float64
	var reportCount, counterCount int
	var topCategory sql.NullString
	if err := sqlDB.QueryRow(
		"SELECT cached_score, report_count, counter_count, top_category FROM phone_numbers WHERE phone_number_id = ?",
		phoneNumberID,
	).Scan(&cachedScore, &reportCount, &counterCount, &topCategory); err != nil {
		t.Fatalf("select phone_numbers: %v", err)
	}
	if cachedScore < 5.9 || cachedScore > 6.1 {
		t.Errorf("cached_score = %v, want ~6.0", cachedScore)
	}
	if reportCount != 3 {
		t.Errorf("report_count = %d, want 3", reportCount)
	}
	if counterCount != 0 {
		t.Errorf("counter_count = %d, want 0", counterCount)
	}
	if !topCategory.Valid || topCategory.String != string(scoring.CategoryScam) {
		t.Errorf("top_category = %v, want scam", topCategory)
	}
}

func TestRecomputeNumber_WasBlockableStickyFlag(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	phoneNumberID, err := UpsertPhoneNumber(ctx, sqlDB, "+14155550007", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}

	// Freshly created, never reported: was_blockable must start false.
	var wasBlockable bool
	if err := sqlDB.QueryRow("SELECT was_blockable FROM phone_numbers WHERE phone_number_id = ?", phoneNumberID).Scan(&wasBlockable); err != nil {
		t.Fatalf("select was_blockable (initial): %v", err)
	}
	if wasBlockable {
		t.Fatalf("was_blockable = true before any recompute, want false")
	}

	device1 := insertDevice(t, sqlDB, "sticky-device-1", 1.0)
	device2 := insertDevice(t, sqlDB, "sticky-device-2", 1.0)
	device3 := insertDevice(t, sqlDB, "sticky-device-3", 1.0)
	for _, deviceID := range []uint64{device1, device2, device3} {
		if _, err := UpsertReport(ctx, sqlDB, deviceID, phoneNumberID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
			t.Fatalf("UpsertReport: %v", err)
		}
	}

	status, err := RecomputeNumber(ctx, sqlDB, phoneNumberID, now)
	if err != nil {
		t.Fatalf("RecomputeNumber (blocked): %v", err)
	}
	if status != scoring.StatusBlocked {
		t.Fatalf("status = %s, want %s", status, scoring.StatusBlocked)
	}
	if err := sqlDB.QueryRow("SELECT was_blockable FROM phone_numbers WHERE phone_number_id = ?", phoneNumberID).Scan(&wasBlockable); err != nil {
		t.Fatalf("select was_blockable (after blocked): %v", err)
	}
	if !wasBlockable {
		t.Fatalf("was_blockable = false after status went blocked, want true")
	}

	// All 3 devices flip to not_spam: status falls back to unknown, but
	// was_blockable must stay true (sticky, never resets 1->0).
	for _, deviceID := range []uint64{device1, device2, device3} {
		if _, err := UpsertReport(ctx, sqlDB, deviceID, phoneNumberID, scoring.CategoryScam, scoring.VoteNotSpam, now.Add(time.Minute)); err != nil {
			t.Fatalf("UpsertReport (not_spam): %v", err)
		}
	}
	status, err = RecomputeNumber(ctx, sqlDB, phoneNumberID, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("RecomputeNumber (unknown): %v", err)
	}
	if status != scoring.StatusUnknown {
		t.Fatalf("status = %s, want %s", status, scoring.StatusUnknown)
	}
	if err := sqlDB.QueryRow("SELECT was_blockable FROM phone_numbers WHERE phone_number_id = ?", phoneNumberID).Scan(&wasBlockable); err != nil {
		t.Fatalf("select was_blockable (after unknown): %v", err)
	}
	if !wasBlockable {
		t.Fatalf("was_blockable = false after falling back to unknown, want true (sticky flag)")
	}
}

func TestRecomputeNumber_NoOpDoesNotBumpUpdatedAt(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	number := "+14155550008"
	phoneNumberID, err := UpsertPhoneNumber(ctx, sqlDB, number, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}

	// Drive it blocked, then flip all votes to not_spam so it falls back to
	// unknown with a clamped cached_score of 0 and was_blockable=1 -- i.e. a
	// removal tombstone that lands in the blocklist delta. A clamped-0 score is
	// stable under a later `now` (further decay of an already-clamped score is
	// still 0), so a subsequent recompute is a genuine no-op even as the clock
	// advances -- exactly the RecomputeAll situation.
	devices := make([]uint64, 0, 3)
	for _, keyID := range []string{"noop-device-1", "noop-device-2", "noop-device-3"} {
		deviceID := insertDevice(t, sqlDB, keyID, 1.0)
		devices = append(devices, deviceID)
		if _, err := UpsertReport(ctx, sqlDB, deviceID, phoneNumberID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
			t.Fatalf("UpsertReport: %v", err)
		}
	}
	if _, err := RecomputeNumber(ctx, sqlDB, phoneNumberID, now); err != nil {
		t.Fatalf("RecomputeNumber (blocked): %v", err)
	}
	for _, deviceID := range devices {
		if _, err := UpsertReport(ctx, sqlDB, deviceID, phoneNumberID, scoring.CategoryScam, scoring.VoteNotSpam, now); err != nil {
			t.Fatalf("UpsertReport (not_spam): %v", err)
		}
	}
	if _, err := RecomputeNumber(ctx, sqlDB, phoneNumberID, now); err != nil {
		t.Fatalf("RecomputeNumber (first): %v", err)
	}

	var updatedAt1 time.Time
	if err := sqlDB.QueryRow("SELECT updated_at FROM phone_numbers WHERE phone_number_id = ?", phoneNumberID).Scan(&updatedAt1); err != nil {
		t.Fatalf("select updated_at (after first): %v", err)
	}

	// Capture the cursor after the first recompute.
	_, nextSec, nextID, err := BlocklistDelta(ctx, sqlDB, 0, 0, "", 500)
	if err != nil {
		t.Fatalf("BlocklistDelta (initial): %v", err)
	}

	// A second recompute with no intervening report must NOT re-stamp
	// updated_at: nothing tracked changed, so the UPDATE is skipped. Use a
	// strictly-later now to prove it isn't merely coincidental equality.
	if _, err := RecomputeNumber(ctx, sqlDB, phoneNumberID, now.Add(time.Hour)); err != nil {
		t.Fatalf("RecomputeNumber (no-op): %v", err)
	}

	var updatedAt2 time.Time
	if err := sqlDB.QueryRow("SELECT updated_at FROM phone_numbers WHERE phone_number_id = ?", phoneNumberID).Scan(&updatedAt2); err != nil {
		t.Fatalf("select updated_at (after no-op): %v", err)
	}
	if !updatedAt1.Equal(updatedAt2) {
		t.Errorf("updated_at changed on a no-op recompute: before=%v after=%v", updatedAt1, updatedAt2)
	}

	// And an incremental client at the post-first-recompute cursor sees nothing
	// after the no-op recompute -- the delta stays empty.
	entries, _, _, err := BlocklistDelta(ctx, sqlDB, nextSec, nextID, "", 500)
	if err != nil {
		t.Fatalf("BlocklistDelta (since cursor): %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("delta since cursor returned %d entries after a no-op recompute, want 0", len(entries))
	}
}

func TestRecomputeNumber_OverrideBlock(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	phoneNumberID, err := UpsertPhoneNumber(ctx, sqlDB, "+14155550005", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}
	if _, err := sqlDB.Exec(
		"INSERT INTO admin_overrides (phone_number_id, mode, admin) VALUES (?, 'block', 'test-admin')",
		phoneNumberID,
	); err != nil {
		t.Fatalf("insert admin_overrides: %v", err)
	}

	status, err := RecomputeNumber(ctx, sqlDB, phoneNumberID, now)
	if err != nil {
		t.Fatalf("RecomputeNumber: %v", err)
	}
	if status != scoring.StatusOverriddenBlock {
		t.Fatalf("status = %s, want %s", status, scoring.StatusOverriddenBlock)
	}

	var wasBlockable bool
	if err := sqlDB.QueryRow("SELECT was_blockable FROM phone_numbers WHERE phone_number_id = ?", phoneNumberID).Scan(&wasBlockable); err != nil {
		t.Fatalf("select was_blockable: %v", err)
	}
	if !wasBlockable {
		t.Fatalf("was_blockable = false after overridden_block, want true")
	}
}

func TestUpsertReport_UsesTransaction(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	deviceID := insertDevice(t, sqlDB, "device-1", 1.0)

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	phoneNumberID, err := UpsertPhoneNumber(ctx, tx, "+14155550006", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber (tx): %v", err)
	}
	inserted, err := UpsertReport(ctx, tx, deviceID, phoneNumberID, scoring.CategoryOther, scoring.VoteSpam, now)
	if err != nil {
		t.Fatalf("UpsertReport (tx): %v", err)
	}
	if !inserted {
		t.Error("expected inserted = true")
	}
	if err := UpsertCallerName(ctx, tx, deviceID, phoneNumberID, "Test Caller", now); err != nil {
		t.Fatalf("UpsertCallerName (tx): %v", err)
	}
	if err := TouchDevice(ctx, tx, deviceID, inserted, now); err != nil {
		t.Fatalf("TouchDevice (tx): %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var count int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM reports WHERE phone_number_id = ?", phoneNumberID).Scan(&count); err != nil {
		t.Fatalf("count reports: %v", err)
	}
	if count != 1 {
		t.Errorf("reports count = %d, want 1", count)
	}
}
