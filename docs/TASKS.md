---
status: active
last-verified: 2026-09-21
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

**2026-09-21 全栈审查（[ODR-065](archive/odr/odr-065-fullstack-static-review.md)）新增 5 项 Critical** — 证据行号、代码示意与 15 commits 修复方案见 [审查报告 §4/§16](archive/reports-2026-Q3/review-report-20260921.md)。**前置**：推送 main 领先 origin 的 51 提交并恢复 feature branch + PR（P2 区 AUD-L1）；每任务一个原子 commit，测试先行。

| ID | 任务 | 位置 | 验收 |
|----|------|------|------|
| AUD-01 | 加固 `/api/copilot/save`（报告 D-1 默认加固，删除为备选需裁决）：`StrategyName` 正则白名单 `^[A-Za-z][A-Za-z0-9_]{0,63}$` + 写盘前 `staticcheck.CheckOrError(req.Code)`（复用 generate 路径同款闸）+ `fmt.Sprintf` 拼路径改 `filepath.Join` | cmd/analysis/handlers_copilot.go#L174-199 | 遍历名 400 / 含 `exec.Command` 代码 422 / 合名合法码 200 落 plugins；测试先行先红后绿 |
| AUD-02 | RBAC 接线：`/api/execution` 三动作端点 **与 legacy 根路径两处都** 挂 `RequireRole(trader, admin)`；`/api/tools` 增副作用分级 map（fail-closed：未登记 = admin）；先验证 auth disabled 时 RequireRole 放行路径（setup.go#L626 只在 Enabled 时挂 Middleware） | cmd/analysis/handlers_execution.go / handlers_tools.go / pkg/auth | viewer 下单 403 / trader 200 / 只读工具 viewer 200 / auth disabled 全放行 / legacy 与 `/api` 行为一致 |
| AUD-05 | **11 张表 DDL 内联移植**（migrations/015~018 → postgres.go migrate() 数组末尾，编号注释续 Migration 028+，幂等 `IF NOT EXISTS`——补上 P1-4 约定的执行缺口）+ 一致性断言测试：静态读 postgres.go 提取 `CREATE TABLE IF NOT EXISTS (\S+)`，断言 ⊇ TableMapper 全部目标表（零 DB 依赖） | pkg/storage/postgres.go + 新 bulk_insert_ddl_test.go | mapper 13 映射目标表全部在 DDL 声明；新环境 `compose down -v && up` 后 13 类数据同步各落库 ≥1 行 |

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
| **P1-6** | ~~**无界 goroutine**：信号量只限并发执行，goroutine 数 = 任务数~~ | **✅ 2026-09-18** `pkg/backtest/workers/`（新 leaf 包）+ `batch/batch.go` + `walkforward/walkforward.go` | 新增泛型 `workers.RunWorkers(ctx, n, jobs, fn)`：先起固定 N 个 worker 从 channel 领活，goroutine 总数 = min(N, 任务数)，与任务数无关。**修之前的问题**：`for { go f() }` + 信号量只限制了同时**执行**几个，1 万个任务的 goroutine 已全部建出，内存/调度是 O(任务数)。两点约定写进文档：① 结果按输入下标落位（完成顺序不定但落位确定，守住 P1-14 可复现）；② ctx 取消后不再领新活（在跑的不强杀）。leaf 包是为避免循环依赖 —— walkforward 不能 import backtest，contracts 是 DTO/接口包不放执行器 |
| **P1-7** | ~~**N+1 查询**：300 只票 = 600 次 DB 往返~~ | **✅ 2026-09-18** `pkg/storage/ohlcv.go`（新增 `GetClosesOn`）+ `pkg/data/factor_attribution.go` | 因子归因原本对每只票调两次 `GetOHLCV`（当前日 + 远期日），300 只票 = 600 次往返。改为每个日期**一次** `WHERE symbol = ANY($1) AND trade_date = $2`。**没行情的票不出现在返回 map 里** —— 调用方据此判断缺失，塞 0 会被读成"白送的股票"（P2-10 那类坑）。测试用计数假 store 断言 `closesCalls == 2` 且 `ohlcvCalls == 0`：**只验结果测不出 N+1**，必须数查询次数 |
| **P1-8** | ~~**部署编排三份合一**：compose × 2 + k8s × 8，互相漂移~~ | **✅ 2026-09-18** `tools/check_deploy_consistency.py`（新）+ `docs/guides/deploy-config.md`（新）+ `deploy/k8s/configmap.yaml` + `.github/workflows/ci.yml` | 现状先变了：compose 已收敛到 1 份（P1-10 删的），k8s 剩 6 份（P2-5 删了 ai-deployment）。**没做「一份源生成两份」** —— 那要引入 Kompose/Helm，为几处配置给单人自托管项目加一整套生成链，收益不抵复杂度。改为**护栏 + 文档**：① 新增一致性校验脚本（比对服务端口与 `DATA_SERVICE_URL`，CI 里跑，实测能把人为改错的两处都抓出来）；② 写清「改端口要动 4 个地方」。**顺手修两处死配置**：configmap 的 `ANALYSIS_PORT`/`DATA_PORT` 从未被读取（代码读 viper 的 `server.port`），已删；`DATA_SERVICE_URL` 在 k8s 里长期缺失、全靠代码默认值碰巧能用，已显式配上 |
| **P1-9** | ~~前端 `/alerts` `/compliance` 缺 `/api` 前缀，dev 下必 404~~ | **✅ 2026-09-18** `web/src/api/`（alerts / compliance / paper-trading / market） | 实际比登记的多：除了 alerts、compliance，paper-trading 有 9 处、market 的 `/health` 也没前缀 —— dev 下 vite 只代理 `/api`，其余一律 404。顺带发现 **`/api/health` 后端根本没注册**，而 Dockerfile 的 HEALTHCHECK 打的就是它 —— 容器从启动起就被判 unhealthy，现在补了别名（与 `/health` 共用 handler） |
| **P1-10** | ~~**`make build` 会失败**：Makefile 仍 build `cmd/execution`、`cmd/risk`，这两个目录 ODR-021 合并后已不存在~~ | **✅ 2026-09-18** `Makefile` | 删掉两个死目标、加 `build-ai` / `push-ai`；`up/down/restart/logs/ps` 从 `docker-compose.services.yml` 改指根 `docker-compose.yml`（前者引用了已删服务的 Dockerfile，一读就炸；docs/guides/local-dev.md 早已记下「只用 docker-compose.yml」，这次把 Makefile 对齐到那个决定，并删掉漂移的那份） |
| **P1-11** | ~~**`cmd/ai` 无 Dockerfile、不在 `docker-compose.yml`**，只能本地 `go run`~~ | **✅ 2026-09-18** `cmd/ai/Dockerfile` | 补 Dockerfile（:8086，探针是 `/health` 不是 `/api/health`，AI 密钥走环境变量不入镜像）。**未**加进 compose —— 根 compose 明确写了 ai 是单独起的（`cmd/ai` 走的是 DEPRECATED 的 `pkg/ai/agents`，见 P2-5，把它塞进默认编排反而是固化一条待废弃路径） |
| **P1-13** | **稳健校验器把「稳定地不赚钱」判成稳健**：「邻域站得住」的阈值取中心的一半，中心 Sharpe 趋零时门槛也趋零 → 中心 0.031 也能拿到高原面积 1.00、概率 1.000。**已修（2026-09-17，随 cf2bbaa）**：按中心高度打折（`MinMeaningfulSharpe=0.5`），同一组数据给 0.248，并提示「别把稳定地不赚钱当成稳健」。单测发现不了 —— 手写的邻域数字都是「合理」的 | **✅** `pkg/validation/robustness.go` | 平坦性说的是结论稳不稳，有效性说的是值不值得做，两者不能互相替代 |
| ~~**P1-14**~~ | ~~**回测不可复现**~~ → **已修（2026-09-17）**：同一份数据连跑两次，成交 122 vs 120 笔、收益 1.33 vs 1.31。四处非确定性来源：① momentum 遍历 `bars` map + `sort.Slice` 不稳定 → top-N 选谁每次不同（8 只动量相同的票选 3 只，200 次调用出 8 种组合）；② tracker 持仓求和顺序（浮点加法不满足结合律，差 1e-10，复利放大后变 38 元）；③ 止损 / 强平遍历持仓 map → 同日平仓顺序互换；④ regime 检测拼接行情顺序 → 仓位倍数不同。**现已 684 笔成交逐位一致** | **✅** `pkg/backtest/tracker/tracker.go`、`pkg/backtest/engine.go`（regime 拼接 + 止损定序）、`pkg/backtest/engine_daily.go`（强平定序）、`pkg/strategy/examples/momentum.go` | 回归测试：`pkg/backtest/engine_reproducibility_test.go`（跑两次逐位比对 + top-N 顺序无关性） |
| ~~**P1-12**~~ | ~~**回测的「今天」取自 `time.Now()`**~~ → **已修（2026-09-17）**：两层都改了。① 引擎侧：`Tracker` 新增 `asOf`，主循环在生成信号**之前**调 `SetAsOf(date)`，`GetPortfolio` 用它填 `UpdatedAt`（零值才退回墙钟 —— 那是实时撮合场景）。② 策略侧：momentum 不再用 `time.Now()` 兜底，改为「组合快照 → 行情最新日期 → 直接报错」；`multi_factor` / `value_screen` 无日期时不发信号；`convertible_bond` 的纯债折现改用回放日期。取证：weekly 252 天 0 成交 → 114 笔 | **✅** `pkg/backtest/tracker/tracker.go`、`pkg/backtest/engine.go:625`、`pkg/strategy/utils.go`（新增 `LatestBarDate`）、`examples/momentum.go`、`plugins/{multi_factor,value_screen,convertible_bond}.go` | 回归测试：`pkg/backtest/engine_rebalance_date_test.go`（spy 盯契约 + weekly 盯后果 + tracker 单测） |

