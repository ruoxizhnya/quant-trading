package storage

// 宏观指标序列（P2-1，Migration 050 / MacroSeriesSchemaDDL）——
// quant.macro_series 的读写面。国家层宏观观测（CPI/PPI/PMI/M2/社融/LPR/
// 汇率），与 industry_vitality（049，行业景气）并列不重复。DDL 的唯一真相
// 在 postgres.go 的 MacroSeriesSchemaDDL（migrate() Migration 050 执行文本）。
//
// 写入口径同产业链层（pkg/storage/supply_chain.go）：
//   - 批量 Save 走「一个事务 + pgx.Batch」，任一行失败整批回滚（不做部分
//     写入——半批脏写比整批失败更难查）；
//   - 幂等用 ON CONFLICT ... DO UPDATE（唯一键即幂等键）：同键重导刷新行
//     内容与 as_of，不插新行。选 DO UPDATE 而不是 DO NOTHING，因为上游数据
//     会被重述修正，静默保留旧值等于让宏观层悄悄过期。
//
// 校验口径（fail-loud，同 PGSignalStore.Save）：空身份列 / 零 as_of /
// 非有限数值一律 apperrors(INVALID_INPUT) 拒绝，脏行不进库。
//
// 读路径（R4/R5，P2-1-R2）：按 obs_date 降序 + LIMIT 护栏（默认 1000 /
// 上限 5000），查询/扫描/迭代错误一律 apperrors(INTERNAL) 包装。

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

// MacroSeriesFilter 宏观指标查询条件。Indicator 必填（指标目录见
// pkg/data/macro.go 的 macroIndicatorSpecs）；Freq 可选（M/D，不传则不限
// 频率）；StartDate / EndDate 为闭区间（EndDate 含当日）；Limit 为返回
// 行数上限（0 = 取 MacroSeriesDefaultLimit；负数或超 MacroSeriesMaxLimit
// → INVALID_INPUT）。
type MacroSeriesFilter struct {
	Indicator string
	Freq      string
	StartDate *time.Time
	EndDate   *time.Time
	Limit     int
}

// 查询行数护栏（R4，P2-1-R2）：单指标最大序列约 2400 行（USDCNY_CLOSE，
// 日频每年约 +250 行且无界），默认 1000 已覆盖常用窗口，上限 5000 防
// 无界大查询。配合 ORDER BY obs_date DESC，截断只丢最旧的行。
const (
	MacroSeriesDefaultLimit = 1000
	MacroSeriesMaxLimit     = 5000
)

// saveMacroSeriesSQL —— 幂等落库一个宏观观测（唯一键含 source：不同来源的
// 同指标各行其行；同来源重导刷新取值——指标会被重述修正）。
const saveMacroSeriesSQL = `INSERT INTO quant.macro_series
	(indicator, freq, obs_date, value, unit, source, as_of, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, now())
	ON CONFLICT (indicator, freq, obs_date, source) DO UPDATE SET
		value      = EXCLUDED.value,
		unit       = EXCLUDED.unit,
		as_of      = EXCLUDED.as_of,
		updated_at = now()`

