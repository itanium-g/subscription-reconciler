-- Create indexes for common queries

-- user_entitlements: frequently queried by user_id
CREATE INDEX idx_user_entitlements_user_id ON user_entitlements(user_id);

-- user_entitlements: check expiring entitlements (within 24h)
CREATE INDEX idx_user_entitlements_expires_at ON user_entitlements(expires_at) 
WHERE active = TRUE AND expires_at IS NOT NULL;

-- store_events: query events for reconciliation
CREATE INDEX idx_store_events_user_id ON store_events(user_id);
CREATE INDEX idx_store_events_event_time ON store_events(event_time_ms DESC);

-- marketplace_revocations: query revocations for a user
CREATE INDEX idx_marketplace_revocations_user_id ON marketplace_revocations(user_id);

-- notifications: query due notifications
CREATE INDEX idx_notifications_scheduled_for ON notifications(scheduled_for) 
WHERE sent_at IS NULL;
CREATE INDEX idx_notifications_user_type ON notifications(user_id, type);

-- audit_logs: query history for a user
CREATE INDEX idx_audit_logs_user_id ON audit_logs(user_id, created_at DESC);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at DESC);
