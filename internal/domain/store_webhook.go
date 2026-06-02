package domain

import "time"

// StoreWebhookPayload represents an incoming store webhook.
type StoreWebhookPayload struct {
	EventID     string `json:"eventId"`
	UserID      string `json:"userId"`
	Type        string `json:"type"`
	EventTimeMs int64  `json:"eventTimeMs"`
	ProductID   string `json:"productId"`
}

// StoreWebhookResponse is returned after processing a webhook.
type StoreWebhookResponse struct {
	EventID     string `json:"eventId"`
	UserID      string `json:"userId"`
	Accepted    bool   `json:"accepted"`
	IsDuplicate bool   `json:"isDuplicate"`
	Message     string `json:"message"`
}

// ReconciliationResult represents the outcome of processing an event.
type ReconciliationResult struct {
	EventProcessed bool
	WasProcessed   bool
	StateChanged   bool
	Active         bool
	ExpiresAt      *time.Time
	Reason         string
}

// ValidateStoreWebhook validates incoming webhook payload.
func (p *StoreWebhookPayload) Validate() error {
	if p.EventID == "" {
		return ErrMissingEventID
	}
	if p.UserID == "" {
		return ErrMissingUserID
	}
	if !isValidEventType(p.Type) {
		return ErrInvalidEventType
	}
	if p.EventTimeMs <= 0 {
		return ErrInvalidEventTime
	}
	if p.ProductID == "" {
		return ErrMissingProductID
	}
	return nil
}

func isValidEventType(t string) bool {
	switch t {
	case "INITIAL_PURCHASE", "RENEWAL", "CANCELLATION", "BILLING_ISSUE", "EXPIRATION", "UN_CANCELLATION":
		return true
	}
	return false
}

// DomainError types for validation
var (
	ErrMissingEventID   = DomainError{Code: "MISSING_EVENT_ID", Message: "eventId is required"}
	ErrMissingUserID    = DomainError{Code: "MISSING_USER_ID", Message: "userId is required"}
	ErrInvalidEventType = DomainError{Code: "INVALID_EVENT_TYPE", Message: "type is not a valid event type"}
	ErrInvalidEventTime = DomainError{Code: "INVALID_EVENT_TIME", Message: "eventTimeMs must be greater than 0"}
	ErrMissingProductID = DomainError{Code: "MISSING_PRODUCT_ID", Message: "productId is required"}
)

type DomainError struct {
	Code    string
	Message string
}

func (e DomainError) Error() string {
	return e.Message
}
