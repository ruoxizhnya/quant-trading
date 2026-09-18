---
status: active
last-verified: 2026-09-18
verified-by: 代码审查（2026-09-16）+ 产品重构讨论；P0-4 落地复核（2026-09-17）；P2-9wire / P2-9f / P2-10 / P1-5 / P2-12 落地（2026-09-18）
---

# TASKS — 未完成项

> **本文件只放未完成项。** 完成即删除（历史见 `archive/TASKS-history.md`，217KB）。
> 上限 5 页，超出说明该拆项目了。

---

## 当前方向

产品定位已重构为：**AI 实验员操作确定性底座做研究，验证器（被调用，不自主循环）独立证伪，人在异步审阅台上看「质疑清单 + 路径形状」，只对校准后的概率下注。**

三条主线，优先级即下列顺序：

1. **止血** — 让现有回测数字变得可信（P0）
2. **通回路** — 让 AI 真正能操作底座跑完一次循环（P0/P1）
3. **加数据** — 宏观/跨境、产业链（P1/P2）

### 已完成（2026-09-16，已从下方列表移出）

- **P0-1 前视偏差**：财务读取改为按 `COALESCE(ann_date, trade_date)` 过滤
  （`GetFundamentalsSnapshot` / `GetFundamentals`）；回归测试见
  `pkg/storage/fundamentals_pit_test.go`（4 例，红→绿已验证）。
  顺带修掉两个扫描崩溃：`ann_date` 为 NULL 时报错、数值列为 NULL 时报错。
- **P0-3 放开测试**：`.gitignore` 的 `*_test.go` 规则已移除，新测试可正常入库。
  副作用：立刻暴露出此前被隐藏的 `cmd/analysis/handlers_proxy_test.go`。
- **P0-2 建 CI**：`.github/workflows/ci.yml`，四个步骤 —— `go build` / `go vet` /
  `go test ./...`（带 Postgres 服务容器，否则存储层会 skip）/ 文档链接校验。
  顺带修掉全仓基线上的 4 个失败，以及一个真 bug：
  `generateOrderID()` 仅用 `UnixNano()`，Windows 时钟粒度粗导致**连续订单 ID 相撞，
  后一笔静默覆盖前一笔** → 加 atomic 序号（`pkg/live/order_manager.go:258`）。
- **P0-4 鉴权收口**：① 无密钥直接 `Fatal` 拒绝启动（裁决逻辑拆成纯函数
  `decideAuthStartup` 以便测试）；唯一豁免是 `AUTH_INSECURE=true` **且**
  `server.host` 为 loopback —— 非 loopback 时豁免无效。
  ② CORS 从硬编码 `*` 改为按 `server.cors.allowed_origins` 回显，留空即 fail closed；
  analysis / data 两份复制粘贴的中间件收成 `internal/httpserver.CORS`。
  回归测试：`cmd/analysis/auth_bootstrap_test.go`、`internal/httpserver/cors_test.go`，
  两侧各一个装配测试（防"实现改对了但 buildRouter 没接上"）。
  **副作用**：`docker-compose.yml` 的 analysis 服务现在强制要求 `JWT_SECRET`
  （未设则 compose 报错退出）；本地 `go run ./cmd/analysis` 也要带密钥或走豁免，
  见 `guides/local-dev.md`。
- **P0-5 实验链路打通**（根因比原描述深两层，见 [ADR-024](adr/adr-024-expression-as-execution-target.md)）：
  ① `Execute` 在生成 YAML 后**从未把策略注册进 registry**，Stage 5 拿
  `parsedIntent.StrategyName` 回测必然 `strategy not found` —— 补上 `buildAndRegister`；
  ② 更深一层：YAML 生成器只在 intent 自带 `signal_expr` 时才产出 expression 段，
  规则解析出的 intent 只有语义类型（momentum/…），`LoadStrategy` 只认 expression
  → 生成的 YAML 根本加载不成策略。补 `defaultSignalExpression()`：意图类型 →
  确定性默认表达式（价量可表达者）；value / quality 需要 PE/ROE 而表达式引擎
  只有 OHLCV，**明确失败而不是给假数字**。
  ③ LLM 生成的 Go 代码降级为**可审阅 artifact**：编译失败 / LLM 未配置只记录不阻断。
  ④ `Execute` 与 `ExecuteAsync` 的五段逻辑是复制粘贴的两份 → 合并为 `p.run`，
  只修一边就会漏另一边。
