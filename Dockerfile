# Multi-stage build for YiMao (Telegram 影视求片助手)
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Tidy dependencies and build
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o yimao ./cmd/bot

# Final stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -S yimao && adduser -S yimao -G yimao

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/yimao .

# Create data directory with proper ownership
RUN mkdir -p /app/data && chown -R yimao:yimao /app

# Set timezone
ENV TZ=Asia/Shanghai

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
  CMD wget -q --spider http://localhost:${PORT:-8080}/health || exit 1

# Run as non-root user
USER yimao

# Run the application
CMD ["./yimao"]
