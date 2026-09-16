# ODR-058: 阶段 P5 切片 1 — 封潜伏直连（退役 `pkg/marketdata` 直连 provider + `hkex` fetcher 显式归 L0）

> **Status**: Completed
> **Date**: 2026-09-15
> **Category**: Implementation
> **Related ADRs**: [ADR-022](../superseded-adr/adr-022-unified-research-platform.md)（§1 五分区 / L0 唯一数据面 / 单一摄取入口 / 单向依赖 L3→L2→L1→L0）
> **Supersedes**: —
> **Related ODRs**: [ODR-051](odr-051-l0-1-single-ingest-entry.md)（L0-1 单一摄取入口落地 — 本记录为其在**存量代码面**上的收口）, [ODR-050](odr-050-p1-base-contract-landing.md)（`ingest.raw` 归档表）, [ODR-057](odr-057-eqd-p2-1-research-profile-tool.md)（阶段 P4 起步 — 本记录为阶段 P5 起点）, [ODR-047](odr-047-equitydeep-integration-audit.md)（EquityDeep 集成审计）
> **Author**: AI Assistant

---

## Context

### 触发条件

阶段 P4 于 [ODR-057](odr-057-eqd-p2-1-research-profile-tool.md) 起步后进入阶段 P5「横截面工作面 v2」。P5 唯一任务 `P5-1` 的**验收标准是架构级断言**（`docs/PRODUCT.md:301`）：

> **两个工作面共享 L0-L2，无平行数据路径。**

该断言无法靠"新增代码"满足 —— 只能靠**清点存量代码里所有绕过 L0 的取数实现**并逐条封堵。因此先做一轮旁路勘察，再按用户裁决拆片执行。

### 旁路勘察结论（4 条平行/旁路数据路径）

| 编号 | 路径 | 说明 | 用户可见 | 本切片处理 |
|---|---|---|---|---|
| **P-A** | `pkg/marketdata` 第二套 Provider 抽象 | `akshare_provider.go` / `tushare_provider.go` —— 与 `pkg/data/source` 重复的**第二套外部源直连实现**（`FactoryDeps` / `buildProvider` 为其专门保留了 `TushareStore OHLCVStore` 与 `SourceConfig.Token/PythonPath/ScriptDir` 字段） | 否 | ✅ **本切片退役** |
| **P-B** | `cmd/analysis` 运行时数据源切换门 | `POST /api/datasource/switch`（可指向任意 URL） | **是**（既有已交付能力） |  **本切片不动**（见 §6） |
| **P-C** | `pkg/data/source` 9 适配器 + `Registry` + `ETLPipeline` | 已由 [ODR-051](odr-051-l0-1-single-ingest-entry.md) 实证：`Registry` 仅在 `cmd/data`（**L0 摄取服务自身**）装配并服务 `registerRegistryRoutes`；`ETLPipeline` 生产实例化点 = 0（仅测试）。**不在真实数据路径上** | 否 | ⛔ 不动（归属正确） |
| **P-D** | `pkg/data/source/hkex` 北向 fetcher | `EastmoneyNorthboundFetcher` 直连 eastmoney push2 | 否 | ✅ **本切片显式归 L0 摄取侧** |

合规对照路径（本切片不动，作为判据基准）：`TushareClient` + `POST /api/ingest/raw` + `ingest.raw` 归档（[ODR-051](odr-051-l0-1-single-ingest-entry.md)）。

### 前置事实（本次现场复核）

