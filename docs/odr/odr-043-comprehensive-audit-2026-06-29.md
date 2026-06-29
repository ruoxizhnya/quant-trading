# ODR-043: 全项目可维护性、模块化与测试覆盖综合审计 (2026-06-29)

> **Status**: Accepted
> **Date**: 2026-06-29
> **Category**: Audit
> **Related ADRs**: ADR-014 (Strategy Framework, 已被 ADR-020 §6 取代), ADR-015 (AI Agent), ADR-019 (Service Merge), ADR-020 (Engine Decomposition)
> **Supersedes**: None (延续 ODR-012 / ODR-013 的审计脉络)

## Context

用户在重构 brainstorming 之后的 session 中要求执行"对现有代码库进行全面的可维护性、模块化设计及测试覆盖情况审查"，并将改进项落地到任务列表 + 长效文档。本次审计在 ODR-012/013 的基础上覆盖更新维度（静态质量、模块化、测试覆盖、前端质量），并验证了上一 session 重构 brainstorming 中的若干假设。

触发原因：
1. AGENTS.md §10 Rule 2 要求审计类工作完成后 48h 内创建 ODR
2. 上一 session 提出的 4 个重构方向（AI 一等居民 / 回测核心 / 统一执行 / 数据正常化）需要事实基线
3. Phase 4 验收表（ADR-015 自称 98%）与 VISION.md（仍标 In Progress）矛盾，需要独立审查

## Decision

本次审计采用 4 个并行 Explore agent 分维度执行（Go 静态质量 / Go 模块化 / Go 测试覆盖 / 前端质量），每个 agent 输出 Top 10 问题清单，最终综合为 4 类共 40 个具体问题点。

**关键决策（本次审计确认 / 推翻的）**：

### D1: 推翻"tracker.go 与 mock_trader.go 高度重复需要 ExecutionCore 合并"假设
- 重复度仅 ~30%（150 行核心逻辑 + 22 处费率公式重复），70% 是各自独有功能
- **决定**: 不合并执行器，改为抽取 3 个共享原语包 (`pkg/fees`/`pkg/settlement`/`pkg/portfolio`)
- 取代方案: ExecutionCore 统一内核 — **拒绝**，会得到臃肿上帝对象，违背 ADR-020 拆分方向

### D2: 确认"AI 不是第一等居民"的诊断，且问题比 brainstorming 中更严重
- `Pipeline.Execute` 回测阶段被 handler 传 nil runner，端到端实际跑不通
- AI 服务(8086) 只暴露 2 个研究端点，pipeline 端点反而注册在 analysis(8085)
- 3 条流水线碎片化、3 种回测调用方式、LLM 输出解析用字符串扫描
- **决定**: 不重写 AI 服务架构，按"修 bug → 收敛流水线 → 扩展表达式引擎 → Tools Registry"渐进推进

### D3: 确认"数据层 A 股特化"诊断，但落地基础已具备
- 22 张表（不是 18 也不是 32），10 张 A 股特化
- domain 层 14 个 A 股特化字段散布 6 个文件
- **决定**: 不做 big-bang schema 重构，采用"软分层 + view 过渡"，新增 `pkg/domain/market/` 子包

### D4: 文档漂移严重，违反 VISION Principle 8
- ADR/ODR 数量偏差（AGENTS.md 停在 15/8，实际 20/42）
- 服务拓扑偏差（risk/execution 已合并但 ARCHITECTURE/VISION/SPEC 未更新）
- 归档文档仍当活跃引用（NEXT_STEPS/IMPLEMENTATION_PLAN）
- ADR 状态过时（ADR-014 被 ADR-020 §6 取代未标记、ADR-019 §1 部分实施未更新、ADR-020 文件头 Proposed 实际 Accepted）
- **决定**: 本次会话内立即修复 AGENTS.md + ADR.md 索引，其余文档修复纳入任务

## Consequences

