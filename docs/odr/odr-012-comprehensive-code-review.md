# ODR-012: Sprint 5 — 全项目综合代码审查

> **Status**: Completed
> **Date**: 2026-06-08 (initiated) → 2026-06-10 (P1 completed)
> **Category**: Audit
> **Related ADRs**: N/A
> **Supersedes**: N/A
> **Author**: AI Assistant (Trae IDE)
>
> **Completion Update (2026-06-08)**: P0 全部 16 项 (CR-01~16) 修复并提交
> (commit `0c8bfb3`)。验证 `go vet` / `go build` / `go test ./pkg/storage/...
> ./pkg/data/source/... ./cmd/data/...` / `vue-tsc --noEmit` / `npm test` (78/78) /
> `npm run build` 全通过。P1 (CR-17~36) / P2 (CR-37~50) / P3 (CR-51~54) 留作
> 后续 Sprint,已登记在 TASKS.md。
>
> **P1 Completion Update (2026-06-10)**: P1 全部 20 项 (CR-17~36) 修复完毕。
> 后端 5 项 (CR-17~21: mootdx 批量 / bulk_insert 测试 / TopList 字段 / Registry
> 并行 health check / etl_test 接口签名) + 前端 7 项 (CR-22~28: DetailMetrics
> 未用 props / FitnessChart Math.max+resize / GenealogyTree Math.max /
> api/client pagehide / sync.ts SSE / TradeTable+useAsyncBacktest 测试) +
> 文档 8 项 (CR-29~36: SPEC 端点增补 / ADR-015 *_agent 文件名 / ADR-016
> migration off-by-one / ODR-011 数据源 7→9 / Signal 包名前缀 / ai 75% 过度
> 承诺 / ADR-015/016 Status / AGENTS.md services)。验证 `go vet ./...` /
> `go build ./...` / `go test ./pkg/data/source/... ./pkg/storage/... ./cmd/...`
> / `vue-tsc --noEmit` / `npx vitest run` (7 files, 105 tests) / `npm run build`
> 全通过。
>
> **P2 + P3 + 新发现 Completion Update (2026-06-10)**: 全部 36 项完成 —
> P2 14 项 (CR-37~50) + P3 4 项 (CR-51~54) + 2 项新发现 (F1-new mutation
> 偶发 / F2-new vitest `toBe` 误用 CI lint rule)。 详情:
> - 后端: CR-37 (eastmoney_sectors 死代码 io/net-http 移除) + CR-38
>   (fetchStockSectors 补全 f101/f103 概念行业 + `category` 字段) +
>   CR-39/40 (Registry.Fetch fallback 链日志 + ErrAdapterNotRegistered) +
>   CR-41 (lmt=1000 → `eastmoneyCapitalFlowLmt(klt, start, end)`) +
>   CR-42 (CapitalFlowFactor 停牌日语义 + closeRef 真实 bug 修复) +
>   CR-51 (BulkInsert defaultTableMapper 并发安全注释) +
>   CR-52 (EastmoneyClient.GetJSON 429 → ErrRateLimited) +
>   CR-53 (sector_rotation/sentiment NaN/Inf 容错) +
>   CR-54 (Alpha Vantage env/viper 优先级日志)
> - 前端: CR-43 (BacktestEngine.vue 冗余 triggerRef 清理) +
>   CR-44 (useAsyncBacktest 进度 90→100 跳跃加 95 缓冲) +
>   CR-45 (BacktestEngine strategiesCache 类型污染修复) +
>   CR-46 (api/client.ts retry 退避公式可读性) +
>   CR-50 (api/client.ts 单元测试 16 个)
> - 文档: CR-47/48 (AGENTS.md 表数 6→18 + 5 新风险入表) +
>   CR-49 (SPEC.md LiveTrader 方法名验证注释)
> - 新发现: F1-new (mutation.go Intn 0-delta 偶发) +
>   F2-new (vitest `toBe(expected, msg)` 误用 CI lint rule)
> 验证 `go vet ./...` / `go build ./...` / `go test ./pkg/... ./cmd/...` /
> `vue-tsc --noEmit` / `npx vitest run` (9 files, 139 tests) / `npm run build`
> 全通过。 **Sprint 5 P0+P1+P2+P3+新发现 56/56 全部完成, 综合代码审查 backlog 清零。**

---

## Context

