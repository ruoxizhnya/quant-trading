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

### D1 · 规范层 schema + 按内核翻译层 + 聚合规则成文 ⬜ 待做（最高优先，无依赖）

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

### D2 · connector 注册契约：时间语义 + 频率口径，fail-closed 拒收 ⬜ 待做（与 D1 同一切片包）

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

### D3 · 公式化因子引擎：定义入库 + 11 因子公式化 ⬜ 待做（依赖 D1 schema 字段定稿）

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

### D4 · 因子注册表治理细则 ⬜ 待做（依赖 D3）

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

### D7 · NT 原生模式（国外线）⬜ 待做（依赖 D1，可与 D2–D6 并行）

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

### DATA-01 · 票池三表灌数 + universe 指数展开链路修复 ⬜ 待做

- **① 目标**：`index_constituents` / `sectors` / `stock_sector_map` 三表有真实数据，且探索链路的 `universe: csi300` 能展开成真实票池（`explore` 链路不再恒空票池）。
- **② 上下文**：归档卷 §八复查记录（2026-10-14 实测）：三表各 0 行 ⇒ YAML 默认 `universe: csi300` 解析为空票池；且 core 仓 `pkg/backtest/engine.go:556` `resolveStockPool` 只在 `len(StockPool)==0 && req.IndexCode != ""` 时展开指数，而探索链路（**quant-trading-agent 仓** `pkg/ai/intent/parser.go:117` → `pkg/ai/yaml/generator.go:198` → `pipeline.go:868 parseUniverse` → `cmd/ai/builders.go:47`）把字面量 `"csi300"` 放进 `StockPool`、**从不填 `IndexCode`** ⇒ 展开分支结构上不可达，`"csi300"` 被当成一只股票代码（core 仓 `engine.go:1125` Debug 级丢弃）。**⚠️ 展开链路修复横跨 core（engine）与 quant-trading-agent（parseUniverse/builders）两仓**，跨仓原子提交 core 先行。**光灌数不够，链路也要修**（OBS-01 验收增量）。同步链路存在：`cmd/data/handlers_sync.go` + `pkg/data/tushare.go`（`index_constituents` 相关）。
- **③ 要求**：
  1. **灌数**：三表经既有同步链路补真实数据（真 token 实测，沿用 P2-1/P2-2 真库核对惯例）；`stock_sector_map` 灌上后 `sector` 字段可用性自动转真（OBS-08 可用性声明是运行时判定，勿硬编码）。
  2. **链路修复**：探索链路的 `universe` 识别指数代码并走 `IndexCode` 展开分支（`"csi300"` 不再进 `StockPool`）；真票池规模进入 `PoolSize`（与 OBS-01-R1 的「不打印未测量数字」配套：修复后 0 才是测量值）。
  3. fail-loud 保持：真展开仍为空 → 维持 invalid 报法（OBS-01 语义不变）。
- **④ 约束与非目标**：不改 `CheckValidity` 语义（OBS-01 已完成面不动）；不趁机改验证器；灌数走既有同步链路不写一次性脚本进库。
- **⑤ 验收**：真库三表行数 > 0（查询留证）；`universe: csi300` 走 `IndexCode` 的单测 + 端到端探索 run 票池非空（留 run id）；**破坏验证**：清空 `index_constituents` 一行 → 展开为空 fail-loud 变红路径可复现，还原绿。
- **⑥ 边界**：只做三表数据 + universe 展开链路；其他数据缺口（`stock_fundamentals` 0 行等）另立。

### OBS-01-R1 · 偏差维输出一个它没量过的数字 ⬜ 待做

