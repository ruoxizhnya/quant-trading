# ODR-060: 阶段 P5 切片 3 — Vue SPA 对接 L0 Evidence API（工作面 2 对齐起步）

> **Status**: Completed
> **Date**: 2026-09-15
> **Category**: Implementation
> **Related ADRs**: [ADR-022](../adr/adr-022-unified-research-platform.md)（§3 四层架构 L3 体验面 / §5 证据服务升级为平台能力）
> **Supersedes**: —
> **Related ODRs**: [ODR-059](odr-059-p5-1-retire-datasource-switch.md)（P5-1 切片 2 — 本记录承接其「未做项」中显式排除的 **P5-1 未关闭原因**）, [ODR-058](odr-058-p5-1-retire-direct-providers.md)（切片 1）, [ODR-050](odr-050-p1-base-contract-landing.md)（L0-3 Evidence API 交付）
> **Author**: AI Assistant

---

## Context

### 触发条件

[ODR-059](odr-059-p5-1-retire-datasource-switch.md) 把 `P5-1` 的关闭条件写死在「未做项」第 4 条：

> `docs/odr/odr-059-*.md:173` — 「`P5-1` 是否关闭 —— 本切片只完成其『**去除旁路取数**』维度；`P5-1` 描述中的『Vue SPA / Research Engine 存量能力**对接 L0 单一数据面 + Evidence API**』尚未开始，故 `P5-1` 整体保持未关闭。」

本记录即该「尚未开始」项中 **Vue SPA 侧**的第一刀：让工作面 2（Vue SPA）**实际消费** L0 的证据服务，而不是仅由 L0 侧「已交付」。

### 现场取证（三条事实）

| # | 取证 | 方法 | 结论 |
|---|---|---|---|
| **a** | **L0 侧已就绪** | 读 `cmd/analysis/handlers_evidence.go`（89 行）+ `cmd/analysis/main.go:375` | L0-3 `GET /api/evidence/:content_hash` handler 已实现且**已注册**在 analysis(:8085)；`store.GetRawIngest` 未命中返回 `(nil, nil)` → handler 映射为 **404 `{"error":"evidence not ingested"}`**，是**一等语义**而非错误 |
| **b** | **工作面 2 零消费** | 全目录 Grep `web/src` 的 `evidence\|content_hash\|citation` | **0 命中** —— 前端从未调用过 Evidence API；`ingest.raw` 的 `content_hash` 坐标在 L3 体验面上**不可达** |
| **c** | **后端零改动即可对接** | 读 `web/vite.config.ts` + `cmd/analysis/handlers_proxy.go` | vite dev proxy 已把 `/api` → `http://localhost:8085`（analysis），Evidence API 天然可达 ⇒ 本切片**不需要任何后端改动** |

### 切片候选与裁决

[ODR-059](odr-059-p5-1-retire-datasource-switch.md) 之后的剩余工作被拆为 4 个候选切片，交用户裁决：

| 候选 | 说明 | 结果 |
|---|---|---|
| **A. 前端对接 Evidence API** | 新增 `web/src/api/evidence.ts` + 证据查询 UI 入口；后端零改动 | ✅ **采纳**（用户裁决） |
| B. SPA 数据管理面对齐 L0 | 修 `web/src/api/sync.ts` 的**死端点**（`/api/sync/status` / `/api/sync/import` / `/api/sync/stream` 在 `cmd/analysis` 与 `cmd/data` 均无注册） |  本切片不做（跨前后端契约变更，风险面大于收益面，须独立评估） |
| C. Research Engine 输出携带 `content_hash` | 让横截面输出（因子 / 回测 / `quant.*`）带 citation 元组 | ⛔ 本切片不做（跨 schema 全链路，高风险） |
| D. 分层/旁路残留清理 | `vite.config.ts` 的 `/market` → `8081`（L3 直连 L0）+ `handlers_proxy.go` 无 `/api` 前缀的遗留镜像路由 | ⛔ 本切片不做（涉及存量读路径，须独立评估） |

---

## Decision

