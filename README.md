# Quant Lab (quant-trading)

> **A股多因子量化交易系统** — Go 后端 + Vue 3 前端 + PostgreSQL + Redis

## 系统架构

```
┌─────────────────────────────────────────────────────┐
│                   Vue 3 SPA (web/)                    │
│              Naive UI + Chart.js + Pinia              │
├──────────┬──────────┬──────────┬──────────┬──────────┤
│ 控制台    │ 回测引擎  │ 选股器   │ Copilot  │ 策略实验室 │
└────┬─────┴────┬─────┴────┬─────┴────┬─────┴────┬─────┘
     │          │          │          │          │
     └──────────┴──────────┴──────────┴──────────┘
                        │ HTTP API (:8085)
                        ▼
┌─────────────────────────────────────────────────────┐
│                  Go Microservices                     │
├──────────┬──────────┬──────────┬──────────┬──────────┤
│ analysis │  data    │ strategy │  redis   │ postgres │
│  :8085   │  :8081   │  :8082   │  :6379   │  :5432   │
└──────────┴──────────┴──────────┴──────────┴──────────┘
```

## 快速开始

```bash
# 启动全部服务 (Docker Compose)
docker compose up -d

# 前端开发服务器
cd web && npm install && npm run dev

# E2E 测试
cd e2e && npx playwright test
```

## 核心功能

| 模块 | 说明 | 状态 |
|------|------|------|
| **回测引擎** | 多因子策略回测, T+1/涨跌停模拟, 净值曲线 | ✅ |
| **因子分析** | IC/IR 计算, 分组收益, 归因分析 | ✅ |
| **选股器** | 多维度股票筛选, PE/PB/市值过滤 | ✅ |
| **策略 Copilot** | LLM 驱动的策略代码生成与回测 | ✅ |
| **策略实验室** | 策略管理, 参数调优, 对比分析 | 🔄 |
| **实盘接口** | MockTrader 预留, broker 抽象层 | 🔲 |

## 文档索引

| 文档 | 内容 |
|------|------|
| [VISION](docs/VISION.md) | 设计原则、领域模型、核心决策 |
| [SPEC](docs/SPEC.md) | 技术规格、API 定义、数据模型 |
| [ARCHITECTURE](docs/ARCHITECTURE.md) | 微服务架构、数据库 schema、缓存设计 |
| [ROADMAP](docs/ROADMAP.md) | Sprint 规划、Phase 里程碑、状态追踪 |
| [ADR](docs/ADR.md) | 架构决策记录 (10 条 ADR) |
| [TEST](docs/TEST.md) | 测试策略、覆盖率目标、测试规范 |
| [NEXT_STEPS](docs/NEXT_STEPS.md) | 审计报告、问题清单、下一步计划 |

## 技术栈

- **后端**: Go 1.22+, Gin/标准库 HTTP, GORM, go-redis
- **前端**: Vue 3, TypeScript, Vite, Naive UI, Chart.js, Pinia
- **数据**: PostgreSQL 16, Redis 7, TimescaleDB (可选)
- **基础设施**: Docker Compose, Playwright E2E

## 项目结构

```
quant-trading/
├── cmd/                 # 服务入口 (analysis, data, strategy, execution)
├── pkg/                 # 核心库 (backtest, data, domain, storage, strategy)
├── web/src/             # Vue 3 前端源码
├── e2e/tests/           # Playwright E2E 测试
├── docs/                # 设计文档
│   ├── adr/             # 架构决策记录
│   └── test-cases/      # 测试用例规范
├── migrations/          # 数据库迁移脚本
└── docker-compose.yml   # 编排配置
```

## License

MIT
