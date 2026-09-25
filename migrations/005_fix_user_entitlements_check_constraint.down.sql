-- Restore the legacy expiration check when rolling back migration 005.
ALTER TABLE user_entitlements ADD CONSTRAINT user_entitlements_check
    CHECK (expires_at IS NULL OR expires_at > updated_at);
