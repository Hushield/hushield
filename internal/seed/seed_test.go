package seed

import (
	"context"
	"errors"
	"testing"
	"time"

	"spamfilter/internal/dbtest"
	"spamfilter/internal/scoring"
	"spamfilter/internal/store"
)

// mockSource is a fixed, in-memory Source used to test Seeder.Seed without
// touching disk.
type mockSource struct {
	records []Record
}

func (s mockSource) Records(ctx context.Context) ([]Record, error) {
	return s.records, nil
}

func TestSeeder_Seed_ImportsValidNumbersAndSkipsInvalid(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	src := mockSource{records: []Record{
		{RawNumber: "+14155551111"},
		{RawNumber: "+14155552222"},
		{RawNumber: "not-a-number"},
	}}
	seeder := Seeder{DB: sqlDB}

	imported, skipped, err := seeder.Seed(ctx, src, "ftc", 1.5, scoring.CategoryRobocall)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if imported != 2 {
		t.Errorf("imported = %d, want 2", imported)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}

	var deviceID uint64
	var trustWeight float64
	if err := sqlDB.QueryRow("SELECT device_id, trust_weight FROM devices WHERE key_id = ?", "seed:ftc").Scan(&deviceID, &trustWeight); err != nil {
		t.Fatalf("select seed device: %v", err)
	}
	if trustWeight != 1.5 {
		t.Errorf("seed device trust_weight = %v, want 1.5", trustWeight)
	}

	var numberCount int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM phone_numbers WHERE source = ?", "ftc").Scan(&numberCount); err != nil {
		t.Fatalf("count phone_numbers: %v", err)
	}
	if numberCount != 2 {
		t.Errorf("phone_numbers with source=ftc = %d, want 2", numberCount)
	}

	// Seed trust 1.5 x robocall multiplier 1.5 = 2.25, above SuspectThreshold
	// (2.0) and below BlockThreshold (5.0): status must be suspected
	// immediately after Seed, without any separate recompute step.
	var status string
	if err := sqlDB.QueryRow("SELECT status FROM phone_numbers WHERE number = ?", "+14155551111").Scan(&status); err != nil {
		t.Fatalf("select status: %v", err)
	}
	if status != string(scoring.StatusSuspected) {
		t.Errorf("status = %q, want %q", status, scoring.StatusSuspected)
	}

	var phoneNumberID uint64
	if err := sqlDB.QueryRow("SELECT phone_number_id FROM phone_numbers WHERE number = ?", "+14155551111").Scan(&phoneNumberID); err != nil {
		t.Fatalf("select phone_number_id: %v", err)
	}

	// Idempotency: re-running Seed must not duplicate the device, the
	// numbers, or the reports.
	imported2, skipped2, err := seeder.Seed(ctx, src, "ftc", 1.5, scoring.CategoryRobocall)
	if err != nil {
		t.Fatalf("Seed (second run): %v", err)
	}
	if imported2 != 2 {
		t.Errorf("imported (second run) = %d, want 2", imported2)
	}
	if skipped2 != 1 {
		t.Errorf("skipped (second run) = %d, want 1", skipped2)
	}

	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM devices WHERE key_id = ?", "seed:ftc").Scan(&numberCount); err != nil {
		t.Fatalf("count devices: %v", err)
	}
	if numberCount != 1 {
		t.Errorf("devices with key_id=seed:ftc after second run = %d, want 1", numberCount)
	}

	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM phone_numbers WHERE source = ?", "ftc").Scan(&numberCount); err != nil {
		t.Fatalf("count phone_numbers after second run: %v", err)
	}
	if numberCount != 2 {
		t.Errorf("phone_numbers with source=ftc after second run = %d, want 2", numberCount)
	}

	var reportCount int
	if err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM reports WHERE reports.device_id = ? AND reports.phone_number_id = ?",
		deviceID, phoneNumberID,
	).Scan(&reportCount); err != nil {
		t.Fatalf("count reports: %v", err)
	}
	if reportCount != 1 {
		t.Errorf("reports for (seed device, number) after second run = %d, want 1", reportCount)
	}
}

// errorSource is a Source whose Records always fails, used to exercise
// Seed's "src.Records returns an error" branch.
type errorSource struct{ err error }

func (s errorSource) Records(ctx context.Context) ([]Record, error) {
	return nil, s.err
}

func TestSeeder_Seed_SourceErrorPropagates(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	seeder := Seeder{DB: sqlDB}
	_, _, err := seeder.Seed(ctx, errorSource{err: errors.New("boom: file unreadable")}, "ftc", 1.5, scoring.CategoryRobocall)
	if err == nil {
		t.Fatal("Seed: want error when the source fails, got nil")
	}
}

// TestSeeder_Seed_ExplicitCategoryOverridesDefault confirms a record's own
// non-blank Category wins over the job's defaultCategory, rather than always
// falling back to it.
func TestSeeder_Seed_ExplicitCategoryOverridesDefault(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	src := mockSource{records: []Record{
		{RawNumber: "+14155554444", Category: scoring.CategoryTelemarketer},
	}}
	seeder := Seeder{DB: sqlDB}

	imported, _, err := seeder.Seed(ctx, src, "fcc", 1.0, scoring.CategoryRobocall)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want 1", imported)
	}

	var category string
	if err := sqlDB.QueryRow(
		"SELECT reports.category FROM reports JOIN phone_numbers ON reports.phone_number_id = phone_numbers.phone_number_id WHERE phone_numbers.number = ?",
		"+14155554444",
	).Scan(&category); err != nil {
		t.Fatalf("select report category: %v", err)
	}
	if category != string(scoring.CategoryTelemarketer) {
		t.Errorf("report category = %q, want %q (record's own category, not the job default)", category, scoring.CategoryTelemarketer)
	}
}

// TestSeeder_Seed_EnsureSeedDeviceErrorPropagates confirms Seed surfaces the
// error when EnsureSeedDevice itself fails (a closed DB), before ever
// touching the source.
func TestSeeder_Seed_EnsureSeedDeviceErrorPropagates(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	sqlDB.Close()

	seeder := Seeder{DB: sqlDB}
	_, _, err := seeder.Seed(context.Background(), mockSource{}, "ftc", 1.5, scoring.CategoryRobocall)
	if err == nil {
		t.Fatal("Seed: want error when EnsureSeedDevice fails, got nil")
	}
}

func TestSeeder_Seed_RecomputeAllTrustLeavesSeedTrustUnchanged(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	src := mockSource{records: []Record{
		{RawNumber: "+14155553333"},
	}}
	seeder := Seeder{DB: sqlDB}

	if _, _, err := seeder.Seed(ctx, src, "ftc", 1.5, scoring.CategoryRobocall); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	if _, err := store.RecomputeAllTrust(ctx, sqlDB, time.Now()); err != nil {
		t.Fatalf("RecomputeAllTrust: %v", err)
	}

	var trustWeight float64
	if err := sqlDB.QueryRow("SELECT trust_weight FROM devices WHERE key_id = ?", "seed:ftc").Scan(&trustWeight); err != nil {
		t.Fatalf("select seed device trust_weight: %v", err)
	}
	if trustWeight != 1.5 {
		t.Errorf("seed device trust_weight after RecomputeAllTrust = %v, want unchanged 1.5", trustWeight)
	}
}
