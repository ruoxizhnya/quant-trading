# `docs/migrations/` — 历史记录，**不被执行**

> ⚠️ **这个目录里的 `.sql` 文件不会被执行。** 加表 / 加列请改
> `pkg/storage/postgres.go` 的 `migrate()` 数组。

这里是 `migrations/` 的**另一批同源副本**：早期文档迁移时按「迁移文件要跟文档
放一起」的习惯留下的，含 025~027 的实际变更记录（`fundamentals` 并入
`stock_fundamentals`、`factor_name` 加宽、`factor_cache` 携 citation 坐标）。

与 `migrations/` 一样：**只读、不维护、不进文档导航**。

唯一执行路径、以及「加表要改哪里」，见 [`../migrations/README.md`](../migrations/README.md)
与 `pkg/storage/postgres.go` 的 `migrate()` 文档注释。

> **为什么不一刀切删掉**：它们是「当时为什么这么改」的证据链，与 ODR / 审计报告
> 同性质。是否物理移入 `docs/archive/` 需要一次单独的裁决（登记为 Cleanup ODR），
> 不在本次文档校准的范围内。
