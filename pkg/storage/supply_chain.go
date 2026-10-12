package storage

// 产业链静态数据层（P2-2，TASKS.md §五 遗留待裁决）—— quant schema 四张表的
// 读写面：sw_industry_tree / supply_chain_edge / industry_vitality /
// etf_constituent。DDL 的唯一真相在 postgres.go 的 SupplyChainSchemaDDL
// （migrate() Migration 049 执行文本）。
//
// 写入口径：
//   - 批量 Save 走「一个事务 + pgx.Batch」，任一行失败整批回滚（不做部分
//     写入——半批脏写比整批失败更难查）；
//   - 幂等用 ON CONFLICT ... DO UPDATE（唯一键即幂等键）：同键重导刷新行
//     内容与 as_of，不插新行。选 DO UPDATE 而不是 DO NOTHING，因为上游数据
//     会被修正（行业改名、权重调整、指标重述），静默保留旧值等于让静态层
//     悄悄过期；obs_date/source 这类身份列天然在唯一键里，不会被刷新覆盖。
//
// 校验口径（fail-loud，同 PGSignalStore.Save）：空身份列 / 零 as_of /
// 非有限数值一律 apperrors(INVALID_INPUT) 拒绝，脏行不进库。

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
)

// ── 查询过滤器 ────────────────────────────────────────────────────────────

// IndustryTreeFilter 行业树查询条件。Level 为 nil 表示不限层级；
// ParentCode 为 nil 表示不限父节点，非 nil（含空串=一级行业）表示精确匹配。
type IndustryTreeFilter struct {
	Level      *int
	ParentCode *string
}

// SupplyChainEdgeFilter 上下游映射查询条件。三个条件均可选，但**至少一个
// 非空**——边表是图结构、规模随映射增长，不接受无过滤全表扫描。
type SupplyChainEdgeFilter struct {
	Upstream   string
	Downstream string
	Relation   string
}

// IndustryVitalityFilter 行业景气指标查询条件。IndustryCode 必填；
// Metric / StartDate / EndDate 可选（EndDate 含当日）。
type IndustryVitalityFilter struct {
	IndustryCode string
	Metric       string
	StartDate    *time.Time
	EndDate      *time.Time
}

// ETFConstituentFilter ETF 成分查询条件。ETFCode 必填；ObsDate 为 nil 表示
// 取最新报告期（PIT：不同报告期各自成行，默认给当前有效的成分快照）。
type ETFConstituentFilter struct {
	ETFCode string
	ObsDate *time.Time
}

// ── 批量写入 ──────────────────────────────────────────────────────────────

// saveIndustryTreeSQL —— 幂等落库行业树节点。ON CONFLICT (code) DO UPDATE：
// 同 code 重导刷新名称/层级/父子/叶子标记与 as_of（上游改名即修正）。
const saveIndustryTreeSQL = `INSERT INTO quant.sw_industry_tree
	(code, parent_code, level, name, is_leaf, source, as_of, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, now())
	ON CONFLICT (code) DO UPDATE SET
		parent_code = EXCLUDED.parent_code,
		level      = EXCLUDED.level,
		name       = EXCLUDED.name,
		is_leaf    = EXCLUDED.is_leaf,
		source     = EXCLUDED.source,
		as_of      = EXCLUDED.as_of,
		updated_at = now()`

// saveSupplyChainEdgeSQL —— 幂等落库一条映射（唯一键 upstream+downstream+relation）。
const saveSupplyChainEdgeSQL = `INSERT INTO quant.supply_chain_edge
	(upstream, downstream, relation, weight, source, as_of, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, now())
	ON CONFLICT (upstream, downstream, relation) DO UPDATE SET
		weight     = EXCLUDED.weight,
		source     = EXCLUDED.source,
		as_of      = EXCLUDED.as_of,
		updated_at = now()`

// saveIndustryVitalitySQL —— 幂等落库一个景气观测（唯一键含 source：不同来源
// 的同指标各行其行；同来源重导刷新取值——指标会被重述修正）。
const saveIndustryVitalitySQL = `INSERT INTO quant.industry_vitality
	(industry_code, metric, obs_date, value, unit, source, as_of, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, now())
	ON CONFLICT (industry_code, metric, obs_date, source) DO UPDATE SET
		value      = EXCLUDED.value,
		unit       = EXCLUDED.unit,
		as_of      = EXCLUDED.as_of,
		updated_at = now()`

