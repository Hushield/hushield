package attest

import (
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestRedisStore spins up an in-process miniredis server and returns a
// RedisChallengeStore backed by a real redis.Client pointed at it, along with
// the miniredis handle so tests can manipulate its clock (FastForward).
func newTestRedisStore(t *testing.T) (*RedisChallengeStore, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })

	return NewRedisChallengeStore(client), mr
}

func TestRedisChallengeStore_IssueConsumeOnce(t *testing.T) {
	store, _ := newTestRedisStore(t)
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

func TestRedisChallengeStore_ConsumeUnknown(t *testing.T) {
	store, _ := newTestRedisStore(t)
	now := time.Unix(1_700_000_000, 0)

	if err := store.Consume([]byte("never-issued-this-one-nope-32byte"), now); !errors.Is(err, ErrChallengeInvalid) {
		t.Errorf("unknown Consume err = %v, want ErrChallengeInvalid", err)
	}
}

func TestRedisChallengeStore_ConsumeExpired(t *testing.T) {
	store, mr := newTestRedisStore(t)
	now := time.Unix(1_700_000_000, 0)

	ch, err := store.Issue(now, 5*time.Minute)
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	mr.FastForward(10 * time.Minute)

	if err := store.Consume(ch, now.Add(10*time.Minute)); !errors.Is(err, ErrChallengeInvalid) {
		t.Errorf("expired Consume err = %v, want ErrChallengeInvalid", err)
	}
}

func TestRedisChallengeStore_IssueUnique(t *testing.T) {
	store, _ := newTestRedisStore(t)
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

func TestRedisChallengeStore_DistinctChallengesIndependentlyConsumable(t *testing.T) {
	store, _ := newTestRedisStore(t)
	now := time.Unix(1_700_000_000, 0)

	ch1, err := store.Issue(now, 5*time.Minute)
	if err != nil {
		t.Fatalf("Issue ch1 returned error: %v", err)
	}
	ch2, err := store.Issue(now, 5*time.Minute)
	if err != nil {
		t.Fatalf("Issue ch2 returned error: %v", err)
	}

	if err := store.Consume(ch1, now); err != nil {
		t.Fatalf("Consume ch1 returned error: %v", err)
	}
	if err := store.Consume(ch2, now); err != nil {
		t.Fatalf("Consume ch2 returned error: %v", err)
	}
}
