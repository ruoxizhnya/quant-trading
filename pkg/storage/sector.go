package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ── Sectors（DATA-01）────────────────────────────────────────────────────────

// ListStockIndustries 读取 stocks 表的 (symbol, industry) 对，作为板块派生的数据源。
//
// 只取 industry 非空非空白的行 —— 空行业无法构成「个股 → 板块」映射，
// 硬灌进去只会让 sector 分组里出现一个空标签。
func (s *PostgresStore) ListStockIndustries(ctx context.Context) ([]domain.StockIndustry, error) {
	query := `
		SELECT symbol, industry
		FROM stocks
		WHERE industry IS NOT NULL AND btrim(industry) <> ''
		ORDER BY symbol
	`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list stock industries: %w", err)
	}
	defer rows.Close()

	var out []domain.StockIndustry
	for rows.Next() {
		var si domain.StockIndustry
		if err := rows.Scan(&si.Symbol, &si.Industry); err != nil {
			return nil, fmt.Errorf("failed to scan stock industry row: %w", err)
		}
		out = append(out, si)
	}
	return out, rows.Err()
}

// SaveSectorBatch 写入板块字典（sectors），同名板块按最新内容更新。
func (s *PostgresStore) SaveSectorBatch(ctx context.Context, sectors []domain.Sector) error {
	if len(sectors) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, sec := range sectors {
		batch.Queue(`
			INSERT INTO sectors (sector_code, sector_name, category, trade_date, change_pct, leading_symbol, source)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (sector_code) DO UPDATE SET
				sector_name    = EXCLUDED.sector_name,
				category       = EXCLUDED.category,
				trade_date     = EXCLUDED.trade_date,
				change_pct     = EXCLUDED.change_pct,
				leading_symbol = EXCLUDED.leading_symbol,
				source         = EXCLUDED.source
		`, sec.SectorCode, sec.SectorName, sec.Category, sec.TradeDate, sec.ChangePct, nullableString(sec.LeadingSymbol), sec.Source)
	}

	results := tx.SendBatch(ctx, batch)
	for i := 0; i < len(sectors); i++ {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return fmt.Errorf("batch sector insert failed at index %d: %w", i, err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("batch sector insert close failed: %w", err)
	}
	return tx.Commit(ctx)
}

// SaveStockSectorMapBatch 写入个股 ↔ 板块映射（stock_sector_map）。
func (s *PostgresStore) SaveStockSectorMapBatch(ctx context.Context, rows []domain.StockSector) error {
	if len(rows) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, r := range rows {
		batch.Queue(`
			INSERT INTO stock_sector_map (symbol, sector_code, sector_name, source)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (symbol, sector_code) DO UPDATE SET
				sector_name = EXCLUDED.sector_name,
				source      = EXCLUDED.source
		`, r.Symbol, r.SectorCode, r.SectorName, r.Source)
	}

	results := tx.SendBatch(ctx, batch)
	for i := 0; i < len(rows); i++ {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return fmt.Errorf("batch stock sector insert failed at index %d: %w", i, err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("batch stock sector insert close failed: %w", err)
	}
	return tx.Commit(ctx)
}
