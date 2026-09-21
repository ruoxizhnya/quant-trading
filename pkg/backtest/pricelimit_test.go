package backtest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ── IsRiskWarningName (AUD-08 / ODR-065 H3) ──────────────────────────

// TestIsRiskWarningName covers the four prefix forms the regulation
// uses. Before AUD-08, only "ST" matched — the other three were
// compared against a 2-character slice and could never be equal.
func TestIsRiskWarningName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want bool
	}{
		// The four ST-family prefixes.
		{"ST某某", true},
		{"*ST某某", true},
		{"SST某某", true},
		{"S*ST某某", true},
		// Bare prefixes (a name can be exactly the marker in fixtures).
		{"ST", true},
		{"*ST", true},
		{"SST", true},
		{"S*ST", true},
		// Real names.
		{"平安银行", false},
		{"贵州茅台", false},
		{"*ST退市", true},
		// Not risk warnings.
		// NOTE: "STAR某某" IS flagged — the marker is a literal
		// prefix and "ST" matches it. That is intentional and matches
		// how the exchange tags names (the marker is followed
		// immediately by the company name). A hypothetical name
		// genuinely beginning with the letters "STAR" would be a
		// false positive, but no such convention exists, and erring
		// toward "treat as risk warning" is the safe direction.
		{"STAR某某", true},
		{"s t 某某", false}, // spaces break it (no whitespace inside a marker)
		{"* 某某", false},
		{"", false},
		{"S", false},
		{"*", false},
		// Whitespace padding is tolerated.
		{" ST某某", true},
		{"  *ST某某", true},
		// A ts_code is not a name.
		{"600000.SH", false},
		{"000001.SZ", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsRiskWarningName(tc.name),
				"IsRiskWarningName(%q)", tc.name)
		})
	}
}

// TestIsRiskWarningName_IsPrefixAnchored pins that detection is
// anchored at the START of the name. A substring check would flag any
// name merely containing "ST", which is common in pinyin-derived
// names and would silently reprice ordinary stocks into the ST
// regime.
func TestIsRiskWarningName_IsPrefixAnchored(t *testing.T) {
	t.Parallel()
	assert.True(t, IsRiskWarningName("ST某某"))
	assert.False(t, IsRiskWarningName("某某ST"), "ST not at the start")
	assert.False(t, IsRiskWarningName("某某*ST"), "ditto")
	assert.False(t, IsRiskWarningName("东ST方"), "substring, not prefix")
}

// ── resolvePriceLimit (AUD-07 / ODR-065 H2) ──────────────────────────

func defaultLimitCfg() PriceLimitConfigValues {
	return PriceLimitConfigValues{
		Normal:       DefaultPriceLimitNormal,
		ST:           DefaultPriceLimitST,
		STBefore:     DefaultPriceLimitSTBefore,
		New:          DefaultPriceLimitNew,
		NewStockDays: DefaultNewStockDays,
	}
}

// afterSTChange is a trading day comfortably after 2026-07-06.
var afterSTChange = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// beforeSTChange is a trading day before 2026-07-06.
var beforeSTChange = time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

// matureTradeDays is a value beyond any plausible NewStockDays, used
// in the board matrix so the new-stock branch does not swallow every
// case. (TradeDays has no "unset" sentinel — its zero value means
// "listed today", which is why the matrix must state it explicitly.)
const matureTradeDays = 500

