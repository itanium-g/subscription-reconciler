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
