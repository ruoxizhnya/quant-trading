# ODR-065: Full-Stack Static Review (全栈静态审查)

> **Status**: Completed
> **Date**: 2026-09-21
> **Category**: Audit
> **Related ADRs**: n/a（发现项的修复将另行决策）
> **Supersedes**: n/a（上一轮系统级审计为 [ODR-043](odr-043-comprehensive-audit-2026-06-29.md)，本轮为其后的全量复检）

---

## Context

- **触发**: 用户要求以顶尖软件团队标准对项目"从上到下"全面审查，先制定审查策略、再按策略执行。
- **背景**: ODR-043（2026-06-29）之后，Phase 4（Hermes Agent / MCP 工具层）与统一研究平台 P1~P5（ODR-048 ~ ODR-064）大量代码落库，`main` 领先 `origin/main` 51 提交，需新一轮全量健康检查。
- **环境限制**: 本机无 Go 工具链（`go` 不在 PATH，全盘搜索无 `go.exe`），`go build` / `go vet` / `go test -race` 未执行。**全部结论为静态审查 + 源码逐行取证**，动态复验清单见报告附录 D。

## Decision

1. **三段式方法论**: 策略制定（7 维度：架构一致性 / 回测正确性 / 数据面 / 后端工程 / 安全运维 / 测试质量 / 前端+E2E）→ 并行执行（6 路子代理深挖）→ 交叉验证（主审对全部 Critical/High 逐条回源码复核行号）。
2. **取证纪律**: 每条 Critical/High 必须带 `文件:行号` + 代码摘录；子代理结论未经主审复核不作为定论；正向结论（"存在 bug"）与负向结论（"缺失防护"）区分置信度标注。
3. **误报处置**: 剔除/降级均记录在案（4 项），保证审查过程本身可审计。
4. **报告落盘与归位**: 报告初稿按用户要求落盘于 `docs/review-report/`；2026-09-21 归位至 **`docs/archive/reports-2026-Q3/review-report-20260921.md`**（AGENTS.md Rule 3：报告为一次性快照，进归档层不进常青层），相对链接层级已同步修正；可执行修复任务已登记 `docs/TASKS.md`（P0-7~P0-11 / P1-15~P1-22 / P2-14~P2-16）。

### 发现摘要（5 Critical / 9 High / 6 Medium / 4 Low）

**Critical**：

| # | 问题 | 位置 |
|---|------|------|
| C1 | 日收益率公式把交易现金流误当外部资金流，现金腿被代数化简抹掉 → Sharpe/Sortino/波动率失真 → walk-forward 门禁 → 基因池 fitness 全链路污染 | `pkg/backtest/metrics/performance.go#L78-100` |
| C2 | walk-forward 并发窗口共享同一 `contracts.EngineRunner`（引擎级缓存为共享槽）→ 跨窗口前视 + 数据竞争，注释与实现相反 | `pkg/backtest/walkforward/walkforward.go#L19-23,142-160`、`cmd/analysis/setup.go#L342` |
| C3 | `/api/copilot/save` 路径遍历 + 未过 staticcheck/沙箱的任意代码落盘到插件热加载目录，等同认证后 RCE | `cmd/analysis/handlers_copilot.go#L174-195,278` |
| C4 | RBAC 未接线：`RequireRole` 全仓仅 1 处（`/api/auth/admin`），viewer 可下单（`/api/execution/*`）并可调用全部 19 个 MCP 工具（`/api/tools/*`）；`CanTrade()` 零生产调用点 | `cmd/analysis/handlers_execution.go`、`handlers_tools.go`、`pkg/auth/auth.go#L34` |
| C5 | ETL 写入的 13 类数据中 11 类目标表 DDL 只存在于**不被执行**的 `migrations/*.sql`（015~018）→ 全新部署 `relation does not exist` 硬失败，违反 postgres.go 自定"加表追加内联数组"约定 | `pkg/storage/bulk_insert.go#L48-62`、`postgres.go#L82-96`、`cmd/data/registry_init.go#L171-207` |

**High（9）**: 印花税默认值 0.001 应为 0.0005（2023-08-28 费率史实错误）；涨跌停缺创业板/科创板/北交所档位且无分取整；`*ST` 识别 `name[:2]` 永久失效；测试把 *ST bug 固化为预期断言；MockTrader `RLock` 下经指针写共享对象（`-race` 必报，实害有限已降级定性）；无 100 股整手取整（负向证据）；Windows 沙箱 rlimit no-op；CI 无 `-race` 无前端门禁；docker-compose PG/Redis 暴露 0.0.0.0。

