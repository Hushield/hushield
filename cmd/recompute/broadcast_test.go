package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"spamfilter/internal/config"
	"spamfilter/internal/dbtest"
	"spamfilter/internal/push"
	"spamfilter/internal/store"
)

// The broadcast path inside runCycle -- ListPushTargets followed by
// BroadcastRefresh -- only executes when NewNotifier reports a REAL APNs
// notifier, which needs an Apple .p8 auth key. Injecting the factory makes it
// reachable with a fake, so the path that pings every enrolled device after a
// recompute is actually exercised.

// withNotifier swaps the package-level factory for the duration of a test.
func withNotifier(t *testing.T, n push.Notifier, real bool, err error) {
	t.Helper()
	prev := newNotifier
	newNotifier = func(_, _, _, _ string) (push.Notifier, bool, error) {
		return n, real, err
	}
	t.Cleanup(func() { newNotifier = prev })
}

// seedDeviceWithPushToken creates a device and registers an APNs token for it,
// so ListPushTargets has something to return. Reuses insertRunCycleDevice from
// runcycle_test.go rather than duplicating the insert.
func seedDeviceWithPushToken(t *testing.T, sqlDB *sql.DB, keyID, token string) uint64 {
	t.Helper()
	deviceID := insertRunCycleDevice(t, sqlDB, keyID, 1.0)
	if err := store.UpsertPushToken(context.Background(), sqlDB, deviceID, token, "production", time.Now()); err != nil {
		t.Fatalf("UpsertPushToken(%s): %v", keyID, err)
	}
	return deviceID
}

func TestRunCycle_BroadcastsToEveryRegisteredDevice(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)

	seedDeviceWithPushToken(t, sqlDB, "push-key-1", "token-aaa")
	seedDeviceWithPushToken(t, sqlDB, "push-key-2", "token-bbb")
	seedDeviceWithPushToken(t, sqlDB, "push-key-3", "token-ccc")

	mock := &push.MockNotifier{}
	withNotifier(t, mock, true, nil)

	cfg := config.Config{AttestMode: "mock", APNSKeyPath: "/injected/ignored.p8"}
	if err := runCycle(context.Background(), sqlDB, cfg, true); err != nil {
		t.Fatalf("runCycle: %v", err)
	}

	calls := mock.Calls()
	if len(calls) != 3 {
		t.Fatalf("notified %d devices, want 3 -- every device with a registered token should be pinged", len(calls))
	}

	got := map[string]bool{}
	for _, c := range calls {
		got[c.Token] = true
		if c.Environment != "production" {
			t.Errorf("target %s has environment %q, want production", c.Token, c.Environment)
		}
	}
	for _, want := range []string{"token-aaa", "token-bbb", "token-ccc"} {
		if !got[want] {
			t.Errorf("device with token %q was never notified", want)
		}
	}
}

// A device that has never registered a token must not be notified -- and must
// not cause an error either.
func TestRunCycle_SkipsDevicesWithoutTokens(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	// One device with a token, one without.
	seedDeviceWithPushToken(t, sqlDB, "has-token", "token-yes")
	insertRunCycleDevice(t, sqlDB, "no-token", 1.0)

	mock := &push.MockNotifier{}
	withNotifier(t, mock, true, nil)

	if err := runCycle(ctx, sqlDB, config.Config{AttestMode: "mock"}, true); err != nil {
		t.Fatalf("runCycle: %v", err)
	}

	calls := mock.Calls()
	if len(calls) != 1 {
		t.Fatalf("notified %d devices, want 1", len(calls))
	}
	if calls[0].Token != "token-yes" {
		t.Errorf("notified %q, want token-yes", calls[0].Token)
	}
}

// A push failure must NOT fail the recompute. The scores are already committed
// by this point; losing the whole cycle because Apple returned an error for one
// device would be strictly worse than a stale client.
func TestRunCycle_PushFailuresDoNotFailTheCycle(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)

	seedDeviceWithPushToken(t, sqlDB, "fail-1", "token-fail-1")
	seedDeviceWithPushToken(t, sqlDB, "fail-2", "token-fail-2")

	mock := &push.MockNotifier{Err: errors.New("apns: BadDeviceToken")}
	withNotifier(t, mock, true, nil)

	if err := runCycle(context.Background(), sqlDB, config.Config{AttestMode: "mock"}, true); err != nil {
		t.Fatalf("a push failure must not fail the recompute, got: %v", err)
	}
	if n := len(mock.Calls()); n != 2 {
		t.Errorf("attempted %d sends, want 2 -- one failure must not abort the batch", n)
	}
}

// A mixed batch: some sends succeed, some fail. Every target must still be
// attempted rather than the loop bailing at the first error.
func TestRunCycle_MixedBatchAttemptsEveryTarget(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)

	d1 := seedDeviceWithPushToken(t, sqlDB, "mix-1", "token-mix-1")
	seedDeviceWithPushToken(t, sqlDB, "mix-2", "token-mix-2")
	d3 := seedDeviceWithPushToken(t, sqlDB, "mix-3", "token-mix-3")

	mock := &push.MockNotifier{FailFor: map[uint64]bool{d1: true, d3: true}}
	withNotifier(t, mock, true, nil)

	if err := runCycle(context.Background(), sqlDB, config.Config{AttestMode: "mock"}, true); err != nil {
		t.Fatalf("runCycle: %v", err)
	}
	if n := len(mock.Calls()); n != 3 {
		t.Errorf("attempted %d sends, want all 3 attempted despite 2 failures", n)
	}
}

// realAPNs=false is the production reality today (no .p8 configured): the
// notifier is built, reports itself as a no-op, and the broadcast is skipped
// entirely -- no targets are even queried.
func TestRunCycle_NoRealAPNsSkipsBroadcastEntirely(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	seedDeviceWithPushToken(t, sqlDB, "noop-1", "token-noop")

	mock := &push.MockNotifier{}
	withNotifier(t, mock, false, nil) // realAPNs = false

	if err := runCycle(context.Background(), sqlDB, config.Config{AttestMode: "mock"}, true); err != nil {
		t.Fatalf("runCycle: %v", err)
	}
	if n := len(mock.Calls()); n != 0 {
		t.Errorf("made %d sends, want 0 -- realAPNs=false must skip the broadcast", n)
	}
}

// A factory error must abort the cycle with a message identifying the notifier,
// so a push-only misconfiguration is not mistaken for a recompute failure.
func TestRunCycle_NotifierFactoryErrorIsIdentified(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	withNotifier(t, nil, false, errors.New("bad .p8: not a PKCS8 EC key"))

	err := runCycle(context.Background(), sqlDB, config.Config{AttestMode: "mock"}, true)
	if err == nil {
		t.Fatal("expected an error when the notifier cannot be built")
	}
	if !strings.Contains(err.Error(), "push notifier") {
		t.Errorf("error = %q, want it to identify the push notifier", err)
	}
}
