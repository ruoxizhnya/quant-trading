# 文档清理整合报告

> **执行日期**: 2026-04-09
> **执行人**: AI Assistant
> **范围**: 全部 `*.md` 文档 (排除 node\_modules/.git)

***

## 一、清理前后对比

| 指标             | 清理前                 | 清理后              | 变化                |
| -------------- | ------------------- | ---------------- | ----------------- |
| **文档总数**       | 26                  | **22**           | **-4**            |
| **总行数**        | 5,363               | **3,908**        | **-1,455 (-27%)** |
| **根目录文档**      | 3 (README + 2 AI生成) | **1** (仅 README) | -2                |
| **docs/ 目录**   | 16                  | **13**           | -3                |
| **memory/ 目录** | 1                   | **0 (已删除)**      | -1                |

***

## 二、删除的文件 (4 个)

### 2.1 `FRONTEND_REFACTOR_PLAN.md` (596 行) — ❌ 已删除

| 属性       | 值                                                        |
| -------- | -------------------------------------------------------- |
| **类型**   | AI 生成的重构计划                                               |
| **内容**   | BFF (Express) 架构方案 + Vue SPA 迁移计划                        |
| **删除原因** | 计划描述的 BFF 架构**未被采用**（实际使用 Vite proxy）；前端迁移**已完成**，此计划已过时 |
| **替代**   | [ARCHITECTURE.md](ARCHITECTURE.md) 新增"前端架构"章节记录实际架构      |

### 2.2 `FRONTEND_REVIEW.md` (197 行) — ❌ 已删除

| 属性       | 值                                                                              |
| -------- | ------------------------------------------------------------------------------ |
| **类型**   | AI 生成的审查报告                                                                     |
| **内容**   | Legacy HTML UI 的技术栈分析（纯 Vanilla HTML/JS, 无框架）+ 问题清单                            |
| **删除原因** | 审查对象（legacy HTML）**已被 Vue SPA 完全替代**；报告中指出的问题（API URL 不一致、无构建工具等）均已通过 Vue 重构解决 |
| **替代**   | [NEXT\_STEPS.md](NEXT_STEPS.md) 包含当前代码质量审计结果                                   |

### 2.3 `docs/PHASE2.5-REVIEW-PLAN.md` (678 行) — ❌ 已删除

| 属性       | 值                                                                                              |
| -------- | ---------------------------------------------------------------------------------------------- |
| **类型**   | 系统审查报告                                                                                         |
| **内容**   | 设计文档/代码一致性/测试/代码质量四维度审计                                                                        |
| **删除原因** | 与新创建的 [NEXT\_STEPS.md](NEXT_STEPS.md) **高度重复**（覆盖相同 4 个维度）；NEXT\_STEPS 更全面且包含可执行的 Action Items |
| **替代**   | [NEXT\_STEPS.md](NEXT_STEPS.md) — 合并了 PHASE2.5 的所有发现并增加了 Phase A\~E 开发计划                       |

### 2.4 `memory/2026-03-26.md` (148 行) + memory/ 目录 — ❌ 已删除

| 属性       | 值                                                                                             |
| -------- | --------------------------------------------------------------------------------------------- |
| **类型**   | 日常操作日志 / heartbeat                                                                            |
| **内容**   | Sprint 4/5 完成总结、测试状态、commit 链                                                                 |
| **删除原因** | **临时性日志**（非设计/规格文档）；信息已归档到 [phase-gate-reviews.md](phase-gate-reviews.md)；`memory/` 目录为空后一并移除 |

***

## 三、更新的文件 (3 个)

### 3.1 `README.md` (2 行 → 87 行) — ✅ 全面重写

| 变更项 | 原来     | 现在                         |
| --- | ------ | -------------------------- |
| 内容  | 仅项目名一行 | 完整的项目 README               |
| 包含  | 无      | 架构图、快速开始、功能表、文档索引、技术栈、目录结构 |
| 格式  | 纯文本    | Markdown 表格 + 代码块 + 链接     |

### 3.2 `docs/SPEC.md` (Line \~130) — ✅ Strategy 接口签名修正

| 变更项       | 原来                                   | 现在                                                      |
| --------- | ------------------------------------ | ------------------------------------------------------- |
| 方法名       | `Signals(ctx, universe, data, date)` | `GenerateSignals(ctx, bars, portfolio)`                 |
| 参数类型      | `[]Stock`, `MarketData`, `time.Time` | `map[string][]OHLCV`, `*Portfolio`                      |
| Weight 签名 | `Weight(signal Signal) float64`      | `Weight(signal Signal, portfolioValue float64) float64` |
| 新增        | —                                    | `Parameters() []Parameter` 方法                           |
| 标注        | 无                                    | 添加 "Canonical definition — matches strategy.go" 引用      |

