# ========================================
# YiMao · 云海求片助手
# Telegram 影视求片助手
# ========================================
# 多阶段构建：Go 1.24 → Alpine
# 搜索求片为默认路径，电影冒险为可选玩法
# ========================================
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

RUN apk add --no-cache ca-certificates tzdata shadow su-exec docker-cli

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/yimao .

# Create data directory
RUN mkdir -p /app/data

# Set timezone
ENV TZ=Asia/Shanghai

# PUID/PGID 动态适配：容器启动时根据 .env 传入的 PUID/PGID 创建用户
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
  CMD wget -q --spider http://localhost:${PORT:-8080}/health || exit 1

# Run via entrypoint (handles PUID/PGID)
ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["./yimao"]
