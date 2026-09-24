package data

// P2-8 取证：**复权口径的前视成分到底漏不漏进回测结果**。
//
// 背景。库里存的是前复权（qfq）行情，而 qfq 的定义是
//
//	qfq(t) = raw(t) × f(t) / f(区间末日)
//
// 分母 f(末日) 是**回测区间之外的未来信息** —— 2024 年那根 K 线的价格里，
// 含着 2024~2026 全部除权事件的知识。TASKS.md 的 P2-8 行记过一条**分析**
// （不是实测）：「qfq 与 hfq 只差一个每股常数，故收益率序列完全相同，
// 前视只落在价格水平上，可能的影响只有 floor(现金/价格) 的取整」。
//
// 这个文件的任务就是把那条分析**实测证伪或证实**。它做三件事：
//
//  1. **重建 hfq 并自检**。从 `daily`（不复权）+ `adj_factor` 算出
//     hfq(t) = raw(t) × f(t)，再验算 raw×f/f(末日) 是否逐点等于**库里存的 qfq**。
//     不先过这一关，后面比的就是两串来路不明的数字。
//  2. **用真引擎跑两次**同一个策略、同一批票：一次喂 qfq，一次喂 hfq（只把
//     四个价格字段乘上每股常数，量/额不动 —— 复权本来就不动成交量）。
//  3. **报告差异**，并钉住结论。
//
// 为什么必须用真引擎而不是只比价格序列：前视要变成结果差异，得先有一个
// 「对价格水平敏感」的环节。代码里当时列了三个候选，现在都已判定：
//
//   - **下单取整** `floor(预算 / currentPrice)` 取整到整手（pkg/risk/manager.go）
//     —— ✅ **这是真通道**（绝对金额约束：一手 = 100 × 价格，而资金固定）；
//   - regime 判定（pkg/risk/regime.go）—— ❌ **已证伪**。它把**所有票、所有
//     日子**的价格拼成一根长序列再算均线，看着最像；但
//     pkg/risk/regime_scale_invariance_test.go 实测它对统一缩放 14/14 个窗口
//     严格不变、对每票各自缩放 14/14 个窗口判定完全一致（2026-09-24）。
//   - 市场冲击模型：**未接线**（internal/repoguard 的 unwiredPackages 里记着），
//     所以它不在候选里。
//
// 还有第四条不在当时的候选里、2026-09-24 才查出来的：**引擎对已持仓位重复
// 下单**（AUD-55，`computeEffectiveTarget` 的抵扣条件写窄了）。它让「订单 vs
// 现金」成为每日事件，于是结果对**任何**扰动都过敏 —— 见下面的更新说明。
//
// 依赖：真库（quant_trading）+ 真 Tushare token。两者缺一就 skip ——
// 那不是代码缺陷，是环境没就绪。testutil 的 quant_trading_test 连错库会
// 得到一个空库然后以「没数据」skip 掉、永远绿，所以这里显式连生产那个库。
//
// ═══════════════════════════════════════════════════════════════════════════
// 2026-09-24：AUD-55 已修，本文件的**数字需要重跑校准**（尚未重跑）
//
// 根因（AUD-55）：`computeEffectiveTarget` 只在 `PendingQty > 0` 时抵扣已持仓位，
// 配上一个**无状态**策略（momentum 每天对 top-N 重发 `Long`）→ 每天重发一次
// 全量买单、每天被 `insufficient cash` 拒一次。本文记录的「21.6pp / 24.9pp」
// 是**修复前**的读数 —— 而那个数在当时就已经被判为不可当稳定估计量读
// （它对资金量级同样敏感、且不收敛）。
//
// 修复后在合成数据上的形态（见 pkg/backtest/scale_invariance_probe_test.go 文件头）：
//
//   - 「只改资金」的敏感度塌了：4.82pp → 0.76pp，资金阶梯转为**收敛**、零拒单；
//   - 「每票各自缩放」（= hfq 腿的真实形状）的差异**没有消失，反而更清晰**
//     （4.22 → 10.31pp），通道被确认为**绝对金额约束**：hfq 价 = qfq 价 × 每股
//     常数（实测 1.7~181），固定资金下高价票连一手都买不起 → **票池成分被改写**。
//     这是真实的资金约束（一手 = 100 × 价格），不是引擎缺陷。修复前那个缺陷
//     恰好把它冲淡了（重复下单让买得起的票买过头、收益虚高）。
//
// 所以下面两条断言（方向是「差异必须实质性」与「uniform ≪ per-symbol」）
// **预期仍然成立**，但具体数字必须重跑才能引用。重跑前提：真库 + token
// （`docker compose up -d postgres`）。本地无真库时本文件 skip ——
// 那不是绿，是没测。
// ═══════════════════════════════════════════════════════════════════════════

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy/examples"
)

