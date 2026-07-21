-- Push tokens let the server nudge a device with a silent (content-available)
-- APNs push so it refreshes its cached blocklist. push_token is the APNs
-- device token; push_environment selects the APNs host (sandbox vs
-- production). These are device tokens, not user PII. All NULL until a device
-- registers via POST /api/v1/devices/push-token.
ALTER TABLE devices
  ADD COLUMN push_token VARCHAR(255) NULL AFTER sign_count,
  ADD COLUMN push_environment ENUM('sandbox','production') NULL AFTER push_token,
  ADD COLUMN push_updated_at TIMESTAMP NULL AFTER push_environment;
