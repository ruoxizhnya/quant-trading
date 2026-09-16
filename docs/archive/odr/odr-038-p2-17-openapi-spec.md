# ODR-038: P2-17 OpenAPI 3.0 spec + Swagger UI

> **Status**: Completed
> **Date**: 2026-06-14
> **Category**: Implementation
> **Related ADRs**: None
> **Supersedes**: None

## Context
P2-17 要求实现 OpenAPI 3.0 规范自动生成和 Swagger UI 端点，为 API 消费者提供交互式文档。

## Decision
1. **OpenAPI 3.0 YAML**: 手写 `docs/openapi.yaml`，覆盖核心端点
2. **Swagger UI**: `/api/docs` 端点提供交互式文档
3. **embed 静态文件**: 使用 Go embed 嵌入 Swagger UI 静态资源
4. **Discovery 端点**: `/api/version` 返回 API 版本信息

## Consequences
- **Positive**: API 消费者可自助查看和测试 API
- **Positive**: OpenAPI spec 可用于生成客户端 SDK
- **Negative**: spec 需要手动维护与代码同步

## Artifacts
- `docs/openapi.yaml` (新建)
- `docs/embed.go` (新建)
- `cmd/analysis/handlers_openapi.go` (新建)
- `cmd/analysis/handlers_openapi_test.go` (新建, 5 TestXxx)

## Metrics
- 测试数: 5 TestXxx
- 覆盖端点: /api/docs, /api/version

## Lessons Learned
- 使用 Go embed 避免了外部文件依赖
- OpenAPI spec 手写比自动生成更可控，但需要纪律性维护
