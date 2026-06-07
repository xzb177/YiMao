FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./

# 用国内代理 + 跳过校验（go.sum checksum 问题）
ENV GOPROXY=goproxy.cn
ENV GONOSUMCHECK=*
ENV GONOSUMDB=*
ENV GOFLAGS=-mod=mod

RUN go mod download

COPY . .

RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o yimao ./cmd/bot

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
