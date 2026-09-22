package marketdata

// 风险警示（ST 系）股票的市场结构事实：名字判定 + 每日买入上限。
//
// 为什么在 pkg/marketdata 而不是 pkg/backtest
// -------------------------------------------
// AUD-22 (ODR-065)。这两个事实原先都住在 pkg/backtest：
// IsRiskWarningName 在 pkg/backtest/pricelimit.go，而「当日累计买入上限」
// 干脆没有任何实现。问题是买入上限必须作用在**下单路径**上，而
// pkg/backtest 是父包 —— pkg/live、pkg/risk 都不能 import 它（会形成
// 反向依赖，S7-P2-1 的 leaf 抽取就是为了破这个环）。
//
// pkg/marketdata 是叶子包，且本来就是「市场结构事实」的家：Board、
// Board.DailyPriceLimit()、ClassifySymbol 都在这里。ST 名字判定与
// 每日买入上限同属此类 —— 它们是交易所规则，不是可调参数。
//
// 判定本身没有重写：AUD-08 已经把 name[:2] 的坏实现改成了前缀匹配，
// 这里搬过来的是那份实现。

// IsRiskWarningName reports whether a stock's display name carries a
// risk-warning (ST-family) prefix.
//
// AUD-08 (ODR-065 H3): the previous implementation was
//
//	prefix := name[:2]
//	return prefix == "ST" || prefix == "*ST" || prefix == "SST" || prefix == "S*ST"
//
// which could only ever match "ST": the other three alternatives are
// 3–4 characters long and can never equal a 2-character slice. The
// condition looked like it handled all four forms and in fact handled
// one. Detection is now prefix-based.
//
// AUD-22 (ODR-065): moved here from pkg/backtest/pricelimit.go so that
// the order path (which cannot import pkg/backtest) can reach it.
//
// Order matters: the longer forms are checked first only for
// readability — HasPrefix is not affected by which one matches first,
// since all of them imply "risk warning".
func IsRiskWarningName(name string) bool {
	if name == "" {
		return false
	}
	// Normalise: some feeds pad the name, and the marker is ASCII, so
	// trimming leading spaces is safe and prevents " ST某某" slipping
	// through.
	s := trimASCIISpace(name)

	// "*ST" (退市风险警示) and "S*ST" (未股改 + 退市风险),
	// "SST" (未股改 + 特别处理), "ST" (特别处理).
	//
	// Written as explicit prefixes rather than a regexp: four cases do
	// not justify compiling a pattern on a hot path (this runs once
	// per symbol per trading day).
	for _, p := range []string{"*ST", "S*ST", "SST", "ST"} {
		if len(s) >= len(p) && s[:len(p)] == p {
			return true
		}
	}
	return false
}

// trimASCIISpace removes leading and trailing ASCII spaces/tabs without
// pulling in strings just for TrimSpace in this file's hot path.
func trimASCIISpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// Per-investor daily cumulative BUY caps for risk-warning stocks
// (风险警示股票当日累计买入数量上限).
//
// Verified against the exchanges' published rule text on 2026-09-22 —
// not against summaries, because 监管常数 的注释与值可能共同错误
// (PITFALLS §4: AUD-06 的印花税两个值都错 2 倍但方向一致):
//
//   - 上交所《交易规则（2026 年修订）》(上证发〔2026〕41 号, 2026-07-06 施行)
//     4.4.10 — 50 万股。4.4.1 把风险警示板限定为「主板股票」，6.14 明确
//     科创板的风险警示股票**不进入风险警示板** → 科创板不受此限。
//   - 深交所《交易规则（2026 年修订）》4.5.4 — 50 万股。4.5.1 未按板块
//     限定（原文不含「主板」），故创业板风险警示股同样适用。
//   - 北交所《交易规则》4.5.4 — 20 万股，2026-08-31 起施行
//     （2026-04-24 发布时该条尚待「另行通知」，是三者中最新的一条）。
//
// 三所口径一致：委托买入数量 + 当日已买入数量 + 已申报买入但尚未成交、
// 也未撤销的数量之和 ≤ 上限；投资者以本人名义开立的证券账户与融资融券
// 信用证券账户的买入量**合并计算**。例外（三所相同）：上市公司回购股份、
// 5% 以上股东根据已披露的增持计划增持股份。
const (
	// RiskWarningDailyBuyCapMainBoard is the 沪深 cap (50 万股) —
	// 沪 4.4.10 / 深 4.5.4.
	RiskWarningDailyBuyCapMainBoard = 500_000

	// RiskWarningDailyBuyCapBSE is the 北交所 cap (20 万股) — 北 4.5.4.
	RiskWarningDailyBuyCapBSE = 200_000
)

// RiskWarningDailyBuyCap returns the maximum number of shares of a
// SINGLE risk-warning stock that one investor may buy cumulatively in
// one trading day, for the board `symbol` trades on. Zero means the cap
// does not apply on that board.
//
// The caller is responsible for establishing that the stock IS under a
// risk warning (IsRiskWarningName); this function answers "which cap",
// not "is there one". Keeping the two questions apart is deliberate: the
// name is a property of the security, the cap is a property of the board.
//
// 科创板 returns 0 rather than a number: 沪 6.14 excludes 科创板 risk-warning
// stocks from 风险警示板, so 4.4.10's 50 万股 never applies to them. This is
// the one board where the answer is genuinely "no cap" rather than "a
// different cap", and collapsing it into the main-board branch would
// wrongly constrain STAR names.
//
// BoardUnknown returns the STRICTER of the two caps. Same reasoning as
// NormalizeOrderQuantity's fallback: a mis-classified symbol should still
// be bounded, and being conservative here costs a slightly pessimistic
// backtest rather than an unrealistically permissive one.
func RiskWarningDailyBuyCap(symbol string) float64 {
	switch ClassifySymbol(symbol) {
	case BoardBSE:
		return RiskWarningDailyBuyCapBSE
	case BoardSTAR:
		// 科创板 ST 不进风险警示板（沪 6.14）→ 无此上限。
		return 0
	case BoardMainBoardSH, BoardMainBoardSZ, BoardChiNext:
		return RiskWarningDailyBuyCapMainBoard
	default:
		// BoardUnknown, plus the non-equity boards (ETF / bond / index /
		// LOF), which can never be 风险警示股票 and so never reach here in
		// practice. Returning the stricter cap keeps a mis-classified
		// equity bounded instead of silently uncapped.
		return RiskWarningDailyBuyCapBSE
	}
}
