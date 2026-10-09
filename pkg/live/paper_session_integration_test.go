package live_test

import (
	"context"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPaperSession_RunAgainstRealDB 是 PaperSession.Run 的真库**只读**冒烟：
// 用真实的 marketdata.Provider 取「3 个交易日 × 3 只票」，跑一个简单的
// 逐日买入策略，断言产出非空且按 (Date, Symbol) 有序。
//
// 只读：全部走 GetTradingDays / BulkLoadOHLCV / SELECT，不写任何表。
// 库里没数据或连不上 → skip（环境未就绪，不是代码缺陷）。
func TestPaperSession_RunAgainstRealDB(t *testing.T) {
	ctx := context.Background()
	store := openReadOnlyStore(t, ctx)

	logger := zerolog.Nop()
	provider := marketdata.NewPostgresProvider(store, logger)

	// 取一段有数据的窗口（避开同步边界）。
	start := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC)
	days, err := provider.GetTradingDays(ctx, start, end)
	if err != nil || len(days) < 3 {
		t.Skipf("跳过：交易日历不足 3 天（err=%v, n=%d）", err, len(days))
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	days = days[:3]
	wStart, wEnd := days[0], days[2]

	symbols := pickSymbols(t, ctx, store, wStart, wEnd, 3)
	if len(symbols) < 1 {
		t.Skipf("跳过：窗口 %s~%s 无行情数据",
			wStart.Format("2006-01-02"), wEnd.Format("2006-01-02"))
	}
	t.Logf("真库窗口 %s ~ %s，票池=%v",
		wStart.Format("2006-01-02"), wEnd.Format("2006-01-02"), symbols)

	trader := live.NewMockTrader(live.MockTraderConfig{
		InitialCash:   1_000_000_000,
		SlippageModel: "fixed",
	}, logger)

	sess, err := live.NewPaperSession(live.PaperSessionConfig{
		Provider: provider,
		Clock:    clock.NewVirtualClock(wStart),
		Trader:   trader,
		Logger:   logger,
	})
	require.NoError(t, err)

	fills, err := sess.Run(ctx, symbols, wStart, wEnd, &buyEverythingStrategy{BaseStrategy: strategy.NewBaseStrategy("buy_everything", "K5 smoke")})
	require.NoError(t, err)
	require.NotEmpty(t, fills, "真库回放应产出非空成交流")

	for i, f := range fills {
		t.Logf("fill[%d] %s %s side=%s qty=%.0f price=%.4f fee=%.4f",
			i, f.Date.Format("2006-01-02"), f.Symbol, f.Side, f.Qty, f.Price, f.Fee)
		if i > 0 {
			prev, cur := fills[i-1], fills[i]
			if cur.Date.Equal(prev.Date) {
				assert.LessOrEqual(t, prev.Symbol, cur.Symbol, "同日 symbol 升序")
			} else {
				assert.True(t, prev.Date.Before(cur.Date), "日期升序")
			}
		}
		// 只读且确定性：成交价必为正、落在窗口内。
		assert.Greater(t, f.Price, 0.0)
		assert.False(t, f.Date.Before(wStart))
		assert.False(t, f.Date.After(wEnd))
	}
}

// buyEverythingStrategy 每个交易日对窗口内每只票买一手（strength 0.1 → 100 股）。
type buyEverythingStrategy struct {
	*strategy.BaseStrategy
}

func (b *buyEverythingStrategy) GenerateSignals(_ context.Context, bars map[string][]domain.OHLCV, _ *domain.Portfolio) ([]strategy.Signal, error) {
	syms := make([]string, 0, len(bars))
	for s := range bars {
		syms = append(syms, s)
	}
	sort.Strings(syms)
	sigs := make([]strategy.Signal, 0, len(syms))
	for _, s := range syms {
		sigs = append(sigs, strategy.Signal{
			Symbol: s, Action: "buy", Direction: domain.DirectionLong, Strength: 0.1,
		})
	}
	return sigs, nil
}

func (b *buyEverythingStrategy) Weight(strategy.Signal, float64) float64 { return 0.1 }

// openReadOnlyStore 按生产 env 命名连库（库名默认 quant_trading），连不上或
// 无数据则 skip。绝不写库。
func openReadOnlyStore(t *testing.T, ctx context.Context) *storage.PostgresStore {
	t.Helper()
	port := 5432
	if p, err := strconv.Atoi(os.Getenv("DATABASE_PORT")); err == nil && p > 0 {
		port = p
	}
	envOr := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	dsn, err := storage.BuildDSN(storage.DatabaseConfig{
		URL:      os.Getenv("DATABASE_URL"),
		Host:     envOr("DATABASE_HOST", "localhost"),
		Port:     port,
		User:     envOr("DATABASE_USER", "postgres"),
		Password: envOr("DATABASE_PASSWORD", "postgres"),
		Name:     envOr("DATABASE_NAME", "quant_trading"),
		SSLMode:  "disable",
	})
	require.NoError(t, err)

	store, err := storage.NewPostgresStore(ctx, dsn)
	if err != nil {
		t.Skipf("跳过：连不上库（%v）", err)
	}
	if err := store.Ping(ctx); err != nil {
		store.Close()
		t.Skipf("跳过：库 ping 不通（%v）", err)
	}
	t.Cleanup(store.Close)
	return store
}

// pickSymbols 选窗口内有行情的 n 只票（按代码顺序，不按完整度排序 —— 用完整度
// 排序等于用未来信息挑样本）。
func pickSymbols(t *testing.T, ctx context.Context, store *storage.PostgresStore, start, end time.Time, n int) []string {
	t.Helper()
	rows, err := store.DB().Query(ctx, `
		SELECT symbol
		FROM ohlcv_daily_qfq
		WHERE trade_date >= $1::date AND trade_date <= $2::date
		GROUP BY symbol
		ORDER BY symbol
		LIMIT $3`, start, end, n)
	if err != nil {
		t.Skipf("跳过：查询行情失败（%v）", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Skipf("跳过：扫描 symbol 失败（%v）", err)
		}
		out = append(out, s)
	}
	return out
}
