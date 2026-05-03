# Data Sync 数据同步管理页面设计文档

> **页面路径**: `/data-sync`  
> **组件文件**: `pages/DataSync.vue`  
> **状态**: Design Spec (Pending Implementation)  
> **依赖**: ADR-013 (Data Sync Enhancement), TASKS.md Phase 3 D7

---

## 1. 概述

### 1.1 目标

Data Sync 页面是 Quant Lab 的数据管理中枢，提供：
- **一站式数据同步控制** — 统一管理所有数据实体的同步操作
- **同步状态可视化** — 实时展示各数据源的同步进度、健康状态和覆盖度
- **定时任务编排** — 配置自动同步策略，减少人工干预
- **数据质量监控** — 展示数据完整性、时效性和一致性指标

### 1.2 用户故事

- 作为**量化研究员**，我想查看当前 OHLCV 数据覆盖到哪个日期，以便确认是否可以运行回测
- 作为**系统管理员**，我想配置每日自动同步任务，确保数据始终保持最新
- 作为**数据工程师**，我想查看同步失败日志并重试失败的任务，以便排查数据问题
- 作为**交易员**，我想快速触发一次指数成分股同步，以便使用最新股票池运行策略

### 1.3 设计原则

| 原则 | 说明 |
|------|------|
| **操作可见** | 所有同步操作的状态变化实时反馈，用户始终知道系统在做什么 |
| **批量优先** | 支持一键全量同步，减少重复点击 |
| **容错友好** | 失败任务可单独重试，不影响其他数据类型 |
| **历史可追溯** | 同步记录保留最近 30 天，支持按时间/类型筛选 |

---

## 2. 信息架构

### 2.1 页面结构

```
Data Sync 页面
├── 页面标题区
│   └── "数据同步" + 副标题 + 全局操作按钮
├── 数据概览卡片区 (SyncOverviewCards)
│   ├── 股票总数
│   ├── OHLCV 最新日期
│   ├── 财务数据最新日期
│   └── 系统健康状态
├── 同步控制区 (SyncControlPanel) [核心区域]
│   ├── 数据类型标签页
│   │   ├── 📊 行情数据 (OHLCV)
│   │   ├── 📈 财务数据 (Fundamentals)
│   │   ├── 🏢 股票信息 (Stocks)
│   │   ├── 📅 交易日历 (Calendar)
│   │   ├── 🧮 因子数据 (Factors)
│   │   └── 📋 其他 (Dividends/Splits/Index)
│   └── 每个标签内容
│       ├── 数据覆盖度指标
│       ├── 同步操作按钮组
│       ├── 定时任务开关
│       └── 最近同步记录
├── 同步任务队列区 (SyncJobQueue)
│   ├── 进行中任务列表
│   ├── 等待中任务列表
│   └── 已完成/失败任务列表
├── 同步日志区 (SyncLogViewer)
│   ├── 日志级别筛选 (Info/Warn/Error)
│   ├── 时间范围筛选
│   └── 实时日志流
└── 数据质量仪表盘 (DataQualityDashboard)
    ├── 数据完整性评分
    ├── 时效性热力图
    └── 异常数据告警
```

### 2.2 数据流

```
用户进入 Data Sync 页面
    ↓
加载数据概览 (GET /api/stocks/count, GET /market/index)
    ↓
加载各数据类型状态 (GET /api/sync/status)
    ↓
展示概览卡片 + 同步控制面板
    ↓
用户触发同步操作 (POST /sync/*)
    ↓
创建同步任务 → WebSocket 推送进度 → 更新 UI
    ↓
任务完成 → 更新数据概览 → 记录同步历史
```

---

## 3. 布局设计

### 3.1 整体布局

