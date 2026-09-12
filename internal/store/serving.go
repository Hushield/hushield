package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
// admin override landing mid-rebuild cannot be lost.
//
// The switch is a single-row UPDATE of blocklist_serving_slot. It holds no DDL
// lock, so readers are never queued behind it: a request either resolves the
// pointer before the update and serves the whole previous snapshot, or after
// and serves the whole new one. Never a torn mixture, which matters because
// the rebuild rewrites derived state for every number and took 58m52s at 732k.
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

	if _, err := db.ExecContext(ctx, `TRUNCATE TABLE `+standbyTable); err != nil {
		return fmt.Errorf("store: truncating standby slot %s: %w", standby, err)
	}

	insert := `INSERT INTO ` + standbyTable + ` (` + servingColumns + `)
SELECT ` + servingColumns + ` FROM phone_numbers`
	if _, err := db.ExecContext(ctx, insert); err != nil {
		return fmt.Errorf("store: populating standby slot %s: %w", standby, err)
	}

	// Count while the rebuild is still fresh in the buffer pool, and publish it
	// in the same UPDATE that moves the pointer, so the count a reader sees
	// always describes the slot it is being pointed at.
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
