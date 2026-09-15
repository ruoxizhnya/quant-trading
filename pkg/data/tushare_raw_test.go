package data

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// recordingRawStore captures archived rows without a database. The embedded
// TushareStore satisfies the other eight interface methods; archiveRaw only
// ever calls SaveRawIngest, so an accidental call to any of them panics —
// which is the signal we want if that ever changes.
type recordingRawStore struct {
	TushareStore
	saved []*storage.RawIngest
}

func (s *recordingRawStore) SaveRawIngest(_ context.Context, record *storage.RawIngest) error {
	s.saved = append(s.saved, record)
	return nil
}

func newRawArchiveTestClient(store TushareStore) *TushareClient {
	return &TushareClient{logger: zerolog.Nop(), store: store}
}

// TestRawIngestKey_SortsParamsDeterministically pins the property that makes
// ingest.raw a stable citation coordinate: the same request replayed in a
// different order must render the same key.
func TestRawIngestKey_SortsParamsDeterministically(t *testing.T) {
	first := rawIngestKey("stk_factor_pro", map[string]interface{}{
		"ts_code":  "600519.SH",
		"end_date": "20240930",
	})
	second := rawIngestKey("stk_factor_pro", map[string]interface{}{
		"end_date": "20240930",
		"ts_code":  "600519.SH",
	})

	assert.Equal(t, first, second)
	assert.Equal(t, "stk_factor_pro:end_date=20240930&ts_code=600519.SH", first)
}

// TestRawIngestKey_DropsEmptyValues verifies that "not requested" and
// "requested as empty" collapse onto the same key, and that a request with no
// usable parameter is keyed by the API name alone.
func TestRawIngestKey_DropsEmptyValues(t *testing.T) {
	assert.Equal(t, "daily", rawIngestKey("daily", nil))
	assert.Equal(t, "daily", rawIngestKey("daily", map[string]interface{}{}))
	assert.Equal(t, "daily", rawIngestKey("daily", map[string]interface{}{"start_date": "", "end_date": ""}))
	assert.Equal(t, "daily:start_date=20240101", rawIngestKey("daily", map[string]interface{}{
		"start_date": "20240101",
		"end_date":   "",
	}))
}

// TestArchiveRaw_ArchivesVerbatimBody is the L0-1 exit criterion at the
// tushare end: a raw response fetched through the client lands in ingest.raw
// as one attributable row carrying the hash a citation must quote.
func TestArchiveRaw_ArchivesVerbatimBody(t *testing.T) {
	store := &recordingRawStore{}
	client := newRawArchiveTestClient(store)

	const body = `{"code":0,"msg":"","data":{"fields":["ts_code","close"],"items":[["600519.SH",1688.0]]}}`
	client.archiveRaw(context.Background(), "stk_factor_pro",
		map[string]interface{}{"ts_code": "600519.SH"}, []byte(body))

	require.Len(t, store.saved, 1)
	got := store.saved[0]

	wantHash, err := storage.ContentHashOf([]byte(body))
	require.NoError(t, err)
	assert.Equal(t, wantHash, got.ContentHash)
	assert.Equal(t, "tushare", got.Source)
	assert.Equal(t, "tushare.stk_factor_pro", got.Dataset)
	assert.Equal(t, "stk_factor_pro:ts_code=600519.SH", got.Key)
	assert.Equal(t, body, string(got.Payload), "the archived payload must be the verbatim source response")
	assert.Nil(t, got.AsOf, "one response may span thousands of trading days, so it has no single as_of")
	assert.False(t, got.FetchedAt.IsZero())
}

// TestArchiveRaw_SameContentDifferentFormatting_SameHash documents the
// idempotency that ContentHashOf gives the archive: a replayed response whose
// key order or whitespace changed still collapses onto the original row.
func TestArchiveRaw_SameContentDifferentFormatting_SameHash(t *testing.T) {
	store := &recordingRawStore{}
	client := newRawArchiveTestClient(store)

	client.archiveRaw(context.Background(), "daily", nil, []byte(`{"a":1,"b":2}`))
	client.archiveRaw(context.Background(), "daily", nil, []byte("{\n  \"b\": 2,\n  \"a\": 1\n}"))

	require.Len(t, store.saved, 2)
	assert.Equal(t, store.saved[0].ContentHash, store.saved[1].ContentHash)
	assert.Equal(t, "daily", store.saved[0].Key)
}

// TestArchiveRaw_SkipsUnusableInput verifies archiving is best-effort: no
// store, no body, or a body that is not JSON must be skipped silently rather
// than fail the fetch they describe.
func TestArchiveRaw_SkipsUnusableInput(t *testing.T) {
	recording := &recordingRawStore{}
	cases := []struct {
		name  string
		store TushareStore
		body  string
	}{
		{"nil store", nil, `{"a":1}`},
		{"empty body", recording, ""},
		{"non-JSON body", recording, "<html>upstream proxy error</html>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newRawArchiveTestClient(tc.store)
			assert.NotPanics(t, func() {
				client.archiveRaw(context.Background(), "daily", nil, []byte(tc.body))
			})
		})
	}
	assert.Empty(t, recording.saved)
}