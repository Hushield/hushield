package store

import (
	"context"
	"time"
)

// PushTarget identifies one device that can receive a silent APNs push: its
// device_id, its registered APNs device token, and the APNs environment
// (sandbox or production) selecting which Apple host to send through.
type PushTarget struct {
	DeviceID    uint64
	Token       string
	Environment string
}

// UpsertPushToken records (or replaces) a device's APNs push token and
// environment, stamping push_updated_at. It is parameterized and scoped to a
// single device row.
func UpsertPushToken(ctx context.Context, exec Execer, deviceID uint64, token, environment string, now time.Time) error {
	const query = `UPDATE devices SET push_token = ?, push_environment = ?, push_updated_at = ? WHERE device_id = ?`
	_, err := exec.ExecContext(ctx, query, token, environment, now.UTC(), deviceID)
	return err
}

// ListPushTargets returns every device that has registered a push token. It
// is the broadcast fan-out list for the recompute job's silent-refresh push.
func ListPushTargets(ctx context.Context, db Execer) ([]PushTarget, error) {
	const query = `SELECT devices.device_id, devices.push_token, devices.push_environment FROM devices WHERE devices.push_token IS NOT NULL`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []PushTarget
	for rows.Next() {
		var t PushTarget
		if err := rows.Scan(&t.DeviceID, &t.Token, &t.Environment); err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return targets, nil
}
