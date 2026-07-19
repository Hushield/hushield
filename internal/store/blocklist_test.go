package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"spamfilter/internal/dbtest"
	"spamfilter/internal/scoring"
)

func TestBlocklistDelta_ActionsAndStatuses(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	blockedNumber := "+14155551101"
	blockedID, err := UpsertPhoneNumber(ctx, sqlDB, blockedNumber, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber (blocked): %v", err)
	}
	for _, keyID := range []string{"blocklist-device-1", "blocklist-device-2", "blocklist-device-3"} {
		deviceID := insertDevice(t, sqlDB, keyID, 1.0)
		if _, err := UpsertReport(ctx, sqlDB, deviceID, blockedID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
			t.Fatalf("UpsertReport (blocked): %v", err)
		}
	}
	if _, err := RecomputeNumber(ctx, sqlDB, blockedID, now); err != nil {
		t.Fatalf("RecomputeNumber (blocked): %v", err)
	}

	suspectedNumber := "+14155551102"
	suspectedID, err := UpsertPhoneNumber(ctx, sqlDB, suspectedNumber, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber (suspected): %v", err)
	}
	suspectedDevice := insertDevice(t, sqlDB, "blocklist-device-4", 1.0)
	if _, err := UpsertReport(ctx, sqlDB, suspectedDevice, suspectedID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
		t.Fatalf("UpsertReport (suspected): %v", err)
	}
	if _, err := RecomputeNumber(ctx, sqlDB, suspectedID, now); err != nil {
		t.Fatalf("RecomputeNumber (suspected): %v", err)
	}

	entries, nextSec, _, err := BlocklistDelta(ctx, sqlDB, 0, 0, "", 500)
	if err != nil {
		t.Fatalf("BlocklistDelta: %v", err)
	}

	byNumber := make(map[string]BlocklistEntry, len(entries))
	for _, e := range entries {
		byNumber[e.Number] = e
	}

	blockedEntry, ok := byNumber[blockedNumber]
	if !ok {
		t.Fatalf("blocked number missing from delta")
	}
	if blockedEntry.Action != "block" {
		t.Errorf("blocked entry action = %q, want %q", blockedEntry.Action, "block")
	}
	if blockedEntry.Status != scoring.StatusBlocked {
		t.Errorf("blocked entry status = %q, want %q", blockedEntry.Status, scoring.StatusBlocked)
	}

	suspectedEntry, ok := byNumber[suspectedNumber]
	if !ok {
		t.Fatalf("suspected number missing from delta")
	}
	if suspectedEntry.Action != "label" {
		t.Errorf("suspected entry action = %q, want %q", suspectedEntry.Action, "label")
	}
	if suspectedEntry.Status != scoring.StatusSuspected {
		t.Errorf("suspected entry status = %q, want %q", suspectedEntry.Status, scoring.StatusSuspected)
	}

	if nextSec <= 0 {
		t.Errorf("cursor sec = %d, want > 0", nextSec)
	}
}

func TestBlocklistDelta_SpoofPrefix(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	// A single "other"-category spam report scores 1 * 1.0 * 0.75 = 0.75:
	// above zero (some spam signal) but below the 2.0 SuspectThreshold, so the
	// number stays "unknown" with cached_score > 0 -- the sparse-signal case
	// the spoof query targets.
	spoofNumber := "+14155559201"
	spoofID, err := UpsertPhoneNumber(ctx, sqlDB, spoofNumber, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}
	device := insertDevice(t, sqlDB, "spoof-device-1", 1.0)
	if _, err := UpsertReport(ctx, sqlDB, device, spoofID, scoring.CategoryOther, scoring.VoteSpam, now); err != nil {
		t.Fatalf("UpsertReport: %v", err)
	}
	status, err := RecomputeNumber(ctx, sqlDB, spoofID, now)
	if err != nil {
		t.Fatalf("RecomputeNumber: %v", err)
	}
	if status != scoring.StatusUnknown {
		t.Fatalf("seed status = %s, want %s", status, scoring.StatusUnknown)
	}

	entries, _, _, err := BlocklistDelta(ctx, sqlDB, 0, 0, "", 500)
	if err != nil {
		t.Fatalf("BlocklistDelta (no prefix): %v", err)
	}
	for _, e := range entries {
		if e.Number == spoofNumber {
			t.Errorf("spoof number present without a prefix param")
		}
	}

	entries, _, _, err = BlocklistDelta(ctx, sqlDB, 0, 0, "415555", 500)
	if err != nil {
		t.Fatalf("BlocklistDelta (with prefix): %v", err)
	}
	var found *BlocklistEntry
	for i := range entries {
		if entries[i].Number == spoofNumber {
			found = &entries[i]
		}
	}
	if found == nil {
		t.Fatalf("spoof number missing with matching prefix")
	}
	if found.Action != "label" {
		t.Errorf("spoof entry action = %q, want %q", found.Action, "label")
	}
	if !found.SpoofSuspected {
		t.Errorf("spoof entry SpoofSuspected = false, want true")
	}
}

