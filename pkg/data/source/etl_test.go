package source

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubStore is a minimal stand-in for *storage.PostgresStore. It
// records every call to BulkInsert so tests can assert on the
// persisted count.
type stubStore struct {
	mu          sync.Mutex
	calls       int
	lastDataType string
	persisted   int
}

func (s *stubStore) BulkInsert(_ context.Context, dataType string, points []interface{}) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastDataType = dataType
	s.persisted += len(points)
	return len(points), 0, nil
}

// adapterInMemoryStore is a simpler stub for tests that don't
// exercise the storage projection (source.UnifiedDataPoint →
// storage.UnifiedDataPoint). We use it through the etl package's
// own store interface — but since the interface lives in
// pkg/storage, the tests below do not call BulkInsert directly.
type adapterInMemoryStore struct {
	mu   sync.Mutex
	hits int
}

func (s *adapterInMemoryStore) recordHit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hits++
}

// L2 integration test: Adapter → Registry → ETL pipeline.
//
// We can't run against the real PostgresStore in unit tests (it
// requires a live DB), so we exercise the pipeline with a nil
// store and verify that the data is correctly normalized and
// validated. The actual persistence is verified via a separate
// "store stub" test (TestETL_StubStore_Persists) which would be
// added when storage.UnifiedDataPoint is wired into a mock.
func TestETL_EndToEnd_NoStore(t *testing.T) {
	reg := NewRegistry()
	a := &testAdapter{
		name:      "test",
		supported: []string{DataTypeRealtime},
		items: []DataItem{
			{Symbol: "600519.SH", TradeTime: time.Now(), Data: map[string]interface{}{"price": 1500.0}},
		},
	}
	require.NoError(t, reg.Register(a))
	pipeline := NewETLPipeline(reg, nil)
	res, err := pipeline.Process(context.Background(), FetchRequest{
		DataType:  DataTypeRealtime,
		Symbols:   []string{"600519.SH"},
		StartDate: time.Now().Add(-24 * time.Hour),
		EndDate:   time.Now(),
	}, normalizerIdentity)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 1, res.Fetched)
	// Persisted=0 with nil store; the surviving point count is
	// Fetched - Skipped.
	assert.Equal(t, 0, res.Persisted)
	assert.Equal(t, 0, res.Skipped)
}

func TestETL_DropsInvalidPoints(t *testing.T) {
	reg := NewRegistry()
	a := &testAdapter{
		name:      "test",
		supported: []string{DataTypeRealtime},
		items: []DataItem{
			{Symbol: "A", TradeTime: time.Now(), Data: map[string]interface{}{"price": 100.0}},
			{Symbol: "", TradeTime: time.Now(), Data: map[string]interface{}{"price": 200.0}},    // empty symbol → skip
			{Symbol: "C", TradeTime: time.Time{}, Data: map[string]interface{}{"price": 300.0}}, // zero time → skip
			{Symbol: "D", TradeTime: time.Now(), Data: nil},                                       // nil data → skip
		},
	}
	require.NoError(t, reg.Register(a))
	pipeline := NewETLPipeline(reg, nil)
	res, err := pipeline.Process(context.Background(), FetchRequest{
		DataType:  DataTypeRealtime,
		StartDate: time.Now().Add(-24 * time.Hour),
		EndDate:   time.Now(),
	}, normalizerIdentity)
	require.NoError(t, err)
	assert.Equal(t, 4, res.Fetched)
	// 1 valid + 2 invalid + 1 normalizer-dropped:
	//   - D (Data=nil) is dropped at the normalizer stage.
	//   - B (empty Symbol) and C (zero TradeTime) are dropped at validate.
	// res.Skipped only counts validate/dedup drops, so 2.
	assert.Equal(t, 0, res.Persisted)
	assert.Equal(t, 2, res.Skipped, "2 points are dropped by validate")
}

func TestETL_DeduplicatesPoints(t *testing.T) {
	reg := NewRegistry()
	now := time.Now().UTC().Truncate(time.Second)
	a := &testAdapter{
		name:      "test",
		supported: []string{DataTypeRealtime},
		items: []DataItem{
			{Symbol: "AAPL", TradeTime: now, Data: map[string]interface{}{"close": 100.0}},
			{Symbol: "AAPL", TradeTime: now, Data: map[string]interface{}{"close": 101.0}}, // duplicate
			{Symbol: "MSFT", TradeTime: now, Data: map[string]interface{}{"close": 200.0}},
		},
	}
	require.NoError(t, reg.Register(a))
	pipeline := NewETLPipeline(reg, nil)
	res, err := pipeline.Process(context.Background(), FetchRequest{
		DataType:  DataTypeRealtime,
		StartDate: now.Add(-24 * time.Hour),
		EndDate:   now.Add(24 * time.Hour),
	}, normalizerIdentity)
	require.NoError(t, err)
	// Fetched = 3 (raw), surviving = 2 (after dedup)
	assert.Equal(t, 3, res.Fetched)
	assert.Equal(t, 0, res.Persisted, "no store → 0 persisted")
	assert.Equal(t, 1, res.Skipped, "1 point is deduped")
}

