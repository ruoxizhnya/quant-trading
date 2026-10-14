package marketdata

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"
)

// 同一路由取 1d/15m/1m 集成测试（D1 验收⑤2）。
//
// 写法遵循 pkg/storage 的 testStore 惯例 + AUD-51 教训：**自灌自证自清理**——
// 不依赖库里预置数据（分钟表当前 0 行），DB 不可达则 Skip 不 Fail。
// 灌入的每根 bar 都是手算期望值的原料；15m 期望值是手算后写死的。

const (
	testDSN    = "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable"
	testSymbol = "D1TEST.SH"
)

type seedBar struct {
	ts              time.Time
	open, high, low float64
	close           float64
	volume          int64
	amount          float64
}

func d1SeedMinuteBars() []seedBar {
	return []seedBar{
		// 2024-01-02（全日）
		{cst(2024, 1, 2, 9, 25), 10.00, 10.05, 9.98, 10.02, 5000, 50100}, // 开盘集合竞价 → 09:30 bucket
		{cst(2024, 1, 2, 9, 30), 10.02, 10.08, 10.00, 10.06, 3000, 30200},
		{cst(2024, 1, 2, 9, 31), 10.06, 10.10, 10.04, 10.09, 2000, 20200},
		{cst(2024, 1, 2, 11, 15), 10.20, 10.25, 10.18, 10.22, 1000, 10200}, // 11:15 bucket（不跨午休）
		{cst(2024, 1, 2, 11, 29), 10.22, 10.30, 10.21, 10.28, 1500, 15400},
		{cst(2024, 1, 2, 13, 0), 10.30, 10.33, 10.29, 10.31, 1200, 12360}, // 13:00 bucket
		{cst(2024, 1, 2, 14, 57), 10.40, 10.45, 10.38, 10.44, 800, 8320},  // 收盘集合竞价 → 14:45 bucket
		// 2024-01-03（10:29 后半日停牌）
		{cst(2024, 1, 3, 9, 30), 11.00, 11.05, 10.95, 11.02, 1000, 11020},
		{cst(2024, 1, 3, 9, 46), 11.02, 11.10, 11.00, 11.08, 1100, 12100},
		{cst(2024, 1, 3, 10, 0), 11.08, 11.12, 11.05, 11.10, 900, 9990},
		{cst(2024, 1, 3, 10, 29), 11.10, 11.15, 11.09, 11.13, 1300, 14440},
	}
}

func d1SeedDailyBars() []seedBar {
	return []seedBar{
		{time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), 10.00, 10.45, 9.98, 10.44, 14500, 146600},
		{time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), 11.00, 11.15, 10.95, 11.13, 4300, 47550},
	}
}

func setupRouterDB(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Skipf("skipping integration test: cannot create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping integration test: cannot connect to DB: %v", err)
	}
	// 防御性清理：上次崩溃运行可能留了种子数据。
	_, _ = pool.Exec(ctx, "DELETE FROM ohlcv_minute WHERE symbol = $1", testSymbol)
	_, _ = pool.Exec(ctx, "DELETE FROM ohlcv_daily_qfq WHERE symbol = $1", testSymbol)
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, "DELETE FROM ohlcv_minute WHERE symbol = $1", testSymbol)
		_, _ = pool.Exec(cctx, "DELETE FROM ohlcv_daily_qfq WHERE symbol = $1", testSymbol)
		pool.Close()
	})
	return pool, ctx
}

func seedAll(t *testing.T, pool *pgxpool.Pool, ctx context.Context) {
	t.Helper()
	for _, b := range d1SeedMinuteBars() {
		_, err := pool.Exec(ctx, `
			INSERT INTO ohlcv_minute (symbol, ts, open, high, low, close, volume, amount)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			testSymbol, b.ts, b.open, b.high, b.low, b.close, b.volume, b.amount)
		require.NoError(t, err)
	}
	for _, b := range d1SeedDailyBars() {
		_, err := pool.Exec(ctx, `
			INSERT INTO ohlcv_daily_qfq (symbol, trade_date, open, high, low, close, volume, turnover)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			testSymbol, b.ts, b.open, b.high, b.low, b.close, int64(b.volume), b.amount)
		require.NoError(t, err)
	}
}

