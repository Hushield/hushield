package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"spamfilter/internal/dbtest"
)

func TestGetDeviceByKeyID(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	wantID := insertDevice(t, sqlDB, "assert-key-1", 1.00)

	gotID, pubDER, signCount, _, err := GetDeviceByKeyID(ctx, sqlDB, "assert-key-1")
	if err != nil {
		t.Fatalf("GetDeviceByKeyID: %v", err)
	}
	if gotID != wantID {
		t.Errorf("deviceID = %d, want %d", gotID, wantID)
	}
	if string(pubDER) != "pubkey-assert-key-1" {
		t.Errorf("publicKeyDER = %q, want %q", pubDER, "pubkey-assert-key-1")
	}
	if signCount != 0 {
		t.Errorf("signCount = %d, want 0 (schema default)", signCount)
	}
}

func TestGetDeviceByKeyID_NotFound(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	_, _, _, _, err := GetDeviceByKeyID(ctx, sqlDB, "no-such-key")
	if !errors.Is(err, ErrDeviceNotFound) {
		t.Errorf("err = %v, want ErrDeviceNotFound", err)
	}
}

func TestGetDeviceByKeyID_returnsPlatform(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	_, err := UpsertDevicePlatform(ctx, sqlDB, "android-key-1", []byte("pubkey-android-key-1"), nil, "android", time.Now())
	if err != nil {
		t.Fatalf("UpsertDevicePlatform: %v", err)
	}

	_, _, _, platform, err := GetDeviceByKeyID(ctx, sqlDB, "android-key-1")
	if err != nil {
		t.Fatalf("GetDeviceByKeyID: %v", err)
	}
	if platform != "android" {
		t.Errorf("platform = %q, want %q", platform, "android")
	}
}

func TestUpsertDevicePlatform_existingRowKeepsItsPlatform(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	id1, err := UpsertDevicePlatform(ctx, sqlDB, "apple-key-1", []byte("pubkey-v1"), nil, "apple", now)
	if err != nil {
		t.Fatalf("first UpsertDevicePlatform: %v", err)
	}

	// Re-enrolling the same key_id must not let a re-attestation silently
	// change its recorded platform.
	id2, err := UpsertDevicePlatform(ctx, sqlDB, "apple-key-1", []byte("pubkey-v2"), nil, "apple", now)
	if err != nil {
		t.Fatalf("second UpsertDevicePlatform: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("re-enrolling key_id created a new device: %d != %d", id1, id2)
	}

	_, pubDER, _, platform, err := GetDeviceByKeyID(ctx, sqlDB, "apple-key-1")
	if err != nil {
		t.Fatalf("GetDeviceByKeyID: %v", err)
	}
	if platform != "apple" {
		t.Errorf("platform = %q, want %q", platform, "apple")
	}
	if string(pubDER) != "pubkey-v2" {
		t.Errorf("public key was not updated on re-enroll: got %q", pubDER)
	}
}

func TestUpdateDeviceSignCount(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	deviceID := insertDevice(t, sqlDB, "assert-key-2", 1.00)
	now := time.Now()

	if err := UpdateDeviceSignCount(ctx, sqlDB, deviceID, 9, now); err != nil {
		t.Fatalf("UpdateDeviceSignCount: %v", err)
	}

	_, _, signCount, _, err := GetDeviceByKeyID(ctx, sqlDB, "assert-key-2")
	if err != nil {
		t.Fatalf("GetDeviceByKeyID: %v", err)
	}
	if signCount != 9 {
		t.Errorf("signCount = %d, want 9", signCount)
	}

	var lastSeenAt time.Time
	if err := sqlDB.QueryRow("SELECT last_seen_at FROM devices WHERE device_id = ?", deviceID).Scan(&lastSeenAt); err != nil {
		t.Fatalf("select last_seen_at: %v", err)
	}
	if diff := lastSeenAt.Sub(now.UTC()); diff < -time.Second || diff > time.Second {
		t.Errorf("last_seen_at not updated to now: diff=%v", diff)
	}
}