**2026-09-21 全栈审查（ODR-065）新增 8 项 High** — 证据行号与修复代码示意见[审查报告 §5/§16](archive/reports-2026-Q3/review-report-20260921.md)：

| ID | 任务 | 位置 | 验收 |
|----|------|------|------|
| AUD-06 | 印花税默认值 0.001→0.0005（注释史实修正：2023-08-28 起 0.1% 减半至 **0.05%**，非"0.2%→0.1%"）；存量断言 0.001 的测试此前固化错误值，一并修 | `pkg/fees/ashare.go#L49-53` | 卖出 10 万元收 50 元 |
| AUD-07 | 涨跌停板块分档 + 分取整：抽纯函数 `resolvePriceLimit`（新股→New / ST 系→ST / 300·301·688·689→20% / 8·4 开头北交所→30% / 其余 10%）；上下限价 `math.Round(x*100)/100` 后再比较；Config 增 Board20/Board30 | `pkg/backtest/engine_daily.go#L168-178` | 600/000/002/300/688/830 × {Normal,ST,*ST,新股} 表驱动；10.05→11.06 |
| AUD-08 | `*ST` 识别修复（`name[:2]` 永匹配不到 3/4 字符前缀）改 `strings.HasPrefix` 多模式；**与下方测试断言修正同一 commit**（测试固化了 bug，分开提交会中途红灯） | `pkg/backtest/engine.go#L1502-1508` + `engine_accessors_test.go#L286-290` | *ST/SST/S*ST/ST 全 true、`平安银行` false |
| AUD-09 | 整手取整 LotSize=100：Weight→shares 换算处归一，<100 跳过；**先 grep 正向确认现状**（负向证据，已存在则关闭） | pkg/backtest 下单量换算处 | 下单量恒为 100 倍数 |
| AUD-10 | MockTrader `GetPositions`/`GetAccount` 在 RLock 下经指针写共享对象 → 改值拷贝（`cp := *pos` 后写局部副本）；AUD-12 `-race` 门禁的前置 | `pkg/live/mock_trader.go#L44,301-338` | `go test ./pkg/live/... -race` 绿 |
| AUD-11 | Windows 沙箱 fail-closed：无 rlimit 能力（windows）时拒绝执行并明确报错；runner_test 的 skip 改断言。Job Object 完整实现列后续增强 | `internal/sandbox/runner` | Windows 上死循环代码被拒绝执行 |
| AUD-12 | CI 补门禁：Test 加 `-race`；新增 frontend job（lint/typecheck/test）。**在 AUD-04/AUD-10 合入后启用**，避免开门即红 | `.github/workflows/ci.yml#L47-48` | 含数据竞争的 PR → CI 红 |
| AUD-13 | docker-compose PG/Redis 端口绑 `127.0.0.1:`（Redis requirepass 涉及全部服务 REDIS_URL 联动，另立任务） | `docker-compose.yml#L27-28,39-40` | 宿主机外主机探测 5432/6379 不通 |

