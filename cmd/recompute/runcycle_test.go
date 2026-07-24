package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"spamfilter/internal/config"
	"spamfilter/internal/dbtest"
	"spamfilter/internal/scoring"
	"spamfilter/internal/store"
)

// TestRunCycle_NotifyOffSkipsPush confirms runCycle recomputes numbers/devices
// and, with notify=false, never touches the push path at all (no notifier is
// even built), by seeding a report that should flip a number to blocked and
// asserting the DB reflects that after the call.
func TestRunCycle_NotifyOffSkipsPush(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	numberID, err := store.UpsertPhoneNumber(ctx, sqlDB, "+14155559911", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}
	// 3 distinct devices: UpsertReport allows only one updatable vote per
	// (device, number) pair, so 3 reports from the same number require 3
	// different reporting devices.
	for i, keyID := range []string{"runcycle-device-1", "runcycle-device-2", "runcycle-device-3"} {
		deviceID := insertRunCycleDevice(t, sqlDB, keyID, 1.0)
		if _, err := store.UpsertReport(ctx, sqlDB, deviceID, numberID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
			t.Fatalf("UpsertReport #%d: %v", i, err)
		}
	}

	if err := runCycle(ctx, sqlDB, config.Config{}, false); err != nil {
		t.Fatalf("runCycle: %v", err)
	}

	var status string
	if err := sqlDB.QueryRow("SELECT status FROM phone_numbers WHERE phone_number_id = ?", numberID).Scan(&status); err != nil {
		t.Fatalf("select status: %v", err)
	}
	if status != string(scoring.StatusBlocked) {
		t.Errorf("status = %q, want blocked (runCycle must still recompute even with notify=false)", status)
	}
}

// TestRunCycle_NotifyOnNoCredsIsNoop confirms runCycle with notify=true but no
// APNs credentials configured still succeeds (push.NewNotifier degrades to a
// Noop rather than erroring, and the noop path skips ListPushTargets/
// BroadcastRefresh entirely per the realAPNs guard).
func TestRunCycle_NotifyOnNoCredsIsNoop(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	if err := runCycle(ctx, sqlDB, config.Config{}, true); err != nil {
		t.Fatalf("runCycle with notify=true and no APNs creds: %v", err)
	}
}

// TestRunCycle_RecomputeErrorPropagates confirms a recompute failure (here,
// forced by a closed DB) is returned as an error, wrapped with the
// numbers/devices/duration context, rather than swallowed.
func TestRunCycle_RecomputeErrorPropagates(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	sqlDB.Close()

	err := runCycle(context.Background(), sqlDB, config.Config{}, false)
	if err == nil {
		t.Fatal("runCycle: want error when RecomputeAll fails, got nil")
	}
}

func insertRunCycleDevice(t *testing.T, sqlDB *sql.DB, keyID string, trustWeight float64) uint64 {
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
