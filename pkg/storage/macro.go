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
// 频率）；StartDate / EndDate 为闭区间（EndDate 含当日）。
type MacroSeriesFilter struct {
	Indicator string
	Freq      string
	StartDate *time.Time
	EndDate   *time.Time
}

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

// listMacroSeriesSQL 的 WHERE 由过滤条件动态拼装（参数化占位符）。
const listMacroSeriesSQL = `SELECT indicator, freq, obs_date, value, unit, source, as_of
	FROM quant.macro_series`

// ListMacroSeries 按指标+频率+日期区间读宏观观测，按 obs_date 排序。
// EndDate 含当日。空结果是合法答案：返回空切片而非 error。
func (s *PostgresStore) ListMacroSeries(ctx context.Context, f MacroSeriesFilter) ([]domain.MacroSeries, error) {
	if f.Indicator == "" {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput,
			"宏观指标查询 indicator 必填")
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
	query := listMacroSeriesSQL + " WHERE " + strings.Join(cond, " AND ") + " ORDER BY obs_date"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query macro_series: %w", err)
	}
	defer rows.Close()

	var out []domain.MacroSeries
	for rows.Next() {
		var m domain.MacroSeries
		if err := rows.Scan(&m.Indicator, &m.Freq, &m.ObsDate, &m.Value, &m.Unit, &m.Source, &m.AsOf); err != nil {
			return nil, fmt.Errorf("failed to scan macro_series row: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
