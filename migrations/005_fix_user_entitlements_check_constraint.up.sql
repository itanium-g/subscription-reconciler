-- Remove the legacy expiration check if it still exists in an environment.
-- Migration 004 is intentionally immutable; this forward migration handles the
-- alternate constraint name found in older databases.
ALTER TABLE user_entitlements DROP CONSTRAINT IF EXISTS user_entitlements_check;
