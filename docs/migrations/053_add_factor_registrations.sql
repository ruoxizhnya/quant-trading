-- Migration 053: factor_registrations + factor_registration_audit（D4 因子注册表治理细则）
--
-- D3 把因子定义从 Go 代码搬进库（Migration 051 的 factor_definitions），
-- D4 定「进库之后的纪律」：登记 / 生效 / 回滚。本迁移落治理账本与审计留痕。
--
-- 设计要点：
--   * 一条登记 = 一行，(factor_name, version_hash) 主键——同一因子可留多版
--     （改公式 = 新哈希 = 新行）；definition 存 JSONB 快照，让回滚后的老版本
--     **自包含可复现**（不依赖别处还留着旧定义对象）。
--   * 四样齐全（公式 / 哈希 / 登记人 / 生效日）由两道关卡守住：Go 写入路径
--     过 domain.FactorRegistration.Validate；DB 侧用 CHECK 兜底——registrar
--     非空、内核体存在（formula 或 code_ref 非空）、状态枚举。绕过写入路径
--     直接 INSERT 缺样，也被库拒绝（fail-closed）。
--   * 回滚只改 status='rolled_back' 并留痕（谁/何时/为什么）。新引用不再命中
--     该版本；历史引用按 version_hash 锁定仍能命中（可复现性红线）。
--   * 审计留痕单列一表（append-only，按 id 排序）：register / rollback 两个
--     动作都写一条（谁、何时、改了什么、为什么）。
--
-- 权威 DDL 在 pkg/storage/postgres.go 的 migrationStatements() 内联数组
-- （本仓唯一执行路径）；本文件是变更记录，两者必须一致。

CREATE TABLE IF NOT EXISTS factor_registrations (
    factor_name     VARCHAR(64)  NOT NULL,
    version_hash    CHAR(64)     NOT NULL,
    definition      JSONB        NOT NULL,
    registrar       TEXT         NOT NULL,
    status          VARCHAR(16)  NOT NULL DEFAULT 'active',
    registered_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    rolled_back_at  TIMESTAMPTZ,
    rolled_back_by  TEXT         NOT NULL DEFAULT '',
    rollback_reason TEXT         NOT NULL DEFAULT '',
    PRIMARY KEY (factor_name, version_hash),
    CONSTRAINT factor_registrations_registrar_present CHECK (btrim(registrar) <> ''),
    CONSTRAINT factor_registrations_status_known CHECK (status IN ('active', 'rolled_back')),
    CONSTRAINT factor_registrations_kernel_body_present CHECK (
        COALESCE(definition ->> 'kernel', '') IN ('leaf', 'formula', 'code')
        AND (COALESCE(definition ->> 'formula', '') <> '' OR COALESCE(definition ->> 'code_ref', '') <> '')
    )
);

CREATE INDEX IF NOT EXISTS idx_factor_registrations_status
    ON factor_registrations(factor_name, status);

CREATE TABLE IF NOT EXISTS factor_registration_audit (
    id           BIGSERIAL    PRIMARY KEY,
    factor_name  VARCHAR(64)  NOT NULL,
    version_hash CHAR(64)     NOT NULL,
    action       VARCHAR(16)  NOT NULL,
    actor        TEXT         NOT NULL,
    reason       TEXT         NOT NULL DEFAULT '',
    detail       TEXT         NOT NULL DEFAULT '',
    audited_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT factor_registration_audit_action_known CHECK (action IN ('register', 'rollback'))
);

CREATE INDEX IF NOT EXISTS idx_factor_registration_audit_name
    ON factor_registration_audit(factor_name, audited_at DESC);
