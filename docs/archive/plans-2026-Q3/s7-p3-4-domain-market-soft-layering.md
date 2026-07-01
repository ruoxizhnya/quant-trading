# S7-P3-4: 数据层软分层 — `pkg/domain/market/` 子包

> **任务来源**: ODR-043 D3 决策 — "不做 big-bang schema 重构，采用'软分层 + view 过渡'，新增 `pkg/domain/market/` 子包"
> **任务追踪**: [docs/TASKS.md](../../docs/TASKS.md) — S7-P3-4
> **执行规范**: AGENTS.md §8 — 测试先行 + 代码审查 + 原子提交

---

## 1. Summary (摘要)

将 `pkg/domain/types.go` 中的 **8 个市场数据相关类型**迁移到新的 `pkg/domain/market/` 子包，同时在 `pkg/domain/types.go` 中保留 **Go type alias**（`type OHLCV = market.OHLCV`）作为兼容层。

**关键洞察**: Go type alias 让 `domain.OHLCV` 与 `market.OHLCV` 成为**同一个类型**（不是新类型），所有 199 个消费者文件**无需任何修改**即可编译通过。这是一次"零破坏"重构 — 旧代码继续工作，新代码可逐步迁移到 `market.` 命名空间。

**软分层的含义**:
- 新 `pkg/domain/market/` 与旧 `domain.X` 类型**并存**（非替换）
- Type alias 作为"过渡视图"，让两者指向同一类型
- `FundamentalData`（带 DB 元数据 ID/CreatedAt）作为持久化视图，与纯领域类型 `Fundamental` 区分

---

## 2. Current State Analysis (现状分析)

### 2.1 现有结构

`pkg/domain/` 当前为**扁平结构**（无子目录）：

```
pkg/domain/
├── types.go              # 329 行 — 全部领域类型集中
├── corporate_action.go  # CorporateActionSplit（避免与 Split 重名）
├── backtest.go           # BacktestResult / Trade / BacktestJob 等
├── execution.go          # 执行层类型
├── factor.go             # 因子类型
├── screen.go             # 选股筛选类型
├── types_test.go
├── corporate_action_test.go
└── execution_test.go
```

### 2.2 待迁移的 8 个市场类型（来自 `pkg/domain/types.go`）

| # | 类型 | 行号 | 性质 | 外部消费者数 |
|---|------|------|------|------------|
| 1 | `OHLCV` | L37-51 | 行情数据 | ~104 文件（最高风险） |
| 2 | `Stock` | L54-62 | 标的基础信息 | ~30 文件 |
| 3 | `IndexConstituent` | L65-72 | 指数成分股 | 少量 |
| 4 | `Split` | L76-84 | 除权事件 | 少量 |
| 5 | `Dividend` | L87-97 | 分红事件 | 少量 |
| 6 | `Fundamental` | L100-115 | 财务基本面（纯领域） | ~26 文件 |
| 7 | `FundamentalData` | L119-138 | 财务数据（持久化视图，带 ID/CreatedAt） | 少量 |
| 8 | `MarketDataProvider` | L249-255 | 市场数据 Provider 接口 | **0 外部消费者** |

### 2.3 不迁移的类型（保留在 `pkg/domain/`）

以下类型**不属于市场数据层**，本次不迁移：
- 交易类型：`Direction`, `OrderType`, `Signal`, `Order`, `Position`, `Portfolio`, `TargetPosition`
- 风控类型：`MarketRegime`, `RiskMetrics`, `PositionSize`, `StopLossEvent`, `RiskManager`
- 配置类型（技术债，留待后续）：`Config`, `DatabaseConfig`, `RedisConfig`, `ServiceConfig`, `TushareConfig`, `StrategyConfig`

### 2.4 消费者影响评估

- **199 个文件** import `pkg/domain`（90 生产 + 109 测试）
- `domain.OHLCV`: 104 文件（48 prod + 56 test）— 使用 type alias 后**零修改**
- `domain.MarketDataProvider`: **0 外部消费者** — 可自由重命名为 `market.Provider`

---

## 3. Proposed Changes (实施方案)

采用 **2 个原子 commit** 完成：

### Commit 1: 新建 `pkg/domain/market/` 子包

#### 3.1.1 新建文件 `pkg/domain/market/doc.go`

```go
// Package market contains market-data domain types for the quant
// trading system.
//
// This sub-package was introduced by S7-P3-4 (ODR-043 D3 "soft layering")
// as the canonical home for market-data types. The legacy
// `pkg/domain` package re-exports these types as type aliases, so
// existing code referencing `domain.OHLCV` continues to compile
// unchanged. New code SHOULD prefer the `market.OHLCV` spelling.
//
// Soft layering means the two packages coexist (no big-bang rename):
//   - market.OHLCV  — canonical definition (this package)
//   - domain.OHLCV  — type alias (= market.OHLCV), backward compat
//
// Both spellings refer to the SAME type; they are interchangeable in
// assignments, function signatures, and type assertions.
package market
```

