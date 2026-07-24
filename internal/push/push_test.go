package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
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

// TestNewNotifier_WithValidCredsIsReal confirms NewNotifier returns a real
// (non-noop) APNsNotifier and real=true when a valid key path is supplied.
func TestNewNotifier_WithValidCredsIsReal(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	path := filepath.Join(t.TempDir(), "AuthKey_TEST.p8")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write p8: %v", err)
	}

	n, real, err := NewNotifier(path, "KEYID", "TEAMID", "com.example.app")
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	if !real {
		t.Errorf("real = false, want true when creds are present and valid")
	}
	if _, ok := n.(*APNsNotifier); !ok {
		t.Errorf("notifier is %T, want *APNsNotifier", n)
	}
}

// TestNewNotifier_BadCredsPropagatesError confirms a present-but-invalid key
// path propagates the underlying error rather than silently degrading to a
// Noop notifier.
func TestNewNotifier_BadCredsPropagatesError(t *testing.T) {
	n, real, err := NewNotifier(filepath.Join(t.TempDir(), "does-not-exist.p8"), "KEYID", "TEAMID", "com.example.app")
	if err == nil {
		t.Fatal("NewNotifier: want error for missing key file, got nil")
	}
	if real {
		t.Errorf("real = true, want false on error")
	}
	if n != nil {
		t.Errorf("notifier = %v, want nil on error", n)
	}
}
