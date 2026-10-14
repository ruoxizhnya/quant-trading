-- Migration 051: factor_definitions（D3 公式化因子引擎——定义入库）
--
-- 「库里公式」的库：因子定义（名字/公式/输入字段/版本哈希/生效日/口径/
-- 前视纪律）的持久形态。ADR-031 蓝图 §5.2/§6 三期①：因子定义从 Go 代码
-- 改为库里公式，因子名注册表从「代码冻结」改「库管理」。
--
-- 设计要点：
--   * version_hash 不由调用方提供——它由定义内容派生
--     （domain.FactorDefinition.VersionHash = canonical JSON 的 SHA-256）。
--     写入时以派生值为准；加载时重新 derive 并与库内值比对，不符即拒绝
--     （fail-closed）。「库里哈希字段」没有独立存储入口可被篡改。
--   * effective_from 是 VARCHAR(10) 而非 DATE：契约是 "YYYY-MM-DD"
--     字符串，格式校验在 Go 侧（time.Parse），DB 不做日期语义。
--   * input_fields 是 TEXT[]：规范层字段名（D1 字段词表 / expression
--     fieldRegistry 合法名 / equitydeep 契约字段码）。
--   * kernel 四态：leaf（字段直取）/ formula（叶子+算子表达式）/
--     code（Go 出口，code_ref 指向绑定）/ pending（名册在册、内核待定
--     ——size/volatility/growth 三个通用因子的现状，不发明数学定义）。
--
-- 权威 DDL 在 pkg/storage/postgres.go 的 migrationStatements() 内联数组
-- （本仓唯一执行路径）；本文件是变更记录，两者必须一致。

CREATE TABLE IF NOT EXISTS factor_definitions (
    factor_name    VARCHAR(64) PRIMARY KEY,
    kernel         VARCHAR(16) NOT NULL,
    formula        TEXT NOT NULL DEFAULT '',
    validity       TEXT NOT NULL DEFAULT '',
    code_ref       VARCHAR(128) NOT NULL DEFAULT '',
    caliber        VARCHAR(16) NOT NULL,
    input_fields   TEXT[] NOT NULL DEFAULT '{}',
    effective_from VARCHAR(10) NOT NULL,
    lookahead      VARCHAR(64) NOT NULL,
    lookback       INT NOT NULL DEFAULT 0,
    version_hash   CHAR(64) NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
