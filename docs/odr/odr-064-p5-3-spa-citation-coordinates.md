# ODR-064: P5-3 对接 L0 单一数据面 + Evidence API — SPA 消费 citation 坐标（因子页 → 一键回溯证据）

> **Status**: Completed（运行时端到端取证通过，2026-09-16）
> **Date**: 2026-09-16
> **Category**: Implementation
> **Related ADRs**: [ADR-022](../adr/adr-022-unified-research-platform.md)（§1 单一数据面 / §3 单向依赖 / §5 citation 契约）
> **Supersedes**: —
> **Related ODRs**: [ODR-061](odr-061-p5-1-slice-c-citation-evaluation.md)（C2 契约定义与「主验收点待运行时取证」交棒）+ [ODR-060](odr-060-p5-1-frontend-evidence-api.md)（`/evidence` 页与 `getEvidence` 消费层先例）+ [ODR-062](odr-062-p5-1-bypass-residue-audit.md)（S-B 网关代理先例）+ [ODR-063](odr-063-e2e-runtime-forensics.md)（运行时取证口径与工程坑范本）
> **Author**: AI Assistant

---

## Context

### 触发条件

用户指令「按新规划继续处理 P5-3：对接 L0 单一数据面 + Evidence API」。`docs/TASKS.md` 阶段 P5 当时仅有 **P5-1**（已全维度关闭），**P5-3 无既有任务条目**（全仓唯一同名项在已归档的 `docs/archive/IMPLEMENTATION_PLAN.md:616`，语义为 `pkg/live` 集成测试，与本议题无关）。故先以 AskUserQuestion 收窄范围与验收方式，**未擅自选范围**：

| 裁决项 | 选项 | 用户选择 |
|---|---|---|
| P5-3 本轮范围 | ① SPA 消费 citation 坐标（推荐）/ ② 龙门面取数改造 / ③ 其他 | **① SPA 消费 citation 坐标** |
| 验收方式 | ① 仅静态门禁 / ② 起运行时端到端取证 | **② 起运行时取证（PG/Redis + 四服务 + token）** |

### 前置已落地事实（本轮不重做）

- **ODR-061 C2 已实施**：`factor_cache.citation JSONB NOT NULL DEFAULT '[]'`（迁移 027）+ 注入链（`loadStatementBook` 保留 `SnapshotURI` → `saveVerticalFactor` 收口）+ 输出面 `expandCitation` 展开五元组 —— 但**从未被任何前端消费**，且 ODR-061 §Metrics 遗留「主验收点（GET → evidence 200 循环）**待运行时取证**」。
- **ODR-060 已建** `web/src/api/evidence.ts::getEvidence` + `/evidence` 页 + `EvidenceLookup.vue`（三态渲染）—— 是**输入 hash** 的独立入口，与**因子面**无接线。
- **ODR-062 S-B 已建** analysis 网关代理先例（`proxyRequest` 型，透传状态码）。

### 路由命名陷阱（本轮关键侦察结论）

| 面 | 路由 | 形状 |
|---|---|---|
| L0 data 真实契约 | `GET /factors/:factor_name`（**无 `/api` 前缀**，`cmd/data/main.go:160`，**复数**） | 携 `citation` 五元组 |
| analysis 本地既有因子路由 | `/api/factor/*`（**单数**） | 形状不互通，不携 citation |
| SPA 消费约定 | `/api/...`（vite 代理 → 8085） | — |

⇒ 三者**互不相通**：SPA 走 `/api` 拿不到 data 的无前缀 citation 路由，且 analysis 既有的单数因子路由形状不符。这是本轮必须补 facade 的原因，而非「已有路由复用」。

### 契约依据（ADR-022 §5 / ODR-061 C2）

`citationTuple{source, dataset, key, as_of, content_hash}` 五元组；未命中 `ingest.raw` 时**只出 `content_hash`**（其余四字段缺席 = 「未归档」一等信号，**不得补造**）；解析失败**降级透传不 5xx**；存储侧 citation 为空时序列化为 `[]`；`factor_cache` 行未命中 → **404**（一等语义）。

### 取证环境