---

## P2 — 数据与清理

| # | 任务 | 位置 |
|---|---|---|
| **P2-1** | 补**宏观/跨境数据源**（美股、汇率、利率、大宗）—— 对产业链认知价值最高 | 新建 adapter |
| **P2-2** | 产业链数据底座最小版：`query_supply_chain(name)` | 新建 |
| **P2-3** | ~~因子加 `hypothesis_source` 字段（因果来源）~~ | **✅ 2026-09-18** `pkg/domain/factor.go` + `pkg/storage/factor_hypothesis.go` + `pkg/tools/builtin/factor_hypothesis_tool.go` + `postgres.go`（migration 032） | 新建 `factor_hypothesis` 表而不是给 factor_cache 加列：假设是**因子级**元数据，而 factor_cache 是 symbol×date×factor 的行级缓存，存成列会让同一个字符串重复几十万次。11 个内置因子的假设全部写实（6 个经典来自文献并给出处，5 个桥 B1 纵向因子来自 ADR-022 的产业链推导）—— 不是编数据，它们本来就有明确来源。source_kind 区分 literature / supply_chain / ai_hypothesis / ad_hoc。**有消费者**：新增 MCP 工具 `factor.hypothesis`（Group 11），让 AI 实验员在采用因子前能问「它凭什么有效」；`recorded` 字段区分「库里记过」和「回退到内置默认值」，两者可信度不同。无 DB 时工具仍可用（回退内置表） |
| **P2-4** | ~~幸存者偏差：无退市/剔除逻辑，回测 universe 不完整~~ | **✅ 2026-09-18** `pkg/domain/market/types.go` + `pkg/data/tushare.go` + `pkg/storage/stocks.go` + `pkg/backtest/engine.go` + `engine_daily.go` + `cmd/data/sync_handlers.go` + `cmd/analysis/handlers_explore.go` | 三层一起修：① **数据侧**——`stock_basic` 的 `delist_date` 此前被 `normalizeStocks` 整个丢弃、`Status` 还硬编码 `active`，现在落进 `stocks.delist_date`（migration 030，`*time.Time` —— 零值时间会被读成"一万年前就退市"，必须区分"没有"和"零值"）；同步入口支持 `list_status=ALL` / 逗号分隔，展开成 L+D+P 三次拉取。② **引擎侧**——预热一次上市日历，`eligibleUniverse` 每天把池子过滤成「当日仍在市」：未上市的剔除（未来股，另一种前视偏差）、已摘牌的剔除（那时它已不存在）、**但退市前一直在池子里**（这才是修偏差的关键，只做"剔除"等于把偏差坐实）。③ **持仓**——已退市但还持仓的票保留在 universe 里并在摘牌后强平，否则这笔钱一路挂到回测结束，中间所有损益被抹平。摘牌当天仍算在市（退市整理期有行情）。**没有日历时不过滤**，且偏差维照实报 `PoolSourceCurrent` —— 债还在就别装作修好了。接线后 `ExploreHandler.biasInput()` 按引擎实际口径切换 `PoolSourcePointInTime`，那条每轮都带的 blocking 到此才能真正消失 |
| **P2-5** | ~~废弃模块清理：`pkg/ai/agents` 标记 DEPRECATED 仍是 `cmd/ai` 主链路~~ | **✅ 2026-09-18** 删 `cmd/ai/` + `config/ai-service.yaml` + `deploy/k8s/ai-deployment.yaml` + k8s configmap/ingress 的 ai 条目 + Makefile 的 build-ai/push-ai + 更新 AGENTS.md / ARCHITECTURE.md / docker-compose.yml；改 `pkg/ai/agents/doc.go` | **删的是服务不是能力**：`pkg/ai/agents` 不能删 —— `pkg/ai/pipeline`（P1-1b 探索主链路）在用 ResearchAgent / GenerateAgent / ValidateAgent。真正的问题是 doc.go 那句 "should NOT be used in new code" 是错的并已造成误导，现在改为**分层说明**：ODR-046 废的是**交互层**（前端 AI UI 从未建成），不是**能力层**（本包仍是 pipeline 的实现细节）。`cmd/ai` 删掉的理由：只有 2 条 HTTP 路由且零调用方（前端/后端/编排都没有），没进 compose，只在 k8s 里有部署配置 |
| **P2-6** | ~~`drift` 概念漂移检测零调用（孤儿代码）；`get_market_regime` 未进主流程~~ | **✅ 2026-09-18** `pkg/tools/builtin/strategy_health_tool.go` + `pkg/ai/drift/detector.go` | `get_market_regime` 其实**早已接线**（`cmd/analysis/setup.go` 注册 GetMarketRegimeTool），登记有误。真正剩下的是 `pkg/ai/drift` 与 `pkg/strategy/monitor` 两个零调用孤儿（约 700 行完整实现，互相配套）。接法：写 `driftAdapter` 把 drift 适配成 monitor 的本地 DriftDetector 接口（monitor 故意不 import drift 以免反向依赖），新增 MCP 工具 `monitor.strategy_health`（Group 12）。**顺手修掉一个真 bug**：三个检测方法的 threshold 语义各不一样（mean 比 p 值 / variance 比 logF / distribution 比 KS 统计量），**没有任何取值能同时让三者合理** —— 传 2.0 会让 mean 把一切都判成漂移而 distribution 永不触发（KS≤1）。现统一为 `pValue < threshold`。与稳健维不重复：稳健维看参数邻域和分年度一致性（回测内部性质），这里看时序上最近是否偏离历史（上线后才有的问题）。样本不足时如实说"判断不了"，不假装健康 |
| **P2-7** | ~~死配置 `config/ai-service.yaml` 从未被读取~~；~~`docker-compose.services.yml` 引用不存在的 Dockerfile~~（后者已于 2026-09-18 删除，见 P1-10） | **✅ 2026-09-18** 随 P2-5 删除 | ai-service.yaml 是 `cmd/ai` 的配置，而 cmd/ai 零调用方且已删；另三个（analysis / data / strategy-service.yaml）都确实被读，不动 |
| **P2-8** | 幸存的前视风险复核：复权口径无 hfq 对照 | migrations |
| **P2-9wire** | └ **接线**（聚合 → 循环 → 日志 → 前端）：**✅ 2026-09-18** `aggregate.go` + `pkg/ai/loop` + `handlers_explore.go` + `web/src/pages/Explore.vue` | 五片全写完时整包零调用（"5/6 片的零件 + 0 接线"）。聚合入口 `ValidateProposal` 取**各维最小值**作综合概率（合取：算术平均会让四维优秀掩盖一维致命，几何平均稀释太狠）；循环控制器**每次尝试后**跑（不是跑完整轮才跑），邻域 = 本轮已试过的参数；裁决写回 `experiments.verdict`（migration 029，`json.RawMessage` 以避免 storage→validation 成环）；偏差维按实测口径注入（前复权 + **池子按当前上市名单** = 幸存者偏差真实存在，所以每轮都会带这条 blocking —— P2-4 那笔债就该长这样）。踩过的坑：`Attempt` 是值类型，`append` 早于 `Verdict` 赋值会让库里那一版永远为空 |
| **P2-9a** | └ **统计**：多重检验校正（试了 N 次，门槛按 N 收紧）。吃 P1-1 的实验日志 | **✅ 2026-09-17** `pkg/validation/statistical.go` | 同样 Sharpe，试 500 次必须比试 5 次更不可信 |
| **P2-9b** | └ **经济**：扣费后净收益 | **✅ 2026-09-17** `pkg/validation/economic.go` + `turnover.go` | 毛收益扣掉手续费 / 印花税 / 过户费 / 冲击成本后仍成立；附盈亏平衡换手率 |
| **P2-9c** | └ **稳健**：参数敏感度（高原面积）+ 分年度一致性 | **✅ 2026-09-17** `pkg/validation/robustness.go` | 邻域不能塌（高原优于尖峰）、收益不能集中在单段；中心太低的高原按高度打折 |
| **P2-9d** | └ **偏差**：前视、幸存者、复权口径 | **✅ 2026-09-17** `pkg/validation/bias.go` | 三子维度几何平均（合取：一维致命就拉垮）。**没查 ≠ 没问题**：未评估的子维度不参与概率，只给 note；一个都没评评估时概率是中性 0.5。接线状态已写进文件头：行情是前复权（`ohlcv_daily_qfq`，自带前视成分）、池子来源取决于同步时的 `list_status`（传 "L" = 当前上市 = 真有幸存者偏差）、PIT 目前只有 equitydeep 链路强制 `ann_date` |
| **P2-9e** | └ **冗余**：与已有策略相关性（防「伪分散」） | **✅ 2026-09-18** `pkg/validation/redundancy.go` | 分散的是风险来源不是策略数量。概率按样本量向 0.5 收缩（n<30 不敢断言）；**零方差序列记入 Skipped 而非当成 ρ=0**（那会伪装成「完全不相关」）；强负相关标明是「反向复制」不是新增风险源。接线口 `RedundancyFromBacktests` 直接吃回测结果。取证：lookback 20 vs 25 → \|ρ\|=0.77 判伪分散；vs 200 → 0.30 |
| **P2-9f** | └ **因果**（L3，需 LLM）：讲得出为什么吗 | **✅ 2026-09-18** `pkg/validation/causal.go` + `pkg/ai/causal/narrator.go` | **形态不是"让 LLM 讲讲为什么"，而是"先下注、再验证"**：模型给机制 + **可证伪的预测**（带数值边界），确定性检验逐条验，讲得出但预测不中的等于没讲。硬规矩：给模型的请求里**物理上不含回测结果**（`ValidateCausal` 里 `blind := req; blind.Result = nil`）—— 知道答案后做的"预测"只是复述，那一维会永远通过。可验种类：胜率 / 成交笔数 / 持有天数 / 最大回撤 / 收益集中度（top-K 天贡献占比）。概率 = Beta(1,1) 后验均值 `(中+1)/(可验+2)`；**一条可证伪的都没有 → 0.25 并 blocking**（"只讲了散文"）；验不了的预测既不算通过也不算失败，只给 note。接线节奏：一次模型调用不便宜，所以**一轮探索只给最终候选做一次**（`AttachCausal` 事后补进裁决并重算综合概率 / 最弱维 / blocking），叙述失败 = 未评估，不假装通过 |
| **P2-10** | ~~`domain.Fundamental` 数值字段是 `float64`，而表中列可为空。P0-1 中用 `COALESCE(col,0)` 兜底，导致**缺失值被当作 0 而非"未知"**（PE=0 会被误判为极便宜）~~ | **✅ 2026-09-18** `pkg/domain/market/types.go` + `pkg/storage/fundamentals.go` + `pkg/data/tushare.go` + `pkg/strategy/examples/value_momentum.go` | 两层都改了：① 类型改 `*float64`，存储层**去掉** `COALESCE(col,0)`（NULL 扫成 nil），源端缺字段也留 nil（`fieldFloatPtr`）；② 因子层显式跳过缺失：`stockFactorData.PE/PB/ROE` 也改指针，`calculateMeanStd` 只统计非 nil（此前靠 `v != 0` 近似"缺数据"，而 ROE=0 是盈亏平衡，是真值），缺项的股票在对应因子上得**中性 0 分**而不是"最便宜"。这个 bug 的杀伤力在于它会主动把人引向错误交易：PE 缺失折成 0 后在"越低越便宜"的排序里冲到第一 |
| **P2-11** | ~~Hermes Agent 系统设计文档遗失（原在 `.trae/documents/`，目录已删）。SPEC §6 与 hermes 验收测试均引用它~~ | **✅ 2026-09-18** `docs/hermes/system-design.md` | 遗失的是 `hermes-agent-integration-system-design.md`，代码里有 4 处引用（gate.go §6.2、factor_tools/gene_pool_tools/walkforward_tool §3.2）。**从代码反推补齐**：源码注释里逐条记了「设计说 X，我们做了 Y，原因 Z」，提炼出来就是 §3.2（工具参数契约 + 3 处已记录的偏离）与 §6.2（GateDecision 门控元数据）。**只有这两节是复原的**，其余章节代码没引用就不凭空补写，并在文档头部写明这是反推而非原件 |
| **P2-12** | ~~**表达式引擎只暴露 OHLCV**（open/high/low/close/volume/turnover），因此 `value` / `quality` 类意图表达不出 —— P0-5 中它们只能明确失败，而不是套一个无关的价格表达式产出误导性回测数字~~ | **✅ 2026-09-18** `pkg/strategy/expression/data_provider.go` + `strategy.go` + `pkg/strategy/strategy.go` + `pkg/storage/fundamentals.go` + `pkg/backtest/engine.go` + `pkg/ai/yaml/generator.go` | 新增 `pe/pb/ps/roe/roa` 五个字段，**按 PIT 对齐**（`GetFundamentalsPITBulk` 返回的 Date 是可用日 `COALESCE(ann_date, trade_date)`，不是报告期；`OHLCVDataProvider.fundamentalSeries` 按每根 K 线的日期切一刀，取不到填 NaN 不是 0）。注入走 `strategy.FundamentalAware` 可选接口（`GenerateSignals` 签名没有基本面参数，不动接口；范式同 `FactorAware`），且**只有声明要财报的策略才预热**——纯价量策略不付这份查询成本。`value` → `cs_rank(neg(pe)) + cs_rank(neg(pb)) > 1.6`，`quality` → `cs_rank(roe) + cs_rank(roa) > 1.6`；`custom` 仍明确失败。**估值倍数非正一律 NaN**：PE 为负不是"便宜"是亏损，`neg(pe)` 不该把亏得最狠的排成最便宜（经典价值陷阱）；ROE/ROA 为负是真实的差，原样保留。**顺手修掉一个潜伏 bug**：`neg(x)` 的函数形式此前从未接上（`evaluateFunction` 无条件走 `applyTimeSeriesOp`），`multi_factor` 的默认表达式 `cs_rank(neg(ts_std(close,20)))` 从落地起就是「解析得过、跑不起来」—— 它只被断言过能解析，从没被求值过 |
| **P2-13** | **验证器链缺真实回测的端到端取证**（2026-09-17 已解决一半）。缺口只剩数据：本地库 `stocks` / `trading_calendar` / `ohlcv_daily_qfq` 均 0 行。~~引擎离线跑不了~~ —— 这是误判，引擎三处 HTTP（仓位 / 择时 / 止损）**都有 in-process 分支**，`cmd/analysis/main.go:160` 也已 `SetRiskManager`；取证时用 `marketdata.NewInMemoryProvider()` + `SetRiskManager` 即可完全离线（范式见 `pkg/validation/economic_integration_test.go`） | `pkg/validation/economic_integration_test.go` | 跑数据同步补齐行情后，用同一范式接真库 |

