---
status: active
last-verified: 2026-10-14
verified-by: 台账换代归档（2026-10-14）；R1 frontmatter 由 DOCS-02 补齐
---

# TASKS — 未完成项

> **只放未完成项**，完成即标 ✅（附日期）并移入 `archive/`。旧任务文档（Sprint 1–8、AUD-01~62、K0–K18 内核线、NT 决策门等全部完成记录）：[archive/TASKS-2026-10-14-archived.md](archive/TASKS-2026-10-14-archived.md)（2026-10-14 归档，归档后不可变）。
> 状态标记：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `✅ 完成（附日期）`
> 每张 task 必须写全六字段（目标/上下文/要求/约束与非目标/验收/边界），判据见 [AGENTS.md §8.5](../AGENTS.md)：**独立 agent 拿着 task + 它显式引用的冻结契约，不需要读其他 task、不需要问人，就能完成并自证。**

---

## 一、D 系列：大双核 + 数据规范层 + 因子热插拔（ADR-031 决议变更，2026-10-14 立项）

> **设计真相**：quant-trading-docs 仓 `docs/design/data-factor-unification.md`（ADR-031 附件，下称「蓝图」）+ `docs/adr/adr-031-kernel-pluggable-nt-ready.md`「决议变更记录」。开工前先读这两份。⚠️ 相对路径跨仓仅在三仓同布局检出时可解析（见 DOCS-01 排除条件），以「quant-trading-docs 仓 docs/design/data-factor-unification.md」为权威定位。
> **依赖图**：`D1 → D2 → D5`；`D1 → D3 → D4 → D8`；`D1 → D6`（兼 D2）；`D1 → D7`；`D9` 独立。**关键路径**：D1 → D3 → D4 → D8。**开工顺序**：D1 先行 → D2 ∥ D3 → D4 ∥ D5 ∥ D6 ∥ D7 → D8；D9 随时可插。
> **切片包**：D1 + D2 为同一切片包（D2 不必等 D1 全部完成，D1 的 schema 字段定稿即可开工 D2），但**分单验收**——D1 验翻译正确性，D2 验 fail-closed 拒收，验收方向相反。
> **文档同步纪律（强制）**：D 系列任何上层设计变更必须同步 SPEC / data flow / schema（DDL）/ ARCHITECTURE.md，不同步 = 变更未完成（蓝图「维护本文件」节）。

### D1 · 规范层 schema + 按内核翻译层 + 聚合规则成文 ✅ 完成（2026-10-14，最高优先，无依赖）

- **① 目标**：全系统数据取用只认一套「规范层 schema」（频率、时间语义字段自带），connector 之外无人感知底层表；1m→15m→1d 聚合口径含边界细则成文进 SPEC。
- **② 上下文**：蓝图 §4.1/§4.3。现状：`pkg/domain/market/types.go` 的 `OHLCV` **无频率字段**，频率靠表名隐式区分——日线 `ohlcv_daily_qfq`（`trade_date DATE`，Tushare，唯一进回测）、1 分钟 `ohlcv_minute`（`ts TIMESTAMPTZ`，mootdx，只存着），建表 DDL 在 `pkg/storage/postgres.go` `migrationStatements()`（DDL 唯一真相，加表加列在数组末尾追加 `// Migration 0NN:`）。反例分界经验：`capital_flow` 用 `period` 列混表（小量聚合周期可混，量大/时间语义不同必分表）。NT 翻译第一条实现在 nt-sidecar 的 Parquet 导出映射（export_bars）。**为什么做**：规范层是 D2–D8 共同地基，不做它后面每张单都在流沙上。
- **③ 要求**：
  1. **规范层 schema 定稿写入 `docs/SPEC.md`**：OHLCV / 基本面 / 因子三类统一字段，必含 `frequency`（1d / 1m / 15m…）与时间语义字段（可见时间点）；缺失语义明确「缺失≠0」。
  2. **透明路由**：调用方声明「要 15 分钟线」即可取数，不感知存在哪张表；路由覆盖 `ohlcv_daily_qfq` / `ohlcv_minute` 两表现有数据，15 分钟由 1m 聚合产出（或物化，实现自选但口径必须一致）。
  3. **按内核翻译层**：Go 内核直吃规范层；NT 翻译层以 export_bars 映射为第一条实现，「加新数据类 = 映射表加一行」要有测试钉住映射正确性（字段名/时间戳口径/单位）。
  4. **聚合规则成文（含边界细则）**：1m→15m→1d 主口径（开=首笔开、高=期间最高、低=期间最低、收=末笔收、量=期间合计）+ **午休断点、集合竞价、停牌日处理**三细则（原蓝图 §9 #2 的归属地在此，不在 D6）。
  5. **回归**：现有日线回测结果数值不变（改的是取数通道，不是数据）。
- **④ 约束与非目标**：不改回测主循环逻辑、不做分钟级撮合、不做多频率策略视野（D6）、不做 connector 注册契约（D2）、不迁移/改写既有两表的数据内容；schema 由 **Go 侧定义**，不被 NT 数据模型反向污染；数据库 schema 变更只加进 `postgres.go` 内联 DDL 数组（工程红线），不建 migration 文件。
- **⑤ 验收**：
  1. `docs/SPEC.md` 有规范层 schema 章节（三类字段表 + 聚合口径 + 三边界细则），字段清单与 Go 类型一一对应（有防漂移测试或生成式校验）。
  2. 机械判据：`go test ./...` 全绿；新增「同一路由取 1d/15m/1m」集成测试通过；聚合口径手算样本比对测试通过（含午休断点样本）。
  3. **破坏验证**：① 把某字段的 `frequency` 语义改错（如 1m 数据声明 1d）→ 相关测试变红；② 造一条绕过规范层直连 DB 的取数路径 → 被护栏/测试拦住（「不许绕过规范层」必须有执法者，不能只写在文档）；均还原绿。
  4. 文档同步：SPEC.md（schema）、core 仓 `docs/ARCHITECTURE.md`（数据流段）、内核蓝图数据流图如涉及则同步。
- **⑥ 边界**：只做 schema + 路由 + 翻译层 + 聚合规则成文；connector 声明与拒收（D2）、新频率的策略消费（D6）、新数据类接入（D5）另立。
- **✅ 完成记录（2026-10-14）**：规范层在 **Go 侧定稿**（`pkg/domain/market`：`Frequency` 类型 + `Freq1m/Freq15m/Freq1d` + `ParseFrequency` fail-loud；`OHLCV` 新增 `Frequency`/`VisibleAt`；`FactorOutput`/`Fundamental` 时间语义注释成文），NT 侧零污染。**透明路由** `pkg/marketdata.Router.Bars(ctx, freq, symbol, start, end)`：1d→`ohlcv_daily_qfq`、1m→`ohlcv_minute`、15m→1m 聚合（不物化）、未知频率 fail-loud；**聚合口径三细则**成文进 SPEC（午休断点 11:30–13:00 拒收时段外 bar、集合竞价并入首/末 bucket、停牌日无 bar=缺失）。**SPEC.md 新增「规范层 Schema」大节**：三类字段表带 `<!-- drift:* -->` 锚点，`pkg/domain/spec_drift_test.go` 反射比对 Go json tag 双向防漂移（4 测试）。**回归**：`router_integration_test.go` 自灌自证与遗留直查逐字段对拍通过（日线数值不变）。**NT 翻译层**第一条实现 `nt-sidecar/sidecar/mapping.py`（`BAR_FIELD_MAP` 声明式映射，加数据类=加一行）+ `export_bars.py` 消费 + `test_d1_mapping.py` 14 测试（含跨语言字面量契约：Python 读 Go 源码正则双向钉住）。**护栏** `internal/repoguard` 棘轮：AST 扫物理表名字面量，`pkg/marketdata` 外非测试文件只减不增（8 个遗留文件带裁决理由登记基线）。**破坏验证两组均「改坏变红、还原变绿」**：① `loadMinute` 频率声明 Freq1m→Freq1d → `TestRouterSameRouteThreeFrequencies` 红；② `pkg/backtest` 造直查 `ohlcv_daily_qfq` 生产文件 → 护栏点名红。文档同步：SPEC.md + ARCHITECTURE.md §7 数据流图。已知边界：`amount` NULL 折零、15m 不物化、不迁移既有两表数据。

### D2 · connector 注册契约：时间语义 + 频率口径，fail-closed 拒收 ✅ 完成（2026-10-14，与 D1 同一切片包）

- **① 目标**：任何数据源 connector 注册时**必须声明**时间语义（何时可见）与频率口径，声明缺失/非法/与数据形态不符 → 显式拒收，从结构上杜绝前视作弊与数据泄漏。
- **② 上下文**：蓝图 §4.2/§4.4。现有雏形：`cmd/data` 的 registry_init adapter 注册 + kill-switch（缺依赖降级不阻断启动）+ 溯源列 + 幂等重放；现有 connector：Tushare（`pkg/data/tushare.go`）、mootdx（分钟）、宏观（P2-1 先例）、产业链（`pkg/data/supply_chain.go`）。行业依据（蓝图 §8）：Databricks / SageMaker Feature Store 的 point-in-time 机制——泄漏**极难事后发现**（回测曲线照样漂亮），所以必须结构性拒收。**高危场景（用户点名）**：活跃度 / 拥挤度类数据「收盘后才可得」，盘中用当日值即偷看。
- **③ 要求**：
  1. **注册契约 schema 落地**：connector 注册项必含两个声明字段——**时间语义**（枚举：按公告日可见 / 收盘后可见 / 次日开盘可见 / 自定义时点，自定义须给出判定表达）+ **频率口径**（日频 / 分钟频 / 事件流；日内数据如何聚合成日频引用 D1 聚合规则）。
  2. **fail-closed 拒收**：未声明、声明非法、或声明与数据实际时间戳形态矛盾（如声明「次日开盘可见」但字段里没有可见时间可推导）→ 注册失败/入库拒收，**不静默放行、不降级**，报错信息点名缺什么。
  3. **存量补齐**：Tushare / mootdx / 宏观 / 产业链 connector 全部补声明并通过注册（数据形态已知，声明可写实；如 Tushare 日线 = 交易日收盘后可见）。
  4. **高危示例入档**：契约文档里写「市场活跃度/拥挤度 = 收盘后可见」的声明样例（蓝图 §4.2 点名场景）。
- **④ 约束与非目标**：不接新数据源（D5）；不改任何 connector 的抓取/清洗逻辑（只补声明字段）；不改 kill-switch 行为；不动 `postgres.go` DDL（本单无新表；如注册表落库，按「加表追加 DDL 数组」惯例办理）。
- **⑤ 验收**：
  1. 机械判据：注册契约的单测通过（合法声明注册成功 / 三种缺失各自拒收且报错点名）；全部存量 connector 带声明注册成功的集成测试通过。
  2. **破坏验证（本单核心）**：① 注册一个不声明时间语义的 connector → 拒收 + 报错指明缺失字段；② 删掉某现有 connector 的声明 → 测试当场变红；③ 把「收盘后可见」改成「盘中即可见」→ PIT 校验测试变红；均还原绿。
  3. 文档同步：契约说明进 SPEC.md 或蓝图 §4.4 指定落点 + ARCHITECTURE.md 数据流段提及。
- **⑥ 边界**：只做「声明 + 拒收 + 存量补齐」；用新数据挖因子（D5）、多频率消费（D6）、注册表治理细则（D4）另立。
- **✅ 完成记录（2026-10-14）**：契约用「接口加必选方法 + 注册期校验」落地，改动全在 `pkg/data/source` 与 `pkg/domain/market`（`cmd/data` 一行未改，与 D3/D4 已提交文件零重叠）。**语义词表**（唯一真相）：`pkg/domain/market` 新增 `TimeSemantics`（`announcement_day`/`after_close`/`next_open`/`custom`）与 `FrequencySpec`（`daily`/`intraday`/`event`）+ `Parse*` fail-loud + `Known*` 拷贝；零值 `""` = 未声明（不算合法）。**契约落点** `pkg/data/source/contract.go`：`ConnectorDeclaration`（`DataType`/`TimeSemantics`/`VisibleExpr`/`VisibleField`/`Frequency`）+ 保留锚点 `VisibleFieldTradeTime`（OHLCV 类锚点在 `DataItem.TradeTime`，不在 schema 字段）+ `VisibleFieldAnnDate`（财报类锚点在规范层 `market.Fundamental.AnnDate`，披露日）+ 三类哨兵 `ErrDeclarationMissing`/`ErrDeclarationInvalid`/`ErrDeclarationMismatch`；`DataSourceAdapter` 接口加**必选方法** `Declarations()`（新 adapter 漏声明 = 编译错误）。**拒收闸门**：`Registry.Register` 在改状态**之前**调 `validateDeclarations` 校验（未声明 / 枚举非法 / custom 缺表达式 / 重复声明 / 声明了未支持类型 / 可见锚点不在 schema 或类型非 date·datetime），被拒 adapter 不污染 Registry。**存量补齐**：10 个 adapter 的声明集中在 `declarations.go`「声明即登记」并逐条写实（tushare 日线=收盘后可见、fundamentals=公告日事件流（可见锚点=**披露日** `ann_date`，非 `trade_time`——后者承载报告期截止日）；mootdx 分钟=周期结束自定义、实时=观察即可见；宏观/公告/新闻=公告日·发布事件流；eastmoney 系日更=收盘后可见；板块/热搜快照=观察即可见）。**测试** `contract_test.go`：三类拒收逐条负例 9 条 + 注册原子性 + 全部存量 connector 声明完整（`SupportedTypes` ⇄ 声明一一对应）且整批注册成功 + 错误点名。**破坏验证三腿均机制复现（改坏→红→还原→绿）**：① 未声明 → `ErrDeclarationMissing` 点名 adapter/数据类型；② 删 tushare 财报声明 → `TestAllExistingConnectorsDeclareEverySupportedType` 红（报 `supports "fundamentals" but has no declaration`）；③ `alpha_vantage` 的 `after_close` 改成词表外 `intraday_visible` → 同测试红（报非法枚举）。**验证**：`go build ./...` 全绿、`go vet ./...` 全绿、`pkg/data/source` + `pkg/domain/...` + `internal/repoguard` 测试全绿。**文档**：SPEC.md 新增「connector 注册契约」节（含高危样例 活跃度/拥挤度=`after_close` + 存量声明一览 + 三类拒收表 + 破坏验证）+ ARCHITECTURE.md §7 数据流段增补「上游闸门」。**已知边界如实登记**：① mootdx 分钟表 `amount` NULL→`Turnover` 记 0 在契约文档收编（不改 ETL 行为，D1 已点名）；② 产业链 fetcher（`pkg/data/supply_chain.go`）不走 Registry，其时间语义成文登记为已知边界，建 adapter 属 D5。**无关红登记**：`pkg/data` 的 `tushare_adjustment_integration_test.go`（实盘数据口径断言）当前红，与本单无关——实证 `pkg/data` 不依赖 `pkg/domain/market` / `pkg/data/source`（无引用），属实盘数据漂移，另议。

### D2-R1 · 财报 `ann_date` 透出链路补齐 + 宏观可见锚点复核 ✅ 完成（2026-10-16，依赖 D2）

- **① 目标**：收口 D2 审查（2026-10-15）发现的两处契约缺口：① 让财报 connector 的 `DataItem` 真正透出 `ann_date`（披露日），使保留锚点有实测载体；② 复核宏观 `obs_date` 可见锚点的前视缺口（是否改判宏观层 PIT 门属 P2-1 层裁决）。
- **② 上下文**：① `pkg/data/source/tushare_adapter.go` 的 `fetchFundamentals` 置 `TradeTime: f.Date`（**报告期截止日**），`fundamentalToMap` 13 个 key 无 `ann_date`；真实披露日由底层 `data.TushareClient.FetchFundamentals` 经 `normalizeFundamentals` 直写 `stock_fundamentals.ann_date`（规范层 `market.Fundamental.AnnDate` 承载可见语义，SPEC「披露日即可见时间点」）。D2 因此用保留记号 `ann_date` 声明锚点——缺口是「connector 的 wire 输出看不到披露日」，声明悬空、不可实测。② `TushareMacroAdapter` 声明 `announcement_day`，但锚点 `obs_date` 是观测日（月频取当月首日），真实发布日**晚于**它（9 月 CPI 10 月中发布）；`quant.macro_series` 现以 `obs_date` 为 PIT 口径（SPEC L1341），若回测按其过滤即潜在前视（实证 `pkg/backtest` 当前无宏观引用，风险为潜在而非现行）。
- **③ 要求**：
  1. **财报透出**：`fundamentalToMap` 补 `ann_date`（取自 `domain.Fundamental.AnnDate`；nil 留空不填 0），`Schema(DataTypeFundamental)` 增 `ann_date` 字段（date/datetime，非必填）；随即调整 `declarations.go` 的锚点声明与 `contract.go` 注释（保留记号 `VisibleFieldAnnDate` 与 schema 字段并存时写明优先级）。
  2. **宏观复核**：核实 `quant.macro_series` 读路径及回测/因子的消费面，给出「前视可达 / 不可达」结论与证据；若可达，改判可见锚点为导入时刻 `as_of` 或补发布日列——此项需 P2-1 层裁决并同步 SPEC L1341 与声明表。
- **④ 约束与非目标**：不改 D2 契约签名与三类拒收语义；宏观若涉及 PIT 门变更须走正式变更评审；不接新数据源（D5 另立）。
- **⑤ 验收**：财报 adapter 的 `DataItem` 带 `ann_date` 且与规范层一致（有单测钉住，含 nil 语义）；宏观复核结论成文（可达/不可达二选一 + 证据指针）。
- **⑥ 边界**：只做上述两处收口；新数据源接入（D5）、多频率消费（D6）另立。
- **✅ 完成记录（2026-10-16，code `6878a41`）**：两处收口分别闭口与出结论。**① 财报 `ann_date` 透出（已闭口）**：`fundamentalToMap` 增 `ann_date`（直接透传 `domain.Fundamental.AnnDate` 指针；nil = 上游未披露 → **留空为 nil**，不用零值时间/0 顶上）、`Schema(DataTypeFundamental)` 增 `ann_date`（`date`，**非必填**——「未披露留空」的结构前提）；`fetchFundamentals` 注释钉死「`TradeTime` = 报告期截止日（end_date）、可见锚点 = `Data["ann_date"]`（披露日）两者不可互替」。**保留锚点与 wire 投影的优先级同时定稿**（contract.go / declarations.go 成文）：语义以保留记号（规范层 `market.Fundamental.AnnDate`）为准，wire 同名字段是可选物理载体；新增 `validateReservedAnchorProjection` 执法——**可以没有**（向后兼容），**一旦有**必须是 date/datetime，用 string/float 冒充可见时间点按 `ErrDeclarationMismatch` 拒收；schema 不可得时不报警（保留锚点本身已足够，探测失败不该变成误拒）。**测试**：新增 `tushare_adapter_fundamental_test.go`（映射 nil 语义 / schema 投影 / 端到端「TradeTime=报告期、ann_date=披露日」且两者不同物 / 未披露留 nil）+ `contract_test.go` 保留锚点投影正负两例。**② 宏观 `obs_date` 复核（结论：当前不可达；声明不保守已登记）**：**证据**：`grep -rn -i macro pkg/backtest/ pkg/strategy/ pkg/mining/` **零命中**（回测/策略/挖掘均不碰宏观）；因子输入字段 ⊆ `pkg/expression` 词表（仅 market/fundamentals/activity/group 四类来源，无宏观）⇒ 宏观**结构上无法**成为因子输入；`quant.macro_series` 唯一读者是只读 API `GET /api/macro/series`（`cmd/analysis/handlers_macro.go:169` → `storage.ListMacroSeries`），不参与决策计算。**结论**：**前视不可达（现行）**，但**声明语义不保守**——`TimeSemantics=announcement_day` + 锚点 `obs_date` 声称「观测日当天可见」，而真实发布日晚于它（9 月 CPI 约 10 月中发布），任何按 `obs_date` 过滤的新消费方一接线即达。**待裁决（P2-1 层，按 ③.2/④ 权限边界本单不擅改）**：选项 A 锚点改 `as_of`（导入时刻，wire 已有，无需 DDL；保守且真实，代价是失去发布日精度）；选项 B 补 `release_date` 列（上游 Tushare 宏观接口实测不提供发布日，需人工维护发布日历，成本高、易错）；选项 C 维持现状 + 前置义务成文（靠纪律不靠结构，正是 D2 的教训）。**推荐 A**。裁决前写入 SPEC「已知边界④」的前置义务：宏观数据**只可按 `as_of` 过滤，不得按 `obs_date` 过滤**。**破坏验证两条全闭环（改坏→红→还原→绿，还原以 SHA-256 校验逐字节一致，全仓 `SABOTAGE` 零残留）**：① 摘掉 `validateReservedAnchorProjection` 调用 → 投影非日期用例红；② `Data["ann_date"]` 改 `f.Date`（报告期冒充披露日）→ 端到端 PIT 用例红。**验证**：`pkg/data/source` 全包 `-count=1` 绿；`go build` / `go vet` 干净；gofmt 本单文件干净。**新增边界登记（本单发现，未修，供台账裁决是否立新单）**：`storage` 的 ETL 落库路径（`BulkInsert` + `defaultTableMapper`）对 `fundamentals` 的列集**不含 `ann_date`**，且 `trade_date` 由 `DataItem.TradeTime` 喂入（= 报告期截止日）；而 `stock_fundamentals.available_date` 是生成列 `GENERATED ALWAYS AS (COALESCE(ann_date, trade_date)) STORED`（真库实测）⇒ **若有人走 ETL 落库财报，available_date 会退化成报告期截止日（前视）**。当前**不可达**（`ETLPipeline.Process` 零生产调用方，生产走 `data.TushareClient.FetchFundamentals` → `SaveFundamentalBatch`，该路径 trade_date/end_date/ann_date 三列都写对），故属潜在洞；修法（mapper 列集补 `ann_date`，或 ETL 路径写入前校验）不属本单 ① 的两处收口范围，**如实登记待裁决**。**协作说明（本单全程）**：改动集中在 `pkg/data/source` 五文件，与并行 agent 无重叠；期间 HEAD 被并行推进三次（`ca06c8b`/`e3b9e39`/`d005187`/`33f845f`），提交前均复核 HEAD 且未扫入他人未提交内容。

### D3 · 公式化因子引擎：定义入库 + 11 因子公式化 ✅ 完成（2026-10-14，依赖 D1 schema 字段定稿——D1 已于同日定稿）

- **① 目标**：因子定义从 Go 代码迁入「库里公式」（名字 + 公式文本 + 输入字段 + 版本哈希 + 生效日 + 口径 + 前视纪律），计算线读公式现算/缓存；11 个冻结因子全部公式化入库，因子名从「代码冻结」改「库管理」。
- **② 上下文**：蓝图 §5.2/§6 三期①。现状：因子名注册表是 `pkg/domain/factor.go` 的**冻结常量**（6 通用：momentum/value/quality/size/volatility/growth + 5 纵向：gross_margin_trend/contract_liability_ratio/ocf_to_net_profit/roe_dupont_leverage/inventory_turnover_delta），计算逻辑散在 `pkg/data/factor.go`（`FactorName` 引用处 L143/L216/L255 等）；**引擎现成**——`pkg/expression` 已有 parser/evaluator/operators/registry（表达式选股就是动态因子引擎），本单以搬运 + 治理为主。产出 schema 一个样（蓝图 §5.2）：因子名/日期/股票/值/存在性（缺失≠0）/口径（z-score/rank/raw）。项目既有红线：因子名清单为冻结协议注册表——本单正是把「代码冻结」升级为「库管理 + fail-closed」，语义不放松。
- **③ 要求**：
  1. **定义骨架落库**：名字、**版本哈希**（改公式=新版本，老版本可复现）、生效日、口径、输入依赖（引用 D1 规范层字段名）、前视纪律（按公告日/交易日对齐声明，与 D2 语义一致）。
  2. **计算内核三型**：叶子（字段直取）/ 公式（叶子 + 算子表达式）/ 代码（复杂逻辑，保留 Go 出口）。
  3. **11 个因子公式化入库，逐个单元测试钉死数学定义**（固定输入必等手算值）。⚠️ **判据写明**：5 个纵向因子是跨期差分/趋势类，若超出表达式引擎能力 → 落「代码」型并登记，**不硬凑公式**；不改变任何因子的现算数值。
  4. **注册表改库管理**：`domain/factor.go` 冻结清单降级为兼容薄壳或删除（以引用面盘点为准）；未登记因子名 → fail-closed 显式报错（继承既有语义，报错带可用清单）。
  5. **产出语义落地**：缺失≠0（存在性标记单独走）、口径标签随分数走。
- **④ 约束与非目标**：不做 AI 挖掘（D8）、不做治理细则（D4）、不接新数据源（D5）、不改任何因子的数学定义与现算结果（回归必须数值一致）；不动策略接口（`SetFactorCache`/`SetFundamentals` 注入方式不变）；表达式引擎若需扩展算子，按 OBS-06/07 先例补注册表 + 自洽护栏同步。
- **⑤ 验收**：
  1. 机械判据：11 个因子「公式库版 vs 原代码版」同输入同输出的对拍测试全绿（数值回归）；每个因子定义的单元测试（手算值）通过；未登记因子名 fail-closed 报错测试通过。
  2. **破坏验证**：① 改公式不换版本哈希 → 版本纪律测试红；② 公式引用不存在的输入字段 → 求值 fail-closed 报错而非算出垃圾分；③ 回到硬编码注册表（绕过库）→ 护栏红；均还原绿。
  3. 版本复现：同一次回测引用固定版本哈希，重跑结果一致（有测试或演练记录）。
  4. 文档同步：因子 schema 进 SPEC.md + ARCHITECTURE.md 因子段。
- **⑥ 边界**：只做「定义入库 + 11 因子公式化 + 注册表库管理」；治理细则（D4）、新因子挖掘（D8）、新数据因子（D5）另立。
- **✅ 完成记录（2026-10-14）**：因子定义从 Go 代码迁入「库里公式」全链路落地。**定义骨架** `pkg/domain/factor_def.go`：`FactorDefinition`（Name/Kernel/Formula/Validity/CodeRef/Caliber/InputFields/EffectiveFrom/Lookahead/Lookback）+ `VersionHash()`（SHA-256 canonical JSON，方法派生非字段、无可篡改存储入口，DB 加载重新 derive 比对不符拒载）+ `Validate()` fail-closed 结构校验；**种子 11 条** `pkg/domain/factor_def_seed.go`：momentum（`close / ts_delay(close, 20) - 1`，validity `close > 0 and ts_delay(close, 20) > 0`）/value（`1 / pe`，`pe > 0`）/quality（leaf `roe`）公式型，5 纵向因子代码型（CodeRef + 字段码字面量，跨期差分/趋势类不硬凑公式），size/volatility/growth **pending 态**（无计算线，名册在册内核待定，fail-closed 上报不发明定义）。**注册表库管理** `pkg/data/factor_registry.go`：`NewRegistry` 六重 fail-closed（结构 Validate / 公式 parse / expression L1 闸门 / identifier↔InputFields 双向相等 / 输入字段 ⊆ AvailableFields / CodeRef 绑定 + 去重），`FailClosedError` 两哨兵（ErrFactorNotRegistered / ErrFactorKernelUndefined）报错带可用清单；`pkg/domain/factor.go` 名册常量降级为兼容薄壳（唯一权威 = 种子 + Registry，保留原因=引用面盘点成文），`ComputeAllFactors` 改 registry 驱动（报告顺序与原硬编码列表兼容），新增 `ComputeFactorByName` 按名分派。**计算内核四态**（leaf / formula / code / pending）：公式内核 `pkg/data/factor_formula.go` 适配 expression 引擎（求值模型 map[symbol][]float64，末点=date 当日，历史不足给 NaN），**域过滤 Validity 机制**（本单设计决策）：公式引擎无三元表达式，直接乘 0/1 掩码会把域外值变成有效 0 分违反「缺失≠0」——Definition.Validity 与 Formula 同形求值，逐点非零且非 NaN 才产出，域外与 NaN 一律 skip。**DB 落库** `pkg/storage/factor_definition.go`（SaveFactorDefinitions 幂等 upsert 写入前 Validate+derive；LoadFactorDefinitions 逐条 derive 哈希比对，不符报 "does not match derived" 拒载）+ Migration 051 DDL（`factor_definitions` 表进 postgres.go 内联数组 + `docs/migrations/051_add_factor_definitions_table.sql` 记录）。**数值回归红线达成**：momentum/value/quality 公式库版 vs 原代码版同输入同输出对拍全等（raw / z-score / percentile 三列，域过滤与历史不足的缺失语义复现），手算值钉死（momentum 20/14、20/24、20/9 三例 + PE 负价域跳过）。**版本复现**：同版本哈希重跑一致有测试（TestRegistry_VersionReproduction）。**handler 收口**：`cmd/data` syncFactorHandler 8 因子硬编码 switch 清零改 `ComputeFactorByName` + errors.Is 两哨兵 → 400 带清单；`cmd/analysis` /list 改 `domain.BuiltinFactorDefinitions` 投影 + kernels 字段。**护栏** `internal/repoguard/factor_registry_test.go` 双判据棘轮（AST 扫 `Compute*Factor` selector 引用 + composite literal 内聚集 ≥3 名册常量，只减不增，4 个遗留文件带裁决理由登记基线）。**防漂移**：SPEC.md「因子定义 Schema」大节 drift:factor_definition 锚点 ↔ FactorDefinition 反射双向（`pkg/domain/spec_drift_test.go`）。**破坏验证三条全部闭环（改坏→红→还原→绿→diff 零残留）**：① canonical 投影去 Formula → 版本哈希不随公式变 → `TestRegistry_VersionHash_FormulaChangeInvalidates` 红；② 跳过 validateFormulaDef → 公式引用未声明字段/声明字段不使用 → fail-closed 两测试红；③ 在 pkg/backtest 造绕过库的生产文件（直调 ComputeMomentumFactor + slice literal 聚集 3 名册常量）→ 护栏双判据精确点名红。**文档同步**：SPEC.md 因子定义 Schema 大节（10 字段锚点表 + 内核四态表 + 版本哈希纪律 + 计算分派图 + 数值回归红线 + 已知边界）+ ARCHITECTURE.md §8 因子定义库数据流 ASCII 图 + 护栏四条 + 迁移方向 + factor.go 薄壳注释。**新 HEAD 复验（91e77e3，含并行 agent D7 提交）**：pkg/domain（+market）、pkg/storage、internal/repoguard、pkg/data（-count=1 32.6s 全绿，含 D1 登记的 TestAdjustmentBasis 既有红连跑 3 次转绿）、pkg/backtest 全家、pkg/strategy 全绿；go vet 干净。**已知边界**：① size/volatility/growth 三因子 pending 待计算线（D5/D6 数据到位后补 kernel）；② universe 规则（全 market→OHLCV / 全 fundamentals→快照 / 混源并集缺侧 NaN）；③ **HEAD 既有破损登记（非本单引入，只登记不修）**：`bdd1949`（并行 agent 半成品修复）致 `go build ./cmd/data` 失败——main.go:135 `syncIndexConstituentsHandler(tc, store)` 两参 vs handlers_sync.go:40 定义只收一参，且 pkg/storage 无 `SaveIndexConstituents` API；补全需发明落库 API（侵入他人工单 DATA-02/03 线），本单不动，留给该线负责人。

