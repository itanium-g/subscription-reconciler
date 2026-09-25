package carrier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/example/subscription-reconciler/internal/domain"
)

// HTTPClient calls the real (or mock) carrier HTTP endpoint.
// It implements domain.CarrierClient.
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewHTTPClient creates a carrier HTTP client pointed at baseURL.
// Example baseURL: "http://localhost:8080"
func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// GetPlanStatus calls GET /mock/carrier/plan?userId=<userID> and returns the
// carrier plan status. A non-200 response is treated as api_error so the
// caller preserves the existing entitlement rather than revoking access.
func (c *HTTPClient) GetPlanStatus(ctx context.Context, userID string) (domain.CarrierPlanStatus, error) {
	endpoint, err := url.Parse(c.baseURL)
	if err != nil {
		return domain.CarrierStatusAPIError, fmt.Errorf("carrier: parse base URL: %w", err)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/mock/carrier/plan"
	query := endpoint.Query()
	query.Set("userId", userID)
	endpoint.RawQuery = query.Encode()
	requestURL := endpoint.String()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return domain.CarrierStatusAPIError, fmt.Errorf("carrier: create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network failure: treat as api_error
		return domain.CarrierStatusAPIError, fmt.Errorf("carrier: GET %s: %w", requestURL, err)
	}
	defer resp.Body.Close()

	// Non-200 status (e.g. the mock returns 500 for api_error)
	if resp.StatusCode != http.StatusOK {
		return domain.CarrierStatusAPIError, nil
	}

	var body domain.MockCarrierResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return domain.CarrierStatusAPIError, fmt.Errorf("carrier: decode response: %w", err)
	}

	switch domain.CarrierPlanStatus(body.Status) {
	case domain.CarrierStatusActive, domain.CarrierStatusInactive:
		return domain.CarrierPlanStatus(body.Status), nil
	default:
		// Unknown status: treat conservatively as api_error
		return domain.CarrierStatusAPIError, nil
	}
}