const (
	// adjPoolSize：取证的股票池规模。比 P2-13 的 40 只小 —— 这里每只票要多花
	// 两次 Tushare 调用（daily + adj_factor）把原始价和因子拉下来。
	adjPoolSize = 20

	// adjMinBars：只在区间里有足够历史的票才进池。regime 的慢均线是 200，
	// 池子太薄的话两条腿都会退化到默认分支、比不出差异。
	adjMinBars = 400
)

var adjWindow = struct{ Start, End string }{"2024-01-02", "2026-06-30"}

// scaleProvider 把一个 Provider 的四个价格字段按 symbol 乘上常数。
//
// 量/额**刻意不动**：复权只改价、不改成交量，所以「qfq → hfq」恰好等价于
// 「每只票的价格乘一个正数」。这个装饰器因此就是口径切换本身，没有别的东西
// 混进来 —— 比「建一张 hfq 表再换个 provider」更少变量。
type scaleProvider struct {
	marketdata.Provider
	factors map[string]float64
}

func (p *scaleProvider) GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error) {
	bars, err := p.Provider.GetOHLCV(ctx, symbol, start, end)
	if err != nil {
		return nil, err
	}
	scaleBars(bars, p.factors[symbol])
	return bars, nil
}

func (p *scaleProvider) BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error) {
	data, err := p.Provider.BulkLoadOHLCV(ctx, symbols, start, end)
	if err != nil {
		return nil, err
	}
	for sym, bars := range data {
		scaleBars(bars, p.factors[sym])
	}
	return data, nil
}

// scaleBars 原地缩放。c == 0 表示「没登记这只票」，按 1 处理 —— 不能默默
// 把价格乘成 0，那会让整只票变成免费股票（P1-7 那条坑的形状）。
func scaleBars(bars []domain.OHLCV, c float64) {
	if c == 0 || c == 1 {
		return
	}
	for i := range bars {
		bars[i].Open *= c
		bars[i].High *= c
		bars[i].Low *= c
		bars[i].Close *= c
	}
}

func adjDSN(t *testing.T) string {
	t.Helper()
	port := 5432
	if p, err := strconv.Atoi(os.Getenv("DATABASE_PORT")); err == nil && p > 0 {
		port = p
	}
	host := os.Getenv("DATABASE_HOST")
	if host == "" {
		host = "localhost"
	}
	user := os.Getenv("DATABASE_USER")
	if user == "" {
		user = "postgres"
	}
	pass := os.Getenv("DATABASE_PASSWORD")
	if pass == "" {
		pass = "postgres"
	}
	name := os.Getenv("DATABASE_NAME")
	if name == "" {
		name = "quant_trading"
	}
	dsn, err := storage.BuildDSN(storage.DatabaseConfig{
		URL: os.Getenv("DATABASE_URL"), Host: host, Port: port,
		User: user, Password: pass, Name: name, SSLMode: "disable",
	})
	if err != nil {
		t.Skipf("跳过 P2-8 取证：库连接配置不可用（%v）", err)
	}
	return dsn
}

func openAdjStore(t *testing.T, ctx context.Context) *storage.PostgresStore {
	t.Helper()
	store, err := storage.NewPostgresStore(ctx, adjDSN(t))
	if err != nil {
		t.Skipf("跳过 P2-8 取证：连不上库（%v）。先 `docker compose up -d postgres`", err)
	}
	if err := store.Ping(ctx); err != nil {
		store.Close()
		t.Skipf("跳过 P2-8 取证：库 ping 不通（%v）", err)
	}
	t.Cleanup(store.Close)
	return store
}

