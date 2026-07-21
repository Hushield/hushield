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

	gotID, pubDER, signCount, err := GetDeviceByKeyID(ctx, sqlDB, "assert-key-1")
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

	_, _, _, err := GetDeviceByKeyID(ctx, sqlDB, "no-such-key")
	if !errors.Is(err, ErrDeviceNotFound) {
		t.Errorf("err = %v, want ErrDeviceNotFound", err)
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

	_, _, signCount, err := GetDeviceByKeyID(ctx, sqlDB, "assert-key-2")
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
