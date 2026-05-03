# 视觉规范

> 色彩、字体、间距、布局的完整规范

---

## 1. 色彩系统

### 1.1 基础色板

| Token | 色值 | 用途 |
|-------|------|------|
| `--q-bg` | `#0d1117` | 页面背景 |
| `--q-surface` | `#161b22` | 卡片背景 |
| `--q-surface2` | `#1c2333` | 次级背景、hover 状态 |
| `--q-surface3` | `#21262d` | 三级背景、表头 |
| `--q-border` | `#30363d` | 边框、分割线 |
| `--q-border-light` | `#21262d` | 浅色边框 |

### 1.2 文字色彩

| Token | 色值 | 用途 |
|-------|------|------|
| `--q-text` | `#e6edf3` | 主标题、重要数据 |
| `--q-text2` | `#8b949e` | 正文、标签 |
| `--q-text3` | `#484f58` | 占位符、禁用状态 |

### 1.3 功能色

| Token | 色值 | 用途 |
|-------|------|------|
| `--q-primary` | `#58a6ff` | 主题色、链接、激活状态 |
| `--q-primary-dark` | `#1f6feb` | 主题色深色变体 |
| `--q-primary-light` | `#79c0ff` | 主题色浅色变体 |
| `--q-success` | `#3fb950` | 正收益、成功状态 |
| `--q-danger` | `#f85149` | 负收益、错误状态 |
| `--q-warning` | `#d29922` | 警告、提示 |
| `--q-info` | `#79c0ff` | 信息提示 |

### 1.4 语义化使用

```css
/* 收益率 */
.positive { color: var(--q-success); }
.negative { color: var(--q-danger); }

/* 交互状态 */
.nav-item.active {
  background: rgba(88, 166, 255, 0.12);
  color: var(--q-primary);
}

/* 悬停效果 */
.metric-box:hover {
  transform: translateY(-1px);
  box-shadow: var(--q-shadow);
}
```

---

## 2. 字体系统

### 2.1 字体栈

```css
--q-font: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Noto Sans SC', Roboto, sans-serif;
--q-mono: 'JetBrains Mono', 'Fira Code', monospace;
```

### 2.2 字号层级

| 层级 | 字号 | 字重 | 用途 |
|------|------|------|------|
| H1 | 24px | 700 | 页面标题 |
| H2 | 18px | 700 | 卡片标题 |
| H3 | 14px | 600 | 区块标题 |
| Body | 13px | 400 | 正文 |
| Small | 11px | 400 | 标签、辅助文字 |
| Data | 20px | 700 | 指标数值 |
| Mono | 13px | 400 | 代码、数字 |

### 2.3 数字格式化

```typescript
// 收益率: +12.34% / -5.67%
fmtPercent(0.1234) // "+12.34%"

// 金额: ¥1,234,567.89
fmtNumber(1234567.89, 2) // "1,234,567.89"

// 成交量: 1.23M / 1.23B
fmtVolume(1230000) // "1.23M"
```

---

## 3. 间距系统

### 3.1 基础单位

以 `4px` 为基础单位：

| Token | 值 | 用途 |
|-------|-----|------|
| `--space-xs` | 4px | 图标与文字间距 |
| `--space-sm` | 8px | 紧凑元素间距 |
| `--space-md` | 12px | 卡片内边距、网格间距 |
| `--space-lg` | 16px | 表单元素间距 |
| `--space-xl` | 20px | 卡片间距 |
| `--space-2xl` | 24px | 页面内边距 |

### 3.2 布局间距

```css
/* 页面容器 */
.page-container {
  max-width: 1400px;
  margin: 0 auto;
  padding: 24px;
}

/* 卡片间距 */
.card + .card { margin-top: 20px; }

/* 表单网格 */
.form-grid {
  display: grid;
  gap: 12px 16px;
}
```

---

## 4. 圆角与阴影

### 4.1 圆角

| Token | 值 | 用途 |
|-------|-----|------|
| `--q-radius-xs` | 4px | 按钮、标签 |
| `--q-radius-sm` | 6px | 输入框、小卡片 |
| `--q-radius` | 10px | 卡片、弹窗 |

### 4.2 阴影

```css
--q-shadow: 0 1px 3px rgba(0, 0, 0, 0.3);    /* 卡片默认 */
--q-shadow-lg: 0 10px 25px rgba(0, 0, 0, 0.3); /* 弹窗、下拉 */
```

---

## 5. 布局规范

### 5.1 整体布局

```
┌─────────────────────────────────────────┐
│  Sidebar (220px)  │  Header (56px)      │
│                   ├─────────────────────┤
│                   │                     │
│                   │    Content Area     │
│                   │    (padding: 24px)  │
│                   │                     │
└───────────────────┴─────────────────────┘
```

### 5.2 响应式断点

```css
/* Desktop */
@media (min-width: 1200px) {
  .metrics-grid { grid-template-columns: repeat(5, 1fr); }
  .form-grid { grid-template-columns: repeat(4, 1fr); }
}

/* Tablet */
@media (max-width: 1200px) {
  .metrics-grid { grid-template-columns: repeat(3, 1fr); }
}

/* Mobile */
@media (max-width: 768px) {
  .metrics-grid { grid-template-columns: repeat(2, 1fr); }
  .form-grid { grid-template-columns: 1fr; }
  .app-sidebar { display: none; } /* 或折叠 */
}
```

### 5.3 网格系统

- 指标卡片: `repeat(5, 1fr)` → `repeat(3, 1fr)` → `repeat(2, 1fr)`
- 表单: `repeat(4, 1fr)` → `repeat(2, 1fr)` → `1fr`
- 导航磁贴: `repeat(auto-fill, minmax(220px, 1fr))`

---

## 6. CSS 变量完整列表

```css
:root {
  /* Background */
  --q-bg: #0d1117;
  --q-surface: #161b22;
  --q-surface2: #1c2333;
  --q-surface3: #21262d;

  /* Border */
  --q-border: #30363d;
  --q-border-light: #21262d;

  /* Text */
  --q-text: #e6edf3;
  --q-text2: #8b949e;
  --q-text3: #484f58;

  /* Accent */
  --q-primary: #58a6ff;
  --q-primary-dark: #1f6feb;
  --q-primary-light: #79c0ff;
  --q-success: #3fb950;
  --q-danger: #f85149;
  --q-warning: #d29922;
  --q-info: #79c0ff;

  /* Shape */
  --q-radius: 10px;
  --q-radius-sm: 6px;
  --q-radius-xs: 4px;
  --q-shadow: 0 1px 3px rgba(0, 0, 0, 0.3);
  --q-shadow-lg: 0 10px 25px rgba(0, 0, 0, 0.3);

  /* Motion */
  --q-transition: 0.2s ease;

  /* Typography */
  --q-font: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Noto Sans SC', Roboto, sans-serif;
  --q-mono: 'JetBrains Mono', 'Fira Code', monospace;

  /* Layout */
  --q-sidebar-width: 220px;
  --q-header-height: 56px;
}
```