// adjPool 按代码顺序取池子（不按历史完整度排 —— 那是在用未来信息挑样本）。
func adjPool(t *testing.T, ctx context.Context, store *storage.PostgresStore) []string {
	t.Helper()
	rows, err := store.DB().Query(ctx, `
		SELECT symbol
		FROM ohlcv_daily_qfq
		WHERE trade_date >= $1::date AND trade_date <= $2::date
		GROUP BY symbol
		HAVING count(*) >= $3
		ORDER BY symbol
		LIMIT $4
	`, adjWindow.Start, adjWindow.End, adjMinBars, adjPoolSize)
	if err != nil {
		t.Fatalf("查股票池失败：%v", err)
	}
	defer rows.Close()

	var pool []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("扫 symbol 失败：%v", err)
		}
		pool = append(pool, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历股票池出错：%v", err)
	}
	if len(pool) == 0 {
		t.Skip("跳过 P2-8 取证：库里没有满足条件的行情，先跑一次 /sync/ohlcv/all")
	}
	return pool
}

// fetchRawAndFactors 拉一只票的**不复权收盘价**与**复权因子**，各自按交易日索引。
//
// 注意区间取的是**库里的同步区间**，不是回测区间。这不是随手取宽一点：
// qfq 的分母是 f(该票同步区间的最后一天)，取窄了算出来的分母就比库里那个小，
// 重建出来的 qfq 会整体偏大 —— 第一版就是这么被自检挡下来的
// （000004.SZ 在 20240102：重建 16.14 vs 库里 14.5578，差 9.8%）。
func fetchRawAndFactors(t *testing.T, ctx context.Context, c *TushareClient, symbol string, span dateSpan) (map[string]float64, map[string]float64) {
	t.Helper()
	params := map[string]interface{}{
		"ts_code":    symbol,
		"start_date": formatDate(span.Start),
		"end_date":   formatDate(span.End),
	}

	dailyResp, err := c.call(ctx, "daily", params, "ts_code,trade_date,close")
	if err != nil {
		t.Fatalf("daily(%s) 失败：%v", symbol, err)
	}
	facResp, err := c.call(ctx, "adj_factor", params, "ts_code,trade_date,adj_factor")
	if err != nil {
		t.Fatalf("adj_factor(%s) 失败：%v", symbol, err)
	}

	raw := make(map[string]float64, len(dailyResp.Data.Items))
	for _, item := range dailyResp.Data.Items {
		if len(item) < 3 {
			continue
		}
		if d := c.fieldStr(item, 1); d != "" {
			raw[d] = c.fieldFloat(item, 2)
		}
	}
	fac := make(map[string]float64, len(facResp.Data.Items))
	for _, item := range facResp.Data.Items {
		if len(item) < 3 {
			continue
		}
		d := c.fieldStr(item, 1)
		f := c.fieldFloat(item, 2)
		if d != "" && f > 0 {
			fac[d] = f
		}
	}
	return raw, fac
}

type dateSpan struct{ Start, End string }

// dataSpan 取库里实际同步下来的日期跨度。基准因子必须是**这个区间**的末日因子，
// 因为它就是入库时 normalizeDailyOHLCV 用的那个分母。
func dataSpan(t *testing.T, ctx context.Context, store *storage.PostgresStore) dateSpan {
	t.Helper()
	var lo, hi time.Time
	if err := store.DB().QueryRow(ctx, `
		SELECT min(trade_date), max(trade_date) FROM ohlcv_daily_qfq
	`).Scan(&lo, &hi); err != nil {
		t.Fatalf("查库里行情跨度失败：%v", err)
	}
	return dateSpan{Start: lo.Format("2006-01-02"), End: hi.Format("2006-01-02")}
}

