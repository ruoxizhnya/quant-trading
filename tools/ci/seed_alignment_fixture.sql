-- K16 ① · 双跑对齐门禁的 CI fixture（合成行情，确定性可复现）。
--
-- 用途：GitHub Actions 上没有 390 万行真实行情（也不该有），但对齐门禁
-- （cmd/dualrun -gate）需要两侧读到同一份行情。本脚本生成两只合成标的
-- 的 2024 全年日线（去周末），灌进 ohlcv_daily_qfq——与真实表同一 schema
-- （Migration 0002），dualrun/export_bars 零改动消费。
--
-- 设计要点（初版正弦平滑收益、二版三频叠加的教训，2026-10-13 实测）：
--
--   1. **收益 regime 必须贴近校准样本**。K15 阈值表是拿真实低波银行股
--     （sharpe≈1.4、sortino≈2、maxDD≈-4.5%）校准的；单频正弦平滑收益会把
--     sharpe/sortino 顶到 4~11，绝对阈 0.05 在那个量级下不可理喻地紧，
--     门禁必然假红。三频叠加（0.73/1.31/2.17）把 sharpe/sortino 拉回了
--     校准 regime，但累计收益是**有界振荡**（价格在 base 附近起伏），
--     maxDD 只有 -0.7%，让 max_drawdown 的 0.5% 相对阈在小分母下失真
--     （实测 +0.73% 假红）。最终版改 **md5 拟随机游走 + 去均值钉收益**：
--     日收益随机分量由 md5(seed||i) 哈希派生（每步独立 ±2% 均匀），窗口内
--     去均值后再加固定漂移，把总收益**确定性钉在 +20%**（K15 校准样本
--     bh Go +21.26%）——收益水平、sharpe、maxDD 全部落回校准 regime。
--   2. **拟随机而非平滑**：收益路径要有「大日子」——持仓边界以大跳价为主
--     越过、边缘日才分叉，这正是 K15 真数据 matched 占比 ~80% 的形态；
--     单频正弦让每天都在整手边界上震荡，两侧 4 位小数 vs 2 位小数的舍入
--     差在**每一天**都被放大成交路径分叉（实测成交对 30 vs 108 病态不对称）。
--   3. **close 保留 4 位小数**（round(...::numeric, 4)）：K15 归因①的主
--     差异来源就是「PG 前复权浮点价 vs catalog tick 0.01 Decimal」的精度
--     差，fixture 保留 4 位小数让这条精度差真实复现（PG 存 10.1234，
--     catalog Decimal 舍到 10.12），门禁才守得住真差异；全 2 位小数的
--     fixture 会让两侧价完全一致、门禁只能测到「零差异」，对回归不设防。
--
-- 确定性：无 random()——md5 是纯确定性哈希，同一 SQL 每次产出逐字节同
-- 一数据集，配合两侧确定性腿，CI 结果稳定可复现（不 flake）。
--
-- 价格限制护栏：单日 |r| ≤ 2%（噪声）+ |mu|（去均值，~0.01%）+ 0.07%
-- （漂移）≈ 2.1% ≪ ±10% 主板涨跌停；
-- volume ≥ 160 万股/日 vs 目标仓 5 万股级，D_max=0.10 参与率不卡。
--
-- 前置：库 quant_trading（同 CI go job 的 POSTGRES_DB）。
-- 窗口 2024-01-02..2024-12-31 = dualrun 默认实验窗（bh / quarterly 两条
-- 信号流的调仓日 2024-04-01/07-01/10-08 全部落在其内）。

CREATE TABLE IF NOT EXISTS public.ohlcv_daily_qfq (
    symbol VARCHAR(20) NOT NULL,
    trade_date DATE NOT NULL,
    open DOUBLE PRECISION NOT NULL,
    high DOUBLE PRECISION NOT NULL,
    low DOUBLE PRECISION NOT NULL,
    close DOUBLE PRECISION NOT NULL,
    volume DOUBLE PRECISION NOT NULL,
    turnover DOUBLE PRECISION DEFAULT 0,
    trade_days INT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (symbol, trade_date)
);

-- 幂等：同批重灌不重复插行（与 P2-1/P2-2 ETL 幂等纪律一致）。
DELETE FROM public.ohlcv_daily_qfq
WHERE symbol IN ('000001.SZ', '600000.SH')
  AND trade_date BETWEEN '2024-01-02' AND '2024-12-31';

INSERT INTO public.ohlcv_daily_qfq
    (symbol, trade_date, open, high, low, close, volume, turnover, trade_days)
