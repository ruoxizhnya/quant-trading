package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ─ fundamentals_detail (contract C1-b, EQD-P1-2) ────────────────────────────
//
// The table stores deep financial statements one field per row, so a number
// keeps its source coordinates instead of being flattened into a wide table.
// Two properties of the schema shape everything below:
//
//   - the primary key contains fetched_at, so a restatement of the same report
//     period is a new row rather than an overwrite — history is never lost;
//   - ann_date is NOT NULL and is the PIT key, so every read used by the
//     vertical factors filters on it rather than on end_date.

// SaveFundamentalsDetailBatch writes normalized rows in one transaction.
//
// Re-ingesting a snapshot is idempotent up to the mutable columns: the primary
// key covers (ts_code, end_date, ann_date, field_code, fetched_at), so posting
// the same snapshot twice rewrites the same rows with the same values instead
// of duplicating them.
func (s *PostgresStore) SaveFundamentalsDetailBatch(ctx context.Context, rows []domain.FundamentalsDetailRow) error {
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
		// value is the only nullable column: a nil pointer stores NULL and
		// keeps "the source had no reading" distinct from a real zero.
		batch.Queue(`
			INSERT INTO fundamentals_detail
				(ts_code, end_date, ann_date, field_code, raw_field_name, value, unit, source, fetched_at, snapshot_uri)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (ts_code, end_date, ann_date, field_code, fetched_at) DO UPDATE SET
				raw_field_name = EXCLUDED.raw_field_name,
				value          = EXCLUDED.value,
				unit           = EXCLUDED.unit,
				source         = EXCLUDED.source,
				snapshot_uri   = EXCLUDED.snapshot_uri
		`, r.TsCode, r.EndDate, r.AnnDate, r.FieldCode, r.RawFieldName,
			r.Value, r.Unit, r.Source, r.FetchedAt, r.SnapshotURI)
	}

	results := tx.SendBatch(ctx, batch)
	defer results.Close()

	for i := range rows {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch fundamentals_detail insert failed at index %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Info().Int("count", len(rows)).Msg("Batch fundamentals_detail saved")
	return nil
}

// GetFundamentalsDetailAsOf returns every reading for the given field codes
// whose announcement date is on or before asOf.
//
// The filter is on ann_date, not end_date: a report period only exists as
// knowledge from the day it was announced, so a factor computed for date D may
// only see rows with ann_date <= D. Filtering on end_date instead would hand
// the model numbers that had not been published yet.
//
// Rows are deduplicated per (ts_code, end_date, ann_date, field_code) keeping
// the latest snapshot, so a restatement of one announcement contributes one
// value rather than two, and the result is deterministic.
func (s *PostgresStore) GetFundamentalsDetailAsOf(ctx context.Context, fieldCodes []string, asOf time.Time) ([]domain.FundamentalsDetailRow, error) {
	if len(fieldCodes) == 0 {
		return nil, nil
	}

	query := `
		SELECT ts_code, end_date, ann_date, field_code, raw_field_name,
		       value, unit, source, fetched_at, snapshot_uri
		FROM (
			SELECT DISTINCT ON (ts_code, end_date, ann_date, field_code)
			       ts_code, end_date, ann_date, field_code, raw_field_name,
			       value, unit, source, fetched_at, snapshot_uri
			FROM fundamentals_detail
			WHERE ann_date <= $1 AND field_code = ANY($2)
			ORDER BY ts_code, end_date, ann_date, field_code, fetched_at DESC
		) latest
		ORDER BY ts_code, end_date, field_code
	`

	rows, err := s.pool.Query(ctx, query, asOf, fieldCodes)
	if err != nil {
		return nil, fmt.Errorf("failed to query fundamentals_detail: %w", err)
	}
	defer rows.Close()

	var out []domain.FundamentalsDetailRow
	for rows.Next() {
		var r domain.FundamentalsDetailRow
		if err := rows.Scan(
			&r.TsCode, &r.EndDate, &r.AnnDate, &r.FieldCode, &r.RawFieldName,
			&r.Value, &r.Unit, &r.Source, &r.FetchedAt, &r.SnapshotURI,
		); err != nil {
			return nil, fmt.Errorf("failed to scan fundamentals_detail row: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read fundamentals_detail rows: %w", err)
	}
	return out, nil
}
