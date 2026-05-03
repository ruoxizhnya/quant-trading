# 组件库

> Quant Lab 自定义组件使用规范

---

## 1. 布局组件

### 1.1 AppLayout

**文件**: `components/layout/AppLayout.vue`

**用途**: 应用整体布局框架，包含侧边栏、顶部栏、内容区。

**Props**: 无

**Slots**:
- 默认 slot: 页面内容（通过 `router-view` 注入）

**状态**:
- `collapsed`: 侧边栏折叠状态

**布局结构**:
```
.app-layout (flex, height: 100vh)
├── AppSidebar (width: 220px / 60px)
└── .app-main (flex: 1)
    ├── AppHeader (height: 56px)
    └── main.app-content (flex: 1, overflow-y: auto, padding: 24px)
        └── <router-view />
```

---

### 1.2 AppSidebar

**文件**: `components/layout/AppSidebar.vue`

**用途**: 左侧导航栏，包含品牌标识、导航菜单、系统状态。

**Props**:
| 属性 | 类型 | 说明 |
|------|------|------|
| `collapsed` | `boolean` | 是否折叠 |

**导航项**:
| 路径 | 标签 | 图标 |
|------|------|------|
| `/` | 控制台 | `HomeOutline` |
| `/backtest` | 回测引擎 | `AnalyticsOutline` |
| `/screener` | 选股器 | `SearchOutline` |
| `/copilot` | 策略 Copilot | `ChatbubbleEllipsesOutline` |
| `/strategy-lab` | 策略实验室 | `BeakerOutline` |

**样式**:
- 宽度: `220px` (展开) / `60px` (折叠)
- 背景: `var(--q-surface)`
- 激活项: `rgba(88,166,255,0.12)` 背景 + `var(--q-primary)` 文字

---

### 1.3 AppHeader

**文件**: `components/layout/AppHeader.vue`

**用途**: 顶部栏，包含菜单切换、API 状态、时钟、主题切换。

**Props**: 无

**Events**:
| 事件 | 说明 |
|------|------|
| `toggle-sidebar` | 点击菜单按钮时触发 |

**元素**:
- 左侧: 菜单按钮 + Logo
- 右侧: API 状态标签 + 实时时钟 + 主题切换按钮

---

## 2. 控制台组件

### 2.1 MarketMetrics

**文件**: `components/dashboard/MarketMetrics.vue`

**用途**: 展示市场指数概览数据。

**Props**:
| 属性 | 类型 | 说明 |
|------|------|------|
| `selectedDate` | `string` | 选择日期 |
| `selectedIndex` | `string` | 选择指数 |
| `loading` | `boolean` | 加载状态 |
| `metrics` | `Record<string, any>` | 指标数据 |

**Events**:
| 事件 | 说明 |
|------|------|
| `update:selectedDate` | 日期变更 |
| `update:selectedIndex` | 指数变更 |
| `refresh` | 刷新数据 |

**布局**: 4 列网格展示收盘价、涨跌幅、成交量、成交额

---

### 2.2 QuickBacktest

**文件**: `components/dashboard/QuickBacktest.vue`

**用途**: 快速回测入口，简化版表单。

**Props**:
| 属性 | 类型 | 说明 |
|------|------|------|
| `strategy` | `string` | 策略名称 |
| `stock` | `string` | 股票代码 |
| `startDate` | `string` | 开始日期 |
| `endDate` | `string` | 结束日期 |
| `running` | `boolean` | 运行中 |
| `strategies` | `string[]` | 策略列表 |
| `quickResult` | `QuickResult \| null` | 快速结果 |

**布局**: inline 表单，一行展示所有字段

---

### 2.3 NavTiles

**文件**: `components/dashboard/NavTiles.vue`

**用途**: 功能导航磁贴，快速跳转到各模块。

**布局**: 响应式网格，`repeat(auto-fill, minmax(220px, 1fr))`

**磁贴数据**:
| 目标 | 标题 | 描述 | 主题色 |
|------|------|------|--------|
| `/backtest` | 回测引擎 | 单股票/组合回测分析 | `#58a6ff` |
| `/screener` | 因子选股 | 多因子筛选与排名 | `#a371f7` |
| `/strategy-lab` | 策略实验室 | 策略管理与参数优化 | `#f78166` |
| `/copilot` | AI Copilot | 智能策略生成助手 | `#3fb950` |
| `/dashboard?tab=batch` | 批量回测 | 多策略多标的对比 | `#f85149` |

---

## 3. 回测组件

### 3.1 BacktestForm

**文件**: `components/backtest/BacktestForm.vue`

**用途**: 回测参数配置表单。

