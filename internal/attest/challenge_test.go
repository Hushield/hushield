package attest

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestMemoryChallengeStore_IssueConsumeOnce(t *testing.T) {
	store := NewMemoryChallengeStore()
	now := time.Unix(1_700_000_000, 0)

	ch, err := store.Issue(now, 5*time.Minute)
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}
	if len(ch) != 32 {
		t.Fatalf("challenge length = %d, want 32", len(ch))
	}

	if err := store.Consume(ch, now.Add(time.Minute)); err != nil {
		t.Fatalf("first Consume returned error: %v", err)
	}

	// Second consume of the same challenge must fail (replay-safe).
	if err := store.Consume(ch, now.Add(time.Minute)); !errors.Is(err, ErrChallengeInvalid) {
		t.Errorf("second Consume err = %v, want ErrChallengeInvalid", err)
	}
}

func TestMemoryChallengeStore_ConsumeExpired(t *testing.T) {
	store := NewMemoryChallengeStore()
	now := time.Unix(1_700_000_000, 0)

	ch, err := store.Issue(now, 5*time.Minute)
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	if err := store.Consume(ch, now.Add(10*time.Minute)); !errors.Is(err, ErrChallengeInvalid) {
		t.Errorf("expired Consume err = %v, want ErrChallengeInvalid", err)
	}
}

func TestMemoryChallengeStore_ConsumeUnknown(t *testing.T) {
	store := NewMemoryChallengeStore()
	now := time.Unix(1_700_000_000, 0)

	if err := store.Consume([]byte("never-issued-this-one-nope-32byte"), now); !errors.Is(err, ErrChallengeInvalid) {
		t.Errorf("unknown Consume err = %v, want ErrChallengeInvalid", err)
	}
}

func TestMemoryChallengeStore_IssuePurgesExpired(t *testing.T) {
	store := NewMemoryChallengeStore()
	now := time.Unix(1_700_000_000, 0)

	// Issue a challenge that is never consumed.
	stale, err := store.Issue(now, time.Minute)
	if err != nil {
		t.Fatalf("Issue (stale) returned error: %v", err)
	}

	// A later Issue, past the stale challenge's expiry, must sweep it out.
	later := now.Add(2 * time.Minute)
	if _, err := store.Issue(later, time.Minute); err != nil {
		t.Fatalf("Issue (later) returned error: %v", err)
	}

	store.mu.Lock()
	_, stalePresent := store.challenges[string(stale)]
	size := len(store.challenges)
	store.mu.Unlock()

	if stalePresent {
		t.Error("expired challenge was not purged on a later Issue")
	}
	if size != 1 {
		t.Errorf("store size = %d, want 1 (only the live challenge remains)", size)
	}
}

func TestMemoryChallengeStore_IssueAtCapacity(t *testing.T) {
	store := NewMemoryChallengeStore()
	now := time.Unix(1_700_000_000, 0)
	future := now.Add(time.Hour)

	// Fill the store to capacity with live (unexpired) entries directly, so
	// the lazy sweep purges nothing and Issue must fail closed.
	for i := 0; i < maxChallenges; i++ {
		store.challenges[strconv.Itoa(i)] = future
	}

	if _, err := store.Issue(now, time.Minute); !errors.Is(err, ErrChallengeStoreFull) {
		t.Errorf("Issue at capacity err = %v, want ErrChallengeStoreFull", err)
	}

	// Once an entry expires, a later Issue sweeps it and succeeds again.
	store.challenges["0"] = now.Add(-time.Second)
	if _, err := store.Issue(now, time.Minute); err != nil {
		t.Errorf("Issue after an entry expired returned error: %v", err)
	}
}

func TestMemoryChallengeStore_IssueUnique(t *testing.T) {
	store := NewMemoryChallengeStore()
	now := time.Unix(1_700_000_000, 0)

	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		ch, err := store.Issue(now, time.Minute)
		if err != nil {
			t.Fatalf("Issue returned error: %v", err)
		}
		if seen[string(ch)] {
			t.Fatalf("duplicate challenge issued at i=%d", i)
		}
		seen[string(ch)] = true
	}
}