func TestRouterSameRouteThreeFrequencies(t *testing.T) {
	pool, ctx := setupRouterDB(t)
	seedAll(t, pool, ctx)
	r := NewRouter(pool)

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 5, 23, 59, 59, 0, time.UTC)

	// ── 1d 路由：取日线表，频率/时间语义字段必须显式声明 ──
	daily, err := r.Bars(ctx, market.Freq1d, testSymbol, start, end)
	require.NoError(t, err)
	require.Len(t, daily, 2, "1d 路由应取到 2 根种子日线")
	assert.Equal(t, market.Freq1d, daily[0].Frequency, "日线 bar 必须声明 1d（改错即红，D1 破坏验证①）")
	assert.True(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC).Equal(daily[0].Date))
	assert.Equal(t, 10.00, daily[0].Open)
	assert.Equal(t, 10.44, daily[0].Close)
	assert.True(t, cst(2024, 1, 2, 15, 0).Equal(daily[0].VisibleAt), "日线可见时间点 = 当日 15:00 收盘（CST）")

	// ── 1m 路由：原样取分钟表 ──
	minutes, err := r.Bars(ctx, market.Freq1m, testSymbol, start, end)
	require.NoError(t, err)
	require.Len(t, minutes, 11, "1m 路由应取到 11 根种子分钟 bar")
	assert.Equal(t, market.Freq1m, minutes[0].Frequency, "分钟 bar 必须声明 1m（改错即红）")
	assert.True(t, cst(2024, 1, 2, 9, 25).Equal(minutes[0].Date))
	assert.True(t, cst(2024, 1, 2, 9, 26).Equal(minutes[0].VisibleAt), "09:25 bar → 09:26 可见")
	assert.Equal(t, 10.02, minutes[0].Close, "09:25 集合竞价 bar 原样返回")
	assert.Equal(t, 10.06, minutes[1].Close, "09:30 bar 原样返回")

	// ── 15m 路由：同一 Router、同一入参形状，聚合产出 ──
	quarters, err := r.Bars(ctx, market.Freq15m, testSymbol, start, end)
	require.NoError(t, err)
	require.Len(t, quarters, 8, "15m 路由：两日共 8 个 bucket（4+4），空 bucket 不零填充")

	type wantBar struct {
		date                   time.Time
		open, high, low, close float64
		volume                 float64
		visibleAt              time.Time
	}
	wants := []wantBar{
		// 2024-01-02
		{cst(2024, 1, 2, 9, 30), 10.00, 10.10, 9.98, 10.09, 10000, cst(2024, 1, 2, 9, 45)},   // 含 09:25 竞价
		{cst(2024, 1, 2, 11, 15), 10.20, 10.30, 10.18, 10.28, 2500, cst(2024, 1, 2, 11, 30)}, // 午休断点
		{cst(2024, 1, 2, 13, 0), 10.30, 10.33, 10.29, 10.31, 1200, cst(2024, 1, 2, 13, 15)},  // 不与上午合并
		{cst(2024, 1, 2, 14, 45), 10.40, 10.45, 10.38, 10.44, 800, cst(2024, 1, 2, 15, 0)},   // 含 14:57 收盘竞价
		// 2024-01-03（半日停牌）
		{cst(2024, 1, 3, 9, 30), 11.00, 11.05, 10.95, 11.02, 1000, cst(2024, 1, 3, 9, 45)},
		{cst(2024, 1, 3, 9, 45), 11.02, 11.10, 11.00, 11.08, 1100, cst(2024, 1, 3, 10, 0)},
		{cst(2024, 1, 3, 10, 0), 11.08, 11.12, 11.05, 11.10, 900, cst(2024, 1, 3, 10, 15)},
		{cst(2024, 1, 3, 10, 15), 11.10, 11.15, 11.09, 11.13, 1300, cst(2024, 1, 3, 10, 30)},
	}
	for i, w := range wants {
		got := quarters[i]
		assert.Equal(t, market.Freq15m, got.Frequency, "bar[%d] 频率声明", i)
		assert.True(t, w.date.Equal(got.Date), "bar[%d] bucket 起点: want %s got %s", i, w.date, got.Date)
		assert.Equal(t, w.open, got.Open, "bar[%d] 开=首笔开", i)
		assert.Equal(t, w.high, got.High, "bar[%d] 高=期间最高", i)
		assert.Equal(t, w.low, got.Low, "bar[%d] 低=期间最低", i)
		assert.Equal(t, w.close, got.Close, "bar[%d] 收=末笔收", i)
		assert.Equal(t, w.volume, got.Volume, "bar[%d] 量=期间合计", i)
		assert.True(t, w.visibleAt.Equal(got.VisibleAt), "bar[%d] 可见时间点=bucket 结束: want %s got %s", i, w.visibleAt, got.VisibleAt)
	}

	// ── 口径一致：1m 路由输出再过聚合器 ≡ 15m 路由输出 ──
	reAgg, err := Aggregate(minutes, market.Freq15m)
	require.NoError(t, err)
	require.Len(t, reAgg, len(quarters))
	for i := range quarters {
		assert.True(t, quarters[i].Date.Equal(reAgg[i].Date), "口径一致[%d] Date", i)
		assert.Equal(t, quarters[i].Open, reAgg[i].Open, "口径一致[%d] Open", i)
		assert.Equal(t, quarters[i].High, reAgg[i].High, "口径一致[%d] High", i)
		assert.Equal(t, quarters[i].Low, reAgg[i].Low, "口径一致[%d] Low", i)
		assert.Equal(t, quarters[i].Close, reAgg[i].Close, "口径一致[%d] Close", i)
		assert.Equal(t, quarters[i].Volume, reAgg[i].Volume, "口径一致[%d] Volume", i)
	}

	// ── 未知频率 fail-loud ──
	_, err = r.Bars(ctx, market.Frequency("30m"), testSymbol, start, end)
	require.Error(t, err, "未支持的频率必须显式报错，不静默回退")
}

