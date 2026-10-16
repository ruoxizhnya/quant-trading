package storage

// DATA-03 / DATA-07：两条读数的库侧真相。
//
// DATA-03 —— `GET /stocks/count` 的 `latest_date` 原来在 handler 里把
// `SELECT MAX(trade_date)` 扫进 `*string`（pgx 收到的是 `**string`），2026-10-16 实测
// 错误是 `cannot scan date (OID 1082) in binary format into **string`：**查询本身成功、
// 值也回来了，是类型映射不进去**，而那条 error 被整个丢弃 ⇒ 字段永远静默缺席。
// 本文件把读数收进库侧并钉三件事：类型映射对（值能读到）、错误如实上抛（不被吞）、
// 「没有读数」(NULL) 与「读失败」(error) 是两种不同结果。
//
// DATA-07 —— `GET /market/index` 的响应从来没有 volume/amount 这两个字段，
// 而库里 `ohlcv_daily_qfq` 就有 `volume`（股，DATA-04）与 `turnover`（CNY，DATA-05）。
// 这里钉「读回的值逐字段等于库里插进去的值，本层不做任何单位换算」——
// 换算一次就有两处真相。
//
// 探针行的两个纪律（都是本仓已登记的坑）：
//  1. symbol 用本次运行独有的假代码（`D37…` 形状与真实 A 股编码不同），删除精确只删自己。
//  2. **日期必须落在既有跨度 [2023-09-25, 2026-09-24] 之内**：
//     `pkg/data/tushare_adjustment_integration_test.go:281` 读的是**全表** min/max
//     `trade_date` 并据此取基准因子，探针日期若把全局 max 推后或把 min 提前，
//     等于我在别人的测试里造偶发红。所以这里用 2026-09-22/23 两个库内日期。

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// quoteProbeSeq 取纳秒后 8 位，配合不同标签生成本次运行独有的假代码。
var quoteProbeSeq = time.Now().UnixNano() % 100000000

// quoteProbe 生成 D37 + 8 位 + 单字母 + .SH：撞不上真票，也撞不上别的 agent 的探针。
func quoteProbe(tag string) string {
	return fmt.Sprintf("D37%08d%s.SH", quoteProbeSeq, tag[:1])
}

// 库内既有跨度的两个安全日期（见文件头纪律 2）。
var (
	probeDayPrev = time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	probeDayLast = time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
)

// seedProbeBar 插一行日线并登记删除。直接写 SQL 而不走 SaveOHLCV ——
// 后者表达不出 turnover 为 NULL 这个形状，而 NULL 正是本单要钉的分支之一。
//
// 删除走 t.Cleanup 而不是 inline DELETE：断言失败时 inline 那行会被跳过，
// 探针行就永久留在共享库里，下一次读数凭空多一行。调用方必须**先**登记
// store.Close —— cleanup 是 LIFO，后登记的删除才会跑在关池之前。
func seedProbeBar(t *testing.T, store *PostgresStore, ctx context.Context, symbol string, date time.Time, close, volume float64, turnover *float64) {
	t.Helper()
	_, err := store.DB().Exec(ctx,
		`INSERT INTO ohlcv_daily_qfq (symbol, trade_date, open, high, low, close, volume, turnover)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (symbol, trade_date) DO UPDATE SET
			close = EXCLUDED.close, volume = EXCLUDED.volume, turnover = EXCLUDED.turnover`,
		symbol, date.Format("2006-01-02"), close, close, close, close, volume, turnover)
	require.NoError(t, err, "insert probe bar %s %s", symbol, date.Format("2006-01-02"))
	t.Cleanup(func() {
		if _, derr := store.DB().Exec(context.Background(),
			`DELETE FROM ohlcv_daily_qfq WHERE symbol=$1`, symbol); derr != nil {
			t.Errorf("cleanup probe %s failed: %v", symbol, derr)
		}
	})
}

// TestLatestQuoteDate_ReadsRealMaxDate 是 DATA-03 的核心腿：**值必须读得到**。
//
// 旧形状（扫进 `**string`）在这里会拿到 pgx 的类型映射错误 ⇒ 本条红。
// 这正是工单 ⑤ 要的破坏验证形态：错误不再可能被丢弃而无声通过。
//
// 前置读的就是断言读的东西（AUD-51 口径）：oracle 取全表 `MAX(trade_date)`，
// 为 NULL 说明这是张空表（环境，不是缺陷）⇒ skip。两趟读之间存在「并发同步把
// MAX 推后」的假红窗口，与 DATA-02 同一取舍（自灌探针会把全局 MAX 推后、
// 带偏 TestAdjustmentBasis，代价更大，故不采用）。
func TestLatestQuoteDate_ReadsRealMaxDate(t *testing.T) {
	store := testStore(t)
	t.Cleanup(store.Close)
	ctx := context.Background()

	var oracle *string
	require.NoError(t, store.DB().QueryRow(ctx,
		`SELECT to_char(MAX(trade_date), 'YYYY-MM-DD') FROM ohlcv_daily_qfq`).Scan(&oracle))
	if oracle == nil {
		t.Skip("前置：ohlcv_daily_qfq 为空（新库未灌数据），本条无从核对读数")
	}

	got, err := store.LatestQuoteDate(ctx)
	require.NoError(t, err, "MAX(trade_date) 必须能被正确类型接住（旧形状报 cannot scan date into **string）")
	require.NotNil(t, got, "库里有行 ⇒ 不该读成 NULL；读到 NULL 说明类型映射或 NULL 语义错了")
	assert.Equal(t, *oracle, got.Format("2006-01-02"), "读数逐值等于库里的 MAX(trade_date)")
}

