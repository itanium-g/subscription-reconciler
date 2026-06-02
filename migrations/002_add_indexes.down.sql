-- Drop indexes
DROP INDEX IF EXISTS idx_audit_logs_created_at;
DROP INDEX IF EXISTS idx_audit_logs_user_id;
DROP INDEX IF EXISTS idx_notifications_user_type;
DROP INDEX IF EXISTS idx_notifications_scheduled_for;
DROP INDEX IF EXISTS idx_marketplace_revocations_user_id;
DROP INDEX IF EXISTS idx_store_events_event_time;
DROP INDEX IF EXISTS idx_store_events_user_id;
DROP INDEX IF EXISTS idx_user_entitlements_expires_at;
DROP INDEX IF EXISTS idx_user_entitlements_user_id;