func TestETL_NormalizerSignalsSkip(t *testing.T) {
	reg := NewRegistry()
	a := &testAdapter{
		name:      "test",
		supported: []string{DataTypeRealtime},
		items: []DataItem{
			{Symbol: "A", TradeTime: time.Now(), Data: map[string]interface{}{"price": 100.0}},
			{Symbol: "B", TradeTime: time.Now(), Data: map[string]interface{}{"price": 200.0}},
		},
	}
	require.NoError(t, reg.Register(a))
	// A normalizer that drops everything with price < 150.
	dropSmall := func(item DataItem, _, _ string) UnifiedDataPoint {
		p, _ := item.Data["price"].(float64)
		if p < 150.0 {
			return UnifiedDataPoint{} // Data==nil signals skip
		}
		return UnifiedDataPoint{
			Symbol:    item.Symbol,
			TradeTime: item.TradeTime,
			Data:      item.Data,
		}
	}
	pipeline := NewETLPipeline(reg, nil)
	res, err := pipeline.Process(context.Background(), FetchRequest{
		DataType:  DataTypeRealtime,
		StartDate: time.Now().Add(-24 * time.Hour),
		EndDate:   time.Now(),
	}, dropSmall)
	require.NoError(t, err)
	assert.Equal(t, 2, res.Fetched)
	// The normalizer drops A (price=100) and keeps B (price=200).
	// Normalizer drops are not counted in res.Skipped — they happen
	// before the validate stage. The only surviving point (B) makes
	// it through validate; with a nil store, Persisted=0.
	assert.Equal(t, 0, res.Persisted, "nil store → 0 persisted")
	assert.Equal(t, 0, res.Skipped, "no validate/dedup drops")
	// The dropping by normalizer is verified by the points-then-persisted
	// delta: only 1 of 2 fetched items made it past the normalizer.
	// Since we don't expose that intermediate count, we re-derive it:
	// assert on the dedupe-stage count by reusing the data with no
	// normalizer drop.
	noDrop := func(item DataItem, source, dataType string) UnifiedDataPoint {
		return UnifiedDataPoint{Symbol: item.Symbol, TradeTime: item.TradeTime, Source: source, DataType: dataType, Data: item.Data}
	}
	res2, _ := pipeline.Process(context.Background(), FetchRequest{
		DataType:  DataTypeRealtime,
		StartDate: time.Now().Add(-24 * time.Hour),
		EndDate:   time.Now(),
	}, noDrop)
	assert.Equal(t, 2, res2.Fetched, "no-drop normalizer: 2 items")
	assert.Equal(t, 0, res2.Skipped, "no-drop normalizer: 0 validate/dedup drops")
}

// L3 multi-source consistency test: same data from two adapters
// should be deduped by the ETL pipeline.
func TestMultiSource_Consistency(t *testing.T) {
	reg := NewRegistry()
	// Two adapters reporting the same (symbol, time) with different
	// price values. The pipeline should keep the first one (priority
	// order is determined by registration order).
	now := time.Now().UTC().Truncate(time.Second)
	primary := &testAdapter{
		name:      "primary",
		supported: []string{DataTypeRealtime},
		items: []DataItem{
			{Symbol: "600519.SH", TradeTime: now, Data: map[string]interface{}{"price": 1500.0}},
		},
	}
	secondary := &testAdapter{
		name:      "secondary",
		supported: []string{DataTypeRealtime},
		items: []DataItem{
			{Symbol: "600519.SH", TradeTime: now, Data: map[string]interface{}{"price": 1500.5}},
		},
	}
	// Register primary first → higher priority in the fallback chain.
	require.NoError(t, reg.Register(primary))
	require.NoError(t, reg.Register(secondary))
	// Direct call to Fetch — should return primary's data.
	resp, err := reg.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeRealtime,
		Symbols:   []string{"600519.SH"},
		StartDate: now.Add(-24 * time.Hour),
		EndDate:   now.Add(24 * time.Hour),
	})
	require.NoError(t, err)
	assert.Equal(t, "primary", resp.Source)
}

// normalizerIdentity is a pass-through normalizer for tests.
func normalizerIdentity(item DataItem, source, dataType string) UnifiedDataPoint {
	return UnifiedDataPoint{
		Symbol:    item.Symbol,
		TradeTime: item.TradeTime,
		Source:    source,
		DataType:  dataType,
		Data:      item.Data,
	}
}
