# ODR-056: 阶段 P3 — EQD-P3-1 `fundamentals` / `stock_fundamentals` 表重叠收口（C-8）

> **Status**: Completed
> **Date**: 2026-09-15
> **Category**: Implementation
> **Related ADRs**: [ADR-022](../superseded-adr/adr-022-unified-research-platform.md)（§2 按数据性质分区：同一性质的数据只应有一处权威存储 —— `fundamentals` 与 `stock_fundamentals` 同属类 C 派生数据，重叠违反分区唯一性）
> **Supersedes**: —
> **Related ODRs**: [ODR-047](odr-047-equitydeep-integration-audit.md)（EquityDeep 集成审计 — DR-7 缺口来源，C-8 登记）, [ODR-054](odr-054-dr-reverification.md)（复核确认 DR-7 未回退、由 `EQD-P3-1` 承接）, [ODR-050](odr-050-p1-base-contract-landing.md)（统一迁移编号空间，预留 `025`）, [ODR-055](odr-055-eqd-p1-2-vertical-factors.md)（阶段 P3 前一任务，迁移编号 `025` 的顺位由其在脚注中声明）
> **Author**: AI Assistant

---

## Context

### 触发条件

[ODR-047](odr-047-equitydeep-integration-audit.md) DR-7 指出 `fundamentals` 与 `stock_fundamentals` 字段重叠，当时仅登记为 `TASKS.md` 任务 `EQD-P3-1`（C-8），未开工；[ODR-054](odr-054-dr-reverification.md) 复核确认该漂移「未回退」且「由阶段 P3 的 EQD-P3-1 承接」。阶段 P3 的前两项（EQD-P1-1 / EQD-P1-2）落地后，本项是阶段 P3 的最后一项，也是**唯一不依赖 EquityDeep M1 数据的一类 DDL 改动**。

出口判据：**两表合一，写入侧不再有两条并行路径，读取侧不再需要判断该查哪张表**；且合并过程中**不引入任何口径转换**（否则就是借「收口」之名做一次静默的数值改写）。

### 前置事实（本次现场复核）

| 项 | 复核结论 |
|---|---|
| `fundamentals` 结构 | `symbol VARCHAR(20)` + `PK(symbol, trade_date)`；指标列为 `DOUBLE PRECISION`；`created_at TIMESTAMPTZ`。真实 DDL 在 `migrations/00000002_2_ohlcv_fundamentals/up.sql` |
| `stock_fundamentals` 结构 | `id SERIAL PRIMARY KEY` + `ts_code VARCHAR(20)` + `UNIQUE(ts_code, trade_date)`；额外 `ann_date` / `end_date`；指标列为 `FLOAT`；`created_at TIMESTAMP` |
| 重叠列 | **12 列同名同义**：`pe` / `pb` / `ps` / `roe` / `roa` / `debt_to_equity` / `gross_margin` / `net_margin` / `revenue` / `net_profit` / `total_assets` / `total_liab` |
| 两表来源 | 同源于 migration 0002 建表；migration 014 各自补 `source` / `ingest_time` / `data_version`（`fundamentals` 无 `idx_*_source` 索引，`stock_fundamentals` 有 `idx_sf_source`） |
| **口径是否相同** | **完全相同** —— `pkg/data/tushare.go` 的 `normalizeFundamentals` 与 `normalizeFundamentalsData` 调用**同一个 tushare `fina_indicator` API、同一份字段列表**，且**都把 `trade_date` 置为 `end_date`**；`fundamentals.symbol` 存的本来就是 `ts_code` 字面量（`FetchFundamentals` 以 `ts_code` 为入参并取 `item[0]`） |
| 因此合并的语义 | **纯列名直通** —— 无格式转换、无量纲换算、无日期语义改写 |
| 写入路径（重叠根源） | ① `FetchFundamentals` → `SaveFundamentalBatch` → `fundamentals`；② `FetchFundamentalsData` → `SaveFundamentalDataBatch` → `stock_fundamentals`；③ `BulkInsert`（`data_type = "fundamentals"`）**已**映射到 `stock_fundamentals` |
| 读取路径（分裂） | 横截面快照 / 筛选 / 历史 → `stock_fundamentals`（`GetFundamentalsSnapshot` / `GetFundamentalDataLatest` / `GetFundamentalDataHistory` / `ScreenFundamentals`）；局部取数 → `fundamentals`（`GetFundamental` / `GetFundamentals`） |
| 实际执行路径 | `pkg/storage/postgres.go` 内联 `migrate()`（raw SQL 切片 + `pool.Exec`）；`docs/migrations/*.sql` 为同源文档副本 |
| `migrations/014_add_source_columns.sql` | 该文件位于 golang-migrate 目录（`migration_manager.go`，指向根 `migrations/`，**全仓无调用方**），仍含 `ALTER TABLE fundamentals ADD COLUMN` |
| 批量写入约定 | 统一 `pgx.Batch` + `SendBatch` + `ON CONFLICT ... DO UPDATE`；**拒绝 COPY** |
| 既有陈旧缺陷 | `SaveFundamentalBatch` 的 `DO UPDATE` 只列了 6 列，重摄取时另外 6 列的陈旧值被静默保留 |
| 工具链 | `go` 为 `C:\Users\ruoxi\sdk\go1.25.0\bin\go.exe`；Docker Desktop 未运行 → DB-backed 测试按既有约定 SKIP |
| 本仓 git 写对象 | 默认 createObject 模式下 `commit` / `add` 报 `Permission denied`，须 `git -c core.createObject=link` |