**2026-09-21 全栈审查（ODR-065）新增 3 项**（Medium/Low + 流程前置）：

| ID | 任务 | 位置 |
|----|------|------|
| AUD-14 | AGENTS.md 校准：§1-3 按 ADR-023/024 现实重写（ADR-021/022 已入 superseded-adr）/ 表数口径改"内联 DDL 22 张（唯一执行路径）"/ `ingest.raw`·`research.*` 改"已落盘"/ Rule 2 的 `docs/odr/` → `docs/archive/odr/`（该目录已不存在）/ `pkg/tools/server.go` → `tool.go`；`migrations/` 与 `docs/migrations/` 目录首加 README"此目录不执行，加表改 postgres.go migrate() 数组"（是否物理移入 archive 另立 Cleanup ODR） | AGENTS.md + migrations/；`tools/check_doc_links.py` 增加 `--include-archive` 开关（现显式跳过 archive/，而 ODR 与报告全在该目录，其死链因此永不被告警——ODR-065 报告自身就有 2 处死链未被抓到） |
| AUD-15 | 删 `e2e/tests/ai-research.spec.ts`（打已删除的 :8086/cmd-ai，恒失败）；`fundamentals_detail` 读取处加空集防御（行数 0 → 显式报错，杜绝纵向因子静默拿空集，待 EQD-P1-2 摄取补齐） | e2e/tests + 纵向因子读取处 |
| AUD-L1 | **推送 main 领先 origin 的 51 提交**并恢复 feature branch + PR 流程（AGENTS.md §8 规范 3）——全部 AUD 修复工作的前置 | git |

---

## 已冻结（本次定位重构后不再投入）

- 多 agent 投票 / arbitrator 仲裁机制 —— 错误相关，放大偏差且不可解释
- 演化算法直接产出交易信号 —— 产出落在"高统计/无因果"象限，不会被采用（改为漏斗顶部的候选生成器）
- EquityDeep 作为"独立工作面"—— 改为数据底座（产业链图谱）+ 研究洞察库

---

_完成一项删一项。本文件超过 5 页 = 该拆分项目了。_
