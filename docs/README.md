---
status: evergreen
last-verified: 2026-09-16
verified-by: 人工整理（文档体系重组）
---

# Quant Lab 文档入口

> **这是文档的唯一入口。** 所有其他文档通过本页导航，本页不复制其他文档的内容。

---

## 一句话

自托管的 A 股量化研究平台。**AI 是操作回测底座的实验员，人是实验室主任**——AI 负责试错，人负责判断。

---

## 我该读哪一份？

| 我想…… | 读这个 | 状态 |
|---|---|---|
| 了解这个产品是什么、为谁做 | [PRODUCT.md](PRODUCT.md) | 待按新定位重写 |
| 了解设计原则 | [VISION.md](VISION.md) | 待重写（74KB 原文见 `archive/VISION-full.md`） |
| 了解系统怎么搭 | [ARCHITECTURE.md](ARCHITECTURE.md) | 待重写（精简至 ≤5 页） |
| 查 API / 数据模型 / 契约 | [SPEC.md](SPEC.md) | 现行（Reference 类，可长） |
| 查某个架构决策的来龙去脉 | [ADR.md](ADR.md) → [adr/](adr/) | 现行（18 条） |
| 了解测试策略 | [TEST.md](TEST.md) | 现行 |
| 看阶段规划 | [ROADMAP.md](ROADMAP.md) | 现行 |
| 本地起步（环境 / 命令 / 已知坑） | [guides/local-dev.md](guides/local-dev.md) | 现行 |
| 加数据源 / 加因子 / 跑一轮实验 | — | 待补（S1 / S3 开工前再写） |
| 实盘接口（**未实现**，仅接口参考） | [live-trading.md](live-trading.md) | 已标注 |
| **当前在做什么** | [TASKS.md](TASKS.md) | 现行（只放未完成项） |
| 找历史记录 / 旧决策 / 取证报告 | [archive/](archive/) | **只读，不维护** |

**新来的人读这份顺序**：PRODUCT → ARCHITECTURE → SPEC。三份读完就能上手。

---

## 文档治理规则

**R1 · Frontmatter 强制**
每份文档顶部必须写 `status` / `last-verified` / `verified-by`。
**没有 `last-verified` 的文档 = 已腐烂**，可读但不可信。

**R2 · 状态只有三种**

| status | 含义 | 规则 |
|---|---|---|
| `evergreen` | 长期有效 | 随代码更新，`last-verified` 必须跟着走 |
| `active` | 当前进行中 | 完成即转 `archived` |
| `archived` | 历史快照 | **只读、不改、不进本页导航** |

**R3 · 常青层字数上限**
PRODUCT ≤3 页 / VISION ≤2 页 / ARCHITECTURE ≤5 页 / ROADMAP ≤2 页 / TASKS ≤5 页。
超限 → 细节外移到 `design/`。

**R4 · 何时该写 ADR**
只记「**高架构显著性 + 高撤销成本**」的决策。其余写进 git commit message。
判断测试：**半年后如果有人问"为什么当初这么选"，答不上来，才需要 ADR。**
不写 ADR 的情形：只是细节设计、只是规范策略、没有真实可行的备选方案。
被取代的 ADR 移入 `archive/superseded-adr/`，不在 `adr/` 保留。

**R5 · 文档必须可被验证**
CI 扫描常青文档里引用的文件路径、API 路径、表名、配置项，验证其真实存在。
**这是文档不腐烂的唯一可靠机制。**（当前 CI 缺失，见 TASKS.md P0）

---

## 归档层说明

`archive/` 下是历史记录，**只读，不维护，不进导航**，仅供参考与溯源：

| 目录 | 内容 |
|---|---|
| `archive/odr/` | ODR 001–064（实施与取证记录，与 git log 重复） |
| `archive/superseded-adr/` | 已被取代的 ADR 009/010/012/021 |
| `archive/TASKS-history.md` | 旧任务文档（217KB，含 Sprint 1–6） |
| `archive/VISION-full.md` | 旧愿景文档原文（74KB） |
| `archive/RESEARCH-equitydeep-legacy.md` | EquityDeep 旧详案（定位已变更） |
| `archive/2026-Q2/*` | 早期报告、计划、调研 |

---

## 维护本页

新增常青文档时，必须在上方导航表加一行。
文档进入 `archived` 状态时，必须从导航表移除。
