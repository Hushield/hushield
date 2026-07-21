package store

import (
	"context"
	"testing"
	"time"

	"spamfilter/internal/dbtest"
	"spamfilter/internal/scoring"
)

func TestLookupNumber_Blocked(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	number := "+14155561101"
	numberID, err := UpsertPhoneNumber(ctx, sqlDB, number, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}
	for _, keyID := range []string{"lookup-device-1", "lookup-device-2", "lookup-device-3"} {
		deviceID := insertDevice(t, sqlDB, keyID, 1.0)
		if _, err := UpsertReport(ctx, sqlDB, deviceID, numberID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
			t.Fatalf("UpsertReport: %v", err)
		}
	}
	if _, err := RecomputeNumber(ctx, sqlDB, numberID, now); err != nil {
		t.Fatalf("RecomputeNumber: %v", err)
	}

	result, found, err := LookupNumber(ctx, sqlDB, number, "")
	if err != nil {
		t.Fatalf("LookupNumber: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if result.Status != scoring.StatusBlocked {
		t.Errorf("status = %q, want %q", result.Status, scoring.StatusBlocked)
	}
	if result.Category == nil || *result.Category != scoring.CategoryScam {
		t.Errorf("category = %v, want %q", result.Category, scoring.CategoryScam)
	}
	if result.CachedScore <= 0 {
		t.Errorf("cached score = %f, want > 0", result.CachedScore)
	}
	if result.Number != number {
		t.Errorf("number = %q, want %q", result.Number, number)
	}
}

func TestLookupNumber_NotFound(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	_, found, err := LookupNumber(ctx, sqlDB, "+14155569999", "")
	if err != nil {
		t.Fatalf("LookupNumber: %v", err)
	}
	if found {
		t.Errorf("found = true, want false for a number never inserted")
	}
}

func TestLookupNumber_NameAgreementFloor(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	agreedNumber := "+14155561201"
	agreedID, err := UpsertPhoneNumber(ctx, sqlDB, agreedNumber, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber (agreed): %v", err)
	}
	nameDevice1 := insertDevice(t, sqlDB, "lookup-name-1", 1.0)
	nameDevice2 := insertDevice(t, sqlDB, "lookup-name-2", 1.0)
	if err := UpsertCallerName(ctx, sqlDB, nameDevice1, agreedID, "Acme Collections", now); err != nil {
		t.Fatalf("UpsertCallerName (device1): %v", err)
	}
	if err := UpsertCallerName(ctx, sqlDB, nameDevice2, agreedID, "Acme Collections", now); err != nil {
		t.Fatalf("UpsertCallerName (device2): %v", err)
	}

	result, found, err := LookupNumber(ctx, sqlDB, agreedNumber, "")
	if err != nil {
		t.Fatalf("LookupNumber (agreed): %v", err)
	}
	if !found {
		t.Fatalf("agreed number not found")
	}
	if result.Name == nil || *result.Name != "Acme Collections" {
		t.Errorf("name = %v, want %q", result.Name, "Acme Collections")
	}

	loneNumber := "+14155561202"
	loneID, err := UpsertPhoneNumber(ctx, sqlDB, loneNumber, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber (lone): %v", err)
	}
	loneNameDevice := insertDevice(t, sqlDB, "lookup-name-lone", 1.0)
	if err := UpsertCallerName(ctx, sqlDB, loneNameDevice, loneID, "Solo Name", now); err != nil {
		t.Fatalf("UpsertCallerName (lone): %v", err)
	}

	loneResult, found, err := LookupNumber(ctx, sqlDB, loneNumber, "")
	if err != nil {
		t.Fatalf("LookupNumber (lone): %v", err)
	}
	if !found {
		t.Fatalf("lone number not found")
	}
	if loneResult.Name != nil {
		t.Errorf("name = %q, want nil (below agreement floor)", *loneResult.Name)
	}
}

func TestLookupNumber_SpoofSuspected(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	number := "+14155561301"
	if _, err := UpsertPhoneNumber(ctx, sqlDB, number, now); err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}

	result, found, err := LookupNumber(ctx, sqlDB, number, "415556")
	if err != nil {
		t.Fatalf("LookupNumber (matching prefix): %v", err)
	}
	if !found {
		t.Fatalf("number not found")
	}
	if !result.SpoofSuspected {
		t.Errorf("SpoofSuspected = false, want true for matching prefix")
	}

	result, found, err = LookupNumber(ctx, sqlDB, number, "")
	if err != nil {
		t.Fatalf("LookupNumber (no prefix): %v", err)
	}
	if !found {
		t.Fatalf("number not found")
	}
	if result.SpoofSuspected {
		t.Errorf("SpoofSuspected = true, want false with no prefix")
	}

	result, found, err = LookupNumber(ctx, sqlDB, number, "999999")
	if err != nil {
		t.Fatalf("LookupNumber (non-matching prefix): %v", err)
	}
	if !found {
		t.Fatalf("number not found")
	}
	if result.SpoofSuspected {
		t.Errorf("SpoofSuspected = true, want false for non-matching prefix")
	}
}
