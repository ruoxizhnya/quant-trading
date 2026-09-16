package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// GetFundamentalsSnapshot returns latest fundamental data for all stocks as of a cutoff date.
// Used by FactorComputer to compute value/quality factors cross-sectionally.
//
// PIT（point-in-time）语义 —— 2026-09-16 修复前视偏差，见 TASKS P0-1
// 与 pkg/storage/fundamentals_pit_test.go 的回归测试。
//
// 一条记录的「可用日」= COALESCE(ann_date, trade_date)。原因：
//   - fina_indicator 路径（pkg/data/tushare.go:457 注释 "Use end_date as trade_date"）
//     把报告期截止日 end_date 写进了 trade_date 列，真实披露日是 ann_date。
//     若按 trade_date 过滤，三季报（9/30 截止、10/25 披露）在 9/30 就对回测可见。
//   - daily_basic 路径（SaveFundamentalBatch）不写 ann_date，其 PE/PB 本身
//     已是当日收盘口径，故回退到 trade_date。
//
// 同一 ts_code 有多期时取「可用日最近」的一期；可用日相同则取报告期更晚的一期，
// 以正确处理同一期财报被重述（restatement）的情况。
func (s *PostgresStore) GetFundamentalsSnapshot(ctx context.Context, cutoffDate time.Time) ([]domain.FundamentalData, error) {
	query := `
		SELECT DISTINCT ON (ts_code)
			id, ts_code, trade_date, ann_date, end_date,
			pe, pb, ps, roe, roa, debt_to_equity, gross_margin, net_margin,
			revenue, net_profit, total_assets, total_liab, created_at
		FROM stock_fundamentals
		WHERE COALESCE(ann_date, trade_date) <= $1 AND pe IS NOT NULL
		ORDER BY ts_code, COALESCE(ann_date, trade_date) DESC, end_date DESC
	`
	rows, err := s.pool.Query(ctx, query, cutoffDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query fundamentals snapshot: %w", err)
	}
	defer rows.Close()

	var results []domain.FundamentalData
	for rows.Next() {
		var f domain.FundamentalData
		// ann_date / end_date 在 daily_basic 写入路径下可能为 NULL，
		// 直接扫进 time.Time 会报 "cannot scan NULL"，故用 NullTime 承接。
		var annDate, endDate sql.NullTime
		if err := rows.Scan(
			&f.ID, &f.TsCode, &f.TradeDate, &annDate, &endDate,
			&f.PE, &f.PB, &f.PS, &f.ROE, &f.ROA, &f.DebtToEquity,
			&f.GrossMargin, &f.NetMargin, &f.Revenue, &f.NetProfit,
			&f.TotalAssets, &f.TotalLiab, &f.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan fundamental row: %w", err)
		}
		f.AnnDate = annDate.Time
		f.EndDate = endDate.Time
		results = append(results, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.logger.Debug().Time("cutoff", cutoffDate).Int("count", len(results)).Msg("Fundamentals snapshot loaded")
	return results, nil
}

// SaveFundamental saves or updates fundamental data.
//
// Targets stock_fundamentals since migration 025 (EQD-P3-1) dropped the
// overlapping `fundamentals` table. f.Symbol carries a ts_code literal — the
// Tushare client passes ts_code as the query key and stores item[0] verbatim —
// and end_date is backfilled from trade_date, mirroring the ETL convention in
// pkg/data/tushare.go (normalizeFundamentals / normalizeFundamentalsData).
// ann_date is not part of domain.Fundamental and is left NULL.
func (s *PostgresStore) SaveFundamental(ctx context.Context, f *domain.Fundamental) error {
	query := `
		INSERT INTO stock_fundamentals (ts_code, trade_date, end_date, pe, pb, ps, roe, roa, debt_to_equity,
			gross_margin, net_margin, revenue, net_profit, total_assets, total_liab)
		VALUES ($1, $2, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (ts_code, trade_date) DO UPDATE SET
			pe = EXCLUDED.pe, pb = EXCLUDED.pb, ps = EXCLUDED.ps,
			roe = EXCLUDED.roe, roa = EXCLUDED.roa, debt_to_equity = EXCLUDED.debt_to_equity,
			gross_margin = EXCLUDED.gross_margin, net_margin = EXCLUDED.net_margin,
			revenue = EXCLUDED.revenue, net_profit = EXCLUDED.net_profit,
			total_assets = EXCLUDED.total_assets, total_liab = EXCLUDED.total_liab
	`
	_, err := s.pool.Exec(ctx, query,
		f.Symbol, f.Date, f.PE, f.PB, f.PS, f.ROE, f.ROA, f.DebtToEquity,
		f.GrossMargin, f.NetMargin, f.Revenue, f.NetProfit, f.TotalAssets, f.TotalLiab,
	)
	if err != nil {
		return fmt.Errorf("failed to save fundamental: %w", err)
	}
	s.logger.Debug().Str("symbol", f.Symbol).Time("date", f.Date).Msg("Fundamental saved")
	return nil
}

// SaveFundamentalBatch saves multiple fundamental records in a batch.
//
// Target table and column semantics match SaveFundamental; the DO UPDATE list
// covers all 12 metric columns (it previously refreshed only 6, silently
// keeping stale margins/revenue/profit/balance-sheet values on re-ingest).
func (s *PostgresStore) SaveFundamentalBatch(ctx context.Context, records []*domain.Fundamental) error {
	if len(records) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, f := range records {
		batch.Queue(`
			INSERT INTO stock_fundamentals (ts_code, trade_date, end_date, pe, pb, ps, roe, roa, debt_to_equity,
				gross_margin, net_margin, revenue, net_profit, total_assets, total_liab)
			VALUES ($1, $2, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (ts_code, trade_date) DO UPDATE SET
				pe = EXCLUDED.pe, pb = EXCLUDED.pb, ps = EXCLUDED.ps,
				roe = EXCLUDED.roe, roa = EXCLUDED.roa, debt_to_equity = EXCLUDED.debt_to_equity,
				gross_margin = EXCLUDED.gross_margin, net_margin = EXCLUDED.net_margin,
				revenue = EXCLUDED.revenue, net_profit = EXCLUDED.net_profit,
				total_assets = EXCLUDED.total_assets, total_liab = EXCLUDED.total_liab
		`, f.Symbol, f.Date, f.PE, f.PB, f.PS, f.ROE, f.ROA, f.DebtToEquity,
			f.GrossMargin, f.NetMargin, f.Revenue, f.NetProfit, f.TotalAssets, f.TotalLiab)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch fundamental insert failed at index %d: %w", i, err)
		}
	}

	s.logger.Info().Int("count", len(records)).Msg("Batch fundamentals saved")
	return nil
}

// GetFundamental retrieves fundamental data for a symbol on a specific date.
func (s *PostgresStore) GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error) {
	query := `
		SELECT ts_code, trade_date, pe, pb, ps, roe, roa, debt_to_equity,
			gross_margin, net_margin, revenue, net_profit, total_assets, total_liab
		FROM stock_fundamentals WHERE ts_code = $1 AND trade_date = $2
	`
	var f domain.Fundamental
	err := s.pool.QueryRow(ctx, query, symbol, date).Scan(
		&f.Symbol, &f.Date, &f.PE, &f.PB, &f.PS, &f.ROE, &f.ROA, &f.DebtToEquity,
		&f.GrossMargin, &f.NetMargin, &f.Revenue, &f.NetProfit, &f.TotalAssets, &f.TotalLiab,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get fundamental: %w", err)
	}
	return &f, nil
}

