package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/caarlos0/env/v11"
	"github.com/example/subscription-reconciler/internal/config"
	"github.com/example/subscription-reconciler/internal/infrastructure/carrier"
	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
	"github.com/example/subscription-reconciler/internal/infrastructure/worker"
)

type Config struct {
	DatabaseURL   string `env:"DATABASE_URL"    envDefault:"postgres://postgres:postgres@localhost:5432/subscription_reconciler?sslmode=disable"`
	LogLevel      string `env:"LOG_LEVEL"       envDefault:"info"`
	CarrierAPIURL string `env:"CARRIER_API_URL" envDefault:"http://localhost:8080"`
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

	logger.InfoContext(ctx, "starting worker", "database", cfg.DatabaseURL)

	// Initialize database connection
	db, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	logger.InfoContext(ctx, "connected to database")

	// Create workers
	carrierClient := carrier.NewHTTPClient(cfg.CarrierAPIURL)
	pollingWorker := worker.NewPollingWorker(db, carrierClient, logger)
	notificationWorker := worker.NewNotificationWorker(db, logger)

	// Run workers concurrently
	go pollingWorker.StartCarrierPolling(ctx)
	go notificationWorker.StartNotificationSending(ctx)

	// Wait for context cancellation
	<-ctx.Done()

	logger.InfoContext(ctx, "worker stopped")
}


