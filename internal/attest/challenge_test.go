package attest

import (
	"errors"
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