### D4 · 因子注册表治理细则 ✅ 完成（2026-10-14，依赖 D3 已完成）

- **① 目标**：因子登记/生效/回滚的治理细则成文并可执行演练，每条登记带「公式、哈希、登记人、生效日」四样，回滚不破坏历史回测复现。
- **② 上下文**：蓝图 §5.2 三条铁律、§6 协议变更纪律、§9 #3（倾向：参考外部门户信号 `content_hash` 幂等先例）。D3 把注册表从代码搬进库，本单定「进库之后的纪律」。
- **③ 要求**：
  1. **细则成文**：登记人、生效日语义、哈希算法（与 D3 版本指纹同一算法）、回滚流程、审计留痕（谁、何时、改了什么、为什么）。
  2. **每条登记四样齐全**方可生效（缺一拒收，fail-closed）：公式、哈希、登记人、生效日。
  3. **回滚语义钉死**：回滚 = 新引用不再命中该版本，**历史回测仍能引到老版本**（可复现性红线）。
- **④ 约束与非目标**：不改因子计算逻辑（D3 已定）、不做挖掘流水线（D8）、不引入外部治理系统（就地实现）。
- **⑤ 验收**：
  1. 演练记录：完整走一遍「登记 → 被公式引用 → 回滚」三步，每步留痕（截图或测试输出）。
  2. 机械判据：登记四样缺失 → 拒收测试通过；回滚后新引用 miss / 历史引用 hit 的测试通过。
  3. **破坏验证**：绕过四样直接写库 → 拒收/护栏红，还原绿。
  4. 文档同步：细则落 SPEC.md 或 governance 专节 + 蓝图 §9 #3 标记收口。
- **⑥ 边界**：只做治理细则 + 演练；挖掘与过拟合治理（D8 治理层）另立。
- **✅ 完成记录（2026-10-14）**：因子注册表「进库之后的纪律」落地。**治理核心** `pkg/domain/factor_registration.go`：`FactorRegistration`（Definition 快照 / VersionHash / Registrar / Status / RegisteredAt / RolledBackAt / RolledBackBy / RollbackReason 八字段）+ 状态两态（active/rolled_back）+ `Validate()` 四样 fail-closed 校验（公式=`Definition.Validate()` 内核体 + pending 拒收 / 哈希=非空且 == `Definition.VersionHash()` 同 D3 算法 / 登记人=`TrimSpace` 非空 / 生效日=`Definition.Validate()` 覆盖）+ `RegistrationAuditEntry`（谁/何时/改了什么/为什么）。**进程内账本** `pkg/data/factor_governance.go`：`RegistrationBook`（`Register` 四样校验 + 复用 D3 `NewRegistry` 表达式闸门 + 去重拒收 `ErrRegistrationDuplicate`；`Rollback` 未登记/已回滚/缺 actor 拒收；`ResolveEffective` 新引用取最新 active 版本、回滚即回落；`ResolvePinned` 历史引用按 (名,哈希) 锁定、回滚仍命中；`NewRegistrationBookFrom` 回读逐条校验去重）。**回滚语义钉死**：回滚只置 `status=rolled_back` + 留痕、**不删版本行**——新引用 miss、历史引用 hit（可复现性红线）。**DB 侧** `pkg/storage/factor_registration.go`：`SaveFactorRegistration`（Validate → INSERT definition JSONB → `ON CONFLICT DO NOTHING` 幂等 → 首插才留痕）/ `RollbackFactorRegistration`（仅 active 可滚，0 行报错）/ `LoadFactorRegistrations`（回读重新 derive 哈希比对，不符报 "does not match derived" 拒载）/ `LoadFactorRegistrationAudit` + `appendRegistrationAudit`。**DDL**：Migration 053 进 `pkg/storage/postgres.go` 内联数组（`factor_registrations` 表 + `factor_registration_audit` 表，CHECK：registrar 非空 / 内核体存在 / 状态枚举）+ `docs/migrations/053_add_factor_registrations.sql` 记录镜像。**演练（⑤1）**：`pkg/data/factor_governance_test.go` `TestRegistrationBook_Drill_RegisterReferenceRollback` 逐步断言=逐步截图——登记 v1 → 解析定义过计算闸门（被引用）+ 登记 v2 抬新引用 → 回滚 v2 新引用回落 v1 且钉 v2 历史引用仍命中，审计三条（register/register/rollback）谁/何时/为什么齐全。**机械判据（⑤2）**：`pkg/domain/factor_registration_test.go` 8 例表驱动四样缺失拒收 + 未知状态 + 陈旧哈希；回滚后新引用 miss / 历史引用 hit 两向测试。**破坏验证（⑤3）**：`pkg/storage/factor_registration_test.go` 绕过写入路径 raw INSERT（缺 registrar / 缺内核体 / 未知状态）→ DB CHECK 精确点名拒收（`factor_registrations_registrar_present` / `factor_registrations_kernel_body_present`）→ 红，走正常写入路径 → 绿；另钉「库行被绕过改坏 → 加载拒载」+ 回滚不删行 + 幂等不重复留痕。**文档同步（⑤4）**：SPEC.md 新增「因子注册表治理细则（Factor Registration，D4）」大节（8 字段 drift:factor_registration 锚点表 + 四样齐全表 + 回滚语义表 + 审计留痕 + 演练记录 + 护栏 + 实现指针），`pkg/domain/spec_drift_test.go` 加 `TestSpecDriftFactorRegistration` 双向比对；蓝图 §9 #3 标记收口。**验证**：pkg/domain（含 SpecDrift）/ pkg/data（drill + 既有注册表）/ pkg/storage（7 例库侧测试全绿，本机 PG）全绿；gofmt 干净、go vet 干净。

### D5 · 新数据源 connector 样板：一个源 + 一个公式因子 ✅ 完成（2026-10-16，依赖 D2、D3）

- **① 目标**：接入**一个**新数据源（市场活跃度或拥挤度，二选一），翻译进规范层 + 按 D2 契约注册 + 写**一个**引用它的公式因子并算出分数序列——打样「挂新数据 → 可挖因子」全链路；多源接入后续一源一单。
- **② 上下文**：蓝图 §4.4/§6 三期②。先例：宏观数据源 P2-1（`cmd/data` adapter 注册、kill-switch、真 token 逐接口实测）；既有工程惯例「数据源集成必须先核实官方 API 文档和 token 配额，不凭文档臆测实现」。活跃度/拥挤度是蓝图 §4.2 点名的**高危 PIT 场景**（收盘后才可得），本单是红线的第一个真实考卷。
- **③ 要求**：
  1. **前置核实**：动手前核实官方 API 文档、时间戳语义、token 配额，结论写进工单上下文（不实测不动工）。
  2. **connector 落地**：按 D2 注册契约声明（如「收盘后可见」+ 频率口径），翻译进 D1 规范层。
  3. **公式因子一个**：引用该数据的公式因子入库（按 D3 定义骨架 + D4 登记四样），算出按日分数序列。
  4. **缺失语义**：缺失日存在性标记正确（缺失≠0），不产出垃圾分。
- **④ 约束与非目标**：只做**一个源 + 一个因子**（样板）；不批量接源（后续一源一单）；不改 connector 注册契约本身（D2 已定）；不为本单放宽 PIT 红线（恰好相反，用它验证红线）。
- **⑤ 验收**：
  1. 机械判据：connector 注册 + 同步 + 因子计算的集成测试通过；因子分数序列有单元测试钉口径。
  2. **PIT 破坏验证**：把可见时间声明改宽（「收盘后可见」→「盘中即可见」）→ 防偷看测试变红，还原绿（证明闸门真的在拦）。
  3. 样板记录：从「挂源」到「因子出分」的端到端步骤留档（后续一源一单的模板）。
- **⑥ 边界**：只做样板全链路；第二个源、AI 挖掘消费（D8）另立。
- **✅ 完成记录（2026-10-16，落地日；用户拍板与前置核实于 2026-10-15）**：全链路「挂新数据 → 可挖因子」样板打通（**用户拍板两项**：源选 `daily_basic` 换手率族、名册走正式协议变更新增名）。**① 前置核实（不实测不动工）**：真 token 逐接口探测（`20261014`）——可访问 `daily_basic`（5563 行）/`moneyflow`（5574）/`index_dailybasic`/`top_list`/`daily`；无权限（40203）`bak_daily`/`limit_list_d`/`limit_step`/`ths_hot`/`hm_list`/`hm_detail`/`kpl_list`/`stk_factor`/`stk_factor_pro`/`moneyflow_ind_dc`。选 `daily_basic` 换手率族：活跃度教科书定义（成交量/股本）、单位 % 无歧义（对比 moneyflow 金额单位未实测，按纪律不实测不声明）、且**未被任何现有链路使用**（现有 fundamentals 走 `fina_indicator`）＝真新数据类。单日一请求（全市场≈5.5k 行，官方单次上限 6000），跨度上限 400 日（自己的配额保护）。**② 规范层**：`domain.MarketActivity`（symbol/trade_date/turnover_rate/turnover_rate_f/volume_ratio/source/as_of，三数值列 `*float64` 缺失≠0）+ 表 `market_activity`（Migration 056，(symbol,trade_date,source) 唯一键幂等；source VARCHAR(64) 对齐溯源列惯例）+ 读写面 `pkg/storage/market_activity.go`（批量事务 upsert / 区间读，写入面校验身份列与有限性）。**③ connector**：`TushareDailyBasicAdapter`（name `tushare_daily_basic`，kill-switch `DATA_DISABLE_TUSHARE_DAILY_BASIC`，复用同一 `TushareClient` 零新 key；wire schema `ts_code/trade_date/turnover_rate/turnover_rate_f/volume_ratio`）；声明 `after_close` + 锚点 `trade_date` + `daily`（D2 契约）；数据层 `pkg/data/tushare_daily_basic.go`（`FetchDailyBasic`/`ProbeDailyBasic`，nil=缺失落 NULL / 脏值跳过 / 缺列点名 fail-loud / 有行但全跳过 fail-loud，纪律同 macro.go）。**④ 语言层**：expression 新增来源 `FieldSourceActivity` + `TableMarketActivity` + 三字段（turnover_rate/turnover_rate_f/volume_ratio），可用性探测随表；策略侧 `OHLCVDataProvider` 同步供应（`activityFields` + 同日对齐 `activitySeries`，未注入即明确报错不返 0）——跨包能力护栏（`Fields() ≡ AvailableDataFields()`）保持绿。**⑤ 因子与名册**：**正式协议变更**新增名册名 `market_activity`（D3 之后第一个；D8 的「不动态新增」指运行期，本单按蓝图 §6 纪律走正式变更）；种子定义 formula 型 `turnover_rate / ts_mean(turnover_rate, 20) - 1`（异常活跃度：相对自身近 20 日基线的偏离，比裸换手率水平更贴合注意力冲击的作用量）、Validity `turnover_rate > 0 and ts_mean(turnover_rate, 20) > 0`、InputFields `["turnover_rate"]`、Lookback 30、Lookahead `trade_date`、EffectiveFrom 2026-10-16；计算链路 `pkg/data/factor_formula.go` 新增 `loadActivitySeries`（activityFieldSet + `GetMarketActivityForDateRange`）；因子假设入册（Barber & Odean 2008 注意力机制）。**⑥ 验收**：机械判据——29 项新测试全绿（adapter 8 / 数据层 9 / 存储真库 4 / 因子 6 / D4 登记演练 2）；因子分数序列手算钉死（19 天 1.0 + 当日 2.0 → 2/1.05−1 = 0.9047619…，容差 1e-12）、历史不足（<20 观测）与域外（换手率 0）与缺失（NULL）三路都**不产出**（缺失≠0，绝非 0 分）。**PIT 破坏验证三腿全闭环（改坏→红→还原→绿，全仓 `SABOTAGE` 零残留）**：① 声明 `after_close`→`custom`+盘中表达式 → `TestDailyBasicDeclaration_MustBeAfterClose` 红；② 取数上界 `trade_date <= end`→`>= end` → `TestMarketActivity_RangeQueryIsPITBounded` 红（点名未来行 2200-03-06 泄漏）；③ 摘掉「有行但全跳过」闸门 → `TestFetchDailyBasic_FailsLoudWhenAllRowsSkipped` 红。**验证**：`go build ./...`/`go vet ./...` 全绿；`pkg/domain`、`pkg/expression`、`pkg/strategy/expression`、`pkg/data/source`、`pkg/storage`、`pkg/mining`、`internal/repoguard`、`cmd/data` 全绿。**文档同步**：core `docs/SPEC.md`（规范层新增「市场活跃度」字段类 + 漂移锚点 `activity` + connector 契约存量表 + 因子定义表 + 新增「新数据源样板」大节：端到端步骤模板/实测记录/名册协议变更记录/破坏验证表/已知边界）、`docs/ARCHITECTURE.md` §7（新数据源接入样板数据流）、docs 仓蓝图 §4.4/§5.5/§6（② 标记落地 + 名册协议变更记录）。**已知边界如实登记**：① 只做一个源一个因子，`moneyflow`/`index_dailybasic` 未接入；② 无 `cmd/data` 定时同步任务（与宏观 P2-1 同姿态：先通链路，调度另立）；③ 回测引擎尚未在快照阶段注入活跃度（策略用该字段会明确报错，注入属后续工单，与 D6 同型）；④ `turnover_rate_f`/`volume_ratio` 已进词表与存储但暂无因子消费。**无关红登记**：`pkg/data` 的 `tushare_adjustment_integration_test.go`（实盘数据口径断言）仍红，与本单无关（D2 已登记）；`cmd/data` 的 `TestSyncAllFactorsHandler_ReportsSkipped` 在 HEAD 上即为陈旧红（D3 名册扩到 11 条时 `wantSkipped` 未同步，本单校正为 8 并留记录）。
- **自审修复（2026-10-16，用户要求复审）**：对本人更改做第二轮审查，发现并修四类问题——① **计数笔误**：完成记录原写「20 项新测试」，实数 29（8+9+4+6+2），已订正；② **日期口径不一致**：首次提交实际落地于 2026-10-16 11:36（commit `fa53ca2`），而记录/种子 `EffectiveFrom` 写的是 2026-10-15——统一为「落地日 2026-10-16（= 定义入库日，与 D3 种子以落地日为 EffectiveFrom 的惯例一致；用户批准与前置核实仍在 2026-10-15，文中保留并标明）」。⚠️ 连带影响：`EffectiveFrom` 参与 `VersionHash` 的 canonical 投影 ⇒ 该定义版本指纹随之变化；因尚无任何消费方（无 DB 登记行、无回测引用），无复现性影响，D4 登记四样须以新哈希为准。③ **健壮性缺口（新增修复）**：`FetchDailyBasic` 原先不校验 `trade_date` 格式，非法输入会变成一次白吃的上游请求 + 口径不明的上游报错；现本地前置校验 `YYYYMMDD`（`INVALID_INPUT` 点名参数），补 `TestFetchDailyBasic_RejectsMalformedTradeDate`（新测试数 29→30）。④ **语义未成文（新增修复）**：多日分片导入「以日为事务边界、中途失败时此前成功的天已入库」此前只存在于实现里，现写入 adapter `Fetch` 文档与 SPEC「已知边界」（调用方须把 error 读作「范围未完整导入」而非「什么都没写」；逐日 upsert 幂等使重跑安全）。**复审另核项（确认无问题）**：接线点与既有数据类型同层（`registry_init.go`/`declarations.go`/`adapter.go` 三处，与 `top_list` 对照一致）；kill-switch 无中心化 env 文档，故无遗漏登记点；死代码扫描无残留（`equalFoldASCII` 已在定稿重写中移除）；`domain.MarketActivity` 放 `pkg/domain` 而非 `pkg/domain/market`——与 `domain.FactorOutput`（D1 三类字段中已有一类不在 market 包）同型，SPEC 内自洽，不改。**复审验证**：`go build`/`go vet` 全绿；受影响包（domain / expression / strategy·expression / data / data·source / storage / cmd·data / mining / repoguard）全绿。
- **自审修复追加（2026-10-16，同一轮复审的第五项）**：⑤ **补齐 ⑤1 的「集成测试」空缺**——原测试矩阵是「adapter 用替身验协议 + storage 用裸 SQL 验读写」，**真 adapter → 真库 → 真 factor_cache** 的贯通用例缺失（`TestNoHandRolledPostgresDSNAssemblyOutsideStorage` 亦未被触发过，说明该路径此前无人走过）。新增 `pkg/data/source/daily_basic_integration_test.go`：真库上跑「注册（D2 契约）→ 同步（真 adapter 逐日拉取落库）→ 因子计算（真 store 取数 → 引擎 → 真 factor_cache）」，断言手算分入真库、范围重放幂等（唯一键 upsert 不增行）、**晚于计算日的观测结构上不可见**（真 SQL 上界）；数据用独享 symbol `ITESTD5.SH` 自造自清并**复核归零**（真库实测 0 残留）。破坏腿：取数上界改向 → 该用例红（真库侧窗口塌陷、无分数产出）→ 还原绿。上游用 httptest stub（不碰外网/不吃配额）；库不可达 skip（与 pkg/data 既有真库用例同款，CI 的 go job 有 Postgres service container，实际会跑）。**顺带撞上并遵守既有护栏**：初版手拼 DSN 被 AUD-42 的 DSN 唯一构造点护栏判红，改用 `storage.BuildDSN(storage.DatabaseConfig{...})`；并据此纠正一处口径笔误（`PercentileRank` 是 0–100 标度，单标的 = 50，不是 0–1）。新测试数 30 → **31**。
- **归属披露（2026-10-16）**：本项对 `docs/SPEC.md` 的编辑（端到端步骤表第 9 行、破坏验证表 ② 追加集成用例、AUD-42 护栏说明段）与上批本单未提交的 SPEC 改动一样，被并行 agent 的 `e3b9e39`（REV-02 自审修正）以文件级 `git add docs/SPEC.md` 一并扫入其提交——与其自身 REV-02 内容同段相邻，经逐字核对**内容完整无篡改**；因该提交已被引用，不改写历史，在此披露。归属方须以 SPEC.md 的实际内容为准，`e3b9e39` 的 message 之外还含本单这 7 行。

### D6 · 多频率进回测策略视野 ✅ 完成（2026-10-15，依赖 D1、D2）

- **① 目标**：策略在回测中可「日频决策 + 引用当日内分钟特征」（如开盘半小时动量），分钟级防偷看闸门按分钟粒度生效，现有纯日线回测结果不变。
- **② 上下文**：蓝图 §4.3 裁定 4、§9 #6（已拍板：前者先行，分钟级撮合另立）。现状：回测只吃日线，1 分钟数据「只存着」。红线：策略运行时**禁止**直接查 `marketdata.Provider`，所有数据回测启动前快照预加载进 Scope（可复现 / 防前视 / 双核输入一致）。分钟级 PIT 语义：10:00 的 1 分钟线 10:01 才可见。
- **③ 要求**：
  1. **策略视野扩展**：回放日可引用「当日内已可见」的分钟特征（由 D1 聚合/路由供给），特征清单以接口声明方式注入（延续 `SetFactorCache`/`SetFundamentals` 的声明注入模式）。
  2. **快照预加载**：多频率数据同样走「启动前快照」，不给运行时查询。
  3. **分钟级防偷看闸门**：按分钟粒度裁剪可见性（10:01 后才可见 10:00 bar）。
  4. **对拍口径**：多频率输入进双核翻译点（D1 聚合规则）有对照验证（手算样本或对拍测试）。
  5. **回归**：现有纯日线回测结果数值不变。
- **④ 约束与非目标**：不做分钟级撮合精度升级（另立工单）；不改现有日线策略行为；不放松快照预加载红线；不动四契约签名。
- **⑤ 验收**：
  1. 机械判据：「日频决策 + 日内特征」样例策略回测测试通过；日线回归对拍通过；分钟 PIT 闸门测试（10:00 bar 在 10:00:30 不可见、10:01 可见）通过。
  2. **破坏验证**：回拨分钟可见时间（全日内可见）→ PIT 测试变红；绕过快照直接查 provider → 红线护栏红；均还原绿。
- **⑥ 边界**：只做策略视野 + 闸门 + 回归；撮合精度、新频率数据接入（D5）另立。
- **✅ 完成记录（2026-10-15）**：
  1. **契约（声明注入，延续老范式）**：`pkg/strategy/intraday.go` 新增
     `IntradayFeatureProvider`（`Features()` 能力声明 + `IntradayFeature(ctx, symbol,
     name, asOf)` 分钟粒度取特征，`ok=false` = 此刻还没有，**不是 0**）与可选能力接口
     `IntradayAware`（范式同 `FactorAware`/`FundamentalAware`，GenerateSignals 签名零改动）。
     `LocalSignalProvider` 加 `SetIntradayProvider` + Signals() 内类型断言注入（nil 供给不注入，
     策略用到特征明确失败）。
  2. **闸门（分钟粒度 PIT）**：判定原语 `pkg/domain/market` 的 `OHLCV.VisibleBy(asOf)`
     （VisibleAt 不晚于 asOf；零值恒不可见——未声明的可见性是泄漏）。10:00 的 1m bar
     （VisibleAt 10:01）在 10:00:30 不可见、10:01 可见，`visibility_test.go` 逐条钉死
     （含 15m bucket / 日线同构与边界前 1ns）。
  3. **快照预加载（红线执行）**：`pkg/backtest/intraday.go`——`warmIntradayIfNeeded`
     仅对 `IntradayAware` 策略触发（纯日线策略零成本，回归红线）；经 `Router.Bars(Freq1m)`
     取数，**取数区间展开为 [start, end+1d)**（引擎 start/end 是 UTC 零点交易日，直接传
     1m 路由会把当日 09:30 CST 之后的 bar 全部挡在门外——自灌自证测试当场抓到的缺口），
     按 365 自然日分片（Router 跨度上限 366 天，REV-01；末片退 1ns 映射半开区间），
     逐 symbol 顺序取（峰值内存确定）。快照 = `map[symbol][]domain.OHLCV`（ts 升序），
     回测期间只读；`SetIntradaySnapshot` 给测试/无 Router 场景留直注门（范式同
     SetFundamentals）。
  4. **按日绑定 provider**：`snapshotIntradayProvider` 持快照 + 回放日边界，二分切当日段
     （每 symbol 每天 O(log n)，不线性扫全史）；特征目录封闭（`open_momentum_30m` =
     窗口末收/窗口首开−1，1m 与 15m 按 D1 聚合规则同值——对拍锚点）；可得条件双闸：
     **窗口闭合**（存在周期跨过 10:00 的 bar，半日停牌/断档 ⇒ 半窗不给残缺样本）+
     **已可见**（首末 bar VisibleAt ≤ asOf）。`TestSnapshotIntradayProvider_NoRuntimeDataChannel`
     反射断言字段集合 = 快照 + 日期边界（无 provider/router/pool 句柄）。
  5. **样例策略回测**：测试策略「日频决策 + 开盘半小时动量」（特征 > 阈值买入，
     strength = 特征值；无供给明确失败不当 0）跑通 `RunBacktest`：01-02 特征手算
     +0.05 ⇒ 收盘 10.50 含滑点成交、末日强平，`TotalTrades`=1（往返口径）、
     fills=2，策略侧按日记录的特征值与手算一致（01-03 −0.03 不买、01-04 停牌无信号）。
  6. **对拍与回归**：`TestSnapshotIntradayProvider_FeatureParity_1m_vs_15m`——1m 直取与
     15m 聚合（D1 口径）两条输入算出同一特征值且等于手算样本 0.05，15m 可见性与 1m
     同构（09:45 bucket VisibleAt = 10:00）；`TestEngine_DailyOnlyBacktest_UnchangedWithIntradayWiring`——
     纯日线 momentum 在「无 D6 装配」与「快照+Router 全挂」两种装配下收益/笔数/equity
     curve 逐点相等。DB 集成（真 PG + 真 Router 自灌自证自清理，不可达 Skip）：31 根种子
     bar 的快照内容、频率声明、VisibleAt 口径（09:30→09:31、10:00→10:01）、端到端特征值。
  7. **红线护栏（结构执法）**：`internal/repoguard/strategy_marketdata_isolation_test.go`——
     AST 扫 `pkg/strategy` 生产文件 import，命中 `pkg/marketdata` 即红（点名文件 + import 行）。
     与 D1 的 `canonical_layer` 护栏分工：那个守「不许直连物理表」（数据从哪来），这个守
     「策略运行时不许摸取数包」（数据怎么到策略手里）。
  8. **破坏验证两组，均「改坏→红→还原→绿」**：① `VisibleBy` 改成「有 VisibleAt 即可见」
     （回拨分钟可见时间 = 全日内可见）⇒ 闸门测试红（含验收原句子例 10:00:30）、provider
     特征测试红；还原后 sha256 与基线一致、复绿。② `pkg/strategy` 造 import marketdata 的
     生产文件 ⇒ 红线护栏红（点名违规文件）；删除后复绿，工作树无残留（git status 复核）。
  9. **文档同步**：SPEC.md 新增「多频率策略视野（Intraday Features，D6）」大节（红线 +
     契约 + 特征目录表 + 对拍口径 + 取数区间坑 + 护栏测试表 + 已知边界）；ARCHITECTURE.md
     §7 增补 D6 数据流图与红线执法；本节登记。
  10. **既有红登记（ODR-067 纪律，stash 证伪非本单造成）**：`cmd/data`
      `TestSyncAllFactorsHandler_ReportsSkipped`（skipped=8 want 5：D3 的 size/volatility/
      growth pending 态 + fundamentals_detail 空表）与 `pkg/data`
      `TestAdjustmentBasis_QfqLookAheadLeaksIntoBacktest`（库内复权因子与重建口径 2/20 只
      不一致）在**不含本单改动**的干净基线上同样红（stash -u 复跑确认）——数据/预期漂移类
      既有红，登记在案，不冒充本单噪声也不冒充本单修复。
  11. **已知边界**：① 分钟级撮合不在本单（日频决策成交价 = 当日收盘含滑点，日内择时执行
      另立）；② 15m 不物化（D1 裁定，快照只预载 1m，特征对 15m 输入同构有对拍证明）；
      ③ 全区间 1m 快照的单标的内存上限约 8.4MB（REV-01 实测口径），大票池 × 长区间的
      窗口化属后续治理项；④ 分钟表 `amount` NULL 折零（D1 已登记）不影响本单特征。

### D7 · NT 原生模式（国外线）✅ 完成（2026-10-14，依赖 D1）

- **① 目标**：nt-sidecar 支持 NT 原生模式（数据模型 + 订单类型 + 原生执行规则），市场→内核映射配置驱动且 fail-closed；一个最小 NT 原生策略跑通一个国外市场样例 run，回执走外壳（ResultSpec）与国内线同 schema。
- **② 上下文**：ADR-031 决议变更记录（路线二）+ 蓝图 §2.1 热转换语义、§6「NT 侧原生模式」。现状：nt-sidecar（Python，`nautilus_trader==2.0.0rc6` 钉死，`docker-compose.nt.yaml` + docs 仓 `docs/guides/nt-sidecar-operations.md`）只做 A 股权重回放对拍；RunSpec 的 `kernel` 路由字段与 fail-closed 语义已在（`pkg/backtest/contracts/runspec.go`、路由测试 `pkg/backtest/job/job_kernel_routing_test.go`：未知内核名显式报错不回退）。热转换语义钉死：切换=配置级（市场→内核映射），非 run 级迁移；「国外策略用 NT 原生写」是新增不是重写。
- **③ 要求**：
  1. **NT 原生模式**：侧车加原生执行通道（NT 数据模型、订单类型、各市场自带规则），不做 A 股规则魔改。
  2. **市场→内核映射配置**：如 `market.US → nautilus`、`market.CN → go`；未注册市场/内核 → **显式拒绝（400/error），不静默回退 Go 内核**（继承 EngineRunner fail-closed）。
  3. **外壳共用**：国外线 run 的 ResultSpec 回执与国内线同一 schema（前端/审计不感知内核差异）。
  4. **端到端样例**：一个最小 NT 原生策略（Python）+ 一个国外市场（样例数据即可，真实数据源等 D9）经 `EngineRunner` 跑通并返回回执。
  5. 国外线数据经 D1 规范层翻译进 NT（翻译层加新映射行）。
- **④ 约束与非目标**：不动国内对拍线现状（K13–K16 资产全保留）；不接国外真实数据源（等 D9 选型后另立）；**不改四契约签名**（只用既有 `kernel` 字段）；不放松侧车安全约束（loopback-only、不直连 DB、LGPL 服务化）；钉死 rc6 不追 nightly（版本升级走 docs 仓 `docs/guides/nt-sidecar-operations.md` 预案）。
- **⑤ 验收**：
  1. 机械判据：端到端样例 run 测试通过（kernel=nautilus + 国外市场映射 → 回执 schema 校验通过）；未注册映射 fail-closed 测试（400/显式错误，无 Go 回退痕迹）通过；国内线对拍回归（`cmd/dualrun -gate`）仍绿。
  2. **破坏验证**：删掉市场映射配置 → fail-closed 红；把国外 run 的回执 schema 改出差异 → 外壳一致性测试红；均还原绿。
  3. 文档同步：运维/配置说明进 docs 仓 `docs/guides/nt-sidecar-operations.md` + ARCHITECTURE.md 双核段。
