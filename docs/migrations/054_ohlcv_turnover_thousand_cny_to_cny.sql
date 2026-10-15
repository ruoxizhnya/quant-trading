-- Migration 054: ohlcv_daily_qfq.turnover 单位统一（千元 → CNY）
-- Date: 2026-10-15
--
-- 定位: 规范层 schema（docs/SPEC.md「规范层 Schema」节的 OHLCV 字段表）声明
--       OHLCV.Turnover 的单位是 **CNY**，但 tushare daily 的 `amount` 字段单位是
--       **千元**，实现把它原样入库 —— 与 052 的 volume 完全同型（契约早就写对了，
--       是实现漏了一步）。本次让实现符合契约：同步侧落 CNY（pkg/data 的
--       cnyPerAmountUnit），存量按本迁移对齐。
--
-- 实测依据（2026-10-15，真库 ohlcv_daily_qfq 392.6 万行全量，非引用文档）:
--   · turnover / (volume_股 × close) 的中位数 = 0.0010127，p01 = 0.000967，
--     p99 = 0.001955，max = 0.003535，min = 0.0003226
--     ⇒ 库里存的是「元」的千分之一，即 tushare 的千元口径。
--   · 单行核对：600519.SH 2026-09-24 close=1237、volume=3123935 股、
--     turnover=3867310.92（千元）→ ×1000 = 38.67 亿元 ≈ vwap × 股数，
--     与公开披露的当日成交额量级一致。
--   · 这里**不取整**：真库实测 ×1000 后有 199,240 行偏离整数元超过 1e-3（其中 5,015 行
--     正好 0.5 元），那是源侧 amount 带 4 位小数 = 元以下的真实精度，取整会把它抹掉；
--     只有 61,699 行属于 <1e-6 的浮点噪声。052 的 volume 必须取整是因为 NT 侧
--     as_quantity 精确判整数；turnover 没有这个约束，NT 导出的 SELECT 根本不带
--     turnover（nt-sidecar/tools/export_bars.py:63）。无约束时不发明精度损失。
--
-- 影响面: ohlcv_daily_qfq 全表 turnover 列 ×1000。读到它的下游数值随之变化：
--         公式型因子的 `turnover` 字段（pkg/data/factor_formula.go）、表达式引擎的
--         `turnover` 输入（pkg/strategy/expression/data_provider.go）、分钟聚合
--         （pkg/marketdata/aggregate.go 期间合计）、市场概览展示。
--         展示侧此前不花钱：/api/market/index 的响应里根本没有 amount 字段，
--         前端 fmtAmount 拿到 undefined 显示 '-'（该缺陷另立工单，见 TASKS.md）。
--
-- 顺序要求: 必须排在 052 之后（内联 DDL 按序执行）—— 准入判据里的 volume 得已经
--           是「股」，比值才落在实测的那个量级上。
--
-- 安全性: ×1000 **不幂等**（重复执行会变 ×1000000），两道独立保险与 052 同款：
--   ① 哨兵（主）：data_migrations 里已有 054 就整个跳过；首次执行后无条件写哨兵，
--      此后 054 至多生效一次。
--   ② 内容（辅）：库里还存在「turnover 明显小于 volume × close」的行，才值得整表
--      换算 —— 防住「清库后用新代码重灌（已经是 CNY）」的场景。
--   只靠 ② 不够：迁移完成后若又落进千元数据（旧二进制没换 / 手工导入 / 混部），
--   ② 会重新成立 ⇒ 整表再 ×1000 ⇒ 已迁移的元数据静默变成 ×1000000。
--   对照实验（pkg/storage/ohlcv_turnover_migration_test.go 的临时表场景，
--   2026-10-15 实跑）：初值 {10, 80}（千元）首次迁移得 {10000, 80000}；同会话重跑
--   保持 {10000, 80000}；「元态 + 一行千元」的混合表在无哨兵时被整表 ×1000
--   （10000 → 10000000），有哨兵时一动不动。
--   ② 只回答「这张表要不要折一次」，是整表换算的**准入条件**，不承担逐行判责；
--   挡住重复放大的责任全在 ①，而 ① 的 INSERT 写在 IF **之外** —— 内容守卫不放行的
--   那次启动同样留痕，否则下次会重新评估 ②。
--   （052 那句「逐行判据会漏掉本来就是整数的行」是**它那套判据**的性质，对 turnover
--   不成立：这里逐行判据 `turnover < volume×close×0.01` 反而能抓住混合行。混合表
--   —— 迁移后又落进千元 —— 不由本迁移修复：① 已经让它永远不会被二次放大，
--   真要修得显式动手（部署纪律见 TASKS.md DATA-09），别指望这条迁移自愈。）
--   阈值 0.01 是实测两头留出的空隙（千元态 max 0.003535 在其下 2.8 倍，元态
--   min 0.3226 在其上 32 倍），不是从判据本身推出来的。
--
-- 备份: 换算前的 turnover 整列留在 ohlcv_daily_qfq_bak_054（最小恢复集，可 DROP；
--       换算可逆 —— 元 ÷ 1000 = 千元）。只在真正换算的那一次创建。
--
-- 同源副本: 实际执行路径为 pkg/storage/postgres.go 的 migrationStatements()，
--           本文件为文档副本。
-- 历史副本不动: 迁移不可改写历史，既有 0xx 文件保持原样。

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM data_migrations WHERE name = '054_ohlcv_turnover_thousand_cny_to_cny')
       AND EXISTS (SELECT 1 FROM ohlcv_daily_qfq
                   WHERE turnover > 0 AND volume > 0 AND close > 0
                     AND turnover < volume * close * 0.01) THEN
        CREATE TABLE IF NOT EXISTS ohlcv_daily_qfq_bak_054 AS
            SELECT symbol, trade_date, turnover FROM ohlcv_daily_qfq;
        UPDATE ohlcv_daily_qfq SET turnover = turnover * 1000;
    END IF;
    INSERT INTO data_migrations (name) VALUES ('054_ohlcv_turnover_thousand_cny_to_cny')
        ON CONFLICT (name) DO NOTHING;
END $$;
