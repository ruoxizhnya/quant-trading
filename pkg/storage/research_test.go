package storage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────────────────────────────────────────────────
// GetResearchProfile — argument validation (no DB required)
// ──────────────────────────────────────────────────────────────────────

// TestGetResearchProfile_RequiresTicker verifies the empty ticker is rejected
// before the pool is touched. research.profile is keyed by ticker, so a lookup
// without one has no meaning.
func TestGetResearchProfile_RequiresTicker(t *testing.T) {
	store := &PostgresStore{}

	got, err := store.GetResearchProfile(context.Background(), "")
	assert.ErrorIs(t, err, ErrEmptyResearchTicker)
	assert.Nil(t, got)
}

// ──────────────────────────────────────────────────────────────────────
// GetResearchProfile — DB-backed round trip
// ──────────────────────────────────────────────────────────────────────

// Fixture tickers sit outside the A-share space so they cannot collide with
// real projected data.
const (
	researchRoundTripTicker = "990001"
	researchEmptyTicker     = "990002"
	researchUnknownTicker   = "990003"
)

// clearResearchRows deletes any leftover rows for ticker (children cascade).
// Tests call it before arranging their fixture and again via defer — the
// caller's `defer store.Close()` is registered first, so LIFO ordering runs
// this while the pool is still open.
func clearResearchRows(ctx context.Context, store *PostgresStore, ticker string) error {
	_, err := store.pool.Exec(ctx, `DELETE FROM research.profile WHERE ticker = $1`, ticker)
	return err
}

// TestGetResearchProfile_RoundTrip verifies the aggregate read model: the
// profile header, its conclusions with citations preserved as JSONB, and its
// questions all come back from one call, ordered by their contract C2 ids.
func TestGetResearchProfile_RoundTrip(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()
	ticker := researchRoundTripTicker
	require.NoError(t, clearResearchRows(ctx, store, ticker))
	defer func() { _ = clearResearchRows(ctx, store, ticker) }()

	updatedAt := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Millisecond)
	sourceMtime := updatedAt.Add(-time.Hour)
	_, err := store.pool.Exec(ctx, `
		INSERT INTO research.profile
			(ticker, name, schema_version, source_file, source_mtime,
			 last_researched, needs_review, review_reason, updated_at)
		VALUES ($1, $2, 1, $3, $4, $5, TRUE, $6, $7)
	`, ticker, "测试股份", ticker+"/_profile.md", sourceMtime,
		time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC), "年报发布", updatedAt)
	require.NoError(t, err)

	citations := `[{"source":"akshare","dataset":"fundamentals.income","key":"` + ticker +
		`/2024Q1","as_of":"2024-03-31","content_hash":"` + strings.Repeat("d", 64) + `","pointer":"/revenue"}]`

	// Insert C2 before C1 to prove the read is ordered by id, not insertion order.
	for _, row := range []struct {
		id, title, status string
	}{
		{"C2", "第二条结论", "superseded"},
		{"C1", "第一条结论", "active"},
	} {
		_, err := store.pool.Exec(ctx, `
			INSERT INTO research.conclusion
				(ticker, conclusion_id, title, body, as_of, confidence, status, citations, evidence_pointer)
			VALUES ($1, $2, $3, $4, '2024-06-30', '高', $5, $6::jsonb, $7)
		`, ticker, row.id, row.title, "结论正文 "+row.id, row.status, citations, "/"+row.id)
		require.NoError(t, err)
	}

	for _, row := range []struct {
		id, text, status string
	}{
		{"Q2", "第二个问题", "closed"},
		{"Q1", "第一个问题", "open"},
	} {
		_, err := store.pool.Exec(ctx, `
			INSERT INTO research.question (ticker, question_id, text, status, raised_at)
			VALUES ($1, $2, $3, $4, '2024-06-30')
		`, ticker, row.id, row.text, row.status)
		require.NoError(t, err)
	}

	got, err := store.GetResearchProfile(ctx, ticker)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, ticker, got.Ticker)
	assert.Equal(t, "测试股份", got.Name)
	assert.Equal(t, 1, got.SchemaVersion)
	assert.True(t, got.NeedsReview)
	require.NotNil(t, got.ReviewReason)
	assert.Equal(t, "年报发布", *got.ReviewReason)
	require.NotNil(t, got.SourceFile)
	assert.Equal(t, ticker+"/_profile.md", *got.SourceFile)
	require.NotNil(t, got.SourceMtime)
	assert.WithinDuration(t, sourceMtime, *got.SourceMtime, time.Second)
	require.NotNil(t, got.LastResearched)
	assert.Equal(t, "2024-06-30", got.LastResearched.Format("2006-01-02"))

	require.Len(t, got.Conclusions, 2)
	assert.Equal(t, []string{"C1", "C2"}, []string{got.Conclusions[0].ID, got.Conclusions[1].ID})
	assert.Equal(t, "第一条结论", got.Conclusions[0].Title)
	assert.Equal(t, "superseded", got.Conclusions[1].Status, "superseded claims are returned, not filtered")
	require.NotNil(t, got.Conclusions[0].Confidence)
	assert.Equal(t, "高", *got.Conclusions[0].Confidence)

	// Citations must survive as JSONB — the evidence anchors are what make the
	// claim traceable (ADR-022 §5), so this layer must not rewrite them.
	assert.JSONEq(t, citations, string(got.Conclusions[0].Citations))

	require.Len(t, got.Questions, 2)
	assert.Equal(t, []string{"Q1", "Q2"}, []string{got.Questions[0].ID, got.Questions[1].ID})
	assert.Equal(t, "open", got.Questions[0].Status)
	assert.Equal(t, "closed", got.Questions[1].Status, "closed questions stay in the record")
}

