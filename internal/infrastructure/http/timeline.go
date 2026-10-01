package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/example/subscription-reconciler/internal/application"
	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/go-chi/chi/v5"
)

// TimelineHandler handles timeline queries.
type TimelineHandler struct {
	service application.TimelineService
	logger  *slog.Logger
}

// NewTimelineHandler creates a new timeline handler.
func NewTimelineHandler(service application.TimelineService, logger *slog.Logger) *TimelineHandler {
	return &TimelineHandler{
		service: service,
		logger:  logger,
	}
}

// HandleGetTimeline retrieves the entitlement change history for a user.
func (h *TimelineHandler) HandleGetTimeline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID := chi.URLParam(r, "userId")
	if err := domain.ValidateTimelineUserID(userID); err != nil {
		var domainErr domain.DomainError
		if errors.As(err, &domainErr) {
			respondError(w, http.StatusBadRequest, domainErr.Code, domainErr.Message)
			return
		}
		respondError(w, http.StatusBadRequest, "INVALID_USER_ID", "userId is invalid")
		return
	}

	// Parse query parameters
	limit := int32(100)
	query := r.URL.Query()
	if query.Has("limit") {
		parsed, err := strconv.ParseInt(query.Get("limit"), 10, 32)
		if err != nil {
			respondError(w, http.StatusBadRequest, "INVALID_PARAMETERS", "limit must be an integer")
			return
		}
		limit = int32(parsed)
	}

	offset := int32(0)
	if query.Has("offset") {
		parsed, err := strconv.ParseInt(query.Get("offset"), 10, 32)
		if err != nil {
			respondError(w, http.StatusBadRequest, "INVALID_PARAMETERS", "offset must be an integer")
			return
		}
		offset = int32(parsed)
	}

	if err := domain.ValidateTimelinePagination(limit, offset); err != nil {
		var domainErr domain.DomainError
		if errors.As(err, &domainErr) {
			respondError(w, http.StatusBadRequest, domainErr.Code, domainErr.Message)
		} else {
			respondError(w, http.StatusBadRequest, "INVALID_PARAMETERS", "invalid pagination parameters")
		}
		return
	}

	// Query timeline
	timeline, err := h.service.GetEntitlementTimeline(ctx, userID, limit, offset)
	if err != nil {
		var domainErr domain.DomainError
		if errors.As(err, &domainErr) {
			respondError(w, http.StatusBadRequest, domainErr.Code, domainErr.Message)
			return
		}
		h.logger.ErrorContext(ctx, "failed to query timeline", "user_id", userID, "err", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to query timeline")
		return
	}

	// Return timeline
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(timeline)

	h.logger.InfoContext(ctx, "timeline queried", "user_id", userID, "entries", len(timeline.Entries), "total", timeline.Total)
}
