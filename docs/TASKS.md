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

**2026-09-21 全栈审查（[ODR-065](archive/odr/odr-065-fullstack-static-review.md)）新增 5 项 Critical** — 证据行号、代码示意与 15 commits 修复方案见 [审查报告 §4/§16](archive/reports-2026-Q3/review-report-20260921.md)。每任务一个原子 commit，测试先行。

**已完成 5 项**：AUD-03（C1 日收益率）、AUD-04（C2 窗口隔离）、AUD-05（C5 DDL 断层）、AUD-01（C3 save 端点 —— **裁决为删除而非加固**：零生产调用方 + 用途与 ADR-024 冲突，攻击面归零优于加固后仍存在。护栏 `cmd/analysis/handlers_copilot_test.go` 断言该路由返回 404，已故意加回端点验证过它会变红）、AUD-02（H5 RBAC 接线）。

> 原「前置：推送 main 领先 origin 的 51 提交」（P2 区 AUD-L1）经复核为**误报**，已作废 —— 实测 `git rev-parse main` == `git ls-remote origin refs/heads/main`。

### AUD-02 落地说明（2026-09-21）

三项决策由若曦裁定：① **纳入 paper**；② legacy 根路径**一并挂同角色**；③ 豁免模式用**服务层短路**。

- **`pkg/auth` 新增 `Service.RequireRole`**（非包级 `RequireRole`）。根因：`Middleware()` 在 `!Enabled()` 时是纯 no-op、**不设 CtxRole**，包级 `RequireRole` 跟在它后面会让豁免模式下**所有请求 401**。规矩写进 doc comment：凡由 `s.Middleware()` 保护的分组，必须配 `s.RequireRole(...)`，两个决定（auth 是否开、要什么角色）必须一致，且只有 Service 知道第一个答案。
- **`pkg/tools/sideeffect.go`（新）**：21 个工具的副作用登记表（19 读 / 2 写）。选集中表而非给 `ToolCore` 加方法 —— 加方法会在编译期打断所有现有实现，且分类散落到各工具里更容易写不一致。**fail-closed**：未登记 = admin。两条漂移测试钉住「登记表 == setup.go 实际注册集」，双向都查（漏登记 / 陈旧行各一条）。
- **`/api/tools/:name` 的角色判定在 handler 内**：路由是通配路径，per-route 中间件表达不了「按工具决定角色」。检查在**读 body 之前**做 —— 无权调用的工具，不该让调用方能区分「body 错」和「你没权限」。
- **`/emergency-flatten` 不加 RBAC**（有意）：它已有服务端配置的 bearer token + confirmation_token 双因子；再加 JWT 依赖会在「auth 服务本身挂了」时锁死平仓路径，而那正是最需要它的时刻。
- **护栏经三处故意破坏实证**（记忆里的铁律：假护栏比没护栏更糟）：
  1. 拆掉 legacy 根路径的 `requireTrader()` → 只 `TestExecution_LegacyPath_SameAuthorityAsAPIPath` 变红，其余 10 项仍绿；
  2. 把 `s.RequireRole` 换回包级 `RequireRole` → `pkg/auth` 与 `cmd/analysis` **两层**同时变红；
  3. 把 `Classify` 的兜底改成 `SideEffectRead`（fail-open）→ `TestClassify_FailClosed` + `TestTools_UnclassifiedTool_FailsClosed` 同时变红。
- **顺带发现（新登记 AUD-19）**：`/api/paper/*` 全部 10 个端点**是死代码** —— `registerPaperTradingRoutes` **零调用方**，全仓无前端/文档引用。原本担心它是「第二处无鉴权写端点」，实为「根本没挂上」。**给不可达代码加 RBAC 是纯装饰**，故不在 AUD-02 内处理，改走 AUD-19（裁决：删除）。这也修正了本次勘察初期的判断。

| ID | 任务 | 位置 | 验收 |
|----|------|------|------|
| AUD-19 | **删除 `/api/paper/*` 死代码**：`registerPaperTradingRoutes` 零调用方，10 个端点（含 POST `/paper/start`、POST `/paper/orders`、DELETE `/paper/orders/:id`）从未挂上任何 router，全仓无引用 | cmd/analysis/handlers_paper_trading.go（整文件，约 300+ 行）+ `live.NewSimulatedBroker`/`NewSimulatedDataFeed` 若因此零调用则一并裁决 | 删前 grep 正向确认零调用方；删后 build 全绿；若 backend 确有规划用途则改为「登记到 main.go 并接 RBAC」，二选一须写明理由 |

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

**已完成 7 项**：AUD-06（印花税）、AUD-07（涨跌停板块分档 + 分取整）、
AUD-08（`*ST` 识别，与 AUD-07 同一 commit 合入）、AUD-09（整手归一）、
AUD-10（MockTrader 读路径写穿共享对象）、AUD-11（沙箱资源限制真正作用在子进程）、
AUD-12（CI 补 `-race` 门禁 + frontend job）。

