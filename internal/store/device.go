package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"spamfilter/internal/trust"
)

// ErrDeviceNotFound is returned by GetDeviceByKeyID when no device row matches
// the given key_id.
var ErrDeviceNotFound = errors.New("store: device not found")

// GetDeviceByKeyID looks up a device by its attestation key_id, returning its
// device_id, stored PKIX-DER public key, last-recorded signature counter,
// and enrolled platform ("apple" or "android"). It returns ErrDeviceNotFound
// when no such device exists.
func GetDeviceByKeyID(ctx context.Context, exec Execer, keyID string) (deviceID uint64, publicKeyDER []byte, signCount uint32, platform string, err error) {
	const query = `SELECT device_id, public_key, sign_count, platform FROM devices WHERE key_id = ?`
	err = exec.QueryRowContext(ctx, query, keyID).Scan(&deviceID, &publicKeyDER, &signCount, &platform)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, 0, "", ErrDeviceNotFound
	}
	if err != nil {
		return 0, nil, 0, "", err
	}
	return deviceID, publicKeyDER, signCount, platform, nil
}

// UpsertDevicePlatform inserts or updates the device row keyed by key_id and
// returns its device_id. New rows get trust.TrustBase, matching upsertDevice's
// existing enrolment behavior. platform is only ever written on INSERT: a
// re-attestation of an existing key_id must not let a client silently change
// its recorded platform.
func UpsertDevicePlatform(ctx context.Context, exec Execer, keyID string, publicKey, receipt []byte, platform string, now time.Time) (uint64, error) {
	const upsert = `INSERT INTO devices (key_id, platform, public_key, receipt, last_seen_at, trust_weight)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    public_key = VALUES(public_key),
    receipt = VALUES(receipt),
    last_seen_at = VALUES(last_seen_at)`
	if _, err := exec.ExecContext(ctx, upsert, keyID, platform, publicKey, receipt, now.UTC(), trust.TrustBase); err != nil {
		return 0, err
	}

	var deviceID uint64
	if err := exec.QueryRowContext(ctx, "SELECT device_id FROM devices WHERE key_id = ?", keyID).Scan(&deviceID); err != nil {
		return 0, err
	}
	return deviceID, nil
}

// UpdateDeviceSignCount records a device's latest App Attest signature counter
// and touches its last_seen_at.
func UpdateDeviceSignCount(ctx context.Context, exec Execer, deviceID uint64, signCount uint32, now time.Time) error {
	const query = `UPDATE devices SET sign_count = ?, last_seen_at = ? WHERE device_id = ?`
	_, err := exec.ExecContext(ctx, query, signCount, now.UTC(), deviceID)
	return err
}
