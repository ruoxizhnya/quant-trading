package data

import (
	"math"
	"testing"

	"github.com/rs/zerolog"
)

// 前复权口径的回归测试。
//
// 为什么值得单独一个文件：复权价是回测里每一个数字的地基，而 qfq 的换算式
// （raw × 当日因子 / 最新因子）**很容易凭直觉写错** —— 写成除以首日因子、
// 或者忘记除、或者因子取反，都能产出「看起来像股价」的数字，单看结果发现不了。
// 所以这里钉两类断言：
//   1. 绝对值：直接拿 Tushare 官方文档给出的算例比对（防口径记错）；
//   2. 边界：缺失因子怎么办、没有因子怎么办（防「拿不复权价冒充复权价」）。

func qfqTestClient() *TushareClient {
	return &TushareClient{logger: zerolog.Nop()}
}

// dailyItem 按 FetchDailyOHLCV 请求的字段顺序构造一行：
// ts_code, trade_date, open, high, low, close, vol, amount
func dailyItem(tradeDate string, open, high, low, close, vol, amount float64) []any {
	return []any{"600519.SH", tradeDate, open, high, low, close, vol, amount}
}

func dailyResp(items ...[]any) *TushareResponse {
	return &TushareResponse{
		Data: TushareData{
			Fields: []string{"ts_code", "trade_date", "open", "high", "low", "close", "vol", "amount"},
			Items:  items,
		},
	}
}

func almost(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.005 {
		t.Errorf("%s = %.4f, want %.4f (官方算例)", what, got, want)
	}
}

// TestNormalizeDailyOHLCV_MatchesOfficialExample 用 Tushare 官方 `adj_factor`
// 接口文档「示例4：利用复权因子计算前复权和后复权价格」的原始数据做绝对值断言。
//
// 官方给出的那一行（贵州茅台 600519.SH，2020-01-02）：
//
//	raw:  open=1128.00 high=1145.06 low=1116.00 close=1130.00
//	factor(当日)=7.3186   factor(最新, 2026-07-13)=8.6463
//	官方算出: open_qfq=954.79 high_qfq=969.23 low_qfq=944.63 close_qfq=956.48
//
// 用官方数字当基准，是为了让「口径写错了」立刻变红，而不是产出一份自洽的错误。
func TestNormalizeDailyOHLCV_MatchesOfficialExample(t *testing.T) {
	resp := dailyResp(
		dailyItem("20200102", 1128.00, 1145.06, 1116.00, 1130.00, 25271.64, 2856061.0),
		dailyItem("20260713", 1197.12, 1215.00, 1190.19, 1210.99, 12000.0, 1400000.0),
	)
	factors := map[string]float64{
		"20200102": 7.3186,
		"20260713": 8.6463,
	}

	got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", factors)
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}

	first := got[0]
	almost(t, "open_qfq", first.Open, 954.79)
	almost(t, "high_qfq", first.High, 969.23)
	almost(t, "low_qfq", first.Low, 944.63)
	almost(t, "close_qfq", first.Close, 956.48)

	// 基准日（区间内最新交易日）的 qfq 价格应当等于原始价 —— scale 恒为 1。
	// 这一条同时钉住「除以最新因子」而不是「除以首日因子」：若写反，这里会不等于原值。
	last := got[1]
	almost(t, "latest open_qfq", last.Open, 1197.12)
	almost(t, "latest close_qfq", last.Close, 1210.99)
}

// TestNormalizeDailyOHLCV_VolumeNotAdjusted 成交量不参与复权。
// tushare 的 vol/amount 本来就是原始成交量，前复权不改它 —— 如果顺手乘了 scale，
// 换手率、成交额类因子会整体偏小，而且偏得没有规律（越早越小）。
func TestNormalizeDailyOHLCV_VolumeNotAdjusted(t *testing.T) {
	resp := dailyResp(dailyItem("20200102", 1128.00, 1145.06, 1116.00, 1130.00, 25271.64, 2856061.0))
	factors := map[string]float64{"20200102": 7.3186}

	got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", factors)
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	almost(t, "volume", got[0].Volume, 25271.64)
	almost(t, "turnover", got[0].Turnover, 2856061.0)
}

// TestNormalizeDailyOHLCV_MissingFactorSkipsRow 某天取不到因子时**跳过**该行，
// 绝不补 1.0。补 1.0 等于宣称「这天没除权」，在除权日附近会造出假跳空，
// 而回测会把那个跳空当成真信号 —— 这正是 P2-10 那条坑的形状（缺失值折成默认值
// = 编数据，而且编出来的数据会主动把人引向错误的交易）。
func TestNormalizeDailyOHLCV_MissingFactorSkipsRow(t *testing.T) {
	resp := dailyResp(
		dailyItem("20260102", 100.0, 101.0, 99.0, 100.5, 1000, 100000),
		dailyItem("20260103", 100.0, 101.0, 99.0, 100.5, 1000, 100000), // 这天没有因子
		dailyItem("20260104", 100.0, 101.0, 99.0, 100.5, 1000, 100000),
	)
	factors := map[string]float64{
		"20260102": 10.0,
		"20260104": 12.0, // 基准（最新）
	}

	got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", factors)
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2 (the factor-less day must be dropped)", len(got))
	}
	for _, r := range got {
		if r.Date.Format("20060102") == "20260103" {
			t.Fatal("20260103 没有复权因子却进了库 —— 那一行是不复权价冒充的 qfq")
		}
	}
	// 留下来的那行必须真的被换算过（不是原值）：100 × 10 / 12 = 83.33
	almost(t, "open_qfq", got[0].Open, 83.333)
}

// TestNormalizeDailyOHLCV_NoFactorsReturnsNothing 整个区间都没有因子时返回空，
// 而不是把不复权价格原样写进 ohlcv_daily_qfq。
//
// 这张表的名字里就写着 qfq —— 把不复权价存进去是**对真值的反面**，而且极难发现：
// 回测照跑、曲线照画，只是每个价格都是错的（除权日会有一堆假跳空）。
func TestNormalizeDailyOHLCV_NoFactorsReturnsNothing(t *testing.T) {
	resp := dailyResp(dailyItem("20260102", 100.0, 101.0, 99.0, 100.5, 1000, 100000))

	if got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", nil); len(got) != 0 {
		t.Fatalf("no factors: got %d records, want 0 (must not store unadjusted prices as qfq)", len(got))
	}
	if got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", map[string]float64{}); len(got) != 0 {
		t.Fatalf("empty factors: got %d records, want 0", len(got))
	}
}

// TestNormalizeDailyOHLCV_NonPositiveFactorSkipped 因子 <= 0 是脏数据，同样跳过
// （它不可能是合法的复权因子；放进去会把价格乘成 0 或负数）。
func TestNormalizeDailyOHLCV_NonPositiveFactorSkipped(t *testing.T) {
	resp := dailyResp(
		dailyItem("20260102", 100.0, 101.0, 99.0, 100.5, 1000, 100000),
		dailyItem("20260104", 100.0, 101.0, 99.0, 100.5, 1000, 100000),
	)
	factors := map[string]float64{
		"20260102": 0,     // 脏：非正
		"20260104": -12.0, // 脏：负数，且它会被当成“最新”
	}

	got := qfqTestClient().normalizeDailyOHLCV(resp, "600519.SH", factors)
	if len(got) != 0 {
		t.Fatalf("got %d records, want 0 (non-positive factors are unusable)", len(got))
	}
}
