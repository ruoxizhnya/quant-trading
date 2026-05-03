# AGENTS.md — AI Agent 通用模板

> **版本**: v2.0
> **创建日期**: 2026-04-11
> **适用场景**: 任何需要 AI 编码助手协同开发的项目
>
> 本文件为 AI 编码助手提供项目上下文。阅读本文件即可快速理解项目全貌。
>
> _Last updated: YYYY-MM-DD_

---

## 1. 项目概述

**[ProjectName]** 是一个 [一句话描述项目]。

- **语言**: [主要语言] [版本要求]
- **当前版本**: v[x.y.z]
- **状态**: [alpha/beta/stable] — [当前阶段描述]
- **入口**: [程序入口文件]
- **构建**: [构建命令]

### 技术栈

| 组件 | 技术 | 版本/端口 |
|------|------|----------|
| 后端 | [语言/框架] | :[端口] |
| 前端 | [框架] | :[端口] |
| 数据库 | [数据库] | :[端口] |
| 缓存 | [缓存] | :[端口] |

---

## 2. 架构概览

[架构图或文字描述核心模块关系]

```
[ASCII 架构图]
```

### 关键架构决策

| ADR | 决策 | 核心理由 |
|-----|------|----------|
| [ADR-xxx] | [简述] | [一句话理由] |

---

## 3. 目录结构

```
project-root/
├── cmd/                 # 入口点
├── internal/            # 私有代码（不对外暴露）
│   ├── core/           # 核心业务逻辑
│   ├── infrastructure/ # 基础设施层
│   └── api/            # API 处理
├── pkg/                # 可复用库
├── web/                # 前端代码
├── docs/               # 文档
├── migrations/         # 数据库迁移
└── tests/              # 测试
```

---

## 4. 角色与边界

### 你是谁
你是 [角色定位]，负责 [职责范围]。

### 工作范围（What You Work On）
- **后端**: `cmd/`, `internal/`, `pkg/` ([具体模块])
- **前端**: `web/src/` ([框架] + [状态管理])
- **测试**: `tests/` 或 `*_test.go`
- **文档**: `docs/`

### 禁区（What You NEVER Modify）
- `node_modules/`, `dist/`, `.vite/` — 自动生成，勿改
- `.env*` 文件 — 含密钥
- `migrations/` 已有 SQL — 只新增，不修改
- 二进制文件、编译产物、vendor 目录

---

## 5. 命令参考

### 后端
```bash
[构建命令]              # 构建
[测试命令]              # 运行测试
[静态分析命令]          # 代码检查
```

### 前端
```bash
npm install             # 安装依赖
npm run dev             # 启动开发服务器
npm run build           # 生产构建
npm run lint            # 代码检查
npm run typecheck       # 类型检查
npm test                # 单元测试
```

### E2E 测试
```bash
npx playwright test     # 全量 E2E
npx playwright test --grep "[关键词]"  # 按名称筛选
```

### 基础设施
```bash
docker compose up -d    # 启动服务
docker compose ps       # 检查状态
docker compose logs -f [service]  # 查看日志
```

---

## 6. 代码规范

### 后端（[语言名]）

- [命名规则]
- [错误处理方式]
- [日志库选择]
- [关键接口约束]

**核心接口示例**：
```[语言]
type [Interface] interface {
    [Method1]() [ReturnType]
    [Method2](ctx context.Context, ...) error
}
```

### 前端（[框架]）

- [API 风格]（Composition API / Options API）
- [UI 库]
- [状态管理方案]
- [组件命名规则]

**关键模式**：
```typescript
// [模式说明]
const [state] = [ref/shallowRef]<[Type]>($initialValue)
```

---

## 7. 数据流

```
[用户端]
  │
  ├──► [请求路径 1] ──► [服务 A]
  ├──► [请求路径 2] ──► [服务 B]
  │
  └──► [缓存] ◄──── [数据源]
              │
              └──► [持久化存储]
```

---

## 8. 工作流规范

### 工作类型分类

| 类型 | 判定关键词 | 示例 |
|------|-----------|------|
| 设计 | 设计/架构/方案/选型/ADR | 新模块设计 |
| 审计 | 审计/审查/Review/检查 | 代码审查 |
| 测试 | 测试/Test/覆盖 | 写单元测试 |
| 实现 | 实现/开发/编写/修复/重构 | 修复 Bug |
| 文档 | 文档/README/API文档/指南 | 更新 SPEC |
| 其他 | 配置/问答/调研 | 环境配置 |

### 前置动作（开始前必做）