| 组件 | 形态 |
|---|---|
| 数据库 | `timescale/timescaledb:latest-pg16` 一次性容器 :15432（qtr-p53-pg） |
| 缓存 | `redis:7-alpine` 一次性容器 :16379（qtr-p53-redis） |
| 服务 | data :8081 / analysis :8085（本地二进制 `bin/*.exe`） |
| 前端 | vite dev :5175（5173/5174 被用户进程占用） |
| token | `TUSHARE_TOKEN` 进程环境注入（不落盘不提交） |

---

## Decision

### 1. 范围 = SPA 消费 citation 坐标（新增 facade，后端改动最小）

在 analysis 网关新增 **`GET /api/factors/:factor_name`** facade，转发至 L0 无前缀路由 `/factors/:factor_name`，`symbol`/`date` 逐字透传、**状态码原样透传**（404 是「无该 factor_cache 行」的一等答案；hash-only citation 必须原样抵达 SPA，不得在本层改写成 error）。选择该方案而非「改造 analysis 既有单数因子路由」或「让 SPA 直连 8081」，理由是：

- 直连 8081 = **L3→L0 旁路**，违反 ADR-022 §3 单向依赖（ODR-062 判定表已明确）；
- 改造单数路由需动既有形状与消费者，改动面远大于「补一个透传 facade」；
- facade 复用 ODR-062 S-B 已确立的 `proxyRequest` 型（状态码透传语义一致）。

### 2. 前端 = 只消费既有契约，不新造数据

新增因子页 `/factors`：输入 `factor_name` + `symbol` + `date` → 调 `GET /api/factors/:name` → 渲染五元组表格 → 「回溯证据」按钮跳 `/evidence?content_hash=<hash>`。

- **归档态判定**：以「`source`/`dataset`/`key`/`as_of` 四字段任一存在」判为已归档；否则显式渲染 warning tag（**不补造缺席字段**）；
- **`/evidence` 预填回跳**：`EvidenceLookup.vue` 新增 `initialHash` prop + `onMounted`/`watch` 立即解析，使「因子数 → 证据坐标 → 原始归档记录」成为一次点击；
- **404 与其它失败分态**：`factor_cache` 行未命中 → 「未命中 factor_cache 行」警告，与 `/evidence` 的 404 = 未摄取语义并列而非混淆。

### 3. 验收 = 运行时端到端取证

同 ODR-063 口径，静态门禁（`go build` / 单测 / `vue-tsc`）之外，必须起真实 PG + Redis + 四服务，用真实播种数据跑通「GET 因子 citation → GET evidence 200」回路并逐字留证。

---

## Consequences

**正向**

- ODR-061 遗留的「主验收点待运行时取证」在本轮**闭合**：citation 不再是「已交付未消费」的契约，而是有前端消费路径与实测证据的活链路。
- L0 单一数据面在**第三处**得到巩固（ODR-060 证据页 → ODR-062 sync 面 → 本轮因子面），且本轮**未产生任何 L3→L0 直连形态**（SPA 全程只经 `/api` → 8085 → 8081）。
- facade 采用「原样透传状态码 + 不补造字段」，使「未归档」「行未命中」两类一等语义**穿透网关不失真**。

**负向 / 边界**

- **覆盖面仍为 5/11 因子**（仅 5 个纵向基本面因子链携 citation，承 ODR-061 未达标记录）——本轮**不改变**该数字，PRODUCT §6.4「100%」仍不宣称达标。
- 网关多一条 facade 路由，属**形状补全**而非旁路；后续若 L0 路由加 `/api` 前缀，此 facade 需同步收敛（已在 Artifacts 注明）。
- `cmd/analysis/handlers_proxy_test.go`（本轮新增 4 用例）命中 `.gitignore:29` 的 `*_test.go` 规则，**未被纳入版本控制**（同 P1-18 类问题，见 §未做项）。

---

## Artifacts

### 后端（1 文件，+24 行）

| 文件 | 变更 |
|---|---|
| `cmd/analysis/handlers_proxy.go` | `registerProxyRoutes` 内新增 `GET /api/factors/:factor_name` facade：`url.Values` 按需装配 `symbol`/`date`（**空值省略，不产生 `?symbol=&date=`**）+ `url.PathEscape` 编码路径段 + `proxyRequest(c, http.MethodGet, dataURL, nil)` 透传 |

