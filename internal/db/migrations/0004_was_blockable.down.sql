DROP INDEX idx_phone_numbers_was_blockable_updated ON phone_numbers;
ALTER TABLE phone_numbers DROP COLUMN was_blockable;
