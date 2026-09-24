package data

// 后复权（hfq）与前复权（qfq）的关系。
//
// 为什么这两条断言值得单独一个文件：P2-8 的整条结论都压在
//
//	hfq(t) = qfq(t) × f(区间末日)          —— 「只差一个每股常数」
//	hfq(t)/hfq(t-1) == qfq(t)/qfq(t-1)    —— 「收益率序列完全相同」
//
// 这两句上。它们是**通行说法**，但通行的说法恰恰最容易在没有对照的时候被
// 顺口重复。写成断言之后，任何一次改动只要破坏了「只差常数」这个性质
// （例如把 qfq 的分母换成首日因子、或者顺手把成交量也乘上 scale），
// 这里立刻变红，而不是等到某天有人拿 hfq 和 qfq 比回测、发现差异来自
// 别的地方。
//
// 换算口径见 tushare.go 的 normalizeDailyOHLCV（官方 adj_factor 文档示例 4）。

import (
	"math"
	"testing"
)

// hfqResp 按 FetchDailyOHLCV 的字段顺序构造 daily 响应：
// ts_code, trade_date, open, high, low, close, vol, amount
func hfqResp(symbol string, rows ...[]any) *TushareResponse {
	items := make([][]any, 0, len(rows))
	for _, r := range rows {
		items = append(items, append([]any{symbol}, r...))
	}
	return &TushareResponse{
		Data: TushareData{
			Fields: []string{"ts_code", "trade_date", "open", "high", "low", "close", "vol", "amount"},
			Items:  items,
		},
	}
}

// day 构造一行「开=高=低=收=close」的行情，只用收盘价参与断言。
func day(tradeDate string, close, vol float64) []any {
	return []any{tradeDate, close, close, close, close, vol, close * vol}
}

func relDiff(a, b float64) float64 {
	if a == 0 && b == 0 {
		return 0
	}
	d := math.Abs(a - b)
	m := math.Max(math.Abs(a), math.Abs(b))
	return d / m
}

// TestHfqIsQfqTimesOnePerShareConstant 钉住「hfq 与 qfq 只差一个每股常数」。
//
// 构造：三天行情，最后一天有除权（因子从 1.0 跳到 1.2）。
//   - qfq 由**生产函数** normalizeDailyOHLCV 算出；
//   - hfq 按定义 raw × 当日因子算出。
//
// 断言两条：
//  1. 每一天上 hfq/qfq 都等于同一个常数（= 区间末日因子，这里 1.2）；
//  2. 两个口径的**收益率**逐点相同，而与**不复权**收益率在除权日不同 ——
//     后半句是防止这个测试变成空转：如果复权根本没起作用，两串收益率
//     也会「相同」，那样断言 2 就什么也没证明。
func TestHfqIsQfqTimesOnePerShareConstant(t *testing.T) {
	const symbol = "600000.SH"

	raw := []float64{100.0, 110.0, 105.0}
	dates := []string{"20260102", "20260105", "20260106"}
	factors := map[string]float64{
		"20260102": 1.0,
		"20260105": 1.0,
		"20260106": 1.2, // 除权日：基准（区间末日因子）= 1.2
	}

	rows := make([][]any, 0, len(dates))
	for i, d := range dates {
		rows = append(rows, day(d, raw[i], 1000))
	}

	qfq := qfqTestClient().normalizeDailyOHLCV(hfqResp(symbol, rows...), symbol, factors)
	if len(qfq) != len(dates) {
		t.Fatalf("qfq 行数 %d，期望 %d（三条都有因子，不该被丢）", len(qfq), len(dates))
	}

	base := factors["20260106"]

	// 断言 1：hfq = qfq × base，常数逐点成立。
	for i, d := range dates {
		hfq := raw[i] * factors[d]
		gotRatio := hfq / qfq[i].Close
		if relDiff(gotRatio, base) > 1e-12 {
			t.Errorf("%s：hfq/qfq = %.12f，期望常数 %.6f（hfq=%.6f qfq=%.6f）",
				d, gotRatio, base, hfq, qfq[i].Close)
		}
	}

	// 断言 2a：两个口径的收益率逐点相同。
	for i := 1; i < len(dates); i++ {
		qRet := qfq[i].Close / qfq[i-1].Close
		hRet := (raw[i] * factors[dates[i]]) / (raw[i-1] * factors[dates[i-1]])
		if relDiff(qRet, hRet) > 1e-12 {
			t.Errorf("%s：qfq 收益率 %.12f ≠ hfq 收益率 %.12f —— 「收益率序列相同」这句不成立",
				dates[i], qRet, hRet)
		}
	}

	// 断言 2b：不复权收益率与复权收益率**必须**在除权日不同，
	// 否则本测试的两串数字其实是同一串（复权没生效），断言 2a 就成了空转。
	rawRet := raw[2] / raw[1]
	qfqRet := qfq[2].Close / qfq[1].Close
	if relDiff(rawRet, qfqRet) < 1e-6 {
		t.Fatalf("除权日的不复权收益率(%.6f)与 qfq 收益率(%.6f)相同 —— 复权没生效，本测试无法区分两个口径",
			rawRet, qfqRet)
	}
}
