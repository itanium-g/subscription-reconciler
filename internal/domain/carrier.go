package domain

// CarrierPlanStatus represents the carrier plan status.
type CarrierPlanStatus string

const (
	CarrierStatusActive   CarrierPlanStatus = "active"
	CarrierStatusInactive CarrierPlanStatus = "inactive"
	CarrierStatusAPIError CarrierPlanStatus = "api_error"
)

// MockCarrierResponse represents the mock carrier API response.
type MockCarrierResponse struct {
	Status string `json:"status"`
}

// CarrierClient is the port the application layer uses to query carrier status.
// In production this is an HTTP client; in tests it can be a stub.
type CarrierClient interface {
	GetPlanStatus(userID string) (CarrierPlanStatus, error)
}
