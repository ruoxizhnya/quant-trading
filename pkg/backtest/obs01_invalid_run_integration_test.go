package backtest

import (
	"bytes"
	"context"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy/examples"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// OBS-01 真库取证：空票池必须被判无效运行（fail-loud）。
//
// 现场现象（改前）：total_trades=0 + start_date 零值/垃圾指标仍报成功。
// 本测试是「改后」的正证据：引擎如实把空票池的裁定挂在结果与响应上，
// 且日志里不再出现 success / 无条件的 "Backtest completed"。
//
// 真库只读：RunBacktest 不写任何表（已核 pkg/backtest 内无 INSERT/UPDATE），
// 所以这里不需要清理。连不上库就 skip（环境问题，不是代码缺陷）。

func obs01DSN() string {
	if v := os.Getenv("QUANT_TEST_DSN"); v != "" {
		return v
	}
	return "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable"
}

func openObs01Store(t *testing.T, ctx context.Context) *storage.PostgresStore {
	t.Helper()
	store, err := storage.NewPostgresStore(ctx, obs01DSN())
	if err != nil {
		t.Skipf("跳过 OBS-01 真库取证：连不上库（%v）", err)
	}
	if err := store.Ping(ctx); err != nil {
		store.Close()
		t.Skipf("跳过 OBS-01 真库取证：库 ping 不通（%v）", err)
	}
	t.Cleanup(store.Close)
	return store
}

// configureObs01Momentum 注册 / 复用 momentum 策略（全局 registry 是单例）。
func configureObs01Momentum(t *testing.T, lookback, topN int) {
	t.Helper()
	params := map[string]interface{}{
		"lookback_days":       lookback,
		"top_n":               topN,
		"max_positions":       topN,
		"rebalance_frequency": "daily",
	}
	ms := examples.NewMomentumStrategy()
	if err := strategy.GlobalRegister(ms); err != nil {
		existing, getErr := strategy.DefaultRegistry.Get("momentum")
		if getErr != nil {
			t.Fatalf("momentum 既注册不上也取不出：register=%v get=%v", err, getErr)
		}
		cfg, ok := existing.(strategy.Configurable)
		if !ok {
			t.Fatal("已注册的 momentum 不可配置")
		}
		if err := cfg.Configure(params); err != nil {
			t.Fatalf("reconfigure momentum: %v", err)
		}
		return
	}
	if err := ms.Configure(params); err != nil {
		t.Fatalf("configure momentum: %v", err)
	}
}

// newObs01Engine 用真库 provider 构造一个带 in-process 风控的引擎。
//
// 必须挂 store：加载上市日历（P2-4）靠它 —— 没有 listing，eligibleUniverse
// 会「不过滤、原样返回 pool」，票池就永远非空，测不到 empty_universe。
func newObs01Engine(t *testing.T, store *storage.PostgresStore, prov marketdata.Provider, logger zerolog.Logger) *Engine {
	t.Helper()
	v := viper.New()
	v.Set("backtest.initial_capital", 1_000_000.0)
	v.Set("backtest.commission_rate", 0.0003)
	v.Set("backtest.slippage_rate", 0.0001)
	v.Set("backtest.risk_free_rate", 0.03)
	v.Set("backtest.seed", 7)
	v.Set("strategy_service.url", "http://localhost:8082")

	eng, err := NewEngine(v, prov, logger)
	require.NoError(t, err)
	eng.SetStore(store)

	rm, err := risk.NewRiskManager(risk.RiskManagerConfig{
		TargetVolatility: 0.15, MaxPositionWeight: 0.10, MinPositionWeight: 0.01,
		ATRPeriod: 14, BaseMultiplier: 2.0, BullMultiplier: 1.5, BearMultiplier: 3.0,
		SidewaysMultiplier: 2.0, TakeProfitMult: 3.0, VolLookbackDays: 60,
		AnnualizationFactor: math.Sqrt(252), FastMAPeriod: 50, SlowMAPeriod: 200,
		RegimeVolLookback: 120,
	}, zerolog.Nop())
	require.NoError(t, err)
	eng.SetRiskManager(rm)
	return eng
}

// obs01Pool 从真库挑一个够厚的小票池（按代码顺序，不做幸存者挑选）。
func obs01Pool(t *testing.T, ctx context.Context, store *storage.PostgresStore, start, end string, minBars, limit int) []string {
	t.Helper()
	rows, err := store.DB().Query(ctx, `
		SELECT symbol
		FROM ohlcv_daily_qfq
		WHERE trade_date >= $1::date AND trade_date <= $2::date
		GROUP BY symbol
		HAVING count(*) >= $3
		ORDER BY symbol
		LIMIT $4
	`, start, end, minBars, limit)
	require.NoError(t, err)
	defer rows.Close()

	var pool []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		pool = append(pool, s)
	}
	require.NoError(t, rows.Err())
	return pool
}