```
┌─────────────────────────────────────────────────────────────┐
│  数据同步管理                                                │
│  统一控制中心 · 定时任务 · 质量监控                            │
├─────────────────────────────────────────────────────────────┤
│  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐               │
│  │ 5491   │ │ 2026-  │ │ 2026-  │ │ ✅ 正常 │               │
│  │ 只股票 │ │ 04-30  │ │ 04-30  │ │ 系统   │               │
│  │        │ │ OHLCV  │ │ 财务   │ │ 状态   │               │
│  └────────┘ └────────┘ └────────┘ └────────┘               │
├─────────────────────────────────────────────────────────────┤
│  [📊行情] [📈财务] [🏢股票] [📅日历] [🧮因子] [📋其他]        │
├─────────────────────────────────────────────────────────────┤
│  数据覆盖度                                                  │
│  ┌─────────────────────────────────────────┐                │
│  │ ████████████████████████████░░░░  87%   │                │
│  │ 1527万条 / 1750万条 (目标)               │                │
│  └─────────────────────────────────────────┘                │
│                                                              │
│  同步操作                              定时同步: [开关]      │
│  ┌─────────────────────────────────────────┐                │
│  │ [🔄 增量同步] [🔄 全量同步] [⏹️ 取消]    │                │
│  │ 日期范围: [2024-01-01] ~ [2026-04-30]   │                │
│  │ 股票池: [全部 ▼] [沪深300 ▼] [自定义...] │                │
│  └─────────────────────────────────────────┘                │
│                                                              │
│  最近同步记录                                                 │
│  ┌─────────────────────────────────────────┐                │
│  │ 时间           │ 类型    │ 数量   │ 状态 │                │
│  │ 2026-04-30 09:00 │ 增量   │ 5,231 │ ✅   │                │
│  │ 2026-04-29 09:00 │ 增量   │ 4,892 │ ✅   │                │
│  │ 2026-04-28 09:00 │ 全量   │ 1.2M  │ ⚠️   │                │
│  └─────────────────────────────────────────┘                │
├─────────────────────────────────────────────────────────────┤
│  同步任务队列                                                 │
│  ┌─────────────────────────────────────────┐                │
│  │ 🟡 运行中 │ 沪深300 OHLCV 同步 │ 67%    │                │
│  │ ⏳ 等待中 │ 中证500 OHLCV 同步 │ --     │                │
│  │ ✅ 已完成 │ 股票列表同步       │ 5491条 │                │
│  └─────────────────────────────────────────┘                │
├─────────────────────────────────────────────────────────────┤
│  实时日志                                                     │
│  ┌─────────────────────────────────────────┐                │
│  │ [INFO]  09:00:01 开始同步 000001.SZ     │                │
│  │ [INFO]  09:00:02 同步完成 000001.SZ     │                │
│  │ [WARN]  09:00:03 000002.SZ 无数据，跳过 │                │
│  └─────────────────────────────────────────┘                │
└─────────────────────────────────────────────────────────────┘
```

### 3.2 响应式断点

| 断点 | 布局调整 |
|------|----------|
| **Desktop (>1200px)** | 双栏布局：左侧同步控制(65%) + 右侧任务队列与日志(35%) |
| **Tablet (768-1200px)** | 单栏布局，标签页横向滚动，概览卡片 2×2 网格 |
| **Mobile (<768px)** | 单栏布局，标签页变为下拉选择器，操作按钮垂直堆叠 |

---

## 4. 组件规格

### 4.1 SyncOverviewCards (数据概览卡片)

**文件**: `components/sync/SyncOverviewCards.vue`

**Props**:
| 属性 | 类型 | 说明 |
|------|------|------|
| `stockCount` | `number` | 股票总数 |
| `ohlcvLatestDate` | `string` | OHLCV 最新日期 |
| `fundamentalLatestDate` | `string` | 财务数据最新日期 |
| `systemHealth` | `'healthy' \| 'degraded' \| 'unhealthy'` | 系统健康状态 |
| `loading` | `boolean` | 加载状态 |

**布局**: 4 列网格 → 2 列(平板) → 1 列(手机)

**样式**:
- 卡片背景: `var(--q-surface)`
- 数字字体: `JetBrains Mono`, 24px, `var(--q-text)`
- 标签字体: 12px, `var(--q-text2)`
- 健康状态指示: 绿色圆点(正常) / 黄色(降级) / 红色(异常)

---

### 4.2 SyncControlPanel (同步控制面板)

**文件**: `components/sync/SyncControlPanel.vue`

**Props**:
| 属性 | 类型 | 说明 |
|------|------|------|
| `activeTab` | `SyncDataType` | 当前选中的数据类型 |
| `syncStatus` | `Record<SyncDataType, DataTypeStatus>` | 各类型同步状态 |
| `onSync` | `(type, options) => void` | 同步触发回调 |
| `onCancel` | `(jobId) => void` | 取消任务回调 |