| 项 | 复核结论 |
|---|---|
| `marketdata.AdapterFactory` 系列生产调用者 | **0 个** —— 全仓 Grep `marketdata\.(FactoryDeps\|NewAdapterFactory\|BuildAdapter\|DataSourceConfig\|DefaultDataSourceConfig)` → No matches（即 `BuildPrimary`/`BuildFallback`/`BuildProvider`/`BuildAdapter` 是**无调用者的第二套工厂**） |
| `cmd/analysis` 实际使用的 `marketdata` 符号 | 仅 `NewHTTPProvider` / `NewPostgresProvider` / `NewDataAdapter` / `NewInMemoryProvider`（`setup.go`、`handlers_datasource.go`）—— **均非外部源直连** |
| `akshare` / `tushare` provider 的测试 | **0 个** —— `pkg/marketdata/` 测试仅 `eventbus_test.go` / `backpressure_bus_test.go` / `adapter_test.go` ⇒ 退役**无需改测试** |
| `hkex` 生产实例化点 | **0 个** —— 全仓 `NewEastmoneyNorthboundFetcher` / `NewNorthboundFactor` 仅命中**构造器自身**（`fetcher.go` / `factor.go`）；无生产调用方 |
| `hkex.NorthboundFactor` 依赖面 | 仅依赖 `NorthboundFetcher` **接口**（`factor.go:32`）—— 纯计算不发网络，可安全留在 L2/L3 侧 |
| `hkex.EastmoneyNorthboundFetcher` 性质 | 直连外部源（eastmoney push2）⇒ 属 **L0 摄取侧外部源适配器**：只能由 L0 摄取所有者 `cmd/data` 驱动，且须先归档 |
| 配置面残留 | `config/analysis-service.yaml` 仍声明 `sources.tushare`（`token`）/ `sources.akshare`（`python_path`/`script_dir`）—— 即已退役分支的**唯一配置载体** |
| L0 侧合规配置 | `config/data-service.yaml` 的 `tushare.*` 属 `cmd/data`（L0 摄取侧）合规配置 ⇒ **保留不动** |
| `SourceConfig.Token` / `PythonPath` / `ScriptDir` | 仅被退役的两个直连 provider 分支消费 ⇒ 随退役一并删除（消除死字段） |
| `FactoryDeps.TushareStore` | 类型为 `OHLCVStore`，该接口**定义在 `tushare_provider.go` 内**，全仓无其他使用 ⇒ 随文件删除一并消失 |
| 工具链 | `go` 为 `C:\Users\ruoxi\sdk\go1.25.0\bin\go.exe`；Docker Desktop 未运行 → DB-backed 测试按既有约定 SKIP |
| 本仓 git 写对象 | 默认 createObject 模式下 `add` / `commit` 报 `Permission denied`，须 `git -c core.createObject=link` |

### 切片裁决

| 候选 | 说明 | 结果 |
|---|---|---|
| **只封潜伏直连** | 退役无生产调用者的 P-A 第二套实现 + P-D fetcher 显式归属 L0；**不动 P-B 用户可见切换门** | ✅ **采纳**（用户裁决） |
| 一并移除 P-B `POST /api/datasource/switch` | 架构上更彻底 | ✗ 属**用户可见行为变更**（存量已交付能力），须单独评估兼容性，不混入本切片 |
| 一并重构 P-C `Registry`/`ETLPipeline` | 消除"声明态"代码 | ✗ 已由 [ODR-051](odr-051-l0-1-single-ingest-entry.md) 实证其**不在真实数据路径**且归属 `cmd/data`（L0）正确，重构无架构收益 |

裁决理由：本切片的验证点是**可机械核验的**（「生产代码中外部源直连实例化点 = 0」），而动 P-B 必然引入用户可见行为变更、动 P-C 必然引入无收益的迁移风险 —— 都不属于"封潜伏直连"。

---

## Decision

**退役 `pkg/marketdata` 侧 akshare/tushare 直连 provider（与 `pkg/data` 重复的第二套实现），在工厂处对「外部源直连类型」显式拒绝并指向 L0 入口，并将 `hkex` 北向 fetcher 显式归为 L0 摄取侧（以注释固化归属）。不动用户可见行为、不改存量语义。**

### 1. 删除两个直连 provider（P-A 退役）

- `pkg/marketdata/akshare_provider.go` —— **删除**
- `pkg/marketdata/tushare_provider.go` —— **删除**（含其内定义的 `OHLCVStore` 接口与 `tushareResponse` / `tushareData` / `fieldGet` / `floatVal` / `strVal` / `parseFloat` / `extractExchange` / `uniqSortedDays` 等辅助函数 —— 经核验**仅被本文件使用**）