> **AUD-06 落地说明（2026-09-21）**：`DefaultStampTaxRate` 由 `0.001` 改为 `0.0005`，
> 沿革为 **2023-08-28 起 0.1% 减半至 0.05%**（财政部/税务总局 2023 年第 39 号公告），
> 原注释写的「0.2% → 0.1%」两头都错 2 倍、方向一致，所以错误自洽难发现。
> **波及面比登记的大**：除 `pkg/fees/ashare.go`，还必须在
> ① `config/analysis-service.yaml`（`trading.stamp_tax_rate: 0.001` —— 这个值**覆盖**常量默认值，
> 不改则等于没改）、② `docs/ARCHITECTURE.md` 费率示例、③ 三个固化旧值的测试
> （`pkg/fees/ashare_test.go`、`pkg/portfolio/portfolio_test.go` 的 `26.2`、
> `cmd/analysis/handlers_risk_execution_test.go` 的 fixture）同步修正。
> 护栏三处实证：改回 `0.001` → 三条测试变红且报出金额（100 vs 50）；改成 `0.00025` →
> 日期沿革测试也报红。
> **顺带发现（新登记 AUD-20）**：`Tracker.feeSchedule()` 返回**固定**费率，不看
> `ExecuteTrade` 已有的 `timestamp` —— 回测窗口若横跨 2023-08-28，卖出费率全程用同一个值。
> 这是加功能而非修 bug，单独决策。

> **AUD-07 + AUD-08 落地说明（2026-09-21，同一 commit）**：两项共享同一段
> `engine_daily.go` 的 if-else，故合并实施 —— 先抽纯函数，AUD-08 复用。
>
> **AUD-07**：新增 `pkg/backtest/pricelimit.go`，把内联分支抽成纯函数
> `resolvePriceLimit(in, cfg)`，优先级 **新股 > ST（且 ST 受板块约束）> 板块**。
> 上限/下限价经 `LimitPrices()` 做 `math.Round(x*100)/100`（分取整）后再比较。
> 关键设计：**ST 不无条件用 ST 费率** —— 创业板/科创板（注册制）的风险警示股
> 保持板块 ±20%，北交所保持 ±30%，只有主板 ST 走 5%/10% 档。
> naive 的 `if isST { return stRate }` 会让创业板 ST 错用 5%。
>
> **审计报告漏掉的两处（本次新发现）**：
> ① **主板 ST 已于 2026-07-06 从 ±5% 上调至 ±10%**（沪深北三所 2026-04 修订
> 交易规则），故新增 `st_before` 配置 + `STBefore` 常量，按 `AsOf` 日期分段；
> 创业板/科创板 ST 不受影响。② **`marketdata.ClassifySymbol` 不认 `301`
> （创业板注册制新增段）和 `689`（科创板 CDR）** → 这两类票的涨跌停被当成
> 主板 10%，而 `pkg/live/price_cage.go` 同样受影响。已在 `pkg/marketdata/board.go`
> 根因处修复（`301xxx`/`689xxx` → 对应板块），而非在调用点打补丁。
>
> **AUD-08**：`hasSTPrefix` 原来的 `name[:2] == "ST"` **永远匹配不到 3 字符的
> `*ST`**（`name[:2]` 是 `"*S"`）。改为前缀匹配四种模式（`*ST`/`S*ST`/`SST`/`ST`）。
> 测试 `TestEngine_hasSTPrefix` 原先把 bug 写成了规格（`{"*STXYZ.SH", false}`），
> 同步修正为真实股票名并补 `SST`/`S*ST`/空串/带空格用例 —— **故与实现同一 commit**。
>
> **配置覆盖陷阱（同 AUD-06）**：`config/analysis-service.yaml` 的
> `price_limit.st` / `price_limit.new` 会覆盖 Go 常量，已同步改 `st: 0.10`、
> 增 `st_before: 0.05`。
>
> **护栏五处实证**（每处故意破坏 → 确认变红 → 恢复）：
> ① ST 判定退回 `name[:2]` → `TestEngine_hasSTPrefix` + `TestIsRiskWarningName` 精确变红；
> ② `301`/`689` 退回 `BoardUnknown` → `pkg/marketdata` 与 `pkg/backtest` **两层**同时变红；
> ③ ST 分支忽略板块（naive `if isST`）→ 4 个注册制 ST 用例变红（0.2 vs 0.1）；
> ④ `roundToCent` 改 `math.Trunc` → 验收项 **10.05→11.06** 报出 `11.05`；
> ⑤ 日期分段失效 → 两个 pre-change 用例变红（0.05 vs 0.10）。
>
> **过程中的一个方法学坑**：第 ③ 处破坏最初写成 `case A, B:` + 空 body，
> 以为是 fall-through，**Go 的 case 并不 fall through** —— 空 body 直接退出 switch，
> 等于没改，测试自然不红。**「护栏没变红」要先怀疑破坏本身无效，再怀疑护栏**。
> （另有一次破坏写成 `return price` 导致 `math` 未使用**编译失败** ——
> build failure 不是 test failure，不能算护栏生效。）
>
> **已知偏差（用户裁定保留，本次不修）**：新股档 `new_stock_days: 60` 不准 ——
> 真实规则是「上市首日 + 前 5 个交易日无涨跌幅限制，其后按板块」，与「60 个
> 交易日内一律 ±20%」不同。已在 `pricelimit.go` 的 `PriceLimitInput.TradeDays`
> 注释里明确写出「这是历史（不正确）行为」。同批修复会让 AUD-07 过大，故单列。
>
> **`PriceLimitInput.TradeDays` 零值语义**：零值 = 「今天上市」（必然 < 阈值），
> 没有 unset 哨兵值。写测试时必须显式传成熟交易日数（`matureTradeDays`），
> 否则所有用例都会被 New 分支吞掉 —— 这个坑在开发时真实踩到过。