- **⑥ 边界**：只做原生模式 + 映射配置 + 样例 run；国外数据源接入选型（D9）与接入实施、A 股线改造另立。
- **✅ 完成记录（2026-10-14）**：
  1. **侧车 · NT 原生模式**：新增 `nt-sidecar/sidecar/markets.py`（市场档案表 `CN`/`US`，
     词表封闭 + 装配期校验）与 `sidecar/native.py`（最小 NT 原生策略 `native_sma_cross`：
     LIMIT 进 / MARKET 出，全走 NT 自带订单类型与执行模型，**不施 T+1/涨跌停/整手**）。
     执行模式由**市场档案**分派（不是看策略名）；`--market` 缺省 `CN` ⇒ 国内对拍线行为
     逐字不变（Python 套件 77 → 100 条，原 77 条未动）。未登记市场在装配期 `MarketError` 退出。
  2. **路由 · 配置驱动 fail-closed**：`contracts.MarketRouter` + `ResolveRunTarget`
     （`pkg/backtest/contracts/marketroute.go`），配置 `kernel_routing.markets`
     （`CN: go` / `US: nautilus`）。`POST /api/backtest` 的可选 `market` 在**路由之前**解析成
     内核名写进既有的 `kernel` 字段 —— **四契约零改动**（④「只用既有 `kernel` 字段」）。
     未登记市场 → 400（点名市场 + 已登记集合 + "no silent fallback"）；market 与 kernel
     双写不一致 → 400；空配置表合法（删掉配置的效果 = 带市场的请求变红，存量请求不受影响）。
  3. **外壳共用 · 跨语言机械护栏**：侧车回执字段名必须落在 `contracts.BacktestResponse`
     的 json tag 集合内；重命名登记为别名表（`equity_curve → portfolio_values`），侧车专属键
     （`nt_stats` / `total_orders` / `total_positions` / `iterations`）逐条登记**理由**。
     测试直接读 Go 源码抽字段集（`tests/test_d7_native_mode.py`），国内线/国外线各跑一遍。
  4. **端到端样例 run（留 run id）**：真侧车（`--market US` + 合成美股 catalog）+ 真
     NautilusTrader + 真 HTTP 链路（gin handler → EngineRegistry → EngineRunner → 侧车）：
     `run_id=a27caca6-6839-4fda-ba51-2fd10d341c84`，`trades=4`、`equity_points=60`、
     `total_return=0.004`，market=US → kernel=nautilus，**go 内核零调用**。
  5. **破坏验证（两组，均「改坏→红→还原→绿」，还原后 sha256 与基线一致）**：
     ① 把 `ResolveRunTarget` 的未注册市场分支改成回退 `DefaultKernel` ⇒
     `TestBacktestPost_UnregisteredMarketFailsClosed` +
     `...DeletedMarketConfigRefusesMarketButKeepsLegacyPath` 双双红；
     ② 把侧车回执键 `total_return` 改成 `totalReturn` ⇒ 外壳一致性测试红（点名该键「既不在
     ResultSpec、也未登记为侧车专属/别名」）。两组还原后均绿。
  6. **文档同步**：`quant-trading-docs` 仓 `docs/guides/nt-sidecar-operations.md` 新增 §4
     （两层配置 / 起市场侧车 / 加新市场三步 / 已知阻断），`docs/ARCHITECTURE.md` 双核段逐条标注
     D7 已实施；core 仓 `docs/openapi.yaml` 的 `BacktestRequest` 增 `market` 字段与 fail-closed 说明。
  7. **⚠️ 未达标一条（如实登记，非本单缺陷）**：验收 1 的「国内线对拍回归（`cmd/dualrun -gate`）仍绿」
     —— 该门禁在本单**开工前即为红**（6 项违例）。已实测：开工前后门禁输出**逐字相同**，
     D7 对国内线零影响。根因是**产物陈旧**：SYNC-01 之后调度器真的会同步底表 ⇒ `ohlcv_daily_qfq`
     的 qfq 基准移动 + volume 精度变化，而 `nt-sidecar/catalog` 仍是 2026-10-12 的旧产物。
     **刷新路径当前被 D1 打断**（两处，均在 `b49629e` 引入的导出路径上）：① `export_bars.py` 的
     SELECT 返回 `trade_date`，翻译层按规范层字段名取 `date`；② `as_quantity` 拒绝非整数成交量，
     而库里的 volume 是两位小数的「手」。把这两处**临时**补齐后重导 catalog，门禁输出
     「全部指标在 K15 阈值内，确定性腿一致，无成交 mismatch」——**对齐本身完好**。修法涉及
     成交量单位的口径裁决（会动国内线数值），④ 约束明令不动国内线，故**未修**，建议另立工单
     （复现步骤与证据见运维指南 §4.4）。
  8. **已知未决（登记，不属本单）**：`market` 目前只接在**同步**端点 `POST /api/backtest` 上；
     异步 job 路径（`CreateJobRequest`）未接市场路由——job 的内核解析发生在 worker 执行期，
     接入需把路由表带进 worker 装配，属另一次改动。

### D8 · AI 因子挖掘流水线 + 治理层 ✅ 完成（2026-10-15，依赖 D3、D4；关键路径终点）

- **① 目标**：AI 挖掘闭环跑通——生成公式文本 → 沙箱验证 → 带哈希入库 → 策略即用（零代码改动）；治理层三件（过拟合检测、样本外验证、审计留痕）可演示。
- **② 上下文**：蓝图 §5.5/§6 三期③。产物是**公式文本不是代码**（LLM+MCTS 公式挖掘、AlphaPilot 学术路线同此，蓝图 §8）——故能动态加载、即挖即测。现有资产可复用：`pkg/ai`（沙箱、loop、gene_pool）、D3 公式引擎、D4 登记纪律。三条命根子（可复现/防偷看/可比）在本单的落法：公式带哈希锁版本、计算走快照、双侧同公式同快照可对拍。
- **③ 要求**：
  1. **闭环流水线**：AI 生成公式 → 沙箱验证（复用现有沙箱资产，安全约束不放宽）→ 入库（D4 四样齐全）→ 策略可引用。
  2. **治理层三件**：过拟合检测（如多重检验校正，注意 `adjP` 的 NumTrials 语义已有先例教训）、样本外验证、审计留痕（来源、哈希、验证记录可追溯）。
  3. **即挖即测**：新公式零代码改动可被策略引用并回测。
- **④ 约束与非目标**：不调挖掘算法本体（LLM/MCTS 是实现细节）；不放宽沙箱与安全约束；不绕过 D4 登记纪律；挖掘产物只进公式库不进代码库。
- **⑤ 验收**：
  1. **端到端样例留记录**：挖一条 → 测一条 → 用一条（全程哈希可追溯）。
  2. 机械判据：治理层各检验的单测通过；过拟合检测有「已知过拟合公式被标记」的正例 + 正常公式不误伤的反例。
  3. **破坏验证**：跳过沙箱直接入库 → 拦截红；去掉哈希锁版本 → 复现测试红；均还原绿。
  4. 三条命根子逐条验证记录（哈希复现 / 快照防偷看 / 双侧可比）。
- **⑥ 边界**：只做流水线 + 治理层；挖掘算法调优、因子应用策略（怎么用分）另立。
- **✅ 完成记录（2026-10-15）**：AI 挖掘闭环与治理层落地在 core 仓新包 `pkg/mining`（挖掘算法本体不在此——`Proposer`/`Evaluator` 接口把 LLM/MCTS 关在外面，符合 D8 ④）。**闭环流水线**：`Pipeline.Run` 逐条候选走「沙箱 → 治理 → 登记」——`Candidate`（产物是**公式文本**：Name/Formula/Validity/Caliber/Lookahead/Lookback/EffectiveFrom + `Provenance` 来源留痕）经 `Candidate.Definition()` 翻成 D3 的 `FactorDefinition`（内核固定 formula，`InputFields` 由公式 identifier 派生排序，与 `NewRegistry` 的 identifier↔InputFields 双向相等闸门天然一致）。**沙箱** `pkg/mining/sandbox.go`：复用 expression 引擎的解析器 + L1 闸门作为隔离边界（挖掘产物是公式不是代码，不开子进程编译），再过「identifier ⊆ 规范层字段词表 / Validity 同规则且字段 ⊆ 已声明 / lookback ≤ `DefaultMaxEvalBars`(4096) / 快照非空 / 有界求值产出至少一个有限值（空洞公式拒收）/ panic 兜底」，通过产出 `Attestation{ContentHash = Definition.VersionHash()}` 凭证。**治理层三件**：① 过拟合检测 `DetectOverfit`（`z=|IC|·√期数` → `rawP=2(1−Φ(z))` → `adjP=1−(1−rawP)^NumTrials`，**NumTrials 含失败**（先例 `pkg/ai/validation` 的教训）、失败占比过半附因、输入不合法 fail-closed 判过拟合）；② 样本外验证 `ValidateOutOfSample`（样本内外 IC 同号 且 |Out|/|In| ≥ 0.5，样本内为 0/NaN 或样本外缺失一律不通过）；③ 审计留痕 `domain.FactorMiningRecord`（以 (名字, 版本哈希) 为键，来源 / 凭证哈希 / 过拟合与样本外判定原样落库）+ `pkg/storage/mining.go` 的 `SaveFactorMiningRecord`/`LoadFactorMiningRecords`（写入过 `Validate`、加载即复核）+ Migration 054（`factor_mining_records` 表进 `postgres.go` 内联数组 + `docs/migrations/054_add_factor_mining_records.sql` 镜像；DB CHECK：生成器非空 / 来源枚举 / 尝试次数自洽 / **admitted ⇒ sandbox_hash==version_hash** / 被拒必须写明原因——绕过写入路径直接 INSERT 缺样也被库拒绝）。**即挖即测**：`Pipeline.Effective` 解析新引用命中的版本，`EvaluateOnSnapshot` 在只读快照上按登记定义算出序列——策略按名引用，零代码改动。**三条命根子逐条钉住**：① 可复现 = `VersionHash` 内容派生（改一个字符即变；重跑同输入同哈希，`TestCandidate_VersionHash_LocksContent`/`TestPipeline_Run_Reproducible`）；② 防偷看 = 沙箱与治理只读 `Snapshot`（只读适配器，无运行时查询入口，缺字段=NaN 而非 0）；③ 可比 = 同公式同快照两次求值逐点相等（NaN 感知，`TestEvaluateOnSnapshot_BilateralComparability`）。**名册边界（继承 D3，非本单放宽）**：`FactorDefinition.Name` 必须落在冻结名册内（`ParseFactorType`）——挖掘产物是「已有因子名的新公式版本」（改公式=新哈希），不动态新增因子名（属协议变更需正式评审），见 `pkg/mining/mining.go` 包文档。**验收⑤覆盖**：端到端「挖一条→测一条→用一条」留痕（`TestPipeline_EndToEnd_MineTestUse`，逐项断言版本哈希可追溯 + 凭证绑定 + 登记注册表接纳 + 用一条按 Lookback 窗口算出序列）；机械判据正例（弱 IC×500 次被标记）/反例（强信号 IC=0.30×252×1 次不误伤）各成测试；**破坏验证两组实跑闭环（改坏→红→还原→绿→`git diff` 零残留）**：① 禁用 `admit` 的「凭证非空」闸门 → `TestPipeline_Admit_RejectsWhenSandboxNotRun` 红；② `VersionHash` 改为内容无关 → `TestCandidate_VersionHash_LocksContent` 红。**验证**：`go build ./...` / `go vet ./...` 全绿；`pkg/mining`+`pkg/domain`+`pkg/storage` 测试全绿；`-race` 在 `golang:1.25` 容器内通过（本机 Windows 无 gcc，改容器跑）。**文档同步**：SPEC.md 新增「AI 因子挖掘流水线 + 治理层（D8）」大节（drift:factor_mining_record 锚点表）+ ARCHITECTURE.md §8 增补。**已知边界**：挖掘产物只进公式库不进代码库；过拟合检测用 IC 的 t 近似（非严密的 block-bootstrap），研究验证侧 IC 由 `Evaluator` 提供，本单不含挖掘算法本体与因子应用策略。
- **🔍 自审修正（2026-10-15，D8 提交后自查）**：① `pkg/mining` 暂无导入者，按「零件先写、后接线」（P2-9wire）登记进 `internal/repoguard` 的 `unwiredPackages`——`TestEveryPackageHasAConsumer` 曾因此红；接线（挖掘编排入口消费本包）时必须**删掉该白名单条目**。② `snapshotProvider.GetField` 此前无视 `lookback`，与生产 provider（`OHLCVDataProvider.GetField`：lookback>0 只取最近 lookback 根）窗口语义不一致——沙箱会在比生产**更长**的窗口上判「公式能跑」，窗口取窄的定义放行后到生产侧变形；已改为同款窗口（`TestSnapshot_ProviderHonorsLookback` 钉住）。③ `Pipeline.Run` 补 `evaluator` 非空闸门（此前 nil 会在 `runOne` panic，而 proposer / book / registrar 都有显式报错）。④ `DetectOverfit` 的 fail-closed 输入校验补「失败次数越界」（与 Migration 054 的 `failed_trials` CHECK 对齐）。
- **🔧 收口记录（2026-10-15，D8 入库前，由收口 agent 执行；不改写上述原始记录）**：
  1. **迁移号订正 054 → 055**：`docs/migrations/054_add_factor_mining_records.sql` 与同期并入 main 的 DATA-05 迁移 `054_ohlcv_turnover_thousand_cny_to_cny.sql` **撞号**，已改名 055 并同步全部引用（`pkg/storage/postgres.go` 内联注释两条、`docs/SPEC.md` 两处、`docs/ARCHITECTURE.md` 两处）。上文原始记录与自审记录中出现的「Migration 054」均指本次订正后的 **055**；迁移执行按 `migrationStatements()` 数组顺序（无版本号键控），故本次订正不影响已建库。
  2. **自审修正未提交状态收口**：8 个自审修正文件此前停留在工作树，已提交（`fix(D8)`）。
  3. **独立破坏复验**：收口 agent 对破坏腿①做了独立复跑（把 `admit` 的「凭证非空」闸门改成默认放行 ⇒ `TestPipeline_Admit_RejectsWhenSandboxNotRun` 红；还原后 sha256 与基线一致、复绿、工作树零残留）。附带确认纵深：即便绕过非空闸，`Attestation.ContentHash != VersionHash` 的绑定闸仍会二次拦截。
  4. **如实登记（✅ 口径边界）**：`pkg/mining` 目前**无生产导入者**（无 `cmd/` 编排命令、无 HTTP 端点）——③1「闭环流水线」与 ③3「即挖即测」在**库层**已成立（`Pipeline.Run` + `Effective` + `EvaluateOnSnapshot` + D4 登记 + D3 注册表，端到端测试覆盖），**生产编排入口未接**（已按 P2-9wire 登记 `unwiredPackages`），接线属另立工单。本单 ✅ 按「库与服务齐备、编排入口另立」口径记。
  5. **与 main 合并**：`docs/ARCHITECTURE.md` / `docs/SPEC.md` 与主线（D6 / DATA-05 文档改动）有内容冲突，已人工消解（本单内容全保留，主线内容全保留）；`go build ./...` + 受影响包测试全绿。

### D9 · 国外市场数据源选型调研 ✅ 完成（2026-10-14，独立，随时可插）

- **① 目标**：产出选型报告（归档 quant-trading-docs 仓）：NT 原生数据通道优先 + ≥2 个外部候选对比，给出明确推荐与接入成本估算。
- **② 上下文**：蓝图 §9 #4（已拍板倾向：NT 原生数据通道优先，另调研）。服务对象：D7 的数据通道与后续国外 connector。
- **③ 要求**：
  1. 对比维度：覆盖市场 / 频率 / **时间语义可声明性** / 价格与 token 配额 / license。
  2. **硬判据**：时间语义声明不了的源直接排除（D2 fail-closed 红线在选型期生效）。
  3. 产出：推荐结论 + 接入成本估算 + 喂给 D7/未来 connector 工单的**接口需求清单**。
- **④ 约束与非目标**：不实现 connector、不签约付费源、不动现有数据链路；报告写进 docs 仓（设计文档归 docs 仓的长效机制）。
- **⑤ 验收**：报告落档 docs 仓且含四要素（对比表、硬判据筛查记录、推荐、接口需求清单）；结论可直接抄进后续 connector 工单的②上下文。
- **⑥ 边界**：只调研不实施。
- **✅ 完成记录（2026-10-14）**：选型报告已落档 **quant-trading-docs 仓 `docs/design/d9-foreign-market-data-source-selection.md`**（跨仓指针不使用可解析相对链接，DOCS-01 排除条件）。四要素齐备（对比表 / 硬判据筛查记录 / 推荐+接入成本 / 接口需求清单 I-01…I-16）；10 个候选项中 **4 项因「时间语义不可声明（值被未来事件重述）」被一票否决**，6 项通过。**主选 Databento**（NT 官方 adapter 通道），**阶段 0 用交易所 crypto（零成本）**供 D7 样例 run；**正式链路必须回灌 D1 规范层**（NT adapter 直连 catalog 仅允许打样/研究态）。后续「国外 connector」工单可直接抄 §0 结论与 §7 清单作为 ②上下文 与 ③要求；报告内嵌机器可读候选登记表 + stdlib-only 校验脚本（A1–A7 断言），可直接复用为 I-16 的 PIT 护栏，破坏验证 3 组均「改坏变红、还原变绿」。

---

## 二、搬入项（2026-10-14 旧台账迁入，未完成）

> 来源：[archive/TASKS-2026-10-14-archived.md](archive/TASKS-2026-10-14-archived.md) §八/§九（本地栈实测 + ODR-067 登记），完整证据链在归档卷，此处不双写。

### DATA-01 · 票池三表灌数 + universe 指数展开链路修复 ✅ 完成（2026-10-14）

- **① 目标**：`index_constituents` / `sectors` / `stock_sector_map` 三表有真实数据，且探索链路的 `universe: csi300` 能展开成真实票池（`explore` 链路不再恒空票池）。
- **② 上下文**：归档卷 §八复查记录（2026-10-14 实测）：三表各 0 行 ⇒ YAML 默认 `universe: csi300` 解析为空票池；且 core 仓 `pkg/backtest/engine.go:556` `resolveStockPool` 只在 `len(StockPool)==0 && req.IndexCode != ""` 时展开指数，而探索链路（**quant-trading-agent 仓** `pkg/ai/intent/parser.go:117` → `pkg/ai/yaml/generator.go:198` → `pipeline.go:868 parseUniverse` → `cmd/ai/builders.go:47`）把字面量 `"csi300"` 放进 `StockPool`、**从不填 `IndexCode`** ⇒ 展开分支结构上不可达，`"csi300"` 被当成一只股票代码（core 仓 `engine.go:1125` Debug 级丢弃）。**⚠️ 展开链路修复横跨 core（engine）与 quant-trading-agent（parseUniverse/builders）两仓**，跨仓原子提交 core 先行。**光灌数不够，链路也要修**（OBS-01 验收增量）。同步链路存在：`cmd/data/handlers_sync.go` + `pkg/data/tushare.go`（`index_constituents` 相关）。
- **③ 要求**：
  1. **灌数**：三表经既有同步链路补真实数据（真 token 实测，沿用 P2-1/P2-2 真库核对惯例）；`stock_sector_map` 灌上后 `sector` 字段可用性自动转真（OBS-08 可用性声明是运行时判定，勿硬编码）。
  2. **链路修复**：探索链路的 `universe` 识别指数代码并走 `IndexCode` 展开分支（`"csi300"` 不再进 `StockPool`）；真票池规模进入 `PoolSize`（与 OBS-01-R1 的「不打印未测量数字」配套：修复后 0 才是测量值）。
  3. fail-loud 保持：真展开仍为空 → 维持 invalid 报法（OBS-01 语义不变）。
- **④ 约束与非目标**：不改 `CheckValidity` 语义（OBS-01 已完成面不动）；不趁机改验证器；灌数走既有同步链路不写一次性脚本进库。
- **⑤ 验收**：真库三表行数 > 0（查询留证）；`universe: csi300` 走 `IndexCode` 的单测 + 端到端探索 run 票池非空（留 run id）；**破坏验证**：清空 `index_constituents` 一行 → 展开为空 fail-loud 变红路径可复现，还原绿。
- **⑥ 边界**：只做三表数据 + universe 展开链路；其他数据缺口（`stock_fundamentals` 0 行等）另立。
- **✅ 完成记录（2026-10-14，跨仓两段提交 core 先行）**：core `5528010`（+ gofmt `5b1bc52`、误带入他人改动的撤销 `7c8f49f`）；agent 仓 `8622fcc`（+ gofmt `90d120d`）。
  - **三表实测（真库 PG 17.5，HTTP 端点灌数）**：`index_constituents` 354 行（300 只仍在最新期）、`sectors` 110、`stock_sector_map` 5575；端点 `POST /sync/sectors` → `{"sectors":110,"mappings":5575,"source":"stocks_industry"}`、`POST /sync/index-constituents/000300.SH` → `{"count":354}`，重跑幂等。
  - **两处代码级根因（逐字核对，非推测）**：① `FetchIndexConstituents` 请求 fields `in_date,out_date` 而 `index_weight` 无此两列 ⇒ Tushare 静默只回两列 ⇒ `len(item)<4` 全跳过 ⇒ 表恒空；② 展开查询是半开区间 `in_date<=d AND (out_date IS NULL OR out_date>d)` ⇒ out_date 零值必须落 NULL（否则 `'0001-01-01'` 判为全退出）、退出日须取最后出现期**次日**（否则只在单期出现过的成分股永远进不了池）。已由多期快照推导 in/out 区间。
  - **链路修复**：core 新增 `indexCodeAliases`（`csi300→000300.SH` 等，未命中原样透传 ⇒ 既有 fail-loud 接管）；agent 新增 `buildBacktestRequest`/`splitUniverseIndex`（白名单式识别，**只识别不翻译**，宁可漏判也不把真股票误判成指数）。
  - **端到端实测**：`TestData01_IndexUniverse_EndToEndBacktest`（真库+真引擎）`universe=[csi300]` → **票池规模 301**，6.9s 跑完。
  - **破坏验证**：`pkg/backtest/universe_expand_integration_test.go` 常驻 —— 清空成分 ⇒ `resolveStockPool` fail-loud 报错且不返回票池；Cleanup 写回 ⇒ 恢复 300 只（改坏→红→还原→绿自动化，不留脏数据）。
  - **⚠️ 缺口登记（不属本单边界，需裁决）**：`pkg/ai/yaml/generator.go:190-191` 硬编码默认回测区间 **2020-01-01~2024-01-01**，而成分快照只覆盖 **2024-10-31 起** ⇒ explore HTTP run 用默认区间**必然**展开为空并 fail-loud（已跑 run `44c7f22d`、`e1441a23` 留证：`params.universe=["csi300"]` 已正确，倒在区间外）。修默认区间会影响所有新建回测，不属本单。另：现场装配未注册任何策略（引擎日志 `available: []`），回测必然 0 交易 ⇒ `invalid_reasons=[zero_trades,garbage_metric]`，属既有装配状况，另立。
  - **顺手修（边界内，已标注）**：`cmd/ai/builders.go` 的适配器逐字段手工拷贝时漏了 `UniverseMaxSize`（票池规模）与 `InvalidReasons`，正是「真票池规模进 PoolSize」链路上的一环，与归档卷 §八 #8（OBS-01-R2 同型、位置不同）一并补齐两个字段。
  - **环境事实**：东财 `push2.eastmoney.com` 实测 **502** ⇒ 板块表既有 eastmoney 链路不可用；Tushare 无个股→行业接口（`stock_industry` 不存在、申万 `index_member` 的 `is_new` 全为 N）⇒ 改由 `stocks.industry`（5908 只全覆盖、110 个细分行业）物化，快照类字段留 NULL 不编造，`source='stocks_industry'` 与东财板块区分。

### OBS-01-R1 · 偏差维输出一个它没量过的数字 ✅ 完成（2026-10-14）

- **① 目标**：验证器偏差维在池子规模未测量时**不打印数量**（「0 只」不再冒充实测值），屏幕上每个数字都可被当场质问。
- **② 上下文**：归档卷 §八工单表 #7（2026-10-14 立单，**代码在 quant-trading-agent 仓**）：`cmd/ai/handlers_explore.go:50` 设 `PoolSource=PoolSourceCurrent` 但从不设 `PoolSize`（只有 `:96` 预热成功路径才填）⇒ `pkg/ai/validation/bias.go:296-303` 无条件把 `in.PoolSize`（Go 零值 0）插进 blocking 文案 ⇒「股票池按今天的上市名单取（**0 只**）」。**0 是零值不是测量结果**（愿景原则 4：每个数字可被当场质问）。`PoolSize<=0` 的「未测量」分支已有先例（`bias.go:202` 与 `:314`），只有 `PoolSourceCurrent` 这支漏了。**概率侧没有 bug**：`biasProbability` 对 `PoolSourceCurrent` 提前返回 `BiasProbFloor`，低分有意为之，错的只有那句话。
- **③ 要求**：`PoolSize<=0` 时不带数量措辞（对齐 `bias.go:202`/`:314` 既有「未测量」分支先例）；文案改动最小化。
- **④ 约束与非目标**：**不改 `biasProbability` 与 `PoolSourceCurrent` 的概率**；不借机重构偏差维；不动其他维。
- **⑤ 验收**：单测：`PoolSize=0`（未测量）输出无数量词、`PoolSize>0` 输出带真实数量；**破坏验证**：改回无条件打印 → 新测试红，还原绿。正常 run 输出文案回归不变（有测量值场景）。
- **⑥ 边界**：只改这一处措辞分支。
- **✅ 完成记录（2026-10-14）**：**代码修复由并发 agent 落地**（quant-trading-agent 仓 `f432074`）：`biasChallenges` 幸存者 switch 新增 `PoolSource == PoolSourceCurrent && PoolSize <= 0` 前置分支，文案改「（池子规模未测量）」，对齐 `bias.go:202`（概率侧未评估返回 0）与 `bias.go:323-328`（文案侧「幸存者偏差未评估」note）既有先例（任务原文引 `:314` 系笔误，该行实为「池子不小却无退市股」的带数量 warning 分支）；severity 保持 blocking，`biasSurvivorship` 对 `PoolSourceCurrent` 仍返回 `BiasProbFloor`——④「概率不动」达成，③「文案改动最小化」达成（+9 行）。**两腿单测**：`TestBias_CurrentPoolWithoutSizeSaysUnmeasured`（零值输出含「未测量」且不含「0 只」）+ `TestBias_CurrentPoolWithSizePrintsCount`（PoolSize=300 带真实数量，⑤ 回归场景）。**本单收口复验（独立重做，不轻信原提交声称）**：`go test ./pkg/ai/validation/ -count=1` 全绿（42.4s）、gofmt/vet 干净；**破坏验证**：把该分支改回原始 bug 形态（无条件 `fmt.Sprintf` 打印零值）→ `TestBias_CurrentPoolWithoutSizeSaysUnmeasured` 精确变红 → 还原 → 复绿，`git diff` 零残留；另做过一次「只拆条件不改文案」的变体验证（红在 WithSize 腿，亦还原），两变体共同证明护栏非空转。**接线现状核对**（`cmd/ai/handlers_explore.go`）：静态 `exploreBias`（:40-53）不带 `PoolSize` = 未测量路径，文案不再冒充实测值；`biasInput()`（:86-103）在引擎预热到上市日历时填 `PoolSize=len(windows)` = 测量路径带真实数量。**提交方式说明**：本标记在 D3 批次（core `5225df5`）提交后以 pathspec 单文件提交，未触碰并发 agent 在途的 7 个无关脏文件。

### OBS-01-R2 · AI 链路把 K9 三值丢在客户端边界上 ⛔ 阻塞（Ask First：改法 a/b 待若曦裁决）

- **① 目标**：AI 侧消费者（Copilot 播报、实验日志、`pkg/ai/loop` 候选评估）能读到 completed / invalid / failed 三值状态，不再只靠 `invalid_reasons` 反推。
- **② 上下文**：归档卷 §八工单表 #8（A-07 收口时逐字核出；**客户端代码在 quant-trading-agent 仓**，domain/core 契约在 core 仓）：agent 仓 `pkg/ai/client/backtest_client.go:47` 的 `RunBacktest` 解出了 `BacktestResponse.Status`（同文件 `:43`）但返回值只有 `*domain.BacktestResult` ⇒ 三值被丢弃；core 仓 `pkg/domain/backtest.go:5-39` 的 `BacktestResult` **没有 `Status` 字段**（只有 `InvalidReasons`，`:38`）。响应语义在 core 仓 `pkg/backtest/contracts/contracts.go:250-252`（Status 与 Error/InvalidReasons 搭配属响应层）。**改法 (a) 涉及 core 仓 domain，改法 (b) 只动 agent 仓**；若走 (b) 则本单纯 agent 仓单点改动，无需跨仓原子提交。⚠️ **不是用户可见假成功**（引擎 `CheckValidity` 每次返回前挂 Code，前端 A-07 已按三值接住并自救），登记的是**契约残缺**。
- **③ 要求**（两条路，**属 Ask First——动 `pkg/domain` 核心类型或 AI 客户端公开签名，落笔前须若曦裁决 a/b**）：
  - **(a)** `domain.BacktestResult` 加 `Status` 字段，引擎 `runBacktestInternal` 末尾按 `contracts.Status*` 常量填；
  - **(b)**（倾向）`RunBacktest` 返回带 `Status`/`Error` 的小结构，不动 domain——三值在规格里本属响应层，塞进「运行指标集合」会造成同一概念两处真相。
- **④ 约束与非目标**：不改前端（已自救且有 7 条单测钉住）；不借机重构 pipeline 结果结构；K9 三值语义（completed/invalid/failed）不变。
- **⑤ 验收**：单测断言 `RunBacktest` 返回能区分三值、invalid 时 `InvalidReasons` 非空；真请求 `POST /api/backtest` 确认线上 JSON `status` 与结构体字段一致；`go test -race` 全绿。
- **⑥ 边界**：只补三值透传（按裁决的 a/b 之一）；前端、pipeline 结构不动。

### DOCS-01 · AGENTS.md 文档指针在文档拆仓后集体失效（🟠）✅ 完成（2026-10-14）

- **① 目标**：`AGENTS.md` 里每条 `docs/...` 引用都能在某个工作树命中，且由一条机器断言守住（新增死链当场变红）。
- **② 上下文**：归档卷 §九（2026-10-14 实测）：core 仓 `AGENTS.md` 共 114 处 `docs/*.md` 引用，**34 个不同目标在本仓不存在**（28 个已迁 quant-trading-docs 仓、3 个已迁 ui 仓、3 个幻影引用——`docs/E2E_TEST_GUIDE.md` 等在三仓 git 历史中从未存在；**抽查补充 2026-10-14**：`guides/nt-sidecar-operations.md` 实际在 quant-trading-docs 仓 `docs/guides/` 下，登记引文写成 core 仓 `guides/` 系笔误，归因时按 docs 仓计）；`docs/archive/odr/` 是空目录而 AGENTS.md §3/§10/§11 仍按「ADR/ODR 都在本仓」描述。归因表（逐目标去向）在归档卷 §九，不双写。

#### 执行记录（裁决 + 证据，2026-10-14）

**三选一 = (b) 显式跨仓措辞。** 每条跨仓死链 `[标签](docs/Y.md)` 改写成 标签（`quant-trading-docs:docs/Y.md`）／（`quant-trading-ui:...`）——路径置于反引号内联代码、去掉 `]()` 链接语法，即非可解析相对链接。
- 排除 **(a) 转发存根**：在 core `docs/` 造 ~33 个同名占位文件，会与真身（docs/ui 仓）影子分叉，grep 到 core 的 `PRODUCT.md` 会被误信；每迁一份就得补一个存根，维护面随迁出增长。
- 排除 **(c) 文档归属表**：正文改指单表 ≈ 重排 §11 导航（违 ④「不重排章节」），且守卫仍需逐链解析，收益不比 (b) 大。
- 硬排除已守：无一条依赖 `../quant-trading-docs/...` 相对路径（三仓不保证同布局检出）。

**守卫 = 扩展既有 `tools/check_doc_links.py`（CI `docs` job 已默认跑），不另起 Go 测试**——避免两套链接守卫各自漂移。改动 = 入口层严格扫描：把仓库根 `AGENTS.md` 纳入扫描，且**入口文档不吃 `MIGRATED_*` 跨仓白名单**（活跃层 `docs/*.md` 维持原白名单行为，不回归）；语义即「入口文档写的每条相对链接必须命中 core」，命中不了即红（空基线零容忍，等价 ui 仓 A-02/A-03 棘轮的满格形态）。

**⑤ 验收逐条：**
- 首跑抓到 34：扩展守卫后、修复前 `python tools/check_doc_links.py` 报 `AGENTS.md -> …` **70 处 / 34 个不同目标**，与 §九 吻合（清单脚本枚举，非手抄）。
- 修复后归零：AGENTS.md 贡献 **0** 条坏链，`✓ 未发现坏链`。
- 破坏验证：临时注入 `[x](docs/__nonexistent_doCS01__.md)` → 守卫当场点名该条变红；还原 → `✓ 未发现坏链`。
- 幻影清零：真实幻影仅 `docs/E2E_TEST_GUIDE.md` 一条（三仓 git 历史 0 命中），已改指 `quant-trading-docs:docs/TEST.md` §7.2「E2E Tests — Playwright」（内容实际所在），不留悬空。

