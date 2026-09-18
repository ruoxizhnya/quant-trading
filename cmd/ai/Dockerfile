# ============================================================
# AI Research Service - :8086
# Role: LLM-driven strategy generation / research orchestration
#
# 之前只有 cmd/analysis | cmd/data | cmd/strategy 有 Dockerfile，
# 这个服务是 docker-compose 里唯一没有镜像的 —— 本地能跑、一上容器就缺一块。
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
    -o ai-service ./cmd/ai

FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache ca-certificates curl tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    rm -rf /var/cache/apk/*

COPY --from=builder /app/ai-service .
COPY config/ ./config/

EXPOSE 8086

# 注意是 /health 而不是 /api/health —— 这个服务没有 /api 前缀（见 cmd/ai/main.go:36）。
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8086/health || exit 1

USER nobody

# AI_API_KEY / AI_API_URL 走环境变量注入，不进镜像 ——
# 密钥打进层里等于公开。
ENTRYPOINT ["./ai-service"]
