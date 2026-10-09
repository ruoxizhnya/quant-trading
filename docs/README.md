---
status: active
last-verified: 2026-10-09
verified-by: 文档拆仓（UI/AI/跨领域文档迁出后重建本入口，2026-10-09）
---

# Quant Trading — 文档入口（core 仓）

本仓是 Quant Lab 的**内核（仪器）**仓库。文档拆仓后（2026-10-09），本仓只保留
**内核自身 + 活跃治理 + 契约**三类文档，其余按 DDD 领域迁出：

| 领域 | 仓库 |
|---|---|
| 内核 / 回测 / 策略 / 表达式 / 执行 | 本仓（`quant-trading`） |
| AI 实验员 | [`quant-trading-agent`](https://github.com/ruoxizhnya/quant-trading-agent) |
| 前端 | [`quant-trading-ui`](https://github.com/ruoxizhnya/quant-trading-ui) |
| 跨领域 / 高层 / 历史档案 | [`quant-trading-docs`](https://github.com/ruoxizhnya/quant-trading-docs) |

## 本仓文档

- [SPEC.md](SPEC.md) — 技术规格（API / 契约）
- [TASKS.md](TASKS.md) — 活跃任务台账（随代码演进）
- [live-trading.md](live-trading.md) — 实盘 / paper 定位

其它不在入口的目录：

- `design/kernel/` — 模块化内核目标架构蓝图（内核领域）
- `migrations/` — 数据库迁移（内核的数据层）
- `test-cases/` — 回测用例
- `openapi.yaml` — 后端 API 契约（被 Go `//go:embed` 嵌进二进制，**不可迁出**）
- `archive/` — 内核领域的历史档案（其余 archive 已按领域迁到对应仓）

## ⚠️ 跨仓引用

本仓文档仍引用已迁出的文档（如 `adr/`、`ARCHITECTURE.md`、`hermes/`）。这些链接的
目标真实存在于其它仓，`tools/check_doc_links.py` 已内置迁移白名单，不会误报。
再次迁出文档时，记得把迁出的路径追加进该脚本的 `MIGRATED_PREFIXES` / `MIGRATED_FILES`。