### 2. 工厂显式拒绝（把"退役"写成可执行的语义）

`pkg/marketdata/config.go` 的 `buildProvider` switch 原有两个实体分支 `case "tushare"` / `case "akshare"`，合并为一个**显式拒绝**分支：

```go
case "tushare", "akshare":
	// ADR-022 §1: 外部数据源只能经由 L0 单一摄取入口（POST /api/ingest/raw）
	// 归档进 ingest.raw，再由各工作面读 L0 数据面。此处显式拒绝，避免出现
	// 绕过 L0 的平行直连取数路径。
	return nil, fmt.Errorf("source %q (%s): external-source direct provider retired per ADR-022; ingest at the L0 door POST /api/ingest/raw and read ingest.raw instead", name, src.Type)
```

要点：

- 保留 `case` 分支而**不落入 `default`** —— 错误信息能明确指出"已退役 + 正确入口"，而非含糊的 `unknown provider type`。
- 错误消息自带 ADR-022 与 `POST /api/ingest/raw` 的**指向性**，使配置写错时能自解释。

### 3. `SourceConfig` / `FactoryDeps` 死字段删减

| 结构体 | 删除字段 | 理由 |
|---|---|---|
| `SourceConfig` | `Token` / `PythonPath` / `ScriptDir` | 仅被退役的两个直连分支消费 |
| `FactoryDeps` | `TushareStore OHLCVStore` | 接口 `OHLCVStore` 定义于已删除的 `tushare_provider.go`，全仓无其他使用 |

保留 `Type` / `URL` / `DBURL` / `RedisURL` / `Options`（`http` / `postgres` / `inmemory` 分支仍在用）。

### 4. 配置面清理

`config/analysis-service.yaml`：删除 `sources.tushare` 与 `sources.akshare` 两块（即上表"配置面残留"），并在 `datasource:` 上方显式注明 ADR-022 §1 归属（外部源不得由本服务直连，统一经 L0 入口）。保留 `http` / `postgres` / `inmemory` 三源。

### 5. `hkex` 北向 fetcher 显式归 L0 摄取侧（P-D 收敛）

`pkg/data/source/hkex` 当前**无生产实例化点**，故不需迁移代码；缺的是**归属的可判定性**——否则未来任一面工作区都可能"顺手"实例化它。以注释固化三条判据：

- `EastmoneyNorthboundFetcher` 属 **L0 摄取侧外部源适配器**：只能由 L0 摄取所有者 `cmd/data` 驱动，且须先归档进 `ingest.raw`；
- 分析 / 回测 / 工作流侧**不得实例化**（`NorthboundFactor` 只依赖 `NorthboundFetcher` 接口，可在 L2/L3 侧安全复用）；
- 本包当前生产实例化点 = 0（即本条判据的核验快照）。

落点：`pkg/data/source/hkex/types.go`（包注释新增 `ADR-022 §1 分层归属（显式归 L0 摄取侧）`）+ `pkg/data/source/hkex/fetcher.go`（`NewEastmoneyNorthboundFetcher` 文档注释标注 `L0 摄取侧专用`）。**函数签名与实现零改动。**

### 6. 明确不动项（切片边界）

| 项 | 状态 | 说明 |
|---|---|---|
| `P-B` `POST /api/datasource/switch` | ⛔ 不动 | 用户可见能力，属独立评估项 |
| `P-C` `pkg/data/source` `Registry` / `ETLPipeline` |  不动 | 归属 L0（`cmd/data`）正确，且 `ETLPipeline` 生产实例化点已 = 0 |
| `config/data-service.yaml` 的 `tushare.*` | ⛔ 不动 | L0 摄取侧合规配置 |
| `cmd/data/registry_init.go` 适配器注册 | ⛔ 不动 | L0 摄取侧注册路径 |
| `pkg/marketdata/provider.go` 接口 | ⛔ 不动 | 退役 provider 只是其隐式实现者；`http` / `postgres` / `inmemory` / `cached` 仍实现它 |

