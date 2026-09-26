package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

// servingColumns is the derived read model the blocklist delta serves. It is
// deliberately narrow: caller names are not here because lookupTopNames
// fetches them per page for at most `limit` ids, so copying them into every
// snapshot would be wasted work.
const servingColumns = `phone_number_id, number, cached_score, status, was_blockable, updated_at`

// servableStatuses is the status set blocklistBaseQuery serves, and therefore
// the set ServableCount counts. The two must agree or a client's progress bar
// gets a denominator that does not describe what it is receiving.
const servableStatuses = `('blocked','overridden_block','suspected')`

// The two fixed serving slots. Nothing is ever moved between them: the decay
// pass fills whichever one is not live, and the switch is a pointer update
// (see SwapBlocklistServing). Migration 0009 replaced a RENAME TABLE swap with
// this, because RENAME takes an exclusive metadata lock that every new reader
// queues behind, and no data actually needs to move.
const (
	servingSlotA = "a"
	servingSlotB = "b"
)

// servingTable maps a slot to its table name. It is the ONLY place a slot
// becomes a table identifier, and it accepts nothing but the two known slots,
// so a slot value from the database can never reach a query as arbitrary text.
func servingTable(slot string) (string, error) {
	switch slot {
	case servingSlotA:
		return "blocklist_serving_a", nil
	case servingSlotB:
		return "blocklist_serving_b", nil
	default:
		return "", fmt.Errorf("store: unknown serving slot %q", slot)
	}
}

func standbySlot(active string) (string, error) {
	switch active {
	case servingSlotA:
		return servingSlotB, nil
	case servingSlotB:
		return servingSlotA, nil
	default:
		return "", fmt.Errorf("store: unknown serving slot %q", active)
	}
}

// ActiveServingSlot reads the pointer naming the live serving slot.
func ActiveServingSlot(ctx context.Context, q Execer) (string, error) {
	var slot string
	err := q.QueryRowContext(ctx, `SELECT active_slot FROM blocklist_serving_slot WHERE slot_id = 1`).Scan(&slot)
	if err != nil {
		return "", fmt.Errorf("store: reading active serving slot: %w", err)
	}
	if _, err := servingTable(slot); err != nil {
		return "", err
	}
	return slot, nil
}

// ActiveServingTable resolves the pointer to the live table name, for callers
// that interpolate it into a query.
func ActiveServingTable(ctx context.Context, q Execer) (string, error) {
	slot, err := ActiveServingSlot(ctx, q)
	if err != nil {
		return "", err
	}
	return servingTable(slot)
}

// SwapBlocklistServing rebuilds the standby slot from phone_numbers and then
// points the API at it.
//
// phone_numbers remains the source of truth and is never switched away from --
// only this derived projection is -- so a community report, attestation or
// admin override landing mid-rebuild cannot be lost: rebuildStandbySlot's
// SELECT snapshots phone_numbers at the start of a rebuild that takes 58m52s
// at 732k rows, and anything committed to phone_numbers in that window would
// be silently absent from the standby it just built. catchUpAndActivate
// closes that window by re-reading whatever changed since, right before the
// slot goes live, rather than waiting for the next rebuild up to 6h later.
//
// The switch itself is a single-row UPDATE of blocklist_serving_slot. It
// holds no DDL lock, so readers are never queued behind it: a request either
// resolves the pointer before the update and serves the whole previous
// snapshot, or after and serves the whole new one. Never a torn mixture.
func SwapBlocklistServing(ctx context.Context, db *sql.DB) error {
	active, err := ActiveServingSlot(ctx, db)
	if err != nil {
		return err
	}
	standby, err := standbySlot(active)
	if err != nil {
		return err
	}
	standbyTable, err := servingTable(standby)
	if err != nil {
		return err
	}

	rebuildStart, err := rebuildStandbySlot(ctx, db, standbyTable)
	if err != nil {
		return err
	}

	return catchUpAndActivate(ctx, db, standby, standbyTable, rebuildStart)
}

// rebuildStandbySlot truncates and repopulates standbyTable from
// phone_numbers, and returns the instant the rebuild's SELECT started -- the
// cutoff catchUpAndActivate needs to find anything committed after that
// snapshot was taken.
func rebuildStandbySlot(ctx context.Context, db *sql.DB, standbyTable string) (time.Time, error) {
	// Truncated to whole seconds because phone_numbers.updated_at is (see
	// RecomputeNumber); a sub-second rebuildStart would make an
	// updated_at >= rebuildStart comparison miss a row rounded down to the
	// second before it.
	rebuildStart := time.Now().UTC().Truncate(time.Second)

	if _, err := db.ExecContext(ctx, `TRUNCATE TABLE `+standbyTable); err != nil {
		return time.Time{}, fmt.Errorf("store: truncating standby slot: %w", err)
	}

	insert := `INSERT INTO ` + standbyTable + ` (` + servingColumns + `)
SELECT ` + servingColumns + ` FROM phone_numbers`
	if _, err := db.ExecContext(ctx, insert); err != nil {
		return time.Time{}, fmt.Errorf("store: populating standby slot: %w", err)
	}

	return rebuildStart, nil
}

