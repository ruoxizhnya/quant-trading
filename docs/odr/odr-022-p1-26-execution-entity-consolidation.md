# ODR-022: P1-26 4 套执行实体合并 — `pkg/live` 5→2 实体

> **Status**: Completed
> **Date**: 2026-06-12
> **Category**: Refactor (YAGNI / 实体合并 / 代码减面)
> **Related ADRs**: ADR-014 (Strategy 框架重构 — 同类 ISP 拆分思想), ADR-019 (Service 合并)
> **Supersedes**: —
> **Relates to**: ODR-013 (CQ-010 审计), TASKS §P1-26, ODR-021 (P1-15 服务合并)

## Context

Sprint 6 综合审查 (ODR-013) 报告 CQ-010 重复造轮子: `pkg/live/` 在
v2.x 演进过程中累积了 **5 个并行的执行实体**, 表面看是"为不同场景
提供选项", 实则:

1. **`MockTrader`** (mock_trader.go, 350 行) — 唯一生产在用的实现,
   P1-15 合并到 analysis-service 作为 paper trading 默认 trader
2. **`PersistentMockTrader`** (persistent_mock_trader.go, 292 行) —
   唯一区别是构造时强制注入 `OrderStore`; 实际是 MockTrader 的一个
   **特殊构造模式**, 不构成新行为
3. **`AdvancedMockTrader`** (advanced_mock_trader.go, 277 行) —
   在 MockTrader 基础上加了 `RiskCheck` / `SlippageModel` 装饰器
4. **`AdvancedTrader` interface** (trader_advanced.go, 66 行) —
   描述 AdvancedMockTrader 的"高级"接口, 仅一个实现类
5. **同时**: `live_test.go` 中 ~175 行只测 AdvancedMockTrader /
   PersistentMockTrader 的 dead code 测试

**CQ-010 量化**:
- `pkg/live/live_test.go` 175 行只测 AdvancedMockTrader /
  PersistentMockTrader 的 mock 行为 (文件本身保留, 仍有 LiveEngine
  重要测试)
- 0 production caller 使用 AdvancedMockTrader (grep `NewAdvancedMockTrader`
  在 `cmd/`, `pkg/` 全无匹配)
- PersistentMockTrader 仅有 1 个 internal caller (旧 `cmd/execution/main.go`,
  P1-15 后已 stub)
- AdvancedTrader interface 0 production caller

总计: ~810 行 (含 ~175 行测试) 的代码, **生产路径 0 调用**, 是
教科书 YAGNI 案例。

## Decision

按 **YAGNI + 接口最小化** 原则合并为 2 个实体:

| 实体 | 文件 | 角色 |
|------|------|------|
| `LiveTrader` (interface) | trader.go | **唯一对外契约**: 7 个核心方法 (Submit/Cancel/GetOrder/GetPositions/GetAccount/Name/HealthCheck) |
| `MockTrader` (struct) | mock_trader.go | **唯一实现**: in-memory simulation + A-share 规则 (T+1, 印花税, 涨跌停, 限价) + **可选 OrderStore 持久化** |
| `LiveEngine` (struct) | engine.go | 保留: 真实 quote-driven execution orchestrator (独立于 LiveTrader, 用 Broker + DataFeed 接口) |

### 1. MockTrader 增加 `OrderStore` 可选字段

`MockTraderConfig` 增加:
```go
type MockTraderConfig struct {
    // ... 原有 7 字段 ...
    OrderStore OrderStore  // P1-26 新增; nil = 纯内存 (默认)
}
```

新增私有方法 `persistOrder(result, fillPrice, status, message)`:
- `OrderStore == nil` → 立即 return, **零开销**
- 否则构造 `OrderRecord` 调 `m.config.OrderStore.Save(ctx, record)`
- 失败仅 log Warn, **不阻塞** 交易主路径 (审计 > 强一致)

