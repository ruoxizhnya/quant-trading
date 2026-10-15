package marketdata

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"
)

// cst 构造 Asia/Shanghai 时刻的便捷函数（聚合口径的时钟基准）。
func cst(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, market.AsiaShanghai)
}

// cstSec 构造带秒的 Asia/Shanghai 时刻（REV-04 秒级边界用例；
// 纳秒级越界用例内联 time.Date，不放大 helper  surface）。
func cstSec(y int, m time.Month, d, hh, mm, ss int) time.Time {
	return time.Date(y, m, d, hh, mm, ss, 0, market.AsiaShanghai)
}

// minuteBar 构造一根规范层 1m bar（Frequency 必须显式声明 —— 零值
// "未声明" 过不了聚合器输入校验）。
func minuteBar(symbol string, ts time.Time, o, h, l, c, v float64) market.OHLCV {
	return market.OHLCV{
		Symbol:    symbol,
		Date:      ts,
		Open:      o,
		High:      h,
		Low:       l,
		Close:     c,
		Volume:    v,
		Frequency: market.Freq1m,
		VisibleAt: market.IntradayVisibleAt(ts, 1),
	}
}

// TestAggregate15m_HandComputed 聚合口径手算样本比对（D1 验收⑤2）：
// 主口径 开=首笔开/高=最高/低=最低/收=末笔收/量=合计 + 午休断点 +
// 集合竞价并入 + 停牌日不产出。每个期望值都是手算后写死的，不是从
// 实现反推的。
func TestAggregate15m_HandComputed(t *testing.T) {
	sym := "600000.SH"
	bars := []market.OHLCV{
		// 2024-01-02 上午：09:25 开盘集合竞价并入 09:30 bucket
		minuteBar(sym, cst(2024, 1, 2, 9, 25), 10.00, 10.05, 9.98, 10.02, 5000),
		minuteBar(sym, cst(2024, 1, 2, 9, 30), 10.02, 10.08, 10.00, 10.06, 3000),
		minuteBar(sym, cst(2024, 1, 2, 9, 31), 10.06, 10.10, 10.04, 10.09, 2000),
		// 11:15 bucket（止于 11:30，不跨午休）
		minuteBar(sym, cst(2024, 1, 2, 11, 15), 10.20, 10.25, 10.18, 10.22, 1000),
		minuteBar(sym, cst(2024, 1, 2, 11, 29), 10.22, 10.30, 10.21, 10.28, 1500),
		// 13:00 bucket（午休后第一个，与 11:15 bucket 是两个）
		minuteBar(sym, cst(2024, 1, 2, 13, 0), 10.30, 10.33, 10.29, 10.31, 1200),
		// 14:57 收盘集合竞价并入 14:45 bucket
		minuteBar(sym, cst(2024, 1, 2, 14, 57), 10.40, 10.45, 10.38, 10.44, 800),
	}

	out, err := Aggregate(bars, market.Freq15m)
	require.NoError(t, err)
	require.Len(t, out, 4, "2024-01-02 应产出 4 个 15m bucket（09:30 / 11:15 / 13:00 / 14:45），空 bucket 不零填充")

	// bucket 1：开=09:25 竞价成交价（首笔），收=09:31 末笔，高/低跨笔极值，量合计
	assert.Equal(t, cst(2024, 1, 2, 9, 30), out[0].Date)
	assert.Equal(t, 10.00, out[0].Open, "开=首笔（集合竞价）开")
	assert.Equal(t, 10.10, out[0].High)
	assert.Equal(t, 9.98, out[0].Low)
	assert.Equal(t, 10.09, out[0].Close, "收=末笔收")
	assert.Equal(t, 10000.0, out[0].Volume, "量=期间合计")
	assert.Equal(t, market.Freq15m, out[0].Frequency)
	assert.Equal(t, cst(2024, 1, 2, 9, 45), out[0].VisibleAt, "15m bar 可见时间点 = bucket 结束时刻")

	// bucket 2：11:15–11:30（午休断点：不与 13:00 合并）
	assert.Equal(t, cst(2024, 1, 2, 11, 15), out[1].Date)
	assert.Equal(t, 10.20, out[1].Open)
	assert.Equal(t, 10.30, out[1].High)
	assert.Equal(t, 10.18, out[1].Low)
	assert.Equal(t, 10.28, out[1].Close)
	assert.Equal(t, 2500.0, out[1].Volume)
	assert.Equal(t, cst(2024, 1, 2, 11, 30), out[1].VisibleAt)

	// bucket 3：13:00–13:15
	assert.Equal(t, cst(2024, 1, 2, 13, 0), out[2].Date)
	assert.Equal(t, 10.30, out[2].Open)
	assert.Equal(t, 10.31, out[2].Close)
	assert.Equal(t, 1200.0, out[2].Volume)
	assert.Equal(t, cst(2024, 1, 2, 13, 15), out[2].VisibleAt)

	// bucket 4：14:45–15:00（收盘集合竞价并入）
	assert.Equal(t, cst(2024, 1, 2, 14, 45), out[3].Date)
	assert.Equal(t, 10.40, out[3].Open)
	assert.Equal(t, 10.45, out[3].High)
	assert.Equal(t, 10.38, out[3].Low)
	assert.Equal(t, 10.44, out[3].Close)
	assert.Equal(t, 800.0, out[3].Volume)
	assert.Equal(t, cst(2024, 1, 2, 15, 0), out[3].VisibleAt)
}

