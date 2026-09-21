# Quant Lab 全栈审查报告

> **日期**: 2026-09-21
> **性质**: 全项目自上而下静态审查（架构 / 回测正确性 / 数据面 / 后端工程 / 安全运维 / 测试 / 前端）
> **范围**: 全仓 Go 后端 + Vue 3 前端 + E2E + 数据库 DDL + CI/部署 + 文档
> **环境限制**: 本机无 Go 工具链（`go` 不在 PATH，全盘搜索无 `go.exe`），`go build` / `go vet` / `go test -race` 未执行。全部结论来自**静态审查 + 源码逐行取证**，动态复验清单见附录 D。
> **说明**: 本报告为**一次性审计快照**（AGENTS.md Rule 3：报告不进常青层）。审计决策归档于 [ODR-065](../../odr/odr-065-fullstack-static-review.md)；可执行修复任务已登记 [docs/TASKS.md](../../../TASKS.md)（P0-7~P0-11 / P1-15~P1-22 / P2-14~P2-16，AUD-xx 编号与任务一一对应）。2026-09-21 自 `docs/review-report/` 归位至此，相对链接层级已同步修正。本文件覆盖了此前误存入本文件的过程性草稿内容。

---

## 1. 执行摘要

| 等级 | 数量 | 一句话概括 |
|------|------|-----------|
| **Critical** | 5 | 度量基准公式错误、walk-forward 跨窗口前视、未授权代码落盘端点、RBAC 未接线、11 张表 DDL 断层 |
| **High** | 9 | 印花税 2 倍偏差、涨跌停缺板块档位、*ST 识别失效（测试固化 bug）、MockTrader 数据竞争、无整手取整、Windows 沙箱无资源限制、CI 门禁缺失、PG/Redis 端口暴露 |
| **Medium** | 6 | 文档架构漂移、表数口径错误、双 migrations 死文档、staticcheck 可绕过、死 E2E 测试、fundamentals_detail 空表 |
| **Low** | 4 | main 领先 origin 51 提交、live engine 组合状态、legacy HTML、文档导航失效 |

**三个最重要的结论**：

1. **研究结论的度量基准不可信**：日收益率公式（C1）把交易现金流当外部资金流，污染 Sharpe/Sortino/波动率 → walk-forward 门禁 → 基因池 fitness → AI 挖因子全链路。**修复合式后，此前的因子筛选结论需要作废重评。**
2. **安全边界只有"认证"没有"授权"**：JWT 认证层做得扎实（fail-closed），但 `RequireRole` 全仓只挂了一处（用户管理端点），下单和全部 19 个 MCP 工具对任何已认证用户开放（C4），另有一个端点可路径遍历写入任意文件（C3）。
3. **多源数据面在全新部署上开箱不可用**：ETL 管道要写的 13 类数据中 11 类的目标表只存在于"不被执行"的 `migrations/*.sql` 里（C5），新库直接 `relation does not exist` 硬失败。

---

## 2. 审查策略与方法论

### 2.1 三段式流程

```
阶段 1  策略制定  →  7 个审查维度 + 取证纪律
阶段 2  并行执行  →  6 路并行深挖（架构/回测/数据面/后端/安全/测试+前端）
阶段 3  交叉验证  →  所有 Critical/High 结论由主审逐条回源码复核行号，
                     未经复核的结论降级标注，误报剔除并记录（§9）
```

### 2.2 七维度定义

| 维度 | 审查对象 | 关注点 |
|------|---------|--------|
| D1 架构与文档一致性 | AGENTS.md / ADR / PRODUCT.md vs 代码 | 文档声明逐条对源码验证；死链；架构落地度 |
| D2 回测正确性 | pkg/backtest, pkg/fees, pkg/metrics | T+1、涨跌停、复权、幸存者偏差、可复现性、费用、滑点、指标公式金融语义 |
| D3 数据面 | pkg/storage, migrations/, pkg/data | DDL 与写入路径对账、单一摄取入口、PIT/ann_date、证据 content_hash |
| D4 后端工程质量 | 全部 Go 包 | 锁作用域、context 传递、错误处理、goroutine 泄漏、资源释放 |
| D5 安全与运维 | pkg/auth, internal/sandbox, docker-compose, CI | 攻击面枚举、RBAC、沙箱逃逸、密钥、端口暴露、CI 门禁 |
| D6 测试质量 | 230 个测试文件 | 占位断言、time.Sleep 同步、skip 掩盖、测试固化 bug |
| D7 前端与 E2E | web/src, e2e/tests | Vue3 规范（shallowRef/markRaw/nextTick）、死代码、指向已删服务的测试 |

### 2.3 取证纪律

- 每条 Critical/High 结论必须带 `文件:行号` 证据与代码摘录；
- 子代理结论**未经主审复核的一律不作为定论**；
- 正向结论（"存在 bug"）与负向结论（"不存在防护"）区分置信度——负向结论（如 H6 整手取整缺失）标注为"未找到"，存在被漏看的可能；
- 全部误报剔除记录在 §9，保证审查过程可审计。

### 2.4 环境限制

| 项目 | 状态 | 后果 |
|------|------|------|
| `go build` | 未执行 | 编译健康度未知（但多文件交叉印证降低风险） |
| `go vet` | 未执行 | 静态分析缺口 |
| `go test ./... -race` | 未执行 | C2/H5 类数据竞争结论基于代码推演，未获得 `-race` 实证 |
| `npm test` / `typecheck` | 未执行 | 前端类型健康度未知 |
| Playwright E2E | 未执行 | 仅静态审查了测试代码本身 |

---

## 3. 项目量化概览

| 指标 | 数值 |
|------|------|
| Go 源码 | 544 文件 / 136,571 行 |
| Go 测试 | 230 文件 / 61,321 行（测试:源码 ≈ 45%，规模真实） |
| 前端 (web/src) | 82 文件 / 9,992 行 |
| 文档 | 158 个 Markdown / 30,240 行 |
| 内联 DDL 建表 | 22 张（[pkg/storage/postgres.go](../../../pkg/storage/postgres.go) `migrate()` 数组，grep `CREATE TABLE IF NOT EXISTS` 实测） |
| `migrations/` 目录 | 10+ 个 .sql，**不被任何代码执行** |
| Git | `main` 领先 `origin/main` **51 提交**（2026-09-21 实测），未走 feature branch 流程 |

---

## 4. Critical 发现

> 每项含：位置 → 证据 → 影响推演 → 修复建议 → 验收标准。

---

### C1 日收益率公式错误 — 夏普/索提诺/波动率全部失真