### 收口策略裁决

| 候选 | 说明 | 结果 |
|---|---|---|
| **合并入 `stock_fundamentals` 并 DROP `fundamentals`** | 以已承载 4 条读取路径的表为唯一幸存表，存量按 `symbol → ts_code` 直通并入后删表 | ✅ **采纳** |
| 降级为视图 | `fundamentals` 改为指向 `stock_fundamentals` 的兼容视图 | ✗ 保留两个名字，重叠在名义上继续存在，且视图写入语义复杂 |
| 仅停写并标记 deprecated | 不动 DDL，只把写入路径收敛 | ✗ 表仍在、索引仍在、读取侧仍需判断，欠债只是被标注而非清偿 |

---

## Decision

**以 `stock_fundamentals` 为唯一幸存表：迁移 025 把 `fundamentals` 存量按 `symbol → ts_code` 直通并入（冲突时幸存表优先）后 DROP 旧表；代码侧四个 `symbol` 系读写函数一次收敛到 `stock_fundamentals`，并顺带修复重摄取漏列缺陷。**

### 1. 迁移 025（`docs/migrations/025_equitydeep_field_consolidation.sql` + 内联 `migrate()`）

守卫式 `DO` 块（**与内联形式逐字对齐**，避免文档副本与执行路径漂移）：

```sql
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = 'public' AND table_name = 'fundamentals') THEN
        INSERT INTO stock_fundamentals (ts_code, trade_date, end_date, /* 12 指标列 */)
        SELECT f.symbol, f.trade_date, f.trade_date, /* 12 指标列 */ FROM fundamentals f
        ON CONFLICT (ts_code, trade_date) DO UPDATE SET
            pe = COALESCE(stock_fundamentals.pe, EXCLUDED.pe), /* …12 列同形… */;
        DROP TABLE fundamentals;
    END IF;
END $$;
```

- **守卫存在性**：表不存在的库（新装）重复启动等价于 no-op ⇒ 幂等。
- **`ON CONFLICT` 冲突语义**：`COALESCE(幸存表值, EXCLUDED 值)` —— **幸存表优先，仅用旧表值补空**。方向的选择理由：旧表的行可能比幸存表更陈旧，让旧表覆盖新值等于用更差的数据赢。
- **`end_date`**：用 `trade_date` 回填（两表口径一致，旧表无 `end_date` 列）。
- **`ann_date`**：旧表不存在该列，并入行该列留 NULL（不猜公告日）。
- **lineage 列**（`source` / `ingest_time` / `data_version`）：**不参与搬运**，并入行沿用 `stock_fundamentals` 的默认值。
- **DROP 不可逆**：回滚需从备份恢复；旧表结构与数据可由 migration 0002 + 014 重建。

### 2. 代码侧收敛 `pkg/storage/fundamentals.go`

