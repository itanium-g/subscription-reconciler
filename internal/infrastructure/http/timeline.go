package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/example/adora/internal/application"
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
	if userID == "" {
		h.respondError(w, http.StatusBadRequest, "MISSING_USER_ID", "User ID is required")
		return
	}

	// Parse query parameters
	limit := int32(100)
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.ParseInt(l, 10, 32); err == nil {
			limit = int32(parsed)
		}
	}

	offset := int32(0)
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.ParseInt(o, 10, 32); err == nil {
			offset = int32(parsed)
		}
	}

	// Validate parameters
	if limit < 1 || limit > 1000 {
		h.respondError(w, http.StatusBadRequest, "INVALID_PARAMETERS", "limit must be between 1 and 1000")
		return
	}
	if offset < 0 {
		h.respondError(w, http.StatusBadRequest, "INVALID_PARAMETERS", "offset must be >= 0")
		return
	}

	// Query timeline
	timeline, err := h.service.GetEntitlementTimeline(ctx, userID, limit, offset)
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to query timeline", "user_id", userID, "err", err)
		h.respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to query timeline")
		return
	}

	// Return timeline
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(timeline)

	h.logger.InfoContext(ctx, "timeline queried", "user_id", userID, "entries", len(timeline.Entries), "total", timeline.Total)
}

// respondError sends an error response.
func (h *TimelineHandler) respondError(w http.ResponseWriter, statusCode int, code string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"error": message,
		"code":  code,
	}

	_ = json.NewEncoder(w).Encode(response)
}