> **AUD-09 落地说明（2026-09-21）**：勘察证实**登记写的验收标准本身是错的**。
>
> **现状（正向确认，不是负向证据）**：`Weight→shares` 换算在
> `pkg/risk/manager.go`（单个 + 批量）与 `pkg/risk/volatility.go` 共三处，
> 全部是 `math.Floor(positionValue / price)` —— 只取整到**整股**，
> 不认整手。实盘侧 `pkg/live/broker/xtp/xtp.go` 反而已有约束
> （`int(quantity)%100 != 0` → 报错），**回测与实盘不一致**：回测算出的
> 5013 股在实盘会被券商拒绝。这是「两个机制各自对、接起来就错」的第五例。
>
> **登记标准「下单量恒为 100 倍数」对科创板/北交所是错的**（已查官方原文）：
>
> | 板块 | 买入申报数量 | 出处 |
> |------|-------------|------|
> | 沪/深主板、创业板 | **100 股或其整数倍** | 上交所/深交所《交易规则（2023 修订）》3.3.8 |
> | 科创板 | **≥200 股**，1 股递增 | 上交所《交易规则（2023 修订）》6.1.7 |
> | 北交所 | **≥100 股** | 北交所《交易规则》3.3.8（2026-07-06 施行） |
>
> 关键：**只有主板/创业板是 bug**。科创/北交所当前 `math.Floor` 的结果
> （如 5013 股）**恰好合法**（≥下限且允许 1 股递增）；强行统一成 100 倍数
> 反而会给它们引入系统性少买偏差（5013 → 5000）。
>
> **顺带查证并否证**：2023-08 讨论的「100+1」（主板/创业板改为
> 100 股起、1 股递增）**从未落地** —— 当时只是「拟调整 / 研究」，
> 现行 2023 年修订版仍是「100 股整数倍」。按「100+1 已生效」写的代码是错的。
>
> **实现**：新增 `pkg/risk/lot.go`，`NormalizeOrderQuantity(shares, symbol)`
> 按 `marketdata.ClassifySymbol` 分档；三处 sizer 改为调用它（`math` import
> 从 `manager.go` 移除 —— 它只有这两个用途）。常量 `LotSize=100` /
> `STARMinShares=200` / `BSEMinShares=100` 是**监管硬规则，不进配置层**
> （已 grep 确认 `risk_manager.*` 无同名覆盖项；operator 改了会产生非法订单）。
> `BoardUnknown` 回落到主板规则 —— 三种规则里最严，误分类也仍合法。
>
> **不足下限时提到下限**（用户裁定）：50 股 → 100 股，而非跳过。
> **代价已知**：会放大订单，最坏情况（输入 1 股）达 100 倍；资金不足时
> `Tracker` 的 `insufficient cash` 会兜底报错。另设一条有意边界：
> **输入 < 1 股归 0**（`positionValue/price` 很小时会产生 0.4 股这种商，
> 提到 100 股等于凭空放大 250 倍 —— 「不足一手」是仓位决策，
> 「不足一股」是错误）。
>
> **顺带修正 AUD-07 的遗漏**：`pkg/marketdata/board.go` 包注释仍写着
> 「ST 股票 ±5%; *ST 股票 ±5%」—— 对注册制板块是错的（应为保持板块档
> ±20%/±30%），已改为分板块说明并指向 `pricelimit.go`。
>
> **测试缺口（勘察时发现）**：现有测试只断言 `ps.Size > 0` / `>= 0`，
> **从不检查具体数值**，所以改动不会被发现。新增 `pkg/risk/lot_test.go`：
> 逐例表驱动 + **规格级性质断言**（输出恒满足板块规则）+ 端到端接线测试
> （构造 `617.28` 这个非整手商，使主板得 600、科创/北交所得 617 ——
> 三者互不相同，能同时证明「分档生效」与「归一被调用」）。
>
> **护栏五处实证**（每处故意破坏 → 确认变红 → 恢复）：
> ① 忽略板块统一 floor → 主板端到端红（600 vs 617）；
> ② 科创板也用 100 倍数 → 只红科创用例（5013 vs 5000）；
> ③ 提到下限改归 0 → 提到下限用例 + 现有 `CalculatePositionsBatch_Basic` 红；
> ④ `shares < 1` 改 `<= 0` → 断崖与 sub-share 用例红；
> ⑤ sizer 不调用归一（改回 `math.Floor`）→ 端到端红（接线本身被验证）。
> 过程中一处破坏写成 `_ = symbol` 导致 `marketdata` 未使用 → **编译失败**，
> 按既有规矩不计入（build failure ≠ test failure），已改成行为有效的破坏。

