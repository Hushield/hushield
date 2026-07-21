package store

import (
	"context"
	"database/sql"
	"sort"
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

// blocklistPrefixLength is the exact length of a valid NPA-NXX prefix (an
// area code + exchange, e.g. "415555"). The handler already validates this
// before calling in; isValidBlocklistPrefix re-checks it here so the store
// stays self-protecting against a future direct caller passing an
// unvalidated prefix straight into a LIKE pattern.
const blocklistPrefixLength = 6

// keysetPredicate is the compound-cursor WHERE clause shared by the base and
// spoof queries: rows strictly after (sec, id) in (updated_at, phone_number_id)
// order. A plain "UNIX_TIMESTAMP(updated_at) > ?" filter with no tie-break on
// phone_number_id silently drops rows once more than one page's worth of
// rows share the same updated_at second (very plausible right after a
// RecomputeAll batch updates many numbers in the same second): the page
// boundary lands inside that second, nextCursor becomes that second, and the
// next call's "> cursor" filter permanently skips the rows that shared it.
// The compound predicate below is drop-free: any row beyond a page's
// truncation cut has a key strictly greater than that page's nextCursor.
const keysetPredicate = `(UNIX_TIMESTAMP(phone_numbers.updated_at) > ? OR (UNIX_TIMESTAMP(phone_numbers.updated_at) = ? AND phone_numbers.phone_number_id > ?))`

const blocklistBaseQuery = `SELECT phone_numbers.phone_number_id, phone_numbers.number, phone_numbers.status, phone_numbers.updated_at, UNIX_TIMESTAMP(phone_numbers.updated_at) FROM phone_numbers
WHERE phone_numbers.status IN ('blocked','overridden_block','suspected')
  AND ` + keysetPredicate + `
ORDER BY phone_numbers.updated_at ASC, phone_numbers.phone_number_id ASC LIMIT ?`

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
  AND ` + keysetPredicate + `
ORDER BY phone_numbers.updated_at ASC, phone_numbers.phone_number_id ASC LIMIT ?`

// blocklistRemovalQuery finds numbers that were once blockable
// (was_blockable = 1, the sticky flag RecomputeNumber sets) and have since
// fallen back to a non-blockable status (unknown, e.g. after enough
// not_spam votes; or allowlisted, e.g. after an admin allow override). These
// rows become action:"unblock" tombstones: without them, a number that
// leaves the blockable set would simply vanish from future deltas, leaving
// an incremental client with no way to learn it should un-block it. Always
// run, independent of prefix -- a removal is not a neighbor-spoof concept.
const blocklistRemovalQuery = `SELECT phone_numbers.phone_number_id, phone_numbers.number, phone_numbers.status, phone_numbers.updated_at, UNIX_TIMESTAMP(phone_numbers.updated_at) FROM phone_numbers
WHERE phone_numbers.was_blockable = 1
  AND phone_numbers.status IN ('unknown','allowlisted')
  AND ` + keysetPredicate + `
ORDER BY phone_numbers.updated_at ASC, phone_numbers.phone_number_id ASC LIMIT ?`

// BlocklistDelta returns the numbers a device should block or label that
// changed since the compound cursor (sinceSec, sinceID) -- (0, 0) for a full
// snapshot -- optionally widened by prefix (the caller's own 6-digit
// NPA-NXX) to include neighbor-spoof candidates. If prefix is non-empty but
// not exactly 6 ASCII digits, it is treated as no prefix (the spoof query is
// skipped): the handler already validates prefix before calling in, but the
// store does not trust that as its only line of defense, since an
// unvalidated prefix fed straight into the spoof query's LIKE pattern could
// otherwise inject wildcards. It also returns nextSec/nextID: the
// (updated_at, phone_number_id) key of the last entry returned, or the
// incoming cursor if nothing was returned.
func BlocklistDelta(ctx context.Context, db *sql.DB, sinceSec int64, sinceID uint64, prefix string, limit int) ([]BlocklistEntry, int64, uint64, error) {
	baseRows, err := queryBlocklistRows(ctx, db, blocklistBaseQuery, sinceSec, sinceSec, sinceID, limit)
	if err != nil {
		return nil, 0, 0, err
	}

	effectivePrefix := prefix
	if effectivePrefix != "" && !isValidBlocklistPrefix(effectivePrefix) {
		effectivePrefix = ""
	}

	byID := make(map[uint64]blocklistRow, len(baseRows))
	for _, row := range baseRows {
		byID[row.phoneNumberID] = row
	}

	if effectivePrefix != "" {
		spoofRows, err := queryBlocklistRows(ctx, db, blocklistSpoofQuery, "+1"+effectivePrefix+"%", sinceSec, sinceSec, sinceID, limit)
		if err != nil {
			return nil, 0, 0, err
		}
		for _, row := range spoofRows {
			// A number present in both sets keeps its base status/action.
			if _, exists := byID[row.phoneNumberID]; exists {
				continue
			}
			byID[row.phoneNumberID] = row
		}
	}

	// The removal sub-query's status set (unknown, allowlisted) is NOT disjoint
	// from the spoof set: a spoof candidate is status='unknown', which the
	// removal query's status IN ('unknown','allowlisted') also matches. So a
	// once-blockable number now sitting at status=unknown, cached_score>0 whose
	// number matches the caller's prefix qualifies as BOTH a spoof label and a
	// removal tombstone. Precedence on that overlap: the base/spoof entry wins.
	// The dedupe below enforces it -- a phone_number_id already present (from
	// the base or spoof pass) is not overwritten by its removal row, so the
	// number surfaces once as its spoof "label", never as a duplicate
	// "unblock". (The base set's statuses -- blocked, overridden_block,
	// suspected -- remain genuinely disjoint from the removal set.)
	removalRows, err := queryBlocklistRows(ctx, db, blocklistRemovalQuery, sinceSec, sinceSec, sinceID, limit)
	if err != nil {
		return nil, 0, 0, err
	}
	removalIDs := make(map[uint64]bool, len(removalRows))
	for _, row := range removalRows {
		if _, exists := byID[row.phoneNumberID]; exists {
			continue
		}
		byID[row.phoneNumberID] = row
		removalIDs[row.phoneNumberID] = true
	}

	if len(byID) == 0 {
		return []BlocklistEntry{}, sinceSec, sinceID, nil
	}

	// The base, spoof, and removal result sets (the keyset k-way merge's
	// three sources) are each individually ordered by the DB, but merging
	// them (and deduping) does not preserve a combined order, and each was
	// independently limited so the merged set can hold up to 3x limit rows.
	// Re-sort by the same (updated_at, phone_number_id) key and truncate to
	// limit so the page boundary -- and thus nextCursor -- is correct for
	// the merged result, not just one source.
	merged := make([]blocklistRow, 0, len(byID))
	for _, row := range byID {
		merged = append(merged, row)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].updatedAtUnix != merged[j].updatedAtUnix {
			return merged[i].updatedAtUnix < merged[j].updatedAtUnix
		}
		return merged[i].phoneNumberID < merged[j].phoneNumberID
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}

	// Removal (unblock) entries carry no community caller-ID name -- a
	// removal is a removal -- so only non-removal ids need the name lookup.
	ids := make([]uint64, 0, len(merged))
	for _, row := range merged {
		if removalIDs[row.phoneNumberID] {
			continue
		}
		ids = append(ids, row.phoneNumberID)
	}

	names, err := lookupTopNames(ctx, db, ids)
	if err != nil {
		return nil, 0, 0, err
	}

	entries := make([]BlocklistEntry, 0, len(merged))
	for _, row := range merged {
		if removalIDs[row.phoneNumberID] {
			entries = append(entries, BlocklistEntry{
				Number:         row.number,
				Status:         row.status,
				Action:         "unblock",
				Name:           nil,
				SpoofSuspected: false,
				UpdatedAt:      row.updatedAt,
			})
			continue
		}

		action := "label"
		if row.status == scoring.StatusBlocked || row.status == scoring.StatusOverriddenBlock {
			action = "block"
		}

		var namePtr *string
		if name, ok := names[row.phoneNumberID]; ok {
			n := name
			namePtr = &n
		}

		entries = append(entries, BlocklistEntry{
			Number:         row.number,
			Status:         row.status,
			Action:         action,
			Name:           namePtr,
			SpoofSuspected: effectivePrefix != "" && strings.HasPrefix(row.number, "+1"+effectivePrefix),
			UpdatedAt:      row.updatedAt,
		})
	}

	last := merged[len(merged)-1]
	return entries, last.updatedAtUnix, last.phoneNumberID, nil
}

// isValidBlocklistPrefix reports whether prefix is exactly 6 ASCII digits
// (an NPA-NXX). Mirrors the API handler's own validation; kept as a
// second, independent check inside the store so BlocklistDelta stays
// self-protecting against a future caller that skips the handler.
func isValidBlocklistPrefix(prefix string) bool {
	if len(prefix) != blocklistPrefixLength {
		return false
	}
	for _, r := range prefix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
	if len(ids) == 0 {
		// A page made up entirely of removal (unblock) rows has no ids left
		// to look up -- an "IN ()" clause is invalid SQL, so short-circuit.
		return map[uint64]string{}, nil
	}

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