// SaveMacroSeriesBatch 批量写入宏观指标观测（一个事务）。
func (s *PostgresStore) SaveMacroSeriesBatch(ctx context.Context, rows []*domain.MacroSeries) error {
	if len(rows) == 0 {
		return nil
	}
	for _, r := range rows {
		if r.Indicator == "" {
			return apperrors.Wrap(fmt.Errorf("indicator=%q", r.Indicator),
				apperrors.ErrCodeInvalidInput, "宏观指标缺少身份列（indicator）", "storage.SaveMacroSeriesBatch")
		}
		if r.Freq == "" {
			return apperrors.Wrap(fmt.Errorf("indicator=%q", r.Indicator),
				apperrors.ErrCodeInvalidInput, "宏观指标缺少频率列（freq）", "storage.SaveMacroSeriesBatch")
		}
		if r.Source == "" {
			return apperrors.Wrap(fmt.Errorf("indicator=%q", r.Indicator),
				apperrors.ErrCodeInvalidInput, "宏观指标缺少 source 溯源列", "storage.SaveMacroSeriesBatch")
		}
		if r.ObsDate.IsZero() {
			return apperrors.Wrap(fmt.Errorf("indicator=%q", r.Indicator),
				apperrors.ErrCodeInvalidInput, "宏观指标 obs_date 为零值（观察日不可缺）", "storage.SaveMacroSeriesBatch")
		}
		if r.AsOf.IsZero() {
			return apperrors.Wrap(fmt.Errorf("indicator=%q", r.Indicator),
				apperrors.ErrCodeInvalidInput, "宏观指标 as_of 为零值（观察时点不可缺）", "storage.SaveMacroSeriesBatch")
		}
		if math.IsNaN(r.Value) || math.IsInf(r.Value, 0) {
			return apperrors.Wrap(fmt.Errorf("indicator=%q value=%v", r.Indicator, r.Value),
				apperrors.ErrCodeInvalidInput, "宏观指标 value 必须有限", "storage.SaveMacroSeriesBatch")
		}
	}
	return s.batchExec(ctx, "macro_series", len(rows), func(b *pgx.Batch) {
		for _, r := range rows {
			// obs_date 是 DATE 列：传 "YYYY-MM-DD" 字符串，避免 timestamptz→date
			// 隐式转换受会话时区影响而漂移一天（同 SaveIndustryVitalityBatch）。
			b.Queue(saveMacroSeriesSQL, r.Indicator, r.Freq, dateOnly(r.ObsDate), r.Value, r.Unit, r.Source, r.AsOf)
		}
	})
}

// listMacroSeriesSQL 的 WHERE 与 ORDER/LIMIT 由过滤条件动态拼装（参数化占位符）。
const listMacroSeriesSQL = `SELECT indicator, freq, obs_date, value, unit, source, as_of
	FROM quant.macro_series`

// ListMacroSeries 按指标+频率+日期区间读宏观观测，按 obs_date **降序**
// （最新在前）返回，行数受 f.Limit 限制（0 → MacroSeriesDefaultLimit，
// 上限 MacroSeriesMaxLimit；越界 fail-loud）。
//
// DESC + LIMIT 是刻意组合（R4，P2-1-R2）：截断只丢最旧的行，客户端拿到
// 的永远是最近窗口；要更旧的数据用 StartDate 显式下钻。EndDate 含当日。
// 空结果是合法答案：返回空切片而非 error。
func (s *PostgresStore) ListMacroSeries(ctx context.Context, f MacroSeriesFilter) ([]domain.MacroSeries, error) {
	if f.Indicator == "" {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput,
			"宏观指标查询 indicator 必填")
	}
	if f.Limit < 0 || f.Limit > MacroSeriesMaxLimit {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput, fmt.Sprintf(
			"宏观指标查询 limit 越界：%d（允许 1..%d；0 表示默认 %d）",
			f.Limit, MacroSeriesMaxLimit, MacroSeriesDefaultLimit))
	}
	limit := f.Limit
	if limit == 0 {
		limit = MacroSeriesDefaultLimit
	}
	args := []any{f.Indicator}
	cond := []string{"indicator = $1"}
	if f.Freq != "" {
		args = append(args, f.Freq)
		cond = append(cond, fmt.Sprintf("freq = $%d", len(args)))
	}
	if f.StartDate != nil {
		args = append(args, dateOnly(*f.StartDate))
		cond = append(cond, fmt.Sprintf("obs_date >= $%d", len(args)))
	}
	if f.EndDate != nil {
		args = append(args, dateOnly(*f.EndDate))
		cond = append(cond, fmt.Sprintf("obs_date <= $%d", len(args)))
	}
	args = append(args, limit)
	query := listMacroSeriesSQL + " WHERE " + strings.Join(cond, " AND ") +
		fmt.Sprintf(" ORDER BY obs_date DESC LIMIT $%d", len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
			"宏观指标查询失败", "storage.ListMacroSeries")
	}
	defer rows.Close()

	var out []domain.MacroSeries
	for rows.Next() {
		var m domain.MacroSeries
		if err := rows.Scan(&m.Indicator, &m.Freq, &m.ObsDate, &m.Value, &m.Unit, &m.Source, &m.AsOf); err != nil {
			return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
				"宏观指标结果行扫描失败", "storage.ListMacroSeries")
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
			"宏观指标结果集迭代失败", "storage.ListMacroSeries")
	}
	return out, nil
}