**数据类型标签 (SyncDataType)**:
```typescript
type SyncDataType = 
  | 'ohlcv'      // 行情数据
  | 'fundamental' // 财务数据
  | 'stocks'     // 股票信息
  | 'calendar'   // 交易日历
  | 'factors'    // 因子数据
  | 'others'     // 分红/拆股/指数成分股
```

**每个标签页内容结构**:
1. **覆盖度指标**: 进度条 + 数字指标
2. **操作区**: 
   - 同步模式: 增量 / 全量
   - 日期范围选择器 (n-date-picker range)
   - 股票池选择: 全部 / 沪深300 / 中证500 / 自定义输入
   - 操作按钮: [增量同步] [全量同步] [取消]
3. **定时同步开关**: 
   - 启用/禁用定时同步
   - Cron 表达式输入 (如 `0 9 * * *`)
   - 下次执行时间预览
4. **最近记录表格**: 时间 / 类型 / 数量 / 状态 / 操作(重试)

---

### 4.3 SyncJobQueue (同步任务队列)

**文件**: `components/sync/SyncJobQueue.vue`

**Props**:
| 属性 | 类型 | 说明 |
|------|------|------|
| `jobs` | `SyncJob[]` | 任务列表 |
| `onCancel` | `(jobId) => void` | 取消任务 |
| `onRetry` | `(jobId) => void` | 重试任务 |

**任务状态 (SyncJobStatus)**:
```typescript
type SyncJobStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

interface SyncJob {
  id: string
  type: SyncDataType
  status: SyncJobStatus
  progress: number        // 0-100
  totalItems: number
  processedItems: number
  startTime: string
  endTime?: string
  errorMessage?: string
}
```

**布局**: 
- 运行中任务: 顶部固定，带实时进度条
- 等待中任务: 可拖拽排序
- 已完成/失败: 可折叠的历史列表

---

### 4.4 SyncLogViewer (同步日志查看器)

**文件**: `components/sync/SyncLogViewer.vue`

**Props**:
| 属性 | 类型 | 说明 |
|------|------|------|
| `logs` | `SyncLogEntry[]` | 日志条目 |
| `maxLines` | `number` | 最大显示行数 (默认 100) |

**日志条目结构**:
```typescript
interface SyncLogEntry {
  timestamp: string
  level: 'info' | 'warn' | 'error' | 'debug'
  message: string
  symbol?: string
  jobId?: string
}
```

**交互**:
- 自动滚动到底部 (跟随最新日志)
- 点击暂停自动滚动
- 日志级别筛选开关
- 按 symbol / jobId 过滤

---

### 4.5 DataQualityDashboard (数据质量仪表盘)

**文件**: `components/sync/DataQualityDashboard.vue`

**功能**:
- **完整性评分**: 各数据类型的字段填充率
- **时效性热力图**: 日历热力图展示每日数据更新状态
- **异常告警列表**: 数据不一致、缺失、延迟的告警

---

## 5. 交互设计

### 5.1 同步操作流程

```
用户选择数据类型标签 (如 "行情数据")
    ↓
选择同步参数:
  - 日期范围 (默认: 最近一年)
  - 股票池 (默认: 全部)
  - 同步模式 (增量/全量)
    ↓
点击 [增量同步] 或 [全量同步]
    ↓
前端验证参数 → 显示确认对话框 (全量同步时)
    ↓
发送 POST /sync/ohlcv (或 /sync/ohlcv/all)
    ↓
创建任务 → 加入任务队列 → 开始轮询状态
    ↓
WebSocket 推送进度 (或轮询 GET /api/sync/jobs/:id)
    ↓
更新进度条 → 刷新概览卡片 → 追加日志
    ↓
任务完成:
  - 成功: 绿色提示 + 更新最新日期 + 刷新记录表格
  - 失败: 红色提示 + 显示错误信息 + 提供 [重试] 按钮
```

### 5.2 状态转换图

```
Idle → Validating → Submitting → Queued → Running → Completed
  │        │           │           │         │          │
  │        │           │           │         │          └──→ 刷新数据概览
  │        │           │           │         │
  │        │           │           │         └──→ 进度更新 (WebSocket/轮询)
  │        │           │           │
  │        │           │           └──→ 等待资源 / 排队中
  │        │           │
  │        │           └──→ 参数错误 → 显示表单验证错误
  │        │
  │        └──→ 全量同步确认对话框
  │
  └──→ 取消 → Cancelled
```