| 函数 | 改动 |
|---|---|
| `SaveFundamental` | `fundamentals` / `symbol` → `stock_fundamentals` / `ts_code` |
| `SaveFundamentalBatch` | 同上；且 `DO UPDATE` 由 **6 列补齐为 12 列**（修复重摄取静默保留陈旧值） |
| `GetFundamental` | 同上（`SELECT ts_code` → 按位置 `Scan` 到 `&f.Symbol`） |
| `GetFundamentals` | 同上 |
| `SaveFundamentalDataBatch` / `GetFundamentalDataLatest\|History` / `GetFundamentalsSnapshot` / `ScreenFundamentals` | **未改** —— 本就走 `stock_fundamentals` |

### 3. DDL 与索引清理 `pkg/storage/postgres.go`

- 内联 `migrate()` 删除 `fundamentals` 建表 DDL 与 `idx_fundamentals_symbol` / `idx_fundamentals_trade_date` 两处索引。
- 迁移 025 的守卫式 `DO` 块追加到 migrations 切片末尾。
- 保留 `idx_stock_fundamentals_code` / `idx_stock_fundamentals_date`。
- 迁移 026（`factor_name` 放宽）与 025 的顺序按编号排列，无相互依赖。

### 4. 测试与文档副本同步

- `pkg/storage/integration_test.go` 的 `truncateAll` 表清单移除 `"fundamentals"`（否则集成测试会 TRUNCATE 一张不存在的表）。
- `docs/SPEC.md` 删除 `### TimescaleDB Schema` 段内的**旧版草案 DDL**（`symbol TEXT` / `date TIMESTAMPTZ` / `PK(symbol, date)` / `create_hypertable`）—— 该草案与实体 DDL 本就不一致（实体为 `VARCHAR(20)` + `DATE`），收口后更无保留价值，原位改为指向本记录的注释。

### 5. 迁移 014 的处置（不改）

`migrations/014_add_source_columns.sql` 仍对 `fundamentals` 执行 `ADD COLUMN`，但**不构成执行冲突**，故不改：

1. 该目录由 `migration_manager.go`（`golang-migrate` 的 `migrate.New("file://./migrations")`）消费，而 `MigrationManager` **全仓无调用方**；
2. 该目录**不含 025**，若被启用，其版本顺序恒早于内联 `migrate()` 的 DROP —— 即「先 ADD COLUMN，后并入并 DROP」，语义自洽；
3. 结论已写入 025 头部注释，避免下次审计重新排查。

---

## Consequences

### 正面

- **单一事实源**：12 个指标列在库中只有一处权威存储，符合 [ADR-022](../superseded-adr/adr-022-unified-research-platform.md) §2「按数据性质分区」的唯一性要求。
- **写入路径由 2 条收敛为 1 条**：`fundamentals` 一侧的写入消失后，重叠的**再生机制**被消除 —— 只删表不收敛代码，重叠会在下次同步时重新长出来。
- **零口径转换**：两表同源于同一 API、同一字段列表、同一 `trade_date = end_date` 约定 ⇒ 合并是可逐列核对的直通，不是一次「数据迁移项目」。
- **顺带修掉一个静默缺陷**：`SaveFundamentalBatch` 的 `DO UPDATE` 补齐到 12 列后，重摄取不再保留陈旧值。
- **读取侧不再需要判断**：`GetFundamental(s)` 与横截面路径指向同一张表，调用方无需知道「这里查的是哪张表」。
- **幂等且可重复部署**：守卫式 `DO` 块使新装库与老库走同一条路径。

### 负面 / 代价

- **DROP 不可逆**：无备份则无法回滚（旧表可从 migration 0002 + 014 重建结构，但**数据无法重建**）。
- **活跃表数 39 → 38**（内联 21 → 20，迁移新增仍 18）：这是项目首次**减少**活跃表 —— 后续文档中的表数断言须同步，否则会引入新的漂移。
- **旧表 lineage 列被丢弃**：`fundamentals` 的 `source` / `ingest_time` / `data_version` 不搬运，并入行沿用 `stock_fundamentals` 默认值 ⇒ 这批行的「当时从哪来」不再可答（可容忍：两表同源，`stock_fundamentals` 侧已有自己的 lineage）。
- **`ann_date` 并入行为 NULL**：若未来 PIT 逻辑要求基本面行必须有公告日，需另行回填。
- **文档副本与执行路径双写**：025 同时存在于 `docs/migrations/`（副本）与 `postgres.go` 内联（实际执行），一致性靠人工维护。

### 风险

