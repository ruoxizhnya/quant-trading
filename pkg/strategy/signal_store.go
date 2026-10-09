// strategy-runtime 信号持久化（K7 切片 1）—— quant.external_signals 的落库接线。
//
// 本文件把外部模型信号落到真库，供内核侧的 SignalStrategy（Actor）按 as_of
// 拉取消费。落位理由与 state_store.go 完全一致：quant.external_signals 的 DB
// 归属是 strategy-runtime 模块（蓝图 §5 矩阵 L3 行），不走 pkg/storage 应用层
// store——「内核模块自带 store」先例（eventstore / PGStateStore 同源）。
//
// ─── 与 pkg/storage/postgres.go migrate() 的关系（本切片不做合并） ──
// 同 state_store.go：项目加表的唯一执行路径是 migrate() 内联数组。本切片把
// DDL 放本包 SignalSchemaDDL + EnsureSchema 显式调用（kernel Boot 装配时调、
// 测试 setup 时调），理由与 state_store.go 注释逐字同源（边界 / 自证）。
// 合并时机 = 后续切片（把 SignalSchemaDDL 逐字追加进 migrate() 数组，保留
// EnsureSchema 作「独立可安装」入口）。本文件 DDL 与
// contracts/kernel_modules.schema.sql 追加节逐字一致，以契约文件为准。
package strategy

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// SignalSchemaDDL 是 quant.external_signals 表 + 索引的建表语句，逐字取自
// contracts/kernel_modules.schema.sql 追加节（K7 冻结）。每条幂等
// （IF NOT EXISTS）——EnsureSchema 会被反复调用（每个测试 setup、每次装配）。
//
// 顺序有意义：schema 先于表（quant 由 strategy_state 的 DDL 保证已建，但
// 本表仍显式 CREATE SCHEMA 以防独立安装时缺失），表先于索引。
var SignalSchemaDDL = []string{
	// pkg/storage/postgres.go 现只建 ingest / research 两个 schema，quant 由
	// strategy-runtime 各 store 自行补（与 state_store.go 的 SchemaDDL 同源）。
	// 幂等，重复执行无害。
	`CREATE SCHEMA IF NOT EXISTS quant`,

	// 外部模型信号表（K7 契约冻结）。列清单：
	//   model_id  —— 外部模型标识（source）；content_hash 坐标的 source 位。
	//   symbol    —— 标的（key 位）。
	//   direction —— long/short/close（TEXT，与 quant.orders.direction 同型）。
	//   strength  —— 强度（不约束范围，Save 层拒 NaN/±Inf）。
	//   as_of     —— 信号生效交易日；「按 as_of 拉取」防前视的锚点。
	//   content_hash —— sha256 可执行字段（model_id/symbol/direction/strength/
	//                as_of），UNIQUE：幂等去重 + 可追溯坐标。
	//   factors   —— 诊断因子快照（JSONB，非身份，不入哈希）。
	`CREATE TABLE IF NOT EXISTS quant.external_signals (
		id           BIGSERIAL PRIMARY KEY,
		model_id     TEXT NOT NULL,
		symbol       TEXT NOT NULL,
		direction    TEXT NOT NULL,
		strength     DOUBLE PRECISION NOT NULL,
		as_of        TIMESTAMPTZ NOT NULL,
		content_hash TEXT NOT NULL,
		factors      JSONB,
		created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
		CONSTRAINT uq_external_signals_hash UNIQUE (content_hash)
	)`,

	// 消费侧（SignalStrategy.ListFor）的查询形态：
	// WHERE model_id = $1 AND symbol = $2 AND as_of <= $3 ORDER BY as_of。
	`CREATE INDEX IF NOT EXISTS idx_external_signals_lookup
		ON quant.external_signals (model_id, symbol, as_of)`,
}

// ensureTimeout / saveTimeout / loadTimeout：沿用 state_store.go 的同名常量
// 语义（建表一次性上限、单次 DB 操作上限）。
const (
	signalEnsureTimeout = 30 * time.Second
	signalSaveTimeout   = 10 * time.Second
	signalListTimeout   = 10 * time.Second
)

// PGSignalStore 是 quant.external_signals 的 PostgreSQL 实现（pgxpool）。
//
// 无全局单例（ADR-027）：连接池由调用方注入，典型写法是把
// pkg/storage.PostgresStore.DB() 那个 pool 传进来。进程里可以有多个实例。
type PGSignalStore struct {
	pool *pgxpool.Pool
}

// NewPGSignalStore 注入连接池构造。pool 为 nil 时返回 error（构造期失败离错误
// 最近，同 eventstore / PGStateStore 对 nil pool 的处理）。
//
// 构造函数**不**隐式建表（同 PGStateStore 先例）：构造期做 I/O 会让「连不上库」
// 伪装成「构造失败」。建表走显式 EnsureSchema。
func NewPGSignalStore(pool *pgxpool.Pool) (*PGSignalStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("strategy: PGSignalStore 连接池为 nil（注入 pkg/storage 的 pool）")
	}
	return &PGSignalStore{pool: pool}, nil
}