#### 3.1.2 新建文件 `pkg/domain/market/types.go`

从 `pkg/domain/types.go` **复制**（非移动）以下 7 个 struct 定义到此处：
- `OHLCV` (L37-51)
- `Stock` (L54-62)
- `IndexConstituent` (L65-72)
- `Split` (L76-84)
- `Dividend` (L87-97)
- `Fundamental` (L100-115)
- `FundamentalData` (L119-138)

保留原有的字段、JSON tag、注释。仅 `package domain` → `package market`，并添加 `import "time"`。

#### 3.1.3 新建文件 `pkg/domain/market/provider.go`

将 `MarketDataProvider` 接口（L249-255）**重命名**为 `Provider` 并迁移：

```go
package market

import (
    "context"
    "time"
)

// Provider defines the interface for accessing market data.
//
// Renamed from domain.MarketDataProvider during S7-P3-4 migration.
// Since no external consumers existed at migration time, the rename
// is safe. The legacy name remains accessible as
// `domain.MarketDataProvider` via type alias.
type Provider interface {
    GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]OHLCV, error)
    GetFundamental(ctx context.Context, symbol string, date time.Time) (*Fundamental, error)
    GetStocks(ctx context.Context, exchange string) ([]Stock, error)
    GetLatestPrice(ctx context.Context, symbol string) (float64, error)
    GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error)
}
```

#### 3.1.4 新建测试 `pkg/domain/market/types_test.go`

9 个 JSON 往返测试，验证字段/tag 完整性：

```go
package market

import (
    "encoding/json"
    "testing"
    "time"
)

// TestOHLCV_JSONRoundTrip verifies OHLCV serialization preserves all fields.
func TestOHLCV_JSONRoundTrip(t *testing.T) { /* ... */ }
func TestStock_JSONRoundTrip(t *testing.T) { /* ... */ }
func TestIndexConstituent_JSONRoundTrip(t *testing.T) { /* ... */ }
func TestSplit_JSONRoundTrip(t *testing.T) { /* ... */ }
func TestDividend_JSONRoundTrip(t *testing.T) { /* ... */ }
func TestFundamental_JSONRoundTrip(t *testing.T) { /* ... */ }
func TestFundamentalData_JSONRoundTrip(t *testing.T) { /* ... */ }
func TestFundamentalData_NilPointers(t *testing.T) { /* ... */ } // *float64 nil 边界
func TestProvider_IsInterface(t *testing.T) { /* compile-time check */ }
```

每个 RoundTrip 测试构造一个完整实例（包含所有字段），序列化 → 反序列化 → 断言字段相等。`TestFundamentalData_NilPointers` 验证 `*float64` 为 nil 时 JSON 为 `null` 而非 0。

#### Commit 1 验证

```bash
go build ./pkg/domain/market/...
go vet ./pkg/domain/market/...
go test ./pkg/domain/market/... -v -count=1 -race
```

此时 `pkg/domain/types.go` 仍含原始定义，新旧两份定义独立存在但尚未关联（`market.X` 与 `domain.X` 是两个不同类型）。下一步用 alias 统一。

---

### Commit 2: 用 type alias 替换 `pkg/domain/types.go` 中的定义

#### 3.2.1 修改 `pkg/domain/types.go`

**删除** L37-138 的 7 个 struct 定义 + L248-255 的 `MarketDataProvider` 接口定义（共 8 处），**替换**为 type alias 块：

```go
// S7-P3-4 (ODR-043 D3): Market-data types migrated to pkg/domain/market.
// These aliases preserve backward compatibility: domain.OHLCV and
// market.OHLCV are the SAME type (not two different types), so all
// existing consumer code compiles unchanged. New code SHOULD prefer
// the `market.` package path.
//
// To migrate a file: change `domain.OHLCV` → `market.OHLCV` and the
// import. No type conversion is needed.
import "github.com/ruoxizhnya/quant-trading/pkg/domain/market"

type (
    OHLCV               = market.OHLCV
    Stock               = market.Stock
    IndexConstituent    = market.IndexConstituent
    Split               = market.Split
    Dividend            = market.Dividend
    Fundamental         = market.Fundamental
    FundamentalData     = market.FundamentalData
    MarketDataProvider  = market.Provider // renamed; alias keeps old name
)
```

> **关键**: `type X = Y` 是 alias（同类型），`type X Y` 是新类型。必须用 `=`，否则 199 个消费者会编译失败。