**对 §九 归因的实测更正**（逐个 `existsSync` 三仓核对）：`docs/archive/RESEARCH-equitydeep-legacy.md` 与 `docs/archive/README.md` **实存于 quant-trading-docs 仓**（拆仓 commit `a5e18e3` 迁过去），非幻影；其中 `archive/README.md` 在 AGENTS.md 是反引号目录散文引用、非 markdown 链接，守卫本就不扫。另 `docs/design/pages/`（§14 速查）是 §九 未列的第 4 条 ui 链接。修复集按归属计：**29 → docs 仓、4 → ui 仓、1 → 幻影**（合计 34）。

**顺带收口**（同守卫扫 `docs/*.md`，且属硬排除模式）：`docs/TASKS.md:11` 两条 `../quant-trading-docs/...` 相对链接改显式仓内路径——该行 ⚠️ 注记本就写「见 DOCS-01 排除条件」，此处闭合。

**登记既有红（非本单引入，HEAD 已红，另立处理，不混入本 commit）**：① 默认档残留 2 条 R1 frontmatter 缺（`docs/ARCHITECTURE.md`、`docs/TASKS.md`）——非指针，越 ⑥「只做指针+守卫」界，留文档元数据单；② `--include-archive` 档残留 8 条死链，全在 `docs/archive/TASKS-2026-10-14-archived.md`（含 `../AGENTS.md` 应为 `../../`）——归档层，④ 仅「许可」修、非本单义务；③ AGENTS.md 尾注「`E2E_TEST_GUIDE.md`/`TEST.md` 还在本仓」与实测不符（`TEST.md` 已迁 docs 仓、`E2E_TEST_GUIDE.md` 从未有）——属版本史散文，§④/⑥ 排除，未动。

- **③ 要求**：先归因（已完成，见归档卷）；再三选一并写理由——**(a)** core `docs/` 放转发存根（每死链一文件，正文只写「已迁 X 仓 yyy.md」）／**(b)** 改显式跨仓措辞（`quant-trading-docs:docs/PRODUCT.md`，不带可解析相对链接）／**(c)** 建「文档归属表」，正文只引该表。**排除条件（硬）**：三仓不保证同布局检出，**任何依赖 `../quant-trading-docs/...` 相对路径的方案先排除**。无论选哪条**必须同时落守卫**：扫 `AGENTS.md`（及 core `docs/*.md`）相对链接，命中不了即红；可抄 ui 仓 A-02/A-03 棘轮基线做法（悬空集合=登记集，双向红）。三条幻影引用要么补出文件、要么从 `AGENTS.md` 删除。
- **④ 约束与非目标**：不改任何文档正文的判断内容（只处理指针）；不重排 `AGENTS.md` 章节；不改 `docs/archive/` 归档内容（唯一例外是失效相对链接可修——本单在此例外内）；不顺手修「指向仍存在文件」的措辞问题。
- **⑤ 验收**：守卫默认随检查跑；**首次上线必须当场抓到这 34 条**（抓不到=假护栏，须做「改坏→变红→还原」）；幻影引用清零（补出或删除）；三选一的裁决理由写进工单记录。
- **⑥ 边界**：只做指针 + 守卫；文档内容重写与 AGENTS.md 版本史整理另立。

### DOCS-02 · 链接守卫整体转绿（DOCS-01 登记的既有红）✅ 完成（2026-10-14）

- **① 目标**：`python tools/check_doc_links.py`（默认档）与 `--include-archive`（归档档）两档退出码均为 0，CI `docs` job 全绿。
- **② 上下文**：DOCS-01 落地后守卫剩两类红，**均 HEAD 已红、非 DOCS-01 引入**（⑥ 边界排除、已登记于 DOCS-01 执行记录）：
  - **(A) 2 份入口层文档缺 R1 frontmatter** — `docs/ARCHITECTURE.md`、`docs/TASKS.md` 缺 `status/last-verified/verified-by`。判据在 `tools/check_doc_links.py::check_entry_frontmatter`，范围由 `docs/README.md` 的链接推导（`SPEC.md` 已有 frontmatter，作格式基准）。
  - **(B) 8 条死链全在 `docs/archive/TASKS-2026-10-14-archived.md`**（仅 `--include-archive` 触发）：行 10 / 980 `[...](../AGENTS.md)`、行 19 / 196 `[...](design/kernel/*)`、行 370 / 387 `[...](docs/design/kernel/k15-dualrun-report.md)` 系**层级笔误**（真实目标在 core，改 `../../AGENTS.md`、`../design/kernel/*` 即命中）；行 9 `[...](archive/TASKS-2026-10-08-archived.md)`、行 462 `[ADR-030](adr/adr-030-draft-observed-improvements.md)` 系**跨仓引用**（真实目标在 quant-trading-docs 仓）。
- **③ 要求**：(A) 给 `docs/ARCHITECTURE.md`、`docs/TASKS.md` 顶部补 R1 frontmatter 三字段，日期与 `verified-by` 如实对应最近一次内容对账事件；(B) 5 处层级笔误改成能命中 core 的相对路径；2 处跨仓引用按 DOCS-01 裁决 (b) 改显式 `quant-trading-docs:docs/...` 内联指针（保留原文件名结论）。
- **④ 约束与非目标**：不改归档正文判断内容（仅修链接指针，落 §4「失效相对链接可修」例外内）；**不动 `check_doc_links.py` 守卫逻辑**（DOCS-01 已定，本单只让数据满足它）；不新增依赖；不碰 config-path 检查与既有 `MIGRATED_*` 白名单。
- **⑤ 验收**：默认档 `✓ 未发现坏链` + 退出码 0；`--include-archive` 退出码 0；破坏验证——临时把任一层级死链改错 → 对应档变红 → 还原 → 绿；`python -m py_compile tools/check_doc_links.py` 通过。
- **⑥ 边界**：只做「让既有红归零所需的最小指针 / 元数据修复」；文档内容重写、AGENTS.md 版本史散文（尾注 `E2E_TEST_GUIDE.md`/`TEST.md` 表述）仍另立。

> **收口记录（2026-10-14）**：两档 `python tools/check_doc_links.py`[ `--include-archive`] 均退出码 0、`✓ 未发现坏链` + `✓ 入口层文档都带 R1 frontmatter`。补 `docs/ARCHITECTURE.md`（last-verified 2026-10-12）/`docs/TASKS.md`（2026-10-14）frontmatter；归档 `../AGENTS.md`×2 改 `../../`、`design/kernel/*`×2 与 `docs/design/kernel/k15-*`×2 改对层级（均在 core 命中）、`archive/TASKS-2026-10-08-archived.md`+`ADR-030`×1 各改 (b) 显式跨仓指针。**破坏验证**：临时把一条改回 `../AGENTS.md` → `--include-archive` 报 1 条坏链退出 1 → 还原 → 退出 0 绿。未动守卫逻辑。

### OPS-01 · 观察页状态轮询 SSE 化 ⛔ core 侧关闭（2026-10-14 改判：前提失效，实现移 agent 仓）

- **① 目标**：观察页 run 状态从轮询改为 SSE 推送（或给只读状态端点单列限流预算），消除 429 与真失败混淆的土壤。
- **② 上下文**：归档卷 §八 6 号裁决（2026-10-14）：`/api/explore/runs/:id` **不进** `RateLimitExemptPaths`（逐字相等匹配机制上做不到 + 判据不满足），真解是 SSE；本仓已有先例 `/api/sync/stream`（前端已接 `EventSource`）。前端已自救（`POLL_MS=2500` + 连续 6 次失败才认断线 + e2e 钉住），本单是后端真解，不急。
- **③ 要求**：观察页 run 状态走 SSE（复用 `/api/sync/stream` 模式）或只读状态端点单列限流预算（二选一，实现时定）；前端接线顺带（ui 仓另立小单，跨仓两段式 core 先行）。
- **④ 约束与非目标**：不动 `IsRateLimitExempt` 的逐字匹配语义（安全不变量）；不改前端既有自救逻辑的判定阈值；不动限流全局参数。
- **⑤ 验收**：SSE 通道状态推送测试通过（或限流预算单列测试）；观察页轮询频率显著下降或消失（前端单确认）；回归：`/api/sync/stream` 既有行为不变。
- **⑥ 边界**：只做观察页状态通道；其他轮询端点另立。

> **core 侧改判关闭（2026-10-14，派工即核查触发，逐条源码核过）—— 前提失效，本单在 core 无法按原样落地：**
> 1. **explore 已不在 core**：`pkg/ai` / `pkg/tools` 在本仓不存在（AI 拆仓 ADR-027；AGENTS.md §1 明写 copilot/pipeline/explore/tools 四族 handler 现住 `quant-trading-agent` 仓）。core 对 `/api/explore/*` 只做纯反代（`cmd/analysis/handlers_ai_proxy.go:63`）——**既无 explore handler，也无 run 状态数据源**，core 侧 SSE 无从推起。⑤「SSE 通道状态推送测试通过」在 core 不可成立（被测源不在此）。
> 2. **core 其实已 SSE-safe**：同代理 `handlers_ai_proxy.go:53` `proxy.FlushInterval = -1 // SSE 逐写直通`（文件头注释已点名流式端点直通）。即 agent 仓一旦暴露 `GET /api/explore/runs/:id/stream`，core 现有代理**原样直通、零新增代码**——拆仓时就把「core 侧要做的」做完了。
> 3. **改判**：实现移 **`quant-trading-agent` 仓另立单**——**已立 `OPS-01-A`（`quant-trading-agent/docs/TASKS.md`）**：在 run 状态源处产出 `runs/:id/stream` SSE，形状复用 data-service 先例 `sseProgressHandler`（`cmd/data/sync_handlers.go:761`）；ui 前端接线仍另立（跨仓两段式）。本 core 单**关闭**，保留可追溯指针不删。
> 4. **不动限流器**：§八 6 号「`/api/explore/runs/:id` 不进 `RateLimitExemptPaths`（逐字匹配做不到 + 判据不满足）」的裁决**继续有效**；本单不因 SSE 触 `IsRateLimitExempt` 语义或全局预算。
>
> **顺带纠偏**：本单 ②③⑤ 引用的先例 `/api/sync/stream` 在本仓**不存在**；真实 SSE 先例是 `GET /api/sync/jobs/:id/progress`（在 **data-service**，非 analysis），且它本身是每 2s 轮询 store 再转推 SSE（非 pub/sub）。不影响结论——SSE 省 429 的原理是「N 次轮询请求（前端 `POLL_MS=2500`≈24 req/min/页）→ 1 条长连接只占 1」。

---

## 二·补 · A-08 前置实测新增项（2026-10-14，源自 ui 仓 A-08 依赖面测量）

> 背景一句话：A-08（数据同步页）要展示 worker / scheduler 统计，实测发现**后端这条链路是死的**，
> 于是先修后端再画前端（「前端不去干预后端设计，后端要改就整体改」）。三条派生项见 SYNC-02/03。

> **入库形态警示（2026-10-14）**：本节正文（SYNC-01/02/03 三条工单）由 commit `5ce6a57`
> （message 只写「D9 国外市场数据源选型调研完成」）**携带入库**，非该单本意——同一工作树有
> 并发会话在提交，它的 `git add` 扫进了尚未提交的这一节。内容与 HEAD 逐字节一致
> （`diff <(git show 5ce6a57:docs/TASKS.md) <(git show HEAD:docs/TASKS.md)` 抽不出这一段的差异），
> **只是归属串了**。SYNC-01 的代码与验收证据在 `65a31a7`（`fix(sync): 调度器接上装配点`），
> 那才是这一节对应的实现 commit。不 rewrite 历史是刻意的：并发会话正在同一工作树连续提交。

### SYNC-01 · 调度器全仓零调用方 ⇒ 排程永不触发 ✅ 完成（2026-10-14）

- **① 目标**：data-service 进程启动即把 cron 调度器跑起来，`sync_schedules` 中 `is_active=true` 的排程**真的会触发**；`GET /api/sync/scheduler/stats` 不再是结构上永远 0/0。
- **② 上下文**：
  - 缺陷形状（2026-10-14 `git grep` 对 HEAD 实测）：`Scheduler.Start()` / `Stop()`（`pkg/sync/scheduler.go:47`、`:61`）**在 `cmd/` 与 `pkg/` 全仓零调用方**——生产没有，测试也没有；同批同步接线的唯一 `.Start()` 是 `cmd/data/sync_handlers.go:71 h.workerPool.Start()`。`SyncHandler` 构造时把 7 种 job type 都注册进了 WorkerPool，调度器却从未被启动。
  - 后果实测（本地栈，容器 `quant-trading-data-service-1`）：修复前 `GET /api/sync/scheduler/stats` → `{"active_schedules":0,"total_entries":0}`，且无论 toggle 与否都不变。
  - 「函数写得对但零调用方」这类缺陷**编译期看不见、行为测试也看不见**（行为测试自己会调 `Start()`，于是永远绿）——同族先例见 `cmd/analysis/wiring_guard_test.go`（K1 切片 3），故本次两腿都补。
- **③ 要求**：
  1. `SyncHandler` 暴露生命周期入口 `StartScheduler() error` / `StopScheduler()`（与既有 `StartWorkerPool`/`StopWorkerPool` 同一组），`Start` 的 error **返回**而非就地吞掉，由装配点决定爆炸半径。
  2. `registerRoutes` 返回 `*SyncHandler`，`main()` 在建路由之后、`startHTTPServer` 之前启动调度器；启动失败 **fail-loud 进日志**（`logger.Error().Err(err)`）但不 kill 进程（store 此时已可达，为调度器起不来而让整个 HTTP 挂掉是把局部失败放大成全局）。
  3. 关闭顺序：先 `gracefulShutdown(srv)` 再 `StopScheduler()`——cron 不能在 worker 正在拆时新建作业。
  4. 两条守卫腿：行为腿（`pkg/sync/scheduler_start_test.go`）+ 接线腿（`cmd/data/wiring_guard_test.go` 扫 `main.go`/`sync_handlers.go` 的**调用形态**）。
- **④ 约束与非目标**：不改 `Scheduler`/`ScheduleStore` 接口签名；不动 `loadAndScheduleAll` 的 cron 解析容错（坏行 warn 跳过）；不加新依赖；**不顺手修 SYNC-02/03**（另立）；不改 `docker-compose.yml`。
- **⑤ 验收**（全部为**真实退出码**，不取管道尾巴）：

  | 判据 | 命令 | 实测 |
  |---|---|---|
  | 行为腿绿 | `go test ./pkg/sync/... -count=1` | `ok`，exit 0 |
  | 接线腿绿 | `go test ./cmd/data/... -count=1` | `ok`，exit 0 |
  | 全仓构建/静态 | `go build ./...` / `go vet ./...` | exit 0 / exit 0 |
  | 格式化 | `gofmt -l` 四个改动文件 | 空输出 |
  | **正证据（活进程）** | `docker compose build data-service && docker compose up -d data-service`，读容器日志 | 三行齐备：`scheduler.go:48 "Starting sync scheduler"` → `:254 "Loaded and scheduled active schedules" count=0` → `:56 "Sync scheduler started"` ⇒ 证明 `main()` 真的走到了这一行 |
  | **正证据（active 会登记）** | `POST /api/sync/schedules/1/toggle {"active":true}` → `GET /api/sync/scheduler/stats` | `total_entries` **0→1**，`next_runs` 出现 `2026-10-15T09:00:00Z`；再 toggle `false` → 回 `0/0`，`is_active=false` 已还原（现场无残留，未触发任何真同步） |
  | **破坏验证 (a) 接线腿非空** | 把 `main.go` 的启动块替换成 `_ = syncHandler` | `--- FAIL: TestStartupStartsScheduler … main 未启动调度器 ⇒ 排程永远不会触发（SYNC-01）`；还原后 `git diff` 该文件为空 |
  | **破坏验证 (b) 行为腿非空** | 在 `pkg/sync/scheduler.go Start()` 里用 `if false {…}` 罩住 `loadAndScheduleAll` | `--- FAIL: TestSchedulerStartFiresActiveSchedule (3.00s) … Start() 之后 3 秒内没有产生任何作业行`；还原后 `git diff pkg/sync/scheduler.go` 为空，全仓 `grep -rn MUTATION cmd pkg` 零命中 |

  - **验收口径是「作业行数」不是计数器**：`ToggleSchedule` 在 `Start()` 之前也会写 `s.entries`，所以 `active_schedules` **在调度器从未启动的状态下同样能变成 1**——只看计数器会被骗。行为腿因此断言 store 里的 job 行；`TestToggleScheduleWithoutStartDoesNotFire` 把这条反证**永久钉成测试**（不调 `Start()`，toggle 后等 400ms 断言零作业行）。
  - ⚠️ **实测边界（诚实登记）**：`go test -race` 本机**跑不了**（`gcc` 不存在，CGO_ENABLED=0）——本单并发面只有 cron goroutine 与既有 mutex 路径，未获 race 证据；另 `gofmt -l cmd pkg` 有 **8 个存量未格式化文件**（含 `cmd/data/setup.go`，HEAD 即如此），非本次改动，与 AGENTS.md §6「整仓 gofmt 可以当结论用」相矛盾，待单独收口。
- **⑥ 边界**：`cmd/data/main.go`、`cmd/data/sync_handlers.go`（仅新增两个生命周期方法）、两个新测试文件；调度器内部逻辑与前端零改动（A-08 才动前端）。

### SYNC-02 · `StopWorkerPool` 零调用方：关闭不排空在跑的作业 ✅ 完成（2026-10-14，commit `fb7cfe0`）

- **① 目标**：SIGTERM 后 worker pool 优雅停止，正在跑的同步作业要么跑完要么被标记为可重试，而不是随进程一起被掐断。
- **② 上下文**：SYNC-01 复测时顺带 grep——`cmd/data/sync_handlers.go:75 StopWorkerPool()` 全仓零调用方（`StartWorkerPool` 有，`main.go:138`）。**与 SYNC-01 同族缺陷形状**（生命周期入口存在但没人调），差别只在后果更窄：只影响关闭时的在跑作业，不影响是否触发。
- **③ 要求**：在 `main()` 关闭序列里接上（顺序须与 SYNC-01 一致：HTTP → scheduler → worker pool）；`WorkerPool.Stop()` 语义若不够（不等待/不标记），先测出来再决定改不改，别在接线单里顺手改语义。
- **④ 约束与非目标**：不改 `WorkerPool` 公开签名；不动限流/队列参数；不碰 scheduler 逻辑。
- **⑤ 验收**：接线守卫新增一条断言（`main.go` 含 `syncHandler.StopWorkerPool(`，锚定调用形态）；**破坏验证**：去掉该调用 → 守卫红，还原绿；有条件时用一次真实「跑到一半发 SIGTERM」观察作业终态。
- **⑥ 边界**：只做关闭接线 + 对应守卫；作业中断后的状态收敛另立。
- **完成记录（2026-10-14，`fb7cfe0`）**：接线落在 [main.go](../cmd/data/main.go) 关闭序列（HTTP → scheduler → worker pool，`StopScheduler` 之后新增 `syncHandler.StopWorkerPool()`）。**Stop() 语义先测后定**：新增行为测试 `pkg/sync/cancel_test.go::TestWorkerPool_StopCancelsInFlightJob` 钉住真实语义——Stop **不等待**作业跑完，而是经池 ctx 派生的 per-job ctx 取消在跑执行器；行停在 `running`，由下次启动的 `CleanupStaleRunning` 收敛（AUD-50 既有测试覆盖）。语义够用（不悬挂、可恢复），按 ④ 不改签名不改语义，只接线。守卫：`wiring_guard_test.go` 新增 `syncHandler.StopWorkerPool(` 调用断言 **+ 顺序断言**（StopWorkerPool 必须在 StopScheduler 之后）。破坏验证已做（去掉调用守卫红，还原绿）；提交树已用临时 worktree 独立编译验证。**诚实缺口**：真实「跑到一半发 SIGTERM」的活进程观测未做（本机无 docker 环境），留待有环境时补。

### SYNC-03 · 停用排程后 `next_run_at` 残留（界面会显示一个不会发生的未来） ✅ 完成（2026-10-14，commit `274dad7`）

- **① 目标**：`is_active=false` 的排程不带 `next_run_at`，界面无法把「从未会发生的下次运行」当成事实展示。
- **② 上下文**：SYNC-01 活进程实测（2026-10-14）：`POST /api/sync/schedules/1/toggle {"active":true}` 后 `next_run_at=2026-10-15T09:00:00Z`；再 toggle `false`，`scheduler/stats` 回到 `0/0`（cron entry 已 `Remove`），但 `GET /api/sync/schedules/1` 仍返回 `is_active:false` **且** `next_run_at:2026-10-15T09:00:00Z`。写点在 `pkg/sync/scheduler.go:278`（`scheduleJob` 设置并持久化），停用分支只 `cron.Remove` + `delete(s.entries)`，未清字段。愿景原则 4「每个数字可被当场质问」在这里答不上来。
- **③ 要求**：停用路径把 `next_run_at` 置 NULL（与 `is_active` 同一次 UPDATE，避免中间态）；确认 `Stats().NextRuns` 不受影响（它读 cron entry，本来就空）。
- **④ 约束与非目标**：不改 `SyncSchedule` 结构体与 JSON 字段名（`next_run_at,omitempty` 语义已够）；不改 toggle 的返回体；A-08 前端**先按「inactive 不显示 next_run」防御**，但后端修好后不改前端判据。
- **⑤ 验收**：单测：toggle false 后 store 读回 `NextRunAt == nil`；活进程复测上表同一条 curl 序列，`next_run_at` 消失；**破坏验证**：还原清除语句 → 单测红。
- **⑥ 边界**：只改停用路径的字段清理；`last_run_job_id` 等其余残留另立。
- **完成记录（2026-10-14，`274dad7`）**：修在 [scheduler.go](../pkg/sync/scheduler.go) `ToggleSchedule` 停用分支——置 `schedule.NextRunAt = nil`，由既有的**单条** `UpdateSyncSchedule` 全字段写回落为 NULL，与 `is_active=false` 同一语句、无中间态；结构体与 JSON 字段名未动。单测两条腿（`pkg/sync/scheduler_start_test.go`）：`TestToggleScheduleOffClearsNextRunAt`（含激活前置事实腿，store 读回 `NextRunAt == nil` + `Stats().NextRuns` 为空）+ `TestToggleScheduleOnStillSetsNextRunAt`（反向腿：激活路径不被清除语句误伤）。破坏验证已做（去掉清除语句单测红，还原绿）。**诚实缺口**：活进程 curl 复测未做（本机无 docker 环境），留待有环境时补。

### SYNC-04 · 调度器启动失败不可观测：`scheduler/stats` 的 `0/0` 有两种成因 ✅ 完成（2026-10-16，`task/sync-04` `c8ef50a`+`04b4b57` 已 `--no-ff` 合入 main `33f845f`；依赖 SYNC-01 ✅、SYNC-03 ✅；ui 仓 A-08 的前置已解锁）

- **① 目标**：`GET /api/sync/scheduler/stats` 能区分「调度器启动失败 / 被禁用」与「已启动但确实没有 active 排程」。现在两种情形都回 `0/0`，前端只能显示一个无法被当场质问的数字——这是 A-08 stats 块「每个数字可被质问」的前置。
- **② 上下文**（2026-10-16 源码复核，行号为当下值）：
  - **同形点**：`cmd/data/sync_handlers.go:752-756` 的 `getSchedulerStatsHandler` 无条件回 `h.scheduler.Stats()`；`pkg/sync/scheduler.go:392-400` 的 `Stats()` 只读 `len(s.entries)` 与 `len(s.cron.Entries())`。而 `Scheduler.Start()`（`pkg/sync/scheduler.go:47-58`）在 `loadAndScheduleAll` 报错时**提前返回**，`cron.Start()`（`:55`）根本没执行 → 两个计数同时为 0，与「启动成功但没有 active 排程」**完全同形**。
  - **失败原因只进日志**：`cmd/data/main.go:71-73` 拿到 `StartScheduler()` 的 error 后只 `logger.Error()` 一条，进程继续服务（这是 SYNC-01 的有意裁决：不把部分失败放大成全部失败，理由见 `cmd/data/main.go:65-70`），HTTP 面**没有任何**可查询的启动状态。
  - **现成先例（照抄形状）**：同包的 `pkg/sync/worker.go:395-400` 的 `WorkerStats` 已带 `IsRunning bool`，在 `:415` 由 `wp.ctx.Err() == nil` 计算——worker pool 的「起没起来」是可观测的，scheduler 缺的正是这一个对应字段。
  - **不变量（勿打破）**：SYNC-01 的守卫 `cmd/data/wiring_guard_test.go` 锚定 `cmd/data/main.go:71` 的 `if err := syncHandler.StartScheduler(); err != nil {` 调用形态，必须继续绿；SYNC-03 已确认 `Stats().NextRuns` 读的是 cron entry（与本单无关）。
  - **归属**：缺口在 core（`pkg/sync` + `cmd/data`）；消费方 A-08 在 `quant-trading-ui` 仓，本单不动前端。
- **③ 要求**：
  1. `pkg/sync`：给 `SchedulerStats`（`pkg/sync/scheduler.go:377-382`）**增补**启动状态——`IsRunning bool`（JSON `is_running`，对标 `WorkerStats.IsRunning`）+ `StartError string`（JSON `start_error,omitempty`）承载失败原因。既有 `active_schedules` / `total_entries` / `next_runs` **一律不改名、不改语义**，纯增量。
  2. `pkg/sync`：`Scheduler.Start()` 在 `loadAndScheduleAll` 失败时把错误记进 scheduler 自身状态（受既有 `s.mu` 保护），使 `Stats()` 能同时报出「未运行 + 原因」；成功路径置运行中并清空原因。**不改** `Start()` 签名与返回语义（仍返回 error，失败仍不启动 cron）。
  3. `cmd/data`：让启动失败经第 2 条的内部状态到达 HTTP 面。**优先方案**：`Start()` 自己记状态，`cmd/data/main.go:71-73` 一行不改（fail-soft 裁决与守卫都不动）；只有当该方案实测不可行时，才在 `SyncHandler` 增一个方法转发，且不改 `main.go` 的判据与调用形态。
  4. **文档同步纪律**（ADR-031 固化）：`/scheduler/stats` 响应 schema 变更 → 必须同步 `docs/SPEC.md` 该端点契约，并在 §三 A-08 指针处注明「stats 现带 `is_running`/`start_error`，前端可据此区分」。
- **④ 约束与非目标**：
  - **不改 blast radius**：加载失败必须仍然不杀进程（`cmd/data/main.go:65-70` 的裁决理由继续成立）；本单只加可观测性，**不加** fail-fast。
  - 不重命名、不删除既有响应字段；不新增会破坏现有解析的必填字段。
  - 不改 `wiring_guard_test.go` 已锚定的调用形态。
  - 不动 SYNC-02/03 已收口语义（关闭顺序、`next_run_at` 清理）；不动 `WorkerPool` 任何行为。
  - **不做前端**：A-08 的 UI 改动在 `quant-trading-ui` 仓，本单只把后端数字变成可质问的。
- **⑤ 验收**：
  1. 单测（`pkg/sync`）：注入一个在读取 active 排程时返回错误的 `ScheduleStore` 替身 → `Start()` 返回 error → 断言 `Stats()` 报**未运行**且 `StartError` 非空、含底层原因；对照组：store 正常返回空列表 → `Start()` 返回 nil → 断言 `Stats()` 报**运行中**且 `StartError` 为空。**核心判据：这两组的 stats 必须可区分**（现状同形）。
  2. 单测（`cmd/data`）：`GET /api/sync/scheduler/stats` 的实际 JSON 含新字段（验 handler 透传，不是只改了 struct）。
  3. **破坏验证**：删掉启动状态记录（让 `Stats()` 退回只读两个计数）→ 第 1 条的区分腿转红；还原转绿。
  4. 既有门禁全绿：`go test ./pkg/sync/...`、`go test ./cmd/data/...`（含 `wiring_guard_test.go`）、`go vet ./pkg/sync/... ./cmd/data/...`。