// EnsureSchema 幂等建立 quant schema 与 quant.external_signals 表（+索引）。
//
// 调用时机由调用方决定，构造函数不隐式建表（见 NewPGSignalStore 注释）。
func (s *PGSignalStore) EnsureSchema(ctx context.Context) error {
	ctx, cancel := mergeTimeout(ctx, signalEnsureTimeout)
	defer cancel()

	for i, ddl := range SignalSchemaDDL {
		if _, err := s.pool.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("strategy: PGSignalStore.EnsureSchema 第 %d 条 DDL 失败: %w", i+1, err)
		}
	}
	return nil
}

// saveSignalSQL —— 幂等落库一条信号。ON CONFLICT (content_hash) DO NOTHING：
// 同一可执行字段的信号（同模型同标的同 as_of 同方向同强度）只保留第一份，
// 重复注入不重复落库（content_hash 是幂等坐标）。
const saveSignalSQL = `INSERT INTO quant.external_signals
	(model_id, symbol, direction, strength, as_of, content_hash, factors)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (content_hash) DO NOTHING`

// listSignalsSQL —— 按 (model_id, symbol) 拉取 as_of <= upTo 的信号，as_of 升序。
// 「as_of <= $3」是防前视主边界：内核回放到 upTo 时未来信号物理不可见。
const listSignalsSQL = `SELECT model_id, symbol, direction, strength, as_of, factors
	FROM quant.external_signals
	WHERE model_id = $1 AND symbol = $2 AND as_of <= $3
	ORDER BY as_of`

// Save 落库一条外部信号（幂等，content_hash 冲突 DO NOTHING）。
//
// Fail-loud 拒绝（宁 Fail 不脏写）：先 ValidateExternalSignal 校验，非法输入
// 直接返回 error、不触库。校验口径见 ValidateExternalSignal（ModelID/Symbol 空、
// Direction 非 long/short/close、Strength 非有限、AsOf 零值）。
//
// Factors 为 nil 时落 NULL（JSONB 列可空），非 nil 时序列化为 JSONB。
func (s *PGSignalStore) Save(ctx context.Context, sig ExternalSignal) error {
	if err := ValidateExternalSignal(sig); err != nil {
		return err
	}

	ctx, cancel := mergeTimeout(ctx, signalSaveTimeout)
	defer cancel()

	var factors any
	if sig.Factors != nil {
		// Factors 是 map[string]float64，可直接作 JSONB 参数（pgx 对 map 类型
		// 走 JSON 编码）。nil 需显式传 nil（否则 pgx 可能报「无法编码」）。
		factors = sig.Factors
	}

	// AsOf 归一 UTC 日边界（dayOf）后落库：信号对齐以「日」为粒度（见
	// external_signal.go 的 dayOf 裁决），TIMESTAMPTZ 存 00:00 UTC。
	asOf := dayOf(sig.AsOf)

	if _, err := s.pool.Exec(ctx, saveSignalSQL,
		sig.ModelID, sig.Symbol, string(sig.Direction), sig.Strength,
		asOf, SignalContentHash(sig), factors); err != nil {
		return fmt.Errorf("strategy: PGSignalStore.Save 落库失败 (model=%s symbol=%s as_of=%s): %w",
			sig.ModelID, sig.Symbol, asOf.Format(time.RFC3339Nano), err)
	}
	return nil
}

// ListFor 读出 (modelID, symbol) 下 as_of <= upTo 的信号，as_of 升序。
//
// 未来信号（as_of > upTo）物理不可见——防前视主边界（见 listSignalsSQL 注释）。
// 无匹配信号返回空切片（不是 error）：空是合法结果，不是「找不到」的哨兵。
func (s *PGSignalStore) ListFor(ctx context.Context, modelID, symbol string, upTo time.Time) ([]ExternalSignal, error) {
	ctx, cancel := mergeTimeout(ctx, signalListTimeout)
	defer cancel()

	rows, err := s.pool.Query(ctx, listSignalsSQL, modelID, symbol, dayOf(upTo))
	if err != nil {
		return nil, fmt.Errorf("strategy: PGSignalStore.ListFor 查询失败 (model=%s symbol=%s upTo=%s): %w",
			modelID, symbol, upTo.UTC().Format(time.RFC3339Nano), err)
	}
	defer rows.Close()

	sigs := make([]ExternalSignal, 0)
	for rows.Next() {
		var (
			sig       ExternalSignal
			direction string
			factors   map[string]float64
		)
		if err := rows.Scan(&sig.ModelID, &sig.Symbol, &direction, &sig.Strength, &sig.AsOf, &factors); err != nil {
			return nil, fmt.Errorf("strategy: PGSignalStore.ListFor 扫描失败 (model=%s symbol=%s): %w", modelID, symbol, err)
		}
		sig.Direction = domainDirection(direction)
		sig.Factors = factors
		sig.AsOf = sig.AsOf.UTC()
		sigs = append(sigs, sig)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("strategy: PGSignalStore.ListFor 遍历失败 (model=%s symbol=%s): %w", modelID, symbol, err)
	}
	return sigs, nil
}

// domainDirection 把落库的 TEXT 方向还原为 domain.Direction。列值由 Save 侧
// 校验过（ValidateExternalSignal 只放行 long/short/close），这里不做二次校验，
// 直接转换——写侧的单一口径保证读侧不会遇到非法值。
func domainDirection(s string) domain.Direction {
	return domain.Direction(s)
}

// ─── 编译期合规检查 ────────────────────────────────────────────────
//
// PGSignalStore 必须满足 SignalStore，否则它不是合法的信号后端。
var _ SignalStore = (*PGSignalStore)(nil)
