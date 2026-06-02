package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Logger setup
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// TODO: Load configuration from environment
	// TODO: Initialize database connection
	// TODO: Set up background jobs (carrier polling, notification sender)
	// TODO: Start job runner

	logger.InfoContext(ctx, "starting worker")

	// Placeholder: just wait for signal
	<-ctx.Done()

	logger.InfoContext(ctx, "worker stopped")
}