// TestRouterRegressionDailyUnchanged 回归红线：走 Router 的 1d 路由与
// 遗留 storage.GetOHLCV 读同一张表，数值逐字段相等 —— 改的是取数通道，
// 不是数据（D1 ③5）。
func TestRouterRegressionDailyUnchanged(t *testing.T) {
	pool, ctx := setupRouterDB(t)
	seedAll(t, pool, ctx)
	r := NewRouter(pool)

	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)

	rows, err := pool.Query(ctx, `
		SELECT symbol, trade_date, open, high, low, close, volume, turnover, trade_days
		FROM ohlcv_daily_qfq
		WHERE symbol = $1 AND trade_date >= $2 AND trade_date <= $3
		ORDER BY trade_date ASC`, testSymbol, start, end)
	require.NoError(t, err)
	var legacy []market.OHLCV
	for rows.Next() {
		var o market.OHLCV
		require.NoError(t, rows.Scan(&o.Symbol, &o.Date, &o.Open, &o.High, &o.Low, &o.Close, &o.Volume, &o.Turnover, &o.TradeDays))
		legacy = append(legacy, o)
	}
	require.NoError(t, rows.Err())
	rows.Close()

	routed, err := r.Bars(ctx, market.Freq1d, testSymbol, start, end)
	require.NoError(t, err)
	require.Len(t, routed, len(legacy))
	for i := range legacy {
		assert.Equal(t, legacy[i].Symbol, routed[i].Symbol)
		assert.True(t, legacy[i].Date.Equal(routed[i].Date))
		assert.Equal(t, legacy[i].Open, routed[i].Open)
		assert.Equal(t, legacy[i].High, routed[i].High)
		assert.Equal(t, legacy[i].Low, routed[i].Low)
		assert.Equal(t, legacy[i].Close, routed[i].Close)
		assert.Equal(t, legacy[i].Volume, routed[i].Volume)
		assert.Equal(t, legacy[i].Turnover, routed[i].Turnover)
		assert.Equal(t, legacy[i].TradeDays, routed[i].TradeDays)
	}
}
