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

// stockColumns 是 stocks 表的读取列序（含 P2-4 新增的 delist_date）。
const stockColumns = `symbol, name, exchange, industry, market_cap, list_date, status, delist_date`

// nullTimePtr 把可空日期转成 *time.Time：NULL = 仍在市，不是零值时间。
func nullTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// SaveStock saves or updates a stock record.
func (s *PostgresStore) SaveStock(ctx context.Context, stock *domain.Stock) error {
	query := `
		INSERT INTO stocks (symbol, name, exchange, industry, market_cap, list_date, status, delist_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (symbol) DO UPDATE SET
			name = EXCLUDED.name,
			exchange = EXCLUDED.exchange,
			industry = EXCLUDED.industry,
			market_cap = EXCLUDED.market_cap,
			list_date = EXCLUDED.list_date,
			status = EXCLUDED.status,
			delist_date = EXCLUDED.delist_date,
			updated_at = NOW()
	`
	_, err := s.pool.Exec(ctx, query,
		stock.Symbol, stock.Name, stock.Exchange, stock.Industry,
		stock.MarketCap, stock.ListDate, stock.Status, stock.DelistDate,
	)
	if err != nil {
		return fmt.Errorf("failed to save stock: %w", err)
	}
	s.logger.Debug().Str("symbol", stock.Symbol).Msg("Stock saved")
	return nil
}

// SaveStockBatch saves multiple stocks in a batch.
func (s *PostgresStore) SaveStockBatch(ctx context.Context, stocks []domain.Stock) error {
	if len(stocks) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, st := range stocks {
		batch.Queue(`
			INSERT INTO stocks (symbol, name, exchange, industry, market_cap, list_date, status, delist_date)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (symbol) DO UPDATE SET
				name = EXCLUDED.name, exchange = EXCLUDED.exchange,
				industry = EXCLUDED.industry, market_cap = EXCLUDED.market_cap,
				list_date = EXCLUDED.list_date, status = EXCLUDED.status,
				delist_date = EXCLUDED.delist_date,
				updated_at = NOW()
		`, st.Symbol, st.Name, st.Exchange, st.Industry, st.MarketCap, st.ListDate, st.Status, st.DelistDate)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(stocks); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch stock insert failed at index %d: %w", i, err)
		}
	}

	s.logger.Info().Int("count", len(stocks)).Msg("Batch stocks saved")
	return nil
}

// GetStocks retrieves stocks, optionally filtered by exchange.
func (s *PostgresStore) GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	var query string
	var args []interface{}

	if exchange != "" {
		query = `
			SELECT ` + stockColumns + `
			FROM stocks WHERE exchange = $1 ORDER BY symbol
		`
		args = []interface{}{exchange}
	} else {
		query = `
			SELECT ` + stockColumns + `
			FROM stocks ORDER BY symbol
		`
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query stocks: %w", err)
	}
	defer rows.Close()

	var results []domain.Stock
	for rows.Next() {
		st, err := scanStock(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, st)
	}

	return results, rows.Err()
}

// scanStock 扫一行 stocks（列序见 stockColumns）。
func scanStock(row interface{ Scan(dest ...any) error }) (domain.Stock, error) {
	var st domain.Stock
	var delist sql.NullTime
	if err := row.Scan(
		&st.Symbol, &st.Name, &st.Exchange, &st.Industry,
		&st.MarketCap, &st.ListDate, &st.Status, &delist,
	); err != nil {
		return domain.Stock{}, fmt.Errorf("failed to scan stock row: %w", err)
	}
	st.DelistDate = nullTimePtr(delist)
	return st, nil
}

// GetStock retrieves a single stock by symbol.
func (s *PostgresStore) GetStock(ctx context.Context, symbol string) (*domain.Stock, error) {
	query := `
		SELECT ` + stockColumns + `
		FROM stocks WHERE symbol = $1
	`
	st, err := scanStock(s.pool.QueryRow(ctx, query, symbol))
	if err != nil {
		if err.Error() == "no rows in result set" ||
			strings.Contains(err.Error(), "no rows in result set") {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get stock: %w", err)
	}
	return &st, nil
}

// GetAllStocks returns all stocks from the database.
func (s *PostgresStore) GetAllStocks(ctx context.Context) ([]domain.Stock, error) {
	query := `
		SELECT ` + stockColumns + `
		FROM stocks ORDER BY symbol
	`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all stocks: %w", err)
	}
	defer rows.Close()

	var results []domain.Stock
	for rows.Next() {
		st, err := scanStock(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, st)
	}
	return results, rows.Err()
}

// ListingWindow 是一只股票的「在市区间」：上市之后、摘牌之前。
//
// Delist == nil 表示还没退市（右端开放）。
// List 为零值表示上市日未知 —— 此时不做左端过滤（宁可放宽也不凭空剔除）。
type ListingWindow struct {
	List  time.Time
	Delist *time.Time
}

// IsListed 报告这只票在 date 当天是否还在市。
func (w ListingWindow) IsListed(date time.Time) bool {
	if !w.List.IsZero() && date.Before(w.List) {
		return false // 还没上市
	}
	if w.Delist != nil && date.After(*w.Delist) {
		return false // 已经摘牌
	}
	return true
}

// GetListingWindows 一次性取回全部股票的上市 / 摘牌日期（P2-4）。
//
// 引擎用它构造内存日历，回测时按天过滤池子 —— 一次查询覆盖整个回测区间，
// 而不是每天查一次库。
//
// 返回 nil（且 err == nil）表示取不到这份数据：调用方必须据此**放弃过滤**
// 并如实上报，不能假设"没记录 = 一直在市"（那正是幸存者偏差的成因）。
func (s *PostgresStore) GetListingWindows(ctx context.Context) (map[string]ListingWindow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT symbol, list_date, delist_date FROM stocks`)
	if err != nil {
		return nil, fmt.Errorf("failed to query listing windows: %w", err)
	}
	defer rows.Close()

	out := make(map[string]ListingWindow)
	for rows.Next() {
		var symbol string
		var listDate, delistDate sql.NullTime
		if err := rows.Scan(&symbol, &listDate, &delistDate); err != nil {
			return nil, fmt.Errorf("failed to scan listing window: %w", err)
		}
		out[symbol] = ListingWindow{
			List:   listDate.Time, // 无效时是零值，IsListed 会跳过左端判断
			Delist: nullTimePtr(delistDate),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.logger.Debug().Int("stocks", len(out)).Msg("Listing windows loaded")
	return out, nil
}
