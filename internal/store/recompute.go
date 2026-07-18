package store

import (
	"context"
	"database/sql"
	"time"

	"spamfilter/internal/trust"
)

// RecomputeAllNumbers re-applies time-decay and recomputes the cached status
// for every phone number, via RecomputeNumber. It is what lets stale spam
// age off (or a number cross into "blocked") even when no new reports have
// come in. It returns how many phone numbers were processed.
func RecomputeAllNumbers(ctx context.Context, db *sql.DB, now time.Time) (int, error) {
	const idsQuery = `SELECT phone_number_id FROM phone_numbers`

	rows, err := db.QueryContext(ctx, idsQuery)
	if err != nil {
		return 0, err
	}
	var ids []uint64
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	for _, id := range ids {
		if _, err := RecomputeNumber(ctx, db, id, now); err != nil {
			return 0, err
		}
	}

	return len(ids), nil
}

// RecomputeAllTrust recomputes every device's trust_weight from its
// reporting history via trust.Compute, and refreshes its cached
// report_count. It returns how many devices were processed.
func RecomputeAllTrust(ctx context.Context, db *sql.DB, now time.Time) (int, error) {
	// See RecomputeNumber for why now is truncated to whole seconds before
	// being used to derive an age from a MySQL TIMESTAMP column.
	now = now.UTC().Truncate(time.Second)

	const idsQuery = `SELECT device_id FROM devices`

	rows, err := db.QueryContext(ctx, idsQuery)
	if err != nil {
		return 0, err
	}
	var ids []uint64
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	const reportCountQuery = `SELECT COUNT(*) FROM reports WHERE reports.device_id = ?`
	const firstSeenQuery = `SELECT devices.first_seen_at FROM devices WHERE devices.device_id = ?`
	const disagreementsQuery = `SELECT COUNT(*) FROM reports JOIN phone_numbers ON reports.phone_number_id = phone_numbers.phone_number_id
WHERE reports.device_id = ? AND ((reports.vote = 'spam' AND phone_numbers.status = 'allowlisted')
OR (reports.vote = 'not_spam' AND phone_numbers.status = 'blocked'))`
	const updateQuery = `UPDATE devices SET trust_weight = ?, report_count = ? WHERE device_id = ?`

	for _, id := range ids {
		var reportCount int
		if err := db.QueryRowContext(ctx, reportCountQuery, id).Scan(&reportCount); err != nil {
			return 0, err
		}

		var firstSeenAt time.Time
		if err := db.QueryRowContext(ctx, firstSeenQuery, id).Scan(&firstSeenAt); err != nil {
			return 0, err
		}
		ageDays := now.Sub(firstSeenAt).Hours() / 24
		if ageDays < 0 {
			ageDays = 0
		}

		var disagreements int
		if err := db.QueryRowContext(ctx, disagreementsQuery, id).Scan(&disagreements); err != nil {
			return 0, err
		}

		weight := trust.Compute(reportCount, ageDays, disagreements)
		if _, err := db.ExecContext(ctx, updateQuery, weight, reportCount, id); err != nil {
			return 0, err
		}
	}

	return len(ids), nil
}

// RecomputeAll runs the full recompute cycle: pass 1 (RecomputeAllNumbers)
// decays and re-scores every phone number using each device's current
// trust_weight; pass 2 (RecomputeAllTrust) then recomputes every device's
// trust_weight using the freshly-updated number statuses (disagreements
// depend on status). Number status and device trust co-depend, so a single
// run cannot fully settle both in one shot; running two passes per cron
// invocation lets the pair converge across successive runs rather than lag
// a full cycle behind. That is intentional, not a bug.
func RecomputeAll(ctx context.Context, db *sql.DB, now time.Time) (numbers int, devices int, err error) {
	numbers, err = RecomputeAllNumbers(ctx, db, now)
	if err != nil {
		return 0, 0, err
	}

	devices, err = RecomputeAllTrust(ctx, db, now)
	if err != nil {
		return numbers, 0, err
	}

	return numbers, devices, nil
}
