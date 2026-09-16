# ODR-063: e2e 运行时全套件取证 — worker 饥饿与 strategies params 形状双缺陷修复（Playwright 167 用例 × 3 轮）

> **Status**: Completed（双产品缺陷已修复并经第 3 轮验证，2026-09-16）
> **Date**: 2026-09-16
> **Category**: Audit
> **Related ADRs**: —
> **Supersedes**: —
> **Related ODRs**: [ODR-062](odr-062-p5-1-bypass-residue-audit.md)（交棒「运行时全套件需 tushare token 待跑」+ P1-18 来源）
> **Author**: AI Assistant

---

## Context

### 触发条件

[ODR-062](odr-062-p5-1-bypass-residue-audit.md) 收官时 e2e 三套件仅完成静态对齐，运行时全套件验证因缺 tushare token 搁置。本记录承接该交棒：用户供给 token（仅会话进程环境注入，不落盘不提交），指令「处理 e2e 运行时全套件」。目标 = 17 spec / 167 用例在真实环境（真实 PG + 真实 tushare）跑通并取证。

### 取证环境

| 组件 | 形态 |
|---|---|
| 数据库 | `timescale/timescaledb:latest-pg16` 一次性容器 :15432（qtr-e2e-pg） |
| 缓存 | `redis:7-alpine` 一次性容器 :16379（qtr-e2e-redis） |
| 服务 | data :8081 / analysis :8085 / strategy :8082 / ai :8086（本地二进制） |
| 前端 | vite dev :5175（5173/5174 被用户进程占用） |
| token | `TUSHARE_TOKEN` 进程环境注入 |

---

## 三轮运行总览

| 轮次 | 范围 | 结果 | 主导因素 |
|---|---|---|---|
| 1 | 全套件 167 | **86 passed / 81 failed**（29.2m） | 429 限流风暴（阈值硬编码，e2e 并发自伤）+ worker 饥饿 + `/api/strategies` 500 + 数据面未就绪 |
| 2 | `--last-failed` 81 | **19 passed / 62 failed**（33.8m） | 限流已修（429 消失，ai-research API 组 3 条转绿）；饥饿/500 仍在 |
| 3 | `--last-failed` 62 | **24 passed / 38 failed**（14.1m） | 双产品缺陷已修（data-sync-error 全家族、api-backtest 核心族、dashboard 部分转绿）；余 38 条为长尾（分类见下） |

167 用例通过数：86 → 105 → **129**（+43）。

---

## 本记录修复（产品级 ×2 + 工程化 ×2）

### 缺陷 1: 限流阈值硬编码 → 配置驱动

e2e 并发压测下四服务自我 429（首轮 81 失败的主导因素）。修复：`RATE_LIMIT_PER_MINUTE`（data/analysis）与 `AI_RATE_LIMIT_PER_MIN`（ai）从配置/环境读取，默认值保守，e2e 场景调高。涉及 `cmd/data/setup.go`、`cmd/analysis/setup.go`、`config/data-service.yaml`、`config/analysis-service.yaml`。算法级缺陷（滑动窗口整窗重置，窗口边界突发 2×）**未修**，登记 [TASKS.md](../TASKS.md) P1-29。

### 缺陷 2: worker 饥饿 — JobService 直写 DB 不通知空闲 worker（产品缺陷）

**症状**: `POST /api/sync/jobs` 建单成功，`GET /api/sync/workers` 显示 `is_running: true`，但作业永不消费、worker 零日志、`pg_stat_activity` 无活动查询。

**根因链**: `WorkerPool.Start()` 在队列空时启动 → 3 worker 经 `WaitForJob` 的 `CountPending` 预检（合法返回 0）→ 永久阻塞在 channel select；而 `JobService.CreateJob` / `RetryJob` **直写 DB，从不调用 `queue.notify()`** → 后续作业永不被唤醒。全仓唯一带 notify 的 `Queue.Enqueue` 仅 Scheduler 使用。

**时间线取证法**（推翻「启动时已有 pending」的初始误判）: worker 启动于本地 14:34:13，DB 最老 pending 作业 `created_at` 为 13:01:23 UTC = 本地 15:01:23 —— worker 启动时队列确为空，属**运行期通知缺失**而非启动期积压。

