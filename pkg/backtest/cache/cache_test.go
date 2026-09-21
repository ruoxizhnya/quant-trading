package cache

// P1-16 (Sprint 6, ODR-013 CQ-001, ADR-020):
// CacheManager 独立单元测试 — 不依赖 Engine 全栈构造。
//
// 验证点：
//   1. NewCacheManager 立即 Publish 一个空 map snapshot
//   2. Load 替换底层 map 并 Publish
//   3. Warm fast-path：L1 已含所有 symbols → 跳过 bulk fetch
//   4. Warm slow-path：L1 缺数据 → 调用 providerFn → 排序 → Publish
//   5. Get 命中 L1 → 区间裁剪正确（binary search）
//   6. Get 未命中 → fallback 到 providerFn
//   7. DateRangeBounds edge cases（empty / partial / out-of-range）

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
)

// fakeProvider — 测试用 marketdata.Provider stub，记录 bulk / single 调用。
type fakeProvider struct {
	bulkCalls   [][]string
	singleCalls []string
	bulkData    map[string][]domain.OHLCV
	singleData  map[string][]domain.OHLCV
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		bulkData:   make(map[string][]domain.OHLCV),
		singleData: make(map[string][]domain.OHLCV),
	}
}

func (f *fakeProvider) Name() string                                { return "fake" }
func (f *fakeProvider) CheckConnectivity(ctx context.Context) error { return nil }
func (f *fakeProvider) GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error) {
	return nil, nil
}
func (f *fakeProvider) GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	return nil, nil
}
func (f *fakeProvider) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}
func (f *fakeProvider) GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error) {
	return nil, nil
}
func (f *fakeProvider) BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	f.bulkCalls = append(f.bulkCalls, symbols)
	result := make(map[string][]domain.OHLCV)
	for _, s := range symbols {
		if bars, ok := f.bulkData[s]; ok {
			result[s] = bars
		}
	}
	return result, nil
}

func (f *fakeProvider) GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	f.singleCalls = append(f.singleCalls, symbol)
	return f.singleData[symbol], nil
}

func (f *fakeProvider) GetTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error) {
	return nil, nil
}
func (f *fakeProvider) GetStock(ctx context.Context, symbol string) (domain.Stock, error) {
	return domain.Stock{}, nil
}
func (f *fakeProvider) CheckCalendarExists(ctx context.Context, start, end time.Time) (bool, error) {
	return true, nil
}

// Compile-time check
var _ marketdata.Provider = (*fakeProvider)(nil)

func newTestCacheManager() *CacheManager {
	return NewCacheManager(zerolog.New(nil))
}

func makeBars(symbol string, dates ...time.Time) []domain.OHLCV {
	bars := make([]domain.OHLCV, len(dates))
	for i, d := range dates {
		bars[i] = domain.OHLCV{
			Symbol: symbol,
			Date:   d,
			Open:   10.0,
			High:   11.0,
			Low:    9.0,
			Close:  10.5,
			Volume: 1000,
		}
	}
	return bars
}

func TestCacheManager_NewPublishesEmptySnapshot(t *testing.T) {
	cm := newTestCacheManager()
	snap := cm.inMemoryOHLCVAtomic.Load()
	require.NotNil(t, snap, "NewCacheManager must publish an empty map snapshot so hot path is non-nil")
	assert.Empty(t, *snap, "fresh snapshot should be empty")
}

func TestCacheManager_Load(t *testing.T) {
	cm := newTestCacheManager()
	d1 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	data := map[string][]domain.OHLCV{
		"600000.SH": makeBars("600000.SH", d1),
	}
	cm.Load(data)
	snap := cm.inMemoryOHLCVAtomic.Load()
	require.NotNil(t, snap)
	assert.Len(t, *snap, 1)
	assert.Contains(t, *snap, "600000.SH")

	// Load(nil) clears
	cm.Load(nil)
	snap = cm.inMemoryOHLCVAtomic.Load()
	require.NotNil(t, snap)
	assert.Empty(t, *snap)
}

func TestCacheManager_Warm_FastPathSkipsBulkFetch(t *testing.T) {
	cm := newTestCacheManager()
	provider := newFakeProvider()

	// Pre-populate L1 covering [d1, d31]
	d1 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	d31 := d1.AddDate(0, 0, 30)
	cm.Load(map[string][]domain.OHLCV{
		"600000.SH": makeBars("600000.SH", d1, d31),
	})

	// 请求区间落在已 warm 范围内 → 跳过拉取
	err := cm.Warm(context.Background(),
		[]string{"600000.SH"},
		d1.AddDate(0, 0, 5), d1.AddDate(0, 0, 10),
		func() marketdata.Provider { return provider },
	)
	require.NoError(t, err)
	assert.Empty(t, provider.bulkCalls, "fast path must NOT call BulkLoadOHLCV when the range is already covered")
}