- **P0-6 编译校验恒真**：`go tool compile -V=full` 是**打印编译器版本号**的开关，
  根本不读文件，`Compiles` 恒为 true。换成在 module 根下真跑 `go build`；
  找不到 go.mod 时报「无法验证」而非假装通过（fail closed）。

---

## P0 — 正确性（不修，其它一切都是沙上建塔）

**P0-1 ~ P0-6 已于 2026-09-17 全部完成**（明细见上方「已完成」）。
S0 止血阶段的出口判据已满足，见 [ROADMAP](ROADMAP.md)。

---

## P1 — 地基与回路

| # | 任务 | 位置 | 验收 |
|---|---|---|---|
| **P1-1** | **实验日志**：记录 AI 每次尝试（参数向量 / 结果 / 父子关系 / 假设来源） | `experiments` 表 + `pkg/storage/experiments.go` | 能回放一条完整探索路径 |
| **P1-1a** | └ 存储层：表 + 存取（插入 / 收尾 / 单查 / 按 seq 回放） | **✅ 2026-09-17** | 单测 5 例全绿，表已落库 |
| **P1-1b** | └ 生产者：pipeline 每次尝试落一行，**失败也要落**；`cmd/analysis` 启动时注入 sink。⚠️ `UNIQUE(run_id, seq)` 要求 seq 由调用方分配 —— 并发跑时不能让两边各自 +1 | **✅ 2026-09-17** `pkg/ai/pipeline/pipeline.go`、`cmd/analysis/handlers_pipeline.go:46` | 真库端到端取证通过（跑完落 completed，失败落 failed） |
| **P1-1c** | └ 回放：路径可读。单次「试了什么」+ 多步「为什么转到下一步」都已具备（expression / params / hypothesis / 指标 / parent_id） | **✅ 2026-09-17** | 一条 run 从 seq 0 到 seq n 能讲成一个故事 |
| **P1-2** | **循环控制器**：让搜索算法（或 LLM）能连续调用底座 + 响应中断 | **✅ 2026-09-17** `pkg/ai/loop/`（新包；TPE 已复用，遗传仍零调用） | 连续跑 N 次 ✅、中途叫停 ✅、单次失败不中断 ✅、seq 连续 + 父子链 ✅ |
| **P1-2b** | ~~搜了但没生效~~ → **已修**：`defaultSignalExpression` 改为读意图参数 + 开结构化传参通道 `WithParameterOverrides`。⚠️ **搜索空间的参数名必须与意图参数同名**（`lookback_days`）—— 写成 `lookback` 之类别名**不报错、只是静默失效**，整轮退化成同一个策略跑 N 遍 | **✅ 2026-09-17** `pkg/ai/yaml/generator.go`、`pkg/ai/pipeline/pipeline.go`、`pkg/ai/loop/loop.go` | 取证：一轮 6 次窗口各不相同（15/30/33/22/47/35）；另有不依赖库的回归 `TestRun_DifferentParamsProduceDifferentConfig` |
| **P1-3** | **观察页**：三栏（正在试什么 / 结果流 / 当前最优）+ 干预入口 | **✅ 2026-09-17** `web/src/pages/Explore.vue`、`web/src/api/explore.ts`、`cmd/analysis/handlers_explore.go` | 能看见路径形状，能输入方向，能叫停 |
| **P1-4** | ~~**schema 收口**：真实 DDL 硬编码在 Go 里，`migrations/` 无版本管理。**含语义债**：`stock_fundamentals.trade_date` 被两种写入路径复用~~ | **✅ 2026-09-18** `pkg/domain/market/types.go` + `pkg/data/tushare.go` + `pkg/storage/fundamentals.go` + `postgres.go`（migration 031） | **调查推翻了原描述**：不是"两种数据源语义不同"，两条路径调的是**同一个 `fina_indicator`**、**都把 end_date 写进 trade_date**；真正的差别是路径 A（`normalizeFundamentals`）把 API 已返回的 `ann_date` **直接丢弃**。所以 P0-1 的 `COALESCE(ann_date, trade_date)` 只是把洞盖住 —— 对路径 A 的行它退化成报告期截止日，三季报（9/30 截止、10/25 披露）仍在 9/30 可见，前视偏差没清。修法：① 路径 A 补存 `ann_date`；② 新增 `available_date` **生成列** `GENERATED ALWAYS AS (COALESCE(ann_date, trade_date)) STORED`，10 处读取全部改用它（此前 5 处 COALESCE、5 处裸用 trade_date）。**为什么用生成列而不是普通列**：普通列要靠每个写入函数记得填，任何绕过写入函数的路径（手工 INSERT、修数）都会留下 NULL，那行数据随后从所有按可用日过滤的查询里凭空消失。生成列从物理上保证它不可能为空、也不可能和 COALESCE 不一致。另修零值脏数据：`FundamentalData.AnnDate` 是 `time.Time` 非指针，缺字段时写入 0001-01-01 会让 COALESCE 拿到"公元前"，整行不可见 —— 写入侧用 `nullableDate` 转 NULL，存量用迁移清理。**schema 收口**：不做迁移（64 条 Go DDL 迁到 golang-migrate 的收益是洁癖，风险是回测依赖的表结构漂移），改为删除零调用的 `MigrationManager` + 在 `postgres.go` 写明「DDL 唯一真相在此，末尾追加并带 `// Migration 0NN:` 注释」；`migrations/` 与 `docs/migrations/` 留作历史记录，只读不执行 |
| **P1-5** | ~~**统一错误中间件**：154 处手写 `gin.H{"error"}`，`c.Error()` 使用 0 次~~ | **✅ 2026-09-18** `internal/httpserver/errors.go` + `cmd/**`（29 个文件、317 处） | 统一出口 `Fail` / `Failf` / `Error` / `Wrap` / `FailCause` + 兜底中间件 `ErrorMiddleware`（只接住「调了 c.Error() 却没写响应」的，已写响应的不覆盖）。**真正的收益不是整洁，是关掉泄露**：此前 5xx 直接把 `err.Error()` 吐给客户端（DB 报错 / 连接串 / 内部路径），现在 5xx 一律通用文案、原因只进日志，4xx 保留原文（那本来就是给人看的）。响应体保持 `{"error": ...}`（前端读这个字段）并新增 `code`。批量转换脚本留在 `tools/convert_error_responses.py`（它只转单行单键形态，跨行/多键一律跳过）。**剩余 4 处**是 `c.SSEvent("error", ...)` —— SSE 是另一条通道（响应已是 200 + 流），不属此列 |
| **P1-6** | **无界 goroutine**：信号量只限并发执行，goroutine 数 = 任务数 | `pkg/backtest/batch/batch.go:173`、`walkforward.go:152` | 固定 worker pool |
| **P1-7** | **N+1 查询**：300 只票 = 600 次 DB 往返 | `pkg/data/factor_attribution.go:89,103` | 改批量查询 |
| **P1-8** | **部署编排三份合一**：compose × 2 + k8s × 8，互相漂移 | `docker-compose*.yml`、`deploy/k8s/` | 一份源 + 生成 |
| **P1-9** | ~~前端 `/alerts` `/compliance` 缺 `/api` 前缀，dev 下必 404~~ | **✅ 2026-09-18** `web/src/api/`（alerts / compliance / paper-trading / market） | 实际比登记的多：除了 alerts、compliance，paper-trading 有 9 处、market 的 `/health` 也没前缀 —— dev 下 vite 只代理 `/api`，其余一律 404。顺带发现 **`/api/health` 后端根本没注册**，而 Dockerfile 的 HEALTHCHECK 打的就是它 —— 容器从启动起就被判 unhealthy，现在补了别名（与 `/health` 共用 handler） |
| **P1-10** | ~~**`make build` 会失败**：Makefile 仍 build `cmd/execution`、`cmd/risk`，这两个目录 ODR-021 合并后已不存在~~ | **✅ 2026-09-18** `Makefile` | 删掉两个死目标、加 `build-ai` / `push-ai`；`up/down/restart/logs/ps` 从 `docker-compose.services.yml` 改指根 `docker-compose.yml`（前者引用了已删服务的 Dockerfile，一读就炸；docs/guides/local-dev.md 早已记下「只用 docker-compose.yml」，这次把 Makefile 对齐到那个决定，并删掉漂移的那份） |
| **P1-11** | ~~**`cmd/ai` 无 Dockerfile、不在 `docker-compose.yml`**，只能本地 `go run`~~ | **✅ 2026-09-18** `cmd/ai/Dockerfile` | 补 Dockerfile（:8086，探针是 `/health` 不是 `/api/health`，AI 密钥走环境变量不入镜像）。**未**加进 compose —— 根 compose 明确写了 ai 是单独起的（`cmd/ai` 走的是 DEPRECATED 的 `pkg/ai/agents`，见 P2-5，把它塞进默认编排反而是固化一条待废弃路径） |
| **P1-13** | **稳健校验器把「稳定地不赚钱」判成稳健**：「邻域站得住」的阈值取中心的一半，中心 Sharpe 趋零时门槛也趋零 → 中心 0.031 也能拿到高原面积 1.00、概率 1.000。**已修（2026-09-17，随 cf2bbaa）**：按中心高度打折（`MinMeaningfulSharpe=0.5`），同一组数据给 0.248，并提示「别把稳定地不赚钱当成稳健」。单测发现不了 —— 手写的邻域数字都是「合理」的 | **✅** `pkg/validation/robustness.go` | 平坦性说的是结论稳不稳，有效性说的是值不值得做，两者不能互相替代 |
| ~~**P1-14**~~ | ~~**回测不可复现**~~ → **已修（2026-09-17）**：同一份数据连跑两次，成交 122 vs 120 笔、收益 1.33 vs 1.31。四处非确定性来源：① momentum 遍历 `bars` map + `sort.Slice` 不稳定 → top-N 选谁每次不同（8 只动量相同的票选 3 只，200 次调用出 8 种组合）；② tracker 持仓求和顺序（浮点加法不满足结合律，差 1e-10，复利放大后变 38 元）；③ 止损 / 强平遍历持仓 map → 同日平仓顺序互换；④ regime 检测拼接行情顺序 → 仓位倍数不同。**现已 684 笔成交逐位一致** | **✅** `pkg/backtest/tracker/tracker.go`、`pkg/backtest/engine.go`（regime 拼接 + 止损定序）、`pkg/backtest/engine_daily.go`（强平定序）、`pkg/strategy/examples/momentum.go` | 回归测试：`pkg/backtest/engine_reproducibility_test.go`（跑两次逐位比对 + top-N 顺序无关性） |
| ~~**P1-12**~~ | ~~**回测的「今天」取自 `time.Now()`**~~ → **已修（2026-09-17）**：两层都改了。① 引擎侧：`Tracker` 新增 `asOf`，主循环在生成信号**之前**调 `SetAsOf(date)`，`GetPortfolio` 用它填 `UpdatedAt`（零值才退回墙钟 —— 那是实时撮合场景）。② 策略侧：momentum 不再用 `time.Now()` 兜底，改为「组合快照 → 行情最新日期 → 直接报错」；`multi_factor` / `value_screen` 无日期时不发信号；`convertible_bond` 的纯债折现改用回放日期。取证：weekly 252 天 0 成交 → 114 笔 | **✅** `pkg/backtest/tracker/tracker.go`、`pkg/backtest/engine.go:625`、`pkg/strategy/utils.go`（新增 `LatestBarDate`）、`examples/momentum.go`、`plugins/{multi_factor,value_screen,convertible_bond}.go` | 回归测试：`pkg/backtest/engine_rebalance_date_test.go`（spy 盯契约 + weekly 盯后果 + tracker 单测） |