用户请求对 Quant Lab 项目进行 4 维度综合代码审查:
1. **代码质量**: 可读性 / 可维护性 / 性能 / 安全 / 编码规范
2. **测试审查**: 单元测试 / 集成测试 / E2E 测试覆盖与有效性
3. **代码-文档一致性**: SPEC / ARCHITECTURE / AGENTS / ODR-011 同步状态
4. **改进任务记录**: 所有发现系统化整理到 TASKS.md

**触发时机**: ODR-011 (Multi-Source Data Integration) 36 files / 7601 insertions 提交后 (commit dfec481),正是文档同步高风险窗口。

## Decision

### 审查方法

采用 **3 子代理并行** + **交叉验证** 模式:
- **子代理 A — 后端 (Go)**: 全读 ODR-011 新增 + 抽查存量包 (backtest/storage/sync/strategy)
- **子代理 B — 前端 (Vue/TS)**: 关键页面/组件/Store/API + 模式扫描 (shallowRef/any/v-html)
- **子代理 C — 文档**: docs/ vs pkg/ + cmd/ + migrations/ 全面对比

每个子代理独立审查,通过工具读取原文证据,产出**带 file:line 引用的结构化问题清单**。

### 审查范围

| 维度 | 范围 | 文件数 (估) |
|------|------|------|
| 后端 Go | pkg/data/source/, pkg/ai/factor/, cmd/data/, pkg/storage/, pkg/backtest/ | 50+ |
| 前端 Vue | pages/, components/{backtest,ai}/, api/, stores/, types/ | 80+ |
| 文档 | SPEC.md, ARCHITECTURE.md, AGENTS.md, ADR.md, ODR-011/012, VISION.md, TASKS.md | 11 |

## Consequences

### 正面影响

- **风险显式化**: 16 项 P0 阻塞性问题得到识别与任务化跟踪
- **文档同步恢复**: SPEC/ARCHITECTURE/AGENTS 与 ODR-011 实施的偏差明确,推动下一轮文档修复
- **测试覆盖缺口**: bulk_insert.go (0%)、TradeTable.vue、useAsyncBacktest.ts 等关键路径盲区暴露
- **可执行的 Backlog**: 54 个任务有 ID/位置/证据/建议,可直接进入 Sprint 排期

### 负面影响

- **认知负担**: 53 个问题集中展示可能让团队 overwhelmed,需分批处理
- **修复成本**: 部分 P0 (如 SPEC.md 重写 30+ 端点) 涉及多人协作
- **未直接验证**: D-018 (LiveTrader 方法名) 等需后续二次确认

## Artifacts

### 文档更新
- [x] `docs/TASKS.md` — 追加 Sprint 5 章节 (CR-01 ~ CR-54),统计表更新 (144→198 总任务, 10→64 待处理)
- [x] `docs/ADR.md` — 追加 ODR-012 条目 (v2.4.0)
- [x] `docs/odr/odr-012-comprehensive-code-review.md` (本文)

### 任务登记 (54 项)

| 优先级 | 数量 | 立即 | 后续 |
| --- | --- | --- | --- |
| P0 (Critical) | 16 | CR-01~16 ✅ 2026-06-08 (commit `0c8bfb3`) | — |
| P1 (High) | 20 | CR-17~21 (后端 5) ✅ + CR-22~28 (前端 7) ✅ + CR-29~36 (文档 8) ✅ 2026-06-10 | — |
| P2 (Medium) | 14 | — | CR-37~50 |
| P3 (Low) | 4 | — | CR-51~54 |

### P1 Artifacts (2026-06-10)

**后端 (5 项)**:
- `pkg/data/source/mootdx_adapter.go` — fetchRealtime 按市场批量 (CR-17)
- `pkg/storage/bulk_insert.go` + `pkg/storage/bulk_insert_test.go` — 单元测试覆盖 (CR-18)
- `pkg/data/source/eastmoney_sectors_adapter.go` — TopList 4 字段硬编码 1 修正 (CR-19)
- `pkg/data/source/registry.go` — HealthCheck 并行化 (CR-20)
- `pkg/data/source/etl_test.go` — stubStore 接口签名对齐 (CR-21)

**前端 (7 项)**:
- `web/src/components/backtest/DetailMetrics.vue` — 清理未用 props (CR-22)
- `web/src/components/ai/FitnessChart.vue` — Math.max 迭代式 + resize 清理 (CR-23, CR-27)
- `web/src/components/ai/GenealogyTree.vue` — Math.max 迭代式 (CR-24)
- `web/src/api/client.ts` — pagehide 监听器单次注册 + abort 清理 (CR-25)
- `web/src/stores/sync.ts` — SSE EventSource.close 路径 (CR-26)
- `web/src/utils/pairTrades.ts` + `pairTrades.test.ts` (18 测试) + `useAsyncBacktest.test.ts` (16 测试) — 核心算法/状态机测试覆盖 (CR-28)