> **AUD-10 落地说明（2026-09-21）**：`GetPositions` / `GetAccount` 在 `RLock` 下
> 经 `range` 拿到的 `*PositionInfo` **写回了刷新价**。`positions` 是
> `map[string]*PositionInfo`，range 变量是**共享对象的指针**，读锁下写它
> 既是数据竞争，也把「谁最后读谁说了算」的临时价格**固化**进了持仓记录。
>
> **并发场景是真实的、不是理论风险**：`cmd/analysis/alert_loop.go` 的后台
> 周期任务调 `GetAccount`，而 `handlers_execution.go` / `handlers_paper_trading.go`
> 的 HTTP handler 同时读 —— 两个 reader 打同一个字段。
>
> **实现**：新增 `snapshot(pos) PositionInfo` helper，先 `cp := *pos` 再在副本上
> 取价；两处调用点改为在副本上算 `MarketValue` / `UnrealizedPnL`。
> `PositionInfo` 是**扁平值类型**（无指针/切片/map），故结构体拷贝即深拷贝。
> `GetOrder` 早已是这个写法（`copy := *order`），本项是把它补齐到另两处。
>
> **已核实不会破坏紧急平仓**：`flattenPosition` 用 `pos.CurrentPrice` 只是
> **feed 挂掉时的回落价** —— 它在第 464-468 行自己调 `PriceProvider`，
> 只有取到 `<= 0` 才用 stored price。修复不动这条路径。
>
> **护栏四处实证**（每处故意破坏 → 确认变红 → 恢复）：
> ① `GetPositions` 改回写 `pos.` → 红 2 例（`GetPositions_DoesNotWriteThrough`
> + `ConcurrentReads`），**`GetAccount` 用例保持绿**；
> ② `GetAccount` 同理 → 红 2 例，**`GetPositions` 保持绿**；
> ③ 把 bug 塞进 `snapshot` helper 内部（未来重构最可能重新引入的位置）→ 红 3 例；
> ④ 删掉 `snapshot` 里的取价 → 红 5 例，而 `NoPriceProviderLeavesStoredValuesAlone`
> 与 `ReadsAreRepeatable` **保持绿**（本就不依赖取价）。
> ①② 的「只红该红的」是这组护栏有区分度的证据。
>
> **测试无法依赖 `-race`**：本机无 gcc，`-race` 跑不起来。故断言
> **直接检查 `mt.positions[...]` 的存储态**（同包可见），不靠竞争检测器；
> `TestMockTrader_ConcurrentReads` 作为第二道防线，等 AUD-12 的 CI `-race` 生效。
>
> **过程中的两个坑（都是「零值不是没填」的变体）**：
> ① `MockTraderConfig` 用 `<= 0` 判「未设置」，测试传 `SlippageRate: 0`
> 被替换成 `pkg/fees` 默认值 `0.0001`，成交价不是 `10.0` 而是 `10.0001`；
> ② `SubmitOrder` 对**市价单**用 `PriceProvider` 定价（忽略传入 price），
> 而测试恰恰要构造「provider 价 ≠ 成交价」才能区分「已刷新」与「未刷新」。
> 解法：改用**限价单**建仓，并从 `mt.positions[sym].CurrentPrice` **读回**真实
> 成交价（不假设），再用 `math.Abs(fillPrice - marketPrice) > 1e-6` 做前置断言。
>
> **顺带发现（新登记 AUD-23）**：`pkg/live/engine.go:173-176`
> `func (e *LiveEngine) GetPortfolio() *domain.Portfolio { return e.portfolio }`
> —— 返回内部指针且**完全不加锁**，调用方可直接改写引擎状态。与 AUD-10 同族
> （读访问器暴露/改写共享状态），但对象不同（引擎组合 vs 模拟盘持仓），单列。

