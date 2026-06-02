package application

import (
	"context"

	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
)

// TimelineService handles entitlement timeline queries.
type TimelineService interface {
	GetEntitlementTimeline(ctx context.Context, userID string, limit int32, offset int32) (*domain.TimelineResponse, error)
}

// NewTimelineService creates a new timeline service.
func NewTimelineService(db postgres.Database) TimelineService {
	return &timelineService{db: db}
}

type timelineService struct {
	db postgres.Database
}

// GetEntitlementTimeline returns the entitlement change history for a user.
func (s *timelineService) GetEntitlementTimeline(ctx context.Context, userID string, limit int32, offset int32) (*domain.TimelineResponse, error) {
	// Validate parameters
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}

	// Query audit logs
	auditLogs, err := s.db.GetAuditLogsByUser(ctx, userID, limit, offset)
	if err != nil {
		return nil, err
	}

	// Count total audit logs
	total, err := s.db.CountAuditLogsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Convert audit logs to timeline entries
	entries := make([]domain.TimelineEntry, len(auditLogs))
	for i, log := range auditLogs {
		// Only include entries where the state actually changed
		active := false
		if log.NextActive {
			active = true
		}

		entries[i] = domain.TimelineEntry{
			Timestamp:         log.CreatedAt,
			Source:            log.Source,
			Active:            active,
			Reason:            log.Reason,
			ExpiresAt:         log.NextExpiresAt,
			TriggeringEventID: log.TriggeringEventID,
		}
	}

	return &domain.TimelineResponse{
		UserID:  userID,
		Entries: entries,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	}, nil
}