- **① 目标**：验证器偏差维在池子规模未测量时**不打印数量**（「0 只」不再冒充实测值），屏幕上每个数字都可被当场质问。
- **② 上下文**：归档卷 §八工单表 #7（2026-10-14 立单，**代码在 quant-trading-agent 仓**）：`cmd/ai/handlers_explore.go:50` 设 `PoolSource=PoolSourceCurrent` 但从不设 `PoolSize`（只有 `:96` 预热成功路径才填）⇒ `pkg/ai/validation/bias.go:296-303` 无条件把 `in.PoolSize`（Go 零值 0）插进 blocking 文案 ⇒「股票池按今天的上市名单取（**0 只**）」。**0 是零值不是测量结果**（愿景原则 4：每个数字可被当场质问）。`PoolSize<=0` 的「未测量」分支已有先例（`bias.go:202` 与 `:314`），只有 `PoolSourceCurrent` 这支漏了。**概率侧没有 bug**：`biasProbability` 对 `PoolSourceCurrent` 提前返回 `BiasProbFloor`，低分有意为之，错的只有那句话。
- **③ 要求**：`PoolSize<=0` 时不带数量措辞（对齐 `bias.go:202`/`:314` 既有「未测量」分支先例）；文案改动最小化。
- **④ 约束与非目标**：**不改 `biasProbability` 与 `PoolSourceCurrent` 的概率**；不借机重构偏差维；不动其他维。
- **⑤ 验收**：单测：`PoolSize=0`（未测量）输出无数量词、`PoolSize>0` 输出带真实数量；**破坏验证**：改回无条件打印 → 新测试红，还原绿。正常 run 输出文案回归不变（有测量值场景）。
- **⑥ 边界**：只改这一处措辞分支。

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

### SYNC-02 · `StopWorkerPool` 零调用方：关闭不排空在跑的作业 ⬜ 待做

- **① 目标**：SIGTERM 后 worker pool 优雅停止，正在跑的同步作业要么跑完要么被标记为可重试，而不是随进程一起被掐断。
- **② 上下文**：SYNC-01 复测时顺带 grep——`cmd/data/sync_handlers.go:75 StopWorkerPool()` 全仓零调用方（`StartWorkerPool` 有，`main.go:138`）。**与 SYNC-01 同族缺陷形状**（生命周期入口存在但没人调），差别只在后果更窄：只影响关闭时的在跑作业，不影响是否触发。
- **③ 要求**：在 `main()` 关闭序列里接上（顺序须与 SYNC-01 一致：HTTP → scheduler → worker pool）；`WorkerPool.Stop()` 语义若不够（不等待/不标记），先测出来再决定改不改，别在接线单里顺手改语义。
- **④ 约束与非目标**：不改 `WorkerPool` 公开签名；不动限流/队列参数；不碰 scheduler 逻辑。
- **⑤ 验收**：接线守卫新增一条断言（`main.go` 含 `syncHandler.StopWorkerPool(`，锚定调用形态）；**破坏验证**：去掉该调用 → 守卫红，还原绿；有条件时用一次真实「跑到一半发 SIGTERM」观察作业终态。
- **⑥ 边界**：只做关闭接线 + 对应守卫；作业中断后的状态收敛另立。

### SYNC-03 · 停用排程后 `next_run_at` 残留（界面会显示一个不会发生的未来） ⬜ 待做

- **① 目标**：`is_active=false` 的排程不带 `next_run_at`，界面无法把「从未会发生的下次运行」当成事实展示。
- **② 上下文**：SYNC-01 活进程实测（2026-10-14）：`POST /api/sync/schedules/1/toggle {"active":true}` 后 `next_run_at=2026-10-15T09:00:00Z`；再 toggle `false`，`scheduler/stats` 回到 `0/0`（cron entry 已 `Remove`），但 `GET /api/sync/schedules/1` 仍返回 `is_active:false` **且** `next_run_at:2026-10-15T09:00:00Z`。写点在 `pkg/sync/scheduler.go:278`（`scheduleJob` 设置并持久化），停用分支只 `cron.Remove` + `delete(s.entries)`，未清字段。愿景原则 4「每个数字可被当场质问」在这里答不上来。
- **③ 要求**：停用路径把 `next_run_at` 置 NULL（与 `is_active` 同一次 UPDATE，避免中间态）；确认 `Stats().NextRuns` 不受影响（它读 cron entry，本来就空）。
- **④ 约束与非目标**：不改 `SyncSchedule` 结构体与 JSON 字段名（`next_run_at,omitempty` 语义已够）；不改 toggle 的返回体；A-08 前端**先按「inactive 不显示 next_run」防御**，但后端修好后不改前端判据。
- **⑤ 验收**：单测：toggle false 后 store 读回 `NextRunAt == nil`；活进程复测上表同一条 curl 序列，`next_run_at` 消失；**破坏验证**：还原清除语句 → 单测红。
- **⑥ 边界**：只改停用路径的字段清理；`last_run_job_id` 等其余残留另立。

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
