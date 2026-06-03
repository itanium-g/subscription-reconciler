-- Remove the CHECK constraint that prevents late-arriving events from being stored.
-- The constraint CHECK (expires_at IS NULL OR expires_at > updated_at) causes a hard error
-- when a late-arriving event (e.g., an INITIAL_PURCHASE from 2 months ago) computes an
-- expires_at that is still in the past while updated_at = NOW().

ALTER TABLE user_entitlements DROP CONSTRAINT IF EXISTS user_entitlements_check1;
