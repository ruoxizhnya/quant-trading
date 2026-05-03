# ============================================================
# Strategy Service - :8082
# Role: Strategy plugin loader, strategy registry, hot-swap
# ============================================================

FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

ARG BUILD_VERSION=dev
ARG BUILD_COMMIT=none
ARG BUILD_TIME=unknown
ENV CGO_ENABLED=0 GOOS=linux
RUN go build \
    -ldflags "-w -s -X main.Version=${BUILD_VERSION} -X main.Commit=${BUILD_COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o strategy-service ./cmd/strategy

FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache ca-certificates curl tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    rm -rf /var/cache/apk/*

COPY --from=builder /app/strategy-service .
COPY config/ ./config/
COPY cmd/strategy/generated_strategies/ ./generated_strategies/

EXPOSE 8082

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8082/strategies || exit 1

USER nobody

ENTRYPOINT ["./strategy-service"]
