-- ============================================================================
-- kernel_modules.schema.sql —— 模块化内核 DDL（K0 契约冻结 · 切片 1 + 切片 2）
-- ============================================================================
--
-- 冻结范围（K0 全部 8 张表 + K7 追加 1 张）：
--   * audit.message_log              —— 切片 1 冻结（下方第一段）
--   * quant.portfolio_snapshot       —— 切片 2 追加（下方第二段）
--   * quant.positions                —— 切片 2 追加
--   * quant.risk_events              —— 切片 2 追加
--   * quant.orders                   —— 切片 2 追加
--   * quant.fills                    —— 切片 2 追加
--   * quant.recon_report             —— 切片 2 追加
--   * quant.strategy_state           —— 切片 2 追加
--   * quant.external_signals         —— K7 切片 1 追加（下方第三段，外部模型信号表）
-- （表清单与归属见 docs/SPEC.md「模块化内核新表」一节，与蓝图
--   §5 模块矩阵的「DB 归属」列一致。）
--
-- 归属模块（冻结，每张表只有一个写者——禁止任何他处双写，
-- AGENTS.md 数据归属铁律）：
--   audit.message_log        → eventstore（经 msgbus 派发前钩子写入，
--                              BusTap「先记录后分发」语义，D4 拍板全量落库）
--   quant.portfolio_snapshot → portfolio
--   quant.positions          → portfolio
--   quant.risk_events        → risk-engine
--   quant.orders             → exec-engine
--   quant.fills              → exec-engine
--   quant.recon_report       → exec-engine
--   quant.strategy_state     → strategy-runtime
--   quant.external_signals   → strategy-runtime
--   （indicators 无表：状态在内存，因子缓存走 Redis；
--     exec-algo 无表：状态在内存 + 子订单落 quant.orders。）
--
-- 合并时机（冻结）：本文件是 K0 契约证据，不直接执行。K1 实施时
-- 将其并入 pkg/storage/postgres.go 的内联 migrate() DDL（项目加表的
-- 唯一执行路径，见 AGENTS.md §3 / migrations/README.md）；届时两者
-- 以本文件为准保持一致，本文件保留作为契约证据。
--
-- 依据：docs/design/kernel/target-architecture-modular-kernel.md
-- §5 模块矩阵（eventstore 行）+ §11 D4 拍板。
-- ============================================================================

-- 前置：audit schema 尚不存在（pkg/storage/postgres.go 现只建
-- ingest/research 两个 schema）。K1 合并时须一并加入此行；
-- 与 postgres.go 现有 CREATE SCHEMA IF NOT EXISTS 模式一致。
CREATE SCHEMA IF NOT EXISTS audit;

CREATE TABLE IF NOT EXISTS audit.message_log (
    id         BIGSERIAL PRIMARY KEY,
    ts         TIMESTAMPTZ NOT NULL,
    topic      TEXT NOT NULL,
    payload    JSONB NOT NULL,
    publisher  TEXT NOT NULL,
    run_id     TEXT
);

CREATE INDEX IF NOT EXISTS idx_message_log_ts ON audit.message_log (ts);
CREATE INDEX IF NOT EXISTS idx_message_log_topic_ts ON audit.message_log (topic, ts);

-- ============================================================================
-- K0 契约冻结 · 切片 2 —— quant.* 7 张表
-- ============================================================================
--
-- 依据：docs/design/kernel/target-architecture-modular-kernel.md §5 模块矩阵
-- （DB 归属列）+ docs/SPEC.md「模块化内核新表」一节。
--
-- 通用约定（冻结）：
--   * 每张业务表都带 run_id —— 一次回测 / 实盘 run 一个 id，所有落库数据
--     可归因到同一 run（回测-实盘同构的比对锚点）；
--     quant.strategy_state 例外地以 (strategy_id, run_id) 为复合主键——
--     一个 run 里可跑多个策略，状态按「策略 × run」定位；
--   * 每张表都带时间戳：业务发生时间（ts / submitted_at / as_of）与
--     落库时间（created_at / updated_at）分开记，不混为一列；
--   * 半结构化 / 明细类字段一律 JSONB（detail），结构化列只留查询与
--     告警 threshold 需要的那些；
--   * 幂等 DDL（CREATE ... IF NOT EXISTS），与 postgres.go 现有风格一致。
--
-- 合并时机（与切片 1 同）：本文件是 K0 契约证据，不直接执行。K1 实施时
-- 将其并入 pkg/storage/postgres.go 的内联 migrate() DDL（项目加表的唯一
-- 执行路径，见 AGENTS.md §3 / migrations/README.md）；届时两者以本文件
-- 为准保持一致，本文件保留作为契约证据。
-- ============================================================================