### 5.3 错误处理流程

| 错误场景 | 前端反馈 | 用户操作 |
|----------|----------|----------|
| 网络错误 | 红色 message + [重试] 按钮 | 点击重试或刷新页面 |
| Tushare 速率限制 | 黄色警告 + 自动重试倒计时 | 等待或降低并发 |
| 部分股票同步失败 | 任务状态为 "部分成功" + 失败列表 | 单独重试失败项 |
| 数据库连接失败 | 红色全局警告横幅 | 联系管理员 |
| 无效日期范围 | 表单字段级红色提示 | 修正日期范围 |

### 5.4 键盘快捷键

| 快捷键 | 功能 |
|--------|------|
| `Ctrl/Cmd + Enter` | 触发当前标签页的同步操作 |
| `Esc` | 取消当前进行中的同步任务 |
| `R` | 刷新数据概览 |
| `L` | 聚焦到日志区域 |

---

## 6. API 接口规范

### 6.1 新增 API 端点

```typescript
// 获取同步状态概览
GET /api/sync/status
→ {
  stocks: { count: 5491, lastSync: '2026-04-30T09:00:00Z' },
  ohlcv: { latestDate: '2026-04-30', coverage: 0.87, lastSync: '2026-04-30T09:00:00Z' },
  fundamentals: { latestDate: '2026-04-30', coverage: 0.92, lastSync: '2026-04-30T08:30:00Z' },
  calendar: { latestDate: '2026-04-30', lastSync: '2026-01-01T00:00:00Z' },
  factors: { latestDate: '2026-04-30', coverage: 0.78, lastSync: '2026-04-30T07:00:00Z' }
}

// 获取同步任务列表
GET /api/sync/jobs?status=running&limit=20
→ { jobs: SyncJob[], total: number }

// 获取单个任务详情
GET /api/sync/jobs/:id
→ SyncJob

// 创建同步任务 (替代现有的直接 POST /sync/*)
POST /api/sync/jobs
Body: {
  type: 'ohlcv' | 'fundamental' | 'stocks' | 'calendar' | 'factors' | 'dividends' | 'splits' | 'index_constituents',
  mode: 'incremental' | 'full',
  symbols?: string[],       // 为空时同步全部
  startDate?: string,       // YYYY-MM-DD
  endDate?: string,         // YYYY-MM-DD
  options?: Record<string, any>
}
→ { jobId: string, status: 'pending' | 'running' }

// 取消同步任务
DELETE /api/sync/jobs/:id
→ { message: 'cancelled' }

// 重试失败任务
POST /api/sync/jobs/:id/retry
→ { jobId: string, status: 'pending' }

// 获取同步日志
GET /api/sync/logs?level=info&startTime=...&endTime=...&limit=100
→ { logs: SyncLogEntry[], total: number }

// WebSocket: 订阅同步进度 (可选)
WS /ws/sync
→ 实时推送: { jobId: string, progress: number, status: string, message?: string }
```

### 6.2 现有端点复用

| 现有端点 | 用途 |
|----------|------|
| `GET /stocks/count` | 股票总数概览 |
| `GET /market/index` | 系统健康检查 |
| `POST /sync/ohlcv` | 单只/批量 OHLCV 同步 |
| `POST /sync/ohlcv/all` | 全量 OHLCV 同步 |
| `POST /sync/fundamentals` | 财务数据同步 |
| `POST /sync/stocks` | 股票列表同步 |
| `POST /sync/calendar` | 交易日历同步 |
| `POST /sync/factors/:name` | 单因子同步 |
| `POST /sync/factors/all` | 全因子同步 |

---

## 7. 状态管理

### 7.1 Pinia Store

```typescript
// stores/sync.ts
export const useSyncStore = defineStore('sync', () => {
  // State
  const overview = ref<SyncOverview | null>(null)
  const jobs = ref<SyncJob[]>([])
  const logs = ref<SyncLogEntry[]>([])
  const activeTab = ref<SyncDataType>('ohlcv')
  const isLoading = ref(false)
  const error = ref<string | null>(null)

  // Actions
  async function fetchOverview()
  async function fetchJobs(status?: SyncJobStatus)
  async function createJob(req: CreateSyncJobRequest)
  async function cancelJob(jobId: string)
  async function retryJob(jobId: string)
  async function fetchLogs(filters: LogFilters)

  // WebSocket 连接管理
  function connectWebSocket()
  function disconnectWebSocket()

  return {
    overview, jobs, logs, activeTab, isLoading, error,
    fetchOverview, fetchJobs, createJob, cancelJob, retryJob, fetchLogs,
    connectWebSocket, disconnectWebSocket
  }
})
```

