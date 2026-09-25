-- devices.platform records which attestation family enrolled this device
-- ("apple" App Attest, "android" Play Integrity). Every existing row is an
-- iOS device, so the default backfills correctly with no data migration.
--
-- devices.push_platform records which push service push_token targets.
-- Independent of `platform` in principle (a future cross-platform build
-- could exist), but today's only real service is APNs, so it defaults the
-- same way.
ALTER TABLE devices
  ADD COLUMN platform ENUM('apple','android') NOT NULL DEFAULT 'apple' AFTER key_id,
  ADD COLUMN push_platform ENUM('apns','fcm') NOT NULL DEFAULT 'apns' AFTER push_environment;