// buildBases 逐票算出「库里 qfq 用的那个基准因子」，并**自检重建是否成立**。
//
// 基准 = 该票 sync 区间末日那天的因子。注意不是「全局末日」：退市票的因子序列
// 止于它自己的最后交易日，入库时用的分母也是那一天。所以先问库要该票的
// **最后一根 K 线**，再取不晚于它的最大因子日。
//
// 自检：raw(t) × f(t) / base 必须逐点等于库里存的 qfq(t)。不等的票**逐条列出来
// 并剔除出对照**，而不是静默跳过（静默跳过会让「20 只票的结论」其实只基于 3 只）
// 也不是直接 Fatal（那样一只票的同步区间漂移就会挡住整条取证，且看不出是几只）。
func buildBases(t *testing.T, ctx context.Context, store *storage.PostgresStore, c *TushareClient, pool []string, span dateSpan) (map[string]float64, []string) {
	t.Helper()

	bases := make(map[string]float64, len(pool))
	var rejected []string
	worst := 0.0

	for _, symbol := range pool {
		var lastBar time.Time
		if err := store.DB().QueryRow(ctx,
			`SELECT max(trade_date) FROM ohlcv_daily_qfq WHERE symbol = $1`, symbol,
		).Scan(&lastBar); err != nil {
			rejected = append(rejected, symbol+"(查不到最后一根 K 线)")
			continue
		}
		lastBarKey := lastBar.Format("20060102")

		raw, fac := fetchRawAndFactors(t, ctx, c, symbol, span)

		baseDate := ""
		for d := range fac {
			if d <= lastBarKey && d > baseDate {
				baseDate = d
			}
		}
		if baseDate == "" {
			rejected = append(rejected, symbol+"(因子序列覆盖不到最后一根 K 线)")
			continue
		}
		base := fac[baseDate]

		start, _ := time.Parse("2006-01-02", adjWindow.Start)
		end, _ := time.Parse("2006-01-02", adjWindow.End)
		stored, err := store.GetOHLCV(ctx, symbol, start, end)
		if err != nil {
			rejected = append(rejected, symbol+"(读库失败)")
			continue
		}

		checked, bad := 0, ""
		for _, bar := range stored {
			d := bar.Date.Format("20060102")
			r, okRaw := raw[d]
			f, okFac := fac[d]
			if !okRaw || !okFac {
				continue
			}
			want := r * f / base
			if want <= 0 {
				continue
			}
			rel := math.Abs(want-bar.Close) / want
			if rel > worst {
				worst = rel
			}
			if rel > 1e-6 && bad == "" {
				bad = fmt.Sprintf("%s 重建 %.6f vs 库里 %.6f（相对差 %.2e）", d, want, bar.Close, rel)
			}
			checked++
		}
		if checked == 0 {
			rejected = append(rejected, symbol+"(没有一天能同时拿到原始价与因子)")
			continue
		}
		if bad != "" {
			rejected = append(rejected, symbol+"("+bad+")")
			continue
		}
		bases[symbol] = base
	}

	if len(rejected) > 0 {
		// 不静默：逐条写出来。这些票的口径与库不一致，比出来的差异不能算到
		// 复权头上。
		for _, r := range rejected {
			t.Logf("剔除（重建口径与入库不一致）：%s", r)
		}
	}
	t.Logf("重建自检通过：%d/%d 只票、逐点比对，最大相对误差 %.2e（阈值 1e-6；基准取该票 sync 区间末日因子）",
		len(bases), len(pool), worst)
	return bases, rejected
}