// 回归：Warm 的 fast-path 此前只检查 symbol 在不在缓存里，**完全不看日期范围**，
// 于是后续任何不同区间的请求都会命中 fast-path 而复用旧区间的数据。
// walk-forward 的 train→test（同一 runner 连续两次不同区间）与窗口之间（共享
// runner）都会踩到：拿到的不是"偏乐观"，而是错位数据或空集。
func TestCacheManager_Warm_DifferentRangeRefetches(t *testing.T) {
	cm := newTestCacheManager()
	provider := newFakeProvider()

	d1 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	d10 := d1.AddDate(0, 0, 9)
	d11 := d1.AddDate(0, 0, 10)
	d20 := d1.AddDate(0, 0, 19)

	// 第一次：warm [d1, d10]
	provider.bulkData["600000.SH"] = makeBars("600000.SH", d1, d10)
	require.NoError(t, cm.Warm(context.Background(), []string{"600000.SH"}, d1, d10,
		func() marketdata.Provider { return provider }))
	require.Len(t, provider.bulkCalls, 1)

	// 第二次：请求 [d11, d20]，与已 warm 区间不重叠 → 必须重新拉取
	provider.bulkData["600000.SH"] = makeBars("600000.SH", d11, d20)
	require.NoError(t, cm.Warm(context.Background(), []string{"600000.SH"}, d11, d20,
		func() marketdata.Provider { return provider }))
	assert.Len(t, provider.bulkCalls, 2, "range outside the warmed window MUST refetch")

	// 两个区间合并后都可用
	snap := cm.inMemoryOHLCVAtomic.Load()
	require.NotNil(t, snap)
	bars := (*snap)["600000.SH"]
	require.Len(t, bars, 4, "merged cache must contain both ranges (2 + 2 bars)")
	for i := 0; i < len(bars)-1; i++ {
		assert.True(t, bars[i].Date.Before(bars[i+1].Date), "merged bars must stay ascending")
	}

	// 且能按新区间正确取回，而不是拿回过期的 [d1, d10]
	got, err := cm.Get(context.Background(), "600000.SH", d11, d20,
		func() marketdata.Provider { return provider })
	require.NoError(t, err)
	require.Len(t, got, 2, "must return the d11-d20 bars, not the stale d1-d10 ones")
	assert.Equal(t, d11, got[0].Date)
}

// 回归：此前 Store(&cm.inMemoryOHLCV) 发布的是**同一个 map 字段的地址**，
// 快照没有任何隔离效果 —— 写者在 mu.Lock 内改这个 map，读者 lock-free 读的
// 还是同一个 map，构成并发 map 读写。后果不止 -race 报警，Go runtime 可能
// 直接 fatal (concurrent map read and map write)，且不可 recover。
func TestCacheManager_PublishedSnapshotIsIsolatedCopy(t *testing.T) {
	cm := newTestCacheManager()

	d1 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	cm.Load(map[string][]domain.OHLCV{"600000.SH": makeBars("600000.SH", d1)})
	snap1 := cm.inMemoryOHLCVAtomic.Load()

	d2 := time.Date(2023, 1, 4, 0, 0, 0, 0, time.UTC)
	cm.Load(map[string][]domain.OHLCV{"600001.SH": makeBars("600001.SH", d2)})
	snap2 := cm.inMemoryOHLCVAtomic.Load()

	assert.Len(t, *snap1, 1, "previously published snapshot must not be mutated by later writes")
	assert.Contains(t, *snap1, "600000.SH")
	assert.Len(t, *snap2, 1)
	assert.Contains(t, *snap2, "600001.SH")
}

func TestCacheManager_Warm_SlowPathFetchesAndSorts(t *testing.T) {
	cm := newTestCacheManager()
	provider := newFakeProvider()

	// Provider returns out-of-order bars; Warm must sort them.
	d1 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2023, 1, 4, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2023, 1, 5, 0, 0, 0, 0, time.UTC)
	provider.bulkData["600000.SH"] = []domain.OHLCV{
		makeBars("600000.SH", d3)[0],
		makeBars("600000.SH", d1)[0],
		makeBars("600000.SH", d2)[0],
	}

	err := cm.Warm(context.Background(),
		[]string{"600000.SH"},
		d1, d3,
		func() marketdata.Provider { return provider },
	)
	require.NoError(t, err)
	require.Len(t, provider.bulkCalls, 1)

	snap := cm.inMemoryOHLCVAtomic.Load()
	require.NotNil(t, snap)
	bars := (*snap)["600000.SH"]
	require.Len(t, bars, 3)
	// Verify ascending order
	for i := 0; i < len(bars)-1; i++ {
		assert.True(t, bars[i].Date.Before(bars[i+1].Date) || bars[i].Date.Equal(bars[i+1].Date),
			"bars must be sorted ascending by date")
	}
}

