package attest

import (
	"crypto/rand"
	"errors"
	"sync"
	"time"
)

// ErrChallengeInvalid is returned by ChallengeStore.Consume when a challenge
// is unknown, already consumed, or expired.
var ErrChallengeInvalid = errors.New("attest: challenge invalid")

// ErrChallengeStoreFull is returned by Issue when the store is at capacity
// even after purging expired entries. Failing closed here bounds memory use on
// the unauthenticated challenge-issuing route.
var ErrChallengeStoreFull = errors.New("attest: challenge store at capacity")

// maxChallenges caps the number of live challenges the in-memory store will
// hold, bounding growth from an unauthenticated caller that requests
// challenges it never consumes.
const maxChallenges = 100_000

// ChallengeStore issues single-use attestation challenges and consumes them
// exactly once.
type ChallengeStore interface {
	// Issue generates a fresh 32-byte random challenge valid for ttl.
	Issue(now time.Time, ttl time.Duration) (challenge []byte, err error)
	// Consume returns nil exactly once for a valid, unexpired challenge and
	// deletes it; otherwise it returns ErrChallengeInvalid.
	Consume(challenge []byte, now time.Time) error
}

// MemoryChallengeStore is an in-memory, mutex-guarded ChallengeStore.
//
// NOTE: this store is process-local. A multi-instance deployment MUST replace
// it with a shared store (e.g. Redis) so a challenge issued by one instance
// can be consumed by another and replay protection holds cluster-wide. It is
// sufficient for a single-instance v1 deployment.
type MemoryChallengeStore struct {
	mu         sync.Mutex
	challenges map[string]time.Time // challenge -> expiry
}

// NewMemoryChallengeStore returns an empty in-memory challenge store.
func NewMemoryChallengeStore() *MemoryChallengeStore {
	return &MemoryChallengeStore{challenges: make(map[string]time.Time)}
}

// Issue generates 32 random bytes and stores them with the given expiry.
// Before inserting it opportunistically purges any expired-but-unconsumed
// entries (a lazy sweep, no background goroutine) and, if still at capacity,
// fails closed with ErrChallengeStoreFull rather than growing unbounded.
func (s *MemoryChallengeStore) Issue(now time.Time, ttl time.Duration) ([]byte, error) {
	ch := make([]byte, 32)
	if _, err := rand.Read(ch); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Lazy sweep: drop entries whose expiry has passed.
	for key, expiry := range s.challenges {
		if !now.Before(expiry) {
			delete(s.challenges, key)
		}
	}

	if len(s.challenges) >= maxChallenges {
		return nil, ErrChallengeStoreFull
	}

	s.challenges[string(ch)] = now.Add(ttl)

	return ch, nil
}

// Consume validates and deletes a challenge, returning ErrChallengeInvalid on
// any failure (unknown, expired, or already consumed).
func (s *MemoryChallengeStore) Consume(challenge []byte, now time.Time) error {
	key := string(challenge)

	s.mu.Lock()
	defer s.mu.Unlock()

	expiry, ok := s.challenges[key]
	if !ok {
		return ErrChallengeInvalid
	}

	// Delete regardless of expiry so an expired challenge cannot be retried.
	delete(s.challenges, key)

	if !now.Before(expiry) {
		return ErrChallengeInvalid
	}

	return nil
}