// TestAggregate15m_SuspendedDayAndPartialDay 停牌日三细则：
// 全日停牌（无 bar）→ 无产出；半日停牌 → 已覆盖 bucket 产出、未覆盖缺失。
func TestAggregate15m_SuspendedDayAndPartialDay(t *testing.T) {
	sym := "000001.SZ"
	// 2024-01-03：10:29 后停牌 —— 只有 09:30/09:45/10:00/10:15 四个 bucket 有 bar
	bars := []market.OHLCV{
		minuteBar(sym, cst(2024, 1, 3, 9, 30), 11.00, 11.05, 10.95, 11.02, 1000),
		minuteBar(sym, cst(2024, 1, 3, 9, 46), 11.02, 11.10, 11.00, 11.08, 1100),
		minuteBar(sym, cst(2024, 1, 3, 10, 0), 11.08, 11.12, 11.05, 11.10, 900),
		minuteBar(sym, cst(2024, 1, 3, 10, 29), 11.10, 11.15, 11.09, 11.13, 1300),
	}
	out, err := Aggregate(bars, market.Freq15m)
	require.NoError(t, err)
	require.Len(t, out, 4, "半日停牌：只产出有 bar 的 4 个 bucket，10:30 之后缺失（不零填充）")
	assert.Equal(t, cst(2024, 1, 3, 10, 15), out[3].Date)
	assert.Equal(t, 11.13, out[3].Close)

	// 全日停牌：输入空 → 输出空（不是一根零量 bar）
	empty, err := Aggregate(nil, market.Freq15m)
	require.NoError(t, err)
	assert.Empty(t, empty, "停牌日不产出 bar —— 缺失≠0")
}

