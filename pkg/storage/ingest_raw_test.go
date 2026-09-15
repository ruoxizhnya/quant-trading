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
// ContentHashOf — pure function, no DB required
// ──────────────────────────────────────────────────────────────────────

// TestContentHashOf_CanonicalizesWhitespaceAndKeyOrder verifies that two
// byte-identical-in-meaning payloads hash the same. ingest.raw is keyed by
// content_hash, so a source response that only differs in formatting must not
// produce a second evidence coordinate.
func TestContentHashOf_CanonicalizesWhitespaceAndKeyOrder(t *testing.T) {
	a := []byte(`{"ticker":"000001.SZ","period":"2024Q1","revenue":123}`)
	b := []byte("{\n  \"revenue\": 123,\n  \"period\": \"2024Q1\",\n  \"ticker\": \"000001.SZ\"\n}")

	hashA, err := ContentHashOf(a)
	require.NoError(t, err)
	hashB, err := ContentHashOf(b)
	require.NoError(t, err)

	assert.Equal(t, hashA, hashB, "formatting differences must not change the hash")
	assert.Len(t, hashA, 64, "sha256 hex digest should be 64 chars")
	assert.Equal(t, strings.ToLower(hashA), hashA, "hash must be lowercase hex")
}

// TestContentHashOf_DistinguishesDifferentContent guards the other direction:
// the canonicalization must not collapse genuinely different payloads.
func TestContentHashOf_DistinguishesDifferentContent(t *testing.T) {
	hashA, err := ContentHashOf([]byte(`{"revenue":123}`))
	require.NoError(t, err)
	hashB, err := ContentHashOf([]byte(`{"revenue":124}`))
	require.NoError(t, err)

	assert.NotEqual(t, hashA, hashB)
}

// TestContentHashOf_RejectsInvalidJSON verifies malformed input is reported
// rather than silently hashed as bytes.
func TestContentHashOf_RejectsInvalidJSON(t *testing.T) {
	_, err := ContentHashOf([]byte("not json"))
	assert.Error(t, err)
}

// ──────────────────────────────────────────────────────────────────────
// SaveRawIngest — argument validation (no DB required, fails before pool use)
// ──────────────────────────────────────────────────────────────────────

// TestSaveRawIngest_RequiresContentHash verifies the immutability contract's
// precondition: without a content_hash there is no evidence coordinate.
// The empty store is intentional — both cases return before touching the pool.
func TestSaveRawIngest_RequiresContentHash(t *testing.T) {
	ctx := context.Background()
	store := &PostgresStore{}

	assert.ErrorIs(t, store.SaveRawIngest(ctx, &RawIngest{}), ErrEmptyContentHash)
	assert.Error(t, store.SaveRawIngest(ctx, nil), "nil record must be rejected")
}

// ──────────────────────────────────────────────────────────────────────
// SaveRawIngest / GetRawIngest — DB-backed
// ──────────────────────────────────────────────────────────────────────

const rawIngestRoundTripPayload = `{"instrument":"000001.SZ","period":"2024Q1","revenue":123456789,"_probe":"ingest-raw-roundtrip-v1"}`

// TestSaveRawIngest_GetRawIngest_RoundTrip verifies the evidence coordinate is
// stable across a second write. ingest.raw is append-only: re-ingesting the
// same source response must not rewrite the archived row (so the original
// source/dataset attribution survives).
func TestSaveRawIngest_GetRawIngest_RoundTrip(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	hash, err := ContentHashOf([]byte(rawIngestRoundTripPayload))
	require.NoError(t, err)

	asOf := time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC)
	first := &RawIngest{
		ContentHash: hash,
		Source:      "unit-test-first",
		Dataset:     "fundamentals.income",
		Key:         "000001.SZ/2024Q1",
		AsOf:        &asOf,
		Payload:     json.RawMessage(rawIngestRoundTripPayload),
	}
	require.NoError(t, store.SaveRawIngest(ctx, first))
	require.False(t, first.FetchedAt.IsZero(), "FetchedAt should be defaulted on save")

	// Second write with the same content but different attribution: the
	// ON CONFLICT DO NOTHING clause must leave the first row in place.
	second := &RawIngest{
		ContentHash: hash,
		Source:      "unit-test-second",
		Dataset:     "fundamentals.income",
		Key:         "000001.SZ/2024Q1",
		Payload:     json.RawMessage(rawIngestRoundTripPayload),
	}
	require.NoError(t, store.SaveRawIngest(ctx, second), "duplicate content must be a no-op, not an error")

	got, err := store.GetRawIngest(ctx, hash)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, hash, got.ContentHash)
	assert.Equal(t, "unit-test-first", got.Source, "archived row must be immutable")
	assert.Equal(t, "fundamentals.income", got.Dataset)
	assert.Equal(t, "000001.SZ/2024Q1", got.Key)
	require.NotNil(t, got.AsOf)
	assert.True(t, asOf.Equal(*got.AsOf))
	assert.JSONEq(t, rawIngestRoundTripPayload, string(got.Payload))
}

// TestGetRawIngest_UnknownHash_ReturnsNilNil verifies the "never ingested"
// signal the Evidence API maps to HTTP 404. Returning (nil, nil) rather than an
// error keeps "absent" distinct from "query failed".
func TestGetRawIngest_UnknownHash_ReturnsNilNil(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	got, err := store.GetRawIngest(context.Background(), strings.Repeat("0", 64))
	require.NoError(t, err)
	assert.Nil(t, got)
}