**在 Vue SPA 中新增 L0 Evidence API 的消费层（`api/evidence.ts` + `types/evidence.ts`）与一个证据查询 UI 入口（`/evidence` 页面 + `EvidenceLookup` 组件 + 侧栏导航），使 `content_hash` 在 L3 体验面上可回溯到 `ingest.raw` 的唯一原始记录；后端零改动。**

### 1. API 客户端层

`web/src/api/evidence.ts`（新建）：

```ts
export async function getEvidence(contentHash: string): Promise<RawIngest> {
  return api.get<RawIngest>(`/api/evidence/${encodeURIComponent(contentHash)}`)
}
```

- 复用既有 `api`（`web/src/api/client.ts` 单例，风格同 `api/market.ts` / `api/sync.ts`）。
- **`encodeURIComponent`**：hash 虽约定为 64 位 hex，但路径段拼接**不依赖调用方自律** —— 含 `/` 的输入被编码为 `%2F`，无法逃出路由段。
- **404 不在本层吞掉**：`ApiError.isNotFound` 由调用方判别，本层只做透传（「未摄取」是**语义**，不是**错误**）。

### 2. 类型层

`web/src/types/evidence.ts`（新建）—— 逐字对应 `pkg/storage/ingest_raw.go` 的 `RawIngest` JSON 标签：

| TS 字段 | Go 字段 | 说明 |
|---|---|---|
| `content_hash: string` | `ContentHash string \`json:"content_hash"\`` | sha256 小写 hex 64 字符 |
| `source: string` | `Source string \`json:"source"\`` | 外部源（如 `tushare`） |
| `dataset: string` | `Dataset string \`json:"dataset"\`` | 源内逻辑数据集 |
| `key: string` | `Key string \`json:"key"\`` | 源内查找键 |
| `as_of?: string` | `AsOf *time.Time \`json:"as_of,omitempty"\`` | **可选**（`omitempty` 对应 TS `?`） |
| `payload: unknown` | `Payload json.RawMessage \`json:"payload"\`` | 归档原始 JSON，原样 |
| `fetched_at: string` | `FetchedAt time.Time \`json:"fetched_at"\`` | 归档时间 |

### 3. UI 入口

| 文件 | 变更 |
|---|---|
| `web/src/components/evidence/EvidenceLookup.vue` | **新建** —— `NCard` + hash 输入（空值禁用提交）+ 查询按钮；三态渲染：**命中**（`NDescriptions` 六字段 + `NCode` 展示原样 payload）/ **404 = 未摄取**（`NAlert type="warning"`）/ **其他失败**（`NAlert type="error"`） |
| `web/src/pages/Evidence.vue` | **新建** —— 薄页面壳（`NH1` + `EvidenceLookup`），同 `pages/DataSync.vue` 骨架 |
| `web/src/router/index.ts` | 注册 `/evidence`（`name: 'evidence'`, `meta.title: '证据查询'`） |
| `web/src/components/layout/AppSidebar.vue` | `navItems` 增 `{ path: '/evidence', label: '证据查询', icon: DocumentTextOutline }` |

**关键 UI 语义**：404 与「查询失败」**渲染为不同形态**（warning vs error）—— 这是把「未摄取」立为一等答案在 L3 面上的落点，避免用户把「这个数从没被摄取过」误读为「服务挂了」。

### 4. 明确不做（切片边界）

| 项 | 状态 | 说明 |
|---|---|---|
| 后端 `cmd/analysis` / `cmd/data` 任何改动 | ⛔ **零改动** | L0-3 已交付且已注册（取证 a）；vite 已代理（取证 c） |
| `web/src/api/sync.ts` 的死端点（切片 B） | ⛔ 不动 | 本切片只做「新增消费层」，不夹带契约修正 |
| Research Engine 输出侧 `content_hash`（切片 C） | ⛔ 不动 | 本切片只覆盖 `P5-1` 描述中的「**Vue SPA**」半句 |
| `/market` → `8081` 与遗留镜像路由（切片 D） | ⛔ 不动 | 涉及存量读路径 |

---

## Consequences

### 正面

