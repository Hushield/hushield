package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"spamfilter/internal/scoring"
)

// MinNameAgreement is the minimum number of distinct-device caller-name
// reports required before a community-supplied caller-ID name is surfaced in
// the blocklist delta. Below this floor, BlocklistDelta leaves Name nil.
const MinNameAgreement = 2

// BlocklistEntry is a single number the /api/v1/blocklist delta tells a
// device to block or label, with an optional community caller-ID name and
// whether the number is suspected of spoofing the caller's own NPA-NXX.
type BlocklistEntry struct {
	Number         string
	Status         scoring.Status
	Action         string // "block" or "label"
	Name           *string
	SpoofSuspected bool
	UpdatedAt      time.Time
}

// blocklistRow is the internal shape of a single base/spoof query result. It
// carries phone_number_id (needed to join caller_names) but that id is not
// part of the public BlocklistEntry. updatedAtUnix is the DB-computed
// UNIX_TIMESTAMP(updated_at), which we also use for nextCursor: the MySQL
// server here does not run in UTC (its session time zone offsets
// UNIX_TIMESTAMP()'s reading of a stored value from what the Go driver's
// time.Time.Unix() reports for that same value), so computing the cursor
// from the DB's own UNIX_TIMESTAMP() keeps it self-consistent with the
// queries' "UNIX_TIMESTAMP(updated_at) > ?" filters on the next call,
// regardless of that offset.
type blocklistRow struct {
	phoneNumberID uint64
	number        string
	status        scoring.Status
	updatedAt     time.Time
	updatedAtUnix int64
}

const blocklistBaseQuery = `SELECT phone_numbers.phone_number_id, phone_numbers.number, phone_numbers.status, phone_numbers.updated_at, UNIX_TIMESTAMP(phone_numbers.updated_at) FROM phone_numbers
WHERE phone_numbers.status IN ('blocked','overridden_block','suspected')
  AND UNIX_TIMESTAMP(phone_numbers.updated_at) > ?
ORDER BY phone_numbers.updated_at ASC LIMIT ?`

// blocklistSpoofQuery finds sparse-signal numbers that spoof the caller's own
// NPA-NXX prefix. Rationale: the spoof-adjusted score
// (cached_score + NeighborSpoofBonus (2.0) >= SuspectThreshold (2.0)) holds
// for any cached_score > 0, so a prefix match with any residual spam signal
// deserves to be surfaced as a "label" entry even though the number's stored
// status is still "unknown" (the cached status/score are computed without
// knowledge of the querying caller's prefix).
const blocklistSpoofQuery = `SELECT phone_numbers.phone_number_id, phone_numbers.number, phone_numbers.status, phone_numbers.updated_at, UNIX_TIMESTAMP(phone_numbers.updated_at) FROM phone_numbers
WHERE phone_numbers.number LIKE ?
  AND phone_numbers.status = 'unknown'
  AND phone_numbers.cached_score > 0
  AND UNIX_TIMESTAMP(phone_numbers.updated_at) > ?
ORDER BY phone_numbers.updated_at ASC LIMIT ?`

// BlocklistDelta returns the numbers a device should block or label that
// changed since sinceUnix (a UNIX_TIMESTAMP cursor; 0 for a full snapshot),
// optionally widened by prefix (the caller's own 6-digit NPA-NXX) to include
// neighbor-spoof candidates. It also returns nextCursor: the max updated_at
// (unix seconds) among the returned entries, or sinceUnix if none were
// returned.
func BlocklistDelta(ctx context.Context, db *sql.DB, sinceUnix int64, prefix string, limit int) ([]BlocklistEntry, int64, error) {
	baseRows, err := queryBlocklistRows(ctx, db, blocklistBaseQuery, sinceUnix, limit)
	if err != nil {
		return nil, 0, err
	}

	merged := make(map[uint64]blocklistRow, len(baseRows))
	order := make([]uint64, 0, len(baseRows))
	for _, row := range baseRows {
		merged[row.phoneNumberID] = row
		order = append(order, row.phoneNumberID)
	}

	if prefix != "" {
		spoofRows, err := queryBlocklistRows(ctx, db, blocklistSpoofQuery, "+1"+prefix+"%", sinceUnix, limit)
		if err != nil {
			return nil, 0, err
		}
		for _, row := range spoofRows {
			// A number present in both sets keeps its base status/action.
			if _, exists := merged[row.phoneNumberID]; exists {
				continue
			}
			merged[row.phoneNumberID] = row
			order = append(order, row.phoneNumberID)
		}
	}

	if len(order) == 0 {
		return []BlocklistEntry{}, sinceUnix, nil
	}

	names, err := lookupTopNames(ctx, db, order)
	if err != nil {
		return nil, 0, err
	}

	entries := make([]BlocklistEntry, 0, len(order))
	var maxUpdated int64
	for _, id := range order {
		row := merged[id]

		action := "label"
		if row.status == scoring.StatusBlocked || row.status == scoring.StatusOverriddenBlock {
			action = "block"
		}

		var namePtr *string
		if name, ok := names[id]; ok {
			n := name
			namePtr = &n
		}

		entries = append(entries, BlocklistEntry{
			Number:         row.number,
			Status:         row.status,
			Action:         action,
			Name:           namePtr,
			SpoofSuspected: prefix != "" && strings.HasPrefix(row.number, "+1"+prefix),
			UpdatedAt:      row.updatedAt,
		})

		if row.updatedAtUnix > maxUpdated {
			maxUpdated = row.updatedAtUnix
		}
	}

	nextCursor := maxUpdated
	if nextCursor == 0 {
		nextCursor = sinceUnix
	}

	return entries, nextCursor, nil
}

func queryBlocklistRows(ctx context.Context, db *sql.DB, query string, args ...any) ([]blocklistRow, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []blocklistRow
	for rows.Next() {
		var row blocklistRow
		var status string
		if err := rows.Scan(&row.phoneNumberID, &row.number, &status, &row.updatedAt, &row.updatedAtUnix); err != nil {
			return nil, err
		}
		row.status = scoring.Status(status)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// lookupTopNames fetches the most-agreed community caller-ID name (at least
// MinNameAgreement distinct-device reports) for each phone_number_id in ids,
// in a single query. Ties on report count are broken by the lexicographically
// smallest name, for a deterministic result.
func lookupTopNames(ctx context.Context, db *sql.DB, ids []uint64) (map[uint64]string, error) {
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, MinNameAgreement)

	query := `SELECT caller_names.phone_number_id, caller_names.name, COUNT(*) FROM caller_names
WHERE caller_names.phone_number_id IN (` + strings.Join(placeholders, ", ") + `)
GROUP BY caller_names.phone_number_id, caller_names.name
HAVING COUNT(*) >= ?`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		name  string
		count int
	}
	best := make(map[uint64]candidate)
	for rows.Next() {
		var id uint64
		var name string
		var count int
		if err := rows.Scan(&id, &name, &count); err != nil {
			return nil, err
		}
		if cur, ok := best[id]; !ok || count > cur.count || (count == cur.count && name < cur.name) {
			best[id] = candidate{name: name, count: count}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make(map[uint64]string, len(best))
	for id, c := range best {
		result[id] = c.name
	}
	return result, nil
}