// TestGetResearchProfile_UnknownTicker_ReturnsNilNil verifies the "never
// projected" signal the research.profile tool maps to HTTP 404. (nil, nil)
// keeps "absent" distinct from "query failed" — the same convention
// GetRawIngest uses.
func TestGetResearchProfile_UnknownTicker_ReturnsNilNil(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()
	require.NoError(t, clearResearchRows(ctx, store, researchUnknownTicker))
	defer func() { _ = clearResearchRows(ctx, store, researchUnknownTicker) }()

	got, err := store.GetResearchProfile(ctx, researchUnknownTicker)
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestGetResearchProfile_NoChildren_ReturnsEmptySlices verifies a profile with
// no conclusions/questions yet is not an error, and that both slices come back
// non-nil so consumers can iterate without a nil check.
func TestGetResearchProfile_NoChildren_ReturnsEmptySlices(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()
	ticker := researchEmptyTicker
	require.NoError(t, clearResearchRows(ctx, store, ticker))
	defer func() { _ = clearResearchRows(ctx, store, ticker) }()

	_, err := store.pool.Exec(ctx, `
		INSERT INTO research.profile (ticker, name, schema_version) VALUES ($1, '空档案', 1)
	`, ticker)
	require.NoError(t, err)

	got, err := store.GetResearchProfile(ctx, ticker)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.NotNil(t, got.Conclusions)
	assert.Empty(t, got.Conclusions)
	assert.NotNil(t, got.Questions)
	assert.Empty(t, got.Questions)
	assert.Nil(t, got.LastResearched)
	assert.False(t, got.NeedsReview)
	assert.Nil(t, got.ReviewReason)
}

// TestGetResearchProfile_EmptyCitations verifies a conclusion stored without
// anchors still decodes into a valid JSON array rather than a nil RawMessage.
func TestGetResearchProfile_EmptyCitations(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()
	ticker := researchEmptyTicker
	require.NoError(t, clearResearchRows(ctx, store, ticker))
	defer func() { _ = clearResearchRows(ctx, store, ticker) }()

	_, err := store.pool.Exec(ctx, `
		INSERT INTO research.profile (ticker, name, schema_version) VALUES ($1, '空引用', 1)
	`, ticker)
	require.NoError(t, err)
	_, err = store.pool.Exec(ctx, `
		INSERT INTO research.conclusion (ticker, conclusion_id, title, status, citations)
		VALUES ($1, 'C1', '无引用结论', 'active', '[]'::jsonb)
	`, ticker)
	require.NoError(t, err)

	got, err := store.GetResearchProfile(ctx, ticker)
	require.NoError(t, err)
	require.Len(t, got.Conclusions, 1)
	assert.JSONEq(t, "[]", string(got.Conclusions[0].Citations))

	var anchors []map[string]interface{}
	require.NoError(t, json.Unmarshal(got.Conclusions[0].Citations, &anchors))
	assert.Empty(t, anchors)
}