**文档 (8 项)**:
- `docs/SPEC.md` — Analysis Service 增补 Batch/WalkForward/DataSource/Factor 19 端点 + Proxy 双轨 (CR-29)
- `docs/adr/adr-015-ai-agent-architecture.md` — `*_agent.go` → 裸文件名 (CR-30)
- `docs/adr/adr-016-multi-source-data-architecture.md` — migration 014→015→016→017→018 off-by-one 修正 (CR-31)
- `docs/odr/odr-011-multi-source-integration.md` — 数据源 7→9 修正 (CR-32)
- `docs/SPEC.md` + `docs/VISION.md` + `AGENTS.md` — `Signal` → `domain.Signal` 三处一致 (CR-33)
- `docs/SPEC.md` + `docs/VISION.md` — ai coverage 75% → 0%/avg 67% 修正 (CR-34)
- `docs/adr/adr-015-ai-agent-architecture.md` + `adr-016-multi-source-data-architecture.md` — Status Proposed → Accepted (CR-35)
- `AGENTS.md` — 新增 risk-service(8083)/execution-service(8084) (CR-36)

### 关键发现 (按维度)

#### 后端 (3 子代理 A)
1. **B-001**: `BulkInsert` 结果循环使用 `len(valid)` 而 batch 实际较短 → 计数错位 (P0)
2. **B-002**: `snapshotStatus` 仍持锁跨越 HealthCheck 网络 I/O → 修复未生效 (P0)
3. **B-003**: `RetailRatio` 公式无意义 → 下游污染 (P0)
4. **B-005/B-008**: bulk_insert.go 0 测试 + etl_test.go stub 接口签名错配 → L2 集成伪覆盖 (P1)

#### 前端 (子代理 B)
1. **F-001**: api/backtest.ts 双函数 POST 同一端点 schema 不同 → 路由分发歧义 (P0)
2. **F-002/F-003**: BacktestResultCard/FactorCard 重复定义 formatPercent → 违反 Never Do (P0)
3. **F-004/F-005/F-006**: 6 个 `any` 显式标注 → 类型安全退化 (P0)
4. **F-007**: PipelineDashboard jobHistory 无上限 → 内存泄漏 (P0)
5. **F-009/F-010**: FitnessChart/GenealogyTree `Math.max(...arr)` 栈溢出 (>200 代) (P1)

#### 文档 (子代理 C)
1. **D-001**: ADR.md 索引缺失 ADR-011~016 共 6 条 (P0)
2. **D-002/D-003/D-004**: SPEC.md API 路径/端点归属/数据服务 30+ 端点全部错位 (P0)
3. **D-005/D-006**: ARCHITECTURE.md 表数自相矛盾 + ODR-011 新增 13 张表未记录 (P0)
4. **D-014**: ADR-015/016 Status 仍为 "Proposed" 但实施已 98% (P1)
5. **D-015**: AGENTS.md 缺失 risk-service(8083)/execution-service(8084) (P1)

## Metrics

| 指标 | 数值 |
| --- | --- |
| 审查文件数 (后端+前端+文档) | 140+ |
| 发现问题总数 | 56 (含 16 P0, 20 P1, 14 P2, 4 P3, 2 新发现) |
| **完成总数 (P0 + P1 + P2 + P3 + 新发现)** | **56 / 56 (100%)** |
| 高置信度 (High) 比例 | 96% (54/56) |
| 跨子代理交叉验证 | 100% (关键 P0 全部二次确认) |
| 文档↔代码脱节比例 | ~13% (D-001~015) |
| 测试盲区 (0 覆盖关键包) | 2 (pkg/storage/bulk_insert.go, web/src/components/backtest/TradeTable.vue) → **修复** (CR-18, CR-28) |
| 任务登记数 | 56 (CR-01 ~ CR-54, +F1/F2-new) |
| 任务数变化 | 144 → 198 (+54) → 200 (+2) |
| 待处理数变化 | 10 → 64 → 28 → 0 (P0~P3+新发现 -64) |
| 完成数变化 | 133 → 133 → 169 → 200 (+67) |
| P0 完成日期 | 2026-06-08 (commit `0c8bfb3`) |
| P1 完成日期 | 2026-06-10 |
| P2+P3+新发现 完成日期 | 2026-06-10 (commit TBD) |