// TestAggregate1mTo1d_HandComputed 1m→1d 聚合口径手算样本（成文备
// D6/对拍；1d 路由不走此路径，见 Router 注释）。
func TestAggregate1mTo1d_HandComputed(t *testing.T) {
	sym := "600000.SH"
	bars := []market.OHLCV{
		minuteBar(sym, cst(2024, 1, 2, 9, 30), 10.00, 10.10, 9.98, 10.06, 3000),
		minuteBar(sym, cst(2024, 1, 2, 14, 57), 10.40, 10.45, 10.38, 10.44, 800),
		minuteBar(sym, cst(2024, 1, 3, 9, 30), 11.00, 11.15, 10.95, 11.13, 4300),
	}
	out, err := Aggregate(bars, market.Freq1d)
	require.NoError(t, err)
	require.Len(t, out, 2, "两个交易日 → 两根日线 bar")

	// 1d 口径：Date 用 UTC 零点（与 ohlcv_daily_qfq 扫描形态一致，两种 1d 路径可比）
	assert.Equal(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), out[0].Date)
	assert.Equal(t, 10.00, out[0].Open, "开=当日首笔开")
	assert.Equal(t, 10.45, out[0].High)
	assert.Equal(t, 9.98, out[0].Low)
	assert.Equal(t, 10.44, out[0].Close, "收=当日末笔收（收盘集合竞价）")
	assert.Equal(t, 3800.0, out[0].Volume)
	assert.Equal(t, market.Freq1d, out[0].Frequency)
	assert.Equal(t, market.DailyVisibleAt(out[0].Date), out[0].VisibleAt)

	assert.Equal(t, time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), out[1].Date)
	assert.Equal(t, 11.13, out[1].Close)
}

// TestAggregate_RejectsWrongFrequencyAndTargets 聚合器 fail-loud：
// 输入频率声明不是 1m → 拒（D1 破坏验证①的第二道闸：路由把 1m 数据
// 声明成别的频率，这里当场红）；目标频率不支持 → 拒。
func TestAggregate_RejectsWrongFrequencyAndTargets(t *testing.T) {
	bad := market.OHLCV{
		Symbol: "600000.SH", Date: cst(2024, 1, 2, 9, 30),
		Frequency: market.Freq1d, // 1m 形态的数据声明成 1d —— 正是破坏验证①造的形状
	}
	_, err := Aggregate([]market.OHLCV{bad}, market.Freq15m)
	require.Error(t, err, "输入频率声明与数据形态不符必须拒收")
	assert.Contains(t, err.Error(), "1m")

	ok := minuteBar("600000.SH", cst(2024, 1, 2, 9, 30), 10, 10, 10, 10, 1)
	_, err = Aggregate([]market.OHLCV{ok}, market.Freq1m)
	require.Error(t, err, "聚合到 1m 是无意义 no-op，拒绝")

	_, err = Aggregate([]market.OHLCV{ok}, market.Frequency("1h"))
	require.Error(t, err, "未知聚合目标必须拒")
}