> **AUD-11 落地说明（2026-09-21）**：登记只说「Windows 无 rlimit 能力时 fail-closed」，
> 勘察发现**真正危险的是 POSIX 侧** —— 而且审计报告 H7 的「生产 Linux 不受影响」
> 这句判断是错的。
>
> **两个缺陷，同一处代码**：
>
> ① **Windows 静默 no-op**（登记项，属实）：`rlimit_windows.go` 的 `applyLimits`
> 无条件 `return nil`，生产传的 `1 GiB / 25 CPU 秒 / 256 fd` **一个都没生效**，
> 日志里也没有任何痕迹。这直接违背 ODR-020 自己写下的「不会静默忽略」设计目标。
>
> ② **POSIX 把限制打在守护进程身上**（本次新发现，Critical）：`rlimit_posix.go`
> 的 `applyLimits` 最终调 `applyLimitsPreExec(l)` → `syscall.Setrlimit(...)`，
> 而 `Run()` 里**并没有** `syscall.Exec`（注释写「then call syscall.Exec to
> replace ourselves」，代码里根本没这回事）。于是限制落在**调用者自己**身上，
> 而调用者就是 analysis-service 守护进程：
>
> | 生产值 | 对守护进程的实际后果 |
> |--------|---------------------|
> | `CPUSeconds: 25` | 累计 25 秒 CPU 后 **SIGXCPU 杀死守护进程**（Go 运行时不为 SIGXCPU 装 handler，默认动作即终止） |
> | `MemoryBytes: 1<<30` | 守护进程地址空间被限 1 GiB，跑回测时极可能 OOM |
> | `OpenFiles: 256` | 一个 HTTP 服务器被限 256 个 fd |
>
> 子进程只是**顺带继承**了这些限制。ODR-020 的作者明确写了「setrlimit 在父进程
> 调用」，但只承认了「并发 Run() 会互相污染」，**没有意识到被限的是守护进程本身**。
>
> **为什么从没被发现**：全仓**没有任何测试传非零 limits** 走过
> `applyLimitsPreExec` —— 这条路只在生产第一次 `go build` 时才第一次执行。
> 又一次「测试全绿 = 没有测试」。
>
> **修复**：`applyLimits`/`applyLimitsPreExec` 删除，改为 `prepareArgv` 在
> **命令行层面**把限制交给子进程：`sh -c '<ulimit …>; exec "$0" "$@"'`。
> shell 给自己设限后 exec 目标，限制恰好落在目标进程上，父进程毫发无损。
> 每个 `ulimit` 都带 `|| { echo 'runner: cannot set …' >&2; exit 125; }` ——
> 设不上就**中止**，绝不留下一个「以为有限制、实际没有」的子进程。
>
> - **零 limits 时不做包装**：不付 shell 开销，也不要求 PATH 里有 `sh`。
> - **参数走 `$0`/`$@` 而非字符串拼接**：shell 没有第二次解析机会
>   （含空格/通配符/`$` 的参数原样抵达，已实测）。
> - **`ulimit -v` 单位是 KiB、`-f` 是 512 字节块，且 0 = 不限** →
>   字节数**向上取整**，1 字节的请求得到 `-v 1` 而不是 `-v 0`（零值陷阱的又一例）。
> - **exit 125 必须配 stderr marker** 才算 setup failure —— 否则一个自己退出 125
>   的子进程会被误报成沙箱故障。
> - `rlimit_linux.go` / `rlimit_darwin.go` 的 `setNProc` 已无用途（NPROC 现在走
>   `ulimit -u`），一并删除。
>
> **Windows 侧 fail-closed + 显式逃生阀**（用户裁定）：默认拒绝执行并报
> `ErrLimitsUnsupported`（消息里带上「请求了哪些限制」和逃生阀名字）；
> 本地开发可设 `SANDBOX_ALLOW_UNENFORCED_LIMITS=1` 打开，此时
> `WithOnUnenforcedLimits` 回调会 WARN 一次「本次构建没有资源限制」——
> **可降级，但必须可观测**。
>
> **护栏五处实证**（每处故意破坏 → 确认变红 → 恢复；POSIX 侧在
> `golang:1.25-alpine` 容器里真跑，本机 Windows 无 Linux 能力）：
>
> | 破坏 | 变红 | 保持绿 |
> |------|------|--------|
> | ① 把 setrlimit 塞回父进程（原 bug） | `TestRun_LimitsDoNotTouchTheParent` 的 CPU/AS/NOFILE 三条断言 | 排除该测试后其余 4 条 limit 测试全绿 |
> | ② Windows 恢复成静默 no-op | 4 例（拒绝契约 ×3 + 未生效回调 ×1） | 12 例，含全部零值路径 |
> | ③ 删掉 `ulimit` 的 `\|\|` 失败分支 | 2 例（脚本结构守卫 + 行为级 fail-closed） | 19 例 |
> | ④ `exec "$0" "$@"` 改成字符串拼接 | 2 例（参数保真 + `LimitsReachTheChild` 连带） | 19 例 |
> | ⑤ 去掉 marker 校验（任何 125 都算） | 1 例（误报守卫） | 20 例 |
>
> ①②③⑤ 都是「只红该红的」。④ 的连带红本身是旁证：拼接会把
> `sh -c "…"` 也拆词，说明拼接对真实命令同样有害。
>
> **①的连带现象值得记一笔**：串行跑时 `LimitsReachTheChild` 与
> `WrapperPreservesArguments` 也会红 —— 因为父进程的 **hard limit 被降后无法再升回**
> （非特权进程只能降不能升），后续测试的 `ulimit` 直接 EPERM。这不是测试脆弱，
> 而是**原 bug 的爆炸半径本来就是进程级**的。
>
> **本机能力补充**：POSIX 测试在 Windows 上跑不了（build tag 排除），本机无 gcc、
> WSL 被安全策略禁用。改用 **docker + `golang:1.25-alpine` 挂载宿主模块缓存**
> 在真实 Linux 上执行 —— 这是本次唯一能真正验证 ① 的途径，也为 AUD-12 的
> `-race` 验证提供了可复用的手法（见工作记忆）。
>
> **顺带实测**：
> - 生产值 `RLIMIT_AS = 1 GiB` 对 `go build ./pkg/risk` **够用**（实测通过），
>   所以修好之后不会立刻把构建打挂。
> - `ulimit -u` 的可移植性：**bash ✅ / busybox ash ✅ / dash ❌**
>   （Debian/Ubuntu 的 `/bin/sh` 报 "Illegal option -u"）。生产没设 `NumProcs`，
>   故是地雷而非现患 → 新登记 **AUD-25**。
> - 源码级守卫 `TestNoSetrlimitOnTheParent`：因为行为级护栏只在 POSIX 跑，
>   加了一条**全平台可执行**的源码扫描（包内非测试文件不得出现 `Setrlimit`），
>   让若曦在 Windows 本机 `go test` 就能发现回归。已单独破坏验证过会变红。
>
> **新登记**：AUD-24（Windows Job Object）、AUD-25（`ulimit -u` 在 dash 上不可用）、
> AUD-26（runner 测试在 Windows 上依赖 PATH 里有 POSIX userland）。

