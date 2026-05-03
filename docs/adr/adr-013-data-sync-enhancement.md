# ADR-013: Data Synchronization Enhancement

**Date:** 2026-05-03
**Status:** Proposed
**Category:** Architecture
**Related ADRs:** ADR-003 (Background Worker), ADR-006 (Job Queue)
**Supersedes:** N/A
**Author:** Quant Lab Architecture Team

---

## Context

当前 Quant Lab 的数据同步实现（详见 [cmd/data/main.go](../../cmd/data/main.go)）存在以下结构性问题：

1. **全手动触发**: 所有 13 个 `/sync/*` 端点均为手动 POST 调用，无定时自动同步机制
2. **无统一任务管理**: 同步操作直接调用 Tushare API，缺乏任务队列、状态追踪和进度反馈
3. **前端无数据管理界面**: 用户无法直观查看数据覆盖度、同步状态或历史记录
4. **错误恢复能力弱**: 失败任务仅记录 Warn 日志，无重试机制和失败隔离
5. **EventBus 未充分利用**: [pkg/marketdata/eventbus.go](../../pkg/marketdata/eventbus.go) 已实现但未在同步流程中实际使用

这些问题在 Phase 3 质量优化阶段成为主要瓶颈，特别是在以下场景：
- 研究员每日开盘前需要确认数据已更新至最新交易日
- 批量回测前需要预热缓存，但无法确认数据完整性
- Tushare 速率限制导致部分股票同步失败，需人工排查

## Decision

### 决策概述

**实施数据同步增强方案**，包含三个核心组件：

1. **统一同步任务队列 (Sync Job Queue)** — 将现有直接同步调用改造为异步任务模式
2. **定时同步调度器 (Scheduled Sync Scheduler)** — 基于 cron 表达式的自动同步编排
3. **数据同步管理页面 (Data Sync UI)** — 提供可视化同步控制中心

### 架构变更

```
Before (Current):
┌─────────────┐     POST /sync/ohlcv      ┌──────────────┐
│   Browser   │ ────────────────────────► │  Data Service│
│             │                           │  (direct call)│
└─────────────┘                           └──────────────┘
                                                 │
                                                 ▼
                                          ┌──────────────┐
                                          │   Tushare    │
                                          │     API      │
                                          └──────────────┘

After (Proposed):
┌─────────────┐     POST /api/sync/jobs   ┌──────────────┐
│  Data Sync  │ ────────────────────────► │  Data Service│
│    UI       │  WS /ws/sync (progress)   │              │
│             │ ◄──────────────────────── │              │
└─────────────┘                           └──────────────┘
                                                 │
                    ┌────────────────────────────┘
                    ▼
            ┌──────────────┐
            │  Sync Job    │  ◄── Redis Streams (Job Queue)
            │   Queue      │
            └──────────────┘
                    │
        ┌───────────┼───────────┐
        ▼           ▼           ▼
   ┌─────────┐ ┌─────────┐ ┌─────────┐
   │ Worker  │ │ Worker  │ │ Worker  │  (goroutine pool)
   │  #1     │ │  #2     │ │  #N     │
   └────┬────┘ └────┬────┘ └────┬────┘
        │           │           │
        └───────────┼───────────┘
                    ▼
            ┌──────────────┐
            │   Tushare    │
            │     API      │
            └──────────────┘
                    │
                    ▼
            ┌──────────────┐
            │  PostgreSQL  │
            │    + Redis   │
            └──────────────┘
```

### 技术选型

| 组件 | 选型 | 理由 |
|------|------|------|
| **任务队列** | PostgreSQL `sync_jobs` 表 + 应用层轮询 | 避免引入新依赖；利用现有 `backtest_runs` 模式；适合中低并发 |
| **定时调度** | `github.com/robfig/cron/v3` | Go 生态标准 cron 库；支持秒级精度；与现有 Go 后端一致 |
| **进度推送** | Server-Sent Events (SSE) | 比 WebSocket 更轻量；单向推送足够；自动重连支持 |
| **前端状态** | Pinia + shallowRef | 与现有前端架构一致；大对象避免响应式开销 |

**未选方案及理由**:
- **Redis Streams**: 虽然 ADR-006 推荐，但当前同步任务量不足以需要独立队列中间件
- **RabbitMQ/NATS**: 增加运维复杂度，与 Phase 3 "简化架构" 目标冲突
- **WebSocket**: SSE 在单向进度推送场景下更简单，减少连接管理开销

