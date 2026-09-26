-- Cache how many rows the live slot can actually serve, so a syncing client
-- has a denominator for a progress bar.
--
-- The alternative was COUNT(*) per request. That is not viable: the delta's
-- status filter is not the leading column of any index, so the count scans,
-- and a full sync asks ~733 times. The rebuild in SwapBlocklistServing already
-- walks every row, so counting there is free and the API reads one row.
--
-- Counts only SERVABLE statuses (block/label), matching blocklistBaseQuery.
-- Two consequences, both deliberate:
--   - Per-row patches from UpsertServingRow do not adjust this, so between
--     switches it drifts by however many numbers were reported in that window.
--     A handful out of 732k is irrelevant to a progress bar and not worth
--     paying for on every report.
--   - A delta also carries "unblock" tombstones, which are NOT in this count,
--     so a removal-heavy sync can apply more entries than the total. The
--     client clamps its displayed fraction rather than pretending otherwise.
ALTER TABLE blocklist_serving_slot
    ADD COLUMN servable_count BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER active_slot;

-- Backfill from whichever slot is live now, so the bar is right before the
-- first switch rather than reading zero.
UPDATE blocklist_serving_slot
SET servable_count = (
    SELECT COUNT(*) FROM blocklist_serving_a
    WHERE status IN ('blocked','overridden_block','suspected')
)
WHERE slot_id = 1 AND active_slot = 'a';

UPDATE blocklist_serving_slot
SET servable_count = (
    SELECT COUNT(*) FROM blocklist_serving_b
    WHERE status IN ('blocked','overridden_block','suspected')
)
WHERE slot_id = 1 AND active_slot = 'b';