> **AUD-12 落地说明（2026-09-21）**：登记只要求「Test 加 `-race` + 新增 frontend
> job」，但**开门前先实测了一次存量**，结果发现竞争，所以本次一并修掉 ——
> 否则门禁上线即红，而那正是登记里担心的情形。
>
> **存量实测**（`go test -race ./...`，docker + `golang:1.25`，gcc 14.2）：
> **17~18 处 `WARNING: DATA RACE`、16 个用例失败，全部集中在 `cmd/analysis`**。
> 竞争地址只有两个，都是 gin 的包级全局：
>
> | 被写对象 | 写方 | 读方 |
> |---------|------|------|
> | `ginMode`（`mode.go:72`） | 测试助手 `rbacTestRouter` / `toolsRBACRouter` 里的 `gin.SetMode(gin.TestMode)` | 另一个并行测试注册路由时 `RouterGroup.handle` → `debugPrintRoute` → `IsDebugging()` |
> | `modeName`（`mode.go:77`） | 同上 | 同上 |
>
> **生产代码无涉**：竞争栈里出现的 `handlers_execution.go:118` /
> `handlers_tools.go:89` 只是**读受害者**（注册路由时读全局 mode）。元凶是测试在
> `t.Parallel()` 下各自调 `gin.SetMode` —— 它用**普通赋值**写全局（不是原子操作），
> 两个并行测试互相竞争，也与别的并行测试的读竞争。
>
> **修法（沿用仓内既有先例）**：`pkg/auth/middleware_test.go` 早在 S7-P0-14 /
> ODR-043 就踩过同一个坑，并在那里写下「Individual tests must NOT call
> `gin.SetMode()` themselves」+ `TestMain` 集中设置。本次把该先例推广到
> `cmd/analysis`：新增 `cmd/analysis/main_test.go` 的 `TestMain`，删掉 15 个测试
> 文件里的 **31 处**逐测试 `gin.SetMode` 调用（含 `handlers_compliance_test.go`
> 的 `func init()` 变体）。**先例存在却没人推广，本身就是这次竞争能活下来的原因。**
>
> **护栏两向实证**：
> - 把 `gin.SetMode(gin.TestMode)` 塞回 `toolsRBACRouter` → `-race` **变红**
>   （EXIT=1、6 处竞争、10 个用例失败，栈指向 `handlers_rbac_test.go` 与 gin 全局）。
> - **同一个破坏，不加 `-race` 时 `ok` / EXIT=0** —— 这正是 AUD-12 的意义：
>   旧门禁对这类竞争**完全不可见**。
> - 恢复后 `go build ./... && go vet ./... && go test ./... -count=1 -race` 全绿。
>
> **CI 改动**：`Test` 步骤 → `go test ./... -count=1 -race`（与 AGENTS.md §8 规范 1
> 对齐 —— 该规范早已存在，缺的从来不是要求而是执法者）；新增 `frontend` job
> （node 22 + `npm ci` + lint / typecheck / test，`working-directory: web`）。
> 前端三项**开门前已实测全绿**：lint `0 errors`（820 warnings，退出码 0）、
> typecheck 通过、vitest 15 files / 172 tests 全过；`npm ci --dry-run` 也验过
> lockfile 同步，不会因 `npm ci` 本身失败。
>
> **没有加 `gofmt -l .` 检查**（报告里列为「可选」）：本仓 blob 里存的是 **CRLF**
> 且无 `.gitattributes`，`gofmt -l .` 会在 CI 上列出**全部** Go 文件 → 开门即红。
> 这是既有债，另立 AUD-27。
>
> **新登记**：AUD-27（`gofmt -l .` 在本仓恒失败）、AUD-28（其余 5 个包仍有
> 「测试各自调 `gin.SetMode`」的模式）、AUD-29（`buildRouter` 运行期按日志格式写
> gin 全局 mode）。

