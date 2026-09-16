---
status: active
last-verified: 2026-09-16
verified-by: 代码审查（2026-09-16）+ 产品重构讨论
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

---

## P0 — 正确性（不修，其它一切都是沙上建塔）

| # | 任务 | 位置 | 验收 |
|---|---|---|---|
| **P0-4** | **鉴权默认关闭**：无 `JWT_SECRET` 时进入 open-access；CORS 硬编码 `*` | `cmd/analysis/setup.go:234-236,536`、`middleware.go:13` | 密钥为空时拒绝启动；CORS 白名单可配 |
| **P0-4** | **鉴权默认关闭**：无 `JWT_SECRET` 时进入 open-access；CORS 硬编码 `*` | `cmd/analysis/setup.go:234-236,536`、`middleware.go:13` | 密钥为空时拒绝启动；CORS 白名单可配 |
| **P0-5** | **AI 编译后不加载**：`go build` 真跑但产物从未被 `plugin.Open`，回测按名字必然 `strategy not found` | `pkg/ai/pipeline/pipeline.go:490,512` | 端到端：一条意图 → 编译 → 加载 → 回测出结果 |
| **P0-6** | **编译校验恒真**：`go tool compile -V=full` 只打印版本号 | `pkg/ai/validator/code_validator.go:105,119` | 换成真实编译检查或显式返回"未实现" |

---

## P1 — 地基与回路

| # | 任务 | 位置 | 验收 |
|---|---|---|---|
| **P1-1** | **实验日志表**：记录 AI 每次尝试（参数向量 / 结果 / 父子关系 / 假设来源） | 新建 `experiments` 表 | 能回放一条完整探索路径 |
| **P1-2** | **循环控制器**：让搜索算法（或 LLM）能连续调用底座 + 响应中断 | `pkg/ai/search`（TPE/遗传已实现但零调用） | 能连续跑 100 次实验并支持中途叫停 |
| **P1-3** | **观察页**：三栏（正在试什么 / 结果流 / 当前最优）+ 干预入口 | `web/src/pages/` 新增 | 能看见路径形状，能输入方向 |
| **P1-4** | **schema 收口**：真实 DDL 硬编码在 Go 里，`migrations/` 无版本管理。**含语义债**：`stock_fundamentals.trade_date` 被两种写入路径复用——fina_indicator 路径存的是报告期截止日，daily_basic 路径存的才是真实交易日（见 P0-1 的 COALESCE 兜底） | `pkg/storage/postgres.go:69-330`、`migration_manager.go:43`（零调用） | migrations 可执行、有版本号、能重放；`trade_date` 语义拆分或改名 |
| **P1-5** | **统一错误中间件**：154 处手写 `gin.H{"error"}`，`c.Error()` 使用 0 次 | `cmd/analysis/setup.go:528-537` | 全局 AppError → HTTP 映射 |
| **P1-6** | **无界 goroutine**：信号量只限并发执行，goroutine 数 = 任务数 | `pkg/backtest/batch/batch.go:173`、`walkforward.go:152` | 固定 worker pool |
| **P1-7** | **N+1 查询**：300 只票 = 600 次 DB 往返 | `pkg/data/factor_attribution.go:89,103` | 改批量查询 |
| **P1-8** | **部署编排三份合一**：compose × 2 + k8s × 8，互相漂移 | `docker-compose*.yml`、`deploy/k8s/` | 一份源 + 生成 |
| **P1-9** | 前端 `/alerts` `/compliance` 缺 `/api` 前缀，dev 下必 404 | `web/src/api/alerts.ts:53`、`compliance.ts:104` | 对齐后端路由 |
| **P1-10** | **`make build` 会失败**：Makefile 仍 build `cmd/execution`、`cmd/risk`，这两个目录 ODR-021 合并后已不存在 | `Makefile:49-70` | `make build` 通过，或删掉这两个目标 |
| **P1-11** | **`cmd/ai` 无 Dockerfile、不在 `docker-compose.yml`**，只能本地 `go run` | `cmd/ai/`、`docker-compose.yml` | 补 Dockerfile 与 compose 条目，或明确标注为仅本地运行 |

---

## P2 — 数据与清理

| # | 任务 | 位置 |
|---|---|---|
| **P2-1** | 补**宏观/跨境数据源**（美股、汇率、利率、大宗）—— 对产业链认知价值最高 | 新建 adapter |
| **P2-2** | 产业链数据底座最小版：`query_supply_chain(name)` | 新建 |
| **P2-3** | 因子加 `hypothesis_source` 字段（因果来源） | `factor_cache` schema |
| **P2-4** | 幸存者偏差：无退市/剔除逻辑，回测 universe 不完整 | `pkg/backtest/engine.go` |
| **P2-5** | 废弃模块清理：`pkg/ai/agents` 标记 DEPRECATED 仍是 `cmd/ai` 主链路 | `cmd/ai/main.go:13` |
| **P2-6** | `drift` 概念漂移检测零调用（孤儿代码）；`get_market_regime` 未进主流程 | `pkg/ai/drift/`、`pkg/tools/builtin/` |
| **P2-7** | 死配置 `config/ai-service.yaml` 从未被读取；`docker-compose.services.yml` 引用不存在的 Dockerfile | config/、deploy/ |
| **P2-8** | 幸存的前视风险复核：复权口径无 hfq 对照 | migrations |
| **P2-9** | **验证器链**：5 个确定性校验器（统计 / 经济 / 稳健 / 偏差 / 冗余）落 L2 + 1 个因果审查落 L3。被调用、不自主循环 | 新建 `pkg/validation/` | 输入一份提案 → 输出质疑清单 + 概率估计；五个校验器无需 LLM |
| **P2-10** | `domain.Fundamental` 数值字段是 `float64`，而表中列可为空。P0-1 中用 `COALESCE(col,0)` 兜底，导致**缺失值被当作 0 而非"未知"**（PE=0 会被误判为极便宜） | `pkg/domain/market/types.go:69` | 改为 `*float64`，或让因子层显式跳过缺失值 |
| **P2-11** | Hermes Agent 系统设计文档遗失（原在 `.trae/documents/`，目录已删）。SPEC §6 与 hermes 验收测试均引用它 | `docs/hermes/` | 补写设计文档，或在引用处说明以配置为准 |

---

## 已冻结（本次定位重构后不再投入）

- 多 agent 投票 / arbitrator 仲裁机制 —— 错误相关，放大偏差且不可解释
- 演化算法直接产出交易信号 —— 产出落在"高统计/无因果"象限，不会被采用（改为漏斗顶部的候选生成器）
- EquityDeep 作为"独立工作面"—— 改为数据底座（产业链图谱）+ 研究洞察库

---

_完成一项删一项。本文件超过 5 页 = 该拆分项目了。_
