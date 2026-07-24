package attest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisChallengeKeyPrefix namespaces challenge keys so the store can share a
// Redis instance with other data.
const redisChallengeKeyPrefix = "attest:challenge:"

// RedisChallengeStore is a Redis-backed ChallengeStore. Unlike
// MemoryChallengeStore it is not process-local: a challenge issued by one
// server instance can be consumed by another, which is required for a
// multi-instance deployment.
//
// Expiry is enforced by Redis's own key TTL rather than by comparing the now
// argument to a stored expiry, so Consume's now parameter is accepted only to
// satisfy the ChallengeStore interface and is otherwise unused.
type RedisChallengeStore struct {
	client redis.Cmdable
}

// NewRedisChallengeStore returns a ChallengeStore backed by client.
func NewRedisChallengeStore(client redis.Cmdable) *RedisChallengeStore {
	return &RedisChallengeStore{client: client}
}

// Issue generates 32 random bytes and stores them under a namespaced key
// with a TTL of ttl.
func (s *RedisChallengeStore) Issue(now time.Time, ttl time.Duration) ([]byte, error) {
	ch := make([]byte, 32)
	if _, err := rand.Read(ch); err != nil {
		return nil, err
	}

	if err := s.client.Set(context.Background(), redisChallengeKey(ch), "1", ttl).Err(); err != nil {
		return nil, fmt.Errorf("attest: redis challenge store issue: %w", err)
	}

	return ch, nil
}

// Consume atomically deletes and returns the challenge key via GETDEL, so a
// challenge can be consumed exactly once cluster-wide. An unknown, expired,
// or already-consumed challenge returns ErrChallengeInvalid.
func (s *RedisChallengeStore) Consume(challenge []byte, now time.Time) error {
	_, err := s.client.GetDel(context.Background(), redisChallengeKey(challenge)).Result()
	if errors.Is(err, redis.Nil) {
		return ErrChallengeInvalid
	}
	if err != nil {
		return fmt.Errorf("attest: redis challenge store consume: %w", err)
	}

	return nil
}

// redisChallengeKey builds the namespaced Redis key for a challenge.
func redisChallengeKey(challenge []byte) string {
	return redisChallengeKeyPrefix + hex.EncodeToString(challenge)
}
