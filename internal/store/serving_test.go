package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"spamfilter/internal/dbtest"
	"spamfilter/internal/scoring"
)

// blockableNumber creates a number with three fresh scam reports from
// trust-1.0 devices: 3 x (BaseWeight 1.0 * trust 1.0 * scam 2.0) = 6.0, over
// BlockThreshold (5.0). It recomputes with the BULK primitive, which
// deliberately does not publish to the serving copy.
func blockableNumber(t *testing.T, sqlDB *sql.DB, ctx context.Context, number, keyPrefix string, now time.Time) uint64 {
	t.Helper()
	id, err := UpsertPhoneNumber(ctx, sqlDB, number, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber(%s): %v", number, err)
	}
	for _, suffix := range []string{"a", "b", "c"} {
		deviceID := insertDevice(t, sqlDB, keyPrefix+"-"+suffix, 1.0)
		if _, err := UpsertReport(ctx, sqlDB, deviceID, id, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
			t.Fatalf("UpsertReport(%s): %v", number, err)
		}
	}
	if _, err := RecomputeNumber(ctx, sqlDB, id, now); err != nil {
		t.Fatalf("RecomputeNumber(%s): %v", number, err)
	}
	return id
}

// The whole point of the split: the bulk decay pass rewrites derived state for
// every number (58m52s at 732k numbers) without readers seeing any of it, then
// publishes the lot in one atomic swap.
func TestSwapBlocklistServing_bulkChangesAreInvisibleUntilSwapped(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	blockableNumber(t, sqlDB, ctx, "+14155559801", "swap-device", now)

	entries, _, _, err := BlocklistDelta(ctx, sqlDB, 0, 0, "", 100)
	if err != nil {
		t.Fatalf("BlocklistDelta before swap: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("bulk recompute leaked into the served copy before any swap: %+v", entries)
	}

	if err := SwapBlocklistServing(ctx, sqlDB); err != nil {
		t.Fatalf("SwapBlocklistServing: %v", err)
	}

	entries, _, _, err = BlocklistDelta(ctx, sqlDB, 0, 0, "", 100)
	if err != nil {
		t.Fatalf("BlocklistDelta after swap: %v", err)
	}
	if len(entries) != 1 || entries[0].Number != "+14155559801" || entries[0].Action != "block" {
		t.Fatalf("swap did not publish the bulk pass's result; got %+v", entries)
	}
}

// A swap must be repeatable. The rename cycles three table names, so a second
// run proves the standby name was restored rather than consumed.
func TestSwapBlocklistServing_isRepeatable(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	blockableNumber(t, sqlDB, ctx, "+14155559811", "swap-twice-1", now)
	if err := SwapBlocklistServing(ctx, sqlDB); err != nil {
		t.Fatalf("first SwapBlocklistServing: %v", err)
	}

	blockableNumber(t, sqlDB, ctx, "+14155559812", "swap-twice-2", now)
	if err := SwapBlocklistServing(ctx, sqlDB); err != nil {
		t.Fatalf("second SwapBlocklistServing: %v", err)
	}

	entries, _, _, err := BlocklistDelta(ctx, sqlDB, 0, 0, "", 100)
	if err != nil {
		t.Fatalf("BlocklistDelta: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("second swap lost or duplicated rows; got %d entries: %+v", len(entries), entries)
	}
}

// A report must reach devices on the next sync, not at the next swap up to six
// hours later, so the request path publishes its own row.
func TestRecomputeNumberServing_visibleWithoutASwap(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	number := "+14155559821"
	id, err := UpsertPhoneNumber(ctx, sqlDB, number, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}
	for _, suffix := range []string{"a", "b", "c"} {
		deviceID := insertDevice(t, sqlDB, "serve-now-"+suffix, 1.0)
		if _, err := UpsertReport(ctx, sqlDB, deviceID, id, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
			t.Fatalf("UpsertReport: %v", err)
		}
	}

	if _, err := RecomputeNumberServing(ctx, sqlDB, id, now); err != nil {
		t.Fatalf("RecomputeNumberServing: %v", err)
	}

	entries, _, _, err := BlocklistDelta(ctx, sqlDB, 0, 0, "", 100)
	if err != nil {
		t.Fatalf("BlocklistDelta: %v", err)
	}
	if len(entries) != 1 || entries[0].Number != number || entries[0].Action != "block" {
		t.Fatalf("a reported number must be servable without waiting for a swap; got %+v", entries)
	}
}

// The serving table's updated_at carries no ON UPDATE CURRENT_TIMESTAMP and is
// copied verbatim, because it IS the sync cursor. If a swap restamped it, every
// device's stored cursor would fall behind the entire table at once and every
// client would re-download the whole blocklist after every swap -- which, at
// 732k entries, is exactly the cost this work exists to remove.
func TestSwapBlocklistServing_preservesCursorSoClientsDoNotResync(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	blockableNumber(t, sqlDB, ctx, "+14155559831", "cursor-keep", now)
	if err := SwapBlocklistServing(ctx, sqlDB); err != nil {
		t.Fatalf("first SwapBlocklistServing: %v", err)
	}

	entries, nextSec, nextID, err := BlocklistDelta(ctx, sqlDB, 0, 0, "", 100)
	if err != nil {
		t.Fatalf("BlocklistDelta: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("setup: want 1 entry, got %d", len(entries))
	}

	// A caught-up client re-syncs from its cursor and should get nothing.
	if err := SwapBlocklistServing(ctx, sqlDB); err != nil {
		t.Fatalf("second SwapBlocklistServing: %v", err)
	}

	entries, _, _, err = BlocklistDelta(ctx, sqlDB, nextSec, nextID, "", 100)
	if err != nil {
		t.Fatalf("BlocklistDelta from cursor: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the swap restamped updated_at, so a caught-up client re-downloads everything: %+v", entries)
	}
}

// The switch is a pointer flip, not a data move: each rebuild fills whichever
// slot is not live and then points at it, so the active slot alternates. This
// is what makes the switch free of DDL locks -- nothing is renamed, both
// tables just sit there.
func TestSwapBlocklistServing_alternatesTheActiveSlotPointer(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()

	first, err := ActiveServingSlot(ctx, sqlDB)
	if err != nil {
		t.Fatalf("ActiveServingSlot: %v", err)
	}

	if err := SwapBlocklistServing(ctx, sqlDB); err != nil {
		t.Fatalf("first SwapBlocklistServing: %v", err)
	}
	second, err := ActiveServingSlot(ctx, sqlDB)
	if err != nil {
		t.Fatalf("ActiveServingSlot after first swap: %v", err)
	}
	if second == first {
		t.Fatalf("the pointer did not move off %q, so the rebuild overwrote the slot it was serving", first)
	}

	if err := SwapBlocklistServing(ctx, sqlDB); err != nil {
		t.Fatalf("second SwapBlocklistServing: %v", err)
	}
	third, err := ActiveServingSlot(ctx, sqlDB)
	if err != nil {
		t.Fatalf("ActiveServingSlot after second swap: %v", err)
	}
	if third != first {
		t.Fatalf("active slot = %q after two swaps, want it back at %q -- only two slots should ever be used", third, first)
	}
}

// A slot value the store does not recognise must not be interpolated into a
// query. The ENUM makes it unstorable, so this guards the code path rather
// than the schema.
func TestServingTable_rejectsUnknownSlot(t *testing.T) {
	if _, err := servingTable("../etc/passwd"); err == nil {
		t.Fatal("servingTable accepted an unknown slot; a pointer value must never reach a query as arbitrary text")
	}
	if _, err := standbySlot("c"); err == nil {
		t.Fatal("standbySlot accepted an unknown slot")
	}
}
