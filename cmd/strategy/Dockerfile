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
# ⚠️ 此处曾有一行 `COPY cmd/strategy/generated_strategies/ ./generated_strategies/`。
# 那个目录不存在，而且全仓（.go / yaml / Makefile）**零引用** —— 纯孤儿。
# 它让 `docker build -f cmd/strategy/Dockerfile`（Makefile 目标）在最后一步炸
# `not found`。与 AUD-33 删 cmd/analysis/static 后两处 Dockerfile 没跟着改同型
# （AUD-48）：删目录时 COPY 不会有任何编译期告警，只有真 build 才炸。
# 需要产物目录时应在运行时 mkdir，而不是 COPY 一个仓库里没有的目录。

EXPOSE 8082

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8082/strategies || exit 1

USER nobody

ENTRYPOINT ["./strategy-service"]
