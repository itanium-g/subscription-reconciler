# Build stage
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build API
FROM builder AS api-builder
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o api ./cmd/api

# Build Worker
FROM builder AS worker-builder
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o worker ./cmd/worker

# API runtime
FROM alpine:latest AS api
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=api-builder /app/api .
EXPOSE 8080
CMD ["./api"]

# Worker runtime
FROM alpine:latest AS worker
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=worker-builder /app/worker .
CMD ["./worker"]
