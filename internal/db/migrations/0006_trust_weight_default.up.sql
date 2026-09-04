-- Align devices.trust_weight column default with trust.TrustBase (0.5).
-- Enrolment (api.upsertDevice) omits trust_weight and relied on DEFAULT 1.00,
-- so brand-new devices looked fully trusted until the first recompute halved them.
ALTER TABLE devices
  MODIFY trust_weight DECIMAL(5,2) NOT NULL DEFAULT 0.50;
