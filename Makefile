.PHONY: help build lint test run-api run-worker docker-build docker-compose-up docker-compose-down clean

help:
	@echo "Available targets:"
	@echo "  build              - Build API and worker binaries"
	@echo "  lint               - Run golangci-lint"
	@echo "  fmt                - Run gofumpt to format code"
	@echo "  test               - Run tests with coverage"
	@echo "  run-api            - Run API server locally (requires DB)"
	@echo "  run-worker         - Run worker locally (requires DB)"
	@echo "  docker-build       - Build Docker images"
	@echo "  docker-compose-up  - Start services with docker-compose"
	@echo "  docker-compose-down - Stop services"
	@echo "  clean              - Remove build artifacts"

build:
	@echo "Building API..."
	@cd cmd/api && go build -o ../../bin/api
	@echo "Building worker..."
	@cd cmd/worker && go build -o ../../bin/worker
	@echo "Build complete: bin/api bin/worker"

lint:
	@echo "Running golangci-lint..."
	@golangci-lint run ./...
	@echo "Linting complete"

fmt:
	@echo "Formatting code..."
	@gofumpt -w ./cmd ./internal ./tests
	@echo "Format complete"

test:
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out

run-api:
	@echo "Starting API server..."
	@go run ./cmd/api

run-worker:
	@echo "Starting worker..."
	@go run ./cmd/worker

docker-build:
	@echo "Building Docker images..."
	@docker compose build
	@echo "Build complete"

docker-compose-up:
	@echo "Starting services..."
	@docker compose up -d
	@echo "Services started. Check with 'docker compose ps'"

docker-compose-down:
	@echo "Stopping services..."
	@docker compose down
	@echo "Services stopped"

docker-compose-logs:
	@docker compose logs -f

clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@rm -f coverage.out
	@echo "Clean complete"