### Positive
- 4 个维度 40 个具体问题点为后续 sprint 提供明确 backlog
- 推翻 brainstorming 中"统一执行内核"假设，避免错误的大重构方向
- 确认 3 个共享原语包方案（fees/settlement/portfolio）作为 P1 优先级
- 暴露 5 个真实 bug 应立即修复（在审计任务中标记 P0）
- 文档漂移得到即时修复（AGENTS.md + ADR.md 索引）

### Negative
- 审计发现的工作量较大（Top 10 总和约 200+ 小时），需要分 sprint 推进
- 暴露的 AI 流水线 bug（nil runner）意味着 Phase 4 验收表 98% 的说法失实
- 前端 AI Research 模块 2500 行代码不可达，需要产品决策（上线 or 删除）

## Artifacts

### 本次审计产出
- `docs/odr/odr-043-comprehensive-audit-2026-06-29.md` (本文件)
- 4 个并行 Explore agent 的详细审查报告（保存在 session memory 中）

### 本次会话内修改
- `docs/ADR.md` — 在 ODR Index 追加 ODR-043 条目
- `AGENTS.md` §10 文档分类表 — 修正 ADR/ODR 数量（15/8 → 20/42）
- `AGENTS.md` §13 当前状态 — 修正测试覆盖率数据（更新为 2026-06-29 实测）
- `AGENTS.md` §14 已知问题 — 追加本次审计发现的 5 个真实 bug + 文档漂移条目

### 待创建/修改（已落入任务列表）
- 见"任务追踪"小节，本次审计共生成 12 个具体改进任务

## Metrics

### 审计覆盖
- 审计维度: 4（Go 静态质量 / Go 模块化 / Go 测试 / 前端质量）
- 审计 agent 数: 4（并行执行）
- 审查文件数: ~330 个 Go 文件 + 67 个前端文件 + 关键文档

### 发现总量
- **Critical 问题**: 12 个（7 God File + 5 真实 bug）
- **High 问题**: 18 个（含分层违规、类型重复、覆盖率缺口、文档漂移）
- **Medium 问题**: 10 个（命名冲突、注释缺失、测试组织）

### 关键数字
- 总代码行数: Go ~75k 行 + Vue/TS ~12.5k 行
- Go 总测试覆盖率: **62.9%**（statement-weighted，无外部依赖时）
- God File (>800 行): **7 个**（Critical）
- 未格式化 Go 文件: **237 个**（gofmt -l）
- 真实 bug: **5 个**（Pipeline nil runner / 硬编码路径 / 美股默认池 / 字符串扫描 JSON / 0.00025 硬编码）
- 前端 AI Research 不可达代码: **~2,500 行**
- 前端死代码 EmergencyFlatten.vue: **311 行**
- 跨层反向依赖: **5 处**（strategy→ai / strategy→internal / storage→sync / marketdata→live / compliance→live）
- 接口重复定义: **3 处**（BacktestRunner × 3 / JobStore × 2 / FactorStore × 2）

### 文档漂移修正
- AGENTS.md ADR 数量: 15 → 20
- AGENTS.md ODR 数量: 8 → 42（本 ODR 后）
- AGENTS.md 测试覆盖率: 更新为 2026-06-29 实测值
- AGENTS.md Known Issues: 新增 6 条

## Lessons Learned

### L1: brainstorming 假设需要代码验证
上一 session 重构 brainstorming 中提出的"tracker/mock_trader 高度重复需要 ExecutionCore"假设，经代码审查后被推翻（重复度仅 30%）。后续重大架构决策应先做事实核查再 brainstorm。

### L2: Phase 4 验收表需独立验证
ADR-015 自带验收表声称 98%，但本次审计发现 Pipeline.Execute 端到端跑不通、AI 服务无持久化、前端 AI Research 模块不可达。验收表应区分"代码实现完成"与"业务指标达成"两个维度。

### L3: 文档维护协议执行不严
AGENTS.md §10 Rule 4 / Rule 5 明确要求归档操作后更新 Document Index、ADR 状态变化后同步索引，但 ODR-021 服务合并、ADR-020 引擎拆分等变更后均未执行。需要在 CI 加入文档一致性检查。

