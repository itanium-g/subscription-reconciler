package postgres

import (
	"context"
	"time"
)

// Entitlement represents a user's premium subscription from a single source.
type Entitlement struct {
	UserID          string
	Source          string
	Active          bool
	ExpiresAt       *time.Time
	Reason          *string
	UpdatedAt       time.Time
	LastEventTime   int64
	CarrierPolledAt *time.Time
}

// StoreEvent represents a store webhook event.
type StoreEvent struct {
	ID          int64
	EventID     string
	UserID      string
	Type        string
	EventTimeMs int64
	ProductID   *string
	CreatedAt   time.Time
}

// MarketplaceRevocation represents a marketplace revoke operation.
type MarketplaceRevocation struct {
	ID        int64
	EventID   string
	UserID    string
	CreatedAt time.Time
}

// ProcessedEvent represents a processed external event (for deduplication).
type ProcessedEvent struct {
	EventID     string
	Source      string
	ProcessedAt time.Time
}

// Notification represents a scheduled notification.
type Notification struct {
	ID           int64
	UserID       string
	Type         string
	ScheduledFor time.Time
	SentAt       *time.Time
	CreatedAt    time.Time
}

// AuditLog represents an entitlement state transition.
type AuditLog struct {
	ID                int64
	UserID            string
	Source            string
	PreviousActive    *bool
	NextActive        bool
	PreviousExpiresAt *time.Time
	NextExpiresAt     *time.Time
	TriggeringEventID *string
	Reason            *string
	CreatedAt         time.Time
}

// EntitlementRepository handles entitlement persistence.
type EntitlementRepository interface {
	GetEntitlementByUserAndSource(ctx context.Context, userID string, source string) (*Entitlement, error)
	GetEntitlementsByUser(ctx context.Context, userID string) ([]Entitlement, error)
	UpsertEntitlement(ctx context.Context, userID string, source string, active bool, expiresAt *time.Time, reason *string, lastEventTime int64, triggeringEventID *string) (bool, error)
	UpdateEntitlementCarrierPolledAt(ctx context.Context, userID string, source string) error
	GetLastEventTimeFromStore(ctx context.Context, userID string) (int64, error)
	GetEntitlementsExpiringWithin24h(ctx context.Context) ([]Entitlement, error)
	GetCarrierEntitlementsForPolling(ctx context.Context, limit int32) ([]Entitlement, error)
}

// StoreEventRepository handles store event persistence.
type StoreEventRepository interface {
	InsertStoreEvent(ctx context.Context, eventID string, userID string, eventType string, eventTimeMs int64, productID *string) (bool, error)
	GetStoreEventsByUser(ctx context.Context, userID string) ([]StoreEvent, error)
	GetStoreEventByID(ctx context.Context, eventID string) (*StoreEvent, error)
}

// MarketplaceRevocationRepository handles marketplace revocation persistence.
type MarketplaceRevocationRepository interface {
	InsertMarketplaceRevocation(ctx context.Context, eventID string, userID string) (bool, error)
	GetMarketplaceRevocationByEventID(ctx context.Context, eventID string) (*MarketplaceRevocation, error)
}

// ProcessedEventRepository handles idempotency checks.
type ProcessedEventRepository interface {
	IsEventProcessed(ctx context.Context, eventID string, source string) (bool, error)
	MarkEventProcessed(ctx context.Context, eventID string, source string) error
	GetProcessedEvent(ctx context.Context, eventID string, source string) (*ProcessedEvent, error)
}

// NotificationRepository handles notification persistence.
type NotificationRepository interface {
	ScheduleNotification(ctx context.Context, userID string, notificationType string, scheduledFor time.Time) error
	GetDueNotifications(ctx context.Context, limit int32) ([]Notification, error)
	MarkNotificationSent(ctx context.Context, id int64) error
	GetNotificationByUserTypeAndDate(ctx context.Context, userID string, notificationType string, date time.Time) (*Notification, error)
}

// AuditLogRepository handles audit log persistence (stretch feature).
type AuditLogRepository interface {
	InsertAuditLog(ctx context.Context, userID string, source string, previousActive *bool, nextActive bool, previousExpiresAt *time.Time, nextExpiresAt *time.Time, triggeringEventID *string, reason *string) error
	GetAuditLogsByUser(ctx context.Context, userID string, limit int32, offset int32) ([]AuditLog, error)
	CountAuditLogsByUser(ctx context.Context, userID string) (int64, error)
}

// Database wraps all repository interfaces.
type Database interface {
	EntitlementRepository
	StoreEventRepository
	MarketplaceRevocationRepository
	ProcessedEventRepository
	NotificationRepository
	AuditLogRepository
}