### 前端（6 文件：3 新增 + 3 修改）

| 文件 | 变更 |
|---|---|
| `web/src/types/factor.ts` | **新增** `CitationTuple`（`content_hash` 必需，其余四字段可选 = 契约的「缺席即未归档」）+ `FactorCacheEntry`（`citation?: CitationTuple[]`） |
| `web/src/api/factor.ts` | **新增** `getFactorCitation(factorName, symbol, date)` → `GET /api/factors/{encodeURIComponent(name)}?symbol=&date=` |
| `web/src/components/factor/FactorCitation.vue` | **新增** 五元组表格（source/dataset/key/as_of/content_hash/归档状态/操作）+ `traceEvidence()` 跳 `/evidence?content_hash=` + 404 分态 |
| `web/src/pages/FactorCitation.vue` | **新增** `/factors` 路由页壳 |
| `web/src/router/index.ts` | 新增 `path: 'factors', name: 'factors'`（`meta.title = 因子证据`） |
| `web/src/components/layout/AppSidebar.vue` | import `StatsChartOutline` + `navItems` 增 `{ path: '/factors', label: '因子证据' }` |
| `web/src/components/evidence/EvidenceLookup.vue` | 新增 `initialHash` prop（`withDefaults`）+ `contentHash` 初值 + `onMounted` 立即解析 + `watch` 原地重解析 |
| `web/src/pages/Evidence.vue` | `useRoute()` 读 `route.query.content_hash` → `<EvidenceLookup :initial-hash="contentHash" />` |

### 测试（3 文件）

| 文件 | 变更 |
|---|---|
| `cmd/analysis/handlers_proxy_test.go` | **新增** 4 用例：前缀补全（断言上游路径 == `/factors/momentum`）/ 空 query 省略 / 404 透传 / hash-only 透传 —— **gitignored，未入库** |
| `web/src/api/factor.test.ts` | **新增** |
| `web/src/components/factor/FactorCitation.test.ts` | **新增** |
| `web/src/components/evidence/EvidenceLookup.test.ts` | 增回跳 / scenario 用例 |

### 播种（临时，不落盘仓库）

`%TEMP%\p53_seed.ps1`：三步真实链路 —— `POST /api/ingest/raw`（归档，取回 `content_hash`）→ `POST /api/ingest/equitydeep?content_hash=<hash>`（3 快照 ndjson 规范化进 `fundamentals_detail`）→ `POST /sync/factors/roe_dupont_leverage`（计算落缓存）。

---

## Metrics

### 运行时端到端取证（逐字实测）

| # | 断言 | 实测 |
|---|---|---|
| 1 | 播种归档 | `POST /api/ingest/raw` → `content_hash = 8e8d829b142f1dce84d4280f991e589dc8ed3bd2f6af50a0e1bb52c091fcc69c` |
| 2 | 快照规范化 | `POST /api/ingest/equitydeep?content_hash=8e8d829b…` → `{"snapshots":3,"rows":6,"dropped":null}` |
| 3 | **网关 facade 携五元组** | `GET /api/factors/roe_dupont_leverage?symbol=600519.SH&date=20250630` → **HTTP 200**，`citation = [{"source":"equitydeep","dataset":"financial_statements","key":"600519,000858,000333@2024-12-31","as_of":"2024-12-31T00:00:00Z","content_hash":"8e8d829b…c69c"}]` |
| 4 | **一键回溯闭环** | `GET /api/evidence/8e8d829b…c69c` → **HTTP 200** + 原始归档 payload（`{"tickers":["600519","000858","000333"],"batch_id":"eqd-p53-b1","report_period":"2024-12-31"}`） |
| 5 | 未归档 hash = 一等信号 | 全 0 hash → **404**（`unarchived_hash=404`） |
| 6 | 行未命中 = 一等信号 | `symbol=999999.SZ` → **404**（`missing_row=404`） |
| 7 | 多标的坐标一致性 | `000858.SZ` / `000333.SZ` 的 `content_hash` 同为 `8e8d829b…c69c`（`raw_value` 1.7272… / 3.2，`percentile` 16.67 / 83.33）⇒ citation 是**请求批次坐标集合**（与 ODR-061 §粒度一致） |
| 8 | 直连 L0 形状一致 | `GET :8081/factors/roe_dupont_leverage?…` → 同五元组（确认 facade 未改形状） |
| 9 | **SPA 路径（vite :5175 代理）** | `/api/factors/…` → **200**；`spa_not_found` → **404**；`spa_evidence` → **200** |
| 10 | 存档未被污染 | `SELECT content_hash, source, dataset FROM ingest.raw;` → 仅 1 行（`8e8d829b…` \| `equitydeep` \| `financial_statements`） |