---

## 8. 响应式设计细节

### 8.1 Desktop (>1200px)

```
┌─────────────────────────────────────────────────────────────┐
│  标题栏                                                      │
├──────────────────────────────┬──────────────────────────────┤
│                              │                              │
│  同步控制面板 (65%)          │  任务队列 + 日志 (35%)       │
│                              │                              │
│  [标签页]                    │  ┌─────────────┐             │
│  ┌────────────────────────┐  │  │ 任务队列    │             │
│  │ 覆盖度 + 操作区        │  │  └─────────────┘             │
│  └────────────────────────┘  │  ┌─────────────┐             │
│  ┌────────────────────────┐  │  │ 实时日志    │             │
│  │ 最近记录表格           │  │  └─────────────┘             │
│  └────────────────────────┘  │                              │
│                              │                              │
└──────────────────────────────┴──────────────────────────────┘
```

### 8.2 Tablet (768-1200px)

- 概览卡片: 2×2 网格
- 标签页: 横向滚动，显示图标+文字
- 同步控制: 全宽
- 任务队列: 全宽，位于控制面板下方
- 日志: 可折叠面板

### 8.3 Mobile (<768px)

- 概览卡片: 垂直堆叠
- 标签页: 下拉选择器 (n-select) 替代标签页
- 日期范围: 垂直堆叠的两个日期选择器
- 操作按钮: 垂直堆叠，全宽
- 任务队列: 卡片式列表，可横向滑动操作
- 日志: 底部固定面板，可上滑展开

---

## 9. 动画与过渡

| 元素 | 动画 | 时长 | 缓动 |
|------|------|------|------|
| 标签页切换 | 内容淡入 + 轻微上移 | 200ms | ease-out |
| 进度条更新 | 宽度平滑过渡 | 300ms | ease-in-out |
| 任务卡片进入 | 从右侧滑入 | 200ms | ease-out |
| 日志追加 | 底部淡入 | 150ms | ease-out |
| 同步完成提示 | 脉冲动画 + 颜色变化 | 500ms | ease-bounce |
| 概览数字更新 | 数字滚动 | 500ms | ease-out |

---

## 10. 可访问性

- 所有操作按钮有 `aria-label`
- 进度条使用 `role="progressbar"` + `aria-valuenow`
- 日志区域使用 `aria-live="polite"` 通知屏幕阅读器
- 颜色不单独传递状态信息（图标 + 文字同时存在）
- 支持键盘导航: Tab 切换焦点, Enter/Space 触发操作

---

## 11. 相关文件

| 文件 | 说明 |
|------|------|
| [pages/DataSync.vue](../../web/src/pages/DataSync.vue) | 页面组件 (待创建) |
| [components/sync/SyncOverviewCards.vue](../../web/src/components/sync/SyncOverviewCards.vue) | 概览卡片 (待创建) |
| [components/sync/SyncControlPanel.vue](../../web/src/components/sync/SyncControlPanel.vue) | 同步控制面板 (待创建) |
| [components/sync/SyncJobQueue.vue](../../web/src/components/sync/SyncJobQueue.vue) | 任务队列 (待创建) |
| [components/sync/SyncLogViewer.vue](../../web/src/components/sync/SyncLogViewer.vue) | 日志查看器 (待创建) |
| [components/sync/DataQualityDashboard.vue](../../web/src/components/sync/DataQualityDashboard.vue) | 质量仪表盘 (待创建) |
| [stores/sync.ts](../../web/src/stores/sync.ts) | Pinia Store (待创建) |
| [api/sync.ts](../../web/src/api/sync.ts) | API 客户端 (待创建) |
| [types/sync.ts](../../web/src/types/sync.ts) | TypeScript 类型 (待创建) |

---

_文档版本: 1.0_  
_创建日期: 2026-05-03_  
_状态: Design Spec (Pending Implementation)_