- **⑥ 边界**：只做 scheduler 启动状态的可观测化 + 对应 SPEC 契约同步；A-08 前端消费、worker pool 的同类观测、`Stats()` 其余字段的语义调整另立。
- **完成记录（2026-10-16，`c8ef50a`，branch `task/sync-04`，worktree `../quant-trading-worktrees/sync-04`）**：
  - **方案裁决（③.3 优先方案落地）**：`Start()` 自己把结局记进 scheduler 状态（`isRunning`/`startError`，受既有 `s.mu` 保护，无嵌套取锁路径），`cmd/data/main.go` **一行未改**——fail-soft 裁决与 `wiring_guard_test.go` 锚定的调用形态原样保绿（守卫三条全过）；handler 无需转发方法，`SchedulerStats` 经 `c.JSON` 天然透传。`Start()` 签名与返回语义未动：失败仍返回同一条 `failed to load schedules: %w` 错误、仍不启动 cron。
  - **字段（③.1，纯增量）**：`SchedulerStats` 追加 `IsRunning bool`（`is_running`，恒在，对标 `WorkerStats.IsRunning` 形状）+ `StartError string`（`start_error,omitempty`，仅失败时出现，不新增必填字段破坏现有解析）；旧三字段不改名不改语义，测试里断言「两组旧计数同形（0/0）」把这一点钉死——防止有人反过来挪用计数造区分。
  - **测试（先红后绿，⑤.1/⑤.2）**：`pkg/sync/scheduler_stats_test.go` 三条腿（失败组 vs 对照组可区分 + `start_error` 承载底层原因文本 / 重试成功后「置运行中 + 清空原因」同时发生 / 旧字段语义挪用防线）——起手编译红（`bad.IsRunning undefined`，exit 1），实现后 `go test ./pkg/sync/ -count=1` exit 0；`cmd/data/sync_handlers_stats_test.go` 两条腿走真实 `StartScheduler`+`getSchedulerStatsHandler` 的**实际 JSON**（失败体 `is_running:false`+`start_error` 含注入原因；运行体 `is_running:true` 且 `start_error` 键**缺席**，并断两组 JSON 可区分）——exit 0。
  - **破坏验证（⑤.3）**：删掉 `Stats()` 里的 `IsRunning`/`StartError` 记录（退回只读两个计数）→ pkg/sync 两条腿红（`StartError Should NOT be empty`）、cmd/data 两条腿红（`is_running` 断言红，含区分腿 `Should not be: false`）；Edit 工具还原后 `sha256sum -c` 与基线**逐字节一致**、复测全绿。
  - **门禁（⑤.4）**：`go build ./...` exit 0、`go vet ./pkg/sync/... ./cmd/data/...` exit 0、`gofmt -l` 四个改动/新增文件空输出、`go test ./pkg/sync/... -count=1` exit 0。`go test ./cmd/data/... -count=1` 全量有**1 条既有红**：`TestSyncAllFactorsHandler_ReportsSkipped`（skipped=8 want 5，原因=共享开发库 `fundamentals_detail` 无行 + `size`/`volatility`/`growth` 内核未定义）——已在**纯 HEAD（2eba0a7）临时 detached worktree** 复现同一条同文失败（临时 worktree 已删），证非本单引入，按 ODR-067 登记在案不动（**后续更新**：D5 线已在 `1489c54` 校正该测试期望，合入预演中全量 `cmd/data` 已绿，见下方自审修订 ③）。⚠️ 顺带发现一处**与本单无关的既有红**：`cmd/data/setup.go` 在 HEAD 上就不过 `gofmt`（import 分组顺序），与 AUD-27「整仓 gofmt 可当结论」的登记相悖，未修（⑥ 边界外），建议另立小单。
  - **文档同步（③.4）**：`docs/SPEC.md` §sync 端点块新增 `GET /api/sync/scheduler/stats` 行 + 响应契约注记（两种成因的判读口径）；本文件 §三 A-08 指针行已注明「stats 现带 `is_running`/`start_error`，前端可据此区分」。（`docs/openapi.yaml` 从未登记过任何 `/api/sync/*` 路径——机器可读契约缺口是本单之前的既有状态，工单只要求 SPEC，未扩；DATA-02 自审先例提示这里迟早要补，另立为宜。）
  - **诚实缺口**：活进程 curl 三态复测未做——本单代码在未合入的 `task/sync-04` 分支上，容器由主工作树构建，合入后需 `docker compose build data-service && up -d data-service` 再按「psql 读数 → curl 网关」链补一次实测（SYNC-01/02/03 同款缺口）。`-race` 本机不可跑（无 gcc，CGO_ENABLED=0）。
  - **合入方式**：主工作树当时有并行 agent 未提交改动（factor/market_activity 系），本单全程在独立 worktree 完成；`c8ef50a` 未 push、未合入 main，合并与台账提交的时机归主任。
  - **自审修订（2026-10-16，审 `c8ef50a` 后追加，不改写上面原始记录）**：
    1. **`is_running` 当时不是活性读数（已修，commit `04b4b57`）**：工单②明说照抄 `WorkerStats.IsRunning` 的形状，而那是 `wp.ctx.Err()==nil` 的活性语义；首版只记最近一次 `Start()` 的结局，`Stop()` 之后仍会报运行中——字段名骗读它的人。修法：`Stop()` 末尾在 `s.mu` 下翻转 `isRunning`（`startError` 不动：正常停止不是启动失败）。先红链：新增 `TestStopClearsIsRunning` 起手红（`Should be false`，exit 1）→ 实现 → `pkg/sync` 全量绿；破坏验证：摘掉翻转**仅该腿红**、其余全绿，还原 `sha256sum -c` 逐字节一致后复测绿。
    2. **SPEC 判读表缺第三形态（随 `04b4b57` 补）**：首版只写「启动失败 vs 无排程」两种成因；`is_running=false` 且 `start_error` 缺席 = 未启动或已停止，也需要判读口径（正常服务走不到这格，启动接线由 wiring guard 钉住）。SPEC 注记现给全三态。
    3. **上面「门禁」那条登记的环境红已过期（已加指针）**：`TestSyncAllFactorsHandler_ReportsSkipped` 由 D5 线在 `1489c54` 校正测试期望；合入预演实测全量 `cmd/data` 绿，它不再是登记红。
    4. **合入预演（新证据）**：临时 worktree 里把 `task/sync-04`（两 commit）合入 main `3de5ef6` —— 唯一重叠文件 `docs/SPEC.md` ort 自动合并零冲突；合并态上 `go build`/`go vet`/`go test ./pkg/sync/... ./cmd/data/... -count=1` **全部 exit 0**（临时 worktree 与预演分支已删）。分支本身未动：`c8ef50a` 已被台账引用，不重写历史，待主任合并。
    5. **合入执行 + 活进程实测闭环（2026-10-16，随「合并吧」指令）**：`task/sync-04` 已 `--no-ff` 合入 main（merge commit `33f845f`，`docs/SPEC.md` ort 自动合并零冲突），合并态复跑 `go build`/`go vet`/`go test ./pkg/sync/... ./cmd/data/... -count=1` 全部 exit 0；`docker compose build data-service && up -d data-service` 后三方对照 —— 重建**前** `:8081` 与网关 `:8085` 均回旧形状 `{"active_schedules":0,"total_entries":0}`，重建**后**两处同回 `{"active_schedules":0,"total_entries":0,"is_running":true}`（**analysis 网关零改动即透传**，流式反代 map 解码再编码），启动日志三行坐实真实形态「启动成功 + 0 个 active 排程」⇒ 该格 `0/0` 现在可当场质问。上面「诚实缺口」与「合入方式」两条的待办就此关闭；「启动失败带 `start_error`」那一格活进程无法安全复现（需弄坏共享库的读路径，写操作授权纪律），由单元 + HTTP 层测试腿覆盖，如实注明。分支 worktree 与已合并分支收尾同批清掉。
  - **台账归属留痕**：本完成记录写入主工作树时挂在并发 agent 的未提交改动上，随后被 D5 线的提交（`fa53ca2`/`1489c54` 系）一并携带入库——内容完整无损，按主任 2026-10-14 裁决接受该形状，仅在此注明归属。

### DATA-02 · 票池状态拆分没有只读通道（幸存者偏差块只能显示空态） ✅ 完成（2026-10-14）

- **① 目标**：SPA 能读到「在市 / 退市」的拆分读数，于是 ui 的幸存者偏差块不必再挂"缺读数通道"，也**不需要在前端自算**。
- **② 上下文**（2026-10-14 psql + curl 当场复核，数字是当下的不是引用的）：
  - 数据侧是干净的：`stocks` 共 **5908** 行，`status=active` 5569 行且 `delist_date` 非空 **0** 条，`status=delisted` 339 行且 `delist_date` 非空 **339** 条，"有摘牌日却仍标 active"的不一致行 **0**。
  - 存储与领域层也已就位：`pkg/storage/stocks.go:15` 的 `stockColumns` 含 `delist_date`，`pkg/domain/market/types.go:49` 用 `*time.Time` 区分"未知"与"仍在市"（注释就写明"没有这个字段就不可能修幸存者偏差"）。
  - **缺的只有 HTTP 面**：SPA 能走到的票池读数只有 `GET /api/stocks/count`，而它返回 `{"count":5908}`——这 5908 **含已退市**，把叫"在市股票数"就是假数字（`cmd/data/handlers_stocks.go:84-104`）。带 `status`/`delist_date` 的 `GET /stocks` 列表在数据服务上有（`cmd/data/main.go:113`），但 analysis 网关没有转发它（`cmd/analysis/handlers_proxy.go:101` 只挂了 count 一条）。
  - ui 侧已按"显式空态 + 指名本工单"落地（`quant-trading-ui` 的 `components/sync/DataQualityPanel.vue`，A-08 断言 6），**没有**造数也没有算比例。
- **③ 要求**：给一条只读拆分读数。**形状由 core 定**（在既有 count 端点上加分组字段，或转发既有 `/stocks` 列表，或新挂一条只读聚合——三种都行，选哪种不算前端的意见）；只要求该读数与 `select status, count(*) … group by status` 逐值一致。
- **④ 约束与非目标**：不改 `stocks` 表结构、不动 `pkg/domain` 契约；不做摄取（本单是**通道**缺口，不是数据缺口）；不让前端自己 `5908 - 339` 之类的客户端重建（同一概念两处真相，AGENTS.md §9 明令禁止）。
- **⑤ 验收**：`curl` 经网关（:8085）取到拆分，值与 psql 直查一致（active 5569 / delisted 339）；`docs/SPEC.md` 登记该端点；ui 侧把空态换成真数另计一小单（不夹在本单）。**破坏验证**：临时把某只股票的 `delist_date` 置空 → 拆分读数随之变化（证明它是真查不是硬编码），还原后再测一次一致。
- **⑥ 边界**：只加只读通道；摄取、回填、前端展示均不在本单。
- **完成记录（2026-10-14）**：
  - **⚠️ 工单 ⑤ 的破坏验证腿与 ③ 的口径互相矛盾（前提证伪，按 ③ 落地）**：⑤ 写「临时把某只股票的 `delist_date` 置空 → 拆分读数随之变化」，而 ③ 要求读数逐值等于 `group by status`。**按 status 分组的读数对 `delist_date` 结构上不敏感**，这条腿永远不动。2026-10-14 活进程实测坐实：置空 `000003.SZ` 的 `delist_date` 后，网关读数原样 `{"active":5569,"delisted":339}`。要满足 ⑤ 那条腿，就得把口径改成按 `delist_date` 分组 —— 那就不是 ③ 要的数。∴ 破坏验证改用**改判 `status`** 这条腿（同样证明「真查非硬编码」，且与 ③ 同口径），`delist_date` 腿保留为反证记录。
  - **形状裁决（③「形状由 core 定」）**：选**在既有 `GET /stocks/count` 上加分组字段**，不转发 `/stocks` 列表、不新挂端点。理由：网关 `proxyRequest` 是 map 解码再编码，**新字段天然透传，网关零改动**；转发列表会把 5908 行推到浏览器再聚合 = 正是 ④ 禁止的客户端重建；新端点则多一条面而 SPA 本来就在读 count。响应 `{"count":<总行数>,"by_status":{"active":5569,"delisted":339}}`。
  - **落点**：`pkg/storage/stocks.go` 新增 `StockStatusCounts`（单条 GROUP BY，NULL/空串折 `"unknown"`，空表返空 map 而非 nil）；`cmd/data/handlers_stocks.go` 新增 `stockCountPayload` 并由它组装响应。**`count` 由各档求和导出**，不再是第二次 `COUNT(*)` —— 同一趟查询、同一个真相，顺带消掉「两次查询之间被并发同步改动，于是各档加起来不等于总数」的窗口。`latest_date` 那行按 ⑥ 不动（属 DATA-03）。
  - **SQL 写法陷阱（已钉）**：分组表达式若写成 `GROUP BY status`（Postgres 在 GROUP BY 里**优先解析为输入列**，不是输出别名），NULL 行与 `''` 行会出成**两行都叫 unknown** 的分组，Go 侧 map 赋值后者覆盖前者 ⇒ 读数**静默少一只票**。现按 `GROUP BY COALESCE(NULLIF(status,''),'unknown')` 显式重复表达式。破坏验证：把它改回 `GROUP BY status` → `TestStockStatusCounts_FoldsNullAndEmptyIntoOneUnknownBucket` 红（`expected: 2, actual: 1`），还原转绿。
  - **测试（三层，先红后绿）**：`pkg/storage/stocks_status_test.go` 三条腿（逐值 vs WHERE 独立路径 / NULL+空串折同一档 / 改判状态两档各移动 1）—— 起手 `store.StockStatusCounts undefined` 红，实现后 exit 0；`cmd/data/handlers_stocks_test.go` 两条腿（`stockCountPayload` 纯形状：空表/nil 序列化 `{}` 不是 `null`；HTTP 面逐值 == 库里 GROUP BY 且 `count == Σby_status == COUNT(*)`）—— 起手 `undefined: stockCountPayload` 红，实现后 exit 0。测试内插的探针行用 `t.Cleanup` 删除且断言取**增量**（开发库随时被并发同步改动，绝对值断言会变偶发红）；跑完实测残留 0 行。
  - **活进程实测**：`docker compose build data-service && up -d data-service` 后 —— psql 直查 `active|5569 / delisted|339`；`:8081/stocks/count` → `{"by_status":{"active":5569,"delisted":339},"count":5908}`；**经网关 `:8085/api/stocks/count` → 同一 body，HTTP 200**（逐值一致）。破坏验证（活进程）：`UPDATE status delisted→active` 一行 → 网关读数变 `{"active":5570,"delisted":338}`、`count` 仍 5908 → 还原 → 复测 5569/339。
  - **门禁**：`go build ./...` exit 0、`go vet ./cmd/data ./pkg/storage` exit 0、`gofmt -l` 四个改动文件空输出、`go test ./pkg/storage/ -count=1` exit 0。`go test ./cmd/data/` 有 **1 条非本单引入的红**：`TestSyncAllFactorsHandler_ReportsSkipped`（want 5 got 8）—— 加测试前后基线一致（`/tmp/base_cd.txt` 留档），属并行 agent 在途的因子改动，按 ODR-067 登记在案不动。诚实边界：`go test -race` 本机跑不了（无 gcc，CGO_ENABLED=0）。
  - **⚠️ 本单执行中的一次数据事故（当场恢复，如实登记）**：做上面那条 `delist_date` 反证腿时，我的**还原**语句漏了 `WHERE`（`UPDATE stocks SET delist_date=(子查询)` 无行限定），把**全表 5908 行 `delist_date` 清成 NULL**（原本 339 非空）。`status` 未受影响，但坏的是 P2-4 那条线 —— `GetListingWindows`(`pkg/storage/stocks.go:202`) 读 `delist_date` 做按天池子过滤，全 NULL 等于「没有一只票退过市」，幸存者偏差会被重新引入。恢复路径正是 ADR-023 的「可重建数据靠同一份数据 + `content_hash`」：从 `ingest.raw` 的 `tushare.stock_basic` 16 页原始响应回填 —— 回填前只读核对：归档带 `delist_date` 的 symbol **339** 个、归档有而 stocks 无 **0**、stocks 标 delisted 而归档无 **0**、同一 symbol 跨页取值冲突 **0**，且我记录过改前值的 `000003.SZ`（表内 `2002-06-14`）与归档 `20020614` 逐字一致。回填 `UPDATE 339`（只写 `delist_date` 一列，不动 `status`/`updated_at`），复测九项全绿：非空 339 / active 带日期 0 / delisted 缺日期 0 / `000003.SZ = delisted|2002-06-14` / 总数 5908 / 拆分 5569-339 / 日期跨度 1999-07-12..2026-07-30 / 30 分钟内 `updated_at` 被改的行 0 / 探针残留 0。**教训**：破坏验证的还原腿必须与写腿带**同一个行限定**；无 `WHERE` 的 UPDATE 不该出现在验证脚本里，改用 `UPDATE … WHERE symbol=$1` 的形。
  - **顺带纠正一处登记**（不改写他人结论，只加指针）：本文件 D3 完成记录「已知边界 ③」称「补全需发明落库 API（`pkg/storage` 无 `SaveIndexConstituents`）」—— 不成立，`(*PostgresStore).SaveIndexConstituentBatch` 在 `pkg/storage/cache.go:369` 已存在且正被 `pkg/data/tushare.go:690` 调用；那次多传的参数已于 `abd9067` 按源码证伪撤销，`cmd/data` 编译恢复。真正的缺口是落库失败只 Warn（见新登记 **DATA-06**）。
  - **留给下游**：ui 侧把「缺读数通道」空态换成真数（`quant-trading-ui` 的 `components/sync/DataQualityPanel.vue`，A-08 断言 6）按 ⑤ 另计一小单，不夹在本单。
  - **自审修订（2026-10-15，审自己的 commit 后追加，不改写上面原始记录）**：三处真问题，均已修并复验。
    1. **契约漏登记**：`docs/openapi.yaml:2206` 的 `/api/stocks/count` 原来只有裸 `type: object`，SPEC 写了形状而机器可读契约没写。已补 schema（`required: [count, by_status]` + `additionalProperties` 整数 + `latest_date` 明写**可能缺席**并指回 DATA-03，避免又造一个"声明了却不返回"）。该文件由 analysis-service `go:embed` 后经 `GET /api/openapi.yaml` 对外供给，改完用 YAML 解析复验属性齐备。
    2. **两条腿在空库下行为不对**：`pkg/storage` 的交叉核对在空表时是**零次循环空过**（谁都没断言的绿，比红更糟），`cmd/data` 的同一条则 `require.NotEmpty` 直接**假红**（空开发库是环境不是缺陷）。两条都改成 skip，并按 AUD-51 口径注明前置与断言读的是同一张 `stocks`。
    3. **我自己的测试注释在夸大**：原写「这条同时钉住 GROUP BY 陷阱」。实测证伪 —— 把生产分组改回原始列 `GROUP BY status` 后**这条照样绿**（库里当前没有 NULL/空串行，折与不折同结果）。守那条陷阱的是 `…_FoldsNullAndEmptyIntoOneUnknownBucket`（它自己造那两行，改坏时红在 `expected: 2, actual: 1`）。注释已按实测改成写清分工，护栏不靠叙述自证。
    - 顺带把 oracle 从 N+1 条 WHERE 收成**单趟** `GROUP BY status` + Go 侧再折叠一次（两条独立实现照样互核，但读数窗口从 5 趟降到 2 趟）—— 开发与测试会话都在写 `stocks`，每多一趟读就多一个「两次读数之间被人改过」的假红窗口。
    - 复验：`go build ./...` exit 0、`go vet ./cmd/data ./pkg/storage` exit 0、`gofmt -l` 四个文件空输出、五条腿全绿（先红后绿链未变）；改坏的 SQL 已还原，`git diff` 生产码为空、`grep -rn MUTATION cmd pkg` 零命中。

### DATA-03 · 两条读数形状不一致（`latest_date` 静默缺席 / PIT 历史端点没通道） ⬜ 待做

- **① 目标**：**声明了就该返回，返回不了就别声明**——把两条端点的契约与实测对齐，PIT 三时刻要么有通道要么明写没有。
- **② 上下文**（2026-10-14 实测）：
  - `GET :8081/stocks/count` 实测 body 只有 `{"count":5908}`，**没有** `latest_date`。代码里那条查询在 `cmd/data/handlers_stocks.go:95-101`：`store.DB().QueryRow(ctx, "SELECT MAX(trade_date) FROM ohlcv_daily_qfq").Scan(&latestDate)`，**返回的 error 被整个丢掉**（没接收），扫不上去就 `latestDate == nil`，于是字段静默缺席——失败不响，界面只是少一个数。旁证：同一请求容器日志 `latency` 只有 14–52 ms，而 psql 直查该 `MAX` 冷启 **2.3 s**（`Index Only Scan Backward using idx_ohlcv_trade_date`），说明不是"查了但查得慢/查回空"，而是**这一趟就没成**。
  - 前端类型 `web/src/types/api.ts:128` 把 `latest_date` 声明为必填 `string`，实测是 `undefined`（当前 SPA 无消费点，`grep latest_date` 在 `web/src` 零命中 ⇒ 还没人踩，但契约已经在说谎）。
  - PIT 三时刻（财务期末 / 公告披露 / 因子可交易起始）本该来自 `GET /fundamentals/:symbol/history`：该路由在数据服务上存在（`cmd/data/main.go`），**网关没有转发**（实测 `GET :8085/api/fundamentals/000001.SZ/history` → **404**），而直连 :8081 是 200 但 body `{"history":null}`；上游是 `fundamentals_detail` 表**实测 0 行**（摄取链路未通 = 已知项 EQD-P1-2，不在本单）。
- **③ 要求**：两处各自二选一并写明理由——(a1) 修 `latest_date`：接住 Scan 的 error，失败就**别静默**（要么日志要么显式 null），并让响应与 SPEC 一致；(a2) 或删掉这条查询与该字段，SPEC/前端类型同步去掉。(b1) 把 `/fundamentals/:symbol/history` 经网关暴露给 SPA；或 (b2) 明写它不对 SPA 开放，PIT 读数改走别的面。**注**：选 (b1) 也不等于 PIT 能用，EQD-P1-2 仍是数据前提——通道与数据是两件事，别混着结案。
- **④ 约束与非目标**：不动 `ResultSpec` 与 `pkg/domain`；不改 `fundamentals_detail` 的摄取（那是 EQD-P1-2）；不在前端用兜底值把缺失"补齐"。
- **⑤ 验收**：`curl` 的 body 字段集合与 `docs/SPEC.md` 对该端点的描述**逐条一致**（多一个少一个都算未完成）；前端 `StockCount` 类型与实测一致（不再出现"声明 string 实为 undefined"）；若选 (b1)，加一条路由级断言（照 `cmd/analysis/deps_test.go` 那份路由登记表的做法）。**破坏验证**：把 Scan 错误重新丢弃 → 新加的那条"字段缺失必须可观测"的断言变红，还原转绿。
- **⑥ 边界**：只处理这两条读数的形状与通道；数据摄取（EQD-P1-2）与票池拆分（DATA-02）各自成单。

### DATA-04 · 成交量单位统一（手 → 股）+ D1 导出路径修复 ✅ 完成（2026-10-14）

- **① 目标**：让 `ohlcv_daily_qfq.volume` 的单位符合规范层 schema **已经声明**的「股」，并打通被 D1 打断的 NT catalog 导出路径（D7 登记的「已知阻断」）。
- **② 上下文**（2026-10-14 实测，非引用）：
  - 规范层契约写的**就是**「股」：`docs/SPEC.md` 规范层 Schema 的 OHLCV 字段表 `Volume | float64 | volume | 成交量（股；聚合 bar = 期间合计）`；`pkg/data/source/*_adapter.go` 的 schema 同样写 `Unit: "share"`；前端 `web/src/utils/format.ts` 的 `fmtVolume` 也按股格式化（`fmtVolume(200000000) === '2.0亿'`）。
  - 但**实现**存的是「手」：`pkg/data/tushare.go` 的 `normalizeDailyOHLCV` 把 tushare `daily` 的 `vol`（单位手）原样写库；真库 392.6 万行里 317.1 万行（80.8%）是带两位小数的「手」——**实现违背了已写明的契约**。
  - 后果链：`as_quantity` 要求整数 → 小数手 ⇒ fail-loud ⇒ NT 导出整条断（D7 记于 docs 仓 `nt-sidecar-operations.md` §4.4）；同时 `pkg/backtest/tracker` 的流动性阈值 `volume × 0.1` 与订单量（股）**量纲错配**，此前偏小 100 倍。
- **③ 要求**：口径定死为「股」并一次改干净——同步侧换算 / 存量迁移 / 导出路径修复 / 契约描述对齐；不发明第二种口径。
- **④ 约束与非目标**：不改 `as_quantity` 的整数纪律（它是对的，是上游给错了）；不改四契约签名；不猜未实测的数据源单位。
- **⑤ 验收**：非整数行归零 + 与备份逐行比对 0 mismatch + 幂等重跑 no-op + 导出真跑值一致 + 对拍回归绿 + 破坏验证三组。
- **⑥ 边界**：只做日线 `ohlcv_daily_qfq`（唯一有数据的表）；`turnover`（成交额）是否统一另立；分钟/实时表 0 行不在本单。

- **完成记录（2026-10-14）**：
  - **同步侧**：`pkg/data/tushare.go` 的 `normalizeDailyOHLCV` 按 `sharesPerLot`（一手 = 100 股）换算 + `math.Round`。取整是必须的——真库 **37.3 万行**的 `volume×100` 在 float64 下不是精确整数（最大误差 4.77e-07），不取整会被 `as_quantity` 的 `is_integer()` 判成损坏。
  - **存量迁移**：`pkg/storage/postgres.go` 的 `migrationStatements()` 加 migration 052（文档副本 `docs/migrations/052_ohlcv_volume_lots_to_shares.sql`）。×100 **不**幂等，故用**哨兵 + 内容**双守卫：① `data_migrations` 表（本项目首个 DML 迁移标记设施，后续同类迁移复用）记录 052 已执行 ⇒ 此后至多生效一次；② 库里还有非整数（= 手的小数）才值得整表换算（防「清库后用新代码重灌」）。**不**写逐行 `WHERE volume <> floor(volume)`——真库 **19.24%** 的行本来就是整数「手」，逐行条件会漏掉它们 ⇒ 半股半手的混合表。真库执行：392.6 万行全表换算（6m20s），结果非整数行 **0**、与备份逐行比对 **0 mismatch**、幂等重跑 no-op。
    - **为什么必须加哨兵（审查时发现的设计缺口）**：只靠内容守卫时，其隐含假设是「非整数 ⇒ 仍是手」。迁移完成后若又落入手数据（旧二进制未换 / 手工导入 / 多节点混部），内容守卫会再次成立 ⇒ **整表**再 ×100 ⇒ 已迁移的股数据**静默**变成 ×10000。对照实验（临时表实跑）：初值 `{1.5, 2000, 3.7}` 首次迁移得 `{150, 200000, 370}`；再落一行手值 `9.9` 后跑无哨兵版得 `{990, 15000, 37000, 20000000}`，加哨兵版保持 `{9.9, 150, 370, 200000}`。
  - **导出路径**：`nt-sidecar/tools/export_bars.py` 的 SELECT 改输出规范层列名 `trade_date AS date`（改 SELECT 而非映射表：`date` 是跨语言契约定下的 json tag）；`mapping.py` 的 `as_quantity` 注释更新（单位属规范层，本层只执行整数纪律）。
  - **契约描述**：`docs/SPEC.md` 那句与字段表自相矛盾的「volume 手 → 原样整数（不造股数语义）」改为「取规范层的股」。
  - **实测证据**：真库导出重跑通过（catalog 值 = 库值逐条一致，全整数；旧 catalog `1158366`〔手，截断〕→ 新 `115836645`〔股〕）；`go run ./cmd/dualrun -strategy bh -gate` 输出「对齐门禁通过：全部指标在 K15 阈值内，确定性腿一致，无成交 mismatch」——**D7 登记的导出阻断解除，对拍恢复全绿**。
  - **破坏验证**（三组，均改坏变红、还原变绿、sha256 与基线一致）：① 删 `date` 别名 → 导出 KeyError；② 去 `×sharesPerLot` → 单测红；③ 去 `math.Round` → 单测红。**过程中发现并修正一个假护栏**：③ 首版测试值恰好 ×100 精确、改坏不红；改用真库误差榜前二的值并让用例**自证**（值若恰好精确则测试自身报错），重跑才有效。
  - **对国内线的影响**：volume 整体 ×100，其中两处是**修正既有量纲错位**（`tracker` 流动性阈值偏小 100 倍；`execution` 的 impact 滑点偏高 100 倍，默认 `SlippageModel="fixed"` 故默认不生效）。对拍两内核同源同变，对齐仍在 K15 阈值内。
  - **备份留存**：迁移前的 volume 列留存于 `ohlcv_daily_qfq_bak_052`（最小恢复集，可 `DROP TABLE` 移除；换算可逆：股 ÷ 100 = 手）。
  - **未决**：同类口径欠账全部转入 **DATA-05**（见下）。
  - **审查修复（同日，收口前自查）**：① migration 守卫补哨兵（见上）；② `docs/design/kernel/kernel-pluggable-nt-ready.md` §8.2 映射表里的「volume 表内原值取整（tushare 手数口径）直传」与本单口径矛盾 → 更正为「表内原值（**股**）直传」；③ `nt-sidecar/sidecar/native.py` 的 `DEFAULT_TRADE_SIZE` 注释「默认每笔下单**手数**（股）」措辞自相矛盾 → 「默认每笔下单**数量**（股）」；④ `pkg/domain/market/types.go` 的 `Volume` 补单位注释——本单根因正是「没有一处写明这个字段的单位」。

### DATA-05 · 成交量/成交额口径收尾（fallback 源 + 成交额 + 空表） ✅ 完成（2026-10-15，branch `task/data-05-units`：`d76b247` / `424d997` / `04d5ffe`）

- **① 目标**：把 DATA-04 未覆盖的**同类口径欠账**一次清掉——其余数据源与其余表的 volume/turnover 单位全部明确，并统一到规范层已声明的口径。
- **② 上下文**（2026-10-14 DATA-04 审查时登记，均为未验证项）：
  - **fallback 源未验**：日线链是 `tushare → mootdx → eastmoney`（`cmd/data/registry_init.go` 的 `DataTypeOHLCDaily`）。tushare 已在 DATA-04 修好（×`sharesPerLot`）；`mootdx` / `eastmoney` 的 vol 单位**在项目内无实测依据**（TDX 惯例是「手」），DATA-04 **刻意未猜着改**。而 `pkg/data/source/mootdx_adapter.go` 的 schema 已写 `Unit: "share"` —— 又一处「声明 ≠ 实现」的候选（DATA-04 的根因同型）。
  - **成交额 `turnover` 仍错位**：`docs/SPEC.md` 规范层声明 Turnover 单位 **CNY**，而 tushare `daily` 的 `amount` 单位是**千元**，`normalizeDailyOHLCV` 原样入库（`pkg/data/tushare.go`）。前端 `fmtAmount`（`web/src/utils/format.ts`）按元格式化 ⇒ 显示值偏小 1000 倍。
  - **三张表 0 行、单位未定**：`ohlcv_minute.volume`(bigint) / `realtime_quote.volume`(bigint) / `global_ohlcv.volume`(float8) 均无数据。`docs/SPEC.md` 规范层 Schema 对 OHLCV 通用声明了「股」，但分钟线那句「原样返回（volume 由表内 BIGINT 承载）」没点明单位，留给读者猜。
- **③ 要求**：每条**先实测再改**，不许猜——mootdx/eastmoney 取一次真实响应核对 vol 量级（与同 symbol 日线对比）；`turnover` 定死单位后统一「同步侧 + SPEC + 前端格式化」三处；三张空表的单位在 SPEC 规范层 Schema 里写明（哪怕暂时 0 行）。
- **④ 约束与非目标**：不重复 DATA-04 已完成的日线 volume 部分；不引入第二套口径；`turnover` 变更会动因子与展示数值，须照 DATA-04 做「改坏→红→还原→绿」破坏验证。
- **⑤ 验收**：每个源/表给出「实测单位 + 换算点 + 落库后抽样比对」三件证据；SPEC 规范层 Schema 的每个数值字段都能回答「单位是什么」；`cmd/dualrun -gate` 回归绿。
- **⑥ 边界**：只做单位口径；源本身的接入与可靠性问题不在本单。

