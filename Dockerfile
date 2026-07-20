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

# Explicit verification target for deployment preflight on Docker-only hosts.
# The production image remains lean, while `docker build --target verify .`
# runs the same source through vet and the full Go test suite first.
FROM builder AS verify
RUN apk add --no-cache font-noto-cjk
ENV YIMAO_CJK_FONT=/usr/share/fonts/noto/NotoSansCJK-Regular.ttc
RUN files=$(find . -type f -name '*.go' -not -path './vendor/*') && \
    test -z "$(gofmt -l $files)" && \
    go vet ./... && \
    go test ./...

# Staging smoke image; not included in the production runtime image.
FROM builder AS smoke
RUN CGO_ENABLED=0 GOOS=linux go build -o /smoke ./cmd/smoke
ENTRYPOINT ["/smoke"]

# Final stage
FROM alpine:latest

# Noto Sans CJK provides modern, high-quality Chinese typography for search cards.
RUN apk add --no-cache ca-certificates tzdata shadow su-exec docker-cli font-noto-cjk
ENV YIMAO_CJK_FONT=/usr/share/fonts/noto/NotoSansCJK-Regular.ttc

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
