-- was_blockable is a sticky (never reset 1->0) tombstone flag: it records
-- whether a number has EVER been in a blockable status (blocked, suspected,
-- overridden_block). BlocklistDelta's removal sub-query uses it to find
-- numbers that used to be blockable and have since fallen back to
-- unknown/allowlisted, so it can emit an action:"unblock" tombstone telling
-- an incremental client to remove a number it previously received as
-- block/label.
ALTER TABLE phone_numbers ADD COLUMN was_blockable TINYINT(1) NOT NULL DEFAULT 0 AFTER status;
CREATE INDEX idx_phone_numbers_was_blockable_updated ON phone_numbers (was_blockable, updated_at, phone_number_id);
