package domain

// MarketplaceRevokeRequest represents a marketplace revoke request.
type MarketplaceRevokeRequest struct {
	UserIDs []string `json:"userIds"`
}

// MarketplaceRevokeResponse is returned after processing a revoke request.
type MarketplaceRevokeResponse struct {
	Accepted bool   `json:"accepted"`
	Count    int32  `json:"count"`
	Message  string `json:"message"`
}

// ValidateMarketplaceRevokeRequest validates the request.
func (r *MarketplaceRevokeRequest) Validate() error {
	if len(r.UserIDs) == 0 {
		return ErrEmptyUserIDList
	}
	if len(r.UserIDs) > 10000 {
		return ErrTooManyUserIDs
	}
	return nil
}

var (
	ErrEmptyUserIDList = DomainError{Code: "EMPTY_USER_ID_LIST", Message: "userIds list cannot be empty"}
	ErrTooManyUserIDs  = DomainError{Code: "TOO_MANY_USER_IDS", Message: "userIds list cannot exceed 10000 items"}
)