func TestBlocklistDelta_DedupeKeepsBaseStatusForMatchingPrefix(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	// A blocked number whose own prefix happens to match the caller's
	// neighbor-spoof prefix must appear exactly once, keeping the base
	// query's "block" action rather than being downgraded by the spoof
	// query's dedupe.
	number := "+14155559501"
	numberID, err := UpsertPhoneNumber(ctx, sqlDB, number, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}
	for _, keyID := range []string{"dedupe-device-1", "dedupe-device-2", "dedupe-device-3"} {
		deviceID := insertDevice(t, sqlDB, keyID, 1.0)
		if _, err := UpsertReport(ctx, sqlDB, deviceID, numberID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
			t.Fatalf("UpsertReport: %v", err)
		}
	}
	if _, err := RecomputeNumber(ctx, sqlDB, numberID, now); err != nil {
		t.Fatalf("RecomputeNumber: %v", err)
	}

	entries, _, _, err := BlocklistDelta(ctx, sqlDB, 0, 0, "415555", 500)
	if err != nil {
		t.Fatalf("BlocklistDelta: %v", err)
	}

	var matches int
	var entry BlocklistEntry
	for _, e := range entries {
		if e.Number == number {
			matches++
			entry = e
		}
	}
	if matches != 1 {
		t.Fatalf("number appeared %d times, want 1 (deduped)", matches)
	}
	if entry.Action != "block" {
		t.Errorf("action = %q, want %q (base status wins over spoof)", entry.Action, "block")
	}
	if !entry.SpoofSuspected {
		t.Errorf("SpoofSuspected = false, want true")
	}
}

func TestBlocklistDelta_NameAgreementFloor(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	agreedNumber := "+14155559301"
	agreedID, err := UpsertPhoneNumber(ctx, sqlDB, agreedNumber, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber (agreed): %v", err)
	}
	for _, keyID := range []string{"name-device-1", "name-device-2", "name-device-3"} {
		deviceID := insertDevice(t, sqlDB, keyID, 1.0)
		if _, err := UpsertReport(ctx, sqlDB, deviceID, agreedID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
			t.Fatalf("UpsertReport (agreed): %v", err)
		}
	}
	nameDevice1 := insertDevice(t, sqlDB, "name-caller-1", 1.0)
	nameDevice2 := insertDevice(t, sqlDB, "name-caller-2", 1.0)
	if err := UpsertCallerName(ctx, sqlDB, nameDevice1, agreedID, "Acme Collections", now); err != nil {
		t.Fatalf("UpsertCallerName (device1): %v", err)
	}
	if err := UpsertCallerName(ctx, sqlDB, nameDevice2, agreedID, "Acme Collections", now); err != nil {
		t.Fatalf("UpsertCallerName (device2): %v", err)
	}
	if _, err := RecomputeNumber(ctx, sqlDB, agreedID, now); err != nil {
		t.Fatalf("RecomputeNumber (agreed): %v", err)
	}

	loneNumber := "+14155559302"
	loneID, err := UpsertPhoneNumber(ctx, sqlDB, loneNumber, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber (lone): %v", err)
	}
	for _, keyID := range []string{"lone-device-1", "lone-device-2", "lone-device-3"} {
		deviceID := insertDevice(t, sqlDB, keyID, 1.0)
		if _, err := UpsertReport(ctx, sqlDB, deviceID, loneID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
			t.Fatalf("UpsertReport (lone): %v", err)
		}
	}
	loneNameDevice := insertDevice(t, sqlDB, "lone-caller-1", 1.0)
	if err := UpsertCallerName(ctx, sqlDB, loneNameDevice, loneID, "Solo Name", now); err != nil {
		t.Fatalf("UpsertCallerName (lone): %v", err)
	}
	if _, err := RecomputeNumber(ctx, sqlDB, loneID, now); err != nil {
		t.Fatalf("RecomputeNumber (lone): %v", err)
	}

	entries, _, _, err := BlocklistDelta(ctx, sqlDB, 0, 0, "", 500)
	if err != nil {
		t.Fatalf("BlocklistDelta: %v", err)
	}

	byNumber := make(map[string]BlocklistEntry, len(entries))
	for _, e := range entries {
		byNumber[e.Number] = e
	}

	agreedEntry, ok := byNumber[agreedNumber]
	if !ok {
		t.Fatalf("agreed number missing from delta")
	}
	if agreedEntry.Name == nil || *agreedEntry.Name != "Acme Collections" {
		t.Errorf("agreed entry name = %v, want %q", agreedEntry.Name, "Acme Collections")
	}

	loneEntry, ok := byNumber[loneNumber]
	if !ok {
		t.Fatalf("lone number missing from delta")
	}
	if loneEntry.Name != nil {
		t.Errorf("lone entry name = %q, want nil (below agreement floor)", *loneEntry.Name)
	}
}

