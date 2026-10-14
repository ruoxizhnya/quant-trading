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
- **✅ 完成记录（2026-10-14）**：契约用「接口加必选方法 + 注册期校验」落地，改动全在 `pkg/data/source` 与 `pkg/domain/market`（`cmd/data` 一行未改，与 D3/D4 已提交文件零重叠）。**语义词表**（唯一真相）：`pkg/domain/market` 新增 `TimeSemantics`（`announcement_day`/`after_close`/`next_open`/`custom`）与 `FrequencySpec`（`daily`/`intraday`/`event`）+ `Parse*` fail-loud + `Known*` 拷贝；零值 `""` = 未声明（不算合法）。**契约落点** `pkg/data/source/contract.go`：`ConnectorDeclaration`（`DataType`/`TimeSemantics`/`VisibleExpr`/`VisibleField`/`Frequency`）+ 保留锚点 `VisibleFieldTradeTime`（OHLCV 类锚点在 `DataItem.TradeTime`，不在 schema 字段）+ 三类哨兵 `ErrDeclarationMissing`/`ErrDeclarationInvalid`/`ErrDeclarationMismatch`；`DataSourceAdapter` 接口加**必选方法** `Declarations()`（新 adapter 漏声明 = 编译错误）。**拒收闸门**：`Registry.Register` 在改状态**之前**调 `validateDeclarations` 校验（未声明 / 枚举非法 / custom 缺表达式 / 重复声明 / 声明了未支持类型 / 可见锚点不在 schema 或类型非 date·datetime），被拒 adapter 不污染 Registry。**存量补齐**：10 个 adapter 的声明集中在 `declarations.go`「声明即登记」并逐条写实（tushare 日线=收盘后可见、fundamentals=公告日事件流；mootdx 分钟=周期结束自定义、实时=观察即可见；宏观/公告/新闻=公告日·发布事件流；eastmoney 系日更=收盘后可见；板块/热搜快照=观察即可见）。**测试** `contract_test.go`：三类拒收逐条负例 9 条 + 注册原子性 + 全部存量 connector 声明完整（`SupportedTypes` ⇄ 声明一一对应）且整批注册成功 + 错误点名。**破坏验证三腿均机制复现（改坏→红→还原→绿）**：① 未声明 → `ErrDeclarationMissing` 点名 adapter/数据类型；② 删 tushare 财报声明 → `TestAllExistingConnectorsDeclareEverySupportedType` 红（报 `supports "fundamentals" but has no declaration`）；③ `alpha_vantage` 的 `after_close` 改成词表外 `intraday_visible` → 同测试红（报非法枚举）。**验证**：`go build ./...` 全绿、`go vet ./...` 全绿、`pkg/data/source` + `pkg/domain/...` + `internal/repoguard` 测试全绿。**文档**：SPEC.md 新增「connector 注册契约」节（含高危样例 活跃度/拥挤度=`after_close` + 存量声明一览 + 三类拒收表 + 破坏验证）+ ARCHITECTURE.md §7 数据流段增补「上游闸门」。**已知边界如实登记**：① mootdx 分钟表 `amount` NULL→`Turnover` 记 0 在契约文档收编（不改 ETL 行为，D1 已点名）；② 产业链 fetcher（`pkg/data/supply_chain.go`）不走 Registry，其时间语义成文登记为已知边界，建 adapter 属 D5。**无关红登记**：`pkg/data` 的 `tushare_adjustment_integration_test.go`（实盘数据口径断言）当前红，与本单无关——实证 `pkg/data` 不依赖 `pkg/domain/market` / `pkg/data/source`（无引用），属实盘数据漂移，另议。

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

### D5 · 新数据源 connector 样板：一个源 + 一个公式因子 ⬜ 待做（依赖 D2、D3）

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

### D6 · 多频率进回测策略视野 ⬜ 待做（依赖 D1、D2）

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

### D8 · AI 因子挖掘流水线 + 治理层 ⬜ 待做（依赖 D3、D4；关键路径终点）

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

### DATA-05 · 成交量/成交额口径收尾（fallback 源 + 成交额 + 空表） ⬜ 待做

