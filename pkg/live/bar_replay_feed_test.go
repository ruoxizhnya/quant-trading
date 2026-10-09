package live

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeReplayProvider 只实现 BarReplayFeed 需要的 BulkLoadOHLCV。
type fakeReplayProvider struct {
	bars map[string][]domain.OHLCV
	// calls 记录 BulkLoadOHLCV 被调用次数 —— 用来钉死「一次物化、禁 per-bar」。
	calls int
}

func (p *fakeReplayProvider) BulkLoadOHLCV(_ context.Context, symbols []string, _, _ time.Time) (map[string][]domain.OHLCV, error) {
	p.calls++
	out := map[string][]domain.OHLCV{}
	for _, s := range symbols {
		out[s] = p.bars[s]
	}
	return out, nil
}

func mkBar(sym string, year int, month time.Month, day int, close, volume float64) domain.OHLCV {
	return domain.OHLCV{
		Symbol: sym,
		Date:   time.Date(year, month, day, 0, 0, 0, 0, time.UTC),
		Open:   close - 0.5,
		High:   close + 1,
		Low:    close - 1,
		Close:  close,
		Volume: volume,
	}
}

// TestBarReplayFeed_Deterministic 是回放确定性的**核心正证据**：同一输入
// 两次 Replay，收到的 (symbol,date,o/h/l/c/v) 序列逐根一致，且顺序为
// (交易日升序 × symbol 字典序)。
func TestBarReplayFeed_Deterministic(t *testing.T) {
	d1 := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	prov := &fakeReplayProvider{bars: map[string][]domain.OHLCV{
		// 故意让 map 插入序与字典序相反，验证 feed 不依赖 map 遍历序。
		"600000.SH": {
			mkBar("600000.SH", 2026, 1, 5, 10, 100000),
			mkBar("600000.SH", 2026, 1, 6, 11, 110000),
			mkBar("600000.SH", 2026, 1, 7, 12, 120000),
		},
		"000001.SZ": {
			mkBar("000001.SZ", 2026, 1, 5, 20, 200000),
			mkBar("000001.SZ", 2026, 1, 6, 21, 210000),
			mkBar("000001.SZ", 2026, 1, 7, 22, 220000),
		},
	}}
	symbols := []string{"600000.SH", "000001.SZ"} // 非字典序传入

	collect := func() []marketdata.Quote {
		feed := NewBarReplayFeed(prov, clock.NewVirtualClock(d1))
		var got []marketdata.Quote
		err := feed.Replay(context.Background(), symbols, d1, time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC), func(q marketdata.Quote) error {
			got = append(got, q)
			return nil
		})
		require.NoError(t, err)
		return got
	}

	first := collect()
	second := collect()

	require.NotEmpty(t, first)
	assert.Equal(t, first, second, "同一输入两次 Replay 必须逐根一致")

	// 顺序断言：(date 升序, symbol 字典序)。
	for i, q := range first {
		t.Logf("bar[%d] symbol=%s date=%s close=%.2f vol=%d",
			i, q.Symbol, q.Timestamp.Format("2006-01-02"), q.Close, q.Volume)
	}
	wantOrder := []string{
		"000001.SZ", "600000.SH", // 01-05
		"000001.SZ", "600000.SH", // 01-06
		"000001.SZ", "600000.SH", // 01-07
	}
	gotOrder := make([]string, len(first))
	for i, q := range first {
		gotOrder[i] = q.Symbol
	}
	assert.Equal(t, wantOrder, gotOrder, "喂序必须是 交易日升序 × symbol 字典序")

	// 一次物化：BulkLoadOHLCV 只该被调一次（两次 Replay 共 2 次）。
	assert.Equal(t, 2, prov.calls, "每次 Replay 只物化一次，禁 per-bar 取数")

	// Bid/Ask 裁决：日线无盘口 → Bid=Ask=Close。
	for _, q := range first {
		assert.Equal(t, q.Close, q.Bid)
		assert.Equal(t, q.Close, q.Ask)
	}
}

// TestBarReplayFeed_ClockRewindStops 是时间倒退护栏：时钟起点晚于首根 bar
// 时，Replay 必须返回包装了 clock.ErrTimeRewind 的 error，且**不喂任何后续
// bar**。
//
// 说明：Replay 先按 (date 升序, symbol 字典序) 排序，因此倒退只可能在
// 「时钟起点晚于最早一根 bar」时出现 —— 那时第一根 bar 即触发倒退，后续
// bar 一根都不喂（这正是「立即停止、不喂后续」的强形式）。
func TestBarReplayFeed_ClockRewindStops(t *testing.T) {
	d1 := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	prov := &fakeReplayProvider{bars: map[string][]domain.OHLCV{
		"000001.SZ": {
			mkBar("000001.SZ", 2026, 1, 5, 10, 100000),
			mkBar("000001.SZ", 2026, 1, 6, 11, 110000),
			mkBar("000001.SZ", 2026, 1, 7, 12, 120000),
		},
	}}

	// 时钟预推进到 01-06：再回放 01-05 的 bar 即构成倒退。
	clk := clock.NewVirtualClock(time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC))
	feed := NewBarReplayFeed(prov, clk)

	fed := 0
	err := feed.Replay(context.Background(), []string{"000001.SZ"}, d1,
		time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC),
		func(marketdata.Quote) error { fed++; return nil })

	require.Error(t, err, "时钟倒退必须 fail-loud")
	assert.True(t, errors.Is(err, clock.ErrTimeRewind),
		"error 必须可通过 errors.Is 判定为 ErrTimeRewind，实际: %v", err)
	assert.Zero(t, fed, "倒退后不得喂任何 bar（后续 bar 全部未喂）")
	t.Logf("倒退护栏：err=%v, fed=%d", err, fed)
}

// TestBarReplayFeed_EmptySymbolsFailLoud 空标的集合直接报错，而不是静默
// 产出零 bar（那会读成「没数据」）。
func TestBarReplayFeed_EmptySymbolsFailLoud(t *testing.T) {
	feed := NewBarReplayFeed(&fakeReplayProvider{}, clock.NewVirtualClock(time.Now()))
	err := feed.Replay(context.Background(), nil, time.Now(), time.Now(), func(marketdata.Quote) error { return nil })
	require.Error(t, err)
}

// TestBarReplayFeed_ImplementsDataFeed 编译期 + 运行期同时确认实现 DataFeed。
func TestBarReplayFeed_ImplementsDataFeed(t *testing.T) {
	var df DataFeed = NewBarReplayFeed(&fakeReplayProvider{}, clock.NewVirtualClock(time.Now()))
	require.NoError(t, df.Subscribe([]string{"000001.SZ"}))
	_, err := df.GetQuote("000001.SZ")
	require.Error(t, err, "未回放的 symbol 无快照 → error（不是零值 Quote）")
	require.NoError(t, df.Unsubscribe([]string{"000001.SZ"}))
}
