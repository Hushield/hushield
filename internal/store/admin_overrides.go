package store

import (
	"context"
	"time"
)

// UpsertOverride inserts an admin_overrides row for phoneNumberID, or
// updates its mode/reason/admin if a row already exists (there is at most
// one override per phone number). An empty reason is stored as NULL.
// RecomputeNumber picks up the change on its next run.
func UpsertOverride(ctx context.Context, exec Execer, phoneNumberID uint64, mode string, reason, admin string, now time.Time) error {
	const query = `INSERT INTO admin_overrides (phone_number_id, mode, reason, admin, created_at)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE mode = VALUES(mode), reason = VALUES(reason), admin = VALUES(admin)`

	var reasonArg any
	if reason != "" {
		reasonArg = reason
	}

	_, err := exec.ExecContext(ctx, query, phoneNumberID, mode, reasonArg, admin, now.UTC())
	return err
}