// TestBucketStart15m_SessionBoundaries 会话边界逐点钉住：
// 集合竞价并入首/末 bucket、午休两侧不合并、时段外 fail-loud。
// REV-04（2026-10-15）起边界按秒判定：11:30:00 上午收盘打点并入
// 11:15 bucket（与 15:00:00 对称——旧语义把 11:30:00 当午休拒收，
// 那是分钟粒度判定的遗漏，不是口径）；带秒/亚秒越界打点拒收。
func TestBucketStart15m_SessionBoundaries(t *testing.T) {
	cases := []struct {
		name string
		ts   time.Time
		want time.Time // 零值 = 期望报错
	}{
		{"开盘集合竞价 09:25 并入 09:30", cst(2024, 1, 2, 9, 25), cst(2024, 1, 2, 9, 30)},
		{"竞价前深夜打点拒绝", cst(2024, 1, 2, 3, 0), time.Time{}},
		{"09:30 整点归 09:30 bucket", cst(2024, 1, 2, 9, 30), cst(2024, 1, 2, 9, 30)},
		{"09:44:59 归 09:30 bucket", cst(2024, 1, 2, 9, 44), cst(2024, 1, 2, 9, 30)},
		{"11:15 归 11:15 bucket", cst(2024, 1, 2, 11, 15), cst(2024, 1, 2, 11, 15)},
		{"11:29 归 11:15 bucket（午休前最后一根）", cst(2024, 1, 2, 11, 29), cst(2024, 1, 2, 11, 15)},
		{"11:30:00 上午收盘打点并入 11:15 bucket（REV-04：与 15:00:00 对称）", cst(2024, 1, 2, 11, 30), cst(2024, 1, 2, 11, 15)},
		{"11:30:01 带秒打点拒收（已进午休）", cstSec(2024, 1, 2, 11, 30, 1), time.Time{}},
		{"12:30 午休打点拒绝", cst(2024, 1, 2, 12, 30), time.Time{}},
		{"13:00 归 13:00 bucket（不与上午合并）", cst(2024, 1, 2, 13, 0), cst(2024, 1, 2, 13, 0)},
		{"14:57 收盘竞价归 14:45 bucket", cst(2024, 1, 2, 14, 57), cst(2024, 1, 2, 14, 45)},
		{"15:00:00 收盘打点归 14:45 bucket", cst(2024, 1, 2, 15, 0), cst(2024, 1, 2, 14, 45)},
		{"15:00:30 带秒打点拒收（分钟粒度会误纳入末 bucket）", cstSec(2024, 1, 2, 15, 0, 30), time.Time{}},
		{"15:00:00 亚秒越界拒收", time.Date(2024, 1, 2, 15, 0, 0, 1, market.AsiaShanghai), time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bucketStart15m(tc.ts)
			if tc.want.IsZero() {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	// 15:01 盘后打点拒绝（15:00:00 是收盘价打点，15:01 不是）
	_, err := bucketStart15m(cst(2024, 1, 2, 15, 1))
	require.Error(t, err)
}

// TestAggregate_SessionBoundarySecondPrecision REV-04：会话校验秒级精度
// 与 11:30 收盘 bar 形态兼容，两个事实一组对称——
//  1. 11:30:00 上午收盘打点合法，并入 11:15 bucket（收=末笔、量合计），
//     与 15:00:00 并入 14:45 bucket 同款语义；Tushare 1m 普遍含 11:30
//     整点 bar（mootdx 导出不带），两种分钟形态在入站处兼容（SPEC 已知边界）。
//  2. 边界带秒的打点（15:00:30 / 11:30:01）拒收且报错点名——分钟粒度
//     判定会把它们与整点收盘打点混同，静默放宽口径边界。
func TestAggregate_SessionBoundarySecondPrecision(t *testing.T) {
	sym := "600000.SH"

	// ① 11:30:00 并入 11:15 bucket：与 11:15/11:29 两根合成一个 bucket，
	// 收=11:30 末笔、高取跨笔极值、量含 11:30 bar。
	morning := []market.OHLCV{
		minuteBar(sym, cst(2024, 1, 2, 11, 15), 10.20, 10.25, 10.18, 10.22, 1000),
		minuteBar(sym, cst(2024, 1, 2, 11, 29), 10.22, 10.30, 10.21, 10.28, 1500),
		minuteBar(sym, cstSec(2024, 1, 2, 11, 30, 0), 10.28, 10.31, 10.27, 10.30, 500),
	}
	out, err := Aggregate(morning, market.Freq15m)
	require.NoError(t, err, "11:30:00 上午收盘打点是合法 bar（并入 11:15 bucket）")
	require.Len(t, out, 1, "11:15/11:29/11:30 三根同归 11:15 bucket（不跨午休）")
	assert.Equal(t, cst(2024, 1, 2, 11, 15), out[0].Date)
	assert.Equal(t, 10.20, out[0].Open, "开=首笔开")
	assert.Equal(t, 10.31, out[0].High, "高=期间最高（含 11:30 bar）")
	assert.Equal(t, 10.18, out[0].Low)
	assert.Equal(t, 10.30, out[0].Close, "收=末笔收（11:30 收盘打点）")
	assert.Equal(t, 3000.0, out[0].Volume, "量=期间合计（含 11:30 bar）")
	assert.Equal(t, cst(2024, 1, 2, 11, 30), out[0].VisibleAt)

	// ② 15:00:30 带秒盘后打点 → fail-loud，不静默并入 14:45 bucket
	afternoon := []market.OHLCV{
		minuteBar(sym, cst(2024, 1, 2, 14, 45), 10.40, 10.45, 10.38, 10.44, 800),
		minuteBar(sym, cstSec(2024, 1, 2, 15, 0, 30), 10.44, 10.46, 10.43, 10.45, 100),
	}
	_, err = Aggregate(afternoon, market.Freq15m)
	require.Error(t, err, "15:00:30 已过收盘，必须拒收而非当收盘打点并入末 bucket")
	assert.Contains(t, err.Error(), "晚于 15:00 收盘")

	// ③ 11:30:01 带秒午休打点 → fail-loud，不静默并入 11:15 bucket
	lunch := []market.OHLCV{
		minuteBar(sym, cst(2024, 1, 2, 11, 29), 10.22, 10.30, 10.21, 10.28, 1500),
		minuteBar(sym, cstSec(2024, 1, 2, 11, 30, 1), 10.28, 10.29, 10.27, 10.28, 100),
	}
	_, err = Aggregate(lunch, market.Freq15m)
	require.Error(t, err, "11:30:01 已进午休，必须拒收")
	assert.Contains(t, err.Error(), "午休")
}

// TestRouter_UnknownFrequencyFailsLoud 路由对未知频率 fail-loud，
// 不静默回退（nil pool 也不会被碰到——校验在查库之前）。
func TestRouter_UnknownFrequencyFailsLoud(t *testing.T) {
	r := NewRouter(nil)
	_, err := r.Bars(context.Background(), market.Frequency("1h"), "600000.SH",
		time.Unix(0, 0), time.Unix(0, 0))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported frequency")

	_, err = r.Bars(context.Background(), market.Frequency(""), "600000.SH",
		time.Unix(0, 0), time.Unix(0, 0))
	require.Error(t, err, "零值频率 = 未声明，不是合法入参")
}

// TestRouter_IntradaySpanCapFailsLoud 钉 REV-01：1m 与 15m 两条路径的大区间请求
// 必须在**查库之前**被拒，而不是把整个区间的分钟行 materialize 进内存。
//
// nil pool 就是「有没有走到 Query」的探针：守卫若在查库之后，这里会 panic 而不是
// 返回错误。两条频率都要过——15m 经 loadMinute 取分钟行，所以它和 1m 共用同一个
// 入口，本单 ③ 要求两条路径都覆盖。
func TestRouter_IntradaySpanCapFailsLoud(t *testing.T) {
	r := NewRouter(nil)
	start := cst(2024, 1, 2, 9, 30)
	end := start.Add(maxIntradaySpan + time.Second)
	wantSpan := (maxIntradaySpan + time.Second).String()

	for _, freq := range []market.Frequency{market.Freq1m, market.Freq15m} {
		freq := freq
		t.Run(string(freq), func(t *testing.T) {
			var got []market.OHLCV
			var err error
			require.NotPanics(t, func() {
				got, err = r.Bars(context.Background(), freq, "600000.SH", start, end)
			}, "校验必须发生在查库之前 —— nil pool 被碰到就会 panic")
			require.Error(t, err, "超上限必须报错，不能静默截断（③ 与 Router 既有 fail-loud 同风格）")
			assert.Nil(t, got)
			// ③ 点名要求：报错要说清「已支持的最大跨度」与「当前请求跨度」。
			assert.Contains(t, err.Error(), "上限")
			assert.Contains(t, err.Error(), maxIntradaySpan.String(), "报错应点名已支持的最大跨度")
			assert.Contains(t, err.Error(), wantSpan, "报错应点名当前请求跨度")
			assert.Contains(t, err.Error(), "切片", "报错应给出可执行的下一步，而不是只说太大")
		})
	}
}

// TestRouter_IntradayCapIsMeaningful 用**绝对**跨度钉住上限的量级：约 14 个月的
// 分钟请求必须被拒。
//
// 这条刻意不用 maxIntradaySpan 去构造输入：上面那组用例的输入是从常量推导的，
// 把上限调到一百年，它们会跟着门柱一起挪而照样绿 —— 而「护栏被调大」正是 REV-01
// 要防的失效形状（一个满足不了任何真实用法的上限会被下一个人删掉）。
// 绝对输入才有资格断言绝对行为。
func TestRouter_IntradayCapIsMeaningful(t *testing.T) {
	r := NewRouter(nil)
	start := cst(2024, 1, 2, 9, 30)
	end := start.AddDate(1, 1, 0) // ≈ 425 自然日

	for _, freq := range []market.Frequency{market.Freq1m, market.Freq15m} {
		t.Run(string(freq), func(t *testing.T) {
			var err error
			require.NotPanics(t, func() {
				_, err = r.Bars(context.Background(), freq, "600000.SH", start, end)
			})
			require.Error(t, err, "约 14 个月的日内请求必须被拒 —— 上限必须是量级上有意义的数")
		})
	}
}

// TestCheckIntradaySpan 是守卫自身的边界表。
func TestCheckIntradaySpan(t *testing.T) {
	base := cst(2024, 1, 2, 9, 30)
	cases := []struct {
		name       string
		start, end time.Time
		wantErr    bool
	}{
		{"恰好等于上限放行", base, base.Add(maxIntradaySpan), false},
		{"远小于上限放行", base, base.AddDate(0, 3, 0), false},
		{"同刻零跨度放行", base, base, false},
		{"超上限一秒拒绝", base, base.Add(maxIntradaySpan + time.Second), true},
		{"十年区间拒绝", base, base.AddDate(10, 0, 0), true},
		// end 早于 start 不在本单判断范围：负跨度由 SQL 的 BETWEEN 自然给空集，
		// 把它当错误是另一件事（不顺手扩大范围）。
		{"end 早于 start 不由本守卫拒绝", base.Add(time.Hour), base, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkIntradaySpan(tc.start, tc.end)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestFrequencyVocabulary 频率词表：解析/校验/日内分钟数。
func TestFrequencyVocabulary(t *testing.T) {
	for _, f := range []market.Frequency{market.Freq1m, market.Freq15m, market.Freq1d} {
		got, err := market.ParseFrequency(string(f))
		require.NoError(t, err)
		assert.Equal(t, f, got)
		assert.True(t, f.Valid())
	}
	_, err := market.ParseFrequency("2h")
	require.Error(t, err)

	assert.False(t, market.Frequency("").Valid(), "零值 = 未声明，不是合法频率")

	m, ok := market.Freq15m.IntradayMinutes()
	assert.True(t, ok)
	assert.Equal(t, 15, m)
	_, ok = market.Freq1d.IntradayMinutes()
	assert.False(t, ok, "日频没有日内分钟数")
	assert.True(t, market.Freq1m.IsIntraday())
	assert.False(t, market.Freq1d.IsIntraday())
}

// TestVisibilityHelpers 时间语义口径：日线 15:00 收盘 CST 可见；
// 日内 = 周期结束时刻可见。
func TestVisibilityHelpers(t *testing.T) {
	// PG DATE 扫描形态：UTC 零点的交易日
	day := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, cst(2024, 1, 2, 15, 0), market.DailyVisibleAt(day))

	bar := cst(2024, 1, 2, 10, 0)
	assert.Equal(t, cst(2024, 1, 2, 10, 1), market.IntradayVisibleAt(bar, 1), "10:00 的 1m bar 10:01 可见")
	assert.Equal(t, cst(2024, 1, 2, 9, 45), market.IntradayVisibleAt(cst(2024, 1, 2, 9, 30), 15), "09:30 的 15m bar 09:45 可见")
}
