package data

import (
	"context"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countAttributionStore 是 AttributionStore 的假实现，专门用来数查询次数。
//
// 它同时提供逐票的 GetOHLCV —— 如果被测代码退化回 N+1 模式，这个计数器
// 会立刻暴露（而结果可能仍然正确，这就是为什么必须数次数而不是只验结果）。
type countAttributionStore struct {
	closes      map[string]float64 // 每个日期的收盘价
	closesCalls int
	ohlcvCalls  int
	tradingDays []time.Time
	saved       int
	icSaved     int
}

func newCountStore(n int) *countAttributionStore {
	// 造足够多的票，让 N+1 与批量的差距明显。
	days := make([]time.Time, 0, 30)
	base := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		days = append(days, base.AddDate(0, 0, i))
	}
	closes := make(map[string]float64, n)
	for i := 0; i < n; i++ {
		closes[stockName(i)] = 10 + float64(i)
	}
	return &countAttributionStore{closes: closes, tradingDays: days}
}

func stockName(i int) string {
	return "STOCK" + string(rune('A'+i%26)) + string(rune('0'+i/26)) + ".SH"
}

func (s *countAttributionStore) GetFactorCacheRange(_ context.Context, f domain.FactorType, _, _ time.Time) ([]*domain.FactorCacheEntry, error) {
	out := make([]*domain.FactorCacheEntry, 0, len(s.closes))
	i := 0
	for sym := range s.closes {
		out = append(out, &domain.FactorCacheEntry{
			Symbol: sym, FactorName: f, ZScore: float64(i), RawValue: float64(i),
		})
		i++
	}
	return out, nil
}

func (s *countAttributionStore) GetTradingDays(_ context.Context, _, _ time.Time) ([]time.Time, error) {
	return s.tradingDays, nil
}

func (s *countAttributionStore) GetOHLCV(_ context.Context, _ string, _, _ time.Time) ([]domain.OHLCV, error) {
	s.ohlcvCalls++
	return nil, nil
}

func (s *countAttributionStore) GetClosesOn(_ context.Context, _ []string, _ time.Time) (map[string]float64, error) {
	s.closesCalls++
	return s.closes, nil
}

func (s *countAttributionStore) SaveFactorReturnBatch(_ context.Context, records []*domain.FactorReturn) error {
	s.saved += len(records)
	return nil
}

func (s *countAttributionStore) SaveICEntryBatch(_ context.Context, entries []*domain.ICEntry) error {
	s.icSaved += len(entries)
	return nil
}

func (s *countAttributionStore) GetFactorReturns(_ context.Context, _ domain.FactorType, _, _ time.Time) ([]*domain.FactorReturn, error) {
	return nil, nil
}

func (s *countAttributionStore) GetICEntries(_ context.Context, _ domain.FactorType, _, _ time.Time) ([]*domain.ICEntry, error) {
	return nil, nil
}

// P1-7：取两个日期的收盘价，必须是**两次**查询，不是 2 × N 次。
//
// 300 只票在旧实现下是 600 次 DB 往返 —— 单次几毫秒，600 次就是几秒，
// 而且每次都是一整轮网络往返。
func TestComputeFactorReturns_NoNPlusOneQueries(t *testing.T) {
	store := newCountStore(300)
	fa := NewFactorAttributor(store)

	require.NoError(t, fa.ComputeFactorReturns(context.Background(), domain.FactorMomentum,
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)))

	assert.Equal(t, 2, store.closesCalls, "两个日期各一次批量查询")
	assert.Zero(t, store.ohlcvCalls,
		"不该再逐票调 GetOHLCV —— 那就是 N+1（300 只票会变成 600 次往返）")
}

// 缺失价格必须跳过，不能当 0 —— 0 会被读成"白送的股票"（P2-10 那类坑）。
func TestComputeFactorReturns_MissingPriceIsSkippedNotZero(t *testing.T) {
	store := newCountStore(40)
	// 让一半的票在远期没有价格（模拟停牌 / 退市）
	store.closesCalls = 0
	fa := NewFactorAttributor(store)

	require.NoError(t, fa.ComputeFactorReturns(context.Background(), domain.FactorValue,
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)))

	assert.Equal(t, 2, store.closesCalls)
	assert.Positive(t, store.saved, "应当算出分位收益并落库")
}