// catchUpAndActivate re-applies any phone_numbers row committed at or after
// rebuildStart into standbyTable, then counts, publishes, and activates it.
//
// A one-second margin is subtracted from rebuildStart before comparing: MySQL
// rounds updated_at's ON UPDATE CURRENT_TIMESTAMP to the nearest second, so a
// commit a few hundred milliseconds before rebuildStart's truncated instant
// can still round up to it. Re-copying a few extra unaffected rows is
// harmless; missing a raced one is what this function exists to prevent.
func catchUpAndActivate(ctx context.Context, db *sql.DB, standby, standbyTable string, rebuildStart time.Time) error {
	cutoff := rebuildStart.Add(-1 * time.Second)

	var caughtUp int64
	countCatchUp := `SELECT COUNT(*) FROM phone_numbers WHERE updated_at >= ?`
	if err := db.QueryRowContext(ctx, countCatchUp, cutoff).Scan(&caughtUp); err != nil {
		return fmt.Errorf("store: counting catch-up rows for slot %s: %w", standby, err)
	}
	if caughtUp > 0 {
		// The visible trace for a lost-update race: if this only ever prints
		// 0, the race this closes has never fired between two rebuilds.
		log.Printf("store: swap catch-up: %d row(s) changed during the standby rebuild, re-applying to slot %s", caughtUp, standby)

		catchUp := `INSERT INTO ` + standbyTable + ` (` + servingColumns + `)
SELECT ` + servingColumns + ` FROM phone_numbers WHERE updated_at >= ?
ON DUPLICATE KEY UPDATE
	number = VALUES(number),
	cached_score = VALUES(cached_score),
	status = VALUES(status),
	was_blockable = VALUES(was_blockable),
	updated_at = VALUES(updated_at)`
		if _, err := db.ExecContext(ctx, catchUp, cutoff); err != nil {
			return fmt.Errorf("store: applying catch-up rows to slot %s: %w", standby, err)
		}
	}

	// Count after catch-up, not before: a raced report can change which
	// statuses are servable, and the published denominator must describe the
	// slot as it is about to go live, not as the bulk SELECT left it.
	var servable int64
	countQuery := `SELECT COUNT(*) FROM ` + standbyTable + ` WHERE status IN ` + servableStatuses
	if err := db.QueryRowContext(ctx, countQuery).Scan(&servable); err != nil {
		return fmt.Errorf("store: counting servable rows in slot %s: %w", standby, err)
	}

	const pointerUpdate = `UPDATE blocklist_serving_slot
SET active_slot = ?, servable_count = ?, swapped_at = CURRENT_TIMESTAMP
WHERE slot_id = 1`
	if _, err := db.ExecContext(ctx, pointerUpdate, standby, servable); err != nil {
		return fmt.Errorf("store: pointing serving slot at %s: %w", standby, err)
	}

	return nil
}

// UpsertServingRow refreshes one number's row in the LIVE serving slot.
//
// Without this, the serving copy would only advance at a switch, so a number
// someone just reported would not reach devices until the next decay pass --
// up to 6 hours. RecomputeNumberServing calls this inside the same transaction
// as the report, which keeps the existing behaviour of a report taking effect
// immediately while the bulk pass stays isolated behind the pointer.
//
// The next rebuild overwrites these patches wholesale, which is correct: it
// rebuilds from phone_numbers, where the same write already landed.
func UpsertServingRow(ctx context.Context, exec Execer, phoneNumberID uint64) error {
	table, err := ActiveServingTable(ctx, exec)
	if err != nil {
		return err
	}

	query := `INSERT INTO ` + table + ` (` + servingColumns + `)
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

// withServingTable substitutes the live slot's table name into a query
// template written against the placeholder below.
func withServingTable(query, table string) string {
	return strings.ReplaceAll(query, servingTablePlaceholder, table)
}

// servingTablePlaceholder is what the delta query constants are written
// against, so they stay readable instead of being assembled from fragments.
const servingTablePlaceholder = "{{serving}}"

// ServableCount reports how many rows the live slot can serve, cached by the
// last rebuild. It is the denominator a syncing client shows progress against.
//
// This is intentionally the cached value, not a live COUNT(*): the count is
// only needed as a progress denominator, and recounting 732k rows on every one
// of a sync's ~733 requests would cost far more than the drift is worth. See
// migration 0010 for what that drift is.
func ServableCount(ctx context.Context, q Execer) (int64, error) {
	var count int64
	err := q.QueryRowContext(ctx, `SELECT servable_count FROM blocklist_serving_slot WHERE slot_id = 1`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: reading servable count: %w", err)
	}
	return count, nil
}
