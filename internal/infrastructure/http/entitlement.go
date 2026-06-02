package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/example/subscription-reconciler/internal/application"
	"github.com/go-chi/chi/v5"
)

// EntitlementHandler handles entitlement queries.
type EntitlementHandler struct {
	service application.EntitlementQueryService
	logger  *slog.Logger
}

// NewEntitlementHandler creates a new entitlement handler.
func NewEntitlementHandler(service application.EntitlementQueryService, logger *slog.Logger) *EntitlementHandler {
	return &EntitlementHandler{
		service: service,
		logger:  logger,
	}
}

// HandleGetEntitlement retrieves the current entitlement for a user.
func (h *EntitlementHandler) HandleGetEntitlement(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID := chi.URLParam(r, "userId")
	if userID == "" {
		h.respondError(w, http.StatusBadRequest, "MISSING_USER_ID", "User ID is required")
		return
	}

	// Query entitlement
	entitlement, err := h.service.GetCanonicalEntitlement(ctx, userID)
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to query entitlement", "user_id", userID, "err", err)
		h.respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to query entitlement")
		return
	}

	// Return entitlement
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(entitlement)

	h.logger.InfoContext(ctx, "entitlement queried", "user_id", userID, "source", entitlement.Source, "active", entitlement.Active)
}

// respondError sends an error response.
func (h *EntitlementHandler) respondError(w http.ResponseWriter, statusCode int, code string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"error": message,
		"code":  code,
	}

	_ = json.NewEncoder(w).Encode(response)
}
