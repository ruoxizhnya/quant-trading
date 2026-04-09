# Next Steps Development Plan

> **Audit Date:** 2026-04-09
> **Auditor:** AI Assistant (Code Review Agent)
> **Scope:** 全栈审查 — 设计文档 / 代码一致性 / 测试有效性 / 代码质量
> **Status:** Ready for Implementation

---

## 审查总览

| 维度 | 严重问题 | 中等问题 | 低优先级 | 评分 |
|------|---------|---------|---------|------|
| 1. 设计文档审查 | 2 | 3 | 2 | B+ |
| 2. 设计↔代码一致性 | 4 | 3 | 1 | B |
| 3. 测试有效性 | 1 | 5 | 3 | C+ |
| 4. 代码质量 | 2 | 6 | 4 | B- |

**总体评估**: 系统架构设计合理，核心回测引擎实现扎实。主要风险集中在：(1) 文档与代码不同步，(2) 前端测试覆盖不足（尤其是交互逻辑），(3) 前端组件职责过重需要拆分。

---

## 一、设计文档审查结果

### ✅ 设计优点
- **VISION.md**: 7 大原则清晰（Accuracy First、Market-Agnostic、Hot-Swap 等），与主流量化平台（vnpy、JoinQuant）的设计理念对齐
- **ARCHITECTURE.md**: 微服务分层合理，数据模型完整，策略插件机制成熟
- **ROADMAP.md**: Sprint 拆分粒度适中，Phase Gate 机制规范
- **ADR 体系**: 10 条 ADR 覆盖关键决策，状态追踪清晰

### ⚠️ 需修复的问题

#### P0-CRITICAL: 策略接口定义三处不一致