// GetFundamentals retrieves all fundamental records for a symbol on or before the given date.
// Returns an empty slice if no records found.
//
// 与 GetFundamentalsSnapshot 一致的 PIT 语义：可用日 = COALESCE(ann_date, trade_date)。
// 见 TASKS P0-1；修复前按 trade_date 过滤存在同样的前视偏差。
//
// 注意：domain.Fundamental 的数值字段是 float64（非指针），而表中这些列可为空，
// 直接扫描会因 "cannot scan NULL" 报错，故用 COALESCE(col, 0) 兜底。
// 缺失值被当作 0 而非"未知"，对因子计算不够严谨 —— 见 TASKS P2-10。
func (s *PostgresStore) GetFundamentals(ctx context.Context, symbol string, date time.Time) ([]domain.Fundamental, error) {
	query := `
		SELECT ts_code, trade_date,
			COALESCE(pe, 0), COALESCE(pb, 0), COALESCE(ps, 0),
			COALESCE(roe, 0), COALESCE(roa, 0), COALESCE(debt_to_equity, 0),
			COALESCE(gross_margin, 0), COALESCE(net_margin, 0),
			COALESCE(revenue, 0), COALESCE(net_profit, 0),
			COALESCE(total_assets, 0), COALESCE(total_liab, 0)
		FROM stock_fundamentals
		WHERE ts_code = $1 AND COALESCE(ann_date, trade_date) <= $2
		ORDER BY COALESCE(ann_date, trade_date) DESC, end_date DESC
	`
	rows, err := s.pool.Query(ctx, query, symbol, date)
	if err != nil {
		return nil, fmt.Errorf("failed to query fundamentals: %w", err)
	}
	defer rows.Close()

	var records []domain.Fundamental
	for rows.Next() {
		var f domain.Fundamental
		if err := rows.Scan(
			&f.Symbol, &f.Date, &f.PE, &f.PB, &f.PS, &f.ROE, &f.ROA, &f.DebtToEquity,
			&f.GrossMargin, &f.NetMargin, &f.Revenue, &f.NetProfit, &f.TotalAssets, &f.TotalLiab,
		); err != nil {
			return nil, fmt.Errorf("failed to scan fundamental row: %w", err)
		}
		records = append(records, f)
	}
	return records, rows.Err()
}

