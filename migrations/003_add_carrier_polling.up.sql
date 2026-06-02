-- Add carrier_state tracking for polling coordination
-- Optimistic locking column for concurrent carrier polling

ALTER TABLE user_entitlements 
ADD COLUMN carrier_polled_at TIMESTAMPTZ;

CREATE INDEX idx_user_entitlements_carrier_polled 
ON user_entitlements(carrier_polled_at) 
WHERE source = 'CARRIER' AND active = TRUE;
