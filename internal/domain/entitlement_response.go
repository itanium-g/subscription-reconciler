package domain

import "time"

// EntitlementResponse represents the current entitlement state for a user.
type EntitlementResponse struct {
	Active        bool       `json:"active"`
	Source        string     `json:"source"` // STORE, CARRIER, MARKETPLACE, NONE
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	LastChangedAt time.Time  `json:"lastChangedAt"`
	Reason        *string    `json:"reason,omitempty"`
}

// SourcePriority defines the priority order for entitlement sources.
// Higher index = higher priority.
var SourcePriority = map[string]int{
	"STORE":       3,
	"CARRIER":     2,
	"MARKETPLACE": 1,
	"NONE":        0,
}
