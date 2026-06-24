# ODR-039: P2-18~P2-26 测试质量提升

> **Status**: Completed
> **Date**: 2026-06-14
> **Category**: Implementation
> **Related ADRs**: None
> **Supersedes**: None

## Context
P2-18 到 P2-26 要求全面提升测试覆盖率，覆盖数据源集成、AI 基因池持久化、风控边界、Pipeline 端到端、领域类型、HTTP 客户端、日志脱敏、LLM 客户端和 E2E 视觉回归。

## Decision
创建 9 个测试文件，覆盖以下领域：

1. **P2-18**: `pkg/data/source/integration_test.go` — 9 个 adapter httptest mock 测试
2. **P2-19**: `pkg/ai/gene_pool/integration_test.go` — FactorPool/StrategyPool save/load 持久化测试
3. **P2-20**: `pkg/risk/boundary_test.go` — stoploss/regime/volatility 边界 + 零值/负值/极值
4. **P2-21**: `pkg/ai/pipeline/e2e_test.go` — Intent → YAML → Code → Compile → Backtest mock 全流程
5. **P2-22**: `pkg/domain/types_test.go` — OHLCV/Portfolio/Signal zero value + JSON 序列化
6. **P2-23**: `pkg/httpclient/client_test.go` — timeout/retry/backoff + httptest mock
7. **P2-24**: `pkg/logging/masking.go` + `masking_test.go` — MaskAPIKey/MaskAccountNumber/MaskMap
8. **P2-25**: `pkg/ai/client_test.go` — MockClient 全方法 + Client options
9. **P2-26**: `e2e/tests/visual-regression.spec.ts` — 12 个截图 baseline

## Consequences
- **Positive**: 测试覆盖率全面提升
- **Positive**: 日志脱敏功能增强了安全性
- **Negative**: 测试维护成本增加

## Artifacts
- `pkg/data/source/integration_test.go` (新建)
- `pkg/ai/gene_pool/integration_test.go` (新建)
- `pkg/risk/boundary_test.go` (新建)
- `pkg/ai/pipeline/e2e_test.go` (新建)
- `pkg/domain/types_test.go` (新建)
- `pkg/httpclient/client_test.go` (新建)
- `pkg/logging/masking.go` + `masking_test.go` (新建)
- `pkg/ai/client_test.go` (扩展)
- `e2e/tests/visual-regression.spec.ts` (新建)

## Metrics
- 新增测试文件: 9 个
- 新增 TestXxx: ~200+
- 新增功能: 日志脱敏 (42 TestXxx)

## Lessons Learned
- 边界测试应覆盖零值、负值、极值和空切片
- httptest mock 是测试 HTTP 客户端的最佳方式
- 视觉回归测试需要稳定的 baseline，首次运行生成