- **完成记录（2026-10-15）**：
  - **先证伪工单前提 —— ② 的三条里有两条与事实不符，按事实做**：
    1. **fallback 源「取一次真实响应核对」没有可实测的对象**（三条独立证据，逐行核对）：
       `cmd/data/registry_init.go` 以 **nil transport** 注册 mootdx ⇒ `Enabled()=false`
       ⇒ `Registry.Fetch` 跳过它（go.mod 里也没有 go-mootdx SDK）；eastmoney 只声明
       `capital_flow`，`Fetch(ohlcv_daily)` 直接 `ErrUnsupported`；`ETLPipeline`
       （`Registry.Fetch` 的唯一生产调用方）**零调用方**（见新登记 **DATA-08**）。
       ⇒ 日线 fallback 链今天整条不可达，**不存在**「mootdx 的 vol 到底是手还是股」
       这个待答问题。据 ④「不猜未实测的源单位」：**不猜着改换算**。
       真正要收的是那处**假声明** —— mootdx 的 schema 写着 `volume Unit:"share"` /
       `amount Unit:"yuan"`，而 `fetchBars` 把 transport 原值直接塞进 DataItem，
       同步边界一个换算点都没有（DATA-04 根因同型）。现在 `Unit` 如实留空（"" = 未声明），
       并写明接线顺序：实测 → 同步边界换算到规范层 → 才补声明；
       `declarations.go` 待办边界与 `registry_init.go` 那条「eastmoney 是日线第三级」
       的设计注释同步订正（后者今天不成立）。守卫落在
       `cmd/data/ohlc_units_guard_test.go`：已启用的 A 股行情源必须声明 volume/amount/
       turnover 的单位 —— 今天够不着 mootdx，接线那一刻自动开始管它，规则的牙由替身
       用例当场证明（不声明必红 / 声明齐全放行 / 未启用不在射程）。
    2. **「前端 fmtAmount 显示值偏小 1000 倍」不成立**：成交额那条显示面今天根本没
       接到 turnover —— `/api/market/index` 的响应结构里没有 `amount`/`volume`
       （`cmd/data/handlers_stocks.go:130-136` 的 `indexData` 只有
       code/name/close/change/pct），且它查的三个指数代码在 `ohlcv_daily_qfq` 里
       **0 行**（实测；表内 5671 个 symbol 无指数），实测 curl 返回 `{"indices":[]}`。
       ⇒ `fmtAmount(undefined)` 走 falsy 分支恒显示 '-'。「同步侧 + SPEC + 前端格式化」
       三处里的**前端那一处不需要改**：`fmtAmount` 把入参按元解读，与规范层 CNY 一致。
       界面这两个格子恒空是另一条缺陷，另立 **DATA-07**。
    3. **turnover 是千元，实测而非引用文档**：真库 392.6 万行全量
       `turnover / (volume_股 × close)` 中位 **0.0010127**、p01 0.000967、p99 0.001955、
       max 0.003535 ⇒ 库里是「元」的千分之一。单行核对：600519.SH 2026-09-24
       close=1237、volume=3123935 股、turnover=3867310.92（千元）→ ×1000 = 38.67 亿元
       ≈ vwap × 股数。
  - **换算点（⑤ 第二件）**：`pkg/data/tushare.go` 的 `cnyPerAmountUnit`（在
    `normalizeDailyOHLCV` 里，与 052 的 `sharesPerLot` 同一行族）。全仓生产写点
    **只此一处**（grep `Turnover:` 实测：另一处是 `pkg/marketdata/aggregate.go` 的
    期间合计求和，单位无关）。**不取整**，与 volume 相反并有实测依据：×1000 后
    **199,240 行**偏离整数元 >1e-3（其中 5,015 行正好 0.5 元）—— 那是源侧 amount
    带 4 位小数、元以下的真实精度，只有 61,699 行属于 <1e-6 的浮点噪声；而下游没有
    整数纪律 —— NT 导出的 SELECT 根本不带 turnover（`nt-sidecar/tools/export_bars.py:63`）。
  - **影响面实测（自审时补的核对，别只写「会影响因子」这种话）**：全仓生产代码里
    **没有**拿 turnover 与绝对金额阈值比较的地方（`1e7`/`10_000_000`/`MinTurnover` grep
    零命中）；`pkg/backtest/tracker/tracker.go:347` 的流动性阈值与 `exec_algo` 的成本模型
    读的都是 **volume** 不是 turnover（grep `pkg/backtest/costexec` 对 turnover 零命中）。
    真正随 ×1000 变的只有：公式/表达式引擎的 `turnover` 字段输入、`/api` 响应数值、
    分钟聚合的期间合计。派生侧无需重算：`factor_cache` 实测 0 行。
  - **落库后抽样比对（⑤ 第三件）**：`ohlcv_daily_qfq` 3,926,510 行 与备份
    `ohlcv_daily_qfq_bak_054` 3,926,510 行逐行比对 **0 mismatch**
    （`abs(turnover − bak×1000) > 1e-6` 计数为 0）；**LEFT JOIN 方向也核了**：
    活表里没有一行在备份中缺 counterpart（`b.symbol is null` 计数 0），
    `turnover IS NULL` 与 `turnover = 0` 均 **0 行** —— 所以「整表 ×1000」这条路
    没有「把 NULL 折没」「漏掉零值行」这类暗礁可走；千元态残留（比值 < 0.01）**0 行**；
    比值中位数从 0.0010127 变到 **1.0127**。派生侧无陈旧值：`factor_cache` 0 行，
    公式型因子注册表里没有引用 `turnover` 的公式（实测 grep；同名的
    `inventory_turnover_delta` 是财报比值，与行情单位无关）。
  - **迁移执行方式的意外（如实记录，不当没发生）**：跑 `go test ./pkg/data/...` 时，
    `pkg/data/tushare_adjustment_integration_test.go` 经 `NewPostgresStore → migrate()`
    把 054 **真的作用在共享开发库上**（2026-10-15 约 11:08，无人授权、无提示）。
    结果与预期一致且可回滚（÷1000 + 删 054 哨兵行；备份整列在 `…_bak_054`）。
    **留下的实际风险不是数据坏了，而是「容器还是旧代码、库已是元」**：旧二进制此后
    任何一次真实日线同步都会写入千元 ⇒ 混合表，而哨兵已置位、重启不会补跑。
    实测 `sync_schedules` 里 `is_active` 为 **0 条**（今天没有自动同步在跑），
    最新交易日仍是 2026-09-24。处置：部署 `task/data-05-units` 后再触发同步；
    在此之前不要跑日线全量同步。根因（测试改共享库）另立 **DATA-09**。
  - **⑤ 未收口的两条（不假装绿）**：
    · `cmd/dualrun -gate` **没跑** —— NT sidecar `:9100` 未起（实测
      `dial tcp 127.0.0.1:9100 … refused`），起它要动共享栈，留给主任决定。
      口径上 turnover 不在 NT 导出的 SELECT 里、两内核读的是同一份 volume，
      本次改动按理不影响对拍 —— 但这是推理，不是跑绿的证据。
    · 「每个源都给实测」对 mootdx / eastmoney 是**不适用**（源未接线），不是失败。
  - **破坏验证（四腿，均「改坏→红→还原→绿」，还原后 sha256 与基线逐字节一致）**：
    ① 去掉 `× cnyPerAmountUnit` ⇒ `TestNormalizeDailyOHLCV_*` 三条断言红
    （含自证的「仍是源侧原值」反证腿）；② 去掉哨兵条件 ⇒
    `TestMigration054_SentinelBlocksStrayLotsRows` 红（10000 → 1e7 的静默放大重现）；
    ③ 把准入阈值 0.01 调大到 1.0 ⇒ `TestMigration054_GoalpostsThreshold` 红
    （贴着实测元态下界的种子被误换算）—— 按「护栏输入别从阈值推导」，
    跑的是调大阈值而不是删守卫；④ 掏空单位守卫的条件 ⇒ 替身用例红。
    过程中本单自己的脚手架被抓住一次：元态种子首版误填 `turnover=322.6`
    （比值其实是 0.0003226，仍是千元态形状），改坏腿当场红，改完才对着实测边界算。
  - **提交后自审（七处，均已改）**：
    ① `normalizeDailyOHLCV` 的文档把 `amount` 称作「原始成交量」—— 成交额不是成交量，
    且本单刚给它加了单位换算，最容易误导；订正为「不复权的原始量值（vol 是成交量、
    amount 是成交额）」。② 「不取整」的依据原本只写了 `max|x−round(x)| = 0.5`，
    升级为分布计数（199,240 行真实 sub-元 vs 61,699 行浮点噪声）—— 原来的说法没错，
    但撑不住「为什么不顺手 round 掉噪声」这个质问。③ 单位守卫的违规清单里混进了一条
    非单位项（`类型: schema错误`）而报错文案只说「字段没声明单位」；统一成
    `源名/类型.字段` 形状、文案涵盖两种，并补第四条腿（声称服务行情却拿不出字段表
    也被抓），免得那条分支空转。④ 影响面从推理改成核对：grep 确认生产代码没有
    拿 turnover 比绝对阈值的判据（见上条）。⑤ 注释指向一个**不存在的测试名**
    （`TestProductionRegistryOHLCUnitsDeclared`）—— 幻影指针会把下一个读者送到死路上，
    改成真名 `TestProductionRegistryMarketUnitsDeclared`；`declarations.go` 里
    「DATA-05（2026-10-14）核对」的日期口径一并订正（登记是 10-14，核对是 10-15）。
    ⑥ **撤掉一条从 052 借来的不成立论证**：形状测试原本断言「UPDATE 不许带逐行 WHERE」，
    理由是「真库 91.6% 的行带 3 位以上小数，逐行判据会漏」—— 那是 052 它那套判据
    （`volume <> floor(volume)` 配 19.24% 本来就是整数的「手」）的性质，对 turnover
    **不成立**：`turnover < volume×close×0.01` 逐行反而能抓住混合行。理由不成立的守卫
    就是假护栏，换成真不变量「哨兵 INSERT 必须写在 IF 之外」（跳过换算的那次启动也要
    留痕），并在 `.sql` 里写明混合表不由本迁移修复、为什么。新增第五腿破坏验证：
    把 INSERT 挪进 IF ⇒ 结构断言与 `SkipsWhenAlreadyCNY` 双双红，还原后 sha256 一致。
    ⑦ 还原腿自己踩了一次环境坑：用 Python **文本模式**改写 `postgres.go` 把 LF 写成了
    CRLF（1246 处），而 `.gitattributes` 的 `eol=lf` 让 `git diff` 显示「无变化」——
    只有 sha256 暴露了它。哈希比对这条腿这次真救了场；改用二进制读写后 sha256 与基线一致。
  - **差点写错的一条**：我原本判断 `/api/market/index` 恒空是 `Scan(&close)`（close 已是
    `*float64`，取地址即 `**float64`）的类型错，实测 pgx **接受**这种目标类型
    （`err = <nil>`，600519.SH 单行探针）—— 真实原因是那三个指数代码在日线表里 0 行。
    DATA-07 的 ② 按实测写，不沿用推断。教训同 `hash 命中≠已上线`：能跑一次的结论
    不要靠形状推断。

### DATA-07 · 仪表盘「市场概览」的成交量/成交额恒显示 '-'（没有数据通道 + 指数 bar 不在日线表） ⬜ 待做

- **① 目标**：`/api/market/index` 真的把 `volume`/`amount` 给到前端并带上指数 bar，或者删掉这两格 —— 界面上不再存在恒为 '-' 的指标。
- **② 上下文**（2026-10-15 DATA-05 实测，两层独立缺失）：ui 仓 `web/src/components/dashboard/MarketMetrics.vue:31-32` 显示「成交量/成交额」两格，取 `metrics.volume` / `metrics.amount`；数据来自 `Dashboard.vue:88 getMarketIndex()` → 本仓 `cmd/data/handlers_stocks.go:125 marketIndexHandler`，其响应结构 `indexData`（:130-136）**只有** code/name/close/change/pct ⇒ 两个字段从来不在响应里，`fmtAmount(undefined)`（ui `web/src/utils/format.ts:40`）走 falsy 分支恒 '-'。第二层：该 handler 查的 `000300.SH / 000001.SH / 399006.SZ` 在 `ohlcv_daily_qfq` 里 **0 行**（实测 count=0，表内 5671 个 symbol 不含指数），实测 curl `:8085/api/market/index` 返回 `{"indices":[]}`。
- **③ 要求**：先定「指数日线 bar 从哪来」（tushare `index_daily` 还是别的源），二选一并写明理由 ——(a1) 补指数同步 + 响应带 `volume`/`amount`（单位按规范层：股 / CNY）；(a2) 删掉这两格，界面与数据结构一致。
- **④ 约束与非目标**：不动单位口径（DATA-05 已定，别在这里重开）；不顺手修 DATA-03 的吞错家族；不新增数据源依赖（需要新源先报主任）。
- **⑤ 验收**：一条行为测试钉住「响应的 volume/amount 与库里逐字段相等」；接口或浏览器实测显示真值（或删格后界面一致）；`go build ./...` + `go vet` + `gofmt -l` 干净。
- **⑥ 边界**：只做「界面有指标但没有数据通道」这一条。

### DATA-08 · 多源 fallback 链零生产调用方（Registry 只服务观测端点） ✅ 完成（2026-10-16，branch `task/data-08-registry-unwired`）

- **① 目标**：`Registry.Fetch` 在生产里有真实调用方并落到某张表，或者把这条链明确停用（配置 + 注释 + SPEC 一致标注「未启用」）。
- **② 上下文**（2026-10-15 DATA-05 实测，逐行 grep）：`ETLPipeline.Process`（`pkg/data/source/etl.go:75`）是 `Registry.Fetch`（`pkg/data/source/registry.go:150`）的唯一生产调用方，而 `ETLPipeline` 在 cmd/pkg 非测试代码里**零调用方**；Registry 只被 `cmd/data/registry_handlers.go` 的 status/health/chains 观测端点使用。⇒ `ohlcv_daily: [tushare, mootdx, eastmoney]` 今天不会真的降级取数。同族先例：SYNC-01（「函数写得对但零调用方」，编译期与行为测试都看不见）。
- **③ 要求**：给出接线计划（哪条工单在什么路径上调 ETLPipeline、落到哪张表、与 sync_jobs 队列的关系），或明确停用并删掉链配置与相应注释。
- **④ 约束与非目标**：单位口径已在 DATA-05 收口，本单不重开；**不在本单顺手接 mootdx SDK**（新依赖需主任批准）；不改 Registry 的降级语义本身。
- **⑤ 验收**：停用路线 —— `grep -rn "SetChain" cmd/data/registry_init.go` 与 SPEC 的链表一致且注明未启用；接线路线 —— 有一条从 HTTP/作业层真的走一次的测试（不是直接调 Process 的行为测试）。
- **⑥ 边界**：只判断「链是否可达」；链上各源的可靠性（ODR-011 风险表）另议。
- **✅ 完成记录（2026-10-16，分支 `task/data-08-registry-unwired`，隔离 worktree `.workbuddy-ai/worktrees/data-08`）**：
  - **裁决 ③ = 停用路线**（主任 2026-10-16 当面二选一确认）。理由五条，全部有实测支撑：
    1. **⑥ 的边界就是「只判断链是否可达」** —— 本单的职责是给这条链一个明确的状态，不是新建 ingestion 能力。
    2. **接线不是「接个线」，是新建一条生产写路径**：`ETLPipeline.Process` 要求调用方传入 `Normalizer`，而全仓生产代码**零个** `source.Normalizer` 实现（grep 实测；`equitydeep.NewNormalizer` 是另一条 ingestion 线的类，不经 Registry）。接线 = 新写规范化层 + 新端点/作业 + 落库对拍，宽度是一个特性单。
    3. **生产 ingestion 全线绕开 Registry 是既定架构，不是疏忽**：`handlers_sync.go` / `sync_handlers.go` 直连 `TushareClient`（`FetchDailyOHLCV` / `FetchTradingCalendar` / `FetchIndexConstituents`，落库在客户端内部）；连 macro（P2-1）与 daily_basic（D5）的 adapter 都是「注册即观测、同步另立」（SPEC 明载「调度另立」）。在 ETL 上平行再接一条 ingestion，等于给同一件事两套架构，须主任级决策而非工单内顺手。
    4. **在途 DATA-09 与任何新库写路径有交互**（测试迁移隔离刚收口），本单不宜同时扩大写面。
    5. **停用路线可完整满足 ⑤**，且与 AUD-45/46 的哲学一致：「零调用方」本身不是缺陷，但必须是**可见且刻意**的状态 —— 本次就是把这个状态钉出来。
  - **落点（代码侧，commit 1）**：
    - `internal/repoguard/registry_etl_wiring_test.go`（新）：机械钉住「未接线」——规则 A：生产代码（cmd/pkg/internal 非测试文件）出现 `NewETLPipeline` 调用、或**包外**出现 `ETLPipeline` 复合字面量 ⇒ 红；规则 B：`registry_handlers.go` 出现 `.Fetch` 选择器 ⇒ 红。替身腿当场证明牙口（跨包调用/同包调用/包外字面量各红；构造器本体的包内字面量与只读观测形态放行）。
    - `cmd/data/registry_init.go`：包文档、`buildDataSourceRegistry` 文档、`configureFallbackChains` 设计注释三处补 DATA-08 状态；顺带订正 realtime 链的**假声明**——「eastmoney 是 SDK 不可用时的备胎」今天不成立（eastmoney 只声明 capital_flow，`Fetch(realtime_quote)` 直接 ErrUnsupported，链腿是 chain drift 形状）。
    - `pkg/data/source/etl.go`：类型/构造器文档补 DATA-08 状态；**订正一处过期注释**——Process 步骤 5 原写 "BulkInsert (currently a stub; Sprint 1 wires the real insert)"，而真实实现早已随 ODR-011 落地（`dfec481`，实测 git log -S 归因；CR-21 只是接口抽取）。叙述与实现分叉正是本仓反复登记的缺陷家族，故按实现改写。
  - **落点（文档侧，commit 2）**：`docs/SPEC.md` 两处——「已知边界」里「链本身何时接通属另一工单」改为 DATA-08 收口事实；「Adapter registry & fallback chain」端点块加注：三端点纯观测、链展示设计意图而非已生效的降级、不可达腿点名、DATA-10 前置与护栏指针。
  - **③ 第一半（接线计划）的兑现**：不停用不等于不留计划 —— 新立 **DATA-10**（见下），把「哪条工单/什么路径/落哪张表/与 sync_jobs 的关系」写全，含候选链按可达性排序与前置条件。
  - **⑤ 验收（停用路线，实测证据）**：`grep -rn "SetChain" cmd/data/registry_init.go` 实测 **15 条**链配置（另 1 行为注释提及，共 16 行 grep 输出）。链表与 SPEC 的一致性按「不手抄」原则落实：`docs/SPEC.md`「Adapter registry & fallback chain」节注明整套 Registry 为**纯观测面、链展示设计意图而非已生效的降级**，并点名两条不可达腿（mootdx nil transport / eastmoney 未声明日线·分钟·实时）；完整链表的唯一真源是运行时 `GET /api/datasource/registry/chains` —— 手抄 15 条进文档正是 AUD-45 登记的「清单腐烂」形状，故不抄。`go test ./internal/repoguard/ -run 'TestProductionCodeDoesNotWireETLPipeline|TestRegistryObservabilitySurfaceDoesNotFetch'` exit 0（仓库级两条），替身四条 exit 0。
  - **破坏验证（两腿，均「改坏→红→还原→绿」）**：① `registry_init.go` 末尾追加 `source.NewETLPipeline(reg, nil)` ⇒ `TestProductionCodeDoesNotWireETLPipeline` 红，点名 `cmd/data/registry_init.go:285: NewETLPipeline 调用`；② `registry_handlers.go` 追加带 `h.reg.Fetch(...)` 的方法 ⇒ `TestRegistryObservabilitySurfaceDoesNotFetch` 红，点名 `:185: .Fetch 选择器调用`。还原用 `git checkout --` + **sha256 与基线逐字节一致**（`a8eaa780…` / `d4ed096e…`，`sha256sum -c` 双 OK）。
  - **开发中自抓并修掉的两个假护栏（如实登记，本项目「假护栏比没护栏更糟」的同款自查）**：① 第一版把包内 `ETLPipeline` 字面量也算违规 ⇒ 构造器本体 `return &ETLPipeline{...}` 会中枪，守卫从第一天起常红 —— 收窄为「包外才算接线」；② 第一版照抄 `package_wiring_test.go` 的 `parser.ImportsOnly`，函数体整个不进 AST ⇒ 替身腿全部落空、仓库级测试**假绿** —— 改完整解析（mode 0）后替身腿才真红真绿。
  - **门禁**：`go build ./...` exit 0；`go vet ./cmd/data ./pkg/data/source ./internal/repoguard` exit 0；`gofmt -l` 对本单 **3 个 Go 文件**空输出（DATA-09 已登记整仓 gofmt 有 8 个别人文件的既有噪声，本单不顺手修）；`go test ./internal/repoguard/ -count=1` 全量 exit 0（**23 条 = 既有 19 + 本单 4**）；`go test ./pkg/data/source/ -count=1` exit 0。
  - **④ 逐条核对**：单位口径未重开（未动任何换算常量）；mootdx SDK 未接（go.mod 无新依赖）；Registry 降级语义未改（registry.go 一字未动）；链配置未删（⑤ 要求 grep 可得且与 SPEC 一致）。
  - **⑥ 边界**：链上各源可靠性（ODR-011 风险表）未议；DATA-10 落地前「链不可达」保持现状。
  - **并行隔离**：主工作树开工时即挂着其他 agent 的未提交改动（`pkg/data/source/contract.go` 一族，且 DATA-09 的合并 `d005187` 在本单核对期间落地），故全程在隔离 worktree 施工，未碰主工作树；合并与否归主任。

### DATA-10 · 多源 Registry 接线计划（停用状态的显式解禁入口） ⬜ 待做

- **① 目标**：把一条 fallback 链真正接进生产 ingestion（经 `ETLPipeline` 或主任认可的等效路径）并落到对应表；若评估后仍不接，显式复审 DATA-08 的停用裁决并留痕。
- **② 上下文**（DATA-08 2026-10-16 收口实测）：`Registry.Fetch` 无生产调用方；`ETLPipeline.Process` 的 `Normalizer` 全仓零生产实现；生产 ingestion 直连 `TushareClient`；`capital_flow`/`global_ohlcv` 两条链的 primary adapter 已启用（eastmoney / yahoo_finance，均无 key、默认启用），`ohlcv_daily` 的 mootdx/eastmoney 两条备胎腿不可达（nil transport / 未声明该类型）。候选落库表现状（2026-10-16 真库 `quant_trading` 只读实测）：`capital_flow` **0 行**、`global_ohlcv` **0 行**、`top_list` **0 行**、`stock_fundamentals` **0 行**、`market_activity` **0 行** —— 全是「表在、没数据」的形状，接线即灌数；另注意本仓**不存在** `capital_flow_daily` 表（`capital_flow` 是 Migration 035 的东财 push2 表，别把两张表的账记混）。「未接线」由 `internal/repoguard/registry_etl_wiring_test.go` 钉住 —— 本单落地时该护栏按计划放行（加白名单 + 写理由），放行本身就是接线决策的留痕。
- **③ 要求（接线计划，DATA-08 ③ 第一半的兑现）**：候选链按**可达性**排序，逐条给「适配器状态 / 落库表 / 前置条件」三件——
  1. `capital_flow: [eastmoney]`（首选）：adapter 默认启用（无 key 公开 push2 接口）、`TableMapper` 已映射 `capital_flow` 表、`BulkInsert` 事务 + upsert 幂等已就绪；表当前 **0 行**（2026-10-16 真库实测；该表属东财线，与 Tushare 额度无关）。
  2. `global_ohlcv: [yahoo_finance, alpha_vantage]`：yahoo 默认启用；境外单位口径 SPEC 已有规定，勿与 A 股口径混。
  3. `daily_basic` / `macro`：adapter 已注册但同步「另立」（P2-1 / D5 既有决议），若先落这两条，须与那两线的排程工单合并考虑，别双开。
  4. `ohlcv_daily` 的备胎腿：前置是 mootdx SDK（新依赖，**须主任批准**）+ 源单位实测 + 同步边界换算（DATA-05 守卫自动开始管）。
  - 入口路径二选一并写明理由：(a) HTTP 层 —— 仿 `POST /sync/index-constituents/:index_code` 新增同步端点，DATA-06 的 (a1) 上抛口径直接适用；(b) 作业层 —— `sync_schedules`（cron）→ `sync_jobs` → worker 既有链路（SYNC-01/02/04 已修生命周期），扩 executor 或加 job_type，与 `sync_handlers.go` 的 `e.tc` 模式对齐。
- **④ 约束与非目标**：不重复 DATA-08 已做的停用标注（本单是解禁不是重贴）；新依赖须主任批准；不改 Registry 降级语义；单位口径不重开（DATA-04/05 已定，接线只做「换算到规范层」）；不借本单修 DATA-03 的吞错家族。
- **⑤ 验收**：一条从 HTTP/作业层**真的走一次**的测试（不是直接调 `Process` 的行为测试）—— DATA-08 ⑤ 接线路线的原话；落库后抽样比对（写进去的行与源响应逐字段一致）；`go build ./...` + `go vet` + `gofmt -l`（本单文件）干净；护栏白名单条目带理由；SPEC 的「Adapter registry & fallback chain」注从「未启用」改为该链的实况。
- **⑥ 边界**：只接**一条**链（一源一单）；链上各源可靠性（ODR-011 风险表）另议；调度策略（cron 表达式、错峰）另议。

### DATA-09 · 库侧测试会跑 migrate()，把 DML 迁移直接作用在共享开发库上 ✅ 完成（2026-10-16，branch `task/data-09-test-migration`：`4a29393`）