| ID | 任务 | 位置 | 验收 |
|----|------|------|------|
| AUD-20 | **费率史按日期分段**（AUD-06 的延伸，非登记项）：`feeSchedule()` 不接收日期，回测跨费率变动日时全程用同一费率。需在 `Tracker.ExecuteTrade(timestamp)` 处按日期选档（2023-08-28 前后 0.1% / 0.05%），并考虑未来更多变动（佣金、过户费也有沿革）。**决策点**：是否值得做 —— 若曦的回测窗口是否常跨 2023-08-28 | `pkg/backtest/tracker/tracker.go#L110-117`、`pkg/fees/ashare.go` | 跨 2023-08-28 的窗口，前后卖出印花税分别为 0.1% / 0.05%；不跨的窗口行为不变 |
| AUD-13 | docker-compose PG/Redis 端口绑 `127.0.0.1:`（Redis requirepass 涉及全部服务 REDIS_URL 联动，另立任务） | `docker-compose.yml#L27-28,39-40` | 宿主机外主机探测 5432/6379 不通 |
| AUD-21 | **XTP 整手检查对科创板/北交所过严**（AUD-09 的实盘侧延伸，非登记项）：`int(quantity)%100 != 0` 一律报错，但科创板允许「≥200 股、1 股递增」、北交所「≥100 股」，617 股在科创板是合法单却被拒。**待查证**：XTP 柜台是否支持科创板 1 股递增 —— 若券商柜台本身只收 100 倍数，则这是券商限制而非本仓 bug，应改为注释说明；若支持，则需按板块放宽 | `pkg/live/broker/xtp/xtp.go#L373-375` | 科创板/北交所合法单不被本地拒单；或明确记录为券商限制 |
| AUD-22 | **北交所风险警示股当日买入上限**（非登记项）：北交所《交易规则》4.5.4 —— 投资者当日累计买入单只风险警示股票**不得超过 20 万股**（竞价 + 大宗 + 盘后固定价格合并计算）。当前引擎无此约束，回测会允许超限买入。沪深是否有同类上限需一并查证 | 下单量校验处（与 AUD-09 同域） | 单日累计买入 ST 股超 20 万股时被拒 |
| AUD-23 | **`LiveEngine.GetPortfolio()` 无锁返回内部指针**（非登记项，AUD-10 顺带发现）：返回 `e.portfolio` 本体，调用方既能读到半更新状态，也能直接改写引擎组合。需裁决：改为返回值拷贝 / 加锁 + 文档化「只读」契约 / 保持现状但在 godoc 明示不可变约定。**先勘察调用方是否真的写它** —— 若无人写，可能只需文档化 | `pkg/live/engine.go#L173-176` | 并发调用 `GetPortfolio` + 组合更新时无竞争；外部改动不回流引擎 |
| AUD-24 | **Windows Job Object 实现**（AUD-11 的后续增强）：`CreateJobObject` + `SetInformationJobObject`（`JOB_OBJECT_LIMIT_PROCESS_MEMORY` / `JOB_OBJECT_LIMIT_ACTIVE_PROCESS` / `JOB_OBJECT_LIMIT_JOB_MEMORY`）+ `AssignProcessToJobObject`。做完之后 Windows 才能真正执行受限子进程，`ErrLimitsUnsupported` 就不再是常态。**注意**：Job Object 需要 `cmd.SysProcAttr.CreationFlags` 里加 `CREATE_SUSPENDED` 才能在 exec 前挂载 | `internal/sandbox/runner/rlimit_windows.go` | Windows 上 `Limits{MemoryBytes: …}` 真正生效；不需要逃生阀即可构建 |
| AUD-25 | **`ulimit -u` 在 dash 上不可用**（AUD-11 顺带发现，非登记项）：`ulimit -u` 的可移植性是 **bash ✅ / busybox ash ✅ / dash ❌**（Debian/Ubuntu 的 `/bin/sh` 报 "Illegal option -u"）。故 `Limits.NumProcs` 在 Debian/Ubuntu 上会让构建 fail-closed 报 `ErrLimitSetupFailed`。生产组合根没设 `NumProcs`，所以是地雷不是现患。**决策点**：① 探测 shell 能力并在缺失时报 `ErrLimitsUnsupported`（语义更准）；② 改走 cgroup `pids.max`；③ 把 `NumProcs` 从 API 移除，只留平台原生实现 | `internal/sandbox/runner/rlimit_posix.go` | Debian/Ubuntu 上设 `NumProcs` 时给出「本平台不支持」而非含糊的 setup 失败 |
| AUD-26 | **runner 测试在 Windows 上依赖 PATH 里有 POSIX userland**（AUD-11 顺带发现，非登记项）：`TestRun_ExitZero` 用 `echo`、`TestRun_Timeout` 用 `sleep`、`TestRun_StdinAndEnv` 用 `sh`、`TestRun_NonZeroExit` 用 `false`、`TestRunExitCode` 用 `sh -c`。本机因为装了 Git for Windows 才全绿，**裸 Windows（无 Git Bash）会失败**。CI 跑 Linux 故不影响门禁，但会让「本机全绿」这个信号在裸 Windows 上失真。修法：改成用 `os.Executable()` 自举（测试二进制支持 `-test.run=TestHelperProcess` 模式）或按平台选命令 | `internal/sandbox/runner/runner_test.go` | 裸 Windows 上 `go test ./internal/sandbox/runner/` 也全绿 |
| AUD-27 | **`gofmt -l .` 在本仓恒失败**（AUD-12 顺带发现，非登记项）：仓库 blob 里存的是 **CRLF**（实测 `git show HEAD:pkg/risk/lot.go` 有 99 行带 CR）、**没有 `.gitattributes`**、`core.autocrlf=true`。于是 `gofmt -l .` 在 Linux CI 与 Windows 本机都会列出**全部** Go 文件 —— AGENTS.md §8 的「`gofmt -l .` 无输出」检查项因此在**所有平台上都失效**（本机一直靠「只对本次改动的文件跑」绕过）。**修法（择一）**：① 加 `.gitattributes`（`*.go text eol=lf`）+ `git add --renormalize .` —— 一次性大 diff，但从此换行符一致；② 保留 CRLF，把检查改成「归一化后再 gofmt」（`tr -d '\r' \| gofmt -d`，每文件一个子进程，慢但可行）；③ 承认现状，把 AGENTS.md §8 那条删掉。**这也是 AUD-12 没有加 `gofmt` CI 步骤的原因** | `.gitattributes`（缺失）、`AGENTS.md#L449` | `gofmt -l .` 在 CI 与本机都无输出；或明确记录该检查项已作废 |
| AUD-28 | **其余 5 个包仍有「测试各自调 `gin.SetMode`」的模式**（AUD-12 顺带发现，非登记项）：`cmd/data/handlers_ingest_test.go`、`cmd/data/setup_test.go`、`internal/httpserver/cors_test.go`、`internal/httpserver/errors_test.go`、`pkg/api/versioning_test.go`。**今天不报竞争** —— 实测这些包都没用 `t.Parallel()`，所以是**潜在雷**而非现患：一旦有人给这些测试加并行，就会复现 AUD-12 修掉的同类竞争。修法同 AUD-12（`TestMain` 集中设置 + 删掉逐测试调用） | 上述 5 个文件 | 这些包加 `t.Parallel()` 后 `-race` 仍绿 |
| AUD-29 | **`buildRouter` 在运行期按日志格式写 gin 全局 mode**（AUD-12 顺带发现，非登记项）：`if v.GetString("logging.format") == "json" { gin.SetMode(gin.ReleaseMode) }`。两个问题：① **语义可疑** —— 日志格式与 gin 运行模式是两件事，用前者决定后者没有依据；② **运行期改进程级全局** —— 当前测试都用 `logging: level: info`，所以没触发；只要有人写一条 `format: json` 的测试并与并行测试共存，就会复现同类竞争，且这次栈里会出现**生产文件**。`cmd/data/setup.go:220`、`cmd/strategy/main.go:102` 同样写法。**决策点**：是否改由语义相符的配置项（如显式 `server.gin_mode`）决定，并在启动早期设置一次 | `cmd/analysis/setup.go#L638`、`cmd/data/setup.go#L220`、`cmd/strategy/main.go#L102` | gin mode 由语义相符的配置项决定，且在启动期设置一次 |

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

