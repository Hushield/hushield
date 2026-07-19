package store

import (
	"context"
	"time"
)

// EnsureSeedDevice upserts the synthetic per-source device used to attribute
// imported public-data reports (see internal/seed), keyed by
// "seed:"+sourceName with a fixed non-null public_key marker. Re-running
// with a different trustWeight updates the existing device's trust_weight
// in place rather than creating a duplicate. It returns the device's
// device_id either way.
func EnsureSeedDevice(ctx context.Context, exec Execer, sourceName string, trustWeight float64, now time.Time) (uint64, error) {
	const query = `INSERT INTO devices (key_id, public_key, trust_weight, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE trust_weight = VALUES(trust_weight), last_seen_at = VALUES(last_seen_at), device_id = LAST_INSERT_ID(device_id)`

	keyID := "seed:" + sourceName
	res, err := exec.ExecContext(ctx, query, keyID, []byte("seed"), trustWeight, now.UTC(), now.UTC())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return uint64(id), nil
}

// UpsertSeedNumber inserts a phone_numbers row for number tagged with the
// given source if one does not already exist, or just touches
// last_reported_at if it does. An existing row's source is never clobbered,
// so a number first reported by the community keeps source='community' even
// if a later seed import also covers it. It returns the row's
// phone_number_id either way.
func UpsertSeedNumber(ctx context.Context, exec Execer, number, source string, now time.Time) (uint64, error) {
	const query = `INSERT INTO phone_numbers (number, source, first_reported_at, last_reported_at)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE last_reported_at = VALUES(last_reported_at), phone_number_id = LAST_INSERT_ID(phone_number_id)`

	res, err := exec.ExecContext(ctx, query, number, source, now.UTC(), now.UTC())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return uint64(id), nil
}