- **① 目标**：把 DATA-04 未覆盖的**同类口径欠账**一次清掉——其余数据源与其余表的 volume/turnover 单位全部明确，并统一到规范层已声明的口径。
- **② 上下文**（2026-10-14 DATA-04 审查时登记，均为未验证项）：
  - **fallback 源未验**：日线链是 `tushare → mootdx → eastmoney`（`cmd/data/registry_init.go` 的 `DataTypeOHLCDaily`）。tushare 已在 DATA-04 修好（×`sharesPerLot`）；`mootdx` / `eastmoney` 的 vol 单位**在项目内无实测依据**（TDX 惯例是「手」），DATA-04 **刻意未猜着改**。而 `pkg/data/source/mootdx_adapter.go` 的 schema 已写 `Unit: "share"` —— 又一处「声明 ≠ 实现」的候选（DATA-04 的根因同型）。
  - **成交额 `turnover` 仍错位**：`docs/SPEC.md` 规范层声明 Turnover 单位 **CNY**，而 tushare `daily` 的 `amount` 单位是**千元**，`normalizeDailyOHLCV` 原样入库（`pkg/data/tushare.go`）。前端 `fmtAmount`（`web/src/utils/format.ts`）按元格式化 ⇒ 显示值偏小 1000 倍。
  - **三张表 0 行、单位未定**：`ohlcv_minute.volume`(bigint) / `realtime_quote.volume`(bigint) / `global_ohlcv.volume`(float8) 均无数据。`docs/SPEC.md` 规范层 Schema 对 OHLCV 通用声明了「股」，但分钟线那句「原样返回（volume 由表内 BIGINT 承载）」没点明单位，留给读者猜。
- **③ 要求**：每条**先实测再改**，不许猜——mootdx/eastmoney 取一次真实响应核对 vol 量级（与同 symbol 日线对比）；`turnover` 定死单位后统一「同步侧 + SPEC + 前端格式化」三处；三张空表的单位在 SPEC 规范层 Schema 里写明（哪怕暂时 0 行）。
- **④ 约束与非目标**：不重复 DATA-04 已完成的日线 volume 部分；不引入第二套口径；`turnover` 变更会动因子与展示数值，须照 DATA-04 做「改坏→红→还原→绿」破坏验证。
- **⑤ 验收**：每个源/表给出「实测单位 + 换算点 + 落库后抽样比对」三件证据；SPEC 规范层 Schema 的每个数值字段都能回答「单位是什么」；`cmd/dualrun -gate` 回归绿。
- **⑥ 边界**：只做单位口径；源本身的接入与可靠性问题不在本单。

### DATA-06 · 指数成分股同步：落库失败只 Warn，端点照样回 "synced successfully" ⬜ 待做

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

---

## 三、跨仓指针（不在本仓，勿双写）

| 项 | 归属 | 说明 |
|---|---|---|
| **AUD-61 · E2E 测试对齐**（含阶段一前置：61 条 `test.fixme` 登记 + 取红清单） | quant-trading-ui 仓 `docs/TASKS.md` | 2026-10-14 自 core 迁出（迁移指针见归档卷）。⚠️ **阶一已从「测试债」升格为 ODR-067 主干工作流前置**（61 failed/116 passed 全量 e2e 现状，不做则直推 main 红灯与历史解耦），启动时点与 DOCS-01 守卫落地协同 |
| **NT v2.0.0 正式版升级** | quant-trading-docs 仓 `docs/guides/nt-sidecar-operations.md` 预案 | 唯一外部等待项：正式版发布后按预案收口，**双跑对齐回归为升级验收门**（NT 决策门裁决一） |
| UI 仓 Stitch 设计稿相关（A-05 等） | quant-trading-ui 仓 `docs/design/stitch_quantlab_ux_redesign/TASKS.md` | 本地栈实测证据全文在其 A-05 节，core 侧只留指针（归档卷 §八） |
| **A-08 · 数据同步页（作业队列 + 排程 CRUD + worker/scheduler 统计）** | quant-trading-ui 仓 `docs/design/stitch_quantlab_ux_redesign/TASKS.md` A-08 节（六字段实施计划） | 前置 SYNC-01 已 ✅（本文件 §二·补）。⚠️ **A-08 已知诚实缺口，勿在 UI 上假装补上**：`scheduler/stats` 的 `0/0` 有两种成因（调度器启动失败 vs 确实没有 active 排程），后端目前不区分，前端只能显示 0/0——真解要后端把启动失败做成可观测状态（另立，勿在 A-08 里编个「未启用」文案冒充诊断） |

---

## 维护本文件

- 新增 task 必须按 [AGENTS.md §8.5](../AGENTS.md) 写全六字段（目标/上下文/要求/约束与非目标/验收/边界）。
- 完成即标 ✅（附日期）并移入 `archive/`；本文件只留未完成项。
- 大任务开工前，若六字段不够细，先补细再派工——**独立可运行判据达不到 = task 没写好，不是 agent 的问题**。
- **D 系列文档同步纪律**：上层设计变更必须同步 SPEC / data flow / schema / ARCHITECTURE.md（ADR-031 决议变更记录固化）。
- **新需求优先于存量未完成任务**；与旧任务重叠的可合并或直接删除，跨仓迁移的任务条目保留可追溯指针（不直接删原条目）。