---

## P2 — 数据与清理

| # | 任务 | 位置 |
|---|---|---|
| **P2-1** | 补**宏观/跨境数据源**（美股、汇率、利率、大宗）—— 对产业链认知价值最高 | 新建 adapter |
| **P2-2** | 产业链数据底座最小版：`query_supply_chain(name)` | 新建 |
| **P2-3** | 因子加 `hypothesis_source` 字段（因果来源） | `factor_cache` schema |
| **P2-4** | ~~幸存者偏差：无退市/剔除逻辑，回测 universe 不完整~~ | **✅ 2026-09-18** `pkg/domain/market/types.go` + `pkg/data/tushare.go` + `pkg/storage/stocks.go` + `pkg/backtest/engine.go` + `engine_daily.go` + `cmd/data/sync_handlers.go` + `cmd/analysis/handlers_explore.go` | 三层一起修：① **数据侧**——`stock_basic` 的 `delist_date` 此前被 `normalizeStocks` 整个丢弃、`Status` 还硬编码 `active`，现在落进 `stocks.delist_date`（migration 030，`*time.Time` —— 零值时间会被读成"一万年前就退市"，必须区分"没有"和"零值"）；同步入口支持 `list_status=ALL` / 逗号分隔，展开成 L+D+P 三次拉取。② **引擎侧**——预热一次上市日历，`eligibleUniverse` 每天把池子过滤成「当日仍在市」：未上市的剔除（未来股，另一种前视偏差）、已摘牌的剔除（那时它已不存在）、**但退市前一直在池子里**（这才是修偏差的关键，只做"剔除"等于把偏差坐实）。③ **持仓**——已退市但还持仓的票保留在 universe 里并在摘牌后强平，否则这笔钱一路挂到回测结束，中间所有损益被抹平。摘牌当天仍算在市（退市整理期有行情）。**没有日历时不过滤**，且偏差维照实报 `PoolSourceCurrent` —— 债还在就别装作修好了。接线后 `ExploreHandler.biasInput()` 按引擎实际口径切换 `PoolSourcePointInTime`，那条每轮都带的 blocking 到此才能真正消失 |
| **P2-5** | 废弃模块清理：`pkg/ai/agents` 标记 DEPRECATED 仍是 `cmd/ai` 主链路 | `cmd/ai/main.go:13` |
| **P2-6** | `drift` 概念漂移检测零调用（孤儿代码）；`get_market_regime` 未进主流程 | `pkg/ai/drift/`、`pkg/tools/builtin/` |
| **P2-7** | 死配置 `config/ai-service.yaml` 从未被读取；~~`docker-compose.services.yml` 引用不存在的 Dockerfile~~（该文件已于 2026-09-18 删除，见 P1-10） | config/（待办） |
| **P2-8** | 幸存的前视风险复核：复权口径无 hfq 对照 | migrations |
| **P2-9wire** | └ **接线**（聚合 → 循环 → 日志 → 前端）：**✅ 2026-09-18** `aggregate.go` + `pkg/ai/loop` + `handlers_explore.go` + `web/src/pages/Explore.vue` | 五片全写完时整包零调用（"5/6 片的零件 + 0 接线"）。聚合入口 `ValidateProposal` 取**各维最小值**作综合概率（合取：算术平均会让四维优秀掩盖一维致命，几何平均稀释太狠）；循环控制器**每次尝试后**跑（不是跑完整轮才跑），邻域 = 本轮已试过的参数；裁决写回 `experiments.verdict`（migration 029，`json.RawMessage` 以避免 storage→validation 成环）；偏差维按实测口径注入（前复权 + **池子按当前上市名单** = 幸存者偏差真实存在，所以每轮都会带这条 blocking —— P2-4 那笔债就该长这样）。踩过的坑：`Attempt` 是值类型，`append` 早于 `Verdict` 赋值会让库里那一版永远为空 |
| **P2-9a** | └ **统计**：多重检验校正（试了 N 次，门槛按 N 收紧）。吃 P1-1 的实验日志 | **✅ 2026-09-17** `pkg/validation/statistical.go` | 同样 Sharpe，试 500 次必须比试 5 次更不可信 |
| **P2-9b** | └ **经济**：扣费后净收益 | **✅ 2026-09-17** `pkg/validation/economic.go` + `turnover.go` | 毛收益扣掉手续费 / 印花税 / 过户费 / 冲击成本后仍成立；附盈亏平衡换手率 |
| **P2-9c** | └ **稳健**：参数敏感度（高原面积）+ 分年度一致性 | **✅ 2026-09-17** `pkg/validation/robustness.go` | 邻域不能塌（高原优于尖峰）、收益不能集中在单段；中心太低的高原按高度打折 |
| **P2-9d** | └ **偏差**：前视、幸存者、复权口径 | **✅ 2026-09-17** `pkg/validation/bias.go` | 三子维度几何平均（合取：一维致命就拉垮）。**没查 ≠ 没问题**：未评估的子维度不参与概率，只给 note；一个都没评评估时概率是中性 0.5。接线状态已写进文件头：行情是前复权（`ohlcv_daily_qfq`，自带前视成分）、池子来源取决于同步时的 `list_status`（传 "L" = 当前上市 = 真有幸存者偏差）、PIT 目前只有 equitydeep 链路强制 `ann_date` |
| **P2-9e** | └ **冗余**：与已有策略相关性（防「伪分散」） | **✅ 2026-09-18** `pkg/validation/redundancy.go` | 分散的是风险来源不是策略数量。概率按样本量向 0.5 收缩（n<30 不敢断言）；**零方差序列记入 Skipped 而非当成 ρ=0**（那会伪装成「完全不相关」）；强负相关标明是「反向复制」不是新增风险源。接线口 `RedundancyFromBacktests` 直接吃回测结果。取证：lookback 20 vs 25 → \|ρ\|=0.77 判伪分散；vs 200 → 0.30 |
| **P2-9f** | └ **因果**（L3，需 LLM）：讲得出为什么吗 | **✅ 2026-09-18** `pkg/validation/causal.go` + `pkg/ai/causal/narrator.go` | **形态不是"让 LLM 讲讲为什么"，而是"先下注、再验证"**：模型给机制 + **可证伪的预测**（带数值边界），确定性检验逐条验，讲得出但预测不中的等于没讲。硬规矩：给模型的请求里**物理上不含回测结果**（`ValidateCausal` 里 `blind := req; blind.Result = nil`）—— 知道答案后做的"预测"只是复述，那一维会永远通过。可验种类：胜率 / 成交笔数 / 持有天数 / 最大回撤 / 收益集中度（top-K 天贡献占比）。概率 = Beta(1,1) 后验均值 `(中+1)/(可验+2)`；**一条可证伪的都没有 → 0.25 并 blocking**（"只讲了散文"）；验不了的预测既不算通过也不算失败，只给 note。接线节奏：一次模型调用不便宜，所以**一轮探索只给最终候选做一次**（`AttachCausal` 事后补进裁决并重算综合概率 / 最弱维 / blocking），叙述失败 = 未评估，不假装通过 |
| **P2-10** | ~~`domain.Fundamental` 数值字段是 `float64`，而表中列可为空。P0-1 中用 `COALESCE(col,0)` 兜底，导致**缺失值被当作 0 而非"未知"**（PE=0 会被误判为极便宜）~~ | **✅ 2026-09-18** `pkg/domain/market/types.go` + `pkg/storage/fundamentals.go` + `pkg/data/tushare.go` + `pkg/strategy/examples/value_momentum.go` | 两层都改了：① 类型改 `*float64`，存储层**去掉** `COALESCE(col,0)`（NULL 扫成 nil），源端缺字段也留 nil（`fieldFloatPtr`）；② 因子层显式跳过缺失：`stockFactorData.PE/PB/ROE` 也改指针，`calculateMeanStd` 只统计非 nil（此前靠 `v != 0` 近似"缺数据"，而 ROE=0 是盈亏平衡，是真值），缺项的股票在对应因子上得**中性 0 分**而不是"最便宜"。这个 bug 的杀伤力在于它会主动把人引向错误交易：PE 缺失折成 0 后在"越低越便宜"的排序里冲到第一 |
| **P2-11** | Hermes Agent 系统设计文档遗失（原在 `.trae/documents/`，目录已删）。SPEC §6 与 hermes 验收测试均引用它 | `docs/hermes/` | 补写设计文档，或在引用处说明以配置为准 |
| **P2-12** | ~~**表达式引擎只暴露 OHLCV**（open/high/low/close/volume/turnover），因此 `value` / `quality` 类意图表达不出 —— P0-5 中它们只能明确失败，而不是套一个无关的价格表达式产出误导性回测数字~~ | **✅ 2026-09-18** `pkg/strategy/expression/data_provider.go` + `strategy.go` + `pkg/strategy/strategy.go` + `pkg/storage/fundamentals.go` + `pkg/backtest/engine.go` + `pkg/ai/yaml/generator.go` | 新增 `pe/pb/ps/roe/roa` 五个字段，**按 PIT 对齐**（`GetFundamentalsPITBulk` 返回的 Date 是可用日 `COALESCE(ann_date, trade_date)`，不是报告期；`OHLCVDataProvider.fundamentalSeries` 按每根 K 线的日期切一刀，取不到填 NaN 不是 0）。注入走 `strategy.FundamentalAware` 可选接口（`GenerateSignals` 签名没有基本面参数，不动接口；范式同 `FactorAware`），且**只有声明要财报的策略才预热**——纯价量策略不付这份查询成本。`value` → `cs_rank(neg(pe)) + cs_rank(neg(pb)) > 1.6`，`quality` → `cs_rank(roe) + cs_rank(roa) > 1.6`；`custom` 仍明确失败。**估值倍数非正一律 NaN**：PE 为负不是"便宜"是亏损，`neg(pe)` 不该把亏得最狠的排成最便宜（经典价值陷阱）；ROE/ROA 为负是真实的差，原样保留。**顺手修掉一个潜伏 bug**：`neg(x)` 的函数形式此前从未接上（`evaluateFunction` 无条件走 `applyTimeSeriesOp`），`multi_factor` 的默认表达式 `cs_rank(neg(ts_std(close,20)))` 从落地起就是「解析得过、跑不起来」—— 它只被断言过能解析，从没被求值过 |
| **P2-13** | **验证器链缺真实回测的端到端取证**（2026-09-17 已解决一半）。缺口只剩数据：本地库 `stocks` / `trading_calendar` / `ohlcv_daily_qfq` 均 0 行。~~引擎离线跑不了~~ —— 这是误判，引擎三处 HTTP（仓位 / 择时 / 止损）**都有 in-process 分支**，`cmd/analysis/main.go:160` 也已 `SetRiskManager`；取证时用 `marketdata.NewInMemoryProvider()` + `SetRiskManager` 即可完全离线（范式见 `pkg/validation/economic_integration_test.go`） | `pkg/validation/economic_integration_test.go` | 跑数据同步补齐行情后，用同一范式接真库 |

---

## 已冻结（本次定位重构后不再投入）

- 多 agent 投票 / arbitrator 仲裁机制 —— 错误相关，放大偏差且不可解释
- 演化算法直接产出交易信号 —— 产出落在"高统计/无因果"象限，不会被采用（改为漏斗顶部的候选生成器）
- EquityDeep 作为"独立工作面"—— 改为数据底座（产业链图谱）+ 研究洞察库

---

_完成一项删一项。本文件超过 5 页 = 该拆分项目了。_