**2026-09-21 全栈审查（ODR-065）新增 5 项**（Medium/Low）：

> **登记缺口（2026-09-21 复核时发现）**：ODR-065 的 24 项里有 3 项在登记环节掉了 ——
> M4（staticcheck 可绕过）、L2（live engine 组合状态，报告自标"未逐行复核"）、
> L3（legacy HTML 残留）。原表只有 AUD-14/AUD-15 两行却写"新增 3 项"，那第 3 项
> 是已被判定为误报的 AUD-L1。现补为 AUD-16/17/18，24 项全部有主。

| ID | 任务 | 位置 |
|----|------|------|
| AUD-14 | AGENTS.md 校准：§1-3 按 ADR-023/024 现实重写（ADR-021/022 已入 superseded-adr）/ 表数口径改"内联 DDL 22 张（唯一执行路径）"/ `ingest.raw`·`research.*` 改"已落盘"/ Rule 2 的 `docs/odr/` → `docs/archive/odr/`（该目录已不存在）/ `pkg/tools/server.go` → `tool.go`；`migrations/` 与 `docs/migrations/` 目录首加 README"此目录不执行，加表改 postgres.go migrate() 数组"（是否物理移入 archive 另立 Cleanup ODR） | AGENTS.md + migrations/；`tools/check_doc_links.py` 增加 `--include-archive` 开关（现显式跳过 archive/，而 ODR 与报告全在该目录，其死链因此永不被告警——ODR-065 报告自身就有 2 处死链未被抓到） |
| AUD-15 | 删 `e2e/tests/ai-research.spec.ts`（打已删除的 :8086/cmd-ai，恒失败）；`fundamentals_detail` 读取处加空集防御（行数 0 → 显式报错，杜绝纵向因子静默拿空集，待 EQD-P1-2 摄取补齐） | e2e/tests + 纵向因子读取处 |
| AUD-16 | **补复核 L2**：live engine 组合状态更新路径"存在不触发场景"（D4 子代理报告，ODR-065 自标**未逐行复核**，复核也把它列进未覆盖项）—— 逐行读组合状态更新路径，确认是否真有分支导致状态不更新；坐实则升级为缺陷并定级，证伪则关闭 | `pkg/live/engine.go` |
| AUD-17 | **M4 威胁模型声明**：`internal/sandbox/staticcheck` 是 14 条正则黑名单，经包别名 / 变量间接调用 / 反射 / 字符串拼接可绕过。注释里明示威胁模型（防 AI 生成代码的**无意**违规，**不防**有意攻击者），别让人误以为它是安全边界；中期评估 gosec / go-ast 分析替代 | `internal/sandbox/staticcheck/staticcheck.go` |
| AUD-18 | **L3 legacy HTML 去留裁决**：`cmd/analysis/static/` 已标 deprecated 但无删除时间表 —— 无限期共存等于两套 UI 都要维护。给出裁决 + 时间表 | `cmd/analysis/static/` |

---

## 已冻结（本次定位重构后不再投入）

- 多 agent 投票 / arbitrator 仲裁机制 —— 错误相关，放大偏差且不可解释
- 演化算法直接产出交易信号 —— 产出落在"高统计/无因果"象限，不会被采用（改为漏斗顶部的候选生成器）
- EquityDeep 作为"独立工作面"—— 改为数据底座（产业链图谱）+ 研究洞察库

---

_完成一项删一项。本文件超过 5 页 = 该拆分项目了。_
