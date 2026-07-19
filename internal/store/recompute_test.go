package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"spamfilter/internal/dbtest"
	"spamfilter/internal/scoring"
)

// insertDeviceWithFirstSeen inserts a device row with an explicit
// first_seen_at (which the plain insertDevice helper leaves at its
// CURRENT_TIMESTAMP default), and returns its device_id.
func insertDeviceWithFirstSeen(t *testing.T, sqlDB *sql.DB, keyID string, trustWeight float64, firstSeenAt time.Time) uint64 {
	t.Helper()
	res, err := sqlDB.Exec(
		"INSERT INTO devices (key_id, public_key, trust_weight, first_seen_at) VALUES (?, ?, ?, ?)",
		keyID, []byte("pubkey-"+keyID), trustWeight, firstSeenAt.UTC(),
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

func TestRecomputeAllNumbers_DecayLowersStatus(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()
	past := now.Add(-90 * 24 * time.Hour)

	// A number with 3 scam reports from 90 days ago: decayed well below the
	// block threshold (0.5^(90/30) = 0.125 decay factor per report), so it
	// must not still read as blocked.
	oldNumberID, err := UpsertPhoneNumber(ctx, sqlDB, "+14155551001", past)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber (old): %v", err)
	}
	for i, keyID := range []string{"decay-device-1", "decay-device-2", "decay-device-3"} {
		deviceID := insertDevice(t, sqlDB, keyID, 1.0)
		if _, err := UpsertReport(ctx, sqlDB, deviceID, oldNumberID, scoring.CategoryScam, scoring.VoteSpam, past); err != nil {
			t.Fatalf("UpsertReport old #%d: %v", i, err)
		}
	}
	// Seed the cached status as blocked (as if RecomputeNumber had run right
	// after the reports were filed 90 days ago), so this test proves decay
	// actively lowers a previously-blocked status rather than merely
	// confirming a status that was never elevated in the first place.
	if _, err := sqlDB.Exec("UPDATE phone_numbers SET status = 'blocked' WHERE phone_number_id = ?", oldNumberID); err != nil {
		t.Fatalf("seed old status: %v", err)
	}

	// A contrasting number with 3 fresh scam reports: must stay blocked.
	freshNumberID, err := UpsertPhoneNumber(ctx, sqlDB, "+14155551002", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber (fresh): %v", err)
	}
	for i, keyID := range []string{"fresh-device-1", "fresh-device-2", "fresh-device-3"} {
		deviceID := insertDevice(t, sqlDB, keyID, 1.0)
		if _, err := UpsertReport(ctx, sqlDB, deviceID, freshNumberID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
			t.Fatalf("UpsertReport fresh #%d: %v", i, err)
		}
	}

	count, err := RecomputeAllNumbers(ctx, sqlDB, now)
	if err != nil {
		t.Fatalf("RecomputeAllNumbers: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	var oldStatus string
	var oldScore float64
	if err := sqlDB.QueryRow("SELECT status, cached_score FROM phone_numbers WHERE phone_number_id = ?", oldNumberID).Scan(&oldStatus, &oldScore); err != nil {
		t.Fatalf("select old number: %v", err)
	}
	if oldStatus == string(scoring.StatusBlocked) {
		t.Errorf("old number status = %s, want decayed below blocked", oldStatus)
	}
	// 3 reports * BaseWeight(1) * trust(1) * scam multiplier(2) * decay(0.125) = 0.75.
	if oldScore < 0.7 || oldScore > 0.8 {
		t.Errorf("old number cached_score = %v, want ~0.75 (decayed)", oldScore)
	}

	var freshStatus string
	if err := sqlDB.QueryRow("SELECT status FROM phone_numbers WHERE phone_number_id = ?", freshNumberID).Scan(&freshStatus); err != nil {
		t.Fatalf("select fresh number: %v", err)
	}
	if freshStatus != string(scoring.StatusBlocked) {
		t.Errorf("fresh number status = %s, want %s", freshStatus, scoring.StatusBlocked)
	}
}

func TestRecomputeAllTrust_RisesWithHistoryAndStartsAtBase(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()
	sixtyDaysAgo := now.Add(-60 * 24 * time.Hour)

	seasonedDeviceID := insertDeviceWithFirstSeen(t, sqlDB, "seasoned-device", 1.0, sixtyDaysAgo)
	for i := 0; i < 20; i++ {
		numberID, err := UpsertPhoneNumber(ctx, sqlDB, phoneNumberForIndex(i), now)
		if err != nil {
			t.Fatalf("UpsertPhoneNumber #%d: %v", i, err)
		}
		if _, err := UpsertReport(ctx, sqlDB, seasonedDeviceID, numberID, scoring.CategoryOther, scoring.VoteSpam, now); err != nil {
			t.Fatalf("UpsertReport #%d: %v", i, err)
		}
	}

	newDeviceID := insertDevice(t, sqlDB, "brand-new-device", 1.0)

	count, err := RecomputeAllTrust(ctx, sqlDB, now)
	if err != nil {
		t.Fatalf("RecomputeAllTrust: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	var seasonedTrust float64
	var seasonedReportCount int
	if err := sqlDB.QueryRow("SELECT trust_weight, report_count FROM devices WHERE device_id = ?", seasonedDeviceID).Scan(&seasonedTrust, &seasonedReportCount); err != nil {
		t.Fatalf("select seasoned device: %v", err)
	}
	if diff := seasonedTrust - 2.0; diff < -0.01 || diff > 0.01 {
		t.Errorf("seasoned device trust_weight = %v, want ~2.0", seasonedTrust)
	}
	if seasonedReportCount != 20 {
		t.Errorf("seasoned device report_count = %d, want 20", seasonedReportCount)
	}

	var newTrust float64
	if err := sqlDB.QueryRow("SELECT trust_weight FROM devices WHERE device_id = ?", newDeviceID).Scan(&newTrust); err != nil {
		t.Fatalf("select new device: %v", err)
	}
	if diff := newTrust - 0.5; diff < -0.01 || diff > 0.01 {
		t.Errorf("brand-new device trust_weight = %v, want ~0.5", newTrust)
	}
}

func phoneNumberForIndex(i int) string {
	return "+1415555" + string(rune('0'+i/10)) + string(rune('0'+i%10)) + "01"
}

func TestRecomputeAllTrust_DisagreementPenalty(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	deviceID := insertDevice(t, sqlDB, "disagreeing-device", 1.0)
	numberID, err := UpsertPhoneNumber(ctx, sqlDB, "+14155559001", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}
	if _, err := UpsertReport(ctx, sqlDB, deviceID, numberID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
		t.Fatalf("UpsertReport: %v", err)
	}

	// Admin overrides the community verdict to allowlisted: this device's
	// spam report is now contradicted by the final outcome.
	if _, err := sqlDB.Exec(
		"INSERT INTO admin_overrides (phone_number_id, mode, admin) VALUES (?, 'allow', 'test-admin')",
		numberID,
	); err != nil {
		t.Fatalf("insert admin_overrides: %v", err)
	}
	status, err := RecomputeNumber(ctx, sqlDB, numberID, now)
	if err != nil {
		t.Fatalf("RecomputeNumber: %v", err)
	}
	if status != scoring.StatusAllowlisted {
		t.Fatalf("status = %s, want %s", status, scoring.StatusAllowlisted)
	}

	if _, err := RecomputeAllTrust(ctx, sqlDB, now); err != nil {
		t.Fatalf("RecomputeAllTrust: %v", err)
	}

	var disagreements int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM reports JOIN phone_numbers ON reports.phone_number_id = phone_numbers.phone_number_id
WHERE reports.device_id = ? AND ((reports.vote = 'spam' AND phone_numbers.status = 'allowlisted')
OR (reports.vote = 'not_spam' AND phone_numbers.status = 'blocked'))`, deviceID).Scan(&disagreements); err != nil {
		t.Fatalf("count disagreements: %v", err)
	}
	if disagreements < 1 {
		t.Fatalf("disagreements = %d, want >= 1", disagreements)
	}

	var trustWeight float64
	if err := sqlDB.QueryRow("SELECT trust_weight FROM devices WHERE device_id = ?", deviceID).Scan(&trustWeight); err != nil {
		t.Fatalf("select trust_weight: %v", err)
	}
	// reportCount=1, ageDays~0, disagreements=1: 0.5 + 0 + 0.05 - 0.25 = 0.3.
	if diff := trustWeight - 0.3; diff < -0.01 || diff > 0.01 {
		t.Errorf("trust_weight = %v, want ~0.3 (penalized)", trustWeight)
	}
}

func TestRecomputeAll_RunsBothPassesAndReturnsCounts(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	device1 := insertDevice(t, sqlDB, "recompute-all-device-1", 1.0)
	device2 := insertDevice(t, sqlDB, "recompute-all-device-2", 1.0)
	number1, err := UpsertPhoneNumber(ctx, sqlDB, "+14155558001", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber #1: %v", err)
	}
	number2, err := UpsertPhoneNumber(ctx, sqlDB, "+14155558002", now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber #2: %v", err)
	}
	if _, err := UpsertReport(ctx, sqlDB, device1, number1, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
		t.Fatalf("UpsertReport device1: %v", err)
	}
	if _, err := UpsertReport(ctx, sqlDB, device2, number2, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
		t.Fatalf("UpsertReport device2: %v", err)
	}

	numbers, devices, err := RecomputeAll(ctx, sqlDB, now)
	if err != nil {
		t.Fatalf("RecomputeAll: %v", err)
	}
	if numbers != 2 {
		t.Errorf("numbers = %d, want 2", numbers)
	}
	if devices != 2 {
		t.Errorf("devices = %d, want 2", devices)
	}
}
