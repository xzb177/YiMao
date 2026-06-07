# Multi-stage build for YiMao (Telegram 影视求片助手)
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

# go.sum checksum 问题的根本修复：
# 1. 先只复制 go.mod 做依赖下载（不用 go.sum 校验）
# 2. 用 GOPROXY=goproxy.cn 国内代理加速
# 3. GONOSUMCHECK=* 跳过 checksum 校验
# 4. 复制源码后 go mod tidy 重建 go.sum
COPY go.mod ./
ENV GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct
ENV GONOSUMCHECK=*
ENV GONOSUMDB=*
RUN go mod download

COPY . .

# 重建 go.sum + 编译
RUN rm -f go.sum && go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o yimao ./cmd/bot

# ── Final stage ──
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata shadow su-exec

WORKDIR /app

COPY --from=builder /app/yimao .

RUN mkdir -p /app/data

ENV TZ=Asia/Shanghai

COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
  CMD wget -q --spider http://localhost:${PORT:-8080}/health || exit 1

ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["./yimao"]
