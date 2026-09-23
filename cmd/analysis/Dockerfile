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
# ⚠️ 此处曾有一行 `COPY --from=builder /app/cmd/analysis/static ./static/`。
# AUD-33（2026-09-22）退役 legacy HTML 时把那个目录整删了，这行没跟着删 →
# `docker build -f cmd/analysis/Dockerfile`（Makefile 的镜像目标）会在这一步炸
# `"/cmd/analysis/static": not found`。与根 Dockerfile.service 里那处完全同型。
# analysis 现在只提供 API，不需要静态目录。
COPY config/ ./config/

EXPOSE 8085

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8085/api/health || exit 1

USER nobody

ENTRYPOINT ["./analysis-service"]
