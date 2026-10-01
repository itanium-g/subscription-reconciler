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
func NewTimelineService(db postgres.AuditLogRepository) TimelineService {
	return &timelineService{db: db}
}

type timelineService struct {
	db postgres.AuditLogRepository
}

// GetEntitlementTimeline returns the entitlement change history for a user.
func (s *timelineService) GetEntitlementTimeline(ctx context.Context, userID string, limit int32, offset int32) (*domain.TimelineResponse, error) {
	if err := domain.ValidateTimelineUserID(userID); err != nil {
		return nil, err
	}
	if err := domain.ValidateTimelinePagination(limit, offset); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	auditLogs, err := s.db.GetAuditLogsByUser(ctx, userID, limit, offset)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	total, err := s.db.CountAuditLogsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Convert audit logs to timeline entries
	entries := make([]domain.TimelineEntry, len(auditLogs))
	for i, log := range auditLogs {
		entries[i] = domain.TimelineEntry{
			Timestamp:         log.CreatedAt,
			Source:            log.Source,
			Active:            log.NextActive,
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