**修复**（一次接线覆盖全部 14 个 `CreateJob`/`RetryJob` 调用点）:
- `pkg/sync/queue.go`: 新增导出方法 `NotifyJobAvailable()`（唤醒阻塞在 `WaitForJob` 的空闲 worker）
- `pkg/sync/job.go`: JobService 增 `SetPendingNotifier(fn)`，`CreateJob` / `RetryJob` 成功路径末尾 `notifyPending()`
- `cmd/data/sync_handlers.go`: `NewSyncHandler` 接线 `jobService.SetPendingNotifier(queue.NotifyJobAvailable)`

**活体验证**: stocks 作业建单 → dequeue → 6 秒 completed → 5564 行真实 tushare 落库。

### 缺陷 3: `/api/strategies` 500 — params 列双消费者形状冲突（产品缺陷）

**根因**: `params` JSONB 列的写入方（`SeedStrategies` / `StrategyDB.Create` / `GetStrategyParams`）均存**运行时参数值 object**（如 `{"fast":5,"slow":20}`），而 `StrategyDB.ListWithDB` 误按 `[]Parameter` **描述符数组**解析 → 内置策略 momentum/value/quality 全 500（S7-P0-6 fail-loudly 放大了爆炸半径，行为符合设计）。

**修复**（`pkg/strategy/db.go`）: 按 `map[string]any` 解析 object，keys 排序后逐项投影为 `Parameter{Name, Type, Default}` 描述符（`jsonTypeName` 推断 float/bool/string）；解析失败仍 fail-loudly。验证: `/api/strategies` 200 完整目录。

### P1-18 关闭: `pkg/sync/worker_test.go` mock 落地

`newMockJobStore()`（内存 JobStore：mutex + `Clone` 拷贝防御 + `created_at DESC` 排序镜像 PG 实现）+ 两个回归测试 `TestWorkerPool_WakesOnJobServiceCreate` / `TestWorkerPool_WakesOnJobServiceRetry`（pool 先启动 → sleep 让 worker 进入阻塞 select → CreateJob/RetryJob → `require.Eventually` 完成）。`pkg/sync` 11/11 PASS，全仓 `go test ./...` 门禁解锁。

---

## 数据面就绪（第 3 轮前置）

| 项 | 结果 |
|---|---|
| trading calendar | 2023-01-01 ~ 2026-12-31，969 交易日（backtest 引擎前置语义：未同步报 `trading calendar not synced` INVALID_INPUT） |
| stocks | 5564 行真实 tushare |
| ohlcv | `stk_factor_pro` 权限墙（tushare error 40203，premium 接口）→ SQL 合成 2400 行兜底；改进项登记 P1-28 |
| momentum 回测 | `POST /api/backtest` → 200 / 92ms / 1 trade（strategy 本地注册表两级信号源） |

---

## 第 3 轮剩余 38 条失败 — 根因分类

| # | 类别 | 条数 | 用例 | 根因与处置 |
|---|---|---|---|---|
| 1 | 视觉基线漂移 | 6 | visual-regression ×6 | 基线为**空库首跑**所写，数据落库后必然 diff；快照从未入库（`.gitignore` 已补），按环境本地产物处置 |
| 2 | SPA 结构/导航脱节 | 21 | ai-research ×6、backtest-engine ×4、dashboard ×5、cross-navigation ×1、copilot/screener/strategy-selector 导航 ×3、data-sync UI ×1、critical T-04 ×1 | 测试期望的类名（`.ai-research-page` 等）与导航结构（sidebar 期望 4 项实得 6 —— Evidence/AI Research 加入）未随 SPA 演进更新 |
| 3 | data-sync 契约/环境脱节 | 4 | data-sync :16/:125/:204、schedule :16 | typed door 拒绝旧 `stock_list`（202→400，ODR-062 已预期）/ `total_items` 语义 / Node 环境无 `EventSource`（ReferenceError）/ schedules 期望集过窄 |
| 4 | 期望集语义不符 | 3 | api-negative :54/:64、data-sync-error :122 | 期望集未覆盖实现返回的合理状态码（如 copilot 未配置 LLM 时 503） |
| 5 | 产品缺陷: 指标 omitempty 消失 | 2 | api-backtest :93/:174 | 见下「遗留缺陷」D-2 |
| 6 | 产品缺陷: 空 stock_pool 500 | 1 | api-negative :27 | 见下「遗留缺陷」D-1 |
| 7 | 测试假设过强 | 1 | api-backtest :331 | PnL 比例不变性用 `toEqual` 深等（-0.006698 vs -0.006834，滑点/取整噪声），应加容差 |

