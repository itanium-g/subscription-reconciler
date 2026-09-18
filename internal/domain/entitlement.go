package domain

import "time"

// Entitlement represents a user's premium subscription from a single source.
type Entitlement struct {
	UserID        string
	Source        Source
	Active        bool
	ExpiresAt     *time.Time
	Reason        *string
	UpdatedAt     time.Time
	LastEventTime int64
}

// IsActive reports whether this entitlement grants access at the supplied
// point in time. Expiration is exclusive: access ends at expires_at itself.
func (e Entitlement) IsActive(at time.Time) bool {
	if !e.Active {
		return false
	}

	return e.ExpiresAt == nil || e.ExpiresAt.After(at)
}

// Source represents where the entitlement comes from.
type Source string

const (
	SourceStore       Source = "STORE"
	SourceCarrier     Source = "CARRIER"
	SourceMarketplace Source = "MARKETPLACE"
	SourceNone        Source = "NONE"
)

// Event represents a change that affects entitlements.
type Event struct {
	EventID     string
	UserID      string
	Source      Source
	Type        EventType
	EventTimeMs int64
	CreatedAt   time.Time
	Reason      *string
}

// EventType represents the type of event.
type EventType string

const (
	EventTypeInitialPurchase EventType = "INITIAL_PURCHASE"
	EventTypeRenewal         EventType = "RENEWAL"
	EventTypeCancellation    EventType = "CANCELLATION"
	EventTypeBillingIssue    EventType = "BILLING_ISSUE"
	EventTypeExpiration      EventType = "EXPIRATION"
	EventTypeUnCancellation  EventType = "UN_CANCELLATION"
)

// Notification represents a scheduled notification.
type Notification struct {
	UserID       string
	Type         NotificationType
	ScheduledFor time.Time
	SentAt       *time.Time
	CreatedAt    time.Time
}

// NotificationType represents the type of notification.
type NotificationType string

const (
	NotificationTypePremiumExpiresSoon NotificationType = "PREMIUM_EXPIRES_SOON"
)