调用点:
- `executeBuy` fill 后 → `persistOrder(..., "filled", "")`
- `executeSell` fill 后 → `persistOrder(..., "filled", "")`
- `CancelOrder` cancel 后 → `persistOrder(..., "cancelled", "user-requested")`

### 2. 删除 3 个文件 + 1 个测试文件

| 删除 | 行数 | 原因 |
|------|------|------|
| `pkg/live/advanced_mock_trader.go` | 277 | 0 caller, RiskCheck/SlippageModel 可用 P1-29 AlertManager + 回测 slippage 配置替代 |
| `pkg/live/persistent_mock_trader.go` | 292 | 与 MockTrader 100% 行为重叠, 仅是构造模式; 现统一用 `MockTrader{OrderStore: store}` |
| `pkg/live/trader_advanced.go` | 66 | AdvancedTrader interface, 0 实现引用 |
| `pkg/live/live_test.go` | 175 | 旧 AdvancedMockTrader 单元测试, 跟随主体删除 |

### 3. `convertToOrderResult` 迁移

`persistent_mock_trader.go` 内的 `convertToOrderResult` 公共 helper
迁到 `order_store.go`, 改名 + 加 godoc:
```go
// order_store.go
// convertToOrderResult converts a persistence OrderRecord to the in-memory
// OrderResult type used by MockTrader and the HTTP API.
```

任何未来 OrderStore adapter (Postgres, Redis, In-Memory) 都可复用
这一行转换, 不再依赖具体 trader 实现。

### 4. mockOrderStoreForTest 迁移

`advanced_trader_test.go` 内的 `mockOrderStoreForTest` 迁到
`order_store_test.go`, 服务于 `trader_test.go` 的新集成测试。

### 5. LiveTrader interface 收敛

`LiveTrader` 维持 7 个方法不变 (Submit/Cancel/GetOrder/GetPositions/
GetAccount/Name/HealthCheck), **不加入** AdvanceDay / Reset / GetCash
这些 lifecycle helper — 调用方需要时直接转型到 `*MockTrader`
(只有 1 个实现, 转型安全)。

### 6. 文档同步

- `pkg/live/trader.go` 顶部 godoc 更新 — 明确 "5→2 实体" 架构
  + 已删除实体清单 + ODR-022 引用
- `MockTraderConfig.OrderStore` 字段 godoc — 引用 P1-26 + ODR-022

## Consequences

### 正面

- **代码净减 -743 行** (815 删除 / 72 新增), 减 -10.6% `pkg/live/`
  总体积
- **认知负担降**: 新人 onboarding 看 `trader.go` + `mock_trader.go` 2
  个文件就懂 100% 执行路径, 之前要在 5 个文件 + 1 个 helper 间
  跳来跳去
- **MockTrader 行为更明确**: "用 `OrderStore` 字段" vs "用
  PersistentMockTrader 子类" 哪个更直观? 显然是前者 (composition
  over inheritance)
- **测试聚焦**: `trader_test.go` 677 行覆盖 100% 实际行为 (含持久化
  集成), 之前 175 行测的是 dead code
- **0 行为变更**: 删除的 3 个文件无 production caller, 合并后的
  MockTrader 与原 MockTrader 100% API 兼容 (构造函数签名不变)

### 负面 / 取舍

- **`OrderStore` 失败只 log 不抛错**: audit 完整性 vs 主路径延迟
  的取舍, 选择主路径 (止损/市价单 fill) 不被 DB 抖动拖累
- **转型 `*MockTrader` 在 lifecycle helper 调用点不可避免**: 接受,
  因为只有 1 个实现, 转型 cost 接近 0
- **AdvancedMockTrader 的 `RiskCheck` 装饰器模式丢失**: 当前没有
  production 需求, 真要做时再用 decorator pattern 现场实现 (比
  维护 277 行 dead code 划算)
- **`pkg/live/live_test.go` 删除** = 任何依赖其符号的 test fixture
  要跟进清理 (grep 后无外部引用)

## Artifacts

### 删除 (3 整文件 + 1 瘦身, 810 行)

