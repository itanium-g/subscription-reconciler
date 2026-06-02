package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/example/adora/internal/application"
	"github.com/example/adora/internal/domain"
)

// MarketplaceRevokeHandler handles marketplace revoke requests.
type MarketplaceRevokeHandler struct {
	service application.MarketplaceRevokeService
	logger  *slog.Logger
}

// NewMarketplaceRevokeHandler creates a new marketplace revoke handler.
func NewMarketplaceRevokeHandler(service application.MarketplaceRevokeService, logger *slog.Logger) *MarketplaceRevokeHandler {
	return &MarketplaceRevokeHandler{
		service: service,
		logger:  logger,
	}
}

// HandleMarketplaceRevoke processes a marketplace revoke request.
func (h *MarketplaceRevokeHandler) HandleMarketplaceRevoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read and parse request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body")
		return
	}
	defer r.Body.Close()

	var request domain.MarketplaceRevokeRequest
	if err := json.Unmarshal(body, &request); err != nil {
		h.respondError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON: "+err.Error())
		return
	}

	// Process revoke
	result, err := h.service.RevokeMarketplaceAccess(ctx, &request)
	if err != nil {
		// Check if it's a domain validation error
		var domainErr domain.DomainError
		if errors.As(err, &domainErr) {
			h.logger.InfoContext(ctx, "validation error", "code", domainErr.Code)
			h.respondError(w, http.StatusBadRequest, domainErr.Code, domainErr.Message)
			return
		}

		// Database or other error
		h.logger.ErrorContext(ctx, "failed to process revoke", "err", err)
		h.respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process revoke")
		return
	}

	// Return 202 Accepted (async processing)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(result)

	h.logger.InfoContext(ctx, "marketplace revoke processed",
		"count", result.Count,
		"message", result.Message,
	)
}

// respondError sends an error response.
func (h *MarketplaceRevokeHandler) respondError(w http.ResponseWriter, statusCode int, code string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"error": message,
		"code":  code,
	}

	_ = json.NewEncoder(w).Encode(response)
}