| 收益 | 说明 |
|---|---|
| **`content_hash` 在 L3 面首次可达** | 工作面 2 从「`ingest.raw` 不可见」变为「可按 hash 回溯唯一原始记录」，`PRODUCT.md:282`「证据完整性 100% 数字可用 `content_hash` 回溯」在 SPA 侧有了操作入口 |
| **「未摄取」成为一等 UI 语义** | 404 不再是错误 toast 而是 warning 态 + 明确文案，消除「无此证据」与「查询失败」的混淆 |
| **后端零风险** | 无 Go 改动 ⇒ 无后端回归面；对接完全建立在既有契约上（ADR-022 §5 的 API 形状未被改动，只是被消费） |
| **契约逐字对齐** | TS 类型与 `pkg/storage/ingest_raw.go` 字段一一对应（含 `as_of` 的 `omitempty` ↔ `?`），避免「前端自造形状」 |
| **`P5-1` 关闭条件被推进一格** | `ODR-059` 未做项第 4 条中「尚未开始」的前半句（Vue SPA 对接 Evidence API）已落地 |

### 负面 / 代价

| 代价 | 说明 | 缓解 |
|---|---|---|
| 新增 1 个页面 + 1 个组件 + 1 个 API 模块 + 1 个类型文件 | 前端表面积增长 | 均为薄层（组件 ~110 行），且复用既有 `api`/`ApiError`/naive-ui 组件，无新依赖 |
| 证据查询需用户**自备 `content_hash`** | 页面不提供「浏览全部证据」列表 —— 当前 Evidence API **只有按 hash 取单条**的契约 | 与 ADR-022 §5 的契约一致（citation 里本来就带 hash）；「列举」若要支持须先扩 API，属独立议题 |
| 新增导航项（侧栏 10 → 11 项） | 侧栏变长 | 语义上与「数据同步」同组（数据治理面），位置紧邻 |

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| 测试/构建回归 | 低 | `vue-tsc --noEmit` EXIT=0；`vitest run` 13 files / 162 tests 全通过；`vite build` EXIT=0 |
| 类型与后端漂移 | 低 | TS 类型逐字段对照 `pkg/storage/ingest_raw.go`（§2 表格）；后端改字段时因 `ingest.raw` 为**不可变**归档、形状稳定 |
| 404 语义被误用 | 低 | API 层只透传、组件层显式 `isNotFound` 分支；`EvidenceLookup.test.ts` 与 `evidence.test.ts` 各有专门用例锁定该语义 |

---

## Artifacts

### 前端

| 文件 | 变更 |
|---|---|
| `web/src/api/evidence.ts` | **新建**（`getEvidence`，`encodeURIComponent` + 404 透传） |
| `web/src/types/evidence.ts` | **新建**（`RawIngest`，逐字对应 `pkg/storage/ingest_raw.go`） |
| `web/src/components/evidence/EvidenceLookup.vue` | **新建**（查询表单 + 三态渲染） |
| `web/src/pages/Evidence.vue` | **新建**（页面壳） |
| `web/src/router/index.ts` | 注册 `/evidence` 路由 |
| `web/src/components/layout/AppSidebar.vue` | 增「证据查询」导航项 + `DocumentTextOutline` import |

### 测试

| 文件 | 变更 |
|---|---|
| `web/src/api/evidence.test.ts` | **新建**（3 用例：请求路径 / hash URL 编码 / 404 透传不变） |
| `web/src/components/evidence/EvidenceLookup.test.ts` | **新建**（4 用例：空值禁用提交 / 命中渲染六字段 + payload / 404 → 「未摄取」/ 非 404 → 错误态且**不**显示「未摄取」） |

> 测试文件按本仓 `.gitignore`（忽略 `*_test.go`，前端测试不在忽略范围）正常纳入版本控制。

### 文档

| 文件 | 变更 |
|---|---|
| `docs/odr/odr-060-p5-1-frontend-evidence-api.md` | **新建**（本记录） |
| `docs/TASKS.md` | 头部版本 3.34.0 → 3.35.0；`P5-1` 行补切片 3；阶段 P5 进展注改为「切片 3 完成」；统计表 Sprint 8 行 + 变更日志 |
| `docs/ADR.md` | ODR 索引新增 ODR-060；尾注 ODR 59 → 60；index 3.15.0 → 3.16.0 |

