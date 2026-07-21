package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrDeviceNotFound is returned by GetDeviceByKeyID when no device row matches
// the given key_id.
var ErrDeviceNotFound = errors.New("store: device not found")

// GetDeviceByKeyID looks up a device by its App Attest key_id, returning its
// device_id, stored PKIX-DER public key, and last-recorded signature counter.
// It returns ErrDeviceNotFound when no such device exists.
func GetDeviceByKeyID(ctx context.Context, exec Execer, keyID string) (deviceID uint64, publicKeyDER []byte, signCount uint32, err error) {
	const query = `SELECT device_id, public_key, sign_count FROM devices WHERE key_id = ?`
	err = exec.QueryRowContext(ctx, query, keyID).Scan(&deviceID, &publicKeyDER, &signCount)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, 0, ErrDeviceNotFound
	}
	if err != nil {
		return 0, nil, 0, err
	}
	return deviceID, publicKeyDER, signCount, nil
}

// UpdateDeviceSignCount records a device's latest App Attest signature counter
// and touches its last_seen_at.
func UpdateDeviceSignCount(ctx context.Context, exec Execer, deviceID uint64, signCount uint32, now time.Time) error {
	const query = `UPDATE devices SET sign_count = ?, last_seen_at = ? WHERE device_id = ?`
	_, err := exec.ExecContext(ctx, query, signCount, now.UTC(), deviceID)
	return err
}