func TestBlocklistDelta_CursorOnlyReturnsChanged(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	t0 := time.Now()

	numberA := "+14155559401"
	idA, err := UpsertPhoneNumber(ctx, sqlDB, numberA, t0)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber (A): %v", err)
	}
	for _, keyID := range []string{"cursor-device-1", "cursor-device-2", "cursor-device-3"} {
		deviceID := insertDevice(t, sqlDB, keyID, 1.0)
		if _, err := UpsertReport(ctx, sqlDB, deviceID, idA, scoring.CategoryScam, scoring.VoteSpam, t0); err != nil {
			t.Fatalf("UpsertReport (A): %v", err)
		}
	}
	if _, err := RecomputeNumber(ctx, sqlDB, idA, t0); err != nil {
		t.Fatalf("RecomputeNumber (A): %v", err)
	}

	numberB := "+14155559402"
	idB, err := UpsertPhoneNumber(ctx, sqlDB, numberB, t0)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber (B): %v", err)
	}
	deviceB := insertDevice(t, sqlDB, "cursor-device-4", 1.0)
	if _, err := UpsertReport(ctx, sqlDB, deviceB, idB, scoring.CategoryScam, scoring.VoteSpam, t0); err != nil {
		t.Fatalf("UpsertReport (B): %v", err)
	}
	if _, err := RecomputeNumber(ctx, sqlDB, idB, t0); err != nil {
		t.Fatalf("RecomputeNumber (B): %v", err)
	}

	firstEntries, sec1, id1, err := BlocklistDelta(ctx, sqlDB, 0, 0, "", 500)
	if err != nil {
		t.Fatalf("BlocklistDelta (initial): %v", err)
	}
	if len(firstEntries) != 2 {
		t.Fatalf("initial entries = %d, want 2", len(firstEntries))
	}
	if sec1 <= 0 {
		t.Fatalf("initial cursor sec = %d, want > 0", sec1)
	}

	// Bump only numberA's updated_at, a couple of seconds later so its
	// UNIX_TIMESTAMP is unambiguously greater than the first cursor.
	t1 := t0.Add(2 * time.Second)
	deviceExtra := insertDevice(t, sqlDB, "cursor-device-5", 1.0)
	if _, err := UpsertReport(ctx, sqlDB, deviceExtra, idA, scoring.CategoryScam, scoring.VoteSpam, t1); err != nil {
		t.Fatalf("UpsertReport (A bump): %v", err)
	}
	if _, err := RecomputeNumber(ctx, sqlDB, idA, t1); err != nil {
		t.Fatalf("RecomputeNumber (A bump): %v", err)
	}

	deltaEntries, sec2, id2, err := BlocklistDelta(ctx, sqlDB, sec1, id1, "", 500)
	if err != nil {
		t.Fatalf("BlocklistDelta (delta): %v", err)
	}
	if len(deltaEntries) != 1 {
		t.Fatalf("delta entries = %d, want 1", len(deltaEntries))
	}
	if deltaEntries[0].Number != numberA {
		t.Errorf("delta entry number = %q, want %q", deltaEntries[0].Number, numberA)
	}
	if sec2 < sec1 || (sec2 == sec1 && id2 <= id1) {
		t.Errorf("newCursor (%d,%d) not strictly greater than previous (%d,%d)", sec2, id2, sec1, id1)
	}
}

func TestBlocklistDelta_EmptyResultEchoesSince(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	entries, nextSec, nextID, err := BlocklistDelta(ctx, sqlDB, 1234567890, 42, "", 500)
	if err != nil {
		t.Fatalf("BlocklistDelta: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %d, want 0", len(entries))
	}
	if nextSec != 1234567890 || nextID != 42 {
		t.Errorf("cursor = (%d,%d), want (1234567890,42) (echoed since)", nextSec, nextID)
	}
}