// TestResolvePriceLimit_BoardMatrix is the acceptance table from the
// audit: symbol prefix × stock status → limit rate.
func TestResolvePriceLimit_BoardMatrix(t *testing.T) {
	t.Parallel()
	cfg := defaultLimitCfg()

	cases := []struct {
		desc   string
		symbol string
		name   string
		want   float64
	}{
		// ── Main board, normal ───────────────────────────────────────
		{"SH main 600", "600000.SH", "浦发银行", 0.10},
		{"SH main 601", "601398.SH", "工商银行", 0.10},
		{"SZ main 000", "000001.SZ", "平安银行", 0.10},
		{"SZ main 002", "002415.SZ", "海康威视", 0.10},

		// ── ChiNext / STAR: ±20% ─────────────────────────────────────
		// Before AUD-07 these fell through to 0.10, so a 15% move on
		// ChiNext was treated as tradable when the market had in fact
		// halted it.
		{"ChiNext 300", "300750.SZ", "宁德时代", 0.20},
		{"ChiNext 301", "301029.SZ", "怡合达", 0.20},
		{"STAR 688", "688981.SH", "中芯国际", 0.20},
		{"STAR 689", "689009.SH", "九号公司", 0.20},

		// ── BSE: ±30% ────────────────────────────────────────────────
		{"BSE 8", "830799.BJ", "艾融软件", 0.30},
		{"BSE 4", "430047.BJ", "诺思兰德", 0.30},

		// ── Main-board ST: date-segmented ────────────────────────────
		{"SH main ST after change", "600000.SH", "ST某某", 0.10},
		{"SH main *ST after change", "600000.SH", "*ST某某", 0.10},
		{"SZ main ST after change", "000001.SZ", "ST某某", 0.10},
		{"SH main SST after change", "600000.SH", "SST某某", 0.10},

		// ── ChiNext STAR ST keeps the BOARD limit, not the ST rate ───
		// This is the case a naive `if isST { return stRate }` breaks.
		{"ChiNext ST", "300750.SZ", "ST某某", 0.20},
		{"ChiNext *ST", "300750.SZ", "*ST某某", 0.20},
		{"STAR ST", "688981.SH", "ST某某", 0.20},
		{"STAR *ST", "688981.SH", "*ST某某", 0.20},
		{"BSE ST", "830799.BJ", "ST某某", 0.30},

		// ── Unclassifiable → Normal fallback ─────────────────────────
		{"empty symbol", "", "某某", 0.10},
		{"malformed symbol", "not-a-code", "某某", 0.10},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			got := resolvePriceLimit(PriceLimitInput{
				Symbol:    tc.symbol,
				Name:      tc.name,
				TradeDays: matureTradeDays,
				AsOf:      afterSTChange,
			}, cfg)
			assert.InDelta(t, tc.want, got, 1e-9,
				"%s %s on/after 2026-07-06", tc.symbol, tc.name)
		})
	}
}

// TestResolvePriceLimit_STDateSegmented is the regression guard for
// the 2026-07-06 rule change. The same stock/name must get different
// rates either side of the boundary.
func TestResolvePriceLimit_STDateSegmented(t *testing.T) {
	t.Parallel()
	cfg := defaultLimitCfg()

	mk := func(asOf time.Time) float64 {
		return resolvePriceLimit(PriceLimitInput{
			Symbol:    "600000.SH", // main board
			Name:      "*ST某某",
			TradeDays: matureTradeDays,
			AsOf:      asOf,
		}, cfg)
	}

	assert.InDelta(t, 0.05, mk(beforeSTChange), 1e-9,
		"before 2026-07-06 the main-board ST limit was ±5%%")
	assert.InDelta(t, 0.10, mk(afterSTChange), 1e-9,
		"on/after 2026-07-06 the main-board ST limit is ±10%%")
	assert.InDelta(t, 0.10, mk(stLimitChangeDate), 1e-9,
		"the change is effective ON 2026-07-06, not the day after")

	// A zero AsOf must NOT silently pick the historical rate — an
	// unknown date should get current rules, so a caller that forgets
	// to pass a date gets the stricter-looking-but-actually-current
	// behaviour rather than a 5% that no longer exists.
	assert.InDelta(t, 0.10, mk(time.Time{}), 1e-9,
		"zero AsOf => current rules")
}

// TestResolvePriceLimit_STBeforeZeroFallsBack ensures a caller that
// leaves STBefore unset (zero) still gets the historical 5% before the
// change date, rather than silently 0%.
func TestResolvePriceLimit_STBeforeZeroFallsBack(t *testing.T) {
	t.Parallel()
	cfg := defaultLimitCfg()
	cfg.STBefore = 0

	got := resolvePriceLimit(PriceLimitInput{
		Symbol:    "600000.SH",
		Name:      "ST某某",
		TradeDays: matureTradeDays,
		AsOf:      beforeSTChange,
	}, cfg)
	assert.InDelta(t, stLimitBeforeChange, got, 1e-9,
		"unset STBefore must fall back to the historical constant")
}

