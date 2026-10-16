package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// SaveOHLCV saves or updates OHLCV data to ohlcv_daily_qfq.
func (s *PostgresStore) SaveOHLCV(ctx context.Context, ohlcv *domain.OHLCV) error {
	query := `
		INSERT INTO ohlcv_daily_qfq (symbol, trade_date, open, high, low, close, volume, turnover, trade_days)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (symbol, trade_date) DO UPDATE SET
			open = EXCLUDED.open,
			high = EXCLUDED.high,
			low = EXCLUDED.low,
			close = EXCLUDED.close,
			volume = EXCLUDED.volume,
			turnover = EXCLUDED.turnover,
			trade_days = EXCLUDED.trade_days,
			created_at = NOW()
	`
	_, err := s.pool.Exec(ctx, query,
		ohlcv.Symbol, ohlcv.Date, ohlcv.Open, ohlcv.High, ohlcv.Low,
		ohlcv.Close, ohlcv.Volume, ohlcv.Turnover, ohlcv.TradeDays,
	)
	if err != nil {
		return fmt.Errorf("failed to save OHLCV: %w", err)
	}
	s.logger.Debug().Str("symbol", ohlcv.Symbol).Time("date", ohlcv.Date).Msg("OHLCV saved")
	return nil
}

// SaveOHLCVBatch saves multiple OHLCV records in a batch.
func (s *PostgresStore) SaveOHLCVBatch(ctx context.Context, records []*domain.OHLCV) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, o := range records {
		batch.Queue(`
			INSERT INTO ohlcv_daily_qfq (symbol, trade_date, open, high, low, close, volume, turnover, trade_days)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (symbol, trade_date) DO UPDATE SET
				open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
				close = EXCLUDED.close, volume = EXCLUDED.volume,
				turnover = EXCLUDED.turnover, trade_days = EXCLUDED.trade_days
		`, o.Symbol, o.Date, o.Open, o.High, o.Low, o.Close, o.Volume, o.Turnover, o.TradeDays)
	}

	results := tx.SendBatch(ctx, batch)

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return fmt.Errorf("batch insert failed at index %d: %w", i, err)
		}
	}

	if err := results.Close(); err != nil {
		return fmt.Errorf("failed to close batch results: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Info().Int("count", len(records)).Msg("Batch OHLCV saved")
	return nil
}

// GetOHLCV retrieves OHLCV data for a symbol within a date range.
func (s *PostgresStore) GetOHLCV(ctx context.Context, symbol string, startDate, endDate time.Time) ([]domain.OHLCV, error) {
	query := `
		SELECT symbol, trade_date, open, high, low, close, volume, turnover, trade_days
		FROM ohlcv_daily_qfq
		WHERE symbol = $1 AND trade_date >= $2 AND trade_date <= $3
		ORDER BY trade_date ASC
	`
	rows, err := s.pool.Query(ctx, query, symbol, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query OHLCV: %w", err)
	}
	defer rows.Close()

	var results []domain.OHLCV
	for rows.Next() {
		var o domain.OHLCV
		if err := rows.Scan(&o.Symbol, &o.Date, &o.Open, &o.High, &o.Low, &o.Close, &o.Volume, &o.Turnover, &o.TradeDays); err != nil {
			return nil, fmt.Errorf("failed to scan OHLCV row: %w", err)
		}
		results = append(results, o)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return results, nil
}

// GetClosesOn 一次取多只票在**某一天**的收盘价（P1-7）。
//
// 存在的理由：因子归因要对几百只票取两个日期的收盘价，逐票调用
// GetOHLCV 就是 2 × N 次 DB 往返（300 只票 = 600 次）。这里一次查询解决。
//
// 返回 map 里**只包含那天有行情的票** —— 查不到就是查不到（停牌、未上市、
// 已退市），调用方必须按缺失处理，不能拿 0 当价格（0 会被读成"白送"）。
func (s *PostgresStore) GetClosesOn(ctx context.Context, symbols []string, date time.Time) (map[string]float64, error) {
	out := make(map[string]float64, len(symbols))
	if len(symbols) == 0 {
		return out, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT symbol, close FROM ohlcv_daily_qfq
		 WHERE symbol = ANY($1) AND trade_date = $2`, symbols, date)
	if err != nil {
		return nil, fmt.Errorf("failed to query closes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var sym string
		var close float64
		if err := rows.Scan(&sym, &close); err != nil {
			return nil, fmt.Errorf("failed to scan close row: %w", err)
		}
		out[sym] = close
	}
	return out, rows.Err()
}

// GetTradingDays returns distinct trading days within a date range.
func (s *PostgresStore) GetTradingDays(ctx context.Context, startDate, endDate time.Time) ([]time.Time, error) {
	query := `
		SELECT DISTINCT trade_date FROM ohlcv_daily_qfq
		WHERE trade_date >= $1 AND trade_date <= $2
		ORDER BY trade_date ASC
	`
	rows, err := s.pool.Query(ctx, query, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query trading days: %w", err)
	}
	defer rows.Close()

	var days []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("failed to scan trading day: %w", err)
		}
		days = append(days, d)
	}
	return days, rows.Err()
}

// GetOHLCVForDateRange returns OHLCV data for ALL stocks within a date range.
// Used by FactorComputer to compute cross-sectional factors (momentum).
func (s *PostgresStore) GetOHLCVForDateRange(ctx context.Context, startDate, endDate time.Time) ([]domain.OHLCV, error) {
	query := `
		SELECT symbol, trade_date, open, high, low, close, volume, turnover, trade_days
		FROM ohlcv_daily_qfq
		WHERE trade_date >= $1 AND trade_date <= $2
		ORDER BY symbol ASC, trade_date ASC
	`
	rows, err := s.pool.Query(ctx, query, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query bulk OHLCV: %w", err)
	}
	defer rows.Close()

	var results []domain.OHLCV
	for rows.Next() {
		var o domain.OHLCV
		if err := rows.Scan(&o.Symbol, &o.Date, &o.Open, &o.High, &o.Low, &o.Close, &o.Volume, &o.Turnover, &o.TradeDays); err != nil {
			return nil, fmt.Errorf("failed to scan OHLCV row: %w", err)
		}
		results = append(results, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.logger.Debug().Time("start", startDate).Time("end", endDate).Int("count", len(results)).Msg("Bulk OHLCV loaded")
	return results, nil
}

// HasOHLCVData checks whether we have OHLCV data for a given symbol.
func (s *PostgresStore) HasOHLCVData(ctx context.Context, symbol string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM ohlcv_daily_qfq WHERE symbol = $1 LIMIT 1)`
	var exists bool
	if err := s.pool.QueryRow(ctx, query, symbol).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check OHLCV data: %w", err)
	}
	return exists, nil
}

// GetLatestOHLCVDate returns the most recent OHLCV trade date for a symbol.
func (s *PostgresStore) GetLatestOHLCVDate(ctx context.Context, symbol string) (time.Time, error) {
	query := `SELECT MAX(trade_date) FROM ohlcv_daily_qfq WHERE symbol = $1`
	var t *time.Time
	if err := s.pool.QueryRow(ctx, query, symbol).Scan(&t); err != nil {
		return time.Time{}, fmt.Errorf("failed to get latest OHLCV date: %w", err)
	}
	if t == nil {
		return time.Time{}, nil
	}
	return *t, nil
}

// LatestQuoteDate 返回全表最新的行情日期；表为空时返回 (nil, nil)。
//
// DATA-03：这个读数原来在 handler 里就地写 SQL，并把 `MAX(trade_date)`（date 类型）
// 扫进 `*string` —— pgx 收到的目标是 `**string`，实测直接拒
// `cannot scan date (OID 1082) in binary format into **string`，而那条 error 被丢弃。
// 查询本身成功、值也回来了，是类型映射不进去，于是 `latest_date` 在任何库里都
// 不可能出现（工单 ② 说的「这一趟没成」据此订正为更准的形状）。
//
// 两个语义分开给：`(nil, nil)` = 库里没有行情（表空），`(non-nil, nil)` = 读到日期，
// `(_, non-nil)` = 这趟没读成。原来三者被压成同一个「字段缺席」。
func (s *PostgresStore) LatestQuoteDate(ctx context.Context) (*time.Time, error) {
	var latest *time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT MAX(trade_date) FROM ohlcv_daily_qfq`).Scan(&latest); err != nil {
		return nil, fmt.Errorf("failed to get latest quote date: %w", err)
	}
	return latest, nil
}

// MarketIndexBar 是「市场概览」里一个指数的最新读数（DATA-07）。
//
// 单位口径沿用规范层已定死的值，本层一律不换算：
//   - Volume：股（DATA-04 把 ohlcv_daily_qfq.volume 从手统一成股）
//   - Turnover：CNY（DATA-05 把 turnover 从千元统一成 CNY）
//
// Turnover / PrevClose 用指针：turnover 列可空（DDL 只有 DEFAULT 0，无 NOT NULL），
// 只有一期时也没有上一期收盘。**缺失读成 0** 会让界面显示「成交额 0 元」
// 「涨跌幅 +0.00%」这两个假数字。
type MarketIndexBar struct {
	Code      string
	Close     float64
	Volume    float64
	Turnover  *float64
	PrevClose *float64
}

// LatestIndexBars 一趟取回给定代码各自的最新一期读数（含上一期收盘）。
//
// 原形状是每个代码两趟查询（最新收盘、上一收盘），且第二趟的 error 被丢弃 ——
// 收进一条窗口函数后，「取到最新但算不出涨跌幅」这种半途状态结构上不存在。
// 库里没有行的代码**不产出条目**（不是产一条零值读数）。
func (s *PostgresStore) LatestIndexBars(ctx context.Context, codes []string) ([]MarketIndexBar, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT symbol, close, volume, turnover, prev_close
		FROM (
			SELECT symbol, close, volume, turnover,
			       LEAD(close) OVER (PARTITION BY symbol ORDER BY trade_date DESC) AS prev_close,
			       ROW_NUMBER() OVER (PARTITION BY symbol ORDER BY trade_date DESC) AS rn
			FROM ohlcv_daily_qfq
			WHERE symbol = ANY($1)
		) latest
		WHERE rn = 1
		ORDER BY symbol`, codes)
	if err != nil {
		return nil, fmt.Errorf("failed to read latest index bars: %w", err)
	}
	defer rows.Close()

	bars := make([]MarketIndexBar, 0, len(codes))
	for rows.Next() {
		var bar MarketIndexBar
		if err := rows.Scan(&bar.Code, &bar.Close, &bar.Volume, &bar.Turnover, &bar.PrevClose); err != nil {
			return nil, fmt.Errorf("failed to scan index bar: %w", err)
		}
		bars = append(bars, bar)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read index bars: %w", err)
	}
	return bars, nil
}
