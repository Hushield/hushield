-- App Attest assertions carry a monotonic signature counter. Persisting the
-- last-seen value per device lets the assertion verifier enforce a strictly
-- increasing counter (replay protection) across token refreshes.
ALTER TABLE devices ADD COLUMN sign_count INT(11) UNSIGNED NOT NULL DEFAULT 0 AFTER receipt;
