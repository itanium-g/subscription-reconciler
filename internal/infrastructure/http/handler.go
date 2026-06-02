package http

import "net/http"

// Handler represents HTTP handlers for the API.
type Handler struct {
	// TODO: Inject services and repositories
}

// NewHandler creates a new HTTP handler.
func NewHandler() *Handler {
	return &Handler{}
}

// RegisterRoutes registers all HTTP routes.
func (h *Handler) RegisterRoutes() http.Handler {
	// TODO: Set up chi router and register routes
	// POST /webhooks/store
	// POST /webhooks/marketplace/revoke
	// GET /users/:id/entitlement
	// GET /users/:id/timeline (stretch)

	return nil
}
