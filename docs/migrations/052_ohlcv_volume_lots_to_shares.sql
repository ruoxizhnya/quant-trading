-- Migration 052: ohlcv_daily_qfq.volume 单位统一（手 → 股）
-- Date: 2026-10-14
--
-- 定位: 规范层 schema（docs/SPEC.md「规范层 Schema」节）声明 OHLCV.Volume 的单位是
--       **股**，但 tushare daily 的 `vol` 字段单位是**手**，两者相差 ×100。这个矛盾
--       此前一直带着，且在四个地方各自以不同方式暴露：
--         · SPEC 字段表写「成交量（股）」—— 契约声明；
--         · pkg/data/source/*_adapter.go 的 schema 同样写 Unit: "share"—— 声明；
--         · 前端 web/src/utils/format.ts 的 fmtVolume 按「股」格式化
--           （fmtVolume(200000000) === '2.0亿'，即 2 亿股）—— 消费侧假设；
--         · nt-sidecar 的 as_quantity 要求整数，而「手」是两位小数 ⇒ fail-loud，
--           D1 导出路径整条断（NT catalog 因此长期导出失败）。
--       本次让**实现**符合已经写明的契约：同步侧落股（pkg/data 的 sharesPerLot），
--       存量按本迁移对齐；两侧都到位，全链路才真正只有一个口径。
--
-- 影响面: ohlcv_daily_qfq 全表（真库 392.6 万行）。下游读到 volume 数值的地方会
--         随之变化，其中两处是**修正既有量纲错位**（订单量单位是股，此前却与
--         「手」相比）：
--           · pkg/backtest/tracker 的流动性阈值 maxLiquidity = volume × 0.1
--             —— 此前偏小 100 倍，会把正常单量误判成超流动性而部分成交；
--           · pkg/execution 的 impact 滑点 orderQty/adv —— 此前偏高 100 倍
--             （默认 SlippageModel 是 "fixed"，故默认不生效）。
--         因此历史回测数值**会变**，K15 对拍基线需重跑（已在 TASKS.md 登记）。
--
-- 安全性: ×100 **不幂等**（重复执行会变 ×10000），所以连「整表换算」这个动作本身
--         都不能重复。两道独立保险：
--           ① 哨兵（主）：data_migrations 里已有 052 就整个跳过；首次执行后无条件
--              写哨兵，此后 052 至多生效一次。
--           ② 内容（辅）：库里还有非整数（= 手的小数）才值得换算——这防住「清库后
--              用新代码重灌（已经是股）」的场景（那时全表整数，跳过）。
--         **只靠 ② 不够**：② 隐含假设「非整数 ⇒ 仍是手」。迁移完成后若又落入手数据
--         （旧二进制未换 / 手工导入 / 多节点混部），② 会再次成立 ⇒ 整表再 ×100 ⇒
--         已迁移的股数据静默变成 ×10000。哨兵堵住这条路径。
--         对照实验（临时表实跑，2026-10-14）：初值 {1.5, 2000, 3.7} 首次迁移得
--         {150, 200000, 370}；再落一行手值 9.9 后跑「无哨兵」版得
--         {990, 15000, 37000, 20000000}（整表再 ×100）；加哨兵版保持
--         {9.9, 150, 370, 200000}。
--         ② **不能**写成逐行 `WHERE volume <> floor(volume)`：真库 19.24% 的存量行
--         volume 本来就是整数「手」，逐行条件会漏掉它们，得到半股半手的混合表。
--         ② 是「整表换算的准入条件」，不是 UPDATE 的 WHERE。
--         实测：×100 后真正非整数的行数为 0（最大浮点误差 4.8e-07），故换算无损。
-- 同源副本: 实际执行路径为 pkg/storage/postgres.go 的 migrationStatements()，本文件为文档副本。
-- 历史副本不动: 迁移不可改写历史，既有 0xx 文件保持原样。

-- DML 迁移的一次性标记表（本项目首个；后续同类迁移复用它，不要另立机制）。
CREATE TABLE IF NOT EXISTS data_migrations (
    name       TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM data_migrations WHERE name = '052_ohlcv_volume_lots_to_shares')
       AND EXISTS (SELECT 1 FROM ohlcv_daily_qfq WHERE volume <> floor(volume)) THEN
        UPDATE ohlcv_daily_qfq SET volume = round(volume * 100);
    END IF;
    INSERT INTO data_migrations (name) VALUES ('052_ohlcv_volume_lots_to_shares')
        ON CONFLICT (name) DO NOTHING;
END $$;