// saveETFConstituentSQL —— 幂等落库一个 ETF 成分观测（唯一键含 obs_date：
// 报告期是身份，调仓后新旧报告期各自成行，同报告期重导刷新权重）。
const saveETFConstituentSQL = `INSERT INTO quant.etf_constituent
	(etf_code, symbol, weight, obs_date, source, as_of, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, now())
	ON CONFLICT (etf_code, symbol, obs_date) DO UPDATE SET
		weight     = EXCLUDED.weight,
		source     = EXCLUDED.source,
		as_of      = EXCLUDED.as_of,
		updated_at = now()`

// SaveIndustryTreeBatch 批量写入申万行业树节点（一个事务，任一行失败整批回滚）。
func (s *PostgresStore) SaveIndustryTreeBatch(ctx context.Context, rows []*domain.SWIndustryNode) error {
	if len(rows) == 0 {
		return nil
	}
	for _, r := range rows {
		if r.Code == "" || r.Name == "" {
			return apperrors.Wrap(fmt.Errorf("code=%q name=%q", r.Code, r.Name),
				apperrors.ErrCodeInvalidInput, "行业树节点缺少身份列（code/name）", "storage.SaveIndustryTreeBatch")
		}
		if r.Source == "" {
			return apperrors.Wrap(fmt.Errorf("code=%q", r.Code),
				apperrors.ErrCodeInvalidInput, "行业树节点缺少 source 溯源列", "storage.SaveIndustryTreeBatch")
		}
		if r.AsOf.IsZero() {
			return apperrors.Wrap(fmt.Errorf("code=%q", r.Code),
				apperrors.ErrCodeInvalidInput, "行业树节点 as_of 为零值（观察时点不可缺）", "storage.SaveIndustryTreeBatch")
		}
		if r.Level < 1 {
			return apperrors.Wrap(fmt.Errorf("code=%q level=%d", r.Code, r.Level),
				apperrors.ErrCodeInvalidInput, "行业树节点 level 必须 >= 1", "storage.SaveIndustryTreeBatch")
		}
	}
	return s.batchExec(ctx, "sw_industry_tree", len(rows), func(b *pgx.Batch) {
		for _, r := range rows {
			b.Queue(saveIndustryTreeSQL, r.Code, r.ParentCode, r.Level, r.Name, r.IsLeaf, r.Source, r.AsOf)
		}
	})
}

// SaveSupplyChainEdgeBatch 批量写入上下游/环节映射（一个事务）。
func (s *PostgresStore) SaveSupplyChainEdgeBatch(ctx context.Context, rows []*domain.SupplyChainEdge) error {
	if len(rows) == 0 {
		return nil
	}
	for _, r := range rows {
		if r.Upstream == "" || r.Downstream == "" || r.Relation == "" {
			return apperrors.Wrap(fmt.Errorf("upstream=%q downstream=%q relation=%q", r.Upstream, r.Downstream, r.Relation),
				apperrors.ErrCodeInvalidInput, "上下游映射缺少身份列（upstream/downstream/relation）", "storage.SaveSupplyChainEdgeBatch")
		}
		if r.Source == "" {
			return apperrors.Wrap(fmt.Errorf("upstream=%q downstream=%q", r.Upstream, r.Downstream),
				apperrors.ErrCodeInvalidInput, "上下游映射缺少 source 溯源列", "storage.SaveSupplyChainEdgeBatch")
		}
		if r.AsOf.IsZero() {
			return apperrors.Wrap(fmt.Errorf("upstream=%q downstream=%q", r.Upstream, r.Downstream),
				apperrors.ErrCodeInvalidInput, "上下游映射 as_of 为零值（观察时点不可缺）", "storage.SaveSupplyChainEdgeBatch")
		}
		if math.IsNaN(r.Weight) || math.IsInf(r.Weight, 0) {
			return apperrors.Wrap(fmt.Errorf("upstream=%q downstream=%q weight=%v", r.Upstream, r.Downstream, r.Weight),
				apperrors.ErrCodeInvalidInput, "上下游映射 weight 必须有限", "storage.SaveSupplyChainEdgeBatch")
		}
	}
	return s.batchExec(ctx, "supply_chain_edge", len(rows), func(b *pgx.Batch) {
		for _, r := range rows {
			b.Queue(saveSupplyChainEdgeSQL, r.Upstream, r.Downstream, r.Relation, r.Weight, r.Source, r.AsOf)
		}
	})
}