#### 3.2.2 新建兼容性测试 `pkg/domain/market_alias_test.go`

```go
package domain

import (
    "testing"

    "github.com/ruoxizhnya/quant-trading/pkg/domain/market"
)

// TestAlias_MarketTypesIdentical verifies that domain.X and market.X
// are the SAME type (via type alias). If a future refactor breaks the
// alias, this test will fail to compile.
func TestAlias_MarketTypesIdentical(t *testing.T) {
    // Compile-time assertions: assigning across packages must work
    // without any conversion.
    var _ OHLCV = market.OHLCV{}
    var _ Stock = market.Stock{}
    var _ IndexConstituent = market.IndexConstituent{}
    var _ Split = market.Split{}
    var _ Dividend = market.Dividend{}
    var _ Fundamental = market.Fundamental{}
    var _ FundamentalData = market.FundamentalData{}

    // Interface alias: a value implementing market.Provider also
    // implements domain.MarketDataProvider (same interface).
    var _ MarketDataProvider = (MarketDataProvider)(nil)
    var _ market.Provider = (market.Provider)(nil)

    // Runtime sanity: zero values are interchangeable in slices/maps.
    ohlcv := OHLCV{Symbol: "000001.SZ"}
    var mO market.OHLCV = ohlcv // no conversion
    if mO.Symbol != "000001.SZ" {
        t.Fatalf("alias round-trip lost Symbol: got %q", mO.Symbol)
    }
}
```

#### 3.2.3 更新 `pkg/domain/types.go` 顶部包注释

```go
// Package domain contains core domain types and interfaces for the
// quant trading system.
//
// S7-P3-4 (ODR-043 D3): Market-data types (OHLCV, Stock, Fundamental,
// etc.) now live in pkg/domain/market. This package re-exports them as
// type aliases for backward compatibility. New code SHOULD import
// pkg/domain/market directly.
package domain
```

#### Commit 2 验证

```bash
go build ./...                    # 全部 199 消费者文件必须编译通过
go vet ./...                      # 静态检查
gofmt -l .                        # 格式检查
go test ./... -count=1 -race      # 全量测试（不含 /e2e）
```

---

## 4. Files Affected (受影响文件)

### 新建文件（4 个）
| 文件 | 用途 |
|------|------|
| `pkg/domain/market/doc.go` | 子包文档 |
| `pkg/domain/market/types.go` | 7 个 struct 定义（canonical） |
| `pkg/domain/market/provider.go` | `Provider` 接口（重命名自 `MarketDataProvider`） |
| `pkg/domain/market/types_test.go` | 9 个 JSON 往返测试 |

### 修改文件（1 个）
| 文件 | 修改 |
|------|------|
| `pkg/domain/types.go` | 删除 8 处定义 → 替换为 type alias 块；更新包注释 |

### 新建测试（1 个）
| 文件 | 用途 |
|------|------|
| `pkg/domain/market_alias_test.go` | alias 同一性编译期断言（force-add） |

### 文档更新（Commit 2 内或紧随）
- `docs/ARCHITECTURE.md`: 在"目录结构"章节补 `pkg/domain/market/` 子包说明 + 软分层策略
- `docs/SPEC.md`: "Core Domain Models" 章节顶部加注 canonical 路径为 `pkg/domain/market`
- `docs/TASKS.md`: S7-P3-4 状态 `⬜` → `✅`

---

## 5. Assumptions & Decisions (假设与决策)

### D1: 采用 type alias（`=`）而非新类型
**理由**: alias 让 `domain.OHLCV` 与 `market.OHLCV` 是**同一类型**，199 个消费者零修改。若用新类型 `type OHLCV market.OHLCV`，则需在所有边界做类型转换，违背"软分层"零破坏原则。

### D2: `MarketDataProvider` → `Provider` 重命名
**理由**: 该接口 0 外部消费者（grep 确认仅在 `types.go` 定义处 + 归档文档出现）。重命名为 `market.Provider` 更符合 Go 惯例（包名已含 market，无需重复）。旧名通过 alias 保留以备未来需要。

### D3: Config 类型不迁移
**理由**: `Config` / `DatabaseConfig` / `RedisConfig` 等是基础设施配置，非市场数据。迁移会扩大 blast radius 且与"软分层市场数据"目标无关。留作技术债，后续单独处理。

### D4: 2 个原子 commit
**理由**:
- Commit 1 单独可构建可测试（`market/` 子包独立成立）
- Commit 2 单独可构建可测试（alias 替换后全量编译）
- 拆分便于回滚：若 alias 出问题，可 revert Commit 2 而保留 Commit 1 的新包