// SaveFundamentalData saves or updates fundamental data from Tushare financial_data API.
func (s *PostgresStore) SaveFundamentalData(ctx context.Context, f *domain.FundamentalData) error {
	query := `
		INSERT INTO stock_fundamentals (ts_code, trade_date, ann_date, end_date,
			pe, pb, ps, roe, roa, debt_to_equity, gross_margin, net_margin,
			revenue, net_profit, total_assets, total_liab)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (ts_code, trade_date) DO UPDATE SET
			ann_date = EXCLUDED.ann_date,
			end_date = EXCLUDED.end_date,
			pe = EXCLUDED.pe,
			pb = EXCLUDED.pb,
			ps = EXCLUDED.ps,
			roe = EXCLUDED.roe,
			roa = EXCLUDED.roa,
			debt_to_equity = EXCLUDED.debt_to_equity,
			gross_margin = EXCLUDED.gross_margin,
			net_margin = EXCLUDED.net_margin,
			revenue = EXCLUDED.revenue,
			net_profit = EXCLUDED.net_profit,
			total_assets = EXCLUDED.total_assets,
			total_liab = EXCLUDED.total_liab
	`
	_, err := s.pool.Exec(ctx, query,
		f.TsCode, f.TradeDate, f.AnnDate, f.EndDate,
		f.PE, f.PB, f.PS, f.ROE, f.ROA, f.DebtToEquity,
		f.GrossMargin, f.NetMargin, f.Revenue, f.NetProfit,
		f.TotalAssets, f.TotalLiab,
	)
	if err != nil {
		return fmt.Errorf("failed to save fundamental data: %w", err)
	}
	s.logger.Debug().Str("ts_code", f.TsCode).Time("date", f.TradeDate).Msg("FundamentalData saved")
	return nil
}

// SaveFundamentalDataBatch saves multiple fundamental data records in a batch.
func (s *PostgresStore) SaveFundamentalDataBatch(ctx context.Context, records []*domain.FundamentalData) error {
	if len(records) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, f := range records {
		batch.Queue(`
			INSERT INTO stock_fundamentals (ts_code, trade_date, ann_date, end_date,
				pe, pb, ps, roe, roa, debt_to_equity, gross_margin, net_margin,
				revenue, net_profit, total_assets, total_liab)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			ON CONFLICT (ts_code, trade_date) DO UPDATE SET
				ann_date = EXCLUDED.ann_date,
				end_date = EXCLUDED.end_date,
				pe = EXCLUDED.pe,
				pb = EXCLUDED.pb,
				ps = EXCLUDED.ps,
				roe = EXCLUDED.roe,
				roa = EXCLUDED.roa,
				debt_to_equity = EXCLUDED.debt_to_equity,
				gross_margin = EXCLUDED.gross_margin,
				net_margin = EXCLUDED.net_margin,
				revenue = EXCLUDED.revenue,
				net_profit = EXCLUDED.net_profit,
				total_assets = EXCLUDED.total_assets,
				total_liab = EXCLUDED.total_liab
		`, f.TsCode, f.TradeDate, f.AnnDate, f.EndDate,
			f.PE, f.PB, f.PS, f.ROE, f.ROA, f.DebtToEquity,
			f.GrossMargin, f.NetMargin, f.Revenue, f.NetProfit,
			f.TotalAssets, f.TotalLiab)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch fundamental data insert failed at index %d: %w", i, err)
		}
	}

	s.logger.Info().Int("count", len(records)).Msg("Batch FundamentalData saved")
	return nil
}

