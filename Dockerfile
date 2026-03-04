# Multi-stage build for Emby Telegram Bot (Enterprise Edition)
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the new binary
RUN CGO_ENABLED=0 GOOS=linux go build -o emby-telegram-bot ./cmd/bot

# Final stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/emby-telegram-bot .

# Create data directory
RUN mkdir -p /app/data

# Set timezone
ENV TZ=Asia/Shanghai

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
  CMD wget -q --spider http://localhost:${PORT:-8080}/health || exit 1

# Run the application
CMD ["./emby-telegram-bot"]
