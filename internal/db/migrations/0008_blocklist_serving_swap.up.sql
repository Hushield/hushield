-- Split the blocklist into a populated copy and a served copy.
--
-- phone_numbers stays the single source of truth: every write -- community
-- reports, device attestations, admin overrides -- continues to land there, so
-- nothing a user submits can be lost by a swap. What gets swapped is only the
-- DERIVED read model the blocklist delta serves.
--
-- Why: the decay pass rewrites derived state for every number. At 732k numbers
-- a pass measured 58m52s, and for that whole hour readers were seeing a table
-- mid-rewrite. Now the pass populates blocklist_serving_next and swaps it in
-- with a single atomic RENAME TABLE, so a reader sees either the old snapshot
-- or the new one, never a torn mixture.
--
-- updated_at deliberately has NO "ON UPDATE CURRENT_TIMESTAMP". It is copied
-- verbatim from phone_numbers because it IS the sync cursor: clients page by
-- (updated_at, phone_number_id) and persist that cursor. If a copy restamped
-- it, every device's stored cursor would fall behind the whole table at once
-- and every client would re-download the entire blocklist after every swap.
CREATE TABLE blocklist_serving (
    phone_number_id BIGINT(20) UNSIGNED NOT NULL,
    number VARCHAR(20) NOT NULL,
    cached_score DECIMAL(10,4) NOT NULL DEFAULT 0.0000,
    status ENUM('unknown','suspected','blocked','allowlisted','overridden_block') NOT NULL DEFAULT 'unknown',
    was_blockable TINYINT(1) NOT NULL DEFAULT 0,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (phone_number_id),
    -- The keyset order every delta query pages by. See migration 0007 for why
    -- this leads with the ordering columns rather than with status.
    KEY idx_blocklist_serving_updated_at_id (updated_at, phone_number_id),
    -- The neighbour-spoof query's LIKE '+1NPANXX%' prefix match.
    KEY idx_blocklist_serving_number (number),
    -- The removal/tombstone query.
    KEY idx_blocklist_serving_was_blockable (was_blockable, updated_at, phone_number_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE blocklist_serving_next (
    phone_number_id BIGINT(20) UNSIGNED NOT NULL,
    number VARCHAR(20) NOT NULL,
    cached_score DECIMAL(10,4) NOT NULL DEFAULT 0.0000,
    status ENUM('unknown','suspected','blocked','allowlisted','overridden_block') NOT NULL DEFAULT 'unknown',
    was_blockable TINYINT(1) NOT NULL DEFAULT 0,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (phone_number_id),
    KEY idx_blocklist_serving_updated_at_id (updated_at, phone_number_id),
    KEY idx_blocklist_serving_number (number),
    KEY idx_blocklist_serving_was_blockable (was_blockable, updated_at, phone_number_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Seed both copies so the blocklist serves correctly from the moment this
-- migration lands, rather than returning an empty delta until the first swap.
INSERT INTO blocklist_serving
    (phone_number_id, number, cached_score, status, was_blockable, updated_at)
SELECT phone_number_id, number, cached_score, status, was_blockable, updated_at
FROM phone_numbers;

INSERT INTO blocklist_serving_next
    (phone_number_id, number, cached_score, status, was_blockable, updated_at)
SELECT phone_number_id, number, cached_score, status, was_blockable, updated_at
FROM phone_numbers;
