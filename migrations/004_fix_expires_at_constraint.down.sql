-- Re-add the CHECK constraint
ALTER TABLE user_entitlements ADD CONSTRAINT user_entitlements_check1
    CHECK (expires_at IS NULL OR expires_at > updated_at);