- **① 目标**：跑测试不再静默改共享开发库的数据；应用迁移是一次**显式**动作（启动/部署路径），而不是测试副产物。
- **② 上下文**（2026-10-15 DATA-05 当场撞上）：`NewPostgresStore` 在 `pkg/storage/postgres.go:43` 直接调 `store.migrate(ctx)`，而直连 `localhost:5432/quant_trading`（**开发库本身**）的测试不少 —— `pkg/storage/postgres_test.go:16 testStore`、`pkg/data/tushare_adjustment_integration_test.go`、`cmd/data/handlers_ingest_test.go`、`cmd/data/handlers_stocks_test.go`。后果实测：`go test ./pkg/data/...` 一条命令就把 054 的整表 ×1000 真的跑了，无授权无提示；而 052/054 这类 DML 迁移**不可重放**（哨兵置位后重启不补跑），混合表只能靠备份回滚。同族：`pkg/storage/factor_registration_test.go:24-25` 与 `:32` 直接 `DELETE FROM factor_registrations` —— 并发会话共用同一个库时会互删数据（本仓当前确实有多个 worktree 会话在跑）。
- **③ 要求**：二选一并写明理由 ——(a1) 库侧测试统一改指一次性容器/独立 schema（`pkg/storage/integration_test.go` 已有 dockertest 先例，把它做成唯一入口）；(a2) 生产入口保留 `migrate()`，但测试不得经 `NewPostgresStore` 走迁移（提供一个「连接不迁移」的构造入口，且该入口在生产侧零调用方，防它被拿去绕门禁）。
- **④ 约束与非目标**：不删既有断言、不把真库测试改成 mock（本仓纪律要求真库证据）；不改 DML 迁移自身的守卫形状（052/054 的双守卫已验证，不是本单对象）；不在本单回滚 054 已经发生的写入。
- **⑤ 验收**：在有开发库的机器上跑 `go test ./pkg/data/... ./pkg/storage/...`，前后对 `data_migrations` 行集与 `ohlcv_daily_qfq` 的计数 + 若干列值做比对，必须**逐字节一致**；一条负例：新增一个假 DML 迁移不会被测试执行（改坏 → 红 → 还原 → 绿）。
- **⑥ 边界**：只治「测试改共享库」；真库备份/恢复流程与并发会话的数据隔离另议。
- **完成记录（2026-10-16，`4a29393`）**：
  - **选型裁决（③ 二选一）= (a2)**，不选 (a1)。理由三条，都在实测里：(a1) 要把这 14 个调用点搬进 dockertest 一次性容器，而本机 **PostgreSQL 是宿主机原生跑的、不在 compose 里**（实测 `docker compose ps` 只有 analysis/data/ai/strategy），容器起来的库是**空库**——本仓大量真库断言（`skipIfNoSeedData`、「ohlcv 非空才跳」那一族、D 系列的取证腿）读的正是开发库里已灌的 392.6 万行，搬过去等于把「真库证据」换成自灌夹具，与 ④「不把真库测试改成 mock」顶牛；且 `pkg/storage/integration_test.go` 的 dockertest 挂在 `//go:build integration` 之后，默认套件根本不跑它。(a2) 将「跑迁移」与「连库」这两件事拆开，改动限于构造入口与调用点，不动数据也不动断言。**代价如实登记**：库侧测试从此**预设库已迁移**，新库第一次 `go test` 会红在 `relation … does not exist`（修法是起一次服务或跑 tagged 套件，见下方「未收口」第 3 条）。
  - **落点**：`pkg/storage/postgres.go` 把连接段抽成 `connectPostgresStore`（parse pool + Ping），`NewPostgresStore` = 连接 + `migrate()` + `SeedBuiltinFactorHypotheses`（**生产语义一字未动**），新增 `NewPostgresStoreConnected` = 只做连接。迁移在 `migrate()` 里的唯一调用点没有增加、没有搬家（grep 实测：全仓仍只有那一个 `store.migrate(ctx)`）。测试侧 14 处调用点改用不迁移入口（`pkg/storage`×4、`cmd/analysis`×4、`cmd/data`×2、`pkg/backtest`×2、`pkg/data`×1、`pkg/live`×1）；`pkg/storage/integration_test.go`（dockertest 一次性容器，「构造 migrate()」就是被测对象）保留迁移入口并进 allowlist。
  - **顺带订正一批会变成假证据的注释**：`scNewStore` / `newIndustryTestRouter` / `newMacroTestRouter` 原本写着「含 migrate()——Migration 049/050 随之执行」，`TestFundamentalsDetailSchema_AppliedByMigrate` 的报错原本写着 "must exist after migrate()"。这些句子改完就是假的，而它们正是下一个人判断「谁负责落库」的依据，所以逐条改成「不迁移入口；049/050 由启动路径落库」，并把那条 schema 断言的证据形状如实改写（它今天证明的是「已迁移的开发库里这张表在」，「migrate() 能建出它」那一半归 tagged 套件）。`postgres_test.go` 的 `TestNewPostgresStore` 改名 `TestNewPostgresStoreConnected_Connects`——它测的确实是新入口。
  - **护栏**（`internal/repoguard/store_migration_isolation_test.go`，go/ast，注释里的提及不算调用；照 `dsn_single_impl_test.go` / `calendar_seed_isolation_test.go` 的先例写）钉**四个方向**，缺一个就是假护栏：① 默认构建（无 `integration` tag）的 `_test.go` 不许调迁移入口——**防的就是本单治的这个病复发**；② 不迁移入口在生产侧零调用方——③(a2) 点名的那条，防它变成「带着没迁移的 schema 起服务」的后门；③ 迁移入口的**生产**调用方必须逐条进 allowlist 且**写理由**，allowlist 里的**死条目同样报红**（登记会腐烂，只查「多」不查「少」的 allowlist 是半个护栏）；④ 两个入口的**函数体形状**：`connectPostgresStore`（两段共用）里出现 `migrate()` 红、`NewPostgresStore` 里 `migrate()` 不是恰好一次也红——只钉调用方的护栏挡不住「有人把迁移塞进共用连接段」，那会让 ① 立刻名存实亡。覆盖面自检腿点名必须看见 `pkg/storage/postgres_test.go` 与 `cmd/data/handlers_ingest_test.go`（walker 瞎了时「无违规」不成立），另有 5 条 snippet 反例腿（含两条对照腿：不迁移入口放行、注释提及放行、tag 判定不由文件名）。
  - **⑤ 验收（库读数逐字节一致）**：快照脚本固定读 `data_migrations` 全行集（name+applied_at 有序）、`ohlcv_daily_qfq` 计数 + **全列有序 md5 校验和** + 首末各 5 行字面值、public 关系指纹（name+kind 有序 md5 与清单）、`factor_hypothesis` 计数、探针表存在性；脚本自身跑两遍先证**读数确定**（diff 空）再用作基线。基线 = 开工前，此后依次跑了 `go test ./pkg/storage/... ./pkg/data/...`、`./pkg/backtest/... ./pkg/live/... ./cmd/data/... ./cmd/analysis/...`、护栏全量、以及下面的负例腿与破坏腿，**每一次前后 diff 空**，收尾再与开工基线整体 diff 仍空：`DEV_DB_BYTE_IDENTICAL_VS_BASELINE`（3,926,510 行校验和 `36b5876a…` 未变，`data_migrations` 仍 2 行、applied_at 仍是 10-14 / 10-15 那两个旧时刻 ⇒ 没有任何新迁移被落进来）。
  - **⑤ 负例（新增一个假 DML 迁移不会被测试执行）**：临时往 `migrationStatements()` 追加一条 `DO $$ CREATE TABLE IF NOT EXISTS data09_probe_negative_leg …; INSERT … NOW() $$`，跑 `pkg/storage`+`pkg/data`+`cmd/data`+`cmd/analysis`+`pkg/backtest`+`pkg/live` 全套 ⇒ 开发库里该表 `count=0`（不存在）。**反证腿（探针不是空转，否则这条负例什么也没证）**：把 `testStore` 改回迁移入口并把 DSN 指到一次性库 `data09_probe_db` ⇒ 同一条测试跑完，探针表**有 1 行**、库里 **39 张表**——证明「只有跑 migrate() 才留下痕迹」这个判据确实敏感，也顺带量到迁移入口在空库上仍能一次建全（显式路径没被我改坏）。同一时刻回读开发库仍是 0。现场还原：`DROP DATABASE data09_probe_db` 后 `pg_database` 计数 0；`testStore` 与 `postgres.go` 还原判据用 **sha256 与基线逐字节一致**（`bffc2e2d…` / `7af28c76…`），不用 `git diff` 为空——见 `local-stack-evidence` 里 CRLF 那条教训。
  - **破坏验证（5 腿，均「改坏→红→还原→绿」）**：生产文件调不迁移入口 ⇒ ②红；生产文件调未登记的迁移入口 ⇒ ③红（两条红点名 `file:line`）；默认构建的测试文件调迁移入口 ⇒ ①红；`connectPostgresStore` 里加 `store.migrate(ctx)` ⇒ ④红且文案指到共用段；`NewPostgresStore` 里删掉 `migrate()` ⇒ ④红且文案指到「必须恰好一次」。临时违规文件与两处临时改写全部删除/还原，还原后 `go build ./...` exit 0、`go vet`（7 个受影响包）exit 0、`gofmt -l` 对**本单文件**空输出、`go test ./internal/repoguard/ -count=1` exit 0。起手是先红后绿：护栏写完时实现还不存在，①报出 14 处、②报「一个调用方都没识别出来」——正是 AGENTS.md §8.5.4 要的那条「证明测试真在测东西」。
  - **既有红登记（不是我引入的，ODR-067 要求红在案）**：① `TestAdjustmentBasis_QfqLookAheadLeaksIntoBacktest`（`pkg/data`）红——在 **pristine `2eba0a7` 的独立 worktree** 上跑同一条，同样红且三个数值逐字一致（1.7867 / 2.9955 / 4.1345 个百分点），归因不靠猜；它是取证阈值随库内数据漂移的问题，与本单无关，不在 ⑥ 范围内动它。② `cmd/data TestSyncAllFactorsHandler_ReportsSkipped` 红（want 5 got 8，2026-10-15 已登记过一次）——该用例用 `emptyFactorStore{}` 假 store，**不连库、不经任何构造入口**，形状上与本单无交集。③ `gofmt -l .` 在 `2eba0a7` 上就列出 **8 个**我没碰的文件（`cmd/data/setup.go`、`pkg/bootstrap/{server,auth_test}.go`、`pkg/kernel/adapters.go`、`pkg/strategy/{checkpoint,signal_provider_test}.go`、`pkg/backtest/reporting/compare_test.go`、`internal/repoguard/ai_boundary_test.go`，全是 import 分组顺序）——AUD-27 说「整仓 gofmt 可以当结论用」，今天就其噪声而言不再成立，登记here，本单不顺手修（会把 8 个别人的文件扫进我的 commit）。
  - **⚠️ 未收口的三半（不假装绿）**：
    1. **②点名的同族另一半没治**：测试仍然**写数据**进开发库——`factor_registration_test.go` 的 `DELETE FROM factor_registrations`（全表，不是自清理）、`stocks` 探针行、AUD-62 那一族 calendar 写删。a2 只把「**迁移状态**」从测试里拿走，没有给并发会话做数据隔离，⑥ 明写「并发会话的数据隔离另议」。要治得先决定隔离单位（per-schema？per-test 事务回滚？一次性库 + 夹具灌数？），那是另一单的宽度。**审查时又量到第三种形状**（比上面那族更轻，但同属「测试碰库」，别以为本单之后测试完全不碰 schema）：`pkg/storage/macro_test.go:186` 与 `pkg/storage/supply_chain_test.go:272` 会 `db.Exec(ctx, ddl)` **重放 DDL** 来核验幂等。它不写 `data_migrations`、不改数据，① 够不着；而 ④ 明令「不删既有断言」，所以**保持原样不动**——但在空库上它只会建出自己那几张表（半迁移态），这正是下面第 3 条的成因之一。
    2. **护栏只判直接调用点**：将来若出现「测试经 `bootstrap.OpenStore` 之类包装间接走到迁移入口」，静态腿看不见（文件头已披露此边界；今天全仓 `migrate()` 只有一个调用方，即 `NewPostgresStore` 自身，无包装）。行为侧的兜底是 ⑤ 那条假 DML 探针，它是手工跑的，没有常驻 CI——把它做成常驻需要一个「能建库」的测试环境，本机不具备（PG 在宿主机原生、CI 侧无库），故留字不做。
    3. **新库第一次跑库侧测试会红**在 `relation … does not exist`。这是 (a2) 的**设计代价**而不是 bug，但必须有明确出路：起一次服务（`docker compose up -d data-service` 之类，`migrate()` 是启动路径）或跑 `go test -tags=integration ./pkg/storage/...`（dockertest 一次性容器）；`scNewStore`/`testStore` 的报错文案已点名「先起 PG」。若日后觉得这条坑太隐蔽，出路是给不迁移入口加「关键表在不在」的启动自检——本单不加（那是往 fail-loud 的路上发明新语义，超出 ⑥）。
  - **并发与归属**：主工作树开工时挂着别人的在途改动（`pkg/storage/postgres.go`、`pkg/data/tushare*.go`、daily_basic / `market_activity` 一线，且**开发库里已经有他们经测试建出的 `market_activity` 表**——正是本单治的那个病的现行样本）。故本单全程在 `.workbuddy-ai/worktrees/data-09`（分支 `task/data-09-test-migration`）上做，未碰主工作树一个字节；合并归主任。代码 commit 与本文档 commit 分开（规范 3「文档同步 = 独立 commit」），她若要把台账收回自己笔下，丢弃文档 commit 不影响代码。
  - **自审修订（2026-10-16，`dc82ab6`，审 `4a29393` 后追加）**：三处，都在我自己笔下——而且第 1 条正是我在这张台账里批评过的那种毛病。
    1. **我在护栏文件头引了自己刚搬动的行号**：写的是 `pkg/storage/postgres.go:19` 与 `:43`，而本单把函数拆了位置，实测现在是定义 `:33`、`store.migrate(ctx)` `:39`。行号引用在这种文件里就是死链发生器，改成只点符号（符号不会随编辑漂，行号当场漂）。
    2. **我写了一句假的自我豁免**：原文声称「两个入口名由片段拼出，所以完整字面量不出现在本文件里」——`grep -o` 实测出现 **8 次**（5 次写的是迁移入口、3 次是 `Connected` 那一段，全在注释里）。 walker 靠 AST 只认 CallExpr，所以功能上没有误报，但**这句声明本身是假的**，而下一个人会信它。修法不是删注释（读不懂的护栏等于没有，注释里写清两个入口的分工正是这份文件的主要价值），而是把它想保护的性质升级成可执行的腿：`TestStoreConstructorGuardDoesNotMatchItsOwnProse` 双向钉——前提腿要求正文确实完整写出入口名（否则这条腿空转，什么都没演示），结论腿要求 walker 在本文件里抓到 0 个调用点。**破坏验证**：把拼装的片段常量改成一个不存在的拼写 ⇒ 前提腿当场红（`"-3" is not greater than or equal to "2"`，负数是刻意的：`Connected` 的拼写里含一整段 `NewPostgresStore`，不减掉就会把「不迁移入口被提了几次」冒充成「迁移入口被提到了」，这条在第一版就写错过、被自己的前提腿抓住）；还原判据 sha256 `fe7c1377…` 与基线逐字节一致。
    3. **一处断言没有配核对**：`NewPostgresStoreConnected` 不跑 `SeedBuiltinFactorHypotheses` 是我定的（seed 也是往共享库写行），但当时只在注释里断言「生产语义不变」，没验证「测试侧不 seed 会不会破坏谁」。补实测：`factor_hypothesis` 在全部 `*_test.go` 里**零命中** ⇒ 14 处测试无一条依赖 seed；把这条核对结果写进入口注释，免得下一个人觉得「测试库没有假设数据」而把 seed 请回测试路径。
    - 复验：`gofmt -l`（本单文件）空输出、`go build ./...` exit 0、`go vet ./internal/repoguard ./pkg/storage` exit 0、`go test ./internal/repoguard/ -count=1` 19 条全绿（含既有 8 个 repoguard 文件不受影响）。
  - **合并预演抓出第 15 处调用点（`f13b503`，随并 `main 3de5ef6` 后新落）**：把 main 并进本分支后护栏立刻红一条 —— D5 线的 `pkg/data/source/daily_basic_integration_test.go:85`（`openD5Store`）走的是迁移入口，且它的注释原本就写着「migrate() 会带上 Migration 056 的 market_activity」，即**这条用例依赖测试自己建表**。已改为不迁移入口并把新前提写回注释。这条不是洁癖：不改，合并态就带着一条新红进门（违反 ODR-067「新红不得混进既有红」）。顺带：`cmd/data TestSyncAllFactorsHandler_ReportsSkipped` 那条既有红已由 D5 线在 `1489c54` 校正，本分支合并态实测 `cmd/data` exit 0，登记表里的它已过期。
  - **⚠️ 本单 (a2) 对 CI 的真实代价（两侧都量过，待主任裁决，未擅自改 `.github/workflows`）**：CI 的 `go` job 带一个 **空的** `postgres:16` service 后直接 `go test ./... -race`。用一次性空库（`data09_ci_sim*`，跑完 DROP，`pg_database LIKE 'data09%'` 残留 0）模拟同一形状，实测两侧：
    · 迁移入口 + 空库 ⇒ `TestD5Integration_RegisterSyncComputeScores` **exit 0**，库里被建出 **39 张表**含 `market_activity` ⇒ **今天 CI 的库侧测试绿，靠的正是「测试自己跑迁移」这件事**（`tools/ci/seed_alignment_fixture.sql` 只建 `ohlcv_daily_qfq` 一张，撑不住其余 38 张）。
    · 不迁移入口 + 空库 ⇒ **exit 1**，`relation "market_activity" does not exist (SQLSTATE 42P01)`，库内 **0 张表**（也反证不迁移入口确实不落任何 schema）。
    ⇒ 合并本单后，CI 需要一次**显式**迁移才跑得动库侧测试。建议形状（等批）：加一个最小命令（读同一套 `storage.BuildDSN` 配置、调 `NewPostgresStore` 建完即退，零业务面），在 `ci.yml` 的 Test 步骤前跑一次；这一来 ①「应用迁移是一次显式动作」在 CI 上也就成了字面事实。**为什么不顺手做**：改 CI/CD 是共享状态且本机无法验证（Actions 不在本地跑），而 ⑥ 的边界只到「测试改共享库」——留字比默默改强。第二方案（**否决**，理由实测）：让库侧测试在 schema 缺失时 skip —— `ci.yml:91-92` 写着提供 Postgres service 的全部目的就是「让 PIT 等回归测试真正被执行而不是被跳过」，skip 方案正好把这行注释的意图作废；另注意 `pkg/storage/integration_test.go` 头部声称的「coverage ≥ 60% 门禁由 tagged 套件满足」在今天的 `ci.yml` 里**没有对应物**（grep `coverage|60%|codecov` 零命中），那是文档漂移，别拿它当合并依据。

### DATA-06 · 指数成分股同步：落库失败只 Warn，端点照样回 "synced successfully" ✅ 完成（2026-10-16）

- **① 目标**：`POST /sync/index-constituents/:index_code` 在**落库失败时不再报成功**——响应体要么 5xx，要么如实区分「取到 N 条」与「落库 N 条」。
- **② 上下文**（2026-10-14 做 DATA-02 时顺带 grep，逐行核对非推测）：
  - `cmd/data/handlers_sync.go:52` 调 `tc.FetchIndexConstituents(...)`，拿到非空切片就回 `{"message":"index constituents synced successfully", "count":N}`；而**落库发生在客户端内部**：`pkg/data/tushare.go:690-692` 调 `c.store.SaveIndexConstituentBatch(...)`，**error 只 `logger.Warn` 后继续**，函数返回 `constituents, nil`。于是「Tushare 取到了、一行都没写进去」与「写成功了」在 HTTP 面上**不可区分**。
  - 这条路径正是 DATA-01 灌 `index_constituents` 354 行的通道（该单 ⑤ 的破坏验证还依赖「清空成分 ⇒ `resolveStockPool` fail-loud」）——下游对它的信任建立在「它报成功」上。
  - **同族先例**：SYNC-01（生命周期入口零调用方，编译期与行为测试都看不见）、DATA-03（`Scan` 的 error 被整个丢掉 ⇒ 字段静默缺席）。本单是第三种形状：**接住了 error，但只记日志不改变响应**。
  - 顺带纠正本文件 D3 完成记录「已知边界 ③」的一处事实错误（不改其结论，只加指针）：该处称「`pkg/storage` 无 `SaveIndexConstituents` API，补全需发明落库 API」——实际 `(*PostgresStore).SaveIndexConstituentBatch` 在 `pkg/storage/cache.go:369` 已存在且正被 `pkg/data/tushare.go:690` 调用。`bdd1949` 多传的那个参数已于 `abd9067` 撤销，`cmd/data` 编译已恢复。
- **③ 要求**：二选一并写明理由 —— **(a1)** 把落库 error 上抛（`FetchIndexConstituents` 返回该 error，或拆出「取数」与「落库」两个返回值），handler 据此回 5xx；**(a2)** 保持 2xx 但响应体如实标 `persisted` 与 reason。无论选哪种，响应里**「取到 N 条」与「落库 N 条」必须是两个可区分的数**，且失败不得只活在日志里。
- **④ 约束与非目标**：不改 `SaveIndexConstituentBatch` 的签名与批量语义；不动 `normalizeIndexConstituents` 的 in/out 区间推导（属 DATA-01）；不碰 `stocks` 表；不顺手修 DATA-02/DATA-03；不加新依赖。
- **⑤ 验收**：行为测试**先红后绿**——经 `TushareStore` 接口注入一个必返回 error 的 stub（生产码不为测试让路），断言 handler 不再回 "synced successfully"（或响应体 `persisted=false` + reason）；反证腿：同一 stub 返回成功时读数照常；活进程一条真实失败路径的 curl 留证；**破坏验证**：把 error 重新吞回 `Warn` ⇒ 新测试红，还原转绿；`go build ./... `+ `go vet` + `gofmt -l` 全绿。
- **⑥ 边界**：只做这一条静默失败的上抛/如实标注；失败后的**重试与作业状态收敛**（与 sync_jobs 队列的关系）另立。
- **✅ 完成记录（2026-10-16，分支 `task/data-06`，隔离 worktree）**：
  - **裁决 ③：选 (a1) 上抛**。理由三条：① 同进程先例 —— `syncCalendarHandler`（handlers_sync.go:484）对 `SaveTradingCalendarBatch` 的 error 直接 5xx；`pkg/data/supply_chain.go` 全线收口为上抛，其测试注释（supply_chain_test.go:292）点名旧 `FetchIndexConstituents` 是吞错反模式、「新代码不许继承」—— 本单把最后这条旧路径收进同一口径。② (a2) 的 2xx 部分成功语义会把「报成功但没落库」换成「报成功但 persisted=false」，调用方照常踩坑。③ `SaveIndexConstituentBatch` 是**单事务全有全无**（cache.go:369，BEGIN→批→COMMIT），结构上不存在「落库一半」的中间态 —— (a1) 后成功路径 `persisted == fetched` 恒成立。
  - **落点**：`pkg/data/tushare.go` —— `FetchIndexConstituents` 落库失败返回 `ErrIndexConstituentPersistFailed` 哨兵 + `%w` 原始错误（错误串自带「index_weight fetched N but saved 0」，两个数写进错误本身）；失败时返回 `nil` 切片，未落库的行不再被当成结果。`cmd/data/handlers_sync.go` —— handler 按哨兵区分环节，落库失败回 500 + 静态文案 `"index constituents fetched but failed to persist"`；内部 DB 原因按本仓 5xx 口径**只进日志不进响应体**（`FailCause`；`httpserver.Error` 对 5xx 一律脱敏，我最初把原始错误断言成对外可见，前提错了，按实测改的）。成功体拆成 `fetched_count` / `persisted_count` 两个数（③ 末句要的「两个可区分的数」）。
  - **对外错误形状（自记边界，别当成内部原因外泄）**：`persisted_count` 是真数（成功路径恒等于 `fetched_count`）；落库失败的「N 取到 / 0 落库」两个数只在日志与错误链里，HTTP 体只有静态环节文案 —— (a1) 下 5xx 状态本身即如实区分。
  - **测试（先红后绿）**：`pkg/data/tushare_index_constituents_persist_test.go` 两腿（stub 经 `TushareStore` 接口注入，生产码未开测试后门）—— 红腿起手 `An error is expected but got nil`，绿后 exit 0；反证腿核对 store 真收到 2 行且 DATA-01 的 in/out 区间语义未被上抛改造碰坏。`cmd/data/sync_index_constituents_persist_test.go` 三腿 —— 红腿起手 `expected: 500, actual: 200` 且 body 原样带 `"synced successfully"`（旧缺陷留影）；成功腿断言 `fetched_count==persisted_count==2` 且 stub 实收 2 行；400 校验腿钉边界不变。
  - **破坏验证（主任 2026-10-16 授权后就地执行）**：把 error 改回旧形态 `logger.Warn` 吞掉 → `TestFetchIndexConstituents_PropagatesSaveError` 红（`An error is expected but got nil`）、`TestSyncIndexConstituents_PersistFailureNotReportedSuccess` 红（`expected: 500 actual: 200`）→ `cp -p` 还原 → **sha256 与基线逐字节一致**（本仓还原判据，不信「diff 为空」）→ 复跑 exit 0。
  - **活进程 curl（同经授权；未重建共享容器，避免把并行 agent 的在途代码带上 :8081）**：从本 worktree 编译 data-service 起 `:8091` —— 成功腿 `POST /sync/index-constituents/000300.SH` → **HTTP 200 `{"fetched_count":354,"persisted_count":354,…}`**（与 DATA-01 登记的 354 行一致），同实例只读 `GET /index/000300.SH/constituents` 回库 **354** 行核对落库为真。失败腿用一次性空库集群 `127.0.0.1:5433`（initdb 于 /tmp，用完 `pg_ctl stop` + 删目录），起 `:8093` 跑完迁移后 `DROP TABLE index_constituents` 再打同一 POST → **HTTP 500 `"index constituents fetched but failed to persist"`**，日志带 `index_weight fetched 354 but saved 0: … relation "index_constituents" does not exist (SQLSTATE 42P01)` —— 「一行没写进去」从此在 HTTP 面可区分。**代价入账**：两次真实 Tushare `index_weight` 调用（共享限流桶），upsert 幂等、库行数不变。
  - **门禁**：`go build ./...` exit 0；`go vet ./cmd/data ./pkg/data` exit 0；`gofmt -l` 五个改动文件空输出；`go test ./cmd/data -run 'TestSyncIndexConstituents_|TestDataRouter|TestStockCountPayload|TestStocksCountHandler'` exit 0；`go test ./pkg/data` 全量 **1 条既有红**：`TestAdjustmentBasis_QfqLookAheadLeaksIntoBacktest` —— 按 D1 先例本单复跑确认 **基线（stash 移开本单改动后）同样红**，非本单引入，登记不修。诚实边界：`-race` 本机跑不了（无 gcc）。
  - **文档同步**：`docs/SPEC.md` 端点行加指针 + 新增「DATA-06 响应形状」节（成功两数恒等的原因、500 静态文案、哨兵机制、以及 **GET 回源路径语义随之一致**：`GetIndexConstituents` DB 空时回源共用本函数，旧行为是把未落库的行直接回给调用方，现在如实 500）。`docs/openapi.yaml` 未登记过该端点（机器可读契约本就没有这条），不动。
  - **④ 逐条核对**：`SaveIndexConstituentBatch` 签名/批量语义未动（只改调用方对 error 的处理）；`normalizeIndexConstituents` 未动（反证腿钉住区间语义）；`stocks` 表未碰；DATA-02/03 未顺手修；零新依赖。`cmd/data/main.go` 只更新了指向 `tushare.go:690` 的易漂移行号注释。
  - **自审修订（2026-10-16，审本单三个 commit 的 diff 后追加，不改写上面原始记录）**：三处真问题，都在叙述与机制的口径上，行为面复核无新问题。
    1. **测试文件头注释与断言不符**：原第 1 条写「落库失败 ⇒ 5xx…原始错误可追溯」—— 那是对外可见的口径，而我中途实测证伪并改掉了（本仓 `httpserver.Error` 对 5xx 一律脱敏，断言改成了「静态环节文案对外、内部原因不外泄」），文件头没跟着改。这正是本单登记的缺陷家族「叙述与护栏各说各话」，我自己又踩了一次。已按断言现状改写注释，并把「原始错误可追溯」的落点写实：error 链上的 `%w` 逐腿断言在 `pkg/data/tushare_index_constituents_persist_test.go`，HTTP 面只保证环节可辨。
    2. **落点 bullet 的 handler 机制描述有歧义**：原句读起来像「Error + 只进日志」，实际是两条通道 —— 落库失败经哨兵走 `FailCause`（静态文案 + cause 进日志），其余错误走 `httpserver.Error`（5xx 同样脱敏）。已在本条订正；上文「裁决 ③」与「对外错误形状」两条与实现一致，不动。另订正一个记法：错误串的实际字面是 `index_weight fetched 354 but saved 0`（数字为真值，活进程日志留证），原始记录里写的 `N` 是规范形式不是字面。
    3. **「更新行号注释」措辞不实**：`main.go:135` 那条注释我实际是**移除**了 `tushare.go:690` 的行号引用（改成按函数名指位），不是更新成新行号 —— 行号引用是本仓反复登记的漂移源，撤掉比续号稳。原句按字面读会误以为留着一个新行号。
    - **rebase 后证据有效性复核（如实边界）**：base 从 `2eba0a7` 移到 `ca06c8b`（d5 两笔落地），其对本单依赖面的改动只在 `TushareStore` 接口追加 `MarketActivityStore`；fetch→persist→error 路径与 handler 行为未变，故活进程 curl 证据仍适用。本单 rebase 后**未重跑真实 Tushare 调用**（省共享限流），复跑的是：`go build ./...` / `go vet` / `gofmt -l` / 两文件全部测试 + `Tushare|IndexConstituent|Constituents` 邻域回归，全部 exit 0。

---

## 二·补二 · 代码深度审查登记（2026-10-15，D1–D4 / D7 / DATA-01·02·04 / SYNC-02·03 完成代码全量审查）

> **审查范围与方法**：对照本文件 ✅ 完成项，对 2026-10-14～15 的 16 个提交涉及的全部生产代码（Go + nt-sidecar Python）做逐文件审查，外加机械检查 `go build ./...` / `go vet ./...` 全绿、`pkg/marketdata` + `pkg/data/source` + `pkg/domain/...` 测试全绿。审查维度：正确性、错误处理、并发、输入校验、资源释放、性能、兼容性、可读性。
> **查重结论（重要，防双登）**：`/stocks/count` 的 `latest_date` 吞错（`cmd/data/handlers_stocks.go:95`）**已被 DATA-03 逐字登记**（含 2026-10-14 实测证据与修法选项），不重复立项；财报 `ann_date` 悬空与宏观 `obs_date` 前视缺口已在 D2-R1；fallback 源/turnover 单位在 DATA-05；index-constituents 假成功在 DATA-06。以下 5 项为本次审查**新发现**。

### REV-01 · `marketdata.Router` 分钟/15m 路径无行数上限的全量加载 [P2] ✅ 完成（2026-10-15，commit `0726e3f`）

- **① 目标**：`Router.Bars` 的 1m/15m 路径对「一次吃进整个查询区间的全部分钟 bar」设定显式边界，大区间请求 fail-loud 或被流式处理，不再无上限 materialize。
- **② 上下文**（2026-10-15 审查发现，逐行核对非推测）：`pkg/marketdata/router.go:44-49` 的 15m 分支先 `loadMinute` 把 `[start, end]` 全区间的分钟行全部 Scan 进 `[]market.OHLCV` 切片，再交 `Aggregate` 建 map 聚合——峰值内存 ≈ 区间内全市场分钟行数。无 LIMIT、无分页、无流式、无区间跨度校验。`ohlcv_minute` 现状 0 行（DATA-05 登记的三张空表之一），故当前不可达；但 D6（多频率进回测）以该表灌数为前提，届时「5900 标的 × 240 根/日 × N 日」一次请求即可压垮内存。1d 路径同无上限但日线量级（单票 727 行）可接受，不在本单。
- **③ 要求**：二选一并写明理由——(a1) 区间跨度上限校验（如 ≤60 自然日），超限 400 且报错点名「已支持的最大跨度 + 当前请求跨度」；(a2) 聚合改流式（`rows` 迭代直接进 bucket 累加器，不落全量切片）。无论选哪种，15m 与 1m 两条路径都要覆盖，且错误/拒绝语义与 Router 既有 fail-loud 风格一致（不静默截断）。
- **④ 约束与非目标**：不改聚合口径（aggregate.go 的 bucket 语义是 D1 定稿）；不动 1d 路径；不引入新依赖；不为本单灌 1m 数据（灌数属 D6/D5 前置）。
- **⑤ 验收**：行为测试——大区间 15m/1m 请求被拒（报错含跨度信息）或流式路径数值与现全量路径**对拍一致**；`pkg/marketdata` 既有测试（含 `TestRouterSameRouteThreeFrequencies`）全绿。**破坏验证**：把上限调大/去掉流式保护 → 新测试红，还原转绿；`go build ./...` + `go vet` + `gofmt -l` 干净。
- **⑥ 边界**：只做 Router 取数边界；分钟表灌数、聚合口径、多频率策略消费（D6）另立。
- **完成记录（2026-10-15，`0726e3f`）**：
  - **⚠️ 先纠正本单 ② 的量级（不改写原文，只加指针）**：那条查询是 `WHERE symbol = $1` —— **单标的**取数。② 写「峰值内存 ≈ 区间内全市场分钟行数」与「5900 标的 × 240 根/日 × N 日 一次请求压垮内存」在本函数里不成立：全市场放大是**调用方循环**的形状（正是 REV-02 描述的 `ComputeAllFactors` 那类），不是 `loadMinute` 一次请求。缺陷真实、也确实该限，但量级比原文小三个数量级：单标的一年 1m = 58,320 行 × 实测 144 B/行（`unsafe.Sizeof(market.OHLCV)`，2026-10-15）≈ **8.4MB**（含 symbol 字符串背板约 9.3MB）。后续以 ② 的量级估内存的人会被带偏，所以这里必须写明。
  - **形状裁决（③ 二选一）**：选 **(a1) 跨度上限**，不选 (a2) 流式。两条理由：(a2) 要在 `rows` 迭代器里把 D1 定稿的 bucket 语义（会话校验、可见时刻、午休断点、集合竞价并入）再实现一遍，而 ④ 不许改聚合口径 —— 第二份实现迟早与第一份漂移；且 `ohlcv_minute` 现状 **0 行**（实测复核），「流式与全量对拍一致」这条验收今天根本做不出来。上限在**查库前**判定，零数据即可证伪。
  - **上限取 366 自然日，不是 ③ 举例的 60 天**（技术岔路由我定，代价写明）：60 天会让 D6「多频率进回测」的 15m 视野根本没法用，而一个满足不了任何真实用法的上限，下一个用它的人只会把它调大或删除 —— 比没有护栏更糟。代价：单次请求最坏 8.4MB 级而非 1.4MB 级，需要多年分钟视野的调用方必须自己切片。理由与代价都写在 `maxIntradaySpan` 常量注释里，含「按自然日 ≠ 按行数」的含义边界（A 股 240 根/日 vs 美股 390 根/日，D7/D9 国外线到位后同一跨度行数差 ~1.6 倍）。
  - **落点**：守卫挂在 `loadMinute` 入口而不是 `Bars` 的 switch 分支 —— 1m 与 15m 都经这个函数取分钟行（15m 再交 `Aggregate`），一处结构性覆盖 ③ 要求的两条路径，不依赖"记得两处都挂"。1d 路径不受约束（④），并有正反两面测试钉住挂错位置会红。报错点名已支持上限 + 当前请求跨度 + 下一步动作（切片），拒绝而非静默截断；天数从常量导出，不写字面量（否则改了常量后报错里两个数各说各话）。
  - **测试（先红后绿）**：`TestRouter_IntradaySpanCapFailsLoud`（1m/15m 各一子例，`nil pool` 兼作「是否走到查库」的探针 —— 守卫若在查库之后就会 panic 而非返回 error）、`TestRouter_IntradayCapIsMeaningful`（**绝对**跨度）、`TestCheckIntradaySpan`（边界表：恰好等于放行 / 超一秒拒绝 / 零跨度 / 负跨度不由本守卫判）、`TestRouterDailyPathNotSpanCapped`（集成，自灌自清理，同一十年区间 1d 成功 + 1m 被拒）。起手 `undefined: maxIntradaySpan/checkIntradaySpan` 红，实现后 `pkg/marketdata` 整包 exit 0（含 D1 既有 `TestRouterSameRouteThreeFrequencies` 与遗留路径对拍腿）。
  - **破坏验证两腿 + 一个自曝的测试缺陷**：① 摘掉守卫挂载（`if false`）→ 2 条红：一条报「同一区间交给 1m 必须被拒」，一条 nil pool 被碰到（证明原本在查库前）。② 把上限调到**一百年** → 只有绝对跨度腿与「十年区间拒绝」红，而 `TestRouter_IntradaySpanCapFailsLoud` **照样绿** —— 因为它的输入是从 `maxIntradaySpan` 推导的，门柱跟着常量一起挪。这不是设计意图，是我第一版测试的缺陷：**常量推导的输入没资格断言绝对行为**，故补绝对跨度腿并在注释写明这条护栏的失效形状（⑤「把上限调大 → 新测试红」要的正是这一类）。两腿还原后全绿，`grep -rn MUTATION pkg cmd` 零残留。
  - **门禁**：`go build ./...` exit 0、`go vet ./pkg/marketdata` exit 0、`gofmt -l` 三个改动文件空输出、`go test ./pkg/marketdata -count=1` exit 0。诚实边界：`-race` 本机跑不了（无 gcc，CGO_ENABLED=0），本单是纯函数校验 + 单次查询，无新增并发面。
  - **⚠️ 未完成的一半（必须留字，别让后人以为已经限到底了）**：③ 写「超限 **400**」，但 `Router.Bars` 今天**零生产调用方**（全仓 grep：仅本包测试与 worktree 副本；`NewRouter` 无生产装配点），HTTP 面不存在，400 无处可映射。本单把判定放在它该在的层并 fail-loud 返回 error；**将来接入 HTTP/网关时，接线方要把这个 error 显式映射成 400**（建议同时导出哨兵供 `errors.Is`，届时再加，本单不为假想需求提前造 API 面）。这属接线，不属取数边界，不在 ⑥ 范围内顺手做。
  - **并发提醒**：本次提交只含 `pkg/marketdata/router.go` + 两个测试文件；同一工作树里他人的 `aggregate.go`（REV-04 在途）与尚未提交的整段 REV 登记文本明确排除。**本完成记录故意不随代码提交** —— 它落在尚未入库的 REV 段里，按主任 2026-10-15 的指示，`docs/TASKS.md` 由她本人入库，代提交会把别人的在途登记文本扫进本单的 commit。实质结论（② 量级纠正、400 缺口、护栏失效形状）已在 `0726e3f` 的 commit message 里，不依赖本段是否一起提交。
  - **自审修订（2026-10-15，`7e41246`，审 `0726e3f` 后追加）**：三处真问题，都在我自己笔下。
    1. **我写进 SPEC 与代码注释的量级是没量过的估算** —— 上一条 bullet 刚批评工单 ② 量级失真，我自己那句「5.9 万行 ≈ 8MB」同样没依据。实测补齐：58,320 行 × `unsafe.Sizeof(market.OHLCV)` = **144 B/行** ⇒ 切片本体 **8.4MB**（含 symbol 字符串背板约 9.3MB）。测量用临时测试文件跑完即删，`pkg/domain/market` 无残留。**改的不只是数字，是数字的来路**：给出算法，下一个人能重算而不是只能信我。
    2. **报错不可执行**：跨度 397 天打成 `9528h0m0s`，读不出「超了多少、该切几片」，而这条报错存在的全部意义就是让调用方当场决定切片粒度。改为两个数都附自然日换算（仍从常量/入参导出，不写字面量）。
    3. **我的测试注释自己算错了日数**：绝对腿写「≈ 425 自然日」，`AddDate(1,1,0)` 从 2024-01-02 起是 366（闰年）+ 31 = **397 天**。改正，并把该腿从「只断言有错」升级为断言文案含 `397.0 自然日` 与 `366 自然日` —— `"366"` 写死是刻意的：改上限必须在这里红一次，逼人重新确认这个数，而不是让断言跟着常量挪门柱（门柱漂移就是上面「破坏验证」那条自曝的失效形状）。
    - 复验：`go build ./...` / `go vet` / `gofmt -l` / `go test ./pkg/marketdata` 全 exit 0。生产行为面只有报错文案变化，阈值与判定边界一字未动。

