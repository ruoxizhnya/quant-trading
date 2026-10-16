package storage

// 市场活跃度观测（D5，Migration 056 / market_activity）的测试。
//
// ─── 真库测试纪律（照 macro_test.go，红线区） ────────────────────────────
//  1. **自己的数据自己造、自己验、自己删**：每个用例独享一个 source 标记
//     （itest-<uuid>），清理只 `DELETE ... WHERE source = $1`。
//     **绝不**无 WHERE 删表——本机 5432 的 quant_trading 是真库（AUD-62 教训）。
//  2. **库不在就红，不 skip**：真库用例连接失败即 t.Fatalf。
//  3. 时间戳取 2200 基准区，避开真实业务时间。

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// maCleanup 删 market_activity 里本标记的行（幂等）。
func maCleanup(t *testing.T, store *PostgresStore, source string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := store.DB().Exec(ctx, "DELETE FROM market_activity WHERE source = $1", source); err != nil {
		t.Errorf("清理 market_activity 残留失败（source=%s，需手工检查）: %v", source, err)
	}
}

func maDay(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func maPtr(v float64) *float64 { return &v }

// TestMarketActivity_SaveAndReadRoundtrip 钉住读写往返 + 缺失语义：
// 非空值原样回读；nil 值以 NULL 落库并回读为 nil（**不是 0**）。
func TestMarketActivity_SaveAndReadRoundtrip(t *testing.T) {
	store := scNewStore(t)
	src := scSource()
	t.Cleanup(func() { maCleanup(t, store, src) })

	ctx := context.Background()
	rows := []*domain.MarketActivity{
		{Symbol: "600519.SH", TradeDate: maDay(2200, 3, 2), TurnoverRate: maPtr(1.23),
			TurnoverRateF: maPtr(1.5), VolumeRatio: maPtr(0.9), Source: src, AsOf: time.Now().UTC()},
		{Symbol: "000001.SZ", TradeDate: maDay(2200, 3, 2), TurnoverRate: nil,
			TurnoverRateF: nil, VolumeRatio: nil, Source: src, AsOf: time.Now().UTC()},
	}
	require.NoError(t, store.SaveMarketActivityBatch(ctx, rows))

	got, err := store.GetMarketActivityForDateRange(ctx, maDay(2200, 3, 1), maDay(2200, 3, 3))
	require.NoError(t, err)
	// 过滤本标记的行（真库里可能有其它数据）。
	var mine []domain.MarketActivity
	for _, r := range got {
		if r.Source == src {
			mine = append(mine, r)
		}
	}
	require.Len(t, mine, 2, "两行都应回读")
	require.Equal(t, "000001.SZ", mine[0].Symbol, "按 (symbol, trade_date) 升序")
	require.Nil(t, mine[0].TurnoverRate, "缺失≠0：NULL 回读为 nil，不是 0.0")
	require.Equal(t, "600519.SH", mine[1].Symbol)
	require.NotNil(t, mine[1].TurnoverRate)
	require.InDelta(t, 1.23, *mine[1].TurnoverRate, 1e-12)
	require.InDelta(t, 0.9, *mine[1].VolumeRatio, 1e-12)
}

// TestMarketActivity_UpsertIsIdempotentAndRefreshes 钉住幂等键语义：
// 同 (symbol, trade_date, source) 重导刷新取值而不插新行。
func TestMarketActivity_UpsertIsIdempotentAndRefreshes(t *testing.T) {
	store := scNewStore(t)
	src := scSource()
	t.Cleanup(func() { maCleanup(t, store, src) })

	ctx := context.Background()
	base := &domain.MarketActivity{Symbol: "600519.SH", TradeDate: maDay(2200, 3, 3),
		TurnoverRate: maPtr(1.0), Source: src, AsOf: time.Now().UTC()}
	require.NoError(t, store.SaveMarketActivityBatch(ctx, []*domain.MarketActivity{base}))

	// 上游重述修正：同键再写一次，取值变。
	revised := &domain.MarketActivity{Symbol: "600519.SH", TradeDate: maDay(2200, 3, 3),
		TurnoverRate: maPtr(2.5), Source: src, AsOf: time.Now().UTC()}
	require.NoError(t, store.SaveMarketActivityBatch(ctx, []*domain.MarketActivity{revised}))

	var n int
	require.NoError(t, store.DB().QueryRow(ctx,
		"SELECT count(*) FROM market_activity WHERE source = $1", src).Scan(&n))
	require.Equal(t, 1, n, "同键重导不插新行（幂等）")

	got, err := store.GetMarketActivityForDateRange(ctx, maDay(2200, 3, 3), maDay(2200, 3, 3))
	require.NoError(t, err)
	for _, r := range got {
		if r.Source == src {
			require.NotNil(t, r.TurnoverRate)
			require.InDelta(t, 2.5, *r.TurnoverRate, 1e-12, "同键重导刷新取值")
		}
	}
}

// TestMarketActivity_RangeQueryIsPITBounded 是 PIT 的取数侧执法点：
// 晚于 end 的行结构上不可见（这正是 after_close 声明的执行面——「收盘后
// 可见」的数据在计算日之前不可能出现）。
//
// 破坏腿（改坏 → 红 → 还原 → 绿）：把 GetMarketActivityForDateRange 的
// `trade_date <= $2` 改成 `>=`（或去掉上界）→ 本测试红。
func TestMarketActivity_RangeQueryIsPITBounded(t *testing.T) {
	store := scNewStore(t)
	src := scSource()
	t.Cleanup(func() { maCleanup(t, store, src) })

	ctx := context.Background()
	rows := []*domain.MarketActivity{
		{Symbol: "PIT.SH", TradeDate: maDay(2200, 3, 4), TurnoverRate: maPtr(1.0), Source: src, AsOf: time.Now().UTC()},
		{Symbol: "PIT.SH", TradeDate: maDay(2200, 3, 5), TurnoverRate: maPtr(9.9), Source: src, AsOf: time.Now().UTC()},
		{Symbol: "PIT.SH", TradeDate: maDay(2200, 3, 6), TurnoverRate: maPtr(9.9), Source: src, AsOf: time.Now().UTC()},
	}
	require.NoError(t, store.SaveMarketActivityBatch(ctx, rows))

	// 计算日 = 03-05：只能看到 03-04 与 03-05，03-06（未来）必须不可见。
	got, err := store.GetMarketActivityForDateRange(ctx, maDay(2200, 3, 1), maDay(2200, 3, 5))
	require.NoError(t, err)
	var mine []domain.MarketActivity
	for _, r := range got {
		if r.Source == src {
			mine = append(mine, r)
		}
	}
	require.Len(t, mine, 2, "end 之后的行（03-06）必须被上界挡在门外")
	for _, r := range mine {
		require.False(t, r.TradeDate.After(maDay(2200, 3, 5)), "回读行不得晚于 end：%s", r.TradeDate)
	}

	// 反向区间 → 参数错误（fail-loud，不静默返回空）。
	_, err = store.GetMarketActivityForDateRange(ctx, maDay(2200, 3, 5), maDay(2200, 3, 1))
	require.Error(t, err)
}

// TestMarketActivity_RejectsDirtyRows 钉住写入面校验：身份列缺失 / 零值
// 日期 / NaN 一律拒绝（脏行不进库）。
func TestMarketActivity_RejectsDirtyRows(t *testing.T) {
	store := scNewStore(t)
	ctx := context.Background()
	src := scSource()

	bad := []struct {
		name string
		row  *domain.MarketActivity
	}{
		{"空 symbol", &domain.MarketActivity{Symbol: "  ", TradeDate: maDay(2200, 3, 7), Source: src, AsOf: time.Now().UTC()}},
		{"空 source", &domain.MarketActivity{Symbol: "X.SH", TradeDate: maDay(2200, 3, 7), Source: "", AsOf: time.Now().UTC()}},
		{"零 trade_date", &domain.MarketActivity{Symbol: "X.SH", TradeDate: time.Time{}, Source: src, AsOf: time.Now().UTC()}},
		{"零 as_of", &domain.MarketActivity{Symbol: "X.SH", TradeDate: maDay(2200, 3, 7), Source: src}},
		{"NaN 换手率", &domain.MarketActivity{Symbol: "X.SH", TradeDate: maDay(2200, 3, 7),
			TurnoverRate: maPtr(math.NaN()), Source: src, AsOf: time.Now().UTC()}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, store.SaveMarketActivityBatch(ctx, []*domain.MarketActivity{tc.row}),
				"脏行必须被拒")
		})
	}
	// 一条都没进库（用本标记计数——这些行都没写进去，所以候选 source 也
	// 不会有残留；空批与全拒都不产生行）。
	got, err := store.GetMarketActivityForDateRange(ctx, maDay(2200, 3, 1), maDay(2200, 3, 8))
	require.NoError(t, err)
	for _, r := range got {
		assert.NotEqual(t, src, r.Source, "被拒的行不应出现在库里")
	}
}
