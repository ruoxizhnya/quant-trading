# ============================================================
# Quant Trading - Service Build & Deploy Makefile
# Usage: make [target] VERSION=v1.0.0
# ============================================================

.PHONY: all build push clean test lint help

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "local")
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
REGISTRY ?= ghcr.io/ruoxizhnya

# Default target
all: build

# ============================================================
# Build targets - Individual services
# ============================================================
build-analysis:
	@echo "🔨 Building analysis-service:${VERSION}..."
	docker build \
		--build-arg BUILD_VERSION=${VERSION} \
		--build-arg BUILD_COMMIT=${COMMIT} \
		--build-arg BUILD_TIME=${BUILD_TIME} \
		-t ${REGISTRY}/quant-trading-analysis:${VERSION} \
		-t ${REGISTRY}/quant-trading-analysis:latest \
		-f cmd/analysis/Dockerfile .

build-data:
	@echo "🔨 Building data-service:${VERSION}..."
	docker build \
		--build-arg BUILD_VERSION=${VERSION} \
		--build-arg BUILD_COMMIT=${COMMIT} \
		--build-arg BUILD_TIME=${BUILD_TIME} \
		-t ${REGISTRY}/quant-trading-data:${VERSION} \
		-t ${REGISTRY}/quant-trading-data:latest \
		-f cmd/data/Dockerfile .

build-strategy:
	@echo "🔨 Building strategy-service:${VERSION}..."
	docker build \
		--build-arg BUILD_VERSION=${VERSION} \
		--build-arg BUILD_COMMIT=${COMMIT} \
		--build-arg BUILD_TIME=${BUILD_TIME} \
		-t ${REGISTRY}/quant-trading-strategy:${VERSION} \
		-t ${REGISTRY}/quant-trading-strategy:latest \
		-f cmd/strategy/Dockerfile .

build-execution:
	@echo "🔨 Building execution-service:${VERSION}..."
	docker build \
		--build-arg BUILD_VERSION=${VERSION} \
		--build-arg BUILD_COMMIT=${COMMIT} \
		--build-arg BUILD_TIME=${BUILD_TIME} \
		-t ${REGISTRY}/quant-trading-execution:${VERSION} \
		-t ${REGISTRY}/quant-trading-execution:latest \
		-f cmd/execution/Dockerfile .

build-risk:
	@echo "🔨 Building risk-service:${VERSION}..."
	docker build \
		--build-arg BUILD_VERSION=${VERSION} \
		--build-arg BUILD_COMMIT=${COMMIT} \
		--build-arg BUILD_TIME=${BUILD_TIME} \
		-t ${REGISTRY}/quant-trading-risk:${VERSION} \
		-t ${REGISTRY}/quant-trading-risk:latest \
		-f cmd/risk/Dockerfile .

# Build all services
build: build-analysis build-data build-strategy build-execution build-risk
	@echo ""
	@echo "✅ All services built successfully!"
	@echo "   Version: ${VERSION}"
	@echo "   Commit:  ${COMMIT}"

# ============================================================
# Push targets
# ============================================================
push-analysis: build-analysis
	docker push ${REGISTRY}/quant-trading-analysis:${VERSION}
	docker push ${REGISTRY}/quant-trading-analysis:latest

push-data: build-data
	docker push ${REGISTRY}/quant-trading-data:${VERSION}
	docker push ${REGISTRY}/quant-trading-data:latest

push-strategy: build-strategy
	docker push ${REGISTRY}/quant-trading-strategy:${VERSION}
	docker push ${REGISTRY}/quant-trading-strategy:latest

push-execution: build-execution
	docker push ${REGISTRY}/quant-trading-execution:${VERSION}
	docker push ${REGISTRY}/quant-trading-execution:latest

push-risk: build-risk
	docker push ${REGISTRY}/quant-trading-risk:${VERSION}
	docker push ${REGISTRY}/quant-trading-risk:latest

push: push-analysis push-data push-strategy push-execution push-risk

# ============================================================
# Docker Compose targets
# ============================================================
up:
	docker compose -f docker-compose.services.yml up -d

down:
	docker compose -f docker-compose.services.yml down

restart:
	docker compose -f docker-compose.services.yml restart

logs:
	docker compose -f docker-compose.services.yml logs -f

ps:
	docker compose -f docker-compose.services.yml ps

# ============================================================
# Utility targets
# ============================================================
clean:
	@echo "🧹 Cleaning up Docker resources..."
	docker system prune -f
	@echo "✅ Cleanup complete"

test:
	@echo "🧪 Running tests..."
	go test ./... -v -coverprofile=coverage.out
	@echo "✅ Tests completed"

lint:
	@echo "🔍 Running linter..."
	golangci-lint run ./...
	@echo "✅ Linting complete"

help:
	@echo ""
	@echo "📦 Quant Trading - Build & Deploy System"
	@echo "=========================================="
	@echo ""
	@echo "Usage: make [target] [VARIABLE=value]"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION      Image version tag (default: git describe or 'dev')"
	@echo "  COMMIT       Git commit hash (default: short SHA)"
	@echo "  BUILD_TIME   Build timestamp (default: current UTC time)"
	@echo "  REGISTRY     Container registry (default: ghcr.io/ruoxizhnya)"
	@echo ""
	@echo "Build Targets:"
	@echo "  build              Build all services"
	@echo "  build-analysis     Build analysis service only"
	@echo "  build-data         Build data service only"
	@echo "  build-strategy     Build strategy service only"
	@echo "  build-execution    Build execution service only"
	@echo "  build-risk         Build risk service only"
	@echo ""
	@echo "Push Targets:"
	@echo "  push               Push all services to registry"
	@echo "  push-analysis      Push analysis service only"
	@echo "  (and so on for each service)"
	@echo ""
	@echo "Docker Compose:"
	@echo "  up                 Start all services with docker-compose"
	@echo "  down               Stop and remove containers"
	@echo "  restart            Restart all services"
	@echo "  logs               View service logs"
	@echo "  ps                 Show service status"
	@echo ""
	@echo "Utilities:"
	@echo "  test               Run Go tests"
	@echo "  lint               Run golangci-lint"
	@echo "  clean              Clean up Docker resources"
	@echo "  help               Show this help message"
	@echo ""
