# `migrations/` — 历史记录，**不被执行**

> ⚠️ **这个目录里的 `.sql` 文件不会被执行。** 加表 / 加列请改
> `pkg/storage/postgres.go` 的 `migrate()` 数组（见下）。

## 唯一执行路径

`pkg/storage/postgres.go` 的 `migrate()` 里有一个 DDL 字符串数组，
服务每次启动按顺序跑一遍。每条都必须是幂等的
（`CREATE TABLE IF NOT EXISTS` / `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` /
`DO $$ ... $$` 守卫）。

没有版本表、没有 down、没有执行记录 —— 这是有意的取舍，完整理由写在
`postgres.go` 的 `migrate()` 文档注释里。要点：现有库都是这套 Go DDL 建的，
没有 `schema_migrations` 表，接入标准迁移工具要先 baseline，而 baseline 一旦和
真实结构对不上，后面每条迁移都在错误的假设上跑。

## 加表 / 加列怎么做

1. 在 `pkg/storage/postgres.go` 的 `migrations` 数组**末尾追加**一条，
   写成幂等形态；
2. 加上 `// Migration 0NN: <说明>` 注释，**编号续最大的那个**（别照抄这里的
   文件名编号 —— 数组已经用到比它们更大的号）；
3. 如果这张表是 ETL 的 `TableMapper` 目标表，`pkg/storage/bulk_insert_ddl_test.go`
   的防漂移断言会要求它出现在内联 DDL 里（零 DB 依赖，静态读源码）。

## 这个目录里有什么

| 形态 | 文件 | 说明 |
|---|---|---|
| golang-migrate 格式 | `0000000{1,2,3}_*/{up,down}.sql` | 3 个目录。**没有 `migration_manager.go` 了**（2026-09-18 删除），所以这 6 个文件零调用方 |
| 裸编号 `.sql` | `003_*`、`012_*` ~ `019_*`、`023_*` | 早期逐条加的变更记录 |

`docs/migrations/` 是另一批同源副本（含 025~027 的实际变更记录），同样不执行。

**约定**：这些 `.sql` 只读、不维护、不进文档导航。它们保留是为了让
「当时为什么这么改」可追溯 —— 想知道**现在**的表结构，看 `postgres.go`。
