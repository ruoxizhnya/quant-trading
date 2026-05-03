# ============================================================
# Analysis Service (API Gateway) - :8085
# Role: Main API gateway, backtest engine, strategy orchestration
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
    -o analysis-service ./cmd/analysis

FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache ca-certificates curl tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    rm -rf /var/cache/apk/*

COPY --from=builder /app/analysis-service .
COPY --from=builder /app/cmd/analysis/static ./static/
COPY config/ ./config/

EXPOSE 8085

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8085/api/health || exit 1

USER nobody

ENTRYPOINT ["./analysis-service"]