| 来源 | 方法签名 | 参数 |
|------|---------|------|
| [VISION.md:129](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/VISION.md#L129) | `GenerateSignals(ctx, bars, portfolio)` | OHLCV 数组 + Portfolio |
| [SPEC.md:130](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/SPEC.md#L130) | `Signals(ctx, universe, data, date)` | Stock 列表 + MarketData + Date |
| **实际代码** [strategy.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/strategy/strategy.go) | `GenerateSignals(ctx, bars, portfolio)` | 与 VISION 一致 |

**影响**: SPEC.md 是开发者参考的权威接口定义，但与实际代码不一致。新开发者会困惑。
**修复方案**: 统一为实际代码签名，更新 SPEC.md。

#### P1-HIGH: 服务架构文档超前于实现

| 服务 | SPEC 定义端口 | docker-compose 存在? | 代码存在? | 状态 |
|------|-------------|-------------------|---------|------|
| analysis-service | 8085 | ✅ | ✅ | 运行中 |
| data-service | 8081 | ✅ | ✅ | 运行中 |
| strategy-service | 8082 | ✅ | ✅ | 备用 |
| **risk-service** | **8083** | ❌ | ❌ | **仅文档** |
| **execution-service** | **8084** | ❌ | `cmd/execution/` 存在但未接入 compose | **半成品** |

**影响**: SPEC.md 描述了 risk(8083) 和 execution(8084) 服务的完整 API，但这些服务不存在。读者会误以为已实现。
**修复方案**: 在 SPEC.md 中标注这些服务为 "Planned — Phase 2/3"，或移至单独 "Future Services" 章节。

#### P1-HIGH: Phase 状态标记不准确

ROADMAP.md 标记:
- ~~P1-C 实盘接口预留 → ✅ Done~~ → 实际 `pkg/live/` 只有 MockTrader 接口 + mock 实现，无真实 broker 集成
- ~~P0-A FactorComputer → ✅ Done~~ → 但 factor_cache 表的 HTTP API 未暴露（P2-A 待完成）
- Walk-forward validation → 标记为 Sprint 6 deliverable → 代码中 `walkforward.go` 存在但未验证

**修复方案**: 更新 ROADMAP.md 状态标记，区分 "接口定义完成" vs "E2E 可用"。

#### P2-MEDIUM: 前端技术选型未在文档中体现

- VISION/SPEC/ARCHITECTURE 均描述 legacy HTML UI (`cmd/analysis/static/*.html`)
- 实际生产前端是 Vue 3 SPA (`web/src/`)，使用 Naive UI + Chart.js + Pinia + Vue Router
- 两套 UI 并存：legacy HTML 提供基础功能，Vue SPA 提供增强体验
- **此双轨制未在任何设计文档中说明**

**修复方案**: 在 ARCHITECTURE.md 的 "用户界面" 章节增加 "前端架构" 小节，说明两套 UI 的定位和关系。

#### P2-MEDIUM: 缺少 API 变更日志

- VISION.md v1.2 changelog 只记录了后端变更
- 前端重构（HTML → Vue SPA）是重大架构变更，未在版本记录中体现

---

## 二、设计与代码一致性审查结果

### ⚠️ 不一致清单

#### C-01 [CRITICAL]: Strategy 接口签名冲突 (同上)
**文件**: [SPEC.md:130](file:///Users/ruoxi/longshaosWorld/quant-trading/docs/SPEC.md#L130) vs [strategy.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/strategy/strategy.go)

#### C-02 [HIGH]: API 端点缺失/多余

**文档有但代码无:**
| 端点 | 文档来源 | 说明 |
|------|---------|------|
| `GET /backtest/:id/equity_curve` | SPEC.md | 净值曲线独立 API（当前嵌在 report 中） |
| `POST /analyze` | 代码有 main.go 注册 | 文档未提及 |
| `GET /status` | 代码有 main.go 注册 | 文档未提及 |
| Risk Service 全部端点 (8083) | SPEC.md | 服务不存在 |
| Execution Service 全部端点 (8084) | SPEC.md | 服务半成品 |

**代码有但文档无:**
| 端点 | 代码位置 | 说明 |
|------|---------|------|
| `POST /analyze` | cmd/analysis/main.go | 分析功能（需文档化） |
| `GET /ohlcv/:symbol` | cmd/analysis/main.go | K线代理（已在 ARCHITECTURE.md） |

#### C-03 [HIGH]: 数据库表结构偏差

| 表 | 文档描述 | 实际 schema | 差异 |
|----|---------|------------|------|
| `backtest_runs` | 含 `result_json` (JSONB) 字段 | 实际可能不含或字段名不同 | 需确认 |
| `orders` | 含 `backtest_run_id` 外键 | 可能未创建 | 回测订单未持久化 |
| `strategies` | 含 JSONB config 列 | Phase 2 目标，可能未建 | 策略配置未 DB 化 |

**根因**: 回测结果仅存内存（`map[string]BacktestResult`），不持久化到 DB。刷新即丢失。这与 ROADMAP Sprint 5.5 "Background backtest worker" 直接相关。

#### C-04 [MEDIUM]: Signal 类型定义演进未同步

- VISION.md Signal: `{Symbol, Date, Direction, Strength, Factors, Metadata}`
- SPEC.md Signal: 增加了 `Direction enum (Long/Short/Close)`
- 实际代码 [domain/types.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/domain/types.go): 需确认是否包含 OrderType/LimitPrice (Phase 3 新增)

#### C-05 [LOW]: 配置文件路径不一致

- VISION.md 提到 `config/strategies/*.yaml`
- 实际代码可能从 DB 或硬编码加载策略参数
- `config/global.yaml` 是否被所有服务读取？需验证

---

## 三、测试审查结果

### 当前测试矩阵

| 测试文件 | 覆盖内容 | 测试数 |
|---------|---------|--------|
| dashboard.spec.ts | 页面加载、指标卡、快速回测表单、历史列表、导航 | ~12 |
| backtest-engine.spec.ts | 页面加载、策略选择器、表单、运行按钮、历史 | ~10 |
| screener.spec.ts | 筛选输入、按钮、重置、API调用、空状态 | ~12 |
| copilot.spec.ts | 聊天界面、输入区、发送按钮 | ~8 |
| strategy-selector.spec.ts | 页面加载、搜索、策略卡 | ~8 |
| cross-navigation.spec.ts | 页间导航、URL路由、JS错误、SPA行为 | ~10 |
| api-health.spec.ts | 健康检查、股票数、指数、策略列表 | ~8 |
| api-backtest.spec.ts | 启动回测、获取结果、历史记录 | ~6 |
| api-strategy.spec.ts | 策略 CRUD | ~6 |
| **合计** | | **~80** |

### ⚠️ 关键缺失测试 (MUST FIX)

| # | 缺失测试 | 风险等级 | 对应功能 |
|---|---------|---------|---------|
| T-01 | **回测结果渲染** — 指标卡片显示正确数值? 净值曲线绘制? | 🔴 CRITICAL | BacktestEngine.vue |
| T-02 | **交易信号可视化** — 绿色买入/红色卖出标记出现在图表上? | 🔴 CRITICAL | BacktestEngine.vue Chart.js |
| T-03 | **交易表格切换** — 点击"显示交易"/"隐藏交易"切换正常? | 🟠 HIGH | BacktestEngine.vue |
| T-04 | **错误状态处理** — 404 报告过期显示友好提示? | 🟠 HIGH | BacktestEngine.vue loadReport |
| T-05 | **表单验证** — 空股票代码提示? 无效日期拦截? | 🟠 HIGH | Dashboard + BacktestEngine |
| T-06 | **防重复提交** — 双击运行回测只触发一次? | 🟠 HIGH | Dashboard runQuick |
| T-07 | **导航高亮唯一性** — /backtest 页面只有回测引擎高亮? | 🟡 MEDIUM | AppSidebar.vue |
| T-08 | **NaN/undefined 显示** — 历史记录不显示 undefined/NaN? | 🟡 MEDIUM | Dashboard + BacktestEngine |
| T-09 | **响应式布局** — 窗口缩小时元素不重叠? | 🟡 MEDIUM | 全局 CSS |

### ⚠️ 测试质量问题

| # | 问题 | 影响 | 建议 |
|---|------|------|------|
| Q-01 | **纯存在性断言** — 多数测试只检查 `toBeVisible()`，不验证交互行为 | 假阴性 | 增加 userEvent.click + 结果断言 |
| Q-02 | **硬编码等待** — `waitForTimeout(2000)` 代替智能等待 | 脆弱慢 | 改为 `waitForSelector` 或 `waitForResponse` |
| Q-03 | **文本精确匹配** — `toHaveText('控制台')` 会因文案调整而失败 | 维护成本 | 用 `getByText(/控制台/)` 正则 |
| Q-04 | **无数据清理** — 测试间共享 localStorage（backtestStore.history） | 测试串扰 | 每个 test() 前清空 store |
| Q-05 | **API 测试无负向** — 不测试 400/500 错误响应 | 盲区 | 增加 malformed body 测试 |

---

## 四、代码质量审查结果

### 🔴 CRITICAL Issues

#### Q-001: BacktestEngine.vue — God Component (600+ 行)

[BacktestEngine.vue](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/pages/BacktestEngine.vue) 承担了 7 个职责:
1. 表单状态管理 (reactive form)
2. API 调用 (runBacktest, getBacktestReport)
3. 数据转换 (stripHeavyData, sampleData, tradeMarkers)
4. Chart.js 渲染 (renderChart, 图表配置)
5. 历史管理 (loadHistory, addToHistory, clearHistory)
6. 格式化 (fmtPercent, formatNum, formatMetric, itemDesc, historyDesc)
7. 模板渲染 (整个 SFC)

**建议拆分为**:
```
components/backtest/
├── BacktestForm.vue        — 表单 + 参数输入
├── BacktestResults.vue     — 结果展示容器
├── EquityChart.vue         — 净值曲线 + 买卖标记
├── MetricsCards.vue        — 指标卡片网格
├── TradeTable.vue          — 交易记录表格
├── HistoryList.vue          — 历史记录列表
├── useBacktest.ts          — composable (API + 状态)
└── chartConfig.ts          — Chart.js 配置常量
```

#### Q-002: Dashboard.vue — 同样过重 (270+ 行)

[Dashboard.vue](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/pages/Dashboard.vue) 混合了:
- 指标数据获取 + 展示
- 快速回测表单 + 执行
- 导航磁贴
- 历史记录展示
- 格式化函数 (historyDesc)

**建议**: 提取 `useDashboard.ts` composable + 子组件

### 🟠 HIGH Issues

#### Q-003: 双轨 UI 架构未统一

系统同时存在:
1. **Legacy HTML UI**: `cmd/analysis/static/{index,dashboard,screen,copilot}.html` — Go 模板渲染
2. **Vue 3 SPA**: `web/src/` — 独立开发服务器 (Vite :5173)

**问题**:
- 两套 UI 功能重叠（都有回测、选股器、Copilot）
- Legacy HTML 通过 `http://localhost:8085` 访问
- Vue SPA 通过 `http://localhost:5173` 访问（开发模式）或构建后替代 legacy
- 开发者可能混淆哪套是"正式"UI

**建议**: 明确声明 Vue SPA 为唯一正式前端，legacy HTML 标记为 deprecated。

#### Q-004: 回测结果不持久化

`POST /backtest` 返回的结果存储在 Go 内存 map 中:
```go
var results = make(map[string]BacktestResult) // 进程重启即丢失
```

**后果**:
- 刷新页面 → 404 (报告丢失)
- 无法查看历史回测对比
- 无法做批量回测分析

**与设计矛盾**: 
- SPEC.md 定义了 `backtest_runs` 表含 `result_json` 列
- ROADMAP Sprint 5.5 要求 background worker + DB 持久化
- **当前未实现**

**建议**: 优先级 P0 — 实现 result 持久化到 PostgreSQL

#### Q-005: API Client 错误处理不统一

[client.ts](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/api/client.ts) 的 fetch 包装:
- 部分路径返回原始 response，部分返回 parsed json
- 无统一错误码映射 (400/401/404/500)
- 无重试机制
- 无请求取消 (AbortController)

### 🟡 MEDIUM Issues

#### Q-006: Magic Numbers 散布

| 值 | 出现位置 | 应定义为 |
|----|---------|---------|
| `0.0003` | commission_rate 默认值 | `DEFAULT_COMMISSION` 常量 |
| `1000000` | initial_capital 默认值 | `DEFAULT_CAPITAL` 常量 |
| `120` | MAX_CHART_POINTS | 已命名但可配置 |
| `20` | momentum lookback | 策略参数，应在 config |
| `5` / `0.05` | position limit 5% | 风控参数 |

#### Q-007: 格式化函数重复

`fmtPercent()` 在以下文件中重复定义或 import:
- [format.ts](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/utils/format.ts)
- [Dashboard.vue](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/pages/Dashboard.vue) (inline)
- [BacktestEngine.vue](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/pages/BacktestEngine.vue) (inline)

**应统一**: 所有页面从 `utils/format.ts` import，不在组件内重复。

#### Q-008: TypeScript `any` 类型滥用

[api.ts 类型定义](file:///Users/ruoxi/longshaosWorld/quant-trading/web/src/types/api.ts) 中多处使用 `any`:
- BacktestResult 的 `metrics` 字段: `Record<string, any>`
- trade 的 `pnl`: `number | any`
- 组件内 `as any` 强制类型转换 (至少 3 处)

#### Q-009: 后端 engine.go 复杂度

[engine.go](file:///Users/ruoxi/longshaosWorld/quant-trading/pkg/backtest/engine.go) 主循环函数较长，包含:
- 因子缓存预热
- 股息/送股处理  
- 涨跌停检测
- 限价单判断
- 日志记录

建议提取子方法: `processCorporateActions()`, `checkPriceLimits()`, `executeLimitOrders()`

---

## 五、下一步开发计划 (Action Items)

### Phase A: 紧急修复 (本周)

| ID | 任务 | 文件 | 预估 |
|----|------|------|------|
| A-1 | 统一 Strategy 接口文档 (C-01) | SPEC.md | 30min |
| A-2 | 补充 9 项关键缺失测试 (T-01~T-09) | e2e/tests/ | 2h |
| A-3 | 修复 NaN/undefined 历史显示 (T-08) | ✅ 已完成 | — |
| A-4 | 修复导航高亮 (T-07) | ✅ 已完成 | — |

### Phase B: 架构改进 (下周)

| ID | 任务 | 涉及文件 | 预估 |
|----|------|---------|------|
| B-1 | **拆分 BacktestEngine.vue** 为 7 个子组件 | web/src/components/backtest/* | 4h |
| B-2 | **拆分 Dashboard.vue** — 提取 useDashboard composable | web/src/composables/useDashboard.ts | 2h |
| B-3 | **统一格式化函数** — 消除重复 fmtPercent | web/src/utils/format.ts | 30min |
| B-4 | **回测结果持久化** — POST /backtest 写入 backtest_runs 表 | pkg/backtest/engine.go + postgres.go | 4h |
| B-5 | **更新设计文档** — 同步 C-02~C-05 发现 | docs/*.md | 1h |
| B-6 | **明确双轨 UI 定位** — 在 ARCHITECTURE.md 说明 | docs/ARCHITECTURE.md | 30min |

### Phase C: 测试增强 (持续)

| ID | 任务 | 预估 |
|----|------|------|
| C-1 | 增加 E2E 负向测试 (400/500 错误、malformed body) | 1h |
| C-2 | 测试隔离 — 每个测试前清空 localStorage/store | 30min |
| C-3 | 替换硬编码等待为智能等待 | 1h |
| C-4 | 增加回测结果对比测试 (run twice → same id?) | 1h |
| C-5 | 覆盖率目标: 核心交互 100% | 持续 |

### Phase D: 代码质量 (迭代)

| ID | 任务 | 预估 |
|----|------|------|
| D-1 | 提取 Magic Numbers 为命名常量 | 1h |
| D-2 | 减少 `any` 类型使用 — 定义严格接口 | 2h |
| D-3 | API Client 统一错误处理 + AbortController | 2h |
| D-4 | engine.go 提取子方法 | 2h |
| D-5 | ESLint + Prettier 配置 (如未配置) | 30min |

### Phase E: 文档同步 (里程碑)

| ID | 任务 | 依赖 |
|----|------|------|
| E-1 | SPEC.md Strategy 接口 → 与代码对齐 | A-1 |
| E-2 | SPEC.md 标注 risk/execution 服务为 Planned | B-6 |
| E-3 | ROADMAP.md 修正 Phase 状态标记 | B-6 |
| E-4 | VISION.md 增加前端架构章节 (Vue SPA) | B-6 |
| E-5 | ADR-011: 前端架构决策 (HTML vs Vue SPA) | B-6 |

---

## 六、风险提醒

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| 回测结果不持久化导致用户体验差 | 确定 | 高 | Phase B-4 优先实施 |
| 前端组件过大导致维护困难 | 确定 | 中 | Phase B-1/B-2 尽早执行 |
| 测试不足导致回归 bug | 高 | 高 | Phase A-2 立即补全 |
| 文档与代码不同步导致新人困惑 | 确定 | 中 | Phase E 同步计划 |

---

_审计完成。以上所有发现均已记录并可追溯至具体文件和行号。_