**Props**:
| 属性 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `strategy` | `string` | - | 策略名称 |
| `stockPool` | `string` | - | 股票池（逗号分隔） |
| `startDate` | `string` | - | 开始日期 |
| `endDate` | `string` | - | 结束日期 |
| `initialCapital` | `number` | - | 初始资金 |
| `commissionRate` | `number` | - | 手续费率 |
| `slippageRate` | `number` | - | 滑点率 |
| `loading` | `boolean` | `false` | 加载状态 |
| `strategies` | `string[]` | `[]` | 策略列表 |

**布局**: 响应式网格，`repeat(auto-fit, minmax(160px, 1fr))`

---

### 3.2 MetricsCards

**文件**: `components/backtest/MetricsCards.vue`

**用途**: 回测结果指标卡片组。

**Props**:
| 属性 | 类型 | 说明 |
|------|------|------|
| `metrics` | `MetricItem[]` | 指标数组 |

**MetricItem 结构**:
```typescript
interface MetricItem {
  label: string   // 指标名称
  value: string   // 格式化后的值
  cls: string     // CSS 类: 'positive' | 'negative' | ''
}
```

**布局**: 5 列网格 → 3 列 → 2 列（响应式）

---

### 3.3 EquityChart

**文件**: `components/backtest/EquityChart.vue`

**用途**: 权益曲线 + 股价曲线 + 交易标记。

**Props**:
| 属性 | 类型 | 说明 |
|------|------|------|
| `portfolioValues` | `PortfolioPoint[]` | 净值数据 |
| `trades` | `Trade[]` | 交易记录 |
| `showTrades` | `boolean` | 是否显示交易标记 |
| `stockPrices` | `PricePoint[]` | 股价数据 |

**图表配置**:
- 净值线: 蓝色 `#58a6ff`，左 Y 轴
- 股价线: 橙色 `#f0883e`，右 Y 轴，虚线
- 买入标记: 绿色三角形
- 卖出标记: 红色叉号
- 最大数据点: 150（自动采样）

---

### 3.4 TradeTable

**文件**: `components/backtest/TradeTable.vue`

**用途**: 交易记录表格。

**Props**:
| 属性 | 类型 | 说明 |
|------|------|------|
| `trades` | `Trade[]` | 交易数据 |

**列定义**:
| 列 | 宽度 | 说明 |
|----|------|------|
| 方向 | 70px | 多/空/平，带颜色标签 |
| 股票 | 110px | 股票代码 |
| 入场日期 | 110px | - |
| 入场价 | 85px | 保留 2 位小数 |
| 出场日期 | 110px | - |
| 出场价 | 85px | 保留 2 位小数 |
| 数量 | 65px | - |
| PnL | 90px | 带颜色（绿/红） |

---

### 3.5 BacktestHistory

**文件**: `components/backtest/BacktestHistory.vue`

**用途**: 回测历史记录列表，支持展开查看交易明细。

**Props**:
| 属性 | 类型 | 说明 |
|------|------|------|
| `history` | `HistoryEntry[]` | 历史记录 |

**Events**:
| 事件 | 说明 |
|------|------|
| `clear` | 清除历史 |
| `view-report` | 查看报告详情 |

---

### 3.6 BacktestProgress

**文件**: `components/backtest/BacktestProgress.vue`

**用途**: 异步回测进度展示。

**Props**:
| 属性 | 类型 | 说明 |
|------|------|------|
| `visible` | `boolean` | 是否显示 |
| `status` | `JobStatus` | 任务状态 |
| `progress` | `number` | 进度百分比 |
| `error` | `string \| null` | 错误信息 |
| `cancellable` | `boolean` | 是否可取消 |

**状态映射**:
| 状态 | 标签 | 颜色 |
|------|------|------|
| `pending` | 等待中 | info |
| `running` | 执行中 | info |
| `completed` | 已完成 | success |
| `failed` | 失败 | error |
| `cancelled` | 已取消 | warning |

---

## 4. 组件使用规范

### 4.1 图标处理

所有图标组件必须使用 `markRaw()` 包装，防止 Vue 响应式代理：

```typescript
import { markRaw } from 'vue'
import { HomeOutline } from '@vicons/ionicons5'

const navItems = [
  { icon: markRaw(HomeOutline), label: '控制台' }
]
```

### 4.2 大对象响应式

回测结果等大对象使用 `shallowRef`：

```typescript
const result = shallowRef<BacktestResult | null>(null)

// 更新时手动触发
result.value = newResult
triggerRef(result)
```

### 4.3 DOM 操作时机

状态变更后访问 canvas 等 DOM 元素，必须 `await nextTick()`：

```typescript
async function renderChart() {
  chartData.value = data
  await nextTick()
  if (!canvasRef.value) return
  // 操作 canvas...
}
```

### 4.4 工具函数复用

格式化函数统一从 `utils/format.ts` 导入，不在组件中重复定义：

```typescript
import { fmtPercent, fmtNumber, fmtVolume } from '@/utils/format'
```
