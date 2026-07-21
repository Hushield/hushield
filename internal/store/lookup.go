package store

import (
	"context"
	"database/sql"
	"strings"

	"spamfilter/internal/scoring"
)

// LookupResult is a single phone number's reputation, as returned by
// LookupNumber for the GET /api/v1/numbers/{e164} on-demand lookup.
type LookupResult struct {
	Number         string
	Status         scoring.Status
	Category       *scoring.Category // top_category, nil if none
	Name           *string           // community name if >= MinNameAgreement agree, else nil
	CachedScore    float64
	SpoofSuspected bool // true if prefix given and number starts with "+1"+prefix
}

// SpoofSuspected reports whether number spoofs the caller's own NPA-NXX
// prefix -- prefix non-empty and number starting with "+1"+prefix. Exported
// so the /api/v1/numbers/{e164} handler can compute the same thing for a
// number that isn't in phone_numbers at all (LookupNumber uses it directly
// for the found case).
func SpoofSuspected(number, prefix string) bool {
	return prefix != "" && strings.HasPrefix(number, "+1"+prefix)
}

// LookupNumber looks up a single phone number's cached reputation by its
// exact E.164 value. It returns found=false (not an error) when number isn't
// in phone_numbers at all -- a not-in-DB number is simply unknown to the
// community, not a lookup failure. The community caller-ID name reuses
// lookupTopNames (see blocklist.go), the same agreement-floor logic
// BlocklistDelta uses, so lookup and /blocklist agree on a number's name.
func LookupNumber(ctx context.Context, db *sql.DB, number string, prefix string) (LookupResult, bool, error) {
	const query = `SELECT phone_numbers.phone_number_id, phone_numbers.status, phone_numbers.cached_score, phone_numbers.top_category FROM phone_numbers WHERE phone_numbers.number = ?`

	var phoneNumberID uint64
	var status string
	var cachedScore float64
	var topCategory sql.NullString
	err := db.QueryRowContext(ctx, query, number).Scan(&phoneNumberID, &status, &cachedScore, &topCategory)
	if err == sql.ErrNoRows {
		return LookupResult{}, false, nil
	}
	if err != nil {
		return LookupResult{}, false, err
	}

	var category *scoring.Category
	if topCategory.Valid {
		c := scoring.Category(topCategory.String)
		category = &c
	}

	names, err := lookupTopNames(ctx, db, []uint64{phoneNumberID})
	if err != nil {
		return LookupResult{}, false, err
	}
	var namePtr *string
	if name, ok := names[phoneNumberID]; ok {
		n := name
		namePtr = &n
	}

	result := LookupResult{
		Number:         number,
		Status:         scoring.Status(status),
		Category:       category,
		Name:           namePtr,
		CachedScore:    cachedScore,
		SpoofSuspected: SpoofSuspected(number, prefix),
	}
	return result, true, nil
}
