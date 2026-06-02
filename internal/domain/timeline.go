package domain

import "time"

// TimelineEntry represents a single entitlement state change.
type TimelineEntry struct {
	Timestamp         time.Time  `json:"timestamp"`
	Source            string     `json:"source"`
	Active            bool       `json:"active"`
	Reason            *string    `json:"reason"`
	ExpiresAt         *time.Time `json:"expiresAt"`
	TriggeringEventID *string    `json:"triggeringEventId"`
}

// TimelineResponse represents the user's entitlement change history.
type TimelineResponse struct {
	UserID  string          `json:"userId"`
	Entries []TimelineEntry `json:"entries"`
	Total   int64           `json:"total"`
	Limit   int32           `json:"limit"`
	Offset  int32           `json:"offset"`
}