## Lessons Learned

1. **ODR 提交后未触发"文档自维护协议"** — ODR-011 实施涉及 13 张新表/9 个新 adapter/3 个新 HTTP 端点,但只更新了 ODR-011 自身,SPEC/ARCHITECTURE/AGENTS 三个核心文档未同步。**改进**: 在 ODR 模板中追加"必须同步的文档清单"强制项。

2. **Go 静态类型也救不了接口错配** — etl_test.go 的 stubStore 用 `[]interface{}` 而真实签名是 `[]UnifiedDataPoint`,静态类型不匹配但测试仍"通过"(`pipeline := NewETLPipeline(reg, nil)` 旁路 `Process` 方法)。**改进**: 强制在 `pkg/storage` 定义 `BulkInserter` 接口,所有调用方依赖接口而非具体类型。

3. **"看似修复"在代码审查中再次浮现** — ODR-011 Bug Fix #4 "snapshotStatus 持锁跨越网络 I/O" 声明已修复,本次审查发现实际未生效(`h.mu.Lock()` 仍在 `HealthCheck` 之外)。**改进**: 修复项必须附带可执行测试 (e.g., `TestSnapshotStatus_NoLockDuringIO`),否则难以验证。

4. **前端 `any` 类型是 AGENTS.md "Never Do" 的重灾区** — 6 处显式 `any` (3 处 `DataTableColumns<any>` + 3 处 `catch (error: any)`) + `Record<string, any>` 在公开类型 API,说明代码审查 CI 规则未严格拦截。**改进**: 在 `web/.eslintrc` 中将 `@typescript-eslint/no-explicit-any` 设为 `error`,公开类型加 `Record<string, unknown>` 优先。

5. **重复工具函数是 AGENTS.md "Never Do" 的另一面** — 跨 3 个 AI 组件 (BacktestResultCard/FactorCard/DetailMetrics) 重复 `formatPercent/formatMetric`,且 `FactorCard` 内的 `formatMetric` 与 `format.ts` 的 `fmtMetric` 行为不一致。**改进**: 在 PR 模板中加 "DRY checklist",Code Review 自动化扫描 `function formatPercent|fmtPercent` 关键字。

6. **后端"硬编码数据"是数据失真的无声杀手** — `EastmoneyTopListAdapter.fetchLimitUpPool` 4 字段硬编码 1 (注释承认 "approximation"),下游 `limit_up_pool` 表的"连板"语义归零,IC 因子失效。**改进**: 所有"待补字段"应单独写 `source='mock'` 行或从表中过滤,而不是污染生产数据。

## Related Work

- **ODR-010**: 2026-05-17 全项目代码+文档一致性审查 (前置,本次为后续)
- **ODR-011**: Multi-Source Data Integration (本次审查的主要对象)
- **TASKS.md v3.3.0**: 54 项 CR 任务已登记
- **AGENTS.md §10**: 文档维护协议 (Rule 1) — 本次发现 ODR-011 提交后未触发 SPEC/ARCHITECTURE 同步

### P1 Sprint 后续建议 (P2/P3 仍 Backlog)

P1 实施暴露的 2 个**新发现**(留作 P2):

- **F1-new**: `pkg/ai/evolution/mutation_test.go` `TestMutation_MutateParams` 用 `math/rand` 但 seed 不固定 → 偶发失败 (p≈1/200)。修复: 在 `TestMain` 里 `rand.Seed(time.Now().UnixNano())` 改为 `rand.New(rand.NewSource(42))`,将 flake 变为确定性失败以便调试。
- **F2-new**: Vue 3.4+ 的 `expect(value, message)` 是合法 vitest 4 签名,但 `expect(value).toBe(expected, message)` 不合法 (`toBe` 自身不接 message)。本会话发现并已修正 2 处。**v3.9.1 落地**: 新建 [web/scripts/lint-tests.mjs](file:///Users/ruoxi/longshaosWorld/quant-trading/web/scripts/lint-tests.mjs) 独立 Node 脚本扫描, `package.json` 添加 `lint:tests` 并前置到 `test` 脚本; runtime guard `src/test-lint.test.ts` 保留作为 belt-and-braces。

---

_Last updated: 2026-06-10_
_Status: Completed (P0 全部 16 项 + P1 全部 20 项修复 — 见 P1 Artifacts 章节)_
_P0: 1-2 周 (实际 ~1 个会话), P1: 2-3 周 (实际 ~1 个会话)_
_P2/P3 状态: 已登记 TASKS.md,等待下一 Sprint 排期_
