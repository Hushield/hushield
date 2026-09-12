-- Switch the serving copy by pointer instead of by renaming tables.
--
-- 0008 published a rebuilt snapshot with an atomic RENAME TABLE. That is
-- correct, but RENAME takes an EXCLUSIVE metadata lock: it waits for in-flight
-- queries on those tables and every new reader queues behind it. Nothing is
-- being physically moved here -- both copies just sit there -- so the switch
-- should be a pointer change, not DDL.
--
-- Two fixed slots, and one singleton row saying which one is live. The decay
-- pass populates whichever slot is NOT live and then updates the pointer, a
-- single-row UPDATE that no reader can block and that takes no DDL lock.
RENAME TABLE
    blocklist_serving      TO blocklist_serving_a,
    blocklist_serving_next TO blocklist_serving_b;

-- Index names are per-table, so both copies carry the same names. Rename them
-- per slot anyway: with the tables no longer swapping identities, an EXPLAIN
-- naming idx_blocklist_serving_a_* tells you which slot you actually read.
ALTER TABLE blocklist_serving_a
    RENAME INDEX idx_blocklist_serving_updated_at_id TO idx_blocklist_serving_a_updated_at_id,
    RENAME INDEX idx_blocklist_serving_number        TO idx_blocklist_serving_a_number,
    RENAME INDEX idx_blocklist_serving_was_blockable TO idx_blocklist_serving_a_was_blockable;

ALTER TABLE blocklist_serving_b
    RENAME INDEX idx_blocklist_serving_updated_at_id TO idx_blocklist_serving_b_updated_at_id,
    RENAME INDEX idx_blocklist_serving_number        TO idx_blocklist_serving_b_number,
    RENAME INDEX idx_blocklist_serving_was_blockable TO idx_blocklist_serving_b_was_blockable;

-- Singleton by construction: the CHECK pins the primary key to one value, so a
-- second row cannot be inserted and "which slot is live" can never be
-- ambiguous. active_slot is an ENUM so the value read back is always one of
-- exactly two known table suffixes -- the store still validates it before
-- interpolating, but the column makes an unexpected value impossible to store.
CREATE TABLE blocklist_serving_slot (
    slot_id TINYINT UNSIGNED NOT NULL DEFAULT 1,
    active_slot ENUM('a','b') NOT NULL,
    swapped_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (slot_id),
    CONSTRAINT chk_blocklist_serving_slot_singleton CHECK (slot_id = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 'a' is the table 0008 was serving from, so this migration does not change
-- which rows are live.
INSERT INTO blocklist_serving_slot (slot_id, active_slot) VALUES (1, 'a');