### 静态门禁

| 项 | 结果 |
|---|---|
| `go build ./...` | EXIT=0 |
| `go test -count=1 -run TestFactorCitationProxy ./cmd/analysis/` | `ok github.com/ruoxizhnya/quant-trading/cmd/analysis 0.169s`（4/4） |
| `npm test -- src/api/factor.test.ts src/components/factor src/components/evidence` | Test Files 3 passed / Tests **16 passed** |
| `npm run typecheck`（vue-tsc --noEmit） | 通过（0 错） |

---

## 未做项

1. **覆盖面未扩**：citation 仍只覆盖 5 个纵向基本面因子（承 ODR-061）；`Warm` 硬编码 `{Momentum, Value, Quality}` 三者在 C2 全不可覆盖，横截面因子的 citation 与「报告面内嵌 citation」仍**推迟**。
2. **`cmd/analysis/handlers_proxy_test.go` 未入库**：命中 `.gitignore:29` 的 `*_test.go` 规则，`git ls-files` 显示未跟踪 —— 与 P1-18（`pkg/sync` mock 从未入库）同类。本轮**如实记录，未以 `git add -f` 绕过**（是否放开忽略规则属独立议题，须与仓库测试文件策略一并裁决）。
3. **facade 前缀依赖**：facade 之所以存在，是因 L0 citation 路由**无 `/api` 前缀**；若日后 L0 统一加前缀，此 facade 应同步收敛（避免成为第二形状）。
4. **`/factors` 页无 E2E 用例**：本轮为 vitest 组件/API 层测试 + 运行时手工取证，未写 Playwright spec（e2e 侧另有 ODR-063 遗留的期望侧对齐清单 P1-31）。

---

## Lessons Learned

1. **「已交付但未被消费」的契约必须显式记账**：ODR-061 C2 落地后，citation 在 `web/src` 对 `citation` 的命中数长期为 0（同 ODR-060 切片前对 `evidence` 的 0 命中）。**契约落地 ≠ 链路闭合**，验收点应写作「有消费方 + 有运行时取证」，而非「有实现」。
2. **路由形状差异要先于实现确认**：本轮真正的成本不在写 facade，而在识别「L0 无前缀复数 / analysis 有前缀单数 / SPA 约定 `/api`」三方不通。若按直觉复用既有单数路由，会写出一个「200 但永远没有 citation」的假成功。
3. **透传语义要显式声明不变量**：facade 注释里写死「404 是一等答案」「hash-only 必须原样抵达」，使后续维护者不会「顺手」把 404 归一成 500 或给缺席字段补默认值——这两条恰是 ADR-022 §5 与 ODR-061 最容易在网关层被无意破坏的地方。
4. **验收方式的裁决应在开工前而非收工后**：AskUserQuestion 先锁定「要跑运行时」，才使得 ODR-061 的遗留验收点能在本轮一次收口，避免又一次「静态全绿但链路未证」。

## 附注（取证工程坑，承 ODR-063）

- PowerShell 5.1 写 JSON body 必须 `[IO.File]::WriteAllText`（无 BOM）；`Set-Content -Encoding UTF8` 带 BOM 会让 `json.Unmarshal` 失败。
- 中文字段名（`资产总计` / `归属于母公司所有者权益合计`）在 PowerShell 脚本中须以 `\uXXXX` 转义，否则编码链路不可控。
- `config/data-service.yaml` 的 `database.host` 默认 `"postgres"`（容器名），本地起服必须 env 覆盖为 `localhost`（viper AutomaticEnv）；analysis 侧直接设 `DATABASE_URL` 最稳（`initStore` 对含 `${` 的模板值会回退到 `database.*` 拼装）。
- `relation "sync_jobs" does not exist` 属迁移前 worker 探测竞态噪声，非缺陷。