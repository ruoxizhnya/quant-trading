# 交互规范

> 状态管理、动画过渡、反馈模式

---

## 1. 状态管理

### 1.1 全局状态

使用 Pinia 管理全局状态：

```typescript
// stores/app.ts
export const useAppStore = defineStore('app', () => {
  const sidebarCollapsed = ref(false)
  const apiConnected = ref(true)
  const currentTheme = ref<'dark' | 'light'>('dark')

  function toggleSidebar() {
    sidebarCollapsed.value = !sidebarCollapsed.value
  }

  return { sidebarCollapsed, apiConnected, currentTheme, toggleSidebar }
})
```

### 1.2 页面级状态

页面内部状态使用 Composition API：

```typescript
// 回测页面状态
const form = ref({
  strategy: 'momentum',
  stockPool: '',
  startDate: '',
  endDate: '',
  initialCapital: 1_000_000,
})

// 大对象使用 shallowRef
const result = shallowRef<BacktestResult | null>(null)
const loading = ref(false)
const error = ref<string | null>(null)
```

### 1.3 状态转换图

```
Idle -> Loading -> Success -> Display
  |       |          |
  |       v          v
  |    Error <------ Retry
  v
Cancelled
```

---

## 2. 动画规范

### 2.1 过渡时长

| 类型 | 时长 | 用途 |
|------|------|------|
| 微交互 | 150ms | 按钮 hover、颜色变化 |
| 标准 | 200ms | 展开/折叠、淡入淡出 |
| 复杂 | 300ms | 页面切换、模态框 |
| 数据 | 500ms | 图表动画、数字滚动 |

### 2.2 缓动函数

```css
--ease-default: cubic-bezier(0.4, 0, 0.2, 1);
--ease-in: cubic-bezier(0.4, 0, 1, 1);
--ease-out: cubic-bezier(0, 0, 0.2, 1);
--ease-bounce: cubic-bezier(0.68, -0.55, 0.265, 1.55);
```

### 2.3 常用动画

```css
/* 淡入 */
.fade-in {
  animation: fadeIn 200ms ease-out;
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

/* 滑入 */
.slide-up {
  animation: slideUp 300ms ease-out;
}

@keyframes slideUp {
  from {
    opacity: 0;
    transform: translateY(20px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

/* 脉冲（加载中） */
.pulse {
  animation: pulse 1.5s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}
```

---

## 3. 反馈模式

### 3.1 加载状态

| 场景 | 反馈方式 |
|------|----------|
| 按钮提交 | 按钮 loading + 禁用 |
| 数据加载 | Skeleton 骨架屏 |
| 长时任务 | 进度条 + 可取消 |
| 图表渲染 | 加载动画覆盖 |

### 3.2 成功反馈

```typescript
// 使用 Naive UI message
import { useMessage } from 'naive-ui'

const message = useMessage()

// 成功提示
message.success('回测完成！')

// 带操作的提示
message.success('策略已保存', {
  action: () => h(NButton, { text: true }, { default: () => '查看' })
})
```

### 3.3 错误处理

| 错误类型 | 处理方式 |
|----------|----------|
| 网络错误 | 自动重试 3 次，失败后提示刷新 |
| 业务错误 | 表单字段级错误提示 |
| 系统错误 | 全局错误边界 + 友好提示 |

```typescript
// 错误边界组件
const errorBoundary = {
  fallback: (error: Error) => h(ErrorPage, { error }),
  onError: (error: Error) => {
    console.error('Global error:', error)
    // 上报监控
  }
}
```

---

## 4. 表单交互

### 4.1 验证时机

- **即时验证**: 输入框失焦时 (`blur`)
- **提交验证**: 点击提交按钮时全量验证
- **实时验证**: 股票代码输入时实时校验格式

### 4.2 错误提示

```vue
<n-form-item
  label="股票代码"
  :rule="{ required: true, message: '请输入股票代码' }"
>
  <n-input v-model:value="form.stockPool" />
</n-form-item>
```

### 4.3 自动保存

表单支持草稿自动保存：

```typescript
const draft = useLocalStorage('backtest-draft', {})

// 监听表单变化，自动保存
watch(form, (value) => {
  draft.value = value
}, { deep: true })
```

---

## 5. 手势与快捷键

### 5.1 键盘快捷键

| 快捷键 | 功能 |
|--------|------|
| `Ctrl/Cmd + Enter` | 提交表单 |
| `Esc` | 关闭弹窗/取消操作 |
| `Ctrl/Cmd + K` | 打开命令面板 |
| `R` | 刷新数据 |

### 5.2 触摸手势

| 手势 | 功能 |
|------|------|
| 左滑 | 展开侧边栏 |
| 下拉 | 刷新列表 |
| 长按 | 显示操作菜单 |

---

## 6. 可访问性

### 6.1 键盘导航

- 所有交互元素可通过 `Tab` 键访问
- 焦点状态清晰可见 (`outline: 2px solid var(--q-primary)`)
- 模态框打开时焦点 trapped 在框内

### 6.2 屏幕阅读器

```vue
<!-- 图表提供文字摘要 -->
<figure>
  <canvas ref="chartRef" aria-label="权益曲线图" />
  <figcaption class="sr-only">
    回测期间净值从 100 万增长至 120 万，最大回撤 5%
  </figcaption>
</figure>
```

### 6.3 颜色对比度

- 文字与背景对比度 ≥ 4.5:1（AA 级）
- 大文字对比度 ≥ 3:1
- 不单独使用颜色传递信息
