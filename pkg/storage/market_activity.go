package storage

// 市场活跃度观测（D5，Migration 056）——market_activity 表的读写面。
// 个股日频活跃度（换手率 / 自由流通换手率 / 量比），Tushare daily_basic 经
// tushare_daily_basic adapter 导入；DDL 的唯一真相在 postgres.go 的
// Migration 056 追加段（migrate() 按序执行）。
//
// 写入口径同宏观层（pkg/storage/macro.go）：
//   - 批量 Save 走「一个事务 + pgx.Batch」，任一行失败整批回滚；
//   - 幂等用 ON CONFLICT ... DO UPDATE（唯一键 symbol+trade_date+source）：
//     同键重导刷新取值与 as_of——上游会重述修正，静默保留旧值等于让活跃度
//     层悄悄过期。
//
// 校验口径（fail-loud）：空 symbol / 空 source / 零 trade_date / 零 as_of
// 一律拒绝；三个数值列**可空**（NULL = 上游未发布，缺失≠0），非空时必须
// 有限（NaN/Inf 拒绝——脏值不进库）。
//
// 读路径：按日期闭区间 [start, end]（PIT 语义：end 即计算日，行不会晚于
// 它——防偷看闸门的数据侧），按 (symbol, trade_date) 升序返回确定性序列。

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

// saveMarketActivitySQL —— 幂等落库一个活跃度观测。数值列走 EXCLUDED 直传
// （*float64 → NULL/DOUBLE PRECISION）：nil 落 NULL，不折 0。
const saveMarketActivitySQL = `INSERT INTO market_activity
	(symbol, trade_date, turnover_rate, turnover_rate_f, volume_ratio, source, as_of)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (symbol, trade_date, source) DO UPDATE SET
		turnover_rate   = EXCLUDED.turnover_rate,
		turnover_rate_f = EXCLUDED.turnover_rate_f,
		volume_ratio    = EXCLUDED.volume_ratio,
		as_of           = EXCLUDED.as_of`

// SaveMarketActivityBatch 批量写入活跃度观测（一个事务）。
func (s *PostgresStore) SaveMarketActivityBatch(ctx context.Context, rows []*domain.MarketActivity) error {
	if len(rows) == 0 {
		return nil
	}
	for _, r := range rows {
		if strings.TrimSpace(r.Symbol) == "" {
			return apperrors.Wrap(fmt.Errorf("symbol=%q", r.Symbol),
				apperrors.ErrCodeInvalidInput, "活跃度观测缺少身份列（symbol）", "storage.SaveMarketActivityBatch")
		}
		if strings.TrimSpace(r.Source) == "" {
			return apperrors.Wrap(fmt.Errorf("symbol=%q", r.Symbol),
				apperrors.ErrCodeInvalidInput, "活跃度观测缺少 source 溯源列", "storage.SaveMarketActivityBatch")
		}
		if r.TradeDate.IsZero() {
			return apperrors.Wrap(fmt.Errorf("symbol=%q", r.Symbol),
				apperrors.ErrCodeInvalidInput, "活跃度观测 trade_date 为零值（交易日不可缺）", "storage.SaveMarketActivityBatch")
		}
		if r.AsOf.IsZero() {
			return apperrors.Wrap(fmt.Errorf("symbol=%q", r.Symbol),
				apperrors.ErrCodeInvalidInput, "活跃度观测 as_of 为零值（导入时点不可缺）", "storage.SaveMarketActivityBatch")
		}
		if err := finiteOrNil(r.TurnoverRate, r.Symbol, "turnover_rate"); err != nil {
			return err
		}
		if err := finiteOrNil(r.TurnoverRateF, r.Symbol, "turnover_rate_f"); err != nil {
			return err
		}
		if err := finiteOrNil(r.VolumeRatio, r.Symbol, "volume_ratio"); err != nil {
			return err
		}
	}
	return s.batchExec(ctx, "market_activity", len(rows), func(b *pgx.Batch) {
		for _, r := range rows {
			// trade_date 是 DATE 列：传 "YYYY-MM-DD" 字符串，避免 timestamptz→date
			// 隐式转换受会话时区影响而漂移一天（同 SaveMacroSeriesBatch）。
			b.Queue(saveMarketActivitySQL, r.Symbol, dateOnly(r.TradeDate),
				r.TurnoverRate, r.TurnoverRateF, r.VolumeRatio, r.Source, r.AsOf)
		}
	})
}

// finiteOrNil 校验「可空数值列」：nil = 缺失（合法，落 NULL）；非 nil 必须
// 有限（NaN/Inf 是脏值，拒绝——缺失≠0，脏值≠缺失）。
func finiteOrNil(v *float64, symbol, column string) error {
	if v == nil {
		return nil
	}
	if math.IsNaN(*v) || math.IsInf(*v, 0) {
		return apperrors.Wrap(fmt.Errorf("symbol=%q %s=%v", symbol, column, *v),
			apperrors.ErrCodeInvalidInput, "活跃度观测数值列必须有限（nil=缺失，脏值拒绝）",
			"storage.SaveMarketActivityBatch")
	}
	return nil
}

// GetMarketActivityForDateRange 按交易日闭区间 [start, end] 读活跃度观测，
// 按 (symbol, trade_date) 升序返回（确定性序列，供公式因子窗口求值）。
//
// end 是 PIT 边界：SQL 层 `trade_date <= end`，晚于计算日的行结构上不可见
// ——这是「收盘后可见」声明在取数侧的执法点。空结果是合法答案。
func (s *PostgresStore) GetMarketActivityForDateRange(ctx context.Context, start, end time.Time) ([]domain.MarketActivity, error) {
	if end.Before(start) {
		return nil, apperrors.New(apperrors.ErrCodeInvalidInput, fmt.Sprintf(
			"活跃度查询 end(%s) 早于 start(%s)", dateOnly(end), dateOnly(start)))
	}
	const query = `SELECT symbol, trade_date, turnover_rate, turnover_rate_f, volume_ratio, source, as_of
		FROM market_activity
		WHERE trade_date >= $1 AND trade_date <= $2
		ORDER BY symbol, trade_date`
	rows, err := s.pool.Query(ctx, query, dateOnly(start), dateOnly(end))
	if err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
			"活跃度区间查询失败", "storage.GetMarketActivityForDateRange")
	}
	defer rows.Close()

	var out []domain.MarketActivity
	for rows.Next() {
		var m domain.MarketActivity
		// 三个数值列可空：pgx 扫描 NULL → 指针留 nil（缺失≠0 的读侧形态）。
		if err := rows.Scan(&m.Symbol, &m.TradeDate,
			&m.TurnoverRate, &m.TurnoverRateF, &m.VolumeRatio, &m.Source, &m.AsOf); err != nil {
			return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
				"活跃度结果行扫描失败", "storage.GetMarketActivityForDateRange")
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
			"活跃度结果集迭代失败", "storage.GetMarketActivityForDateRange")
	}
	return out, nil
}