**影响**: 消除了 SPEC.md 与 VISION.md、strategy.go 之间的三处不一致。

### 3.3 `docs/ARCHITECTURE.md` — ✅ 两处更新

#### 更新 A: 服务端口表 (Line \~72)

- 新增 "状态" 列
- 标注 risk-service(8083)、execution-service(8084) 为 🔲 规划中
- 所有运行中服务标注 ✅

#### 更新 B: 新增"前端架构 (Vue 3 SPA)"章节 (末尾追加 \~70 行)

- 技术栈表 (Vue 3 / TS / Vite / Naive UI / Chart.js / Pinia)
- 页面结构树 (`web/src/` 目录)
- 前后端通信图
- Legacy HTML deprecated 说明

***

## 四、整合后的文档目录结构

```
quant-trading/
├── README.md                          # ★ 项目入口 (重写: 2→87行)
│
├── docs/
│   ├── VISION.md                      # 设计愿景 + 7大原则 + 领域模型
│   ├── SPEC.md                        # 技术规格 (接口已修正 ✓)
│   ├── ARCHITECTURE.md                # 系统架构 (新增前端章节 ✓)
│   ├── ROADMAP.md                     # Sprint/Phase 规划
│   ├── TEST.md                        # 测试策略与规范
│   ├── CACHE.md                       # 缓存架构设计
│   ├── NEXT_STEPS.md                  # 审计报告 + 下一步计划
│   ├── PHASE3-PLAN.md                 # Phase 3 融合发展计划
│   ├── phase-gate-reviews.md          # Phase Gate 审查历史记录
│   ├── ADR.md                         # ADR 索引 (10条)
│   │
│   ├── adr/                           # 架构决策记录 (10个文件)
│   │   ├── adr-001-plugin-loading.md
│   │   ├── adr-002-timescaledb.md
│   │   ├── adr-003-background-worker.md
│   │   ├── adr-004-scoring-method.md
│   │   ├── adr-005-strategy-config.md
│   │   ├── adr-006-job-queue.md
│   │   ├── adr-007-ai-sandbox.md
│   │   ├── adr-008-inter-service-comm.md
│   │   ├── adr-009-speed-target-revision.md
│   │   └── adr-010-speed-architecture.md
│   │
│   └── test-cases/
│       └── T1_AND_ZHANGTING.md        # T+1 + 涨跌停 测试用例规范
```

### 文档分类与职责

| 分类       | 文件                                           | 职责                | 目标读者   |
| -------- | -------------------------------------------- | ----------------- | ------ |
| **入口**   | README.md                                    | 项目概览、快速开始、索引      | 所有人    |
| **设计核心** | VISION, SPEC, ARCHITECTURE                   | 原则、接口、架构          | 开发者    |
| **规划**   | ROADMAP, PHASE3-PLAN, NEXT\_STEPS            | 进度、里程碑、待办         | PM/开发者 |
| **质量**   | TEST, phase-gate-reviews, T1\_AND\_ZHANGTING | 测试策略、Gate 审查、用例规范 | QA/开发者 |
| **决策**   | ADR.md + adr/\*                              | 关键决策记录            | 架构师    |
| **专题**   | CACHE.md                                     | 缓存架构深度说明          | 后端开发者  |

***

## 五、命名规范确认

| 规则             | 应用情况                                 |
| -------------- | ------------------------------------ |
| 文件名大写          | ✅ `VISION.md`, `SPEC.md`, `ADR.md` 等 |
| 子目录小写          | ✅ `adr/`, `test-cases/`              |
| kebab-case ADR | ✅ `adr-001-*.md`                     |
| 根目录仅 README    | ✅ 已清理其他根级 md                         |
| 无中文文件名         | ✅ 全部英文命名                             |

***

## 六、遗留问题 (待后续处理)

| ID   | 问题                           | 建议                             | 优先级    |
| ---- | ---------------------------- | ------------------------------ | ------ |
| D-01 | ROADMAP.md Phase 状态标记仍需人工校准  | 对比 NEXT\_STEPS.md 发现逐一修正       | Medium |
| D-02 | phase-gate-reviews.md 可精简为摘要 | Sprint 1/2 详情已过时，保留结论即可        | Low    |
| D-03 | PHASE3-PLAN.md 部分决策需验证是否仍有效  | Event-Driven Pipeline 是否仍是目标？  | Medium |
| D-04 | ADR-011 (前端架构决策) 待撰写         | Vue SPA vs legacy HTML 的正式 ADR | High   |

***

_报告完成。文档从 26 个精简至 22 个，减少 27% 冗余内容，结构更清晰，职责更明确。_