WITH days AS (
    SELECT (ts)::date AS trade_date,
           row_number() OVER (ORDER BY ts) - 1 AS i
    FROM generate_series('2024-01-02'::timestamp, '2024-12-31'::timestamp, '1 day') AS ts
    WHERE extract(isodow FROM ts) < 6
),
ret AS (
    -- 日收益随机分量 z = 0.85 × z_common(i) + 0.15 × z_ind(seed,i)，两个分量
    -- 各为 ±2% 均匀（u 由 md5 前 24 bit，bit(24) 无符号，/16777215）。
    --
    -- 为什么必须**高相关**（2026-10-13 实测教训，三段修正）：两标的若各走
    -- 独立噪声，50/50 组合会把回撤分散掉——单标的 maxDD -6~-8%（与 K15 校准
    -- 样本 -4.5% 同阶），组合只剩 -1.4%，于是 sortino 12 / calmar 29（K15 是
    -- 3.55 / 4.69），绝对阈 0.05 又变得不可理喻地紧（假红）。K15 真数据是两只
    -- **同板块银行股**（同涨同跌，分散化弱，组合 maxDD -4.55% ≈ 单标的）。
    -- 0.85/0.15 混合给两标的 ~0.97 的收益相关，组合回撤 ≈ 单标的回撤——regime
    -- 回到校准样本。独立 15% 分量保留路径微差（go_only/nt_only 分叉不消失）。
    SELECT d.trade_date, d.i, s.symbol, s.base, s.seed,
           0.85 * (0.02 * (2.0 * (('x' || substr(md5('common' || d.i::text), 1, 6))::bit(24)::int
                                  / 16777215.0) - 1.0))
             + 0.15 * (0.02 * (2.0 * (('x' || substr(md5(s.seed || d.i::text), 1, 6))::bit(24)::int
                                      / 16777215.0) - 1.0)) AS z
    FROM days d
    CROSS JOIN (VALUES ('000001.SZ', 10.0, 'a1'), ('600000.SH', 8.0, 'b2'))
        AS s(symbol, base, seed)
),
detrend AS (
    -- 窗口内 log(1+z) 的均值（确定性标量）。把随机分量去均值后，累计收益
    -- **只由 target_log 决定**，与 seed 无关——不用换 seed 撞运气。
    SELECT symbol, avg(ln(1 + z)) AS mu FROM ret GROUP BY symbol
),
px AS (
    -- price_i = base × exp(Σ (ln(1+z) − mu + target_log))，target_log = 0.003
    -- → 价格 +122%（base×2.22）。去均值使随机分量累计恰为 0，故**收盘价
    -- 水平精确钉死、与 seed 无关**。
    --
    -- 为什么是 +122% 价格 / 而不是 +20%（2026-10-13 实测教训，二段修正）：
    -- bh 策略首日建仓、末日清仓，**净利润 = 卖价 − 买价**（绝对额，非百分
    -- 收益），价格涨 20% 只贡献 ~+2% 净利润——分母仍太小。K15 校准样本是
    -- bh Go total_return = **+21.26%**，对应价格 +122% 量级（利润 21 万 /
    -- 本金 100 万）。K15 的 total_return 阈值是**相对差** ≤0.2%，而 Go/NT
    -- 净值绝对差是结构性的（PG 4 位小数 vs catalog tick 0.01，实测 ~2.3e-4）
    -- —— 相对差 = 2.3e-4 / |总收益|，收益水平必须与校准样本同量级，否则
    -- 0.372% 假红（实测）。这是「regime 贴近校准样本」纪律的**收益水平**
    -- 维度：不只是 sharpe/sortino/maxDD 要像，收益量级也要像。
    SELECT r.trade_date, r.i, r.symbol, r.base,
           r.base * exp(sum(ln(1 + r.z) - dt.mu + 0.003)
                          OVER (PARTITION BY r.symbol ORDER BY r.i)) AS price
    FROM ret r
    JOIN detrend dt USING (symbol)
)
SELECT
    p.symbol,
    p.trade_date,
    (p.price * (1 + 0.0015 * sin(p.i / 3.0)))::numeric(18,4)::float8 AS open,
    (p.price * 1.004)::numeric(18,4)::float8 AS high,
    (p.price * 0.996)::numeric(18,4)::float8 AS low,
    (p.price)::numeric(18,4)::float8 AS close,
    2000000 + 400000 * sin(0.37 * p.i) AS volume,
    (p.price * (2000000 + 400000 * sin(0.37 * p.i)))::float8 AS turnover,
    1 AS trade_days
FROM px p;