---

## Consequences

### 正面

| 收益 | 说明 |
|---|---|
| **平行数据路径减少 2 条** | P-A（重复第二套实现）被物理删除、P-D（fetcher 归属）被显式固化，P5-1 验收断言「无平行数据路径」的存量面收缩 |
| **架构约束可执行化** | "外部源只能走 L0"从**文档约定**变为**工厂级拒绝 + 指向性错误消息**——写错配置会立刻得到自解释的失败，而非静默绕过 |
| **消除重复实现** | `pkg/marketdata` 与 `pkg/data/source` 两套外部源直连实现收敛为一套（后者归属 L0） |
| **死字段清零** | `SourceConfig` 3 字段 + `FactoryDeps` 1 字段随退役删除，无残留 |
| **零用户可见行为变更** | 工厂原本无生产调用者，删除对运行中的 analysis / data 服务**不可观测** |

### 负面 / 代价

| 代价 | 说明 | 缓解 |
|---|---|---|
| 失去"应急直连"能力 | 若 L0 摄取链路故障，analysis 服务无法再经 `tushare` type 临时直连外部源 | 这是 ADR-022 §1 的**有意代价**——应急应走 `http` 源（`:8081` data-service）而非绕过 L0 |
| 配置面为破坏性变更 | 若某部署环境的 `analysis-service.yaml` 仍含 `sources.tushare` 并被 `primary`/`fallback` 引用，升级后启动即失败 | 改为**显式报错**而非静默忽略（fail-fast，错误消息直接给正确入口）；本仓配置已同步清理 |
| `hkex` 归属仅靠注释固化 | 未来仍可能被误实例化，无编译期强制 | 已在 `types.go` 包注释写明三条判据；若后续 `cmd/data` 接入该 fetcher，应同批加 `var _ NorthboundFetcher = (*EastmoneyNorthboundFetcher)(nil)` 断言与 L0 归档调用 |

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| 退役误删仍被间接使用的符号 | 低 | 已逐符号核验：`OHLCVStore` 及全部辅助函数仅在被删文件内使用；`go build ./...` 通过 |
| 两套实现的实际差异被一并抹掉 | 低 | 仅删除**无调用者**的第二套（`AdapterFactory` 系列全仓 0 调用），合规路径 `TushareClient` 未受影响 |
| P-B 切换门使"无平行数据路径"断言仍不成立 | **中** | 本切片范围外，须在 P5-1 后续切片中单独评估（用户可见行为变更） |

---

## Artifacts

### 代码

| 文件 | 变更 |
|---|---|
| `pkg/marketdata/akshare_provider.go` | **删除** |
| `pkg/marketdata/tushare_provider.go` | **删除**（含 `OHLCVStore` 接口与 8 个辅助函数） |
| `pkg/marketdata/config.go` | `SourceConfig` 删 3 字段 / `FactoryDeps` 删 1 字段 / `buildProvider` 的两个实体分支合并为显式拒绝分支 |
| `pkg/data/source/hkex/types.go` | 包注释新增「ADR-022 §1 分层归属（显式归 L0 摄取侧）」三条 |
| `pkg/data/source/hkex/fetcher.go` | `NewEastmoneyNorthboundFetcher` 文档注释标注「L0 摄取侧专用（ADR-022 §1）」（签名与实现未变） |

### 配置

| 文件 | 变更 |
|---|---|
| `config/analysis-service.yaml` | 删 `sources.tushare` / `sources.akshare`；`datasource:` 上方新增 ADR-022 §1 归属注释 |

### 文档

| 文件 | 变更 |
|---|---|
| `docs/archive/odr/odr-058-p5-1-retire-direct-providers.md` | **新建**（本记录） |
| `docs/SPEC.md` | Provider 实现表删 2 行（改为退役说明）+ `DataAdapter` fallback 示例改为合规 provider |
| `docs/TASKS.md` | 头部版本 3.32.0 → 3.33.0；D1-3 / D1-5 标注「已退役」；`L0-1` 行补存量收口说明；`P5-1` 行标注切片 1 进展；统计与变更日志 |
| `docs/ARCHITECTURE.md` | `pkg/marketdata/` 目录树删 2 个已退役文件名 |
| `docs/archive/RESEARCH-equitydeep-legacy.md` | §4 实施路线表加编号口径注（与 ADR-022 阶段编号区分） |
| `docs/ADR.md` | ODR 索引新增 ODR-058；尾注 ODR 57 → 58；index 3.13.0 → 3.14.0 |

