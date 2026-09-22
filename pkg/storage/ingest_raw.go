package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrEmptyContentHash is returned when a RawIngest is saved without a content hash.
var ErrEmptyContentHash = fmt.Errorf("raw ingest: content_hash is required")

// RawIngest is one archived external-source response (ADR-022 §2 class A).
// ingest.raw is the single evidence coordinate of the whole platform: every
// number produced anywhere must be resolvable back to exactly one row here.
type RawIngest struct {
	ContentHash string          `json:"content_hash"`
	Source      string          `json:"source"`
	Dataset     string          `json:"dataset"`
	Key         string          `json:"key"`
	AsOf        *time.Time      `json:"as_of,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	FetchedAt   time.Time       `json:"fetched_at"`
}

// ContentHashOf computes the canonical sha256 (lowercase hex, 64 chars) of a raw
// source payload. Canonicalization re-marshals the decoded JSON, so whitespace and
// object key order differences in the source response do not change the hash for
// otherwise identical content.
func ContentHashOf(payload []byte) (string, error) {
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", fmt.Errorf("failed to decode payload for hashing: %w", err)
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize payload for hashing: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// SaveRawIngest archives a raw source response. The write is idempotent: identical
// content produces the same content_hash, and an existing row is left untouched
// because ingest.raw is immutable (re-ingesting the same source never rewrites history).
func (s *PostgresStore) SaveRawIngest(ctx context.Context, r *RawIngest) error {
	if r == nil {
		return fmt.Errorf("raw ingest: nil record")
	}
	if r.ContentHash == "" {
		return ErrEmptyContentHash
	}
	if r.FetchedAt.IsZero() {
		r.FetchedAt = time.Now()
	}

	query := `
		INSERT INTO ingest.raw (content_hash, source, dataset, key, as_of, payload, fetched_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (content_hash) DO NOTHING
	`
	_, err := s.pool.Exec(ctx, query,
		r.ContentHash, r.Source, r.Dataset, r.Key, r.AsOf, []byte(r.Payload), r.FetchedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save raw ingest: %w", err)
	}
	return nil
}

// GetRawIngest returns the unique archived record for contentHash, or nil when the
// hash was never ingested (callers map nil to HTTP 404 — "not ingested").
func (s *PostgresStore) GetRawIngest(ctx context.Context, contentHash string) (*RawIngest, error) {
	query := `
		SELECT content_hash, source, dataset, key, as_of, payload, fetched_at
		FROM ingest.raw WHERE content_hash = $1
	`
	record := &RawIngest{}
	var asOf *time.Time
	var payload []byte

	err := s.pool.QueryRow(ctx, query, contentHash).Scan(
		&record.ContentHash, &record.Source, &record.Dataset, &record.Key,
		&asOf, &payload, &record.FetchedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get raw ingest: %w", err)
	}

	record.AsOf = asOf
	record.Payload = json.RawMessage(payload)
	return record, nil
}