| 风险 | 等级 | 应对 |
|---|---|---|
| 生产库中两表同键行的指标值**实际不一致**时，`COALESCE` 静默以幸存表为准 | 中 | 方向已显式声明为「幸存表优先」并写入 025 头部注释；如需审计须在 DROP **之前**做差异比对（见未做项） |
| 存量行数远超预期导致 `INSERT ... SELECT` 长事务 | 低 | 单语句、无逐行往返；基本面数据量级为「标的数 × 季度数」，非行情量级 |
| 其他仓 / 报表 / 手工脚本仍按 `fundamentals` 表名查询 | 中 | 全仓 grep `(FROM\|INTO\|UPDATE\|TABLE\|JOIN)\s+fundamentals\b` 已归零（残留命中仅为 025 自身、归档文件与未被调用的 golang-migrate 目录）；跨仓消费方不在本次可见范围 |
| 活跃表数断言散落在多份文档，易漏改 | 中 | 本次同步 ARCHITECTURE.md / AGENTS.md 两处；计数规则（内联 + 迁移，两侧同定义只计一次）已在文档中写明可复核 |

---

## Artifacts

| 动作 | 文件 | 内容 |
|---|---|---|
| **新建** | `docs/migrations/025_equitydeep_field_consolidation.sql` | 守卫式 `DO` 块：并入 + DROP + 冲突语义 + 安全性 + 014 无冲突结论 + 收口校验注释 |
| **修改** | `pkg/storage/fundamentals.go` | 4 个函数改指 `stock_fundamentals` / `ts_code`；`SaveFundamentalBatch` 的 `DO UPDATE` 补齐 12 列 |
| **修改** | `pkg/storage/postgres.go` | 内联 `migrate()` 删除 `fundamentals` 建表与两处索引；追加迁移 025 的 `DO` 块 |
| **修改** | `pkg/storage/integration_test.go` | `truncateAll` 移除 `"fundamentals"` |
| **修改** | `docs/SPEC.md` | 删除旧版 `fundamentals` 草案 DDL，原位改为指向本记录的注释 |
| **修改** | `docs/ARCHITECTURE.md` | 活跃表数 39 → 38（内联 21 → 20）；表清单移除 `fundamentals` 行；L467 C-8 备注改为已收口 |
| **修改** | `docs/archive/RESEARCH-equitydeep-legacy.md` | §3.7 C-8 行标注收口 |
| **修改** | `docs/TASKS.md` | `EQD-P3-1` → ✅；阶段 P3 `2/3` → `3/3`；迁移编号脚注去「预留 025」；版本 3.30.0 → 3.31.0；统计与变更日志 |
| **修改** | `docs/ADR.md` | ODR 索引新增 ODR-056；尾注 ODR 55 → 56、Implementation 31 → 32；index 3.11.0 → 3.12.0 |
| **修改** | `AGENTS.md` | `docs/odr/` 范围 `ODR-001~054` → `~056`；表数 39 → 38；`fundamentals` 重叠条目改为已收口 |
| **新建** | `docs/archive/odr/odr-056-fundamentals-table-consolidation.md` | 本文件 |

---

## Metrics

| 指标 | 值 |
|---|---|
| 提交 | 1 个 atomic commit（代码 + 文档 + 本记录同批，hash 见 `git log`） |
| 新建文件 | 1（迁移 SQL）+ 本 ODR |
| 修改文件 | 6（源码 3 + 文档 3，含 `docs/SPEC.md`） |
| 新增表 | 0；**删除表 1**（`fundamentals`） |
| 活跃表数 | 39 → **38**（内联 21 → 20 + 迁移新增 18） |
| 消除的冗余列 | 12（`pe` / `pb` / `ps` / `roe` / `roa` / `debt_to_equity` / `gross_margin` / `net_margin` / `revenue` / `net_profit` / `total_assets` / `total_liab`） |
| 收敛的写路径 | 2 → 1 |
| 收敛的读函数 | 4（`SaveFundamental` / `SaveFundamentalBatch` / `GetFundamental` / `GetFundamentals`） |
| 连带修复 | `SaveFundamentalBatch` `DO UPDATE` 6 列 → 12 列 |
| 残留 SQL 引用 | 0（8 处 grep 命中全部归因为：025 自身 2 处、`docs/SPEC.md` 已改、归档 1 处、未调用的 golang-migrate 目录 3 处） |
| `go build ./...` | 通过（exit 0） |
| `go test ./pkg/storage/... ./pkg/data/... ./pkg/marketdata/...` | 通过（DB-backed 用例按约定 SKIP） |
| `gofmt` | 通过（原始 `gofmt -l` 对 CRLF 文件报错，经 CRLF→LF + UTF-8 无 BOM 副本判定为行尾误报） |
| 任务增减 | 0（EQD-P3-1 由 ⬜ → ✅，总计 233 不变） |
| 阶段 P3 进度 | **3/3 全部关闭**（EQD-P1-1 ✅ + EQD-P1-2 ✅ + EQD-P3-1 ✅） |
| ADR 累计 | 22 |
| ODR 累计 | 56 |

