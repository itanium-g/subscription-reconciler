package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/example/subscription-reconciler/internal/application"
	"github.com/example/subscription-reconciler/internal/domain"
)

// StoreWebhookHandler handles incoming store webhooks.
type StoreWebhookHandler struct {
	service application.StoreWebhookService
	logger  *slog.Logger
}

// NewStoreWebhookHandler creates a new store webhook handler.
func NewStoreWebhookHandler(service application.StoreWebhookService, logger *slog.Logger) *StoreWebhookHandler {
	return &StoreWebhookHandler{
		service: service,
		logger:  logger,
	}
}

// HandleStoreWebhook processes a store webhook.
func (h *StoreWebhookHandler) HandleStoreWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read and parse request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body")
		return
	}
	defer r.Body.Close()

	var payload domain.StoreWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON: "+err.Error())
		return
	}

	// Process webhook
	result, err := h.service.ProcessStoreWebhook(ctx, &payload)
	if err != nil {
		// Check if it's a domain validation error
		var domainErr domain.DomainError
		if errors.As(err, &domainErr) {
			h.logger.InfoContext(ctx, "validation error", "event_id", payload.EventID, "code", domainErr.Code)
			respondError(w, http.StatusBadRequest, domainErr.Code, domainErr.Message)
			return
		}

		// Database or other error
		h.logger.ErrorContext(ctx, "failed to process webhook", "event_id", payload.EventID, "err", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process webhook")
		return
	}

	// Return 202 Accepted (async processing)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(result)

	h.logger.InfoContext(ctx, "webhook processed",
		"event_id", payload.EventID,
		"user_id", payload.UserID,
		"type", payload.Type,
		"duplicate", result.IsDuplicate,
	)
}
