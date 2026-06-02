package http

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/example/adora/internal/application"
	"github.com/example/adora/internal/infrastructure/postgres"
)

// Router sets up all HTTP routes.
type Router struct {
	storeWebhookHandler      *StoreWebhookHandler
	entitlementHandler       *EntitlementHandler
	marketplaceRevokeHandler *MarketplaceRevokeHandler
	mockCarrierHandler       *MockCarrierHandler
	timelineHandler          *TimelineHandler
}

// NewRouter creates a new router.
func NewRouter(db postgres.Database, logger *slog.Logger) *Router {
	storeService := application.NewStoreWebhookService(db)
	storeHandler := NewStoreWebhookHandler(storeService, logger)

	entitlementService := application.NewEntitlementQueryService(db)
	entitlementHandler := NewEntitlementHandler(entitlementService, logger)

	marketplaceService := application.NewMarketplaceRevokeService(db)
	marketplaceHandler := NewMarketplaceRevokeHandler(marketplaceService, logger)

	carrierService := application.NewCarrierPollingService(db)
	mockCarrierHandler := NewMockCarrierHandler(carrierService, logger)

	timelineService := application.NewTimelineService(db)
	timelineHandler := NewTimelineHandler(timelineService, logger)

	return &Router{
		storeWebhookHandler:      storeHandler,
		entitlementHandler:       entitlementHandler,
		marketplaceRevokeHandler: marketplaceHandler,
		mockCarrierHandler:       mockCarrierHandler,
		timelineHandler:          timelineHandler,
	}
}

// Mount mounts all routes on the chi router.
func (r *Router) Mount(router chi.Router) {
	router.Post("/webhooks/store", r.storeWebhookHandler.HandleStoreWebhook)
	router.Post("/webhooks/marketplace/revoke", r.marketplaceRevokeHandler.HandleMarketplaceRevoke)
	router.Get("/users/{userId}/entitlement", r.entitlementHandler.HandleGetEntitlement)
	router.Get("/users/{userId}/timeline", r.timelineHandler.HandleGetTimeline)
	router.Get("/mock/carrier/plan", r.mockCarrierHandler.HandleGetPlan)
}