// TestBlocklistDelta_KeysetPaginationDoesNotDropSameSecondRows pins the
// keyset-pagination fix: with a plain second-only cursor and no tie-break on
// phone_number_id, once more than one page's worth of rows share the same
// updated_at second (as a Task-5 RecomputeAll batch update plausibly
// produces), the page boundary lands inside that second, nextCursor becomes
// that second, and the next call's "> cursor" filter permanently skips the
// rows that shared it. This test fails against that old behavior and passes
// with the compound (sec, id) keyset cursor.
func TestBlocklistDelta_KeysetPaginationDoesNotDropSameSecondRows(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	// Truncate to whole seconds: RecomputeNumber writes this exact value as
	// updated_at, and all four numbers below share it, forcing them into
	// the same updated_at second.
	now := time.Now().UTC().Truncate(time.Second)

	numbers := []string{
		"+14155559601",
		"+14155559602",
		"+14155559603",
		"+14155559604",
	}
	for i, number := range numbers {
		numberID, err := UpsertPhoneNumber(ctx, sqlDB, number, now)
		if err != nil {
			t.Fatalf("UpsertPhoneNumber (%d): %v", i, err)
		}
		for _, suffix := range []string{"1", "2", "3"} {
			deviceID := insertDevice(t, sqlDB, fmt.Sprintf("keyset-device-%d-%s", i, suffix), 1.0)
			if _, err := UpsertReport(ctx, sqlDB, deviceID, numberID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
				t.Fatalf("UpsertReport (%d): %v", i, err)
			}
		}
		if _, err := RecomputeNumber(ctx, sqlDB, numberID, now); err != nil {
			t.Fatalf("RecomputeNumber (%d): %v", i, err)
		}
	}

	const limit = 2
	seen := make(map[string]int)
	var sec int64
	var id uint64
	for page := 0; page < 10; page++ {
		entries, nextSec, nextID, err := BlocklistDelta(ctx, sqlDB, sec, id, "", limit)
		if err != nil {
			t.Fatalf("BlocklistDelta page %d: %v", page, err)
		}
		if len(entries) == 0 {
			break
		}
		for _, e := range entries {
			seen[e.Number]++
		}
		if nextSec == sec && nextID == id {
			t.Fatalf("cursor did not advance on page %d: (%d,%d)", page, nextSec, nextID)
		}
		sec, id = nextSec, nextID
	}

	if len(seen) != len(numbers) {
		t.Fatalf("saw %d distinct numbers across pages, want %d; seen=%v", len(seen), len(numbers), seen)
	}
	for _, number := range numbers {
		if seen[number] != 1 {
			t.Errorf("number %q returned %d times across pages, want exactly 1", number, seen[number])
		}
	}
}

// TestBlocklistDelta_MalformedPrefixSkipsSpoofQuery pins the store-side
// defense-in-depth guard: BlocklistDelta must not trust prefix as already
// validated by the handler. A malformed prefix containing LIKE wildcards
// must not be allowed to widen the spoof query into matching numbers under
// an unrelated NPA-NXX.
func TestBlocklistDelta_MalformedPrefixSkipsSpoofQuery(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	// A sparse-signal number (single "other" category report, stays
	// "unknown" with cached_score > 0) under a completely different
	// NPA-NXX than the malformed prefix below. If the store trusted prefix
	// as-is, "+1" + "%%%%%%" + "%" would match virtually any "+1" number,
	// leaking this one into a caller-scoped spoof result it has no
	// business appearing in.
	number := "+19995551234"
	numberID, err := UpsertPhoneNumber(ctx, sqlDB, number, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}
	device := insertDevice(t, sqlDB, "malformed-prefix-device", 1.0)
	if _, err := UpsertReport(ctx, sqlDB, device, numberID, scoring.CategoryOther, scoring.VoteSpam, now); err != nil {
		t.Fatalf("UpsertReport: %v", err)
	}
	status, err := RecomputeNumber(ctx, sqlDB, numberID, now)
	if err != nil {
		t.Fatalf("RecomputeNumber: %v", err)
	}
	if status != scoring.StatusUnknown {
		t.Fatalf("seed status = %s, want %s", status, scoring.StatusUnknown)
	}

	entries, _, _, err := BlocklistDelta(ctx, sqlDB, 0, 0, "%%%%%%", 500)
	if err != nil {
		t.Fatalf("BlocklistDelta (malformed prefix): %v", err)
	}
	for _, e := range entries {
		if e.Number == number {
			t.Errorf("sparse-signal number leaked via malformed prefix %q -- store must validate prefix itself, not just trust the caller", "%%%%%%")
		}
	}
}