| 任务类型 | 必做事项 |
|---------|---------|
| 设计 | 研究现有架构、查阅 ADR、横向比较方案 |
| 审计 | 通读相关文档、审查变更历史 |
| 测试 | 理解需求、分析源码、识别边界条件 |
| 实现 | 理解设计文档、熟悉编码规范、检查依赖 |
| 文档 | 对照源码验证、检查格式规范 |
| 其他 | 无特定要求 |

### 后置动作（完成后必做）

| 任务类型 | 必做事项 |
|---------|---------|
| 设计 | 更新文档、编写 ADR、记录待办 |
| 审计 | 输出发现、更新任务列表 |
| 测试 | 确保通过、审计测试质量、更新覆盖率 |
| 实现 | 运行 lint/typecheck、编写测试、确保构建通过 |
| 文档 | 验证一致性、检查交叉引用、更新导航 |
| 其他 | 无特定要求 |

---

## 9. 行为边界

### Always Do（必须做）
- [ ] [规则 1]
- [ ] [规则 2]
- [ ] 修改代码前运行 [检查命令]

### Ask First（先问再做）
- [ ] 修改数据库 schema
- [ ] 添加新依赖
- [ ] 修改核心接口
- [ ] 不确定的设计决策

### Never Do（绝对不做）
- ❌ 硬编码密钥/密码
- ❌ 直接提交到 main 分支
- ❌ 修改自动生成文件
- ❌ 使用 `any` 类型（无明确注释）
- ❌ 生成无法解释的代码

---

## 10. 文档维护

### 文档分类

| 类型 | 目录 | 职责 |
|------|------|------|
| 设计文档 | `docs/design/` | 解释系统原理 |
| 决策文档 | `docs/decisions/` | 记录架构决策 (ADR) |
| 运营决策 | `docs/odr/` | 记录运营决策 (ODR) |
| 任务文档 | `docs/TASKS.md` | 统一追踪可执行任务 |
| 参考文档 | `docs/` | 持续维护的状态/进度 |
| 归档文档 | `docs/archive/` | 过时但保留的历史文档 |

### 文档生命周期
```
Active → Stale → Archived → Purged
```
- **Active**: 当前准确，被引用
- **Stale**: 过时但有价值 → 加 ⚠️ 标记
- **Archived**: 移至 `docs/archive/`
- **Purged**: 删除（需 12+ 个月且经批准）

### 更新触发器（Rule 1: Update-on-Change）

| 代码变更 | 必须更新的文档 | 更新内容 |
|---------|--------------|---------|
| 新增/修改 API 端点 | SPEC.md | API section: endpoint, method, request/response |
| 新增/修改数据库表 | ARCHITECTURE.md | DB schema: table, columns, indexes |
| 新增/修改服务端口 | ARCHITECTURE.md + Data Flow | Service topology |
| 修改核心接口 | VISION.md + SPEC.md + AGENTS.md | Interface signature (all 3 must match) |
| 新增页面/路由 | ARCHITECTURE.md (frontend) | Page structure tree |
| 新增依赖 | Commands section (if needed) | Build/run commands |
| 修复已知问题 | Known Issues table | Remove fixed entry |
| 发现新问题 | Known Issues table | Add new entry with workaround |
| 变更 docker-compose | ARCHITECTURE.md + Data Flow | Service list and ports |

### ODR 创建触发器（Rule 2）

| 操作 | ODR 类别 | 创建时机 |
|------|---------|---------|
| 删除/归档文档 | Cleanup | 操作后立即 |
| 审计/审查工作 | Audit | 完成后 48h 内 |
| 文档架构迁移 | Migration | 完成后 72h 内 |
| 变更工具/流程 | Tooling/Process | 上线前 |

**ODR 模板**：
```markdown
# ODR-[next-number]: [Short Title]

> **Status**: [Proposed/Accepted/Completed/Deprecated]
> **Date**: [YYYY-MM-DD]
> **Category**: [Cleanup/Audit/Migration/Tooling/Process]
> **Related ADRs**: [adr-xxx] (if any)
> **Supersedes**: [odr-yyy] (if replacing)

## Context
[Why was this needed? What problem triggered it?]

## Decision
[What was decided? Why this approach?]

## Consequences
[Positive and negative impacts]

## Artifacts
[Files created/modified/deleted]

## Metrics
[Quantifiable results]

## Lessons Learned
[What would you do differently?]
```

### 会话结束检查清单（Rule 6）

在每次会话结束前（特别是建议 commit 前），运行此检查：

