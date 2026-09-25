package store

import (
	"context"
	"database/sql"
	"time"
)

// PushTarget identifies one device that can receive a silent push: its
// device_id, its registered push token, the APNs environment (sandbox or
// production, ignored for FCM) selecting which Apple host to send through,
// and the push platform ("apns" or "fcm") selecting which service to send
// through.
type PushTarget struct {
	DeviceID    uint64
	Token       string
	Environment string
	Platform    string
}

// UpsertPushToken records (or replaces) a device's push token, its platform
// ("apns" or "fcm"), and (for apns) which environment to send through.
// environment is ignored for platform "fcm" but still stored as given, so a
// caller need not branch on platform before calling this. push_environment
// is a nullable ENUM('sandbox','production'), so an empty environment is
// written as SQL NULL rather than an invalid enum value.
func UpsertPushToken(ctx context.Context, exec Execer, deviceID uint64, token, environment, platform string, now time.Time) error {
	const query = `UPDATE devices SET push_token = ?, push_environment = ?, push_platform = ?, push_updated_at = ? WHERE device_id = ?`
	var env sql.NullString
	if environment != "" {
		env = sql.NullString{String: environment, Valid: true}
	}
	_, err := exec.ExecContext(ctx, query, token, env, platform, now.UTC(), deviceID)
	return err
}

// ListPushTargets returns every device that has registered a push token. It
// is the broadcast fan-out list for the recompute job's silent-refresh push.
func ListPushTargets(ctx context.Context, db Execer) ([]PushTarget, error) {
	const query = `SELECT devices.device_id, devices.push_token, devices.push_environment, devices.push_platform FROM devices WHERE devices.push_token IS NOT NULL`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []PushTarget
	for rows.Next() {
		var t PushTarget
		var env sql.NullString
		if err := rows.Scan(&t.DeviceID, &t.Token, &env, &t.Platform); err != nil {
			return nil, err
		}
		t.Environment = env.String
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return targets, nil
}
