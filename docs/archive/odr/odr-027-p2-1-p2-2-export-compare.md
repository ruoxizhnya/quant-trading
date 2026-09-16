# ODR-027: P2-1 / P2-2 回测报告导出 & 多策略对比

> **Status**: Completed
> **Date**: 2026-06-12
> **Category**: Implementation (Reporting / UX)
> **Related ADRs**: ADR-005 (Strategy Config 标准化), ADR-019 (Service 合并)
> **Related ODRs**: ODR-021 (P1-15 服务合并), ODR-022 (P1-26 实体合并)
> **Supersedes**: —
> **Relates to**: TASKS §P2-1, §P2-2, BR-005 (回测可重现), BR-012 (报告可分享)

## Context

回测引擎 (Backtest Engine) 一直是 Quant Lab 的核心能力，但报告形态
单一 — 只能在前端 SPA 浏览器里看。两条具体用户痛点：

1. **报告无法分享 / 归档**: A 股研究员做完回测后，要把结果发给 PM
   或合规存档。SPA 截图既不专业也不可重现 (字体、缩放、视口宽度都会
   变化)。PDF 是目标格式，但 server-side PDF (chromedp、wkhtmltopdf)
   部署重、字体渲染在 headless 容器里经常炸。
2. **多策略无法横向比较**: 同一组股票、同一段时间，跑 5 个不同策略，
   现在必须开 5 个 tab 切来切去手动对指标。研究员真实工作流是
   "批量跑 → 选最优 → 写报告"，缺了第二步。

P1-15 服务合并 (ODR-021) 后 backtest/compare 不需要新服务；导出 +
对比 = 纯 Go 端模板 + HTTP endpoint + 前端薄包装。

## Decision

### P2-1: HTML 自包含报告导出

**后端** (`pkg/backtest/export.go` + `cmd/analysis/handlers_backtest.go`):

- `RenderHTML(resp BacktestResponse, opts HTMLReportOptions) ([]byte, string, error)`
  - 接受一个已完成回测的 `BacktestResponse` + 渲染选项，返回
    `text/html; charset=utf-8` 的字节流
  - 模板在 `export.go` 内嵌 (`htmlReportTemplate`)，含 SVG 权益曲线、
    指标表、交易明细
  - **不**使用客户端 JS / 外部 CDN — 打印 / 邮件归档必须完全离线
    可重现
- `HTMLReportOptions`:
  - `IncludeEquityChart` (bool) — 是否渲染 SVG 权益曲线
  - `IncludeTrades` (bool) — 是否渲染交易明细表
  - `Theme` ("light" / "dark") — CSS 变量切换
  - `FooterNote` (string) — 操作员自附水印 (e.g. "策略评审 2026Q2")
- HTTP endpoint: `GET /api/backtest/:id/export/html`
  - Query: `?theme=dark&equity=0&trades=0&footer=...`
  - 响应: `Content-Disposition: attachment; filename="backtest-<id>-<date>.html"`
    + 自定义头 `X-Backtest-Id`, `X-Backtest-Strategy`
  - **不**直接生成 PDF — 浏览器 `Ctrl+P → 保存为 PDF` 是我们推荐的
    下游路径。`format=pdf` 返回 400，错误信息明确指引用户用浏览器打印
- lookup 复用 `lookupBacktestResponse`: in-memory (Engine) 优先，
  fallback DB-stored Job

**前端** (`web/src/api/backtest.ts` + `web/src/pages/BacktestEngine.vue`):

- `api.download(path)` 新方法 (在 `web/src/api/client.ts`): 绕过 JSON
  parse，直接 `fetch().blob()`，并解析 `Content-Disposition: filename*=`
  / `filename=`
- `exportHtml(id, opts)` API helper: 构造 query string、调 `download()`、
  返回 `{ blob, filename }`
- BacktestEngine.vue 在 result 区上方加 action bar:
  - 左侧: "ID: <id>" tag + "加入对比" checkbox
  - 右侧: "导出 HTML" 按钮 (调 `exportHtml` → 触发 `<a download>`)
- `<a download>` 模式 + `URL.revokeObjectURL(setTimeout 1s)` 释放
  内存，避免 Chromium 大 blob 泄漏

### P2-2: 多策略对比

**后端** (`pkg/backtest/compare.go` + handler):

- `CompareReports(ctx, ids, resolver) (CompareReport, error)`:
  - 接受 2-8 个 ID，调用 `CompareResultResolver` 拉取每个报告
  - 拉取顺序: in-memory (Engine) → DB (JobStore)，复用
    `lookupBacktestResponse` 的 fallback 策略
  - **Min/Max 校验**: < 2 个或 > 8 个 ID 直接 error，handler 翻译为
    400 BadRequest
  - **去重**: 重复 ID 仅查询一次 (FIFO dedup)
  - **部分成功**: 单个 ID resolve 失败 → 进 `Missing` 列表而非中断
    整体请求 (UI 端用 warning banner 展示)
  - 输出 `CompareReport`:
    - `Entries`: 表格友好的扁平 projection (12 个指标)
    - `Best`: 每指标的最优 ID (e.g. `TotalReturn → bt-2`)
    - `Missing`: 加载失败的 ID + 原因
    - `Requested / Resolved`: 数字摘要 (用于 banner "加载 N of M")
- HTTP endpoint: `GET /api/backtest/compare?ids=bt-1,bt-2,bt-3`
  - 错误分类: min/max count → 400; resolver error → 仍 200 + Missing