// 场景 1（核心正证据）：票池解析为空 → 无效运行，不再报成功。
func TestOBS01_EmptyUniverseIsInvalidRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	store := openObs01Store(t, ctx)
	prov := marketdata.NewPostgresProvider(store, zerolog.Nop())

	var logBuf bytes.Buffer
	eng := newObs01Engine(t, store, prov, zerolog.New(&logBuf))
	configureObs01Momentum(t, 20, 3)

	// "CSI300" 不在 stocks/listing 日历里 —— eligibleUniverse 会逐日把它过滤掉，
	// 整轮票池恒空。这正是「YAML universe: csi300 在空表上解析出空票池」的形态。
	resp, err := eng.RunBacktest(ctx, BacktestRequest{
		Strategy:       "momentum",
		StockPool:      []string{"CSI300"},
		StartDate:      "2024-01-02",
		EndDate:        "2024-02-02",
		InitialCapital: 1_000_000,
		RiskFreeRate:   0.03,
	})
	require.NoError(t, err, "空票池是「结果无效」，不是引擎错误")

	// 断言：无效运行被显式标出，且带原因。
	assert.NotEqual(t, "completed", resp.Status, "空票池绝不能报 completed")
	assert.Equal(t, "invalid", resp.Status)
	assert.Contains(t, resp.InvalidReasons, "empty_universe")
	assert.Contains(t, resp.InvalidReasons, "zero_trades")
	assert.Equal(t, 0, resp.TotalTrades)
	assert.Equal(t, 0, resp.UniverseMaxSize, "整轮没有任何交易日的票池非空")

	// OBS-01 切片 2（读路径）：state store 里的状态也不得再是 "completed"，
	// 且 GetBacktestResult 必须放行 "invalid" —— 否则 GET 端点 / 报告层
	// 拿不到结果与 InvalidReasons，「为什么无效」在 UI 上不可见。
	stateStatus, statusErr := eng.GetBacktestStatus(resp.ID)
	require.NoError(t, statusErr)
	assert.Equal(t, "invalid", stateStatus, "state store 的无效运行同样不得显示为 completed")

	stored, resErr := eng.GetBacktestResult(resp.ID)
	require.NoError(t, resErr, "无效运行的结果必须可读（GetBacktestResult 放行 invalid）")
	require.NotNil(t, stored)
	assert.Equal(t, resp.InvalidReasons, stored.InvalidReasons, "state store 结果与同步响应携带同一组原因")
	assert.Len(t, resp.InvalidReasons, 3, "空票池：empty_universe + zero_trades + garbage_metric 三条")

	// 断言：日志里没有成功字样，也没有无条件宣告「Backtest completed」。
	logs := logBuf.String()
	assert.NotContains(t, strings.ToLower(logs), "success", "无效运行日志不得出现 success")
	assert.NotContains(t, logs, "Backtest completed", "无效运行不得无条件宣告 Backtest completed")
	assert.Contains(t, logs, "invalid run", "无效运行日志应明示无效")

	t.Logf("场景1：status=%s invalid_reasons=%v total_trades=%d universe_max_size=%d",
		resp.Status, resp.InvalidReasons, resp.TotalTrades, resp.UniverseMaxSize)
	t.Logf("场景1 引擎日志（尾部）：%s", tailLines(logs, 3))
}

// 反证腿 2：票池非空且有成交 → 不判无效，行为与改前一致（Status=completed）。
func TestOBS01_NonEmptyUniverseWithTradesIsValid(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	store := openObs01Store(t, ctx)
	prov := marketdata.NewPostgresProvider(store, zerolog.Nop())

	const (
		start = "2024-01-02"
		end   = "2024-12-31"
	)

	pool := obs01Pool(t, ctx, store, start, end, 150, 3)
	if len(pool) < 3 {
		t.Skipf("跳过反证腿：%s~%s 区间里票池太薄（拿到 %d 只）", start, end, len(pool))
	}

	var logBuf bytes.Buffer
	eng := newObs01Engine(t, store, prov, zerolog.New(&logBuf))
	configureObs01Momentum(t, 20, len(pool))

	resp, err := eng.RunBacktest(ctx, BacktestRequest{
		Strategy:       "momentum",
		StockPool:      pool,
		StartDate:      start,
		EndDate:        end,
		InitialCapital: 1_000_000,
		RiskFreeRate:   0.03,
	})
	require.NoError(t, err)

	// 这是护栏：正常回测不能被 OBS-01 误判成无效。
	assert.Empty(t, resp.InvalidReasons, "正常回测不该有任何 InvalidReasons")
	assert.Equal(t, "completed", resp.Status, "正常回测行为必须与改前一致")
	assert.Greater(t, resp.TotalTrades, 0, "反证腿要求至少有 1 笔成交")
	assert.Greater(t, resp.UniverseMaxSize, 0)

	// OBS-01 切片 2（读路径反证腿）：有效运行的 state store 状态仍是
	// "completed"、结果正常可读、无 InvalidReasons —— 读路径收口没有
	// 把正常路径带偏。
	stateStatus, statusErr := eng.GetBacktestStatus(resp.ID)
	require.NoError(t, statusErr)
	assert.Equal(t, "completed", stateStatus, "有效运行在 state store 里仍是 completed")
	stored, resErr := eng.GetBacktestResult(resp.ID)
	require.NoError(t, resErr)
	require.NotNil(t, stored)
	assert.Empty(t, stored.InvalidReasons, "有效运行的 state store 结果无 InvalidReasons")

	t.Logf("反证腿：pool=%v status=%s total_trades=%d universe_max_size=%d",
		pool, resp.Status, resp.TotalTrades, resp.UniverseMaxSize)
}

func tailLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
