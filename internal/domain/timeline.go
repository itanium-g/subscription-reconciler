package domain

import "time"

const (
	MinTimelineLimit int32 = 1
	MaxTimelineLimit int32 = 1000
)

var (
	ErrInvalidUserID         = DomainError{Code: "INVALID_USER_ID", Message: "userId must match ^[a-z_][a-z0-9_]*$"}
	ErrInvalidTimelineLimit  = DomainError{Code: "INVALID_PARAMETERS", Message: "limit must be between 1 and 1000"}
	ErrInvalidTimelineOffset = DomainError{Code: "INVALID_PARAMETERS", Message: "offset must be >= 0"}
)

// ValidateTimelineUserID enforces the public user identifier format.
func ValidateTimelineUserID(userID string) error {
	if userID == "" {
		return ErrMissingUserID
	}
	if !isValidTimelineUserID(userID) {
		return ErrInvalidUserID
	}
	return nil
}

func isValidTimelineUserID(userID string) bool {
	for i := 0; i < len(userID); i++ {
		ch := userID[i]
		if i == 0 {
			if !((ch >= 'a' && ch <= 'z') || ch == '_') {
				return false
			}
			continue
		}
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return false
		}
	}
	return true
}

// ValidateTimelinePagination enforces the supported timeline page size and offset.
func ValidateTimelinePagination(limit, offset int32) error {
	if limit < MinTimelineLimit || limit > MaxTimelineLimit {
		return ErrInvalidTimelineLimit
	}
	if offset < 0 {
		return ErrInvalidTimelineOffset
	}
	return nil
}

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
