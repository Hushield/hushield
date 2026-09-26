DROP TABLE blocklist_serving_slot;

ALTER TABLE blocklist_serving_a
    RENAME INDEX idx_blocklist_serving_a_updated_at_id TO idx_blocklist_serving_updated_at_id,
    RENAME INDEX idx_blocklist_serving_a_number        TO idx_blocklist_serving_number,
    RENAME INDEX idx_blocklist_serving_a_was_blockable TO idx_blocklist_serving_was_blockable;

ALTER TABLE blocklist_serving_b
    RENAME INDEX idx_blocklist_serving_b_updated_at_id TO idx_blocklist_serving_updated_at_id,
    RENAME INDEX idx_blocklist_serving_b_number        TO idx_blocklist_serving_number,
    RENAME INDEX idx_blocklist_serving_b_was_blockable TO idx_blocklist_serving_was_blockable;

RENAME TABLE
    blocklist_serving_a TO blocklist_serving,
    blocklist_serving_b TO blocklist_serving_next;
