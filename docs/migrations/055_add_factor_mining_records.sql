-- Migration 055: factor_mining_records（D8 因子挖掘留痕——治理层审计）
--
-- D3 把因子定义搬进库（051 factor_definitions），D4 定登记治理（053
-- factor_registrations + 审计）。D8 补上「挖出来那一步」的留痕：一个因子
-- 进了库还不够，要知道它是怎么挖出来的、过了哪些检验。
--
-- 设计要点：
--   * 一条 = 一行，(factor_name, version_hash) 主键——一个定义版本一条留痕。
--     被拒的候选也留痕（version_hash 非空即可）：「试过但没要」同样是审计
--     信息，能看出挖掘会不会在反复试同一批名字。
--   * 来源列（source_kind / generator / generation / rationale）记「谁生成、
--     第几代、依据什么」；num_trials / failed_trials 记过拟合校正的输入，
--     尝试次数**含失败**（先例 pkg/ai/validation 的 NumTrials 教训）。
--   * sandbox_hash 是沙箱凭证哈希（= 该定义的版本哈希，绑定内容指纹）；只有
--     通过沙箱才写得出。admitted=true 必须带与 version_hash 相等的凭证 ——
--     「跳过沙箱直接入库」在库侧也被拦下。
--   * 治理层两件（过拟合 / 样本外）的判定结果原样落库，便于事后复盘：
--     这条因子凭什么被放进来，或凭什么被挡在外面。
--
-- DB 兜底 CHECK：生成器非空 + 来源枚举 + 尝试次数为正且失败数不超过总次数
-- + 已入库必须带绑定该版本的沙箱凭证 + 被拒必须写明原因。绕过写入路径直接
-- INSERT 缺样，也被库拒绝（fail-closed）。
--
-- 权威 DDL 在 pkg/storage/postgres.go 的 migrationStatements() 内联数组
-- （本仓唯一执行路径）；本文件是变更记录，两者必须一致。

CREATE TABLE IF NOT EXISTS factor_mining_records (
    factor_name    VARCHAR(64)      NOT NULL,
    version_hash   CHAR(64)         NOT NULL,
    source_kind    VARCHAR(32)      NOT NULL,
    generator      TEXT             NOT NULL,
    generation     INT              NOT NULL DEFAULT 0,
    rationale      TEXT             NOT NULL DEFAULT '',
    num_trials     INT              NOT NULL,
    failed_trials  INT              NOT NULL DEFAULT 0,
    sandbox_hash   CHAR(64)         NOT NULL DEFAULT '',
    admitted       BOOLEAN          NOT NULL DEFAULT FALSE,
    reject_reason  TEXT             NOT NULL DEFAULT '',
    overfit_raw_p  DOUBLE PRECISION NOT NULL DEFAULT 0,
    overfit_adj_p  DOUBLE PRECISION NOT NULL DEFAULT 0,
    overfit_flag   BOOLEAN          NOT NULL DEFAULT FALSE,
    oos_in_sample  DOUBLE PRECISION NOT NULL DEFAULT 0,
    oos_out_sample DOUBLE PRECISION NOT NULL DEFAULT 0,
    oos_pass       BOOLEAN          NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    PRIMARY KEY (factor_name, version_hash),
    CONSTRAINT factor_mining_records_generator_present CHECK (btrim(generator) <> ''),
    CONSTRAINT factor_mining_records_source_known CHECK (
        source_kind IN ('ai_hypothesis', 'literature', 'supply_chain', 'ad_hoc')
    ),
    CONSTRAINT factor_mining_records_trials_coherent CHECK (
        num_trials > 0 AND failed_trials >= 0 AND failed_trials <= num_trials
    ),
    CONSTRAINT factor_mining_records_admitted_attested CHECK (
        NOT admitted OR sandbox_hash = version_hash
    ),
    CONSTRAINT factor_mining_records_rejected_reason CHECK (
        admitted OR btrim(reject_reason) <> ''
    )
);

CREATE INDEX IF NOT EXISTS idx_factor_mining_records_name
    ON factor_mining_records(factor_name, created_at DESC);
