# ============================================================
# Risk Service - :8084
# Role: Risk management, position sizing, stop-loss monitoring
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
    -o risk-service ./cmd/risk

FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache ca-certificates curl tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    rm -rf /var/cache/apk/*

COPY --from=builder /app/risk-service .
COPY config/ ./config/

EXPOSE 8084

HEALTHCHECK --interval=20s --timeout=2s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8084/risk/health || exit 1

USER nobody

ENTRYPOINT ["./risk-service"]