### REV-02 · 公式型因子逐（因子 × 计算日）全市场重拉窗口数据 [P2] ✅ 完成（2026-10-16，SPEC 本体 `ca06c8b`）

- **① 目标**：公式型因子的窗口数据拉取从「每个因子的每个计算日各拉一次全市场」降为「同一数据多因子/多日共享」，或在 SPEC 与已知边界中诚实登记该性能特征及适用面。
- **② 上下文**（2026-10-15 审查发现）：`pkg/data/factor_formula.go:96-97` 的 `loadFactorSeries` 每个计算日调 `store.GetOHLCVForDateRange(ctx, start, date)`——该接口**无 symbol 过滤**（全市场），窗口 `[date-Lookback, date]`。于是：单日单因子（handler 按名调用路径）≈ 每次全市场扫一遍（5900 标的 × 21 日 ≈ 12 万行，秒级，可接受）；但 `ComputeAllFactors(ctx, date, parallel=true)`（`pkg/data/factor.go:392-418`）下，当日 3 个 leaf/formula 因子（momentum/value/quality）**并发各拉一次同一窗口**，历史回填 N 日即 N×M 次全市场扫描。D3 完成记录登记了「公式型不物化中间序列、按定义现算」的设计，但**全市场重拉的放大效应没有作为已知边界成文**。
- **③ 要求**：(a1) 批处理路径（`ComputeAllFactors` / 将来的回填入口）按「日区间 × InputFields 来源表」预取一次，多因子多日共享（请求级缓存或预聚合二选一，数值必须与现路径对拍一致）；(a2) 若本期不做共享，则在 SPEC 因子章节 + D3 已知边界写明「公式型因子现算的单日成本模型与批量场景限制」，避免后人把批量回填跑成灾难还以为代码坏。handler 单日按名路径保留现状即可，但要写进文档。
- **④ 约束与非目标**：不改任何因子的数学定义与现算数值（D3 红线）；不改 `FactorStore` 接口签名（`GetOHLCVForDateRange` 的无 symbol 形态是既有契约，加过滤方法另立）；不动代码型因子路径。
- **⑤ 验收**：若选 (a1)：对拍测试（共享预取 vs 逐因子重拉，同日同因子数值逐只相等）+ 查询次数基准（N 日 M 因子的全市场查询次数从 N×M 降为 ≈N）；若选 (a2)：SPEC/已知边界成文 + `docs/` 链接守卫绿。两种都要求 `pkg/data` 既有测试全绿。
- **⑥ 边界**：只做公式型因子的数据拉取效率/文档登记；代码型因子、因子落库批次大小、回测侧消费另立。
- **完成记录（2026-10-16，SPEC 本体 `ca06c8b`；本段正文实际由 `92cc6c7` 携带入库——非归属方本人 commit，归属说明见本段末条）**：选 **(a2) 文档登记**。三条理由：① **(a1) 本期没有共享对象** —— 见下面对 ② 的实测订正，当下每日真正打全市场区间扫描的因子只有 momentum 一个，请求级缓存无从共享；② (a1) 的验收要求「数值与现路径对拍一致」，落点 `pkg/data/factor*.go` 正被并行施工占用，对拍是对 D3 数值红线的独立行为变更，不该挤进本单夹带；③ 形状成文本来就是 ② 自己指出的缺口（「放大效应没有作为已知边界成文」），先堵认知面。**等「名册扩量」接线落地后 (a1) 才有兑现面，届时另立工单**（预取 + 对拍 + 查询次数基准一体做）——见下方自审修订：计算名册当下是编译期种子，登记线到计算注册表尚未接线，扩量的前提本身没成立。
  - **⚠️ 先订正本单 ② 的形状（不改写原文，只加指针）**：「3 个 leaf/formula 因子（momentum/value/quality）并发各拉一次**同一窗口**」在 main HEAD 不成立。`loadFactorSeries` 按 InputFields 来源分路：**只有 momentum（`[close]`）打 `GetOHLCVForDateRange` 全市场区间扫描**；value（`[pe]`）/ quality（`[roe]`）走 `GetFundamentalsSnapshot` 单日快照（非区间扫描，且 `stock_fundamentals` 现 0 行）；D5 后名册多了一个 `market_activity`（`[turnover_rate]`，扫 `market_activity` 区间，现 0 行，形态同构）。所以放大面当下 = **交易日数 × 1 次行情系全市场扫描**，不是 ×3。缺陷形状真实（逐因子逐日无共享、无 symbol 过滤），但量级比 ② 的估计小一截——后续按 ×3 估回填成本的人会被带偏。**量级现量（2026-10-16，本机库实测，非估算）**：momentum 窗口（Lookback=30 自然日）内 122,097 行 / 5,569 标的 × 22 交易日；`ohlcv_daily_qfq` 全库 distinct symbol 5,671。
  - **落点**：SPEC.md「因子定义 Schema（D3）」→「已知边界（D3 如实登记）」新增成本形状条（逐来源形状 + 实测数 + handler 单日按名路径保留现状的裁决 + 批量回填适用面），顺带把 universe 条的「三个公式型因子均不混源」订正为「种子名册单源、混源并集路径已实现」（D5 后名册为 4 个，且 D4 登记可扩）。
  - **验收**：`tools/check_doc_links.py` exit 0（扫描 13 个常青/活跃层 Markdown，含 SPEC.md）。⑤ 的「`pkg/data` 既有测试全绿」**未本机实测，如实登记**：主工作区 `pkg/data` 是并行 agent 的在途改动（新迁移 056 + `postgres.go` 已改未提交），跑 `go test ./pkg/data/` 会经 `migrate()` 把在途 DML 打到共享开发真库（DATA-09 的隔离修复在 `task/data-09-test-migration` 未并 main）；替代方案「`git archive main` 提纯副本跑」被本机 auto 权限闸门拦下。本单改动纯文档、零 Go 代码，无测试回归因果面；请入库方 / CI 在干净 main 上补跑这一腿。
  - **自审修订（2026-10-16，审 `ca06c8b` 正文后追加，修正 commit 另笔）**：两处断言在写时未核，本轮源码核对后一处被证伪、一处补了来路。
    1. **「批内因子数随登记数增长」在 main HEAD 不成立——被我自己的成本条带偏**。核对：计算名册 = 编译期种子（`NewFactorComputer` 只装 `BuiltinFactorDefinitions`，`pkg/data/factor.go:62`）；库 `factor_definitions` 表是种子向库的**投影**（`SyncFactorDefinitions`），反向读取 `LoadFactorDefinitions` 与 D4 登记线、D8 挖掘求值（自建 sandbox，不经 `loadFactorSeries`）**均零生产调用方**。故当下放大系数是 1（momentum）而非「随登记增长」——那是**接线后即出现**的放大面。SPEC 条改为条件句并把「接线工单必须连本形状一起读」写成前置义务，免得接线人看不到成本面。
    2. **「单次秒级」补来路**：本机 psql 全列端到端实测 **1.22s**（122,097 行，含传输不含 Go scan 开销），量级与工单口径一致；原先是我没量过就沿用 ② 的话，形状同 REV-01 自审第 1 条。
    3. 顺手收口：universe 条与落点句里「D4 登记可扩」同因 1 改为「扩量尚未接线的放大面」；「handler 单日按名路径 = 一次全市场扫描」订正为「行情系一次全市场扫描、财报系一次快照」（按名路径同走 `loadFactorSeries` 分来源）。修正已复跑 `tools/check_doc_links.py` exit 0。
  - **协作/归属说明（2026-10-16，实况非提案）**：① 修正 commit `e3b9e39` 是 pathspec 整文件提交，把 D5 三段**在途** SPEC 片段（实现矩阵第 9 行、破坏验证②第三用例名、AUD-42 护栏段）一并扫入——扫入当时 `daily_basic_integration_test.go` 尚未入库，文档短暂处于幻影引用态，`92cc6c7`（D5 方提交测试本体）后自愈；那三段归属 D5，被本单 commit 携带非本意，照 REV-04 先例披露不改写。② 我发现扫入后曾在工作树临时摘除那三段，随即被 `92cc6c7` 的整体提交吸收，最终态 SPEC/TASKS 工作树与 HEAD 一致、无残留。③ 本完成记录整段（含自审与本子条）正是随 `92cc6c7` 入的库，「由归属方本人入库」的台账纪律这次未兑现——不是我不想守，是并行 agent 的文件级 add 抢在了前面；内容完整、未动他人分毫，按主任 2026-10-14 裁决口径可接受，归属以本条为准。

### REV-03 · `RegistrationBook` 可变状态无并发保护 [P2] ✅ 完成（2026-10-15，commit `abf0af3`+`d891567`）

- **① 目标**：`RegistrationBook` 的并发安全语义显式化——要么加锁成为并发安全类型，要么在类型契约中明示「非并发安全 + 调用方串行化约束」，不留「看起来能用其实会 race」的中间态。
- **② 上下文**（2026-10-15 审查发现）：`pkg/data/factor_governance.go` 的 `RegistrationBook` 与 D3 的 `Registry` 不同——后者是**构造时定稿的不可变快照**（注释明示读侧无需加锁），前者是**可变账本**：`Register`（L82-116）与 `Rollback`（L121-147）直接写 `byNameHash` map / `ordered` / `audit` slice，无任何 mutex；`ResolveEffective`/`ResolvePinned`/`Registrations`/`Audit` 是读路径。当前**无生产调用方**（全仓 grep：仅 SPEC.md 文档引用与测试），故今天不会 race；但它是 D4 治理细则的唯一可执行形态，将来接入「因子登记/回滚」的 HTTP 或后台入口时，并发 Register/Rollback 就是 map 并发写（panic 或静默丢失登记与审计留痕）。`Registrations()`/`Audit()` 返回拷贝 ✓，`Rollback` 对 nil map 读安全 ✓——仅缺写侧保护与契约明示。
- **③ 要求**：二选一并写明理由——(a1) 加 `sync.RWMutex`（读多写少：Resolve*/Registrations/Audit 走 RLock，Register/Rollback 走 Lock），保持现有签名与语义不变；(a2) 保持无锁，但在类型注释与 SPEC 治理细则中明示「非并发安全，调用方负责串行化（单线程装配期或外部锁）」，照 D3 Registry 的不可变快照思路写清约束。无论哪种，都要有 `-race` 测试钉住所选语义。
- **④ 约束与非目标**：不改登记四样校验、回滚语义（新引用 miss / 历史引用 hit）与审计字段（D4 定稿）；不动 `pkg/storage/factor_registration.go` 的 DB 侧（其并发由调用方事务管理）；不引入外部治理系统。
- **⑤ 验收**：若选 (a1)：`go test -race` 下并发 Register/Resolve/Rollback 混合测试绿（含同因子双版本并发登记去重）；若选 (a2)：类型注释 + SPEC 成文 + 一条「文档明示非并发安全」的守卫测试（如注释存在性断言，照 internal/repoguard 先例）。两种都要求 D4 既有测试（`factor_governance_test.go` 演练 + `factor_registration_test.go` 表驱动）全绿。
- **⑥ 边界**：只做进程内账本的并发语义；DB 侧落库的并发/事务、HTTP 接入面另立。
- **完成记录（2026-10-15，`abf0af3` + `d891567`）**：选 **(a1) 加 `sync.RWMutex`**——理由：本账本是**可变类型**，将来接 HTTP/后台「登记 / 回滚」入口即并发写；加锁让类型自成并发安全类型，契合本项目「并发访问共享资源须正确加锁」的约定，且保持签名与治理语义不变（比 (a2) 只写文档更彻底地消除 race 隐患）。实现在 [factor_governance.go](../pkg/data/factor_governance.go)：`RegistrationBook` 增 `mu sync.RWMutex`，写路径 `Register`/`Rollback` 持写锁（整方法为一个写事务：校验 + 去重 + 落账 + 留痕原子完成），读路径 `ResolveEffective`/`ResolvePinned`/`Registrations`/`Audit` 持读锁；类型注释明示并发语义并注明「因含锁不可按值拷贝」（构造入口均返回指针，无值拷贝调用点）。新增 `TestRegistrationBook_ConcurrentRegisterResolveRollback`（[factor_governance_test.go](../pkg/data/factor_governance_test.go)）：64×2 并发登记 v1/v2 + 8 读 goroutine 全程交叠 + 64 并发回滚；去重封闭——同因子双版本并发登记恰好 1 成功、其余 `ErrRegistrationDuplicate`，并发回滚恰好 1 成功，最终审计恰 3 条（register×2 + rollback×1）。**验证**：本机无 gcc（`-race` 需 cgo + gcc），`-race` 腿改在 `golang:1.25-alpine` 容器内跑（容器内 `apk add gcc musl-dev`）——`go test -race ./pkg/data/ -run TestRegistrationBook` 5 例全绿；**破坏验证**：去掉全部 `Lock`/`RLock` → `-race` 报多处 `WARNING: DATA RACE`（红），还原 → 绿。`gofmt -l` 空、`go build ./...`、`go vet ./pkg/data/`、`go test ./internal/...`（repoguard）干净；D4 既有测试（`pkg/data` 演练 4 例 + `pkg/domain` 表驱动）全绿。SPEC 同步：D4 治理细则新增「### 并发语义（进程内账本是并发安全类型，REV-03）」小节 + 护栏清单第 5 条。**自审补丁（`d891567`）**：复审本次改动时发现测试自身有缺陷——阶段 1 的 `t.Fatalf` 若提前退出，8 个读 goroutine 会一直忙等到整个测试二进制退出，可能拖慢/干扰同批其它测试；改为启动读 goroutine 后立即注册 `t.Cleanup`（`close(stopRead)` + `readers.Wait()`）收束，成功与失败两条路径都干净，忙等窗口顺带覆盖到最终态断言（`-race` 下读写交叠更充分），并重跑容器 `-race` 复验 5 例全绿。**诚实缺口**：`-race` 未在本机原生跑（无 gcc），以容器替代；建议 CI 原生 `-race` 复跑兜底。

### REV-04 · `checkInSession` 分钟粒度判定丢失秒级边界 + Tushare 1m 的 11:30 bar 形态未登记 [P3] ✅ 完成（2026-10-15，commit `a3730bb`+`b4282e6`）

- **① 目标**：会话校验的时间边界要么做到秒级精确，要么把「分钟粒度」假设与已知数据形态不兼容面写成已知边界，不让「11:30 整点 bar 会被当午休拒收」这类事实靠读代码的人自己悟。
- **② 上下文**（2026-10-15 审查发现，两处关联同一函数族）：`pkg/marketdata/aggregate.go:191` 的 `checkInSession` 用 `hm := t.Hour()*60 + t.Minute()` 判定——**秒被丢弃**，于是 15:00:01–15:00:59 的 bar 与 15:00:00 收盘打点同判「合法」并入末 bucket（注释只承诺「含 15:00:00 收盘打点」）。标准分钟 bar 秒恒 0，此路径实际不可达，但带秒的非标数据会静默改变口径边界。**同族未登记事实**：A 股分钟数据普遍含 **11:30 整点 bar**（Tushare 1m 即有），而 `checkInSession` 把 11:30:00 判为「午休」（`hm < afternoonOpenMin` 分支）⇒ 整条 `Aggregate` fail-loud。这是「不静默丢 bar」设计决策的**兼容面**：mootdx 是当前唯一 1m 源且 `ohlcv_minute` 0 行，一旦按 D6 灌入 Tushare 1m，15m 聚合会以「午休」报错拒收——现象像数据脏，实为形态不兼容。
- **③ 要求**：(a1) 秒级订正——hm 判定带秒（如 `15:00:00` 整接受、`>15:00:00` 拒），并补测试；(a2) 11:30 兼容性二选一：在 SPEC「规范层 Schema」已知边界成文「Tushare 1m 的 11:30 收盘 bar 当前被会话校验拒收，15m 聚合前须在同步/normalize 边界处理」，或在 aggregate 入站处显式接纳 11:30 整点并入 11:15 bucket（开口子必须写清理由与测试）。拒绝语义（不静默丢）不变。
- **④ 约束与非目标**：不改聚合主口径与三细则（D1 定稿）；不动路由层；不顺手改 15:00 之后数据的其它语义；不为本单灌任何 1m 数据。
- **⑤ 验收**：(a1) 有「15:00:30 bar → 拒收」测试钉住秒级语义；(a2)  whichever 选择都有对应测试（拒收且报错点名 / 接纳且并入正确 bucket）+ SPEC 或代码注释成文。`pkg/marketdata` 既有测试全绿；**破坏验证**：把秒级判断改回分钟粒度（或删 11:30 处理）→ 新测试红，还原转绿。
- **⑥ 边界**：只做会话校验的边界精度与已知边界成文；分钟数据灌数（D6 前置）、聚合口径另立。
- **✅ 完成记录（2026-10-15）**：两处一并落地，改动集中在 `pkg/marketdata/aggregate.go`（+测试 + SPEC 成文），未动路由层与其他 agent 并行施工的文件。**(a1) 秒级订正**：`checkInSession` 边界按秒判定——11:30:00 / 15:00:00 整点收盘打点合法；11:30:01+ / 15:00:01+（含 15:00:00.000000001 亚秒越界）拒收。分钟粒度 `hm` 判定会把 15:00:30 与 15:00:00 混同、静默放宽口径边界；标准分钟 bar 秒恒 0 使该路径实际不可达，但非标数据不该靠「恰好没出现」活着。**(a2) 选接纳方案**：`bucketStart15m` 增 `hm == morningCloseMin` 分支，11:30:00 并入 11:15 bucket。理由：① 与 15:00:00 并入 14:45 bucket 对称——下午收盘打点接纳、上午收盘打点拒收没有口径理由；② D1 bucket 定义「11:15 bucket 止于 11:30」本就含边界包含语义，代码此前没实现；③ Tushare 1m 普遍含 11:30 整点 bar（mootdx 导出不带），入站处兼容两种分钟形态，免得 D6 灌数后把形态不兼容伪装成「数据脏」报错，也免掉同步侧 normalize 层。拒绝语义不变：11:30:01+ / 15:00:01+ 仍 fail-loud，不静默丢 bar。**测试**：既有 `TestBucketStart15m_SessionBoundaries` 的「11:30 午休打点拒绝」用例按新语义订正为「11:30:00 并入 11:15 bucket」，并补 11:30:01 / 15:00:30 / 15:00:00 亚秒越界三条边界；新增 `TestAggregate_SessionBoundarySecondPrecision`（11:30:00 与 11:15/11:29 合并的数值手算对拍 + 15:00:30 / 11:30:01 fail-loud 且报错点名）。**破坏验证**：把秒级判断改回分钟粒度（`hm <= afternoonCloseMin` 且 11:30:00 归午休分支）→ 新测试红（11:30:00 被拒、15:00:30 被误纳），还原转绿；`git diff` 确认无破坏残留。**验证**：`go build ./...` / `go vet ./...` 全绿；`gofmt -l pkg/marketdata/` 干净（整仓 `gofmt -l .` 另有 **11 个 HEAD 既有脏文件**——`cmd/data/setup.go`、`internal/repoguard/ai_boundary_test.go`、`internal/repoguard/package_wiring_test.go`、`pkg/backtest/reporting/compare_test.go`、`pkg/bootstrap/auth_test.go`、`pkg/bootstrap/server.go`、`pkg/kernel/adapters.go`、`pkg/msgbus/bus_pg_test.go`、`pkg/strategy/checkpoint.go`、`pkg/strategy/signal_provider_test.go` 等，系 2026-10-08/09 提交混入、AUD-27「可当结论用」已失效，另立工单，不在本单）；`go test ./pkg/marketdata/... ./pkg/domain/... ./internal/repoguard/... -count=1` 全绿（spec_drift 与护栏同批过）。本机无 C 工具链（`-race` 需 cgo/gcc，不可用），race 门禁留 CI；本单改动为无并发纯函数，不涉及数据竞争面。**文档**：SPEC.md 聚合口径细则 2 补上午收盘打点对称并入、硬规则补秒级精度条、D1 已知边界登记两种分钟源 11:30 形态差异。**协作说明**：① 本单测试改动被并行施工 REV-01 的 `0726e3f` 按文件名 add 带入，修复本体由 `a3730bb` 补齐，两者合计 HEAD 全绿；REV-01 方则在其完成记录中明确排除本单在途文件（`aggregate.go` 与未入库的 REV 登记段），纪律可鉴。② **反向披露**：`a3730bb` 以文件级 `git add docs/SPEC.md` 时，把 REV-01 未提交的「日内跨度边界」SPEC 段一并扫入——内容经核对与其 `0726e3f`/`7e41246` 的实现一致（366 自然日 = `maxIntradaySpan`），非错误内容，但归属在本单 message 之外，在此披露；因 `a3730bb` 已被本登记段引用，不改写历史。③ REV-03 经 `task/rev03-registration-book-lock` 合入 main（7071aa8）；REV-05 代码 `22ba02e`+`d9aeeb6` 已提交；REV 登记段由台账归属方以 `d8e010b` 入库，本单 ✅ 随之落地。④ 自审修复 `b4282e6` 落在 `task/d6-multi-frequency-strategy` 分支——施工期间另一 agent 将本工作树切至 D6 分支，提交随该分支合并回 main（main 侧 REV-04 功能本体 `a3730bb` 已完整，`b4282e6` 为报错格式/注释/测试补钉，无功能缺口）。

**自审修复（2026-10-15，`b4282e6`）**：对本人更改的复审发现并修复四项——① 两处新增边界拒收（11:30:00 带秒 / 15:00:00 带秒及亚秒）的报错格式 RFC3339→**RFC3339Nano**：亚秒越界（15:00:00.000000001）在 RFC3339 下会显示成 `15:00:00`，「晚于 15:00 收盘」与时间字面自相矛盾，排查反被误导；RFC3339Nano 仅在有小数位时打印，整分/整秒 bar 输出与原来逐字相同。补测试钉住（报错含 "15:00:00.000000001" 字样）；破坏验证「改回 RFC3339 → 新用例红 → 还原 → 绿」通过。② 注释订正：「13:00–14:59 下午连续竞价」补「收盘集合竞价（14:57 起）」；文件头块补亚秒越界表述，与函数注释一致。③ 测试注释笔误（`helper  surface`→`helper surface`）。④ 本记录「验证」条如实订正（原「`gofmt -l .` 干净」不实，整仓 11 个 HEAD 既有脏文件，见上）。

### REV-05 · `Registry.Fetch` 不防御 adapter 返回 `(nil, nil)` [P3] ✅ 完成（2026-10-15，commit `22ba02e`+`d9aeeb6`）

- **① 目标**：`Fetch` 对「adapter 返回 `(nil, nil)`」这一契约违背有显式处理，不再让 nil error 穿过成功检查、最终把 nil 包进 `%w` 产出 `%!w(<nil>)` 的损坏诊断。
- **② 上下文**（2026-10-15 审查发现）：`pkg/data/source/registry.go:209-228` 的成功判定是 `if err == nil && resp != nil`——adapter 返回 `(nil, nil)` 时该分支不进，但下面 `if !IsRetryable(err)` 也不进（err 为 nil），于是循环继续、`lastErr = nil`；若链上所有 adapter 都如此，函数末尾 `fmt.Errorf("registry: all adapters exhausted for %s (tried %v): %w", ..., lastErr)` 把一个 **nil error** 包进 `%w`，错误文本变成 `%!w(<nil>)`——「所有源耗尽」的真实诊断被损坏。虽然现有 10 个 adapter 无一返回 (nil, nil)，但这是接口契约漏洞：`DataSourceAdapter.Fetch` 的文档没有钉死「err==nil 时 resp 必非 nil」。
- **③ 要求**：`err == nil && resp == nil` 时按该 adapter 失败处理——记入 `tried`，`lastErr` 置为显式哨兵（如 `fmt.Errorf("adapter %q returned (nil, nil) response", name)`），继续走链；链耗尽时报错信息可读、不含 `%!w`。同时在该 adapter 接口注释或 D2 契约文档补一句「err==nil ⇒ resp 非 nil」的调用方契约。
- **④ 约束与非目标**：不改 fallback 链语义（retryable 跳过 / 非 retryable 立即返回）；不动三类拒收与健康检查；不给现有 adapter 的行为找补（它们本来就不返回 nil, nil）。
- **⑤ 验收**：单测——mock adapter 返回 `(nil, nil)`，断言 (1) 不 panic、(2) 链继续尝试后续 adapter、(3) 全部如此时最终错误文本含 "(nil, nil)" 字样且不含 `%!w`；`pkg/data/source` 既有测试（含 D2 契约测试）全绿。**破坏验证**：把 (nil,nil) 防御删掉 → 新测试红，还原转绿。
- **⑥ 边界**：只做 Fetch 的 nil 响应防御与契约成文；adapter 实现质量、kill-switch 行为另立。
- **✅ 完成记录（2026-10-15，`22ba02e` + 自审 `d9aeeb6`）**：③ 全部落地。`registry.go` 在成功检查之后新增 `err == nil && resp == nil` 分支：记入 `tried`、`lastErr` 置为点名该 adapter 的哨兵 `fmt.Errorf("adapter %q returned (nil, nil) response", name)`、`continue` 走链（不再落入 `!IsRetryable(err)` 那条把 (nil,nil) 当非可重试直接 return 的旧路径——`IsRetryable(nil)==false`，旧路径会让 Fetch 自身把 nil 响应 + nil error 交回调用方）。链耗尽时末尾 `%w` 包的是非 nil 哨兵，报错含 "(nil, nil)" 且无 `%!w(<nil>)`。`adapter.go` 的 `DataSourceAdapter.Fetch` 接口注释补「err==nil ⇒ resp 非 nil」调用方契约。`registry_rev05_test.go` 新增：(nil,nil) 源落链后健康源胜出；全链 (nil,nil) 时报错可读、含 "(nil, nil)"、不含 `%!w`。**破坏验证**：反转防御条件 → 两条测试转红（commit 自述已实测）。**自审补丁 `d9aeeb6`**：(nil,nil) 日志补回本文件既有 `failed:` token（避免运维按 failed 抓日志漏掉）；测试替身 `nilResponseAdapter` 从裸 int 计数改为 mutex + `callCount()`，对齐 `DataSourceAdapter`「实现须并发安全」的自身声明。**门禁**：commit 自述 `go build ./pkg/...` / `go vet ./pkg/data/source/...` / `gofmt -l` / `go test ./pkg/data/source` 干净。
- **⚠️ 状态订正说明（本条由台账审查补记，2026-10-15）**：本段登记时其代码已随 `22ba02e`+`d9aeeb6` 入库，但标题误留 `⬜ 待做`、缺完成记录 —— 与 REV-01/03/04「✅ + 完成记录随台账一并落地」的先例不一致（那三条登记时码已在途或未在途都标注清楚了，本条是唯一漏改的）。已核对 `pkg/data/source/registry.go` 现存防御分支 + `adapter.go` 契约注释与 ③ 逐条相符，订正为 ✅；完成记录严格取自这两个 commit 的内容与当前源码，未补任何未经核实的细节。

---

## 三、跨仓指针（不在本仓，勿双写）

| 项 | 归属 | 说明 |
|---|---|---|
| **AUD-61 · E2E 测试对齐**（含阶段一前置：61 条 `test.fixme` 登记 + 取红清单） | quant-trading-ui 仓 `docs/TASKS.md` | 2026-10-14 自 core 迁出（迁移指针见归档卷）。⚠️ **阶一已从「测试债」升格为 ODR-067 主干工作流前置**（61 failed/116 passed 全量 e2e 现状，不做则直推 main 红灯与历史解耦），启动时点与 DOCS-01 守卫落地协同 |
| **NT v2.0.0 正式版升级** | quant-trading-docs 仓 `docs/guides/nt-sidecar-operations.md` 预案 | 唯一外部等待项：正式版发布后按预案收口，**双跑对齐回归为升级验收门**（NT 决策门裁决一） |
| UI 仓 Stitch 设计稿相关（A-05 等） | quant-trading-ui 仓 `docs/design/stitch_quantlab_ux_redesign/TASKS.md` | 本地栈实测证据全文在其 A-05 节，core 侧只留指针（归档卷 §八） |
| **A-08 · 数据同步页（作业队列 + 排程 CRUD + worker/scheduler 统计）** | quant-trading-ui 仓 `docs/design/stitch_quantlab_ux_redesign/TASKS.md` A-08 节（六字段实施计划） | 前置 SYNC-01 已 ✅、**SYNC-04 已 ✅（2026-10-16，合入 main `33f845f`，data-service 已重建）**（均见本文件 §二·补）。原「`scheduler/stats` 的 `0/0` 两种成因不可区分」缺口已在后端关闭并上线：stats 现带 `is_running`/`start_error`（契约与三态判读见 `docs/SPEC.md` §sync 端点块），2026-10-16 活进程经网关 `:8085` 实测读数 `{"active_schedules":0,"total_entries":0,"is_running":true}`，前端可据此区分 |

---

## 维护本文件

- 新增 task 必须按 [AGENTS.md §8.5](../AGENTS.md) 写全六字段（目标/上下文/要求/约束与非目标/验收/边界）。
- 完成即标 ✅（附日期）并移入 `archive/`；本文件只留未完成项。
- 大任务开工前，若六字段不够细，先补细再派工——**独立可运行判据达不到 = task 没写好，不是 agent 的问题**。
- **D 系列文档同步纪律**：上层设计变更必须同步 SPEC / data flow / schema / ARCHITECTURE.md（ADR-031 决议变更记录固化）。
- **新需求优先于存量未完成任务**；与旧任务重叠的可合并或直接删除，跨仓迁移的任务条目保留可追溯指针（不直接删原条目）。