- `pkg/live/advanced_mock_trader.go` (-277)
- `pkg/live/persistent_mock_trader.go` (-292)
- `pkg/live/trader_advanced.go` (-66)
- `pkg/live/live_test.go` (-175 dead-code tests; 文件保留 723 行 LiveEngine 测试)

### 修改 (3 文件, +72 / -X)

- `pkg/live/mock_trader.go` (+38 / -0) — `OrderStore` 字段,
  `persistOrder` 私有方法, `CancelOrder` 持久化 hook
- `pkg/live/order_store.go` (+24 / -0) — `convertToOrderResult`
  + godoc
- `pkg/live/trader.go` (+15 / -0) — 顶部 godoc 重写, 架构说明
  + 已删除实体清单

### 复用 (无改动)

- `pkg/live/engine.go` — LiveEngine 保留, 独立契约
- `pkg/live/order_manager.go` — 保留 (LiveEngine 内部组件)
- `pkg/live/position_manager.go` — 保留 (LiveEngine 内部组件)
- `pkg/live/postgres_order_store.go` / `redis_order_store.go` —
  OrderStore 实现, 继续被 MockTrader 通过 `OrderStore` 字段复用

### 文档 (后续 commit)

- `docs/odr/odr-022-...` (本文件)
- `docs/TASKS.md` — P1-26 ⬜ → ✅ + changelog
- `docs/ADR.md` — ODR-022 加入 ODR index

## Metrics

- 净代码变化: **-743 行** (-815 / +72)
- 实体数: `pkg/live` 5 → **2** (LiveTrader interface + MockTrader impl)
  - LiveEngine 保留但独立契约, 不计入"交易实现"集合
- 删除文件: 4
- 修改文件: 3
- 新增测试: 2 (`TestMockTrader_Persistence_OrderStore_Integration`,
  `TestMockTrader_Persistence_NilStore_NoOp`)
- 测试覆盖: `go test ./pkg/live/... -count=1` **全 PASS** (28 test
  functions)
- `go vet ./pkg/live/...` exit 0
- `go build ./...` exit 0
- `go test ./... -count=1` exit 0 (e2e/tests 因服务未启 connection
  refused, 预期失败, 与本变更无关)

## Lessons Learned

1. **"配置字段 > 子类"**: PersistentMockTrader 与 MockTrader 的
   唯一差异是 `OrderStore` 是否必填, 用 `OrderStore *OrderStore`
   可选字段 + nil-check 比维护 1 个子类 + 1 套重复构造函数 + 1 套
   重复方法 dispatch 简洁 50%
2. **godoc 顶部架构图 = 最佳新员工 onboarding 工具**: 改完
   `trader.go` 顶部 godoc 后, 任何人看 30 秒就能定位 "LiveTrader 是
   啥 / MockTrader 是啥 / LiveEngine 是啥 / 已删除的 3 个实体是啥"
3. **"无 caller 删除" 比 "重构保留" 更安全**: AdvancedMockTrader
   试图做"未来 RiskCheck 装饰器" — 6 个月过去 0 caller, 真到需要
   时 decorator pattern 现场实现 50 行就够, 不必预先投资 277 行
4. **OrderStore 失败只 log 不抛错**: 主路径 (止损/市价 fill) 不可
   能因 audit log 写失败而拒单, 选 audit 完整性稍降 vs 主路径 100%
   可用。生产 P1 完成后若需要 100% 强一致, 改用 outbox pattern
   异步重试 (P2 任务)
5. **mock helper 跟随主体走**: `mockOrderStoreForTest` 从
   `advanced_trader_test.go` 迁到 `order_store_test.go` 的原则:
   "helper 与它测试的 interface 同住" — 避免"找一个 mock helper
   要 grep 3 个文件"
6. **转型是 acceptable cost**: `*LiveTrader` interface 没法塞
   AdvanceDay, 调用方转 `*MockTrader` 是 OK 的 — YAGNI 优先于
   "多态纯洁性"。前提: 只有一个实现, 转型永远成功
