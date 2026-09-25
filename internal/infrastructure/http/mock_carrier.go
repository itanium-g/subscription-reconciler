package http

import (
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/example/subscription-reconciler/internal/domain"
)

// MockCarrierHandler handles mock carrier API requests.
// It simulates a real carrier API with randomised responses:
//   - 85% active
//   - 10% inactive
//   - 5% api_error (returned as HTTP 500)
type MockCarrierHandler struct {
	logger *slog.Logger
}

// NewMockCarrierHandler creates a new mock carrier handler.
func NewMockCarrierHandler(logger *slog.Logger) *MockCarrierHandler {
	return &MockCarrierHandler{logger: logger}
}

// HandleGetPlan returns a randomised mock carrier plan status.
// GET /mock/carrier/plan?userId=<id>
func (h *MockCarrierHandler) HandleGetPlan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID := r.URL.Query().Get("userId")
	if userID == "" {
		respondError(w, http.StatusBadRequest, "MISSING_USER_ID", "userId query parameter is required")
		return
	}

	status := mockCarrierStatus(userID)

	w.Header().Set("Content-Type", "application/json")

	// api_error is surfaced as HTTP 500 so the HTTP client maps it correctly.
	if status == domain.CarrierStatusAPIError {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "api_error"})
		h.logger.InfoContext(ctx, "mock carrier api error", "user_id", userID)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(domain.MockCarrierResponse{Status: string(status)})
	h.logger.InfoContext(ctx, "mock carrier plan queried", "user_id", userID, "status", status)
}

// mockCarrierStatus simulates a carrier API response.
// The seed combines the user ID hash with a per-minute bucket so the same
// user gets consistent results within a polling cycle but varies over time.
func mockCarrierStatus(userID string) domain.CarrierPlanStatus {
	seed := int64(hashUserID(userID)) + time.Now().Unix()/60
	r := rand.New(rand.NewSource(seed))
	roll := r.Intn(100)
	switch {
	case roll < 85:
		return domain.CarrierStatusActive
	case roll < 95:
		return domain.CarrierStatusInactive
	default:
		return domain.CarrierStatusAPIError
	}
}

// hashUserID returns a simple hash of the user ID string.
func hashUserID(userID string) uint64 {
	h := uint64(5381)
	for _, c := range userID {
		h = ((h << 5) + h) + uint64(c)
	}
	return h
}
