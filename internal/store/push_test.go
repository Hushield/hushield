package store

import (
	"context"
	"testing"
	"time"

	"spamfilter/internal/dbtest"
)

func TestUpsertPushToken(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	deviceID := insertDevice(t, sqlDB, "push-key-1", 1.00)
	now := time.Now()

	if err := UpsertPushToken(ctx, sqlDB, deviceID, "abc123", "production", now); err != nil {
		t.Fatalf("UpsertPushToken: %v", err)
	}

	var token, env string
	var updatedAt time.Time
	if err := sqlDB.QueryRow(
		"SELECT push_token, push_environment, push_updated_at FROM devices WHERE device_id = ?",
		deviceID,
	).Scan(&token, &env, &updatedAt); err != nil {
		t.Fatalf("select push columns: %v", err)
	}
	if token != "abc123" {
		t.Errorf("push_token = %q, want abc123", token)
	}
	if env != "production" {
		t.Errorf("push_environment = %q, want production", env)
	}
	if diff := updatedAt.Sub(now.UTC()); diff < -time.Second || diff > time.Second {
		t.Errorf("push_updated_at not set to now: diff=%v", diff)
	}

	// A second call replaces the prior token/environment.
	if err := UpsertPushToken(ctx, sqlDB, deviceID, "def456", "sandbox", now); err != nil {
		t.Fatalf("UpsertPushToken (replace): %v", err)
	}
	if err := sqlDB.QueryRow(
		"SELECT push_token, push_environment FROM devices WHERE device_id = ?",
		deviceID,
	).Scan(&token, &env); err != nil {
		t.Fatalf("select push columns after replace: %v", err)
	}
	if token != "def456" || env != "sandbox" {
		t.Errorf("after replace got token=%q env=%q, want def456/sandbox", token, env)
	}
}

func TestListPushTargets(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	withToken := insertDevice(t, sqlDB, "push-key-with", 1.00)
	insertDevice(t, sqlDB, "push-key-without", 1.00) // no token registered

	if err := UpsertPushToken(ctx, sqlDB, withToken, "tok-with", "production", now); err != nil {
		t.Fatalf("UpsertPushToken: %v", err)
	}

	targets, err := ListPushTargets(ctx, sqlDB)
	if err != nil {
		t.Fatalf("ListPushTargets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("len(targets) = %d, want 1 (only devices with a token)", len(targets))
	}
	got := targets[0]
	if got.DeviceID != withToken || got.Token != "tok-with" || got.Environment != "production" {
		t.Errorf("target = %+v, want device %d/tok-with/production", got, withToken)
	}
}
