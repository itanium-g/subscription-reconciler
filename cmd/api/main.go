package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/example/subscription-reconciler/internal/config"
	infrahttp "github.com/example/subscription-reconciler/internal/infrastructure/http"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Config struct {
	Port            string `env:"PORT" envDefault:"8080"`
	DatabaseURL     string `env:"DATABASE_URL" envDefault:"postgres://postgres:postgres@localhost:5432/subscription_reconciler?sslmode=disable"`
	LogLevel        string `env:"LOG_LEVEL" envDefault:"info"`
	MockCarrierMode bool   `env:"MOCK_CARRIER_MODE" envDefault:"false"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Load configuration
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		panic(err)
	}

	// Logger setup
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: config.ParseLevelFromString(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	logger.InfoContext(ctx, "starting API server", "port", cfg.Port, "database", cfg.DatabaseURL)

	// Set up HTTP router
	router := chi.NewRouter()

	// Health check endpoint
	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Middleware
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.RequestID)

	// Handle Mock Carrier Mode
	if cfg.MockCarrierMode {
		logger.InfoContext(ctx, "running in mock carrier mode")
		mockHandler := infrahttp.NewMockCarrierHandler(logger)
		router.Get("/mock/carrier/plan", mockHandler.HandleGetPlan)
	} else {
		// Normal API mode requires DB
		db, err := postgres.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			logger.ErrorContext(ctx, "failed to connect to database", "err", err)
			os.Exit(1)
		}
		defer db.Close()
		logger.InfoContext(ctx, "connected to database")

		// Mount API routes
		apiRouter := infrahttp.NewRouter(db, logger)
		apiRouter.Mount(router)
	}

	// Create HTTP server
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.InfoContext(ctx, "API server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.ErrorContext(ctx, "server error", "err", err)
		}
	}()

	// Wait for interrupt signal
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	logger.InfoContext(shutdownCtx, "shutting down server")
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.ErrorContext(shutdownCtx, "shutdown error", "err", err)
	}

	logger.InfoContext(shutdownCtx, "server stopped")
}
