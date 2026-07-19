-- phone_numbers.number is already covered by the UNIQUE key
-- uq_phone_numbers_number; the plain idx_phone_numbers_number on the same
-- column is redundant. Drop it, keeping the UNIQUE constraint.
ALTER TABLE phone_numbers DROP INDEX idx_phone_numbers_number;