// TestLatestQuoteDate_PropagatesQueryError 钉「失败要响」：ctx 取消时返回 error，
// 而不是 (nil, nil) —— 否则调用方分不清「表是空的」与「这趟没读成」，
// 而后者正是 DATA-03 的缺陷形状（读失败被当成没数据，界面无声少一个数）。
func TestLatestQuoteDate_PropagatesQueryError(t *testing.T) {
	store := testStore(t)
	t.Cleanup(store.Close)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := store.LatestQuoteDate(cancelled)
	require.Error(t, err, "查询失败必须返回 error（旧 handler 把 error 丢了，字段于是静默缺席）")
	assert.Nil(t, got, "失败时不得给出任何日期读数")
}

// TestLatestIndexBars_FieldByFieldAgainstInserted 是工单 ⑤「响应的 volume/amount
// 与库里逐字段相等」的库侧腿：插两期已知值，读回的必须是**那两期的真值**。
func TestLatestIndexBars_FieldByFieldAgainstInserted(t *testing.T) {
	store := testStore(t)
	t.Cleanup(store.Close)
	ctx := context.Background()

	sym := quoteProbe("A")
	amount := 375461676.1428 // CNY 口径（DATA-05 已统一），本层不得再换算

	seedProbeBar(t, store, ctx, sym, probeDayPrev, 4400.5, 150000000, ptrFloat(360000000))
	seedProbeBar(t, store, ctx, sym, probeDayLast, 4439.1443, 151965787, &amount)

	bars, err := store.LatestIndexBars(ctx, []string{sym})
	require.NoError(t, err)
	require.Len(t, bars, 1, "一个代码一条读数（最新一期），不是逐日全给")

	bar := bars[0]
	assert.Equal(t, sym, bar.Code)
	assert.InDelta(t, 4439.1443, bar.Close, 1e-9, "close 必须原样等于库里最新一期")
	assert.InDelta(t, 151965787, bar.Volume, 1e-6, "volume 单位是股（DATA-04），原样透出不换算成手")
	require.NotNil(t, bar.Turnover, "turnover 有值时必须读出来，不能当 0")
	assert.InDelta(t, amount, *bar.Turnover, 1e-6,
		"turnover 单位是 CNY（DATA-05），原样透出：不除 1e8、也不乘 1000")
	require.NotNil(t, bar.PrevClose, "有两期数据时必须给出上一期收盘，否则涨跌幅无从算起")
	assert.InDelta(t, 4400.5, *bar.PrevClose, 1e-9, "prev_close 取次新那期（LEAD），不是最早一期")
}

// TestLatestIndexBars_NullTurnoverStaysNull 钉「缺失不等于 0」：
// turnover 列可空（DDL 只有 DEFAULT 0，无 NOT NULL）。NULL 若被读成 0，
// 界面就把「没有成交额读数」显示成「成交额 0 元」——那是假数字。
func TestLatestIndexBars_NullTurnoverStaysNull(t *testing.T) {
	store := testStore(t)
	t.Cleanup(store.Close)
	ctx := context.Background()

	sym := quoteProbe("N")
	seedProbeBar(t, store, ctx, sym, probeDayLast, 100.25, 200000, nil)
	// 先证前提：那一行确实落成 NULL（没被 DDL 的 DEFAULT 0 填掉）。
	// 前提不成立时下面的断言就是空转，所以这一步不能省。
	var isNull bool
	require.NoError(t, store.DB().QueryRow(ctx,
		`SELECT turnover IS NULL FROM ohlcv_daily_qfq WHERE symbol=$1 AND trade_date=$2`,
		sym, probeDayLast.Format("2006-01-02")).Scan(&isNull))
	require.True(t, isNull, "前提：探针行的 turnover 必须真的是 NULL")

	bars, err := store.LatestIndexBars(ctx, []string{sym})
	require.NoError(t, err)
	require.Len(t, bars, 1)
	assert.Nil(t, bars[0].Turnover, "turnover 为 NULL 必须读成 nil，不是 0")
	assert.InDelta(t, 200000, bars[0].Volume, 1e-9)
}

// TestLatestIndexBars_UnknownCodeYieldsNoBar 钉「查不到不编造」：
// 没有行的代码不得产出一条零值读数 —— 零值 close 会被界面读成「收盘价 0.00」。
//
// 只用本次运行独有的假代码：不把「000300.SH 在库里 0 行」这个**当下数据状态**
// 写成永久断言 —— 指数日线一旦灌进来，那条会莫名变红，而红出来的信息不指向缺陷。
// 指数 0 行的实测读数记在工单 ② 与台账里，HTTP 面那条腿另有安排（见 cmd/data 侧）。
func TestLatestIndexBars_UnknownCodeYieldsNoBar(t *testing.T) {
	store := testStore(t)
	t.Cleanup(store.Close)
	ctx := context.Background()

	bars, err := store.LatestIndexBars(ctx, []string{quoteProbe("Z"), quoteProbe("Q")})
	require.NoError(t, err)
	assert.Empty(t, bars, "库里没有行 ⇒ 一条读数都不产，而不是编一条零值")
}

func ptrFloat(v float64) *float64 { return &v }