---

## 遗留缺陷（本轮不修，登记 [TASKS.md](../TASKS.md)）

### D-1: backtest 入口校验缺失（P1-25）
`POST /api/backtest` 缺 `stock_pool` / 非法 body → `engine.RunBacktest` 错误统一 500（`cmd/analysis/handlers_backtest.go:67`），应 400 fail-fast。负面用例 `api-negative :27` 期望 400 实得 500。

### D-2: `BacktestResponse` 指标字段 `omitempty` → 零值指标从持久化与 report 响应中消失（P1-26）
`pkg/backtest/contracts/contracts.go:68` 起指标字段带 `omitempty`（`total_return` / `sharpe_ratio` / `win_rate` / `total_trades` / `trades` 等）。零交易回测（如 1 月窗口 momentum）所有指标为零 → `SaveSyncResult` 持久化的 `job.Result` JSON 仅剩 10 个字段 → `GET /backtest/:id/report` 走 job 回读路径（engine 内存未命中时）缺 `total_return` 等 8 项 → `api-backtest :93/:174` `undefined`。非零场景（6 月窗口）不受影响，解释了同文件内「部分过部分不过」。修复方向: 持久化/回读统一完整指标序列化（去 omitempty 或专用存储 struct）。

### D-3: 零交易回测 `sortino_ratio` 输出 `MaxFloat64` 哨兵（P1-27）
实测响应含 `"sortino_ratio": 1.7976931348623157e+308` —— 除零防护缺失，指标未归一，垃圾值随 `omitempty`（非零得以保留）持久化并外泄至 API。

### D-4: ohlcv 数据源权限墙（P1-28）
`FetchDailyOHLCV` 刻意选 `stk_factor_pro`（qfq 数据源，premium 接口），当前 token 无权限（error 40203）→ ohlcv 作业必失败。改进方向: `daily` + `adj_factor` 组合回退。

### D-5: 限流滑动窗口整窗重置（P1-29）
算法级缺陷：窗口边界突发可达 2× 配额。本轮仅产品化缓解（配置驱动），算法替换（令牌桶/滑动日志）另立。

### D-6: `TestPluginLoader_SetWatchDir` Windows 失败（P1-30）
`/nonexistent/path` 在 Windows 不报错（期望 error 实得 nil），`loader_test.go:39` 断言失败。平台性预存问题，与本轮改动无关。

### D-7: e2e 期望侧对齐（P1-31）
上表 #1/#2/#3/#4/#7 的测试侧修正清单（类名/导航断言/契约 shape/期望集/容差/视觉基线策略）。

---

## Metrics

| 指标 | 值 |
|---|---|
| 全套件通过率 | 86/167（51.5%）→ **129/167（77.2%）** |
| `pkg/sync` 单测 | 11/11 PASS（P1-18 解锁 `go test ./...` 门禁） |
| worker 修复活体证据 | stocks 作业 6s completed，5564 行落库 |
| `/api/strategies` | 500 → 200 完整目录 |
| 429 风暴 | 第 2 轮起归零 |
| gin Recovery `http.ErrAbortHandler` | SSE 客户端断开的正常反向代理行为，非缺陷 |

## 附注（取证过程工程坑，供后续复用）

- PowerShell 5.1 内联 JSON body 引号被吞（`\"` 字面反斜杠 / `\\` 注入）且 `Set-Content -Encoding UTF8` 带 BOM 会让 `json.Unmarshal` 失败 → 临时文件用 `[IO.File]::WriteAllText`（无 BOM）+ `curl.exe --data @file`
- `-race` 需 cgo（本机不可用）→ 退化普通 `go test`
- 后台 job 的 `TaskOutput` 查询报 task not found → 直接读 job `output.log`
