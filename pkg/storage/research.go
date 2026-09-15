package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrEmptyResearchTicker is returned when a research lookup is made without a
// ticker.
var ErrEmptyResearchTicker = fmt.Errorf("research profile: ticker is required")

// ResearchProfile is the aggregate read model of one EquityDeep research
// archive (ADR-022 §2 class E: research.* is a droppable, rebuildable
// projection of the vault markdown — never the fact source itself).
//
// The profile columns mirror research.profile verbatim; Conclusions and
// Questions are the child rows of research.conclusion / research.question for
// the same ticker. Ticker is the bare 6-digit code (contracts/profile.schema.json
// pattern ^[0-9]{6}$) — the exchange-suffixed ts_code form lives only in
// market-facing tables.
type ResearchProfile struct {
	Ticker         string     `json:"ticker"`
	Name           string     `json:"name"`
	SchemaVersion  int        `json:"schema_version"`
	SourceFile     *string    `json:"source_file,omitempty"`
	SourceMtime    *time.Time `json:"source_mtime,omitempty"`
	LastResearched *time.Time `json:"last_researched,omitempty"`
	NeedsReview    bool       `json:"needs_review"`
	ReviewReason   *string    `json:"review_reason,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`

	Conclusions []ResearchConclusion `json:"conclusions"`
	Questions   []ResearchQuestion   `json:"questions"`
}

// ResearchConclusion is one row of research.conclusion. Citations holds the
// ADR-022 §5 anchor array ({source, dataset, key, as_of, content_hash} +
// JSON Pointer) verbatim as stored JSONB — this layer never rewrites it.
type ResearchConclusion struct {
	Ticker          string          `json:"ticker"`
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	Body            *string         `json:"body,omitempty"`
	AsOf            *string         `json:"as_of,omitempty"`
	Confidence      *string         `json:"confidence,omitempty"`
	Status          string          `json:"status"`
	Citations       json.RawMessage `json:"citations"`
	EvidencePointer *string         `json:"evidence_pointer,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// ResearchQuestion is one row of research.question.
type ResearchQuestion struct {
	Ticker   string  `json:"ticker"`
	ID       string  `json:"id"`
	Text     string  `json:"text"`
	Status   string  `json:"status"`
	RaisedAt *string `json:"raised_at,omitempty"`
}

// GetResearchProfile loads the archived profile of one ticker together with its
// conclusions and questions. It returns (nil, nil) when the ticker was never
// projected — callers map that to "no profile" (HTTP 404 for the
// research.profile MCP tool), exactly like GetRawIngest does for an unknown
// content hash.
//
// Missing children are not an error: a freshly created profile legitimately has
// no conclusions or questions yet, so both slices come back empty (non-nil).
func (s *PostgresStore) GetResearchProfile(ctx context.Context, ticker string) (*ResearchProfile, error) {
	if ticker == "" {
		return nil, ErrEmptyResearchTicker
	}

	profile := &ResearchProfile{}
	err := s.pool.QueryRow(ctx, `
		SELECT ticker, name, schema_version, source_file, source_mtime,
		       last_researched, needs_review, review_reason, updated_at
		FROM research.profile WHERE ticker = $1
	`, ticker).Scan(
		&profile.Ticker, &profile.Name, &profile.SchemaVersion,
		&profile.SourceFile, &profile.SourceMtime, &profile.LastResearched,
		&profile.NeedsReview, &profile.ReviewReason, &profile.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get research profile: %w", err)
	}

	conclusions, err := s.listResearchConclusions(ctx, ticker)
	if err != nil {
		return nil, err
	}
	questions, err := s.listResearchQuestions(ctx, ticker)
	if err != nil {
		return nil, err
	}
	profile.Conclusions = conclusions
	profile.Questions = questions
	return profile, nil
}

// listResearchConclusions returns every conclusion of ticker, ordered by
// conclusion_id. Superseded rows are returned as-is (with their status) rather
// than filtered — the archive is the fact source and consumers decide what to
// do with a superseded claim.
func (s *PostgresStore) listResearchConclusions(ctx context.Context, ticker string) ([]ResearchConclusion, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ticker, conclusion_id, title, body, as_of, confidence,
		       status, citations, evidence_pointer, updated_at
		FROM research.conclusion WHERE ticker = $1
		ORDER BY conclusion_id
	`, ticker)
	if err != nil {
		return nil, fmt.Errorf("failed to list research conclusions: %w", err)
	}
	defer rows.Close()

	out := make([]ResearchConclusion, 0)
	for rows.Next() {
		var c ResearchConclusion
		var citations []byte
		if err := rows.Scan(
			&c.Ticker, &c.ID, &c.Title, &c.Body, &c.AsOf, &c.Confidence,
			&c.Status, &citations, &c.EvidencePointer, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan research conclusion: %w", err)
		}
		c.Citations = json.RawMessage(citations)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to list research conclusions: %w", err)
	}
	return out, nil
}

// listResearchQuestions returns every question of ticker, ordered by
// question_id. Closed questions are kept: "what was asked, and since resolved"
// is part of the research record — the row itself carries open/closed.
func (s *PostgresStore) listResearchQuestions(ctx context.Context, ticker string) ([]ResearchQuestion, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ticker, question_id, text, status, raised_at
		FROM research.question WHERE ticker = $1
		ORDER BY question_id
	`, ticker)
	if err != nil {
		return nil, fmt.Errorf("failed to list research questions: %w", err)
	}
	defer rows.Close()

	out := make([]ResearchQuestion, 0)
	for rows.Next() {
		var q ResearchQuestion
		if err := rows.Scan(&q.Ticker, &q.ID, &q.Text, &q.Status, &q.RaisedAt); err != nil {
			return nil, fmt.Errorf("failed to scan research question: %w", err)
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to list research questions: %w", err)
	}
	return out, nil
}