## Consequences

### Positive

1. **自动化**: 定时同步减少 90% 以上的人工操作
2. **可观测性**: 统一任务状态追踪，支持进度实时反馈
3. **容错性**: 失败任务自动重试 + 人工重试入口，减少数据缺失
4. **扩展性**: 任务队列模式为未来多数据源扩展（如 Wind、JQData）预留接口
5. **用户体验**: 可视化界面降低数据管理门槛

### Negative

1. **复杂度增加**: 引入 cron 调度器和任务队列，增加系统复杂度
2. **数据库压力**: `sync_jobs` 表写入增加，需考虑归档策略（保留 30 天）
3. **前端工作量**: 新增 1 个页面 + 5 个组件 + 1 个 Store
4. **迁移成本**: 现有 `/sync/*` 端点需改造为任务创建模式，保持向后兼容

### Neutral

- 速率限制逻辑保持不变（Tushare 200 req/min）
- 缓存策略保持不变（Cache-Aside）
- 数据实体和字段保持不变

## Implementation Plan

### Phase 1: 后端任务队列 (Week 1)

1. 创建 `sync_jobs` 表（迁移脚本）
2. 实现 `pkg/sync/job.go` — 任务定义和状态机
3. 实现 `pkg/sync/queue.go` — 队列管理（基于 PostgreSQL）
4. 实现 `pkg/sync/worker.go` — Worker goroutine pool
5. 改造现有 `/sync/*` 端点为任务创建模式
6. 新增 `/api/sync/*` REST API

### Phase 2: 定时调度器 (Week 2)

1. 集成 `robfig/cron/v3`
2. 实现 `pkg/sync/scheduler.go`
3. 配置持久化（数据库或配置文件）
4. 调度器与任务队列集成

### Phase 3: 前端 UI (Week 2-3)

1. 创建 `pages/DataSync.vue`
2. 创建 `components/sync/*` 子组件
3. 实现 `stores/sync.ts` Pinia Store
4. 实现 `api/sync.ts` API 客户端
5. 集成 SSE 进度推送

### Phase 4: 集成测试 (Week 3)

1. E2E 测试：完整同步流程
2. 性能测试：批量同步 5000+ 股票
3. 故障注入测试：网络中断、Tushare 限流

## Artifacts

| 文件 | 类型 | 说明 |
|------|------|------|
| `docs/adr/adr-013-data-sync-enhancement.md` | ADR | 本文档 |
| `docs/design/pages/data-sync.md` | Design Spec | UI 页面设计规范 |
| `migrations/0xx_add_sync_jobs_table.sql` | Migration | 同步任务表 |
| `pkg/sync/job.go` | Go | 任务模型和状态机 |
| `pkg/sync/queue.go` | Go | 队列管理 |
| `pkg/sync/worker.go` | Go | Worker 实现 |
| `pkg/sync/scheduler.go` | Go | 定时调度器 |
| `cmd/data/sync_handlers.go` | Go | HTTP Handler (从 main.go 拆分) |
| `web/src/pages/DataSync.vue` | Vue | 数据同步页面 |
| `web/src/components/sync/*.vue` | Vue | 子组件 |
| `web/src/stores/sync.ts` | TypeScript | Pinia Store |
| `web/src/api/sync.ts` | TypeScript | API 客户端 |

## Metrics

| 指标 | 目标 | 测量方式 |
|------|------|----------|
| 数据新鲜度 | T-1 交易日数据 09:30 前可用 | 定时任务执行时间监控 |
| 同步成功率 | > 99% | `sync_jobs` 状态统计 |
| 失败恢复时间 | < 5 分钟 | 重试机制 + 告警响应 |
| 用户操作次数 | 每日手动同步 < 3 次 | 操作日志统计 |

## Risks & Mitigations

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| cron 调度器单点故障 | 高 | 调度器无状态，可水平扩展；使用分布式锁防止重复执行 |
| `sync_jobs` 表数据膨胀 | 中 | 自动归档策略：保留 30 天活跃记录，历史记录压缩存储 |
| Tushare 限流导致队列堆积 | 中 | Worker 速率限制 + 队列长度告警 + 动态并发控制 |
| SSE 连接泄漏 | 低 | 页面卸载时主动关闭连接；服务端设置超时 |

---

_文档版本: 1.0_
_创建日期: 2026-05-03_
_状态: Proposed (Pending Review)_