- **位置**: [pkg/backtest/metrics/performance.go#L78-L100](../../../pkg/backtest/metrics/performance.go#L78-L100)（`CalculateReturns`）
- **置信度**: 确证（代数推导 + 代码逐行复核）
- **影响面**: 回测报告全部风险调整指标 → walk-forward L1-L5 门禁 → 基因池 fitness 排序 → Hermes 自主因子挖掘

**证据**：

```go
// CalculateReturns
cashFlow := portfolioValues[i].Cash - portfolioValues[i-1].Cash
netValue := currValue - cashFlow
dailyReturn := (netValue - prevValue) / prevValue
```

**问题本质**：该公式假设 `Cash` 的变化是**外部资金流**（申赎），属于经典的时间加权收益率（TWR）算法。但回测是**封闭系统**——现金变化 100% 来自买卖交易，不存在外部流。做代数化简即可看清破坏性：

设第 i-1 日现金 C₀、持仓市值 P₀（TotalValue = C₀ + P₀）；当日买入 B 股花费 b（Cash 变为 C₀-b，持仓市值变为 P₀+b，假设价格未动）。则：

```
cashFlow = (C₀ - b) - C₀ = -b
netValue = (C₀ + b + P₀) - (-b) = C₀ + P₀ + 2b
dailyReturn = (netValue - prevValue) / prevValue = 2b / TotalValue
```

一次**价格完全未动的买入**被计入 `2b / TotalValue` 的虚假日收益；对应的卖出再注入反向虚假收益。**一次买卖往返给收益率序列注入 ±10% 量级（满仓时）的噪声**。

**影响推演**：

1. 日收益率序列被交易噪声污染 → 波动率被系统性高估；
2. Sharpe / Sortino / Calmar 分母失真 → 指标不可比、不可信；
3. walk-forward 以这些指标做门禁 → 真好假好无法区分；
4. 基因池 / TPE / 遗传算法以 fitness 排序 → **进化方向被噪声主导**；
5. AI 挖掘（Hermes autonomous factor mining）产出的"好因子"实际是拟合了交易噪声的因子。

**修复建议**：

```go
// 封闭回测无外部资金流，直接用 TotalValue 差分
dailyReturn := (currValue - prevValue) / prevValue
```

删除 `cashFlow` 相关逻辑。若未来支持申赎，再引入带外部流时间加权的正确 TWR 实现（分交易日核算，非本公式）。

**验收标准**：

1. 回归测试：构造「T0 满仓现金 → T1 全仓买入（价格不变）→ T2 价格不变」序列，断言 T1/T2 日收益均为 0；
2. 对照测试：单一标的买入持有，`CalculateReturns` 产出与该标的前复权日收益逐日一致；
3. 修复后**重跑全部历史回测与 walk-forward 报告**，旧报告标记作废。

---

### C2 Walk-forward 并发窗口共享同一引擎实例 — 跨窗口前视偏差

- **位置**:
  - [pkg/backtest/walkforward/walkforward.go#L19-L23](../../../pkg/backtest/walkforward/walkforward.go#L19-L23)（`runner contracts.EngineRunner` 共享字段）
  - [pkg/backtest/walkforward/walkforward.go#L142-L160](../../../pkg/backtest/walkforward/walkforward.go#L142-L160)（固定 4 并发跑全部窗口）
  - [cmd/analysis/setup.go#L342](../../../cmd/analysis/setup.go#L342)（`NewWalkForwardEngine(engine, store)` 注入单例）
- **置信度**: 确证（结构体字段 + 调用链复核）

**证据**：

```go
type WalkForwardEngine struct {
    runner contracts.EngineRunner   // ← 共享字段，所有窗口复用
    store  storage.Storage
    ...
}
```

[setup.go](../../../cmd/analysis/setup.go) 中所有 walk-forward 请求共享同一个 `engine` 实例：

```go
wfEngine := backtest.NewWalkForwardEngine(engine, store)
```

而 `engine` 内部的基本面缓存 / 因子缓存 / 上市日历缓存是**引擎级共享槽**（非 per-run 生命周期）。随后窗口以固定并发度并行执行。

**注释与事实相反**：代码注释声称"窗口之间互不共享状态"，但 runner 恰恰是共享的。

**影响推演**：

1. **跨窗口前视**：窗口 N+1 的样本外（OOS）数据可能在窗口 N 执行期间被写入共享缓存，又被并发窗口读到 → OOS 指标系统性偏乐观 → walk-forward 的"防过拟合"目的落空；
2. **数据竞争**：并发窗口同时读写引擎级缓存（map/slice），无锁保护 → `go test -race` 必报；
3. 与 C1 叠加：门禁指标本身失真 + 门禁隔离失效，**双重击穿**验证体系。

**修复建议**：

1. 注入**工厂函数**而非实例：`NewWalkForwardEngine(func() contracts.EngineRunner { return backtest.NewEngine(...) }, store)`，每个窗口独立构造 runner；
2. 或给引擎缓存引入 per-run 命名空间（`runID` 作 key）；
3. 注释更正为与实现一致。

**验收标准**：

1. 单测：两个并发窗口分别写入同名因子缓存，断言互不可见；
2. `go test -race` 下 walkforward 全测试通过；
3. 修复后重跑历史 walk-forward 报告并与旧值对比（预期 OOS 指标下降——这才是真实水平）。

---

### C3 `/api/copilot/save` 路径遍历 + 未校验代码落盘（远程代码执行）

- **位置**:
  - [cmd/analysis/handlers_copilot.go#L174-L195](../../../cmd/analysis/handlers_copilot.go#L174-L195)（`saveStrategyHandler`）
  - [cmd/analysis/handlers_copilot.go#L278](../../../cmd/analysis/handlers_copilot.go#L278)（路由注册）
- **置信度**: 确证

**证据**：

```go
func saveStrategyHandler(c *gin.Context) {              // L174
    filename := fmt.Sprintf("strategy_%s.go", req.StrategyName)  // L181: 无净化
    dir := "./pkg/strategy/plugins"
    filePath := fmt.Sprintf("%s/%s", dir, filename)
    os.WriteFile(filePath, []byte(req.Code), 0644)      // L189: 代码未过任何校验即落盘
}
// L278: copilot.POST("/save", saveStrategyHandler)
```

**三重缺陷**：

1. **路径遍历**：`StrategyName` 未做任何白名单校验。传入 `../../../cmd/analysis/main` 即可把任意内容写到任意路径（`.go` 后缀固定，但仍可覆盖任意 `.go` 源文件或插件目录外的同名文件）；
2. **绕过沙箱**：AI generate 路径有 [internal/sandbox/staticcheck](../../../internal/sandbox/staticcheck/staticcheck.go) 静态黑名单 + runner 沙箱两道关卡，`/save` 端点完全绕过——`req.Code` 里可以写 `exec.Command`、`os.RemoveAll`、网络外联，**原样落盘**；
3. **热加载目录**：目标目录是插件热加载目录（ADR-001 hot-swap），写入即加载 → **等同远程代码执行**（认证后）。

**影响推演**：任何已认证用户（结合 C4，viewer 也可以）可提交任意 Go 代码 → 服务进程内执行 → 读取 `.env`、写数据库、横向移动。这是全仓风险最高的单点。

**修复建议**（按优先序）：

1. **首选**：直接删除 `/save` 端点，统一走 generate 流水线（改动最小、攻击面归零）；
2. 若必须保留：
   ```go
   var validName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
   if !validName.MatchString(req.StrategyName) { c.JSON(400, ...); return }
   filePath := filepath.Join("./pkg/strategy/plugins", "strategy_"+req.StrategyName+".go")
   if err := staticcheck.Check(req.Code); err != nil { c.JSON(422, ...); return }  // 复用 generate 路径的关卡
   ```

**验收标准**：

1. 测试：`StrategyName = "../../evil"` 返回 400；
2. 测试：`Code` 含 `exec.Command` 返回 422；
3. 渗透复验：遍历字符、URL 编码、绝对路径注入全部被拒。

---

### C4 授权（RBAC）缺失：viewer 可下单、可调用全部 19 个 MCP 工具

- **位置**:
  - [cmd/analysis/handlers_execution.go#L69-L92](../../../cmd/analysis/handlers_execution.go#L69-L92)（`/api/execution/*` 零角色校验，含 legacy 根路径 `/orders`）
  - [cmd/analysis/handlers_tools.go#L55-L64](../../../cmd/analysis/handlers_tools.go#L55-L64)（`/api/tools/*` 零角色校验）
  - [cmd/analysis/handlers_auth.go#L30](../../../cmd/analysis/handlers_auth.go#L30)（全仓唯一 `RequireRole` 调用点）
  - [pkg/auth/auth.go#L34](../../../pkg/auth/auth.go#L34)（`RoleViewer` 只读语义定义了但没接线）
- **置信度**: 确证（全仓 grep `RequireRole` 仅 1 处；`CanTrade()` 无生产调用点）

**证据**：

```go
// handlers_execution.go — 任何已认证用户可下单
execGroup := router.Group("/api/execution")
{
    execGroup.POST("/orders", h.createOrder)
    ...
}
// legacy 根路径同样裸奔
router.POST("/orders", h.createOrder)
```

```go
// pkg/auth/auth.go — 角色模型完整，但 CanTrade() 除测试外无调用点
RoleViewer Role = "viewer" // read-only
```

**缓解因素**（如实陈述，故定为 C4 而非"可直接资金损失"）：

1. 认证层 fail-closed：[cmd/analysis/setup.go#L617-L629](../../../cmd/analysis/setup.go#L617-L629) 全局 `authSvc.Middleware()`；`allow_insecure` 仅 loopback 生效（[config/analysis-service.yaml#L19-L25](../../../config/analysis-service.yaml#L19-L25)）；
2. 执行层当前是 MockTrader，非真实券商。

**影响推演**：一旦接真实券商（Phase 4 live），或 `/api/tools` 中的写入型工具（save_factor / 数据同步 / 回测触发）被滥用，viewer/任何泄漏的 token 均可执行。授权模型已经写好却没接线，属于"最后一公里"缺失。

**修复建议**：

```go
// /api/execution/* — 交易动作
execGroup.POST("/orders", auth.RequireRole(auth.RoleTrader, auth.RoleAdmin), h.createOrder)
execGroup.POST("/orders/:id/cancel", auth.RequireRole(auth.RoleTrader, auth.RoleAdmin), h.cancelOrder)

// /api/tools/* — 按工具副作用分级
tools.POST("/:name", h.toolGate(registry), h.handleExecute)
// toolGate: 查 registry 中工具声明的 side-effect（readonly / write / execute）
// readonly → 任意角色; write → trader+; execute → trader+（或 admin）
```

**验收标准**：

1. 测试：viewer 调 `POST /api/execution/orders` 返回 403；
2. 测试：viewer 调 `POST /api/tools/save_factor` 返回 403，调 `GET /api/tools` 返回 200；
3. legacy 根路径与 `/api/*` 路径行为一致。

---

### C5 迁移双轨断层：ETL 要写的 11 张表，内联 DDL 从不创建

- **位置**:
  - [pkg/storage/bulk_insert.go#L48-L62](../../../pkg/storage/bulk_insert.go#L48-L62)（`TableMapper` 13 个映射）
  - [pkg/storage/postgres.go#L97-L575](../../../pkg/storage/postgres.go#L97-L575)（内联 `migrate()`，grep 实测 22 张 `CREATE TABLE`）
  - [pkg/storage/postgres.go#L82-L96](../../../pkg/storage/postgres.go#L82-L96)（注释明确 `migrations/` **不被执行**）
  - [cmd/data/registry_init.go#L171-L207](../../../cmd/data/registry_init.go#L171-L207)（11 类数据的摄取链路已实盘注册）
- **置信度**: 确证（建表清单 vs 写入清单求差集 + 全仓 Go grep 无覆盖）

**证据 — TableMapper 声明的 13 个映射**：

| data_type | 目标表 | 内联 DDL 是否创建 |
|-----------|--------|------------------|
| ohlcv_daily | ohlcv_daily_qfq | ✅（postgres.go L110） |
| fundamentals | stock_fundamentals | ✅（L123） |
| ohlcv_minute | ohlcv_minute | ❌ 仅 migrations/015 |
| realtime_quote | realtime_quote | ❌ 仅 migrations/015 |
| capital_flow | capital_flow | ❌ 仅 migrations/015 |
| sectors | sectors | ❌ 仅 migrations/016 |
| stock_sector | stock_sector_map | ❌ 仅 migrations/016 |
| top_list | top_list | ❌ 仅 migrations/016 |
| limit_up_pool | limit_up_pool | ❌ 仅 migrations/016 |
| announcements | announcements | ❌ 仅 migrations/017 |
| news | news | ❌ 仅 migrations/017 |
| hot_search | hot_search | ❌ 仅 migrations/017 |
| global_ohlcv | global_ohlcv | ❌ 仅 migrations/018 |

**这 11 类数据不是死代码**——摄取链路已实盘接线：

- [pkg/data/source/adapter.go#L47-L58](../../../pkg/data/source/adapter.go#L47-L58) 定义全部 DataType 常量；
- [yahoo_finance_adapter.go#L44-L72](../../../pkg/data/source/yahoo_finance_adapter.go#L44-L72) 产 `global_ohlcv`、[xueqiu_adapter.go#L41-L80](../../../pkg/data/source/xueqiu_adapter.go#L41-L80) 产 `hot_search`/`news`，mootdx/eastmoney 产 realtime/sector/top_list 系；
- [registry_init.go#L171-L207](../../../cmd/data/registry_init.go#L171-L207) 为全部类型 `SetChain` 注册；
- [pkg/data/source/etl.go#L113-L123](../../../pkg/data/source/etl.go#L113-L123) `p.Store.BulkInsert(ctx, req.DataType, ...)` 落库。

**执行路径事实**：postgres.go L86-L96 注释明确——`migrations/` 与 `docs/migrations/` **不被执行**，golang-migrate 封装 2026-09-18 已删除；唯一 DDL 真相是内联数组。但 11 张表从未被追加进去，**违反了该项目自己写下的约定**（"加表/加列就在这个数组末尾追加一条"，L94-L96）。

**影响推演**：全新部署（新环境 / CI 集成库 / 同事本机）→ 任一多源同步任务触发 → `INSERT INTO realtime_quote ...` → **`relation "realtime_quote" does not exist`**。且 [etl.go#L114-L123](../../../pkg/data/source/etl.go#L114-L123) 错误上抛（不静默），整批同步失败。多源数据面（ODR-011/012 核心成果）在干净环境**开箱不可用**。存量库靠手工跑过 .sql 才活着——这是隐性运维债。

**修复建议**：

1. 将 11 张表的 DDL 从 migrations/015~018 移植为内联数组末尾追加（编号续 028+，幂等 `CREATE TABLE IF NOT EXISTS`）；
2. **加一道一致性断言测试**（一次性根治此类漂移）：

```go
func TestTableMapperTablesExistInInlineDDL(t *testing.T) {
    mapper := NewTableMapper()
    ddl := readInlineMigrateSource(t) // 或对测试库执行 migrate() 后查 information_schema
    for _, table := range mapper.allTables() {
        require.Contains(t, ddl, table, "TableMapper 目标表未在内联 DDL 中创建: "+table)
    }
}
```

**验收标准**：新库（docker compose down -v && up）启动后，11 类数据各自的同步任务可成功落库 ≥1 行。

---

## 5. High 发现

### H1 印花税默认值是实际的 2 倍

- **位置**: [pkg/fees/ashare.go#L53](../../../pkg/fees/ashare.go#L53)
- **证据**: `DefaultStampTaxRate = 0.001`，注释称"0.2% 减半到 0.1%"——史实错误。实际沿革：2023-08-28 起 **0.1% → 0.05%**，当前应为 `0.0005`。
- **影响**: 高换手策略交易成本被系统性高估约一倍（仅卖方，但量化组合年换手常达 10-50 倍）→ 成本敏感的真因子被误杀、假因子被误留。与 C1 叠加后回测结论双重失真。
- **修复**: 改 `0.0005`；注释写清费率沿革（含生效日期）以便未来再调时溯源；补费率史测试用例（按日期分段验证）。
- **验收**: `TestStampTax_20230828` 断言卖出 10 万元收 50 元。

### H2 涨跌停判定缺板块维度

- **位置**: [pkg/backtest/engine_daily.go#L168-L178](../../../pkg/backtest/engine_daily.go#L168-L178)
- **证据**: 仅 Normal(±10%) / New(±20%) / ST(±5%) 三档，无创业板(300/301, ±20%)、科创板(688/689, ±20%)、北交所(±30%) 分档；涨停价未做 0.01 元取整。
- **影响**: 300/688 标的按 10% 判停 → 该拦的没拦（20% 涨幅日被误判可成交）、该放的没放；价格取整缺失导致与真实盘面限价不一致，成交判定边缘错误。
- **修复**: 按 symbol 前缀+名称建立档位函数 `limitPct(symbol, name, board)`；`math.Round(price*100)/100` 取整到分；北交所 30% 一并纳入。
- **验收**: 表驱动测试覆盖 600/000/002/300/688/8 开头 × {Normal, ST, *ST} 的限价矩阵。

### H3 `*ST` 识别永久失效

- **位置**: [pkg/backtest/engine.go#L1503-L1509](../../../pkg/backtest/engine.go#L1503-L1509)
- **证据**:

```go
prefix := name[:2]           // 只取前 2 字符
// "ST" 命中；"*ST"(3字符)/"SST"(3)/"S*ST"(4) 永远匹配不到
```

- **影响**: `*ST` 股（退市风险警示，±5% 档）被当作 Normal（±10%）或落到其他档 → 涨跌停判定与真实规则不符；*ST 恰是高波动、高信号价值的样本。
- **修复**: 改用 `strings.HasPrefix` 多模式依序匹配：`"*ST"` → `"S*ST"` → `"SST"` → `"ST"`；或 `regexp: ^\*?S?\*?ST`。
- **验收**: `{"*STXYZ.SH", true}` 等 4 类前缀全部正确。

### H4 测试把 H3 的 bug 固化为预期

- **位置**: [pkg/backtest/engine_accessors_test.go#L286-L290](../../../pkg/backtest/engine_accessors_test.go#L286-L290)
- **证据**: `{"*STXYZ.SH", false}` —— 断言 *ST 返回 false，把 bug 写成了规格。
- **影响**: 修 H3 时该测试会红，容易被误判为"回归"而回滚正确修复。**这是测试反模式的典型样本**（AGENTS.md §8 规范 1 明确要求测试覆盖边界条件，此测试做到了覆盖却固化了错误行为）。
- **修复**: 与 H3 **同一 commit** 修正断言（原子性要求）。

### H5 MockTrader 在 RLock 下经指针写共享对象

- **位置**: [pkg/live/mock_trader.go#L44](../../../pkg/live/mock_trader.go#L44)、[#L301-L338](../../../pkg/live/mock_trader.go#L301-L338)
- **证据**:

```go
positions map[string]*PositionInfo    // L44 — 存指针
func (m *MockTrader) GetPositions(...) {   // L301-315, RLock 内
    pos.CurrentPrice = ...            // 经指针写共享对象
}
func (m *MockTrader) GetAccount(...) {     // L317-338, RLock 内同类写
```

- **定性说明**（重要）：这是**确证的数据竞争**（`-race` 必报），但两个并发读者写入的是**同一确定值**（同一 PriceProvider 的同一快照），非"撕裂写"，实际业务危害低于字面严重性。原子代理报 Critical，经复核**降级为 High**。
- **修复**: `GetPositions`/`GetAccount` 内改 `Lock()`；或更优——构造值拷贝返回（`info := *pos; info.CurrentPrice = new; append`），读路径零写锁。
- **验收**: `go test ./pkg/live/... -race` 通过。

### H6 无整手（100 股）取整

- **位置**: pkg/backtest（下单/仓位计算路径）
- **置信度**: ◐ 负向证据——D2 审查未找到 lot-size 归一逻辑（负向结论存在漏看可能，需正向确认）
- **影响**: A 股最小交易单位 100 股，回测中出现 137 股这类不可成交仓位 → 成交额/费用/收益率与真实执行脱节；小市值股偏差更大。
- **修复**: 信号量 → 下单量处加 `qty = qty / 100 * 100`（并处理 qty<100 时跳过）；费用计算与该取整共用同一常量。
- **验收**: 测试断言任意权重 → 下单量恒为 100 的倍数。

### H7 Windows 下沙箱资源限制为 no-op

- **位置**: [internal/sandbox/runner/rlimit_posix.go#L76-L105](../../../internal/sandbox/runner/rlimit_posix.go#L76-L105)、[rlimit_linux.go#L18](../../../internal/sandbox/runner/rlimit_linux.go#L18)、[runner_test.go#L63](../../../internal/sandbox/runner/runner_test.go#L63)
- **证据**: `Setrlimit`（CPU/AS/NOFILE/FSIZE/NPROC）全部在 posix/linux 文件；测试对 windows 直接 skip。本仓开发机即 Windows。
- **影响**: 开发机上 AI 生成代码（copilot/expression 编译执行）无 CPU/内存/文件上限——死循环即打满主机；生产 Linux 不受影响但开发体验与安全测试不等价。
- **修复**: Windows 用 Job Object（`CreateJobObject` + `JOB_OBJECT_LIMIT_*`）；或务实方案——无 rlimit 能力时**拒绝执行**并明确报错（fail-closed），而不是默默无限制运行。
- **验收**: Windows 上提交死循环代码，5s 内被终止。

### H8 CI 未执行项目自己的强制规范

- **位置**: [.github/workflows/ci.yml#L47-L48](../../../.github/workflows/ci.yml#L47-L48)
- **证据**:

```yaml
- name: Test
  run: go test ./... -count=1        # 无 -race
```

且整个 workflow 无前端 job（`npm test` / `npm run typecheck` / `npm run lint` 全缺）。

- **影响**: AGENTS.md §8 规范 1 明文要求 `go test ./... -count=1 -race` 通过、前端 `npm test` 通过——**CI 是规范的唯一执法者，但它自己不执法**。H5 这类数据竞争因此从未被 CI 捕获。前端无门禁则 §6 规范（shallowRef/any 禁令）全靠自觉。
- **修复**: Test 步骤改 `go test ./... -count=1 -race`；新增 frontend job（node 20 → npm ci → lint + typecheck + test）；可选加 `gofmt -l .` 检查。
- **验收**: 故意提交一个含数据竞争的 PR，CI 红。

### H9 docker-compose 把 PG/Redis 暴露到宿主机所有接口

- **位置**: [docker-compose.yml#L27-L28](../../../docker-compose.yml#L27-L28)、[#L39-L40](../../../docker-compose.yml#L39-L40)
- **证据**:

```yaml
ports:
  - "5432:5432"     # postgres → 0.0.0.0
ports:
  - "6379:6379"     # redis → 0.0.0.0（无密码）
```

- **影响**: 局域网内任何主机可直接连 PostgreSQL 与 Redis（Redis 未设 requirepass）→ 全量行情/回测数据可读写；结合 Redis 未授权可尝试写 crontab 等经典攻击。
- **修复**: 改 `"127.0.0.1:5432:5432"` / `"127.0.0.1:6379:6379"`（本地调试语义不变）；或在 override 文件中处理；Redis 加 `requirepass`。
- **验收**: 宿主机外主机 `nc -zv <ip> 5432` 失败。

---

## 6. Medium 发现

### M1 AGENTS.md 架构描述落后于代码现实

- **证据**（2026-09-21 实测）:
  - AGENTS.md v3.3（2026-09-15）仍以 ADR-022「四层 L0-L3 / 双工作面 / equitydeep-research 容器待建」为顶层定义；但 [docs/adr/](../../../docs/adr) 现有 [adr-023-ai-experimenter-lab.md](../../../docs/adr/adr-023-ai-experimenter-lab.md) 与 [adr-024-expression-as-execution-target.md](../../../docs/adr/adr-024-expression-as-execution-target.md) 后出，ADR-021/022 已移入 [docs/archive/superseded-adr/](../../../docs/archive/superseded-adr)——架构基线已迁移，AGENTS.md 未跟上；
  - AGENTS.md §3 称 "`ingest.raw` / `research` schema … 尚未落盘"，实际 [postgres.go#L281-L318](../../../pkg/storage/postgres.go#L281-L318) 已创建 `ingest.raw`、`research.profile/conclusion/question`——"规划中"标注过期；
  - AGENTS.md 称 "共 22 条 ADR：ADR-001~022"，实际存在 ADR-024。
- **影响**: AI 编码助手与新人以 AGENTS.md 为第一入口（本仓设计的核心机制），文档漂移会系统性误导后续开发——C5 正是"文档/注释约定与执行不一致"的同类病。
- **修复**: 按 ADR-023/024 重写 AGENTS.md §1/§2/§3 顶层叙述；建表状态改为"已落盘清单 + 未落盘清单"。

### M2 AGENTS.md 表数口径错误（"38 张活跃表"）

- **证据**: AGENTS.md §11 CR-47 称 "38 张活跃表 = 内联 20 张 + 迁移新增 18 张"。grep `CREATE TABLE IF NOT EXISTS` 实测内联 **22 张**；且迁移目录不被执行（C5），"迁移新增 18 张"的口径不成立。
- **修复**: 改为"内联 DDL 22 张（唯一执行路径）"；删除对未执行迁移的计数。

### M3 双 migrations 死文档持续制造歧义

- **位置**: [migrations/](../../../migrations)、[docs/migrations/](../../../docs/migrations)
- **证据**: [postgres.go#L82-L96](../../../pkg/storage/postgres.go#L82-L96) 注释确认两目录均不被执行、只读不维护；[postgres_test.go#L516-L532](../../../pkg/storage/postgres_test.go#L516-L532) 测试注释亦标注"文档副本"。但 13 个 .sql 仍在仓库中被任务/文档引用（C5 的根因即开发时引用了 015-018）。
- **修复**: 二选一——(a) 归档到 `docs/archive/migrations-history/` 并加 stale 横幅；(b) 若短期无法补 DDL（C5），至少在目录加 README："此目录不执行，加表请改 postgres.go"。(a) 更彻底。

### M4 staticcheck 为正则黑名单，可被绕过

- **位置**: [internal/sandbox/staticcheck/staticcheck.go#L113-L185](../../../internal/sandbox/staticcheck/staticcheck.go#L113-L185)
- **证据**: 14 条 `regexp.MustCompile` 匹配 `os.Remove(`、`exec.Command(` 等字面量。经包别名（`import x "os"`）、变量间接调用、反射、字符串拼接构造可绕过。
- **定性**: 防御纵深的一层（配合 C3 修复后风险可控），但应明示其威胁模型：防 AI 生成代码的无意违规，**不防**有意的攻击者。
- **修复**: 注释声明威胁模型；中期可引入 `gosec` 或基于 go/ast 的分析替代正则（AST 层可识别别名调用）。

### M5 死 E2E 测试：ai-research.spec.ts 指向已删服务

- **位置**: [e2e/tests/ai-research.spec.ts#L76-L88](../../../e2e/tests/ai-research.spec.ts#L76-L88)
- **证据**: 打 `http://localhost:8086/api/ai/*`，而 `cmd/ai` 已于 2026-09-18 删除（TASKS P2-5），:8086 无监听。
- **影响**: 该 spec 恒失败或恒超时——CI 若跑全量 E2E 则一直红/被跳过，掩盖真实回归；不跑则测试目录存在死代码。
- **修复**: 删除该 spec；AI 能力的 E2E 改走 `/api/tools/*`（ODR-046 新主路径）。

### M6 `fundamentals_detail` 表已建但摄取链路未通

- **位置**: [pkg/storage/postgres.go#L333](../../../pkg/storage/postgres.go#L333)（表存在）；摄取侧 EQD-P1-2 未完成（AGENTS.md §14 已知问题，本次复核确认表结构在、数据摄取代码未见）
- **影响**: 纵向因子若直接查该表会**静默拿到空集**（SQL 不报错）→ 因子全空/NaN，易被误判为"策略无效"。
- **修复**: 短期在因子读取处加空集防御（行数 0 时显式报错或告警）；中期完成 EQD-P1-2 摄取。

---

## 7. Low 发现

| # | 问题 | 证据 / 位置 | 说明 |
|---|------|------------|------|
| L1 | `main` 领先 `origin/main` 51 提交 | git 实测（2026-09-21） | 违反 AGENTS.md §8 规范 3"不直接提交 main、使用 feature branch + PR"。单点丢失风险 + 评审缺失。建议尽快推送并恢复流程 |
| L2 | live engine 组合状态更新路径存在不触发场景 | D4 子代理报告，**未逐行复核** | 待复核后升级或关闭；见附录 B 未覆盖项 |
| L3 | legacy HTML UI 残留 `cmd/analysis/static/` | AGENTS.md §14 已登记 | 已标 deprecated；建议给出删除时间表而非无限期共存 |
| L4 | 文档导航失效与 ADR 编号漂移 | 2026-09-21 实测：`docs/odr/` **不存在**（全部 ODR 实际位于 [docs/archive/odr/](../../../docs/archive/odr)），AGENTS.md §10 Rule 2 仍引导在 `docs/odr/` 创建 ODR；ADR-023/024 存在但 AGENTS.md 导航只到 022 | 新 ODR 会创建到错误位置或需中途改路径。按 ADR-023 口径统一修正导航 |

---

## 8. 正面发现（值得保留与推广的工程实践）

1. **认证层 fail-closed 设计扎实**——[cmd/analysis/setup.go#L617-L629](../../../cmd/analysis/setup.go#L617-L629)：CORS 白名单回显（不再硬编码 `*`，未配置=不回显）、全局 JWT 中间件；[config/analysis-service.yaml#L19-L25](../../../config/analysis-service.yaml#L19-L25)：`allow_insecure` 仅 loopback 生效、非 loopback 监听时豁免无效并打 WARN。注释里甚至预判了"0.0.0.0 上 open-access 等于把下单接口开给整个局域网"——安全意识在线，缺的只是授权接线（C4）。

2. **DDL 单一真相的取舍有书面论证**——[pkg/storage/postgres.go#L76-L96](../../../pkg/storage/postgres.go#L76-L96)：明确写了为什么不用 golang-migrate（baseline 对不上则后续每条迁移都在错误假设上跑；"为了工程整洁去动能用的库，收益是洁癖，风险是数据"），并给出"加表追加到数组末尾"的可执行约定。C5 是**执行没跟上约定**，不是设计缺陷——这比没有约定好得多。

3. **order_manager.go 并发是正确范本**——[pkg/live/order_manager.go#L212-L225](../../../pkg/live/order_manager.go#L212-L225)：`syncOrderStatus` 先 RLock 拷贝 pending 再释放锁调 broker（copy-under-lock）；全文件 42 处共享字段访问全部在锁内。与 mock_trader.go（H5）形成同仓对比，说明团队掌握正确写法，H5 是疏漏而非能力缺口。

4. **ETL 错误不静默**——[pkg/data/source/etl.go#L114-L123](../../../pkg/data/source/etl.go#L114-L123)：`BulkInsert` 失败 `fmt.Errorf("etl: persist %s: %w", ...)` 上抛而非 log-and-continue。这让 C5 的故障模式是"响亮失败"而非"静默丢数"。

5. **测试规模真实**——61,321 行测试 / 230 文件，历史覆盖率 risk 85.3%、compliance 91.6%、fees 100%、marketdata 79.0%。发现的测试问题（H4、M5）是质量缺口而非"测试是摆设"。

6. **CI 有自研一致性元检查**——[.github/workflows/ci.yml#L64-L70](../../../.github/workflows/ci.yml#L64-L70)：`check_doc_links.py`（文档死链）+ `check_deploy_consistency.py`（compose ↔ k8s 漂移）。这类"检查检查工具"的元实践很少见。建议未来把 C5 的一致性断言做成同类元检查。

---

## 9. 误报剔除记录（交叉验证的价值证明）

| 子代理原始结论 | 复核结果 | 处置 |
|---------------|---------|------|
| "`contracts/` 契约目录不存在（D1）" | **误报**。Glob 实测存在 4 个文件：`profile.schema.json`、`snapshot.schema.json`、`fundamentals_detail.schema.sql`、`field_dictionary.yaml` | 剔除 |
| "MockTrader 撕裂写，Critical（D4）" | **降级 High（H5）**。确证数据竞争，但两读者写入同一确定值（同一 PriceProvider），无数据损坏 | 降级并改写定性 |
| "`order_manager.go` 锁外读共享字段（D4）" | **误报**。42 处访问逐条核对均在锁内，`syncOrderStatus` 是标准 copy-under-lock | 剔除 |
| "ETL 写不存在表 → 静默丢数（D3）" | **修正**。[etl.go#L114-L123](../../../pkg/data/source/etl.go#L114-L123) 错误上抛，是响亮失败非静默丢失（危害仍在——开箱不可用——但故障模式不同，影响排障方式） | 修正表述后保留为 C5 |

---

## 10. 修复路线图

### 第一批：阻断研究结论可信度 + 安全（建议立即）

| 顺序 | 项 | 理由 |
|------|-----|------|
| 1 | **C3** /api/copilot/save | 改动最小（白名单+staticcheck 或删端点）、风险最高（RCE） |
| 2 | **C4** RBAC 接线 | `RequireRole`/`CanTrade()` 已写好，纯接线工作；修复后 C3 的暴露面进一步收窄 |
| 3 | **C1** 日收益率公式 | 修完**必须重跑历史回测，此前因子筛选结论作废重评** |
| 4 | **C2** walk-forward 独立 runner | 与 C1 同属验证体系，一并修复后统一重跑报告 |

### 第二批：数据面可用性

| 顺序 | 项 | 理由 |
|------|-----|------|
| 5 | **C5** 11 张表 DDL 补齐 + 一致性断言测试 | 一次性根治迁移漂移类问题 |
| 6 | **H9** compose 端口改 loopback | 一行改动 |
| 7 | **H8** CI 补 -race + 前端 job | 先建立防线，再修防线要抓的问题（H5） |

### 第三批：A 股规则正确性（同批原子提交）

| 顺序 | 项 | 备注 |
|------|-----|------|
| 8 | **H1** 印花税 0.0005 | 独立可测 |
| 9 | **H2** 板块涨跌停分档 + 取整 | 含表驱动测试 |
| 10 | **H3+H4** *ST 识别 + 测试断言 | **必须同 commit**（H4 固化了 H3 的 bug） |
| 11 | **H6** 整手取整 | 先正向确认现状再修 |
| 12 | **H5** MockTrader 锁语义；**H7** Windows rlimit | H5 依赖 H8 的 -race 验证 |

### 第四批：文档与流程对齐

| 顺序 | 项 |
|------|-----|
| 13 | **M1/M2/M3** AGENTS.md 按 ADR-023/024 重写、表数口径改 22、migrations 归档 |
| 14 | **M4/M5/M6** staticcheck 威胁模型注释、删死 spec、fundamentals_detail 空集防御 |
| 15 | **L1** 推送 51 提交 + 恢复 feature branch；**L4** 文档导航修正 |

---

## 11. 附录 A：建议登记到 TASKS.md 的任务条目

> 建议编号 AUD-01 ~ AUD-15，每条含验收标准，符合 AGENTS.md §8 执行规范（测试先行 / 代码审查 / 原子提交）。

| ID | 等级 | 任务 | 验收标准（摘要） | 原子提交范围 |
|----|------|------|------------------|--------------|
| AUD-01 | C | 删除或加固 /api/copilot/save | 遍历名 400 / 危险代码 422 / 渗透用例全拒 | cmd/analysis + 测试 |
| AUD-02 | C | /api/execution 与 /api/tools 挂 RequireRole | viewer 下单 403；工具按副作用分级 | cmd/analysis + pkg/tools |
| AUD-03 | C | CalculateReturns 改 TotalValue 差分 | 买入不动日收益=0；买入持有与标的日收益一致 | pkg/backtest/metrics |
| AUD-04 | C | walk-forward 每窗口独立 runner | 并发窗口缓存隔离测试；-race 通过 | pkg/backtest/walkforward + cmd/analysis |
| AUD-05 | C | 11 张表 DDL 补进内联 migrate() | 新库全类型同步落库成功；mapper↔DDL 一致性测试 | pkg/storage |
| AUD-06 | H | 印花税默认值改 0.0005 | 2023-08-28 后卖 10 万收 50 元 | pkg/fees |
| AUD-07 | H | 涨跌停板块分档 + 分取整 | 600/000/002/300/688/8×档位矩阵测试 | pkg/backtest |
| AUD-08 | H | *ST 识别修复 + 测试断言修正 | 4 类前缀全对；与 AUD-07 分开但各自原子 | pkg/backtest + 测试 |
| AUD-09 | H | 整手（100 股）取整 | 下单量恒为 100 倍数 | pkg/backtest |
| AUD-10 | H | MockTrader 值拷贝替代锁内写 | pkg/live -race 通过 | pkg/live |
| AUD-11 | H | Windows 沙箱 fail-closed 或 Job Object | Windows 死循环 5s 内终止 | internal/sandbox |
| AUD-12 | H | CI 加 -race + 前端 job | 含竞争的 PR CI 红 | .github/workflows |
| AUD-13 | H | compose 端口绑 loopback + Redis 密码 | 外部主机 5432/6379 不可达 | docker-compose |
| AUD-14 | M | AGENTS.md 按 ADR-023/024 重写 + 表数 22 + migrations 归档 | 导航链接全通过 check_doc_links | docs |
| AUD-15 | M | 删 ai-research.spec.ts；M6 空集防御 | E2E 全绿；空表显式报错 | e2e + pkg/ai |

---

## 12. 附录 B：审查覆盖范围

| 区域 | 覆盖度 | 说明 |
|------|--------|------|
| pkg/backtest（engine/metrics/walkforward/fees） | 深 | C1/C2/H1-H4/H6 均在此 |
| pkg/storage（postgres/bulk_insert） | 深 | C5/M2/M3 逐行核对 |
| pkg/live（mock_trader/order_manager） | 中 | H5 逐行；engine.go 组合状态仅子代理报告（L2） |
| pkg/data/source（registry/etl/adapters） | 中 | C5 链路核对；reconcile/背压未深审 |
| pkg/auth + cmd/analysis 路由 | 中 | C3/C4 认证授权面 |
| internal/sandbox | 中 | M4/H7 |
| docker-compose / CI | 中 | H8/H9；K8s manifests 未逐行 |
| pkg/ai/*（intent/pipeline/search/evolution…） | 浅 | 抽查；其上游依赖 C1/C2，修复后需专项复审 |
| web/src 前端 | 浅 | D7 抽查规范符合度；未发现 Critical |
| e2e/tests | 中 | M5 确证；其余 spec 未逐行 |
| migrations/ + docs/migrations | 深 | C5 求差集 |
| 文档（AGENTS/ADR 导航） | 中 | M1/M2/L4 实测核对 |

---

## 13. 附录 C：文档漂移清单（全部实测验证）

| # | AGENTS.md 声明 | 实测现实 |
|---|---------------|---------|
| 1 | 顶层定义 = ADR-022 四层 L0-L3 双工作面 | ADR-021/022 已移入 `docs/archive/superseded-adr/`；`docs/adr/` 现有 ADR-023、ADR-024 |
| 2 | "共 22 条 ADR：ADR-001~022" | ADR-024 存在 |
| 3 | "`ingest.raw` / `research` schema 尚未落盘（规划中）" | postgres.go L281/L305/L318 已创建 `ingest.raw`、`research.profile/conclusion/question` |
| 4 | "38 张活跃表 = 内联 20 + 迁移 18" | 内联实测 22 张；迁移目录不被执行 |
| 5 | ODR 目录为 `docs/odr/`（Rule 2 引导在此创建） | `docs/odr/` 不存在；ODR 实际位于 `docs/archive/odr/` |
| 6 | `migrations/` 为活跃迁移目录（§3 目录树） | postgres.go L82-L96 注释明确不被执行、只读不维护 |

---

## 14. 附录 D：建议的动态验证清单（在有 Go 工具链的环境执行）

1. `go build ./...` — 确认编译健康；
2. `go vet ./...` + `gofmt -l .` — 静态分析基线；
3. `go test ./... -count=1 -race` — **重点预期**：`pkg/backtest/walkforward`（C2）与 `pkg/live`（H5）报竞争；
4. `go test ./pkg/fees/ -run StampTax -v` — 观察现行费率断言（H1 修复前该测试可能"通过"地固化着 0.001）；
5. 构造复现脚本验证 C1：买入不动日收益是否非 0；
6. 新环境 `docker compose down -v && up` 后触发任一多源同步，确认 C5 的 `relation does not exist` 复现；
7. `cd web && npm test && npm run typecheck` — 前端基线。

---

## 15. 后续动作建议

按 AGENTS.md §10 Rule 2（审计类工作 48h 内 ODR）：

1. 创建 ODR 归档本次审计（Category: Audit），并更新 `docs/ADR.md` 的 ODR Index（注意 L4：ODR 目录按现实应为 `docs/archive/odr/` 或先修导航）；
2. 将 §11 的 AUD-01~15 登记到 `docs/TASKS.md`；
3. Critical 修复优先走 feature branch + PR（同时解决 L1 的 51 提交问题）。

---

*审查方法与全部证据行号均可回溯；本报告的过程性草稿（原误存于本文件）已由本正式报告替换。*

---

## 16. 修复实施方案（2026-09-21 增补）

> 落地前已核实现行签名：`staticcheck.CheckOrError(code string) error`（[staticcheck.go#L210](../../../internal/sandbox/staticcheck/staticcheck.go#L210)）、`backtest.NewEngine(v *viper.Viper, provider marketdata.Provider, logger zerolog.Logger)`（[engine.go#L171](../../../pkg/backtest/engine.go#L171)）、`ToolInfo` 位于 `pkg/tools/tool.go#L112`（无副作用字段；AGENTS.md 所写 `pkg/tools/server.go` 不存在，属又一处文档漂移）。

### 16.0 全局约定

- **流程**：全部修复走 feature branch（建议 `fix/odr-065-audit-batchN`）+ PR，不直接提交 main（§8 规范 3）；**前置动作：先推送 main 现有 51 提交**（L1），否则 PR 基线混乱。
- **纪律**：每任务 = 一个原子 commit；测试先行（先写失败测试再修）；commit message 按 §8 格式带 `Refs: AUD-xx`。
- **每次提交前门禁**：`go build ./... && go vet ./... && gofmt -l . && go test ./... -count=1 -race`（-race 在 Commit 7 落地前先在本地执行）。

### 16.1 需用户裁决的三个前置决定

| # | 决定点 | 默认方案 | 备选 |
|---|--------|---------|------|
| D-1 | C3 `/api/copilot/save`：加固还是删除 | **加固**（保留功能，补三道闸） | 删除端点更彻底（API 变更，需确认前端/Hermes 无调用方） |
| D-2 | H7 Windows 沙箱：fail-closed 还是完整 Job Object | **fail-closed**（拒绝执行并明确报错） | `golang.org/x/sys/windows` Job Object（工作量大，列后续增强） |
| D-3 | H9 Redis 是否加密码 | **仅先绑 loopback**（零行为变化） | `requirepass` 牵连全部服务的 `REDIS_URL`，另立任务 |

### 16.2 Batch 1 — 安全 + 度量根基（4 commits）

#### Commit 1（AUD-01 / C3）加固 saveStrategyHandler

**文件**：[cmd/analysis/handlers_copilot.go](../../../cmd/analysis/handlers_copilot.go)

```go
// 包级新增
var strategyNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

// saveStrategyHandler 内，BindJSON 之后：
if !strategyNameRe.MatchString(req.StrategyName) {
    httpserver.Fail(c, http.StatusBadRequest, "invalid strategy name")
    return
}
// staticcheck 黑名单闸（复用 generate 路径同款，fail-closed 便利函数已存在）
if err := staticcheck.CheckOrError(req.Code); err != nil {
    httpserver.Fail(c, http.StatusUnprocessableEntity, err.Error())
    return
}
// 路径拼接改 filepath.Join，杜绝分隔符/遍历字面量
filePath := filepath.Join("./pkg/strategy/plugins", "strategy_"+req.StrategyName+".go")
```

**测试（先行）**：新建 `cmd/analysis/handlers_copilot_test.go`
1. `TestSaveStrategy_RejectsPathTraversal`：`StrategyName = "../../evil"` → 400；
2. `TestSaveStrategy_RejectsDangerousCode`：`Code` 含 `exec.Command(` → 422；
3. `TestSaveStrategy_AcceptsCleanStrategy`：合法代码 → 200 且文件落在 plugins 目录内。

**波及**：合法调用方若曾用含 `-`/中文的名字会被拒——属预期收紧，发版说明标注。

#### Commit 2（AUD-02 / C4）RBAC 接线

**文件**：[handlers_execution.go](../../../cmd/analysis/handlers_execution.go)、[handlers_tools.go](../../../cmd/analysis/handlers_tools.go)

```go
// execution：三个动作端点挂角色（GET 保持任何已认证用户）
trade := auth.RequireRole(auth.RoleTrader, auth.RoleAdmin)
execGroup.POST("/orders", trade, h.createOrder)
execGroup.POST("/orders/:id/cancel", trade, h.cancelOrder)
execGroup.POST("/emergency-flatten", trade, h.emergencyFlattenHandler)
// legacy 根路径（/orders、/orders/:id/cancel）同样挂 trade —— 两处都挂，否则旁路
```

```go
// tools：handler 侧副作用分级（fail-closed：未登记 = admin）
var toolMinRole = map[string]auth.Role{ /* 只读工具 → RoleViewer；写/执行 → RoleTrader */ }
// handleExecute 开头：
minRole, ok := toolMinRole[name]
if !ok { minRole = auth.RoleAdmin }
// 已认证用户 role < minRole → 403
```

**实施时必须验证的一处**：[setup.go#L626-L629](../../../cmd/analysis/setup.go#L626-L629) 只在 `authSvc.Enabled()` 时挂 Middleware；auth disabled（本地 dev）时 RequireRole 不应生效。方案：RequireRole 检查 context 中无认证链标记时直接放行，或在 disabled 分支改挂 no-op 版本——以 `pkg/auth` 现有测试（`TestRequireRole_NoUser_401`）为准做兼容设计。

**测试**：viewer 下单 403 / trader 下单 200 / viewer 调写型工具 403 / 调只读工具 200 / auth disabled 全放行 / legacy 路径与 `/api` 路径行为一致。

#### Commit 3（AUD-03 / C1）日收益率公式

**文件**：[pkg/backtest/metrics/performance.go#L84-L97](../../../pkg/backtest/metrics/performance.go#L84-L97)

```go
// CalculateReturns calculates daily returns from portfolio values.
// 回测是封闭系统（无外部申赎），日收益 = TotalValue 直接差分。
func CalculateReturns(portfolioValues []domain.PortfolioValue) []float64 {
    if len(portfolioValues) < 2 {
        return nil
    }
    returns := make([]float64, 0, len(portfolioValues)-1)
    for i := 1; i < len(portfolioValues); i++ {
        prevValue := portfolioValues[i-1].TotalValue
        currValue := portfolioValues[i].TotalValue
        if prevValue > 0 {
            returns = append(returns, (currValue-prevValue)/prevValue)
        }
    }
    return returns
}
```

**测试（先行，先红后绿）**：
1. `TestCalculateReturns_BuyNoPriceChange_ZeroReturn`：T0 现金 100k → T1 现金 0 + 持仓 100k（价格未动）→ 断言 `returns[0] == 0`（**修复前该测试必红**，即 bug 复现测试）；
2. `TestCalculateReturns_BuyAndHold_MatchesUnderlying`：标的 10/11/12 → 日收益 0.1 / 0.0909…；
3. `TestCalculateReturns_SellRoundTrip_NoPhantomReturn`。

**波及面（关键）**：
1. grep `CalculateReturns(` 全部调用点逐一确认（含 pkg/ai/metrics、walkforward 报告面）；
2. 修复后 Sharpe/Sortino 数值变化 → 存量期望值断言需同步更新（跑 `go test ./pkg/backtest/... ./pkg/ai/...` 找断言漂移）；
3. **历史回测报告与 walk-forward 报告作废，修复合入后全量重跑**——此点写入 PR 描述与 TASKS 验收项。

#### Commit 4（AUD-04 / C2）walk-forward runner 工厂化

**文件**：[pkg/backtest/walkforward/walkforward.go](../../../pkg/backtest/walkforward/walkforward.go)、[cmd/analysis/setup.go#L342](../../../cmd/analysis/setup.go#L342)

```go
// 字段与构造器：共享实例 → 工厂
type WalkForwardEngine struct {
    runnerFactory func() (contracts.EngineRunner, error)
    store         storage.Storage
    // ...
}
func NewWalkForwardEngine(factory func() (contracts.EngineRunner, error), store storage.Storage) *WalkForwardEngine

// 并发窗口循环内（原 L142-160），每窗口：
runner, err := we.runnerFactory()
if err != nil { /* 该窗口 fail，不污染其他窗口 */ }
```

```go
// setup.go：闭包捕获与主 engine 相同的构造参数（NewEngine 签名已核实）
wfEngine := backtest.NewWalkForwardEngine(func() (contracts.EngineRunner, error) {
    return backtest.NewEngine(v, provider, logger)
}, store)
```

同步更正"窗口之间互不共享状态"注释。**波及**：grep `NewWalkForwardEngine(` 其他调用点（测试/文档示例）同步更新。

**测试**：`TestWalkForward_WindowsIsolated`——stub 工厂返回带实例 ID 的 runner，断言每窗口拿到独立实例；`-race` 下两窗口写同名缓存互不可见。

### 16.3 Batch 2 — 数据面 + 门禁（3 commits）

#### Commit 5（AUD-05 / C5）11 张表 DDL 内联移植 + 漂移断言

- **postgres.go**：`migrations` 数组末尾追加 11 条 `CREATE TABLE IF NOT EXISTS`（逐字移植 `migrations/015~018` 的 DDL，确保幂等形态；编号注释续 `// Migration 028:` ~ `// Migration 038:`，遵守 postgres.go L94-96 自定约定）；
- **新测试 `pkg/storage/bulk_insert_ddl_test.go`**（零 DB 依赖）：

```go
func TestTableMapper_TablesDeclaredInInlineDDL(t *testing.T) {
    src, _ := os.ReadFile("postgres.go") // 静态读源码，无 DB 也可跑
    declared := extractCreateTableNames(src) // regex: CREATE TABLE IF NOT EXISTS (\S+)
    for _, table := range NewTableMapper().allTables() {
        assert.Contains(t, declared, table, "TableMapper 目标表未在内联 DDL 声明: "+table)
    }
}
```

有 DB 环境下可选集成验证：`testutil/testdb.go` migrate 后查 `information_schema.tables`。

#### Commit 6（AUD-13 / H9）compose 端口绑定

[docker-compose.yml](../../../docker-compose.yml)：`"5432:5432"` → `"127.0.0.1:5432:5432"`、`"6379:6379"` → `"127.0.0.1:6379:6379"`。**验证**：容器启动后从宿主机 `Test-NetConnection localhost -Port 5432` 通、同网段另一主机探测不通。Redis 密码（D-3）另立任务。

#### Commit 7（AUD-12 / H8）CI 门禁

[ci.yml](../../../.github/workflows/ci.yml)：
1. Test step → `go test ./... -count=1 -race`；
2. 新增 frontend job：node 20 → `npm ci` → `npm run lint` → `npm run typecheck` → `npm test`；
3. 可选：`gofmt -l .` 非空即 fail。

**顺序约束**：本 commit 放在 Commit 4（walkforward 隔离）与 Commit 12（MockTrader）之后，避免 `-race` 上线即红。**风险**：首次 `-race` 仍可能暴露未知竞争——按 CI 报告逐个修复，不得回退门禁。

### 16.4 Batch 3 — A 股规则正确性（5 commits）

#### Commit 8（AUD-06 / H1）印花税

[ashare.go#L49-L53](../../../pkg/fees/ashare.go#L49-L53)：`DefaultStampTaxRate = 0.0005`，注释改为 *"0.1% halved to 0.05% effective 2023-08-28 (CSRC [2023] No. 17)"*。**测试**：断言常量 0.0005 + 卖出 100,000 元印花税 = 50 元。**波及**：grep 存量断言 `0.001` 的费用测试——这些测试此前固化了错误值，一并修正。

#### Commit 9（AUD-07 / H2）板块涨跌停分档 + 取整

[engine_daily.go#L168-L178](../../../pkg/backtest/engine_daily.go#L168-L178) 抽出纯函数：

```go
func resolvePriceLimit(symbol, name string, tradeDays int, cfg PriceLimitConfig) float64 {
    if tradeDays < cfg.NewStockDays { return cfg.New }
    if hasSTPrefix(name) { return cfg.ST }        // 用 Commit 10 修复后的 hasSTPrefix
    switch {
    case strings.HasPrefix(symbol, "300"), strings.HasPrefix(symbol, "301"),
         strings.HasPrefix(symbol, "688"), strings.HasPrefix(symbol, "689"):
        return cfg.Board20                        // 新增字段，默认 0.20
    case strings.HasPrefix(symbol, "8"), strings.HasPrefix(symbol, "4"):
        return cfg.Board30                        // 新增字段，默认 0.30（北交所）
    default:
        return cfg.Normal
    }
}
```

上/下限价各做 `math.Round(x*100)/100` 后再参与 `>=`/`<=` 比较（[engine_daily.go#L175-L178](../../../pkg/backtest/engine_daily.go#L175-L178) 的 `limitPrice` 同步取整）。**测试**：表驱动 600/000/002/300/688/830 × {Normal, ST, *ST, 新股}；取整用例 prevClose=10.05 → 上限 11.06。**波及**：`Config` struct + `config/*.yaml` 示例 + SPEC 文档同步。

#### Commit 10（AUD-08 / H3+H4，同一 commit）*ST 识别 + 测试断言修正

[engine.go#L1502-L1508](../../../pkg/backtest/engine.go#L1502-L1508)：

```go
func hasSTPrefix(name string) bool {
    for _, p := range []string{"*ST", "S*ST", "SST", "ST"} {
        if strings.HasPrefix(name, p) {
            return true
        }
    }
    return false
}
```

[engine_accessors_test.go#L286-L290](../../../pkg/backtest/engine_accessors_test.go#L286-L290) 同 commit 修正：`{"*STXYZ.SH", true}`、`{"SSTXXX.SZ", true}`、`{"S*STX.SZ", true}`、`{"平安银行", false}`。**必须与 H3 同 commit**（H4 固化了 H3 的 bug，分开提交会造成中途红灯）。

#### Commit 11（AUD-09 / H6）整手取整

**先正向确认**：grep 下单量/股数换算处，若已有取整则关闭任务（本项为负向证据）。实现：Weight → shares 换算处 `shares = shares / 100 * 100`，`shares < 100` 跳过下单；费用计算与取整共用同一常量 `LotSize = 100`。**测试**：任意权重 → 下单量恒为 100 倍数；边界：零股、不足一手。

#### Commit 12（AUD-10 / H5）MockTrader 值拷贝

[mock_trader.go#L301-L338](../../../pkg/live/mock_trader.go#L301-L338)：`GetPositions`/`GetAccount` 在 RLock 内构造**值拷贝**后再写 `CurrentPrice`（写只发生在局部副本）：

```go
cp := *pos            // 值拷贝，非共享指针
cp.CurrentPrice = mark
out = append(out, cp)
```

**验证**：`go test ./pkg/live/... -race` 全绿（此修复也是 Commit 7 `-race` 门禁的前置）。

#### Commit 13（AUD-11 / H7）Windows 沙箱 fail-closed（按 D-2 默认方案）

[internal/sandbox/runner](../../../internal/sandbox/runner)：执行前探测 rlimit 能力，平台不可用（windows）→ 返回明确错误 `sandbox: resource limits unavailable on this platform; refusing to run untrusted code`。[runner_test.go#L63](../../../internal/sandbox/runner/runner_test.go#L63) 的 windows skip 改为断言该错误。Job Object 完整实现列后续增强任务。

### 16.5 Batch 4 — 文档与清理（2 commits）

#### Commit 14（AUD-14 / M1+M2+M3+L4）文档对齐

- AGENTS.md：§1-§3 按 ADR-023/024 现实重写；表数口径改"内联 DDL 22 张（唯一执行路径）"；`ingest.raw`/`research.*` 改"已落盘"；Rule 2 的 `docs/odr/` → `docs/archive/odr/`；ADR 编号更新；`pkg/tools/server.go` → `pkg/tools/tool.go`；
- `migrations/` + `docs/migrations/`：目录首加 README（"此目录不执行，加表请改 postgres.go migrate() 数组"）；是否物理移动到 `docs/archive/migrations-history/` 属 Cleanup 类操作，另立小 ODR。

#### Commit 15（AUD-15 / M5+M6）测试与防御

- 删除 [e2e/tests/ai-research.spec.ts](../../../e2e/tests/ai-research.spec.ts)（:8086 已随 cmd/ai 删除）；AI 能力 E2E 改走 `/api/tools/*` 另立任务；
- [fundamentals_detail](../../../pkg/storage/postgres.go#L333) 读取处加空集防御：行数 0 → 显式错误/告警，杜绝纵向因子静默拿空集。

### 16.6 顺序依赖与总验证

```
Commit 1 (C3) ─┐
Commit 2 (C4) ─┤─ 独立，可最先
Commit 3 (C1) ─┼─ 合入后触发「历史回测全量重跑」
Commit 4 (C2) ─┘
Commit 5 (C5) → Commit 6 (H9) → Commit 8-10 (H1/H2/H3+4) → Commit 11 (H6) → Commit 12 (H5) → Commit 7 (H8 -race 门禁最后上) → Commit 13 (H7)
Commit 14/15（文档/清理）随时可做
```

**总验收**（全批次合入后）：`go build ./... && go vet ./... && gofmt -l . && go test ./... -count=1 -race` 全绿 + `cd web && npm run lint && npm run typecheck && npm test` 全绿 + CI 全绿 + 新环境 `docker compose down -v && up` 后 13 类数据同步各落库 ≥1 行 + 历史回测/walk-forward 报告重跑完毕并归档旧版本。

