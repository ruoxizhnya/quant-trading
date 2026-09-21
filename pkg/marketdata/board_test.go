package marketdata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestClassifySymbol covers the code-prefix → board mapping.
//
// board.go had no test at all before AUD-07, which is how 301xxx and
// 689xxx went missing: both fell through to BoardUnknown and were
// priced at ±10% instead of ±20%. The table below pins every prefix
// the classifier claims to handle, so a future edit that drops one is
// caught here rather than in a backtest's P&L.
func TestClassifySymbol(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tsCode string
		want   Board
	}{
		// ── Shanghai main board ──────────────────────────────────────
		{"600000.SH", BoardMainBoardSH},
		{"601398.SH", BoardMainBoardSH},
		{"603259.SH", BoardMainBoardSH},
		{"605499.SH", BoardMainBoardSH},

		// ── STAR (科创板): 688 AND 689 ───────────────────────────────
		{"688981.SH", BoardSTAR},
		{"688111.SH", BoardSTAR},
		{"689009.SH", BoardSTAR}, // CDR — missing before AUD-07

		// ── Shenzhen main board ──────────────────────────────────────
		{"000001.SZ", BoardMainBoardSZ},
		{"001979.SZ", BoardMainBoardSZ},
		{"002415.SZ", BoardMainBoardSZ},

		// ── ChiNext (创业板): 300 AND 301 ────────────────────────────
		{"300750.SZ", BoardChiNext},
		{"300059.SZ", BoardChiNext},
		{"301029.SZ", BoardChiNext}, // registration-based — missing before AUD-07

		// ── BSE (北交所) ─────────────────────────────────────────────
		{"830799.BJ", BoardBSE},
		{"430047.BJ", BoardBSE},

		// ── Funds ────────────────────────────────────────────────────
		{"510300.SH", BoardETF},
		{"159915.SZ", BoardETF},
		{"160105.SZ", BoardFundLOF},

		// ── Indices / bonds ──────────────────────────────────────────
		{"000300.SH", BoardIndex},
		{"399001.SZ", BoardIndex},
		{"113050.SH", BoardBond},

		// ── Malformed → Unknown (conservative) ───────────────────────
		{"", BoardUnknown},
		{"600000", BoardUnknown},
		{"NOTACODE", BoardUnknown},
		{"600000.XX", BoardUnknown},
		{"12345.SH", BoardUnknown}, // too short
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.tsCode, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ClassifySymbol(tc.tsCode))
		})
	}
}

// TestBoard_DailyPriceLimit pins the rate per board. These are the
// values the backtest price-limit resolver and the live price cage
// both depend on.
func TestBoard_DailyPriceLimit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		board Board
		want  float64
	}{
		{BoardMainBoardSH, 0.10},
		{BoardMainBoardSZ, 0.10},
		{BoardChiNext, 0.20},
		{BoardSTAR, 0.20},
		{BoardBSE, 0.30},
		{BoardETF, 0.10},
		{BoardFundLOF, 0.10},
		{BoardBond, 0.10},
		{BoardUnknown, 0.10}, // conservative default
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.board), func(t *testing.T) {
			t.Parallel()
			assert.InDelta(t, tc.want, tc.board.DailyPriceLimit(), 1e-9)
		})
	}
}

// TestClassifySymbol_BoardLimitEndToEnd ties the two together for the
// cases AUD-07 was about: the symbol alone must yield the right rate.
func TestClassifySymbol_BoardLimitEndToEnd(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tsCode string
		want   float64
	}{
		{"600000.SH", 0.10},
		{"301029.SZ", 0.20}, // regression: was 0.10
		{"689009.SH", 0.20}, // regression: was 0.10
		{"830799.BJ", 0.30},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.tsCode, func(t *testing.T) {
			t.Parallel()
			got := ClassifySymbol(tc.tsCode).DailyPriceLimit()
			assert.InDelta(t, tc.want, got, 1e-9,
				"%s must have a ±%.0f%% limit", tc.tsCode, tc.want*100)
		})
	}
}