---

## 未做项（明确排除）

| 项 | 原因 |
|---|---|
| DROP **之前**的两表同键值差异比对 | 需 Docker + PG 且需真实存量数据；本次以「口径同源」的源码取证替代抽样核对（`normalizeFundamentals` / `normalizeFundamentalsData` 同 API 同字段）。生产执行时建议先跑差异查询再接受 DROP |
| `migrations/014_add_source_columns.sql` 的 `ALTER TABLE fundamentals` | 该目录未被应用调用且不含 025，版本顺序恒早于内联 DROP ⇒ 无冲突，改动会引入无收益的跨目录不一致 |
| `docs/archive/research-2026-Q2/PHASE3-PLAN.md` 的表名引用 | 归档文件，按文档维护规则不改 |
| `SaveFundamental*` 函数名未重命名 | 函数名不含表名，重命名会波及调用方且无收益 |
| 阶段 P2 / P4 / P5 | 分别记入新的 ODR |

---

## Lessons Learned

1. **「两表重叠」的根因是「两条写入路径」，不是「两个表名」** —— 只删表不收敛写入方，重叠会在下一次数据同步时重新长出来。**收口必须同时落在 DDL 与写入代码两侧**，缺一不可。
2. **合并前先确认口径，再决定是否需要「迁移」** —— 本次取证发现两表同源于同一 API、同一字段列表、同一 `trade_date = end_date` 约定，甚至 `fundamentals.symbol` 存的就是 `ts_code` 字面量。**先证同源，才能把「数据迁移项目」降级为「12 列直通」**；若跳过这一步，就会写一堆没有必要的格式转换代码。
3. **冲突方向必须显式声明并由「谁的数据更可能更新」来定** —— `COALESCE(幸存表值, 旧表值)` 与反向写法在结果上不同，而两者都不会报错。**让旧表覆盖新值是静默的数据降级**；方向写进迁移头部注释，才能被下一次审计复核。
4. **桥接型 `UPDATE` 的列清单会随表成长而失配** —— `SaveFundamentalBatch` 的 `DO UPDATE` 停在 6 列，而表早已有 12 个指标列：**重摄取不报错，只是悄悄保留旧值**。这类缺陷不会进任何测试断言，只会在某天被「为什么数字没更新」的问题暴露。**改表加列时，必须同时 grep `DO UPDATE` 的列清单**。
5. **DROP 掉的表名会残留在文档、测试与归档里** —— 全仓 grep 出 8 处引用，其中「测试的 TRUNCATE 清单」与「SPEC 的旧版草案 DDL」是真正会出错的。**DDL 收口必须伴随一次全仓表名扫描**，并逐项判定「改 / 不改（并说明为什么）」。
6. **未被调用的迁移目录是「看不见的地雷」** —— `migrations/014` 仍对已 DROP 的表 `ADD COLUMN`，初看是执行顺序缺陷；查明 `migration_manager.go` 全仓无调用方后才确认无冲突。**结论要写进迁移注释，否则每次审计都会重排一次这个雷**。
7. **表数是文档里最容易漏改的断言** —— 它同时出现在 ARCHITECTURE.md 的标题行、AGENTS.md 的能力表与两处计数说明中。**减少表（而非增加表）时尤其容易漏**，因为历史上所有修订都是「+N」的方向。

---

_本记录为 2026-09-15 阶段 P3 EQD-P3-1 落地操作，标志阶段 P3（计算面补齐）3/3 全部关闭。阶段 P2/P4/P5 的实施情况应记入新的 ODR，不在本文件追加。_