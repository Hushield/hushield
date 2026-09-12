package store

import (
	"context"
	"database/sql"
	"fmt"
)

// servingColumns is the derived read model the blocklist delta serves. It is
// deliberately narrow: caller names are not here because lookupTopNames
// fetches them per page for at most `limit` ids, so copying them into every
// snapshot would be wasted work.
const servingColumns = `phone_number_id, number, cached_score, status, was_blockable, updated_at`

// SwapBlocklistServing rebuilds the standby copy of the blocklist read model
// from phone_numbers and swaps it in atomically.
//
// phone_numbers remains the source of truth and is never swapped -- only this
// derived projection is -- so a community report, attestation or admin
// override landing mid-rebuild cannot be lost.
//
// The point of the swap is that the decay pass rewrites derived state for
// every number, which took 58m52s at 732k numbers. Without this, readers spent
// that entire hour querying a table mid-rewrite. RENAME TABLE with multiple
// pairs is a single atomic DDL statement in MySQL 8, so a reader gets either
// the whole previous snapshot or the whole new one, and a failure mid-statement
// rolls back every rename rather than leaving the tmp name behind.
func SwapBlocklistServing(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `TRUNCATE TABLE blocklist_serving_next`); err != nil {
		return fmt.Errorf("store: truncating standby serving table: %w", err)
	}

	insert := `INSERT INTO blocklist_serving_next (` + servingColumns + `)
SELECT ` + servingColumns + ` FROM phone_numbers`
	if _, err := db.ExecContext(ctx, insert); err != nil {
		return fmt.Errorf("store: populating standby serving table: %w", err)
	}

	const swap = `RENAME TABLE
	blocklist_serving TO blocklist_serving_tmp,
	blocklist_serving_next TO blocklist_serving,
	blocklist_serving_tmp TO blocklist_serving_next`
	if _, err := db.ExecContext(ctx, swap); err != nil {
		return fmt.Errorf("store: swapping serving table: %w", err)
	}

	return nil
}

// UpsertServingRow refreshes one number's row in the LIVE serving table.
//
// Without this, the serving copy would only advance at a swap, so a number
// someone just reported would not reach devices until the next decay pass --
// up to 6 hours. RecomputeNumber calls this inside the same transaction as the
// report, which keeps the existing behaviour of a report taking effect
// immediately while the bulk pass stays isolated behind the swap.
//
// The swap overwrites these patches wholesale on its next run, which is
// correct: it rebuilds from phone_numbers, where the same write already
// landed.
func UpsertServingRow(ctx context.Context, exec Execer, phoneNumberID uint64) error {
	const query = `INSERT INTO blocklist_serving (` + servingColumns + `)
SELECT ` + servingColumns + ` FROM phone_numbers WHERE phone_number_id = ?
ON DUPLICATE KEY UPDATE
	number = VALUES(number),
	cached_score = VALUES(cached_score),
	status = VALUES(status),
	was_blockable = VALUES(was_blockable),
	updated_at = VALUES(updated_at)`
	if _, err := exec.ExecContext(ctx, query, phoneNumberID); err != nil {
		return fmt.Errorf("store: upserting serving row %d: %w", phoneNumberID, err)
	}
	return nil
}