**Medium（6）**: AGENTS.md 架构描述落后于 ADR-023/024 现实；表数口径 38 张与内联实测 22 张不符；双 migrations 死文档；staticcheck 正则黑名单可绕过；`ai-research.spec.ts` 打已删除的 :8086 服务；`fundamentals_detail` 空表无摄取防御。

**Low（4）**: main 领先 origin 51 提交；live engine 组合状态更新（未复核）；legacy HTML 残留；文档导航失效（`docs/odr/` 不存在、ADR 编号漂移）。

**正面发现（6）**: 认证层 fail-closed（CORS 白名单 + loopback 限定 insecure）；DDL 单一真相取舍有书面论证；`order_manager.go` 为并发正确范本（copy-under-lock）；ETL 错误上抛不静默；测试规模真实（61,321 行）；CI 自研一致性元检查（doc links + deploy consistency）。

**误报剔除（4）**: `contracts/` 目录实存（4 文件）；`order_manager.go` 锁外读系误报（42 处访问全在锁内）；MockTrader"撕裂写"降级为 High（写同一确定值）；"静默丢数"修正为"响亮失败"（错误上抛）。

## Consequences

**Positive**:

- 验证体系两大根基问题（度量公式 + 窗口隔离）首次被显式定性并定位到行级；
- 迁移漂移获得根治方案（`TableMapper ↔ 内联 DDL` 一致性断言测试）；
- 审查方法本身可复用：三段式 + 取证纪律 + 误报记录。

**Negative**:

- 修复（C1/C2）落地前，历史回测结论与 walk-forward 报告暂不可信，此前因子筛选结果需作废重评；
- 全新部署的多源数据面开箱不可用（C5），存量库依赖手工执行过 .sql 的隐性运维债；
- ~~15 项修复任务（AUD-01~15）尚未登记 `docs/TASKS.md`~~ → **已于同日登记**（P0/P1/P2 分区，ID=AUD-xx，见 Artifacts）。

## Artifacts

- `docs/archive/reports-2026-Q3/review-report-20260921.md` — 完整审查报告（16 章：策略/概览/Critical·High·Medium·Low 详录/正面发现/误报记录/路线图/覆盖矩阵/文档漂移清单/动态验证清单 + §16 修复实施方案 15 commits）；原 `docs/review-report/` 临时目录已删除
- 新增本 ODR
- 更新 `docs/ADR.md` ODR Index（追加 ODR-065 行）
- 更新 `docs/TASKS.md`（frontmatter 校验日期 + 新增 P0-7~P0-11 / P1-15~P1-22 / P2-14~P2-16，共 16 条，AUD 编号在任务描述中保留回溯）

## Metrics

- 审查范围: Go 544 文件 / 136,571 行；测试 230 文件 / 61,321 行（测试:源码 ≈ 45%）；前端 82 文件 / 9,992 行；文档 158 MD / 30,240 行
- 发现: **24 项**（5 C / 9 H / 6 M / 4 L）；误报剔除 **4 项**
- 复核强度: Critical/High 共 14 项全部由主审回源码取证行号；Medium/Low 部分标注置信度（H6、L2 为负向/未复核）
- 修复任务: 15 条（AUD-01 ~ AUD-15，含验收标准与原子提交范围）

## Lessons Learned

1. **"有测试"不等于"测试对"** — H4 把 *ST 识别 bug 固化为断言 `{"*STXYZ.SH", false}`，修复时会被误判为回归。测试需审"断言方向"，不只审覆盖率。
2. **没有执法者的约定必然漂移** — postgres.go L94-96 自定"加表追加内联数组末尾"约定，C5 恰好违反之；CI 缺 `-race` 使 H5 类竞争从未被捕获。**元检查（mapper↔DDL 断言、CI 门禁）比纪律更可靠**。
3. **授权模型写好不接线等于没有** — `RoleViewer`/`CanTrade()` 语义完整、测试齐全，但零生产调用点；"最后一公里"缺失是安全审查的独立检查项。
4. **文档导航漂移已系统性化** — `docs/odr/` 目录已不存在（ODR 实际位于 `docs/archive/odr/`，本 ODR 遵循现实约定归档于此），AGENTS.md Rule 2 仍引导在 `docs/odr/` 创建；ADR 编号、表数口径同步漂移。修正归入 AUD-14。
5. **子代理结论必须交叉验证** — 本轮 4 项误报/误定性被复核拦截（约占 Critical+High 结论量的 25%），其中一项若直接采信会把 High 报成 Critical、另一项会把正确代码报成缺陷。