// GetFundamentalDataLatest retrieves the latest fundamental data for a symbol.
func (s *PostgresStore) GetFundamentalDataLatest(ctx context.Context, tsCode string) (*domain.FundamentalData, error) {
	query := `
		SELECT id, ts_code, trade_date, ann_date, end_date,
			pe, pb, ps, roe, roa, debt_to_equity, gross_margin, net_margin,
			revenue, net_profit, total_assets, total_liab, created_at
		FROM stock_fundamentals
		WHERE ts_code = $1
		ORDER BY trade_date DESC
		LIMIT 1
	`
	var f domain.FundamentalData
	err := s.pool.QueryRow(ctx, query, tsCode).Scan(
		&f.ID, &f.TsCode, &f.TradeDate, &f.AnnDate, &f.EndDate,
		&f.PE, &f.PB, &f.PS, &f.ROE, &f.ROA, &f.DebtToEquity,
		&f.GrossMargin, &f.NetMargin, &f.Revenue, &f.NetProfit,
		&f.TotalAssets, &f.TotalLiab, &f.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get latest fundamental data: %w", err)
	}
	return &f, nil
}

// GetFundamentalDataHistory retrieves historical fundamental data for a symbol.
func (s *PostgresStore) GetFundamentalDataHistory(ctx context.Context, tsCode string, startDate, endDate *time.Time) ([]domain.FundamentalData, error) {
	var query string
	var args []interface{}

	if startDate != nil && endDate != nil {
		query = `
			SELECT id, ts_code, trade_date, ann_date, end_date,
				pe, pb, ps, roe, roa, debt_to_equity, gross_margin, net_margin,
				revenue, net_profit, total_assets, total_liab, created_at
			FROM stock_fundamentals
			WHERE ts_code = $1 AND trade_date >= $2 AND trade_date <= $3
			ORDER BY trade_date DESC
		`
		args = []interface{}{tsCode, *startDate, *endDate}
	} else {
		query = `
			SELECT id, ts_code, trade_date, ann_date, end_date,
				pe, pb, ps, roe, roa, debt_to_equity, gross_margin, net_margin,
				revenue, net_profit, total_assets, total_liab, created_at
			FROM stock_fundamentals
			WHERE ts_code = $1
			ORDER BY trade_date DESC
		`
		args = []interface{}{tsCode}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query fundamental data history: %w", err)
	}
	defer rows.Close()

	var results []domain.FundamentalData
	for rows.Next() {
		var f domain.FundamentalData
		if err := rows.Scan(
			&f.ID, &f.TsCode, &f.TradeDate, &f.AnnDate, &f.EndDate,
			&f.PE, &f.PB, &f.PS, &f.ROE, &f.ROA, &f.DebtToEquity,
			&f.GrossMargin, &f.NetMargin, &f.Revenue, &f.NetProfit,
			&f.TotalAssets, &f.TotalLiab, &f.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan fundamental data row: %w", err)
		}
		results = append(results, f)
	}

	return results, rows.Err()
}

// ScreenFundamentals filters stocks by fundamental criteria.
func (s *PostgresStore) ScreenFundamentals(ctx context.Context, filters domain.ScreenFilters, date *time.Time, limit int) ([]domain.ScreenResult, error) {
	query, args := buildScreenFundamentalsQuery(filters, date, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to screen fundamentals: %w", err)
	}
	defer rows.Close()

	var results []domain.ScreenResult
	for rows.Next() {
		var r domain.ScreenResult
		if err := rows.Scan(
			&r.TsCode, &r.PE, &r.PB, &r.PS, &r.ROE, &r.ROA,
			&r.DebtToEquity, &r.GrossMargin, &r.NetMargin, &r.MarketCap,
		); err != nil {
			return nil, fmt.Errorf("failed to scan screen result row: %w", err)
		}
		results = append(results, r)
	}

	return results, rows.Err()
}

