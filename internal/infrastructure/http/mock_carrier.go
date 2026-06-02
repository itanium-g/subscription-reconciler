package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/example/adora/internal/application"
	"github.com/example/adora/internal/domain"
)

// MockCarrierHandler handles mock carrier API requests.
type MockCarrierHandler struct {
	service application.CarrierPollingService
	logger  *slog.Logger
}

// NewMockCarrierHandler creates a new mock carrier handler.
func NewMockCarrierHandler(service application.CarrierPollingService, logger *slog.Logger) *MockCarrierHandler {
	return &MockCarrierHandler{
		service: service,
		logger:  logger,
	}
}

// HandleGetPlan returns a mock carrier plan status.
func (h *MockCarrierHandler) HandleGetPlan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID := r.URL.Query().Get("userId")
	if userID == "" {
		h.respondError(w, http.StatusBadRequest, "MISSING_USER_ID", "userId query parameter is required")
		return
	}

	// Get mock carrier status
	status := h.service.MockCarrierStatus(userID)

	// Return response
	w.Header().Set("Content-Type", "application/json")

	// If API error, return 500
	if status == domain.CarrierStatusAPIError {
		w.WriteHeader(http.StatusInternalServerError)
		response := map[string]interface{}{
			"error": "api_error",
		}
		_ = json.NewEncoder(w).Encode(response)
		h.logger.InfoContext(ctx, "mock carrier api error", "user_id", userID)
		return
	}

	// Return success
	w.WriteHeader(http.StatusOK)
	response := domain.MockCarrierResponse{
		Status: string(status),
	}
	_ = json.NewEncoder(w).Encode(response)

	h.logger.InfoContext(ctx, "mock carrier plan queried", "user_id", userID, "status", status)
}

// respondError sends an error response.
func (h *MockCarrierHandler) respondError(w http.ResponseWriter, statusCode int, code string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"error": message,
		"code":  code,
	}

	_ = json.NewEncoder(w).Encode(response)
}
