package push

import (
	"context"
	"errors"
	"testing"

	"spamfilter/internal/store"
)

func TestBroadcastRefresh_TalliesAndContinues(t *testing.T) {
	ctx := context.Background()
	targets := []store.PushTarget{
		{DeviceID: 1, Token: "t1", Environment: "production"},
		{DeviceID: 2, Token: "t2", Environment: "sandbox"},
		{DeviceID: 3, Token: "t3", Environment: "production"},
	}

	// A notifier that always fails: every target should be counted as failed
	// and the batch must run to completion (all three recorded).
	failing := &MockNotifier{Err: errors.New("boom")}
	sent, failed := BroadcastRefresh(ctx, failing, targets)
	if sent != 0 || failed != 3 {
		t.Errorf("failing broadcast: sent=%d failed=%d, want 0/3", sent, failed)
	}
	if len(failing.Calls()) != 3 {
		t.Errorf("failing broadcast recorded %d calls, want 3 (must not abort)", len(failing.Calls()))
	}

	// A notifier that always succeeds.
	ok := &MockNotifier{}
	sent, failed = BroadcastRefresh(ctx, ok, targets)
	if sent != 3 || failed != 0 {
		t.Errorf("ok broadcast: sent=%d failed=%d, want 3/0", sent, failed)
	}
	if got := ok.Calls(); len(got) != 3 || got[0].DeviceID != 1 || got[2].DeviceID != 3 {
		t.Errorf("ok broadcast recorded %+v, want the three targets in order", got)
	}

	// A MIXED batch: device 2 fails, devices 1 and 3 succeed. The tallies must
	// be exact (sent=2, failed=1) and -- critically -- ALL three targets must
	// have been attempted, proving a mid-batch failure does not abort the run.
	mixed := &MockNotifier{FailFor: map[uint64]bool{2: true}}
	sent, failed = BroadcastRefresh(ctx, mixed, targets)
	if sent != 2 || failed != 1 {
		t.Errorf("mixed broadcast: sent=%d failed=%d, want 2/1", sent, failed)
	}
	got := mixed.Calls()
	if len(got) != 3 || got[0].DeviceID != 1 || got[1].DeviceID != 2 || got[2].DeviceID != 3 {
		t.Errorf("mixed broadcast recorded %+v, want all three targets in order (not aborted at the failure)", got)
	}
}

func TestNoopNotifier_ReturnsNil(t *testing.T) {
	if err := (NoopNotifier{}).SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 1, Token: "t"}); err != nil {
		t.Errorf("NoopNotifier.SendSilentRefresh = %v, want nil", err)
	}
}

func TestNewNotifier_NoCredsIsNoop(t *testing.T) {
	n, real, err := NewNotifier("", "", "", "")
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	if real {
		t.Errorf("real = true, want false when APNS_KEY_PATH empty")
	}
	if _, ok := n.(NoopNotifier); !ok {
		t.Errorf("notifier is %T, want NoopNotifier when creds absent", n)
	}
}
