package marketdata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── IsRiskWarningName (AUD-08, relocated by AUD-22) ──────────────────

// TestIsRiskWarningName covers the four prefix forms the regulation
// uses. Before AUD-08, only "ST" matched — the other three were
// compared against a 2-character slice and could never be equal.
//
// AUD-22 moved the implementation here from pkg/backtest/pricelimit.go.
// The backtest-side test still exercises the delegating wrapper; this one
// exercises the implementation where it now lives, so a change made here
// cannot hide behind a wrapper that was left correct.
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
		// NOTE: "STAR某某" IS flagged — the marker is a literal prefix and
		// "ST" matches it. Intentional, and matches how the exchange tags
		// names (marker immediately followed by the company name). A name
		// genuinely beginning with the letters "STAR" would be a false
		// positive, but no such convention exists, and erring toward
		// "treat as risk warning" is the safe direction.
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

// TestIsRiskWarningName_IsPrefixAnchored pins that detection is anchored
// at the START of the name. A substring check would flag any name merely
// containing "ST", which is common in pinyin-derived names and would
// silently reprice ordinary stocks into the ST regime.
func TestIsRiskWarningName_IsPrefixAnchored(t *testing.T) {
	t.Parallel()
	assert.True(t, IsRiskWarningName("ST某某"))
	assert.False(t, IsRiskWarningName("某某ST"), "ST not at the start")
	assert.False(t, IsRiskWarningName("某某*ST"), "ditto")
	assert.False(t, IsRiskWarningName("东ST方"), "substring, not prefix")
}

// ── RiskWarningDailyBuyCap (AUD-22) ──────────────────────────────────

// TestRiskWarningDailyBuyCap_BoardMatrix pins the per-board daily buy cap
// for risk-warning stocks.
//
// The three numbers come from three different rule texts and are NOT
// interchangeable — which is exactly why this table exists:
//
//	沪深主板/创业板  50 万股   沪 4.4.10 / 深 4.5.4
//	北交所           20 万股   北 4.5.4
//	科创板           不适用    沪 6.14 科创板 ST 不进风险警示板
//
// The 科创板 row is the one a naive implementation gets wrong in the
// harmful direction: applying the main-board 50 万股 there would reject
// legal STAR trades that the exchange permits.
func TestRiskWarningDailyBuyCap_BoardMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tsCode string
		want   float64
		why    string
	}{
		{"600000.SH", RiskWarningDailyBuyCapMainBoard, "沪主板 4.4.10"},
		{"000001.SZ", RiskWarningDailyBuyCapMainBoard, "深主板 4.5.4"},
		{"300750.SZ", RiskWarningDailyBuyCapMainBoard, "创业板：深 4.5.1 未按板块限定"},
		{"301029.SZ", RiskWarningDailyBuyCapMainBoard, "创业板注册制段"},
		{"830799.BJ", RiskWarningDailyBuyCapBSE, "北交所 4.5.4"},
		{"430047.BJ", RiskWarningDailyBuyCapBSE, "北交所 4 段"},
		{"688981.SH", 0, "科创板：沪 6.14 不进风险警示板 → 无此上限"},
		{"689009.SH", 0, "科创板 CDR 同"},
		// Unclassifiable: bounded rather than uncapped (conservative).
		{"", RiskWarningDailyBuyCapBSE, "unknown → 取较严的一档"},
		{"not-a-code", RiskWarningDailyBuyCapBSE, "unknown → 取较严的一档"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.tsCode, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, RiskWarningDailyBuyCap(tc.tsCode),
				"RiskWarningDailyBuyCap(%q) — %s", tc.tsCode, tc.why)
		})
	}

	// The two caps must not collapse into one value: 沪深 and 北交所
	// differ, and a copy-paste that makes them equal is the most likely
	// way this regresses.
	assert.NotEqual(t, RiskWarningDailyBuyCapMainBoard, RiskWarningDailyBuyCapBSE,
		"沪深 50 万股 and 北交所 20 万股 are different rules; equal values mean one was copied")
}