- [ ] 是否修改了接口？→ 更新 SPEC.md / VISION.md / AGENTS.md
- [ ] 是否添加/删除了文件？→ 更新 ARCHITECTURE.md 目录树（如果是结构性变更）
- [ ] 是否修复了已知问题？→ 从 Known Issues 表中移除
- [ ] 是否发现了新问题？→ 添加到 Known Issues 表
- [ ] 是否归档/删除了文档？→ 创建 ODR + 更新 ADR.md 索引
- [ ] 是否改变了项目约定？→ 更新 Code Style / Boundaries
- [ ] 是否添加了新依赖？→ 检查 Commands 部分是否需要更新

---

## 11. 文档导航

### 理解设计（Explanation）

| 文档 | 用途 | 何时阅读 |
|------|------|---------|
| [VISION.md] | 设计原则、领域模型 | 开始新功能时 |
| [SPEC.md] | 技术规格、API 定义 | 实现功能时 |
| [ARCHITECTURE.md] | 服务拓扑、DB schema | 调试问题时 |

### 查找状态（Reference）

| 文档 | 内容 | 更新频率 |
|------|------|---------|
| [ROADMAP.md] | 进度里程碑 | Sprint 结束时 |
| [NEXT_STEPS.md] | TODO 和行动项 | 代码审查后 |
| [ADR.md] + decisions/ | 架构决策索引 | 新增决策时 |
| [ODR 索引] | 运营决策索引 | 新增运营决策时 |

### 执行任务（How-to）

| 文档 | 用途 |
|------|------|
| [TEST.md] | 测试策略和覆盖率目标 |
| [CONTRIBUTING.md] | 贡献指南 |

### 历史归档（Archive）

| 文档 | 替代文档 | 归档日期 |
|------|---------|---------|
| [旧报告.md] | → [新文档.md] | YYYY-MM |

---

## 12. 会话管理

### 启动新会话
创建或更新 `.session/task-current.md`：
```markdown
# Active Task: [简要标题]

## Objective
[要完成什么]

## Progress
- [ ] 步骤 1
- [ ] 步骤 2

## Notes
[阻塞项、发现、决策]
```

### 重置上下文（Standup 格式）
当对话漂移或上下文丢失时，使用此格式重置：
```
@AGENTS.md

## Standup
Since last session:
- Completed: [完成的项]
- In Progress: [进行中的项]
- Blocked: [阻塞？]
- Next: [下一步]

Context from previous session: [关键决策摘要]
Please continue from where we left off.
```

---

## 13. 当前状态

### 健康
- [✅/❌] 测试: [x/y] 通过
- [✅/❌] 构建: [通过/失败]
- [✅/❌] Lint: [通过/失败]

### 待办（按优先级）
1. **P0** — [紧急任务]
2. **P1** — [重要任务]
3. **P2** — [改进任务]

### 技术债
- [x/y] 项已完成
- 剩余: [描述]

---

## 14. 已知问题与变通方案

| 问题 | 变通方案 |
|------|---------|
| [问题描述] | [如何绕过] |

---

## 附录：快速启动清单

新会话开始时，按顺序确认：
- [ ] 阅读「项目概述」→ 理解技术栈
- [ ] 查看「当前状态」→ 了解健康状况
- [ ] 检查「已知问题」→ 避免踩坑
- [ ] 确认「工作范围」→ 明确边界
- [ ] 如继续旧任务 → 查看 `.session/task-current.md`

---

## 使用指南

### 按项目规模裁剪

| 项目规模 | 推荐章节 |
|---------|---------|
| 小型项目（<10 文件） | 1-5, 9, 14 |
| 中型项目（10-100 文件） | 1-9, 11, 13, 14 |
| 大型项目（100+ 文件） | 全部 14 章 |

### 动态维护建议

- **每次会话结束前**：检查第 13 章（当前状态）是否需要更新
- **每次代码变更后**：触发第 10 章（文档维护）的对应规则
- **每周审查**：第 14 章（已知问题）是否有过时条目

### 避免常见陷阱

| 陷阱 | 解决方案 |
|------|---------|
| 文件过长（>600 行） | 将详细内容拆分到子文档，AGENTS 只保留索引 |
| 规则过时 | 在第 14 章添加"上次审查日期"，定期刷新 |
| 规则冲突 | 第 9 章（行为边界）的优先级最高 |

---
_基于 quant-trading AGENTS.md + Claudeer AGENTS.md 对比分析总结_
_模板版本: v2.0 | 最后更新: 2026-04-11_