// buildScreenFundamentalsQuery constructs the parameterized SQL query and
// argument list for ScreenFundamentals. Extracted as a pure function so
// the query-building logic can be unit-tested without a live database
// (pgxpool.Query cannot be mocked with sqlmock; pgxmock is not a project
// dependency). The returned query uses PostgreSQL positional parameters
// ($1, $2, ...) aligned with the returned args slice.
func buildScreenFundamentalsQuery(filters domain.ScreenFilters, date *time.Time, limit int) (string, []interface{}) {
	query := `
		SELECT sf.ts_code, sf.pe, sf.pb, sf.ps, sf.roe, sf.roa, sf.debt_to_equity,
			sf.gross_margin, sf.net_margin, st.market_cap
		FROM stock_fundamentals sf
		LEFT JOIN stocks st ON sf.ts_code = st.symbol
	`
	var conditions []string
	var args []interface{}
	argIdx := 1

	if date != nil {
		conditions = append(conditions, fmt.Sprintf("sf.trade_date = $%d", argIdx))
		args = append(args, *date)
		argIdx++
	} else {
		query = fmt.Sprintf(`
			SELECT sf.ts_code, sf.pe, sf.pb, sf.ps, sf.roe, sf.roa, sf.debt_to_equity,
				sf.gross_margin, sf.net_margin, st.market_cap
			FROM (
				SELECT ts_code, pe, pb, ps, roe, roa, debt_to_equity,
					gross_margin, net_margin,
					ROW_NUMBER() OVER (PARTITION BY ts_code ORDER BY trade_date DESC) as rn
				FROM stock_fundamentals
			) sf
			LEFT JOIN stocks st ON sf.ts_code = st.symbol
			WHERE sf.rn = 1
		`)
	}

	if filters.PE_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.pe IS NULL OR sf.pe >= $%d)", argIdx))
		args = append(args, *filters.PE_min)
		argIdx++
	}
	if filters.PE_max != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.pe IS NULL OR sf.pe <= $%d)", argIdx))
		args = append(args, *filters.PE_max)
		argIdx++
	}
	if filters.PB_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.pb IS NULL OR sf.pb >= $%d)", argIdx))
		args = append(args, *filters.PB_min)
		argIdx++
	}
	if filters.PB_max != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.pb IS NULL OR sf.pb <= $%d)", argIdx))
		args = append(args, *filters.PB_max)
		argIdx++
	}
	if filters.PS_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.ps IS NULL OR sf.ps >= $%d)", argIdx))
		args = append(args, *filters.PS_min)
		argIdx++
	}
	if filters.PS_max != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.ps IS NULL OR sf.ps <= $%d)", argIdx))
		args = append(args, *filters.PS_max)
		argIdx++
	}
	if filters.ROE_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.roe IS NULL OR sf.roe >= $%d)", argIdx))
		args = append(args, *filters.ROE_min)
		argIdx++
	}
	if filters.ROA_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.roa IS NULL OR sf.roa >= $%d)", argIdx))
		args = append(args, *filters.ROA_min)
		argIdx++
	}
	if filters.DebtToEquity_max != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.debt_to_equity IS NULL OR sf.debt_to_equity <= $%d)", argIdx))
		args = append(args, *filters.DebtToEquity_max)
		argIdx++
	}
	if filters.GrossMargin_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.gross_margin IS NULL OR sf.gross_margin >= $%d)", argIdx))
		args = append(args, *filters.GrossMargin_min)
		argIdx++
	}
	if filters.NetMargin_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.net_margin IS NULL OR sf.net_margin >= $%d)", argIdx))
		args = append(args, *filters.NetMargin_min)
		argIdx++
	}
	if filters.MarketCap_min != nil {
		conditions = append(conditions, fmt.Sprintf("(st.market_cap IS NULL OR st.market_cap >= $%d)", argIdx))
		args = append(args, *filters.MarketCap_min)
		argIdx++
	}

	if len(conditions) > 0 {
		if strings.Contains(query, "WHERE sf.rn = 1") {
			query += " AND " + strings.Join(conditions, " AND ")
		} else {
			query += " WHERE " + strings.Join(conditions, " AND ")
		}
	}

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	return query, args
}