### L4: 全局单例 + 双注册表是技术债温床
`pkg/strategy` 同时存在 `DefaultRegistry` + `DefaultOldRegistry` + `GlobalPluginLoader` 三个全局单例，迁移未完成。这是测试隔离困难、热加载实际不工作（main.go 未调用 Watch）的根因。

### L5: 前端缺乏静态门禁
`package.json` 无 `lint`/`typecheck` 脚本，未安装 ESLint。AGENTS.md "Always Do" 规则要求的命令实际无法执行。文档与工具链脱节。

## 任务追踪

本次审计产出 12 个具体改进任务，按优先级分类（已在 TaskList 中追踪）：

### P0 — 立即修复（真实 bug，1-2 天）
- 修复 Pipeline.Execute 传 nil runner 导致端到端跑不通
- 修复 Pipeline 硬编码 buildCmd.Dir 为开发者本机路径
- 修复 ValidateAgent L3 默认股票池为美股（应为 A 股）
- 修复 research.go/generate.go 用 extractField 字符串扫描解析 JSON
- 修复 simulated_broker.go:151 硬编码 0.00025 与 fees 包不一致
- 修复 9 处 _ = json.Unmarshal 静默吞错（gene_pool/strategy/db）
- 修复 pkg/risk/take_profit.go 构造器 3 处 panic（违反生产代码不 panic 约定）
- 修复 e2e/tests 无 skip guard 导致 go test ./... 永远 FAIL
- 修复 16 个 ReviewActions.spec.ts 测试失败（缺 MessageProvider）
- 一次性 gofmt -w 全代码库（237 文件）

### P1 — 高优先级重构（1-2 sprint）
- 抽取 pkg/settlement + pkg/portfolio 共享原语包（消除 tracker/mock_trader 重复）
- 修复 strategy → ai / strategy → internal/sandbox 反向分层
- 抽取 BacktestRunner 接口到共享包（消除 3 处重复定义）
- 修复费率配置三重定义（让 backtest 引用 fees.AShareFees）
- 补 pkg/ai 顶层 5 子系统测试（client/cost/metrics/ratelimit/tracer）

### P2 — 中优先级改进（2-3 sprint）
- 拆分 pkg/backtest 上帝包为子包（12 职责 → 12 子包）
- 拆分 pkg/live 上帝包为子包（6 职责 → 子包）
- 拆分 cmd/analysis/main.go 372 行 main() 函数
- 拆分 registerRoutes 16 参数函数为 ServerDeps 结构体
- 补前端 ESLint + @vitest/coverage-v8 依赖 + lint/typecheck 脚本
- 决策 AI Research 模块去留（2500 行不可达代码）
- 决策 EmergencyFlatten.vue 去留（311 行死代码）

### P3 — 长期改进（按需）
- 扩展表达式引擎到信号/仓位/风控层 + ExpressionStrategy 适配器
- 建 pkg/tools/registry.go Tools Registry（AI 一等居民的最后一公里）
- 数据层"软分层"（新增 pkg/domain/market/ 子包，与旧类型并存）
- 修复文档漂移（ARCHITECTURE/VISION/SPEC 同步 ODR-021 服务合并）
- 标记 ADR-014 为 Superseded by ADR-020 §6

## Next Steps

1. 用户确认本 ODR 与任务列表后，按 P0 → P1 → P2 → P3 顺序推进
2. 每个 P0 任务完成后立即创建实施类 ODR（如 odr-044-p0-bug-fixes）
3. P1 共享原语包完成后，重新评估 brainstorming 中"统一执行"方向的剩余必要性
4. P3 表达式引擎扩展是 AI 一等居民的最高杠杆改动，建议作为下个里程碑的核心

---

_审计执行者: GLM-5 + 4 并行 Explore agent_
_审计耗时: 约 30 分钟（并行）_
_下次审计建议: P0+P1 完成后重测覆盖率与文档一致性_