// SaveIndustryVitalityBatch 批量写入行业景气指标观测（一个事务）。
func (s *PostgresStore) SaveIndustryVitalityBatch(ctx context.Context, rows []*domain.IndustryVitality) error {
	if len(rows) == 0 {
		return nil
	}
	for _, r := range rows {
		if r.IndustryCode == "" || r.Metric == "" {
			return apperrors.Wrap(fmt.Errorf("industry_code=%q metric=%q", r.IndustryCode, r.Metric),
				apperrors.ErrCodeInvalidInput, "景气指标缺少身份列（industry_code/metric）", "storage.SaveIndustryVitalityBatch")
		}
		if r.Source == "" {
			return apperrors.Wrap(fmt.Errorf("industry_code=%q metric=%q", r.IndustryCode, r.Metric),
				apperrors.ErrCodeInvalidInput, "景气指标缺少 source 溯源列", "storage.SaveIndustryVitalityBatch")
		}
		if r.ObsDate.IsZero() {
			return apperrors.Wrap(fmt.Errorf("industry_code=%q metric=%q", r.IndustryCode, r.Metric),
				apperrors.ErrCodeInvalidInput, "景气指标 obs_date 为零值（观察日不可缺）", "storage.SaveIndustryVitalityBatch")
		}
		if math.IsNaN(r.Value) || math.IsInf(r.Value, 0) {
			return apperrors.Wrap(fmt.Errorf("industry_code=%q metric=%q value=%v", r.IndustryCode, r.Metric, r.Value),
				apperrors.ErrCodeInvalidInput, "景气指标 value 必须有限", "storage.SaveIndustryVitalityBatch")
		}
	}
	return s.batchExec(ctx, "industry_vitality", len(rows), func(b *pgx.Batch) {
		for _, r := range rows {
			// obs_date 是 DATE 列：传 "YYYY-MM-DD" 字符串，避免 timestamptz→date
			// 隐式转换受会话时区影响而漂移一天。
			b.Queue(saveIndustryVitalitySQL, r.IndustryCode, r.Metric, dateOnly(r.ObsDate), r.Value, r.Unit, r.Source, r.AsOf)
		}
	})
}

// SaveETFConstituentBatch 批量写入 ETF 成分观测（一个事务）。
func (s *PostgresStore) SaveETFConstituentBatch(ctx context.Context, rows []*domain.ETFConstituent) error {
	if len(rows) == 0 {
		return nil
	}
	for _, r := range rows {
		if r.ETFCode == "" || r.Symbol == "" {
			return apperrors.Wrap(fmt.Errorf("etf_code=%q symbol=%q", r.ETFCode, r.Symbol),
				apperrors.ErrCodeInvalidInput, "ETF 成分缺少身份列（etf_code/symbol）", "storage.SaveETFConstituentBatch")
		}
		if r.Source == "" {
			return apperrors.Wrap(fmt.Errorf("etf_code=%q symbol=%q", r.ETFCode, r.Symbol),
				apperrors.ErrCodeInvalidInput, "ETF 成分缺少 source 溯源列", "storage.SaveETFConstituentBatch")
		}
		if r.ObsDate.IsZero() {
			return apperrors.Wrap(fmt.Errorf("etf_code=%q symbol=%q", r.ETFCode, r.Symbol),
				apperrors.ErrCodeInvalidInput, "ETF 成分 obs_date 为零值（报告期不可缺）", "storage.SaveETFConstituentBatch")
		}
		if math.IsNaN(r.Weight) || math.IsInf(r.Weight, 0) {
			return apperrors.Wrap(fmt.Errorf("etf_code=%q symbol=%q weight=%v", r.ETFCode, r.Symbol, r.Weight),
				apperrors.ErrCodeInvalidInput, "ETF 成分 weight 必须有限", "storage.SaveETFConstituentBatch")
		}
	}
	return s.batchExec(ctx, "etf_constituent", len(rows), func(b *pgx.Batch) {
		for _, r := range rows {
			b.Queue(saveETFConstituentSQL, r.ETFCode, r.Symbol, r.Weight, dateOnly(r.ObsDate), r.Source, r.AsOf)
		}
	})
}