### D5: 不修改任何消费者文件
**理由**: 软分层核心是"并存"。消费者迁移到 `market.` 命名空间是后续渐进工作（可在 TASKS.md 记录为 P3-4-followup），不在本任务范围。

---

## 6. Test Strategy (测试策略)

遵循 AGENTS.md §8 规范 1（测试先行）：

### Commit 1 测试（先写后实现）
- `types_test.go`: 9 个 JSON 往返测试 — 验证字段定义完整、tag 正确、`*float64` nil 边界
- **测试名描述行为**: `TestOHLCV_JSONRoundTrip`, `TestFundamentalData_NilPointers` 等

### Commit 2 测试
- `market_alias_test.go`: 编译期断言 alias 同一性 — 若 alias 被破坏，**编译失败**（比运行时断言更强）
- 全量回归: `go test ./... -count=1 -race` 确保现有 44+ 包测试不退化

### 测试文件 git add
- `*_test.go` 默认被 .gitignore 忽略（AGENTS.md memory 教训）
- 新建的两个测试文件需 `git add -f`

---

## 7. Code Review Checklist (代码审查清单)

提交前自审 + 工具审：

- [ ] **正确性**: alias 用 `=` 而非 `type X Y`；字段定义与原文件完全一致（含 JSON tag）
- [ ] **可维护性**: `market/` 包注释清晰说明软分层策略与迁移指引
- [ ] **规范遵循**: `go vet ./...` 无警告；`gofmt -l .` 无输出
- [ ] **测试质量**: JSON 往返测试覆盖所有字段；nil 指针边界有专门测试；alias 同一性有编译期断言
- [ ] **文档同步**: ARCHITECTURE.md / SPEC.md / TASKS.md 更新

---

## 8. Verification Steps (验证步骤)

### Commit 1 完成后
```bash
go build ./pkg/domain/market/...
go vet ./pkg/domain/market/...
go test ./pkg/domain/market/... -v -count=1 -race
```

### Commit 2 完成后（全量回归）
```bash
go build ./...                                  # 199 消费者必须通过
go vet ./...                                    # 静态检查
gofmt -l .                                      # 格式
go test ./... -count=1 -race                    # 全量测试（excl. /e2e）
```

预期：所有包测试通过，无编译错误，无 vet 警告。

---

## 9. Commit Message Format (提交格式)

### Commit 1
```
feat(domain/market): create market sub-package with soft-layered types

Adds pkg/domain/market/ as the canonical home for market-data domain
types (OHLCV, Stock, Fundamental, etc.) per ODR-043 D3 "soft layering"
decision. The legacy pkg/domain package will re-export these as type
aliases in a follow-up commit, keeping 199 consumer files unchanged.

MarketDataProvider interface renamed to market.Provider (0 external
consumers verified). Includes 9 JSON round-trip tests covering all
fields and nil-pointer edge cases.

Refs: S7-P3-4
Reviewed: self-review + go vet + gofmt + go test -race
```

### Commit 2
```
refactor(domain): replace market type definitions with aliases

Replaces 8 market-data type definitions in pkg/domain/types.go with
type aliases pointing to pkg/domain/market (e.g. `type OHLCV =
market.OHLCV`). This makes domain.OHLCV and market.OHLCV the SAME
type, so all 199 consumer files compile unchanged — zero blast radius.

Adds TestAlias_MarketTypesIdentical as a compile-time canary: if a
future refactor breaks the alias, the test fails to compile.

Refs: S7-P3-4
Reviewed: self-review + go vet + gofmt + full test suite -race
```

---

## 10. Out of Scope (不在本任务范围)

- **消费者迁移**: 将 199 个文件的 `domain.OHLCV` → `market.OHLCV` — 后续渐进任务
- **Config 类型迁移**: `Config`/`DatabaseConfig`/`RedisConfig` 等 — 技术债，单独处理
- **数值仲裁**: ODR-011 多源冲突 reconcile — Phase 5
- **ADR-014 标记 Superseded**: 属于 S7-P3-6

---

## 11. Risk Assessment (风险评估)

| 风险 | 概率 | 缓解 |
|------|------|------|
| alias 误用为新类型导致编译失败 | 低 | 编译期断言测试 + commit 2 全量构建验证 |
| 字段/tag 复制时遗漏 | 中 | JSON 往返测试逐字段断言 |
| `MarketDataProvider` 重命名破坏隐藏消费者 | 极低 | grep 确认 0 外部消费者；alias 保留旧名 |
| 测试文件被 gitignore | 中 | `git add -f` 强制添加（memory 教训） |

整体风险：**低**。Type alias 是 Go 语言原生支持的兼容性机制，无运行时开销。