func configureAdjMomentum(t *testing.T, params map[string]interface{}) {
	t.Helper()
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

// runAdjBacktest 用给定 provider 跑一次回测，返回结果摘要。
//
// initialCapital 是变量而不是常数：它决定「一个仓位预算买得起几手」，
// 而仓位预算 / 价格 正是复权口径泄漏进决策的那条路径（见文件末尾的结论）。
func runAdjBacktest(t *testing.T, ctx context.Context, prov marketdata.Provider, pool []string, initialCapital float64) *domain.BacktestResult {
	t.Helper()

	v := viper.New()
	v.Set("backtest.initial_capital", initialCapital)
	v.Set("backtest.commission_rate", fees.DefaultCommissionRate)
	v.Set("backtest.slippage_rate", fees.DefaultSlippageRate)
	v.Set("backtest.risk_free_rate", 0.03)
	v.Set("backtest.seed", 7)
	v.Set("strategy_service.url", "http://localhost:8082")

	eng, err := backtest.NewEngine(v, prov, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	rm, err := risk.NewRiskManager(risk.RiskManagerConfig{
		TargetVolatility: 0.15, MaxPositionWeight: 0.10, MinPositionWeight: 0.01,
		ATRPeriod: 14, BaseMultiplier: 2.0, BullMultiplier: 1.5, BearMultiplier: 3.0,
		SidewaysMultiplier: 2.0, TakeProfitMult: 3.0, VolLookbackDays: 60,
		AnnualizationFactor: math.Sqrt(252), FastMAPeriod: 50, SlowMAPeriod: 200,
		RegimeVolLookback: 120,
	}, zerolog.Nop())
	if err != nil {
		t.Fatalf("risk.NewRiskManager: %v", err)
	}
	eng.SetRiskManager(rm)

	configureAdjMomentum(t, map[string]interface{}{
		"lookback_days":       20,
		"top_n":               5,
		"max_positions":       5,
		"rebalance_frequency": "daily",
	})

	resp, err := eng.RunBacktest(ctx, backtest.BacktestRequest{
		Strategy:       "momentum",
		StockPool:      pool,
		StartDate:      adjWindow.Start,
		EndDate:        adjWindow.End,
		InitialCapital: initialCapital,
		RiskFreeRate:   0.03,
	})
	if err != nil {
		t.Fatalf("RunBacktest: %v", err)
	}
	if resp.Status != "completed" {
		t.Fatalf("回测未完成：status=%s err=%s", resp.Status, resp.Error)
	}

	return &domain.BacktestResult{
		TotalReturn:     resp.TotalReturn,
		AnnualReturn:    resp.AnnualReturn,
		SharpeRatio:     resp.SharpeRatio,
		MaxDrawdown:     resp.MaxDrawdown,
		WinRate:         resp.WinRate,
		TotalTrades:     resp.TotalTrades,
		PortfolioValues: resp.PortfolioValues,
		Trades:          resp.Trades,
	}
}

// medianBase 取基准因子的中位数，用作「均匀缩放腿」的常数 —— 量级要与
// per-symbol 那组相当，否则两条腿的整手粒度不可比。
func medianBase(bases map[string]float64) float64 {
	vals := make([]float64, 0, len(bases))
	for _, v := range bases {
		vals = append(vals, v)
	}
	sort.Float64s(vals)
	if len(vals) == 0 {
		return 1
	}
	return vals[len(vals)/2]
}

func sumAbsQty(trades []domain.Trade) float64 {
	total := 0.0
	for _, tr := range trades {
		total += math.Abs(tr.Quantity)
	}
	return total
}

// describeTrade 把一笔成交压成「哪天、哪只票、买还是卖」—— **不含数量、价格**。
//
// 故意不含数量/价格：数量在 qfq 与 hfq 下本来就该差一个每股常数（不是分歧），
// 价格更不用说。要比的是**决策本身是不是同一个**。
func describeTrade(tr domain.Trade) string {
	return fmt.Sprintf("%s %s %s", tr.Timestamp.Format("2006-01-02"), tr.Symbol, tr.Direction)
}

// firstDivergence 找出两条成交序列**第一次做出不同决策**的位置。
//
// 这是区分「系统性通道」与「路径发散」的关键判据：
//   - 第一笔就不一样 → 口径直接改变了决策（系统性）；
//   - 前几百笔完全一致、之后才岔开 → 决策本来是同一个，是某个离散的
//     价格比较（涨跌停取整到分、止损阈值）在某个临界点上翻了面，
//     之后回测走上另一条路径。**那种情况下「差 20 个百分点」不能读成
//     「前视偏差值 20 个百分点」，只能读成「这个回测对微扰不稳定」。**
func firstDivergence(a, b []domain.Trade) (idx, common int, diverged bool) {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if describeTrade(a[i]) != describeTrade(b[i]) {
			return i, i, true
		}
	}
	return n, n, len(a) != len(b)
}

// TestAdjustmentBasis_QfqLookAheadLeaksIntoBacktest 是 P2-8 的取证主体。
func TestAdjustmentBasis_QfqLookAheadLeaksIntoBacktest(t *testing.T) {
	token := os.Getenv("TUSHARE_TOKEN")
	if token == "" {
		t.Skip("跳过 P2-8 取证：TUSHARE_TOKEN 未设置（复活原始价与因子需要它）")
	}

	ctx := context.Background()
	store := openAdjStore(t, ctx)
	pool := adjPool(t, ctx, store)
	span := dataSpan(t, ctx, store)

	// 必须走构造函数：直接写结构体字面量会得到 httpClient == nil，
	// 而 call() 是经由它发请求的（第一版就是这么崩的）。
	// store / cache 传 nil —— 本测试只调 call()，不触发落库那条路。
	client := NewTushareClient(token, "https://api.tushare.pro", 3, nil, nil)
	bases, rejected := buildBases(t, ctx, store, client, pool, span)

	// 对照只在**重建通过**的那些票上做：口径对不上的票进了池子，差异就分不清
	// 是复权造成的还是数据本身对不上造成的。剔完太少就说明这次取证不成立。
	comparePool := make([]string, 0, len(bases))
	for _, s := range pool {
		if _, ok := bases[s]; ok {
			comparePool = append(comparePool, s)
		}
	}
	if len(comparePool) < len(pool)/2 {
		t.Fatalf("重建自检只通过了 %d/%d 只票（剔除 %d 只），样本不足以支撑结论：\n  %s",
			len(comparePool), len(pool), len(rejected), strings.Join(rejected, "\n  "))
	}

	baseProvider := marketdata.NewPostgresProvider(store, zerolog.Nop())

	// 四腿：两种口径 × 两个资金量级。
	//
	// 为什么要两个资金量级：**口径本身不可能改变收益率序列**（见
	// tushare_hfq_test.go 的断言 2a）。所以两条腿一旦跑出差异，差异必然来自
	// 某个「吃价格绝对水平」的环节。候选里最像的是下单取整 ——
	// `floor(仓位预算 / 价格)` 再向下取整到 100 股整手（pkg/risk/manager.go）。
	// 如果它真是那条路径，那么把资金放大到「一手怎么都买得起」之后，差异
	// 就应该塌掉：同样的信号、同样的收益序列，只是每笔都能凑够整手。
	const (
		smallCapital = 1_000_000.0
		hugeCapital  = 100_000_000.0
	)

	// 第三条腿：**所有票乘同一个常数**（取 hfq 那些基准的中位数，量级相当）。
	// 这条腿是区分两类通道的关键：
	//   - 「每股各自的常数」与「全体同一个常数」的差别，在于**跨票的价格水平
	//     是否被一致地缩放**。若差异只来自「整手粒度变粗」（每股价格整体抬高），
	//     那么均匀缩放应当产生与 hfq 同等量级的差异；
	//   - 若均匀缩放几乎不产生差异，而各自缩放产生 20 多个百分点，则通道
	//     在**跨票比较**上 —— 即某个把不同票的价格放在一起比大小的环节。
	median := medianBase(bases)
	uniform := make(map[string]float64, len(comparePool))
	for _, s := range comparePool {
		uniform[s] = median
	}
	t.Logf("  均匀缩放腿用的常数 = 基准中位数 = %.4f", median)

	qfqSmall := runAdjBacktest(t, ctx, &scaleProvider{Provider: baseProvider}, comparePool, smallCapital)
	hfqSmall := runAdjBacktest(t, ctx, &scaleProvider{Provider: baseProvider, factors: bases}, comparePool, smallCapital)
	uniSmall := runAdjBacktest(t, ctx, &scaleProvider{Provider: baseProvider, factors: uniform}, comparePool, smallCapital)
	qfqHuge := runAdjBacktest(t, ctx, &scaleProvider{Provider: baseProvider}, comparePool, hugeCapital)
	hfqHuge := runAdjBacktest(t, ctx, &scaleProvider{Provider: baseProvider, factors: bases}, comparePool, hugeCapital)

	gapSmall := math.Abs(hfqSmall.TotalReturn - qfqSmall.TotalReturn)
	gapHuge := math.Abs(hfqHuge.TotalReturn - qfqHuge.TotalReturn)
	gapUniform := math.Abs(uniSmall.TotalReturn - qfqSmall.TotalReturn)

	t.Logf("口径对照（池子 %d 只，%s~%s；库跨度 %s~%s）",
		len(comparePool), adjWindow.Start, adjWindow.End, span.Start, span.End)
	leg := func(name string, r *domain.BacktestResult) {
		t.Logf("  %s: 收益 %9.4f%%  夏普 %7.4f  回撤 %8.4f%%  成交 %4d 笔  累计股数 %.0f",
			name, r.TotalReturn*100, r.SharpeRatio, r.MaxDrawdown*100, r.TotalTrades, sumAbsQty(r.Trades))
	}
	leg(fmt.Sprintf("qfq @%.0f    ", smallCapital), qfqSmall)
	leg(fmt.Sprintf("hfq @%.0f    ", smallCapital), hfqSmall)
	leg(fmt.Sprintf("均匀×%.2f @%.0f ", median, smallCapital), uniSmall)
	leg(fmt.Sprintf("qfq @%.0f  ", hugeCapital), qfqHuge)
	leg(fmt.Sprintf("hfq @%.0f  ", hugeCapital), hfqHuge)
	t.Logf("  → 口径差异：@%.0f = %.4f 个百分点；@%.0f = %.4f 个百分点",
		smallCapital, gapSmall*100, hugeCapital, gapHuge*100)
	t.Logf("  → 均匀缩放(×%.2f)差异 = %.4f 个百分点（用来区分「整手粒度」与「跨票水平不一致」）",
		median, gapUniform*100)

	// 症断：差异是「决策本身就不同」还是「同一个决策在某个临界点上翻了面」。
	type pair struct {
		name string
		a, b *domain.BacktestResult
	}
	for _, p := range []pair{
		{"qfq@小资金 vs hfq@小资金", qfqSmall, hfqSmall},
		{"qfq@小资金 vs qfq@大资金(只改资金)", qfqSmall, qfqHuge},
		{"qfq@大资金 vs hfq@大资金", qfqHuge, hfqHuge},
	} {
		first, common, diverged := firstDivergence(p.a.Trades, p.b.Trades)
		if !diverged {
			t.Logf("  症断 %s：%d 笔决策**逐笔一致**（差异纯粹在成交数量/价格上）", p.name, common)
			continue
		}
		t.Logf("  症断 %s：前 %d 笔决策一致，第 %d 笔起岔开（两腿共 %d / %d 笔）",
			p.name, common, first+1, len(p.a.Trades), len(p.b.Trades))
		if first < len(p.a.Trades) && first < len(p.b.Trades) {
			t.Logf("       A: %s", describeTrade(p.a.Trades[first]))
			t.Logf("       B: %s", describeTrade(p.b.Trades[first]))
		}
	}

	// 逐票诊断：hfq 口径下「一手」要多少钱，对比一个仓位预算。
	// 仓位预算上限 = 初始资金 × MaxPositionWeight(0.10)。
	budgetSmall := smallCapital * 0.10
	unaffordable := 0
	sorted := make([]string, 0, len(bases))
	for s := range bases {
		sorted = append(sorted, s)
	}
	sort.Strings(sorted)
	start, _ := time.Parse("2006-01-02", adjWindow.Start)
	for _, s := range sorted {
		var qfqClose float64
		if err := store.DB().QueryRow(ctx,
			`SELECT close FROM ohlcv_daily_qfq WHERE symbol = $1 AND trade_date = $2`,
			s, start,
		).Scan(&qfqClose); err != nil {
			continue
		}
		hfqClose := qfqClose * bases[s]
		lotCost := hfqClose * 100
		over := ""
		if lotCost > budgetSmall {
			over = "  ← 一手就超过仓位预算，这笔单会取整成 0 股"
			unaffordable++
		}
		t.Logf("  %s base=%9.4f  qfq收盘=%8.3f  hfq收盘=%10.2f  一手=¥%11.0f%s",
			s, bases[s], qfqClose, hfqClose, lotCost, over)
	}
	t.Logf("  → %d/%d 只票在 hfq 口径下一手买不起（@%.0f 时仓位预算上限 ¥%.0f）",
		unaffordable, len(sorted), smallCapital, budgetSmall)

	// 前提：两条腿都必须真的跑出成交。「都是 0 笔」不是「口径无关」，
	// 是没跑起来 —— 后半句是本仓反复踩过的坑。
	if qfqSmall.TotalTrades == 0 || hfqSmall.TotalTrades == 0 {
		t.Fatalf("两条腿至少有一条零成交（qfq=%d hfq=%d）—— 这测的是「没跑起来」，不是口径差异",
			qfqSmall.TotalTrades, hfqSmall.TotalTrades)
	}

	// 结论 1：小资金下口径差异是**实质性的**，不是取整噪声。
	// TASKS.md P2-8 原记的判据（「只差一个每股常数……可能有实质影响的是按股数
	// 下单」）方向对了，但把它当成「可能有影响」低估了两个数量级。
	//
	// ⚠️ 「约 21.6」是 **AUD-55 修复前**的读数，修复后尚未重跑（本地无真库，
	// 见文件头）。这条断言只钉**方向**（差异必须远大于取整噪声），不钉数值；
	// 重跑后请把新读数写进 TASKS.md 的 P2-8 取证说明。
	if gapSmall < 0.05 {
		t.Errorf("小资金下 qfq 与 hfq 的收益差只有 %.4f 个百分点 —— 判据是「差异必须实质性」，"+
			"这里已经退到取整噪声的量级，需重新取证（修复前的实测记录约 21.6pp）",
			gapSmall*100)
	}

	// 结论 2（**这一条是从失败里长出来的**）：原假设「差异主要来自整手取整」
	// 被资金实验推翻 —— 把资金放大 100 倍之后差异没有收缩。撤掉那条断言，
	// 改成记录真实形态：差异与资金量级无关。
	t.Logf("  → 资金放大 100 倍后差异 %.4f 个百分点（小资金 %.4f）：**取整不是主因**",
		gapHuge*100, gapSmall*100)

	// 结论 3：per-symbol 缩放的杀伤力远大于均匀缩放。
	//
	// ⚠️ 归因已在 2026-09-24 修正（AUD-55 修复后）。原来写的是「通道在
	// 『把不同票的价格放在一起比大小』的环节上」，**那是错的** —— 那个候选
	// （regime 判定）已被 pkg/risk/regime_scale_invariance_test.go 证伪
	// （对统一缩放 14/14 窗口严格不变）。真通道是**绝对金额约束**：
	// 一手 = 100 × 价格，而资金固定。
	//
	//   - 均匀缩放（所有票同乘一个常数）：全体票的「一手金额 / 预算」同比变化，
	//     一起跨过阈值 —— 合成里 5/20 只被抬到一手（最大超买 ×3.41）；
	//   - per-symbol（每票各自的常数）：**只把高基准的那几只**推出去 ——
	//     合成里 9/20 只（最大超买 ×68.20），票池成分改写得更狠。
	//
	// 两者同源、只是程度不同。所以这条断言钉的是**程度关系**（uniform 必须
	// 明显小于 per-symbol），不是「两条腿走了不同通道」。
	if gapUniform > gapSmall/3 {
		t.Errorf("均匀缩放(×%.2f)的差异 %.4f 个百分点，与 per-symbol 的 %.4f 同量级 —— "+
			"「per-symbol 只推高基准票、均匀缩放推全体」这个程度关系不成立，需重新归因",
			median, gapUniform*100, gapSmall*100)
	}
}