func TestCacheManager_Get_HitReturnsRangeClipped(t *testing.T) {
	cm := newTestCacheManager()
	provider := newFakeProvider()

	d1 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2023, 1, 4, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2023, 1, 5, 0, 0, 0, 0, time.UTC)
	cm.Load(map[string][]domain.OHLCV{
		"600000.SH": makeBars("600000.SH", d1, d2, d3),
	})

	// Request only d2
	got, err := cm.Get(context.Background(), "600000.SH", d2, d2,
		func() marketdata.Provider { return provider })
	require.NoError(t, err)
	assert.Len(t, got, 1, "should clip to single bar d2")
	assert.Equal(t, d2, got[0].Date)

	assert.Empty(t, provider.singleCalls, "L1 hit must NOT call GetOHLCV")
}

func TestCacheManager_Get_MissFallsBackToProvider(t *testing.T) {
	cm := newTestCacheManager()
	provider := newFakeProvider()
	d1 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	provider.singleData["600001.SH"] = makeBars("600001.SH", d1)

	got, err := cm.Get(context.Background(), "600001.SH", d1, d1,
		func() marketdata.Provider { return provider })
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "600001.SH", got[0].Symbol)
	assert.Len(t, provider.singleCalls, 1, "L1 miss must call GetOHLCV exactly once")
}

func TestCacheManager_DateRangeBounds(t *testing.T) {
	d1 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2023, 1, 4, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2023, 1, 5, 0, 0, 0, 0, time.UTC)
	bars := makeBars("X", d1, d2, d3)

	// Whole range
	lo, hi := DateRangeBounds(bars, d1, d3)
	assert.Equal(t, 0, lo)
	assert.Equal(t, 2, hi)

	// Single day in middle
	lo, hi = DateRangeBounds(bars, d2, d2)
	assert.Equal(t, 1, lo)
	assert.Equal(t, 1, hi)

	// Empty bars
	lo, hi = DateRangeBounds(nil, d1, d3)
	assert.Equal(t, 0, lo)
	assert.Equal(t, -1, hi)

	// Out-of-range (start > all bars)
	lo, hi = DateRangeBounds(bars, d3.AddDate(0, 0, 1), d3.AddDate(0, 0, 2))
	assert.Equal(t, 3, lo, "no bar >= out-of-range start")
	assert.Equal(t, 2, hi)
}

// TestCacheManager_Peek (S7-P2-1) verifies the Peek method added when
// CacheManager moved to the cache/ subpackage. Peek returns the cached
// bars for a symbol without falling back to the provider on miss.
func TestCacheManager_Peek(t *testing.T) {
	cm := newTestCacheManager()
	d1 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2023, 1, 4, 0, 0, 0, 0, time.UTC)

	// Empty cache → Peek returns nil
	assert.Nil(t, cm.Peek("600000.SH"), "Peek on empty cache must return nil")

	// Load data → Peek returns the bars
	cm.Load(map[string][]domain.OHLCV{
		"600000.SH": makeBars("600000.SH", d1, d2),
	})
	got := cm.Peek("600000.SH")
	require.Len(t, got, 2)
	assert.Equal(t, d1, got[0].Date)
	assert.Equal(t, d2, got[1].Date)

	// Miss → nil (no provider fallback)
	assert.Nil(t, cm.Peek("NONEXISTENT"), "Peek on missing symbol must return nil, not call provider")
}

// TestCacheManager_Snapshot (S7-P2-1) verifies the Snapshot method
// added when CacheManager moved to the cache/ subpackage. Snapshot
// returns the full L1 cache map for test assertions without accessing
// the unexported inMemoryOHLCVAtomic field.
func TestCacheManager_Snapshot(t *testing.T) {
	cm := newTestCacheManager()

	// Fresh cache → Snapshot returns an empty map (NewCacheManager
	// publishes an empty map snapshot, never nil)
	snap := cm.Snapshot()
	require.NotNil(t, snap, "fresh cache Snapshot must not be nil")
	assert.Empty(t, snap)

	// Load data → Snapshot reflects it
	d1 := time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC)
	cm.Load(map[string][]domain.OHLCV{
		"600000.SH": makeBars("600000.SH", d1),
	})
	snap = cm.Snapshot()
	assert.Len(t, snap, 1)
	assert.Contains(t, snap, "600000.SH")

	// Clear → Snapshot is empty again
	cm.Load(nil)
	snap = cm.Snapshot()
	assert.NotNil(t, snap, "cleared cache Snapshot must not be nil (empty map)")
	assert.Empty(t, snap)
}
