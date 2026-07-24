package attest

import (
	"context"
	"errors"
	"log"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// silentRedisLogger implements the go-redis internal.Logging interface as a
// no-op, so a deliberately-triggered connection failure (in the Issue/Consume
// error tests below) doesn't spam stray "redis: ... connection refused"
// lines into otherwise-pristine test output.
type silentRedisLogger struct{}

func (silentRedisLogger) Printf(ctx context.Context, format string, v ...any) {}

// defaultRedisLogger mirrors go-redis's own default logger (unexported in
// the library as internal.NewDefaultLogger), so tests can restore normal
// logging behavior after silencing it.
type defaultRedisLogger struct{ log *log.Logger }

func (l defaultRedisLogger) Printf(ctx context.Context, format string, v ...any) {
	l.log.Printf(format, v...)
}

// silenceRedisLogsForUnreachableServer suppresses go-redis's own internal
// logging for the duration of the calling test, restoring it on cleanup. Use
// only in tests that deliberately point a client at an unreachable server,
// where go-redis's internal dial-retry logging would otherwise spam pristine
// test output despite the resulting error being correctly handled.
func silenceRedisLogsForUnreachableServer(t *testing.T) {
	t.Helper()
	redis.SetLogger(silentRedisLogger{})
	t.Cleanup(func() {
		redis.SetLogger(defaultRedisLogger{log: log.New(os.Stderr, "redis: ", log.LstdFlags|log.Lshortfile)})
	})
}

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

// TestRedisChallengeStore_IssueError confirms Issue wraps and returns the
// underlying Redis error (rather than panicking or silently succeeding) when
// the Redis server is unreachable.
func TestRedisChallengeStore_IssueError(t *testing.T) {
	silenceRedisLogsForUnreachableServer(t)
	store, mr := newTestRedisStore(t)
	mr.Close() // server now unreachable

	if _, err := store.Issue(time.Unix(1_700_000_000, 0), 5*time.Minute); err == nil {
		t.Fatal("Issue: want error when redis is unreachable, got nil")
	}
}

// TestRedisChallengeStore_ConsumeError confirms Consume wraps and returns the
// underlying Redis error (distinct from ErrChallengeInvalid) when the Redis
// server is unreachable.
func TestRedisChallengeStore_ConsumeError(t *testing.T) {
	silenceRedisLogsForUnreachableServer(t)
	store, mr := newTestRedisStore(t)
	now := time.Unix(1_700_000_000, 0)
	ch, err := store.Issue(now, 5*time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	mr.Close() // server now unreachable

	err = store.Consume(ch, now)
	if err == nil {
		t.Fatal("Consume: want error when redis is unreachable, got nil")
	}
	if errors.Is(err, ErrChallengeInvalid) {
		t.Errorf("Consume err = %v, want a transport error distinct from ErrChallengeInvalid", err)
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
