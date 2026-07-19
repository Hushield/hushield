// Package store provides parameterized MySQL access for phone number
// reports, and the shared RecomputeNumber unit that folds a number's reports
// through the scoring engine and caches the result on phone_numbers.
package store

import (
	"context"
	"database/sql"
	"time"

	"spamfilter/internal/scoring"
)

// Execer is satisfied by both *sql.DB and *sql.Tx, letting the functions in
// this package run either directly against the pool or inside a
// caller-managed transaction.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// UpsertPhoneNumber inserts a phone_numbers row for number if one does not
// already exist, or touches last_reported_at if it does. It returns the
// row's phone_number_id either way.
func UpsertPhoneNumber(ctx context.Context, exec Execer, number string, now time.Time) (uint64, error) {
	const query = `INSERT INTO phone_numbers (number, first_reported_at, last_reported_at)
VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE last_reported_at = VALUES(last_reported_at), phone_number_id = LAST_INSERT_ID(phone_number_id)`

	res, err := exec.ExecContext(ctx, query, number, now.UTC(), now.UTC())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return uint64(id), nil
}

// UpsertReport inserts a reports row for (deviceID, phoneNumberID) with
// weight 1.000, or updates the existing row's category/vote if the device
// already reported this number (one updatable vote per device per number).
// It reports whether a new row was inserted.
func UpsertReport(ctx context.Context, exec Execer, deviceID, phoneNumberID uint64, category scoring.Category, vote scoring.Vote, now time.Time) (bool, error) {
	const query = `INSERT INTO reports (device_id, phone_number_id, category, vote, weight, created_at, updated_at)
VALUES (?, ?, ?, ?, 1.000, ?, ?)
ON DUPLICATE KEY UPDATE category = VALUES(category), vote = VALUES(vote), updated_at = VALUES(updated_at)`

	res, err := exec.ExecContext(ctx, query, deviceID, phoneNumberID, string(category), string(vote), now.UTC(), now.UTC())
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	// MySQL's INSERT ... ON DUPLICATE KEY UPDATE reports 1 row affected for
	// a fresh insert, and 2 for an update that changed a value (0 if the
	// update was a no-op). Only 1 means a brand new row was inserted.
	return affected == 1, nil
}

// UpsertCallerName inserts or updates the community-supplied caller name for
// (deviceID, phoneNumberID).
func UpsertCallerName(ctx context.Context, exec Execer, deviceID, phoneNumberID uint64, name string, now time.Time) error {
	const query = `INSERT INTO caller_names (phone_number_id, device_id, name, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE name = VALUES(name), updated_at = VALUES(updated_at)`

	_, err := exec.ExecContext(ctx, query, phoneNumberID, deviceID, name, now.UTC(), now.UTC())
	return err
}

// TouchDevice updates a device's last_seen_at, optionally incrementing its
// report_count.
func TouchDevice(ctx context.Context, exec Execer, deviceID uint64, incrementReportCount bool, now time.Time) error {
	query := `UPDATE devices SET last_seen_at = ? WHERE device_id = ?`
	if incrementReportCount {
		query = `UPDATE devices SET last_seen_at = ?, report_count = report_count + 1 WHERE device_id = ?`
	}
	_, err := exec.ExecContext(ctx, query, now.UTC(), deviceID)
	return err
}

// RecomputeNumber reloads all reports and any admin override for
// phoneNumberID, runs them through scoring.Score, and caches the result onto
// phone_numbers. It is the shared recompute unit used after every report
// write. It returns the recomputed status.
// RecomputeNumber accepts an Execer so it can run either directly against the
// pool (the batch/backstop path) or, critically, inside the same transaction
// that just wrote the report/override. Running it in-tx makes the row lock
// held by UpsertPhoneNumber serialize concurrent recomputes for the same
// number, closing the lost-update race between the read and the cached write.
func RecomputeNumber(ctx context.Context, exec Execer, phoneNumberID uint64, now time.Time) (scoring.Status, error) {
	// MySQL TIMESTAMP columns round created_at to the nearest second, which
	// can put it up to 0.5s ahead of an unrounded now and make a
	// just-written report look negatively aged. Truncating now to whole
	// seconds (a stable lower bound on the rounded created_at) keeps the
	// resulting ageDays at or below zero for reports written this instant,
	// so scoring.Score's own clamp gives exact-1.0 decay for fresh reports.
	now = now.UTC().Truncate(time.Second)

	const reportsQuery = `SELECT reports.category, reports.vote, devices.trust_weight, reports.created_at
FROM reports
JOIN devices ON devices.device_id = reports.device_id
WHERE reports.phone_number_id = ?`

	rows, err := exec.QueryContext(ctx, reportsQuery, phoneNumberID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var reports []scoring.Report
	var reportCount, counterCount uint64
	for rows.Next() {
		var category, vote string
		var trust float64
		var createdAt time.Time
		if err := rows.Scan(&category, &vote, &trust, &createdAt); err != nil {
			return "", err
		}
		v := scoring.Vote(vote)
		reports = append(reports, scoring.Report{
			Category:    scoring.Category(category),
			Vote:        v,
			DeviceTrust: trust,
			CreatedAt:   createdAt,
		})
		if v == scoring.VoteSpam {
			reportCount++
		} else {
			counterCount++
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	const overrideQuery = `SELECT admin_overrides.mode FROM admin_overrides WHERE admin_overrides.phone_number_id = ?`
	var overrideMode sql.NullString
	if err := exec.QueryRowContext(ctx, overrideQuery, phoneNumberID).Scan(&overrideMode); err != nil && err != sql.ErrNoRows {
		return "", err
	}
	override := scoring.OverrideNone
	if overrideMode.Valid {
		override = scoring.Override(overrideMode.String)
	}

	res := scoring.Score(scoring.Input{
		Reports:       reports,
		Override:      override,
		NeighborSpoof: false,
		Now:           now,
	})

	var topCategory any
	if res.TopCategory != "" {
		topCategory = string(res.TopCategory)
	}

	const updateQuery = `UPDATE phone_numbers
SET cached_score = ?, status = ?, top_category = ?, report_count = ?, counter_count = ?, updated_at = ?
WHERE phone_number_id = ?`
	if _, err := exec.ExecContext(ctx, updateQuery, res.Score, string(res.Status), topCategory, reportCount, counterCount, now.UTC(), phoneNumberID); err != nil {
		return "", err
	}

	return res.Status, nil
}