---

## Metrics / 验证

| 项 | 结果 |
|---|---|
| `go build ./...` | **EXIT=0** |
| `go test ./pkg/marketdata/... ./pkg/data/...` | **全部 `ok`（EXIT=0）** |
| **验收点**：生产代码中「外部源直连实例化点 = 0」 | ✅ 全仓 Grep `NewAkShareProvider\|NewTushareProvider\|NewEastmoneyNorthboundFetcher\|NewNorthboundFactor` → 仅命中**构造器自身**（`fetcher.go` / `factor.go`），**无生产调用方** |
| 退役符号残留引用 | ✅ 无（`go build` 通过即证） |
| 用户可见行为变更 | ✅ 零（`AdapterFactory` 系列原本 0 生产调用者） |
| `gofmt -l` | 报 `config.go` / `fetcher.go` / `types.go` —— `gofmt -d` 两侧逐字相同（仅 CRLF 行尾），按本仓既有约定判定为**行尾误报，不修改** |
| 无关失败（不属本切片） | `pkg/backtest/state` 的 `TestDiskStateStore_ConcurrentSaveLoad` 因用例生成含 `>`/`<`/`?` 等字符的临时文件名在 Windows 上失败 —— **既有平台缺陷**，与本切片无关（单独重跑 `./pkg/marketdata/... ./pkg/data/...` 全绿） |

---

## 未做项（明确排除）

1. **`POST /api/datasource/switch`（P-B）** —— 用户可见行为变更，须单独评估（见 §6）。
2. **`pkg/data/source` `Registry` / `ETLPipeline` 重构（P-C）** —— 归属 L0 正确、`ETLPipeline` 生产实例化点已 = 0，重构无架构收益。
3. **`hkex` fetcher 的编译期约束** —— 未加 `var _ NorthboundFetcher = (*EastmoneyNorthboundFetcher)(nil)` 断言与 L0 归档调用（该 fetcher 尚无生产接入需求；接入时应同批补齐）。
4. **`provider.go` 接口瘦身** —— `Provider` 接口 11 方法未随退役调整（仍有 4 个实现者）。
5. **`docs/archive/research-2026-Q2/PHASE3-PLAN.md`** 中的 D1.3 / D1.5 描述 —— 属**归档历史快照**，按历史口径保留原样（不回写）。

---

## Lessons Learned

1. **"无调用者的实现"是架构噪声，不是技术债** —— `AdapterFactory` 系列有完整实现、配置结构与 `FactoryDeps` 依赖注入，看起来是**一等公民**；但全仓 0 调用者。它不会被任何静态检查（编译、lint、测试）标记为死代码，却实打实地构成**第二条平行数据路径**。清点"平行路径"必须用**调用者数量**作判据，而非"代码是否完整"。
2. **退役要写成"可执行的拒绝"，而非"删除文件"** —— 只删文件，配置里写 `type: tushare` 会静默落入 `default` 报 `unknown provider type`，语义上"从未存在过"。保留 `case` 分支并给出 ADR 编号 + 正确入口，才把**架构决策钉进运行时**。
3. **归属不清比实现错误更危险** —— `hkex` fetcher 代码本身没问题，问题在于"它属于哪一层"没有任何可判定依据。**零改动 + 三条判据注释**即可消除未来误用风险；不是所有治理动作都需要改代码。
4. **切片边界要显式写出"不动项"** —— 本切片只封"潜伏直连"。把 P-B（用户可见）/ P-C（归属正确）/ 归档文档逐条写进「未做项」，才使"切片 1"这一命名有意义，并让后续切片的起点无需重新勘察。