-- 前置：quant schema 尚不存在（pkg/storage/postgres.go 现只建
-- ingest/research 两个 schema，audit 由切片 1 本文件补）。K1 合并时
-- 须连同 audit 那行一并加入；与 postgres.go 现有
-- CREATE SCHEMA IF NOT EXISTS 模式一致。
CREATE SCHEMA IF NOT EXISTS quant;

-- ── portfolio：组合净值 / 持仓快照（Portfolio.Snapshot 的落点） ──────────
CREATE TABLE IF NOT EXISTS quant.portfolio_snapshot (
    id         BIGSERIAL PRIMARY KEY,
    run_id     TEXT NOT NULL,
    ts         TIMESTAMPTZ NOT NULL,      -- 快照时刻（回测=虚拟时间，实盘=墙钟）
    nav        DOUBLE PRECISION NOT NULL, -- 组合净值 = 现金 + 持仓市值
    cash       DOUBLE PRECISION NOT NULL,
    positions  JSONB NOT NULL,            -- []domain.Position 序列化
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_portfolio_snapshot_run_ts
    ON quant.portfolio_snapshot (run_id, ts);

-- ── portfolio：当前持仓（每个 run × symbol 一行，upsert 语义） ───────────
CREATE TABLE IF NOT EXISTS quant.positions (
    run_id          TEXT NOT NULL,
    symbol          TEXT NOT NULL,
    ts              TIMESTAMPTZ NOT NULL,       -- 最后更新时间
    quantity        DOUBLE PRECISION NOT NULL,
    avg_cost        DOUBLE PRECISION NOT NULL,
    current_price   DOUBLE PRECISION NOT NULL,
    market_value    DOUBLE PRECISION NOT NULL,
    unrealized_pnl  DOUBLE PRECISION NOT NULL DEFAULT 0,
    realized_pnl    DOUBLE PRECISION NOT NULL DEFAULT 0,
    weight          DOUBLE PRECISION NOT NULL DEFAULT 0,
    detail          JSONB,                      -- T+1 分仓、追踪止损等扩展状态
    PRIMARY KEY (run_id, symbol)
);

CREATE INDEX IF NOT EXISTS idx_positions_symbol ON quant.positions (symbol);

-- ── risk-engine：风控裁决记录（CheckOrder 挂点 + 止损事件） ──────────────
CREATE TABLE IF NOT EXISTS quant.risk_events (
    id          BIGSERIAL PRIMARY KEY,
    run_id      TEXT NOT NULL,
    ts          TIMESTAMPTZ NOT NULL,
    kind        TEXT NOT NULL,             -- "order_verdict" / "stop_loss" / "take_profit"
    symbol      TEXT,
    allowed     BOOLEAN,                   -- 裁决是否放行（止损类事件为 NULL）
    reason      TEXT,                      -- 拒绝原因 / 触发说明
    detail      JSONB                      -- OrderIntent + Verdict 或 StopLossEvent 明细
);

CREATE INDEX IF NOT EXISTS idx_risk_events_run_ts ON quant.risk_events (run_id, ts);
CREATE INDEX IF NOT EXISTS idx_risk_events_kind ON quant.risk_events (kind, ts);

-- ── exec-engine：订单生命周期（唯一写者 exec-engine） ────────────────────
CREATE TABLE IF NOT EXISTS quant.orders (
    id           BIGSERIAL PRIMARY KEY,
    run_id       TEXT NOT NULL,
    order_id     TEXT NOT NULL,            -- 业务订单号（回链 fills / risk_events）
    parent_id    TEXT,                     -- 拆单场景：回链父订单（exec-algo）
    symbol       TEXT NOT NULL,
    direction    TEXT NOT NULL,            -- long / short / close
    order_type   TEXT NOT NULL,            -- market / limit / stop / trailing
    quantity     DOUBLE PRECISION NOT NULL,
    price        DOUBLE PRECISION NOT NULL DEFAULT 0, -- 限价；市价单为 0
    status       TEXT NOT NULL,            -- pending/submitted/partial/filled/cancelled/rejected/expired
    submitted_at TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    detail       JSONB,                    -- 券商原始回报 / 拒绝原因等
    CONSTRAINT uq_quant_orders_order_id UNIQUE (order_id)
);

CREATE INDEX IF NOT EXISTS idx_orders_run_submitted ON quant.orders (run_id, submitted_at);
CREATE INDEX IF NOT EXISTS idx_orders_status ON quant.orders (status, submitted_at);
CREATE INDEX IF NOT EXISTS idx_orders_parent ON quant.orders (parent_id);

-- ── exec-engine：成交回报（portfolio.ApplyFill 的输入） ──────────────────
CREATE TABLE IF NOT EXISTS quant.fills (
    id         BIGSERIAL PRIMARY KEY,
    run_id     TEXT NOT NULL,
    order_id   TEXT NOT NULL,              -- 回链 quant.orders.order_id
    symbol     TEXT NOT NULL,
    side       TEXT NOT NULL,              -- long / short / close
    qty        DOUBLE PRECISION NOT NULL,  -- 成交数量（>0）
    price      DOUBLE PRECISION NOT NULL,  -- 成交均价
    ts         TIMESTAMPTZ NOT NULL,       -- 成交时间（回测=数据时间，实盘=回报时间）
    detail     JSONB                       -- 费用拆分等（由 portfolio 侧记账后回填）
);

CREATE INDEX IF NOT EXISTS idx_fills_run_ts ON quant.fills (run_id, ts);
CREATE INDEX IF NOT EXISTS idx_fills_order ON quant.fills (order_id);

-- ── exec-engine：对账差异报告（Reconciler.Reconcile 的落点） ─────────────
CREATE TABLE IF NOT EXISTS quant.recon_report (
    id         BIGSERIAL PRIMARY KEY,
    run_id     TEXT NOT NULL,
    as_of      TIMESTAMPTZ NOT NULL,       -- 对账基准时刻
    diff_count INTEGER NOT NULL DEFAULT 0, -- 差异条数（0 = 账实相符）
    detail     JSONB,                      -- 差异明细（reconciliation.Discrepancy JSON 形状）
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_recon_report_run_asof ON quant.recon_report (run_id, as_of);

-- ── strategy-runtime：L2/L3 流式策略状态（BarHandler.SaveState 的落点） ──
-- 复合主键 (strategy_id, run_id)：一个 run 可跑多个策略，状态按
-- 「策略 × run」定位；同一个 (策略, run) 只保留最新一份（upsert），
-- 历史版本留断点续跑时按 created_at 取。
CREATE TABLE IF NOT EXISTS quant.strategy_state (
    strategy_id TEXT NOT NULL,
    run_id      TEXT NOT NULL,
    ts          TIMESTAMPTZ NOT NULL,      -- 状态对应时刻（回测=虚拟时间，实盘=墙钟）
    state       BYTEA NOT NULL,            -- SaveState() 的原始字节，不解释内容
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (strategy_id, run_id)
);

CREATE INDEX IF NOT EXISTS idx_strategy_state_run ON quant.strategy_state (run_id);

-- ============================================================================
-- K7 契约冻结 · 切片 1 —— quant.external_signals（外部模型信号表）
-- ============================================================================
--
-- 依据：docs/design/kernel/target-architecture-modular-kernel.md §6.4（L3b
-- 外部模型信号注入）+ 蓝图 §5 模块矩阵 L3 行「quant.strategy_state + 信号表」
-- （信号表的 DB 归属 = strategy-runtime 模块）。
--
-- 语义（冻结）：
--   * 外部模型（进程外 ML）只写这张表，不直接下单；内核侧 SignalStrategy
--     （Actor）按 as_of 拉取消费——自由度挡在内核外；
--   * content_hash = sha256(可执行字段 model_id/symbol/direction/strength/
--     as_of 的规范 JSON)，UNIQUE 约束做幂等去重 + 可追溯坐标（对齐 AGENTS.md
--     citation = {source, dataset, key, as_of, content_hash}：source=model_id，
--     key=symbol，as_of=as_of）；
--   * as_of 是「按 as_of 拉取」防前视的锚点：消费查询 WHERE as_of <= upTo，
--     未来信号物理不可见；
--   * factors 是诊断快照（JSONB），非身份，不入 content_hash。
--
-- 合并时机（与 K0 冻结的 8 张表同）：本文件是契约证据，不直接执行。K1 实施时
-- 并入 pkg/storage/postgres.go 的内联 migrate() DDL。当前运行时的建表入口是
-- pkg/strategy/signal_store.go 的 SignalSchemaDDL（EnsureSchema 显式调用），
-- 其 DDL 与本节逐字一致，以本契约文件为准。
-- ============================================================================

-- ── strategy-runtime：外部模型信号（SignalStore.Save 的落点） ──────────────
CREATE TABLE IF NOT EXISTS quant.external_signals (
    id           BIGSERIAL PRIMARY KEY,
    model_id     TEXT NOT NULL,              -- 外部模型标识（source 位）
    symbol       TEXT NOT NULL,              -- 标的（key 位）
    direction    TEXT NOT NULL,              -- long / short / close
    strength     DOUBLE PRECISION NOT NULL,  -- 强度（Save 层拒 NaN/±Inf）
    as_of        TIMESTAMPTZ NOT NULL,       -- 信号生效交易日（防前视锚点）
    content_hash TEXT NOT NULL,              -- 可执行字段 sha256（幂等 + 可追溯）
    factors      JSONB,                      -- 诊断因子快照（非身份，不入哈希）
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_external_signals_hash UNIQUE (content_hash)
);

CREATE INDEX IF NOT EXISTS idx_external_signals_lookup
    ON quant.external_signals (model_id, symbol, as_of);