**前端** (`web/src/pages/BacktestCompare.vue` + 路由 + 侧边栏):

- 新页面 `web/src/pages/BacktestCompare.vue`:
  - 顶部: 已选数量 tag + 刷新 / 清空按钮
  - 4 列指标卡片: 已加载/已请求、最佳总收益、最佳 Sharpe、最低回撤
  - 绩效对比表: 12 行指标 × N 列策略，**最优列高亮** (浅绿底)
  - 权益曲线叠加: Chart.js 8 色 palette，目前用 initial→final 端点
    示意 (完整时间序列 gated on a follow-up endpoint，详见下文)
- 路由 `web/src/router/index.ts`: `path: 'backtest/compare'`，
  `name: 'backtest-compare'`
- 侧边栏 `AppSidebar.vue`: 新增 `GitCompareOutline` icon 入口
- 选择持久化 `web/src/constants/backtest.ts`:
  - `localStorage` key: `quantlab:backtest:compare_ids`
  - 容量上限 8 (FIFO)
  - BacktestEngine 勾选 ↔ BacktestCompare 顶部 tag 双向同步

### T+1 / 风险控制: 不受影响

对比 / 导出都是**只读**端点，不调用 risk / execution，不修改任何
状态。T+1 校验、position sizing 都不在路径上。

## Consequences

### 正面
- **报告可重现**: 同一 backtest ID + 同一 footer 永远生成字节级一致
  的 HTML (无随机 ID、无 CDN)
- **PDF 路径**: 浏览器打印 → PDF 是 0 服务端依赖的方案
- **对比速度**: 8 个 ID × 一次 in-memory / DB 查询 < 100ms (实测)
- **UX**: 从"5 个 tab 切"到"一页全景"是质的提升
- **离线归档**: HTML 自包含 → 邮件附件 / 监管存档无副作用

### 负面 / Trade-offs
- **HTML ≠ PDF**: PDF 用户必须多一步浏览器打印。我们评估过
  server-side PDF (chromedp, wkhtmltopdf) 都引入重依赖 + 字体问题，
  不值得。ODR-013 标记为后续 ODR (无时间表)
- **完整 equity 曲线未实现**: `CompareReport` 只含 summary metrics，
  不含每个 ID 的 `PortfolioValues` 数组。叠加图当前是
  initial→final 端点示意，**看起来不像真曲线**。完整曲线需要后端
  扩展 `?ids=...&series=1` query param (resolve 全部 ID + 在内存
  中 join)。**列为 ODR-027 follow-up**，规模 P3-2
- **多语言**: HTML 模板硬编码中文。后续需要 i18n 抽取

## Artifacts

### 新增文件
- `pkg/backtest/export.go` — HTML 模板 + RenderHTML
- `pkg/backtest/compare.go` — CompareReports 逻辑
- `pkg/backtest/compare_test.go` — 11 个单元测试
- `cmd/analysis/handlers_compare_test.go` — 6 个 handler 测试
- `web/src/constants/backtest.ts` — localStorage 选择持久化
- `web/src/pages/BacktestCompare.vue` — 对比页面

### 修改文件
- `cmd/analysis/handlers_backtest.go` — `/export/:format` 已有 + 新
  `/compare` 路由 + `strings` import
- `web/src/api/client.ts` — 新 `download()` 方法 + filename 解析
- `web/src/api/backtest.ts` — `exportHtml()` + `compareBacktests()` +
  CompareReport 类型
- `web/src/pages/BacktestEngine.vue` — 顶部 action bar + 导出 / 对比
  按钮 + 路由跳转
- `web/src/router/index.ts` — `/backtest/compare` 路由
- `web/src/components/layout/AppSidebar.vue` — "多策略对比" 入口

## Metrics

| 指标 | 数值 |
|------|------|
| 新增 Go 代码 (loc) | ~520 (export 320 + compare 200) |
| 新增 TS/Vue 代码 (loc) | ~480 (page 320 + api 60 + constants 100) |
| 新增测试 (cases) | 17 (compare 11 + handler 6) |
| 新增 API endpoint | 2 (`/export/html`, `/compare`) |
| 新增前端路由 | 1 (`/backtest/compare`) |
| 端到端耗时 (8 IDs 对比) | < 100ms in-memory, < 200ms with DB lookup |
| 文档同步更新 | TASKS.md (§P2-1, §P2-2 状态), ADR.md (ODR index) |

## Lessons Learned

1. **自包含 HTML 真的够用**: 担心用户会嫌"还得打印" — 实际上浏览器
   的 "Print to PDF" 在 Chrome 88+ 已经稳定，A4 / Letter 自适应，字体
   fallback 完善。Server-side PDF 是 over-engineering
2. **resolver pattern 大幅简化测试**: `CompareResultResolver` 是个
   `func(ctx, id) (Response, error)` — 测试时直接传 stub 闭包，
   完全不依赖 JobService / Engine 的具体类型
3. **partial resolution 设计** (200 + Missing list vs 4xx) 是关键 UX
   决策：5 个 ID 里 1 个失效不应该让用户重头选剩下 4 个
4. **不要把完整 equity 塞进 CompareReport**: 单 ID 1000 个
   `PortfolioValue` × 8 个 ID = 8000 个点 × 4 个 metric = 32KB JSON
   每次对比都重传。前端应**按需**拉曲线 (后续 ODR 跟进)