// batchExec 是四张表共用的批量写入骨架：一个事务 + pgx.Batch，逐条取结果，
// 任一条失败即整体回滚（半批脏写比整批失败更难查）。
func (s *PostgresStore) batchExec(ctx context.Context, table string, n int, queue func(b *pgx.Batch)) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for %s: %w", table, err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	queue(batch)

	results := tx.SendBatch(ctx, batch)
	for i := 0; i < n; i++ {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return fmt.Errorf("batch %s insert failed at index %d: %w", table, i, err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("failed to close batch results for %s: %w", table, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit %s batch: %w", table, err)
	}
	s.logger.Info().Str("table", table).Int("count", n).Msg("Supply chain batch saved")
	return nil
}

// ── 只读查询 ──────────────────────────────────────────────────────────────

// listIndustryTreeSQL 的 WHERE 由过滤条件动态拼装（参数化占位符）。
const listIndustryTreeSQL = `SELECT code, parent_code, level, name, is_leaf, source, as_of
	FROM quant.sw_industry_tree`

// ListIndustryTree 按条件读行业树节点（Level/ParentCode 可选），按 code 排序。
// 空结果是合法答案：返回空切片而非 error。
func (s *PostgresStore) ListIndustryTree(ctx context.Context, f IndustryTreeFilter) ([]domain.SWIndustryNode, error) {
	var (
		cond []string
		args []any
	)
	if f.Level != nil {
		args = append(args, *f.Level)
		cond = append(cond, fmt.Sprintf("level = $%d", len(args)))
	}
	if f.ParentCode != nil {
		args = append(args, *f.ParentCode)
		cond = append(cond, fmt.Sprintf("parent_code = $%d", len(args)))
	}
	query := listIndustryTreeSQL
	if len(cond) > 0 {
		query += " WHERE " + strings.Join(cond, " AND ")
	}
	query += " ORDER BY code"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query sw_industry_tree: %w", err)
	}
	defer rows.Close()

	var out []domain.SWIndustryNode
	for rows.Next() {
		var n domain.SWIndustryNode
		if err := rows.Scan(&n.Code, &n.ParentCode, &n.Level, &n.Name, &n.IsLeaf, &n.Source, &n.AsOf); err != nil {
			return nil, fmt.Errorf("failed to scan sw_industry_tree row: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// listSupplyChainEdgesSQL 的 WHERE 由过滤条件动态拼装。
const listSupplyChainEdgesSQL = `SELECT upstream, downstream, relation, weight, source, as_of
	FROM quant.supply_chain_edge`

// ListSupplyChainEdges 按条件读上下游映射。三个过滤条件至少一个非空——
// 边表是图结构，不接受无过滤全表扫描（fail-loud，防慢查询拖垮连接池）。
func (s *PostgresStore) ListSupplyChainEdges(ctx context.Context, f SupplyChainEdgeFilter) ([]domain.SupplyChainEdge, error) {
	if f.Upstream == "" && f.Downstream == "" && f.Relation == "" {
		// 注意用 New 而不是 Wrap(nil, ...)：后者返回 nil *AppError，作为 error
		// 接口返回时是「非 nil 接口包 nil 指针」，调用方 errors.As 命中后解引用
		// 直接 panic（本测试族踩过，见 RoundtripFiltersAndIdempotency）。
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput,
			"上下游映射查询至少需要一个过滤条件（upstream/downstream/relation）")
	}
	var (
		cond []string
		args []any
	)
	for _, c := range []struct {
		name string
		val  string
	}{
		{"upstream", f.Upstream},
		{"downstream", f.Downstream},
		{"relation", f.Relation},
	} {
		if c.val != "" {
			args = append(args, c.val)
			cond = append(cond, fmt.Sprintf("%s = $%d", c.name, len(args)))
		}
	}
	query := listSupplyChainEdgesSQL + " WHERE " + strings.Join(cond, " AND ") + " ORDER BY upstream, downstream"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query supply_chain_edge: %w", err)
	}
	defer rows.Close()

	var out []domain.SupplyChainEdge
	for rows.Next() {
		var e domain.SupplyChainEdge
		if err := rows.Scan(&e.Upstream, &e.Downstream, &e.Relation, &e.Weight, &e.Source, &e.AsOf); err != nil {
			return nil, fmt.Errorf("failed to scan supply_chain_edge row: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// listIndustryVitalitySQL 的 WHERE 由过滤条件动态拼装。
const listIndustryVitalitySQL = `SELECT industry_code, metric, obs_date, value, unit, source, as_of
	FROM quant.industry_vitality`

// ListIndustryVitality 按行业+指标+日期区间读景气观测，按 (obs_date, metric)
// 排序。EndDate 含当日。空结果是合法答案。
func (s *PostgresStore) ListIndustryVitality(ctx context.Context, f IndustryVitalityFilter) ([]domain.IndustryVitality, error) {
	if f.IndustryCode == "" {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput,
			"景气指标查询 industry_code 必填")
	}
	args := []any{f.IndustryCode}
	cond := []string{"industry_code = $1"}
	if f.Metric != "" {
		args = append(args, f.Metric)
		cond = append(cond, fmt.Sprintf("metric = $%d", len(args)))
	}
	if f.StartDate != nil {
		args = append(args, dateOnly(*f.StartDate))
		cond = append(cond, fmt.Sprintf("obs_date >= $%d", len(args)))
	}
	if f.EndDate != nil {
		args = append(args, dateOnly(*f.EndDate))
		cond = append(cond, fmt.Sprintf("obs_date <= $%d", len(args)))
	}
	query := listIndustryVitalitySQL + " WHERE " + strings.Join(cond, " AND ") + " ORDER BY obs_date, metric"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query industry_vitality: %w", err)
	}
	defer rows.Close()

	var out []domain.IndustryVitality
	for rows.Next() {
		var v domain.IndustryVitality
		if err := rows.Scan(&v.IndustryCode, &v.Metric, &v.ObsDate, &v.Value, &v.Unit, &v.Source, &v.AsOf); err != nil {
			return nil, fmt.Errorf("failed to scan industry_vitality row: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// listETFConstituentsLatestSQL —— 最新报告期成分（PIT 默认口径）。
const listETFConstituentsLatestSQL = `SELECT etf_code, symbol, weight, obs_date, source, as_of
	FROM quant.etf_constituent
	WHERE etf_code = $1
	  AND obs_date = (SELECT MAX(obs_date) FROM quant.etf_constituent WHERE etf_code = $1)
	ORDER BY weight DESC, symbol`

// listETFConstituentsByDateSQL —— 指定报告期成分。
const listETFConstituentsByDateSQL = `SELECT etf_code, symbol, weight, obs_date, source, as_of
	FROM quant.etf_constituent
	WHERE etf_code = $1 AND obs_date = $2
	ORDER BY weight DESC, symbol`

// ListETFConstituents 读 ETF 成分。ObsDate 为 nil 时取最新报告期（调仓后
// 旧报告期数据仍在库里，按需显式指定）。空结果是合法答案。
func (s *PostgresStore) ListETFConstituents(ctx context.Context, f ETFConstituentFilter) ([]domain.ETFConstituent, error) {
	if f.ETFCode == "" {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput,
			"ETF 成分查询 etf_code 必填")
	}

	var (
		query string
		args  []any
	)
	if f.ObsDate != nil {
		query = listETFConstituentsByDateSQL
		args = []any{f.ETFCode, dateOnly(*f.ObsDate)}
	} else {
		query = listETFConstituentsLatestSQL
		args = []any{f.ETFCode}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query etf_constituent: %w", err)
	}
	defer rows.Close()

	var out []domain.ETFConstituent
	for rows.Next() {
		var c domain.ETFConstituent
		if err := rows.Scan(&c.ETFCode, &c.Symbol, &c.Weight, &c.ObsDate, &c.Source, &c.AsOf); err != nil {
			return nil, fmt.Errorf("failed to scan etf_constituent row: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// dateOnly 把时间归一到 "YYYY-MM-DD"（UTC）——DATE 列的统一入口：
// 写入传字符串避免 timestamptz→date 隐式转换受会话时区影响；读回时 pgx 给
// 的是 UTC 零点 time.Time，两边口径一致。
func dateOnly(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}
