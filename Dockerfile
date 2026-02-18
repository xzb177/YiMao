FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download || true

COPY main.go ./
RUN CGO_ENABLED=0 go build -o emby-telegram-bot .

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Set timezone (change to your preference)
ENV TZ=Asia/Shanghai

COPY --from=builder /app/emby-telegram-bot .

EXPOSE 8080

CMD ["./emby-telegram-bot"]
