-- Remove carrier polling tracking
DROP INDEX IF EXISTS idx_user_entitlements_carrier_polled;
ALTER TABLE user_entitlements DROP COLUMN IF EXISTS carrier_polled_at;