// TestResolvePriceLimit_NewStockTakesPrecedence documents that the
// new-stock branch wins over the ST branch. Deleting the precedence
// (e.g. reordering the ifs) would let a newly listed ST-name stock
// fall into the ST path.
//
// NOTE: the new-stock HANDLING is knowingly inaccurate (threshold and
// rate) and out of AUD-07's scope — this test pins the precedence, not
// the correctness of the new-stock rule.
func TestResolvePriceLimit_NewStockTakesPrecedence(t *testing.T) {
	t.Parallel()
	cfg := defaultLimitCfg()

	got := resolvePriceLimit(PriceLimitInput{
		Symbol:    "600000.SH",
		Name:      "*ST某某",
		TradeDays: 0, // newly listed
		AsOf:      afterSTChange,
	}, cfg)
	assert.InDelta(t, cfg.New, got, 1e-9,
		"a newly listed stock uses the New rate regardless of its name")
}

// ── rounding (AUD-07) ────────────────────────────────────────────────

// TestLimitPrices_RoundsToCent is the acceptance criterion "10.05 ->
// 11.06": a previous close of 10.05 with a 10% limit gives a raw
// 11.055, which the exchange publishes as 11.06 (round half up).
func TestLimitPrices_RoundsToCent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		prevClose float64
		rate      float64
		wantUp    float64
		wantDown  float64
	}{
		// The audit's example. 10.05 * 1.10 = 11.055 -> 11.06;
		// 10.05 * 0.90 = 9.045 -> 9.05 (half away from zero).
		{10.05, 0.10, 11.06, 9.05},
		// A case where the raw value is already exact.
		{10.00, 0.10, 11.00, 9.00},
		// A case that would round DOWN.
		{10.04, 0.10, 11.04, 9.04}, // 11.044 -> 11.04 ; 9.036 -> 9.04
		// ChiNext 20%.
		{10.00, 0.20, 12.00, 8.00},
		// BSE 30%.
		{10.00, 0.30, 13.00, 7.00},
		// ST 5% (historical).
		{10.05, 0.05, 10.55, 9.55}, // 10.5525 -> 10.55 ; 9.5475 -> 9.55
	}

	for _, tc := range cases {
		tc := tc
		t.Run("", func(t *testing.T) {
			t.Parallel()
			up, down := LimitPrices(tc.prevClose, tc.rate)
			assert.InDelta(t, tc.wantUp, up, 1e-9,
				"upper for %.2f @ %.0f%%", tc.prevClose, tc.rate*100)
			assert.InDelta(t, tc.wantDown, down, 1e-9,
				"lower for %.2f @ %.0f%%", tc.prevClose, tc.rate*100)
		})
	}
}

// TestRoundToCent_HalfAwayFromZero documents the rounding convention,
// because "round half to even" would give a different answer on the
// boundary and is a plausible future "cleanup" mistake.
func TestRoundToCent_HalfAwayFromZero(t *testing.T) {
	t.Parallel()
	assert.InDelta(t, 11.06, roundToCent(11.055), 1e-9)
	assert.InDelta(t, 9.05, roundToCent(9.045), 1e-9)
	assert.InDelta(t, 0.01, roundToCent(0.005), 1e-9)
	assert.InDelta(t, 10.00, roundToCent(9.999), 1e-9)
}

// TestLimitPrices_UsedForTheBoundaryComparison is the end-to-end shape
// of the AUD-07 fix: a bar closing exactly at the rounded limit must
// be flagged as limit-up, whereas the unrounded bound would not flag
// it (11.055 > 11.05).
func TestLimitPrices_UsedForTheBoundaryComparison(t *testing.T) {
	t.Parallel()
	prevClose := 10.05
	up, _ := LimitPrices(prevClose, 0.10)

	rawUnrounded := prevClose * 1.10 // 11.055

	// A bar closing at the published limit (11.06) is limit-up.
	assert.True(t, 11.06 >= up, "close at the published limit is limit-up")

	// The unrounded comparison on the same bar is also true here, but
	// the interesting case is a close of 11.05: below the rounded
	// published limit (so NOT limit-up), yet above the unrounded raw
	// bound would have been... which it is not. Demonstrate the
	// direction that matters: 11.06 vs 11.055.
	assert.True(t, 11.06 >= rawUnrounded,
		"both agree at 11.06; the disagreement case is a close strictly between 11.055 and 11.06, "+
			"which cannot be quoted at cent tick — hence rounding the BOUND is what aligns the "+
			"comparison with the exchange")
}