---

## Metrics / 验证

| 项 | 结果 |
|---|---|
| 前端 `npm run typecheck`（`vue-tsc --noEmit`） | **EXIT=0** |
| 前端 `npm test`（`lint:tests` + `vitest run`） | **13 files / 162 tests 全通过**（新增 2 文件 / 7 用例） |
| 前端 `npm run build`（`vue-tsc -b && vite build`） | **EXIT=0**（`✓ built`） |
| **验收点**：`web/src` 出现 Evidence API 消费 | ✅ 全目录 Grep `evidence\|content_hash` → **8 文件命中，全部为本切片新增**（切片前为 0 命中，取证 b）；无后端改动 |
| 后端未改动 | ✅ 本切片 0 个 `.go` 文件变更 |

---

## 未做项（明确排除）

1. **`P5-1` 是否关闭** —— 本切片只覆盖其描述中的「**Vue SPA** 存量能力对接 L0 单一数据面 + Evidence API」半句；「**Research Engine**」侧（横截面输出携带 citation 元组）与「**去除旁路取数**」的剩余维度（`/market` → `8081` 的 L3 直连、`handlers_proxy.go` 遗留镜像路由、`api/sync.ts` 死端点）未动，故 `P5-1` **整体仍未关闭**。
2. **SPA 数据管理面对齐 L0（切片 B）** —— `web/src/api/sync.ts` 的 `/api/sync/status` / `/api/sync/import` / `/api/sync/stream` 在 `cmd/analysis` 与 `cmd/data` 均无注册（全仓 Grep 0 命中）；data 侧实际交付的是 `/api/sync/jobs*`。修正属**跨前后端契约变更**，须独立评估。
3. **Research Engine 输出携带 `content_hash`（切片 C）** —— 让 `quant.*` / 因子 / 回测输出带 citation 元组，使乘积侧数字也可回溯。跨 schema 全链路，高风险。
4. **分层/旁路残留清理（切片 D）** —— `web/vite.config.ts` 的 `/market` → `8081`（L3 → L0 直连，绕过 analysis 代理）与 `cmd/analysis/handlers_proxy.go` 无 `/api` 前缀的遗留镜像路由。
5. **证据「列举」能力** —— Evidence API 仅有按 hash 取单条；OS 面「浏览全部证据」须先扩 API（属契约变更）。
6. **`docs/odr/odr-059-*.md` / `odr-058-*.md` 原文回写** —— 按本仓既有口径（ODR-057 裁决），**历史决策记录保留原文不回写**；其「未做项」在其时点成立，本记录以 `Related ODRs` 承接。

---

## Lessons Learned

1. **「已交付」≠「被消费」** —— L0-3 Evidence API 从 ODR-050 起就在仓库里、也注册在 analysis 上、vite 也已代理，形态完整；但工作面 2 对 `content_hash` 的引用数为 **0**。**验收「两工作面共享 L0-L2」不能只看 L0 侧交付，必须看另一侧的消费点是否 > 0** —— 否则「共享」只存在于架构图上。
2. **错误语义要跟着跨成 HTTP 边界** —— `GetRawIngest` 未命中返回 `(nil, nil)` → handler 映射 404，这是后端侧的「一等答案」设计。若前端把它当普通错误 toast 掉，该设计在 L3 面上就退化成了「又是一次失败」。**承接这类语义时，UI 必须为它保留独立形态**（本切片：warning vs error）。
3. **低风险切片的价值在于把「契约」变成「可达」** —— 本切片后端零改动、零新依赖，却让 `PRODUCT.md` 的「100% 数字可 `content_hash` 回溯」从断言变成可操作入口。当一个大任务（`P5-1`）含多处异构风险时，**先做「只消费既有契约」的那一格**，比先动契约更划算。

---

_Last updated by: AI Assistant — 2026-09-15 (P5-1 切片 3: Vue SPA 对接 L0 Evidence API)_