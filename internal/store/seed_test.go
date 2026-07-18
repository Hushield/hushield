package store

import (
	"context"
	"testing"
	"time"

	"spamfilter/internal/dbtest"
	"spamfilter/internal/scoring"
)

func TestEnsureSeedDevice_CreatesThenUpdatesTrust(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	deviceID, err := EnsureSeedDevice(ctx, sqlDB, "ftc", 1.5, now)
	if err != nil {
		t.Fatalf("EnsureSeedDevice: %v", err)
	}

	var keyID string
	var publicKey []byte
	var trustWeight float64
	if err := sqlDB.QueryRow(
		"SELECT key_id, public_key, trust_weight FROM devices WHERE device_id = ?", deviceID,
	).Scan(&keyID, &publicKey, &trustWeight); err != nil {
		t.Fatalf("select device: %v", err)
	}
	if keyID != "seed:ftc" {
		t.Errorf("key_id = %q, want %q", keyID, "seed:ftc")
	}
	if string(publicKey) != "seed" {
		t.Errorf("public_key = %q, want %q", publicKey, "seed")
	}
	if trustWeight != 1.5 {
		t.Errorf("trust_weight = %v, want 1.5", trustWeight)
	}

	// Re-running with a different trust must update the same row, not
	// create a second device.
	deviceID2, err := EnsureSeedDevice(ctx, sqlDB, "ftc", 2.0, now)
	if err != nil {
		t.Fatalf("EnsureSeedDevice (second call): %v", err)
	}
	if deviceID2 != deviceID {
		t.Errorf("second call device_id = %d, want %d (same row)", deviceID2, deviceID)
	}

	var count int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM devices WHERE key_id = ?", "seed:ftc").Scan(&count); err != nil {
		t.Fatalf("count devices: %v", err)
	}
	if count != 1 {
		t.Errorf("device count for seed:ftc = %d, want 1", count)
	}

	if err := sqlDB.QueryRow("SELECT trust_weight FROM devices WHERE device_id = ?", deviceID).Scan(&trustWeight); err != nil {
		t.Fatalf("select updated trust_weight: %v", err)
	}
	if trustWeight != 2.0 {
		t.Errorf("trust_weight after update = %v, want 2.0", trustWeight)
	}
}

func TestUpsertSeedNumber_SetsSourceAndDoesNotClobberOnDuplicate(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	phoneNumberID, err := UpsertSeedNumber(ctx, sqlDB, "+14155551234", "ftc", now)
	if err != nil {
		t.Fatalf("UpsertSeedNumber: %v", err)
	}

	var source string
	if err := sqlDB.QueryRow("SELECT source FROM phone_numbers WHERE phone_number_id = ?", phoneNumberID).Scan(&source); err != nil {
		t.Fatalf("select source: %v", err)
	}
	if source != "ftc" {
		t.Errorf("source = %q, want %q", source, "ftc")
	}

	// A number first reported by the community keeps source='community'
	// even if a later seed import also covers it.
	communityNumberID, err := UpsertPhoneNumber(ctx, sqlDB, "+14155555678", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}
	later := now.Add(time.Hour)
	sameID, err := UpsertSeedNumber(ctx, sqlDB, "+14155555678", "fcc", later)
	if err != nil {
		t.Fatalf("UpsertSeedNumber (duplicate): %v", err)
	}
	if sameID != communityNumberID {
		t.Errorf("UpsertSeedNumber returned id %d, want %d (same row)", sameID, communityNumberID)
	}
	if err := sqlDB.QueryRow("SELECT source FROM phone_numbers WHERE phone_number_id = ?", communityNumberID).Scan(&source); err != nil {
		t.Fatalf("select source after duplicate: %v", err)
	}
	if source != "community" {
		t.Errorf("source after duplicate seed upsert = %q, want unchanged %q", source, "community")
	}

	var lastReportedAt time.Time
	if err := sqlDB.QueryRow("SELECT last_reported_at FROM phone_numbers WHERE phone_number_id = ?", communityNumberID).Scan(&lastReportedAt); err != nil {
		t.Fatalf("select last_reported_at: %v", err)
	}
	// MySQL TIMESTAMP rounds to the nearest second, so allow slack rather
	// than requiring an exact match.
	if diff := lastReportedAt.Sub(later); diff < -time.Second || diff > time.Second {
		t.Errorf("last_reported_at = %v, want ~%v (touched by duplicate upsert)", lastReportedAt, later)
	}
}

func TestRecomputeAllTrust_ExcludesSeedDevices(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	seedDeviceID, err := EnsureSeedDevice(ctx, sqlDB, "ftc", 1.5, now)
	if err != nil {
		t.Fatalf("EnsureSeedDevice: %v", err)
	}
	numberID, err := UpsertSeedNumber(ctx, sqlDB, "+14155559999", "ftc", now)
	if err != nil {
		t.Fatalf("UpsertSeedNumber: %v", err)
	}
	if _, err := UpsertReport(ctx, sqlDB, seedDeviceID, numberID, scoring.CategoryRobocall, scoring.VoteSpam, now); err != nil {
		t.Fatalf("UpsertReport: %v", err)
	}

	// A regular device must still be recomputed.
	regularDeviceID := insertDevice(t, sqlDB, "regular-device", 1.0)

	count, err := RecomputeAllTrust(ctx, sqlDB, now)
	if err != nil {
		t.Fatalf("RecomputeAllTrust: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (seed device excluded)", count)
	}

	var seedTrust float64
	if err := sqlDB.QueryRow("SELECT trust_weight FROM devices WHERE device_id = ?", seedDeviceID).Scan(&seedTrust); err != nil {
		t.Fatalf("select seed device trust_weight: %v", err)
	}
	if seedTrust != 1.5 {
		t.Errorf("seed device trust_weight after RecomputeAllTrust = %v, want unchanged 1.5", seedTrust)
	}

	var regularReportCount int
	if err := sqlDB.QueryRow("SELECT report_count FROM devices WHERE device_id = ?", regularDeviceID).Scan(&regularReportCount); err != nil {
		t.Fatalf("select regular device report_count: %v", err)
	}
	if regularReportCount != 0 {
		t.Errorf("regular device report_count = %d, want 0", regularReportCount)
	}
}
