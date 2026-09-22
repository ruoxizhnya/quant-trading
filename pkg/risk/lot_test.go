package risk

import (
	"context"
	"math"
	"testing"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Representative symbols per board. Kept as named constants so the
// board a case is meant to exercise is obvious at the call site.
const (
	symMainSH = "600000.SH" // 沪市主板
	symMainSZ = "000001.SZ" // 深市主板
	symSME    = "002415.SZ" // 中小板 (深市主板段)
	symChi300 = "300750.SZ" // 创业板
	symChi301 = "301029.SZ" // 创业板注册制段
	symSTAR   = "688981.SH" // 科创板
	symSTARCD = "689009.SH" // 科创板 CDR
	symBSE8   = "830799.BJ" // 北交所 (8 段)
	symBSE4   = "430047.BJ" // 北交所 (4 段)
)

// TestNormalizeOrderQuantity_BoardMatrix pins the per-board rounding.
// The three boards must not collapse to one rule: main board floors to
// a multiple of 100, STAR and BSE keep whole shares above their floor.
func TestNormalizeOrderQuantity_BoardMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc   string
		symbol string
		in     float64
		want   float64
	}{
		// ── Main board / ChiNext: exact multiples of 100 ──────────
		{"SH main 5013.7", symMainSH, 5013.7, 5000},
		{"SZ main 5013.7", symMainSZ, 5013.7, 5000},
		{"SME 5013.7", symSME, 5013.7, 5000},
		{"ChiNext 300", symChi300, 5013.7, 5000},
		{"ChiNext 301", symChi301, 5013.7, 5000},
		{"SH main exact 5000", symMainSH, 5000, 5000},
		{"SH main just under 5099", symMainSH, 5099.9, 5000},

		// ── STAR (科创板): floor to whole shares, min 200 ─────────
		// Rounding these to 100 would systematically under-buy.
		{"STAR 5013.7", symSTAR, 5013.7, 5013},
		{"STAR CDR 5013.7", symSTARCD, 5013.7, 5013},
		{"STAR exact 5000", symSTAR, 5000, 5000},
		{"STAR 201", symSTAR, 201, 201},

		// ── BSE (北交所): floor to whole shares, min 100 ──────────
		{"BSE 8x 5013.7", symBSE8, 5013.7, 5013},
		{"BSE 4x 5013.7", symBSE4, 5013.7, 5013},
		{"BSE exact 500", symBSE8, 500, 500},
		{"BSE 101", symBSE8, 101, 101},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			got := NormalizeOrderQuantity(tc.in, tc.symbol)
			assert.InDelta(t, tc.want, got, 1e-9,
				"%s: %v shares -> want %v", tc.symbol, tc.in, tc.want)
		})
	}
}

// TestNormalizeOrderQuantity_LiftsSubMinimumToMinimum pins the
// deliberate choice that an under-sized order is raised to the board
// minimum rather than dropped. Note the consequence, asserted
// explicitly below: this can amplify the order substantially.
func TestNormalizeOrderQuantity_LiftsSubMinimumToMinimum(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc   string
		symbol string
		in     float64
		want   float64
	}{
		{"main 50 -> 100", symMainSH, 50, 100},
		{"main 99.9 -> 100", symMainSH, 99.9, 100},
		{"main 1 -> 100", symMainSH, 1, 100},
		{"ChiNext 30 -> 100", symChi300, 30, 100},
		{"STAR 150 -> 200", symSTAR, 150, 200},
		{"STAR 199 -> 200", symSTAR, 199, 200},
		{"BSE 50 -> 100", symBSE8, 50, 100},
		{"BSE 99 -> 100", symBSE8, 99, 100},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			got := NormalizeOrderQuantity(tc.in, tc.symbol)
			assert.InDelta(t, tc.want, got, 1e-9)
		})
	}
}

// TestNormalizeOrderQuantity_AmplificationIsBoundedAboveOneShare
// documents the sharp edge of "lift to minimum": one whole share on
// the main board becomes a 100-share order (100x), while 0.99 shares
// becomes nothing. The cliff is intentional — sub-share quotients come
// from a position value too small to trade, not from a real intent —
// but it is worth knowing exactly where it sits.
func TestNormalizeOrderQuantity_AmplificationIsBoundedAboveOneShare(t *testing.T) {
	t.Parallel()

	assert.InDelta(t, 100, NormalizeOrderQuantity(1.0, symMainSH), 1e-9,
		"exactly one share is a real intent, so it is lifted to one lot")
	assert.InDelta(t, 0, NormalizeOrderQuantity(0.99, symMainSH), 1e-9,
		"just under one share is treated as no intent")

	// The worst-case amplification on each board, for the record.
	assert.InDelta(t, 100, NormalizeOrderQuantity(1, symMainSH), 1e-9) // 100x
	assert.InDelta(t, 200, NormalizeOrderQuantity(1, symSTAR), 1e-9)   // 200x
	assert.InDelta(t, 100, NormalizeOrderQuantity(1, symBSE8), 1e-9)   // 100x
}

// TestNormalizeOrderQuantity_SubShareAndNonPositiveYieldZero covers
// the inputs that must not become orders.
func TestNormalizeOrderQuantity_SubShareAndNonPositiveYieldZero(t *testing.T) {
	t.Parallel()

	for _, sym := range []string{symMainSH, symChi300, symSTAR, symBSE8} {
		sym := sym
		t.Run(sym, func(t *testing.T) {
			t.Parallel()
			for _, in := range []float64{0, -1, -100, 0.0001, 0.5, 0.999} {
				assert.InDelta(t, 0, NormalizeOrderQuantity(in, sym), 1e-9,
					"%s: %v should yield no order", sym, in)
			}
		})
	}
}

// TestNormalizeOrderQuantity_ResultIsAlwaysOrderable is the
// specification-level guard. Rather than pinning individual numbers it
// asserts the property every output must satisfy, so it keeps working
// if the exact rounding strategy is ever revisited.
func TestNormalizeOrderQuantity_ResultIsAlwaysOrderable(t *testing.T) {
	t.Parallel()

	inputs := []float64{
		1, 50, 99, 99.9, 100, 101, 150, 199, 200, 201, 617.28,
		1000, 5013.7, 99999.9, 123456.78,
	}

	check := func(t *testing.T, symbol string, minShares, lotMultiple float64) {
		t.Helper()
		for _, in := range inputs {
			got := NormalizeOrderQuantity(in, symbol)
			if got == 0 {
				continue // "no order" is always acceptable
			}
			assert.GreaterOrEqual(t, got, minShares,
				"%s: %v -> %v is below the board minimum %v",
				symbol, in, got, minShares)
			if lotMultiple > 0 {
				assert.InDelta(t, 0, math.Mod(got, lotMultiple), 1e-9,
					"%s: %v -> %v is not a multiple of %v",
					symbol, in, got, lotMultiple)
			}
			assert.LessOrEqual(t, got, math.Max(in, minShares)+1e-9,
				"%s: %v -> %v should never exceed the requested size "+
					"(except for the lift to the board minimum)",
				symbol, in, got)
		}
	}

	t.Run("main board", func(t *testing.T) {
		t.Parallel()
		check(t, symMainSH, LotSize, LotSize)
	})
	t.Run("ChiNext", func(t *testing.T) {
		t.Parallel()
		check(t, symChi300, LotSize, LotSize)
	})
	t.Run("STAR", func(t *testing.T) {
		t.Parallel()
		check(t, symSTAR, STARMinShares, 0) // 1-share increments allowed
	})
	t.Run("BSE", func(t *testing.T) {
		t.Parallel()
		check(t, symBSE8, BSEMinShares, 0)
	})
}

// TestNormalizeOrderQuantity_UnknownSymbolUsesMainBoardRule — an
// unclassifiable symbol falls back to the strictest rule (100
// multiples), so a mis-classified symbol still yields a legal order.
func TestNormalizeOrderQuantity_UnknownSymbolUsesMainBoardRule(t *testing.T) {
	t.Parallel()

	assert.InDelta(t, 5000, NormalizeOrderQuantity(5013.7, ""), 1e-9)
	assert.InDelta(t, 5000, NormalizeOrderQuantity(5013.7, "not-a-code"), 1e-9)
	assert.InDelta(t, 100, NormalizeOrderQuantity(50, "not-a-code"), 1e-9)
}

// ---------------------------------------------------------------------
// Wiring: the normalizer must actually be reachable through the sizers.
// A pure-function test proves the rule is right; these prove it is
// connected. Both are needed — AUD-07 was a case of a correct helper
// nobody called.
// ---------------------------------------------------------------------

// normalizationConfig produces a weight that lands on a deliberately
// non-lot quotient, so an un-normalized size is distinguishable.
//
//	base   = TargetVolatility(0.15) capped to MaxPositionWeight(0.05) = 0.05
//	weight = base * Strength(1.0)                                    = 0.05
//	value  = TotalValue(123456) * 0.05                               = 6172.8
//	size   = 6172.8 / price(10)                                      = 617.28
//
// 617.28 is not a multiple of 100, so the main-board result (600) can
// only be reached through the normalizer.
func normalizationConfig() RiskManagerConfig {
	return RiskManagerConfig{
		TargetVolatility:  0.15,
		MaxPositionWeight: 0.05,
		MinPositionWeight: 0.01,
		ATRPeriod:         14,
		TakeProfitMult:    3.0,
		VolLookbackDays:   60,
	}
}

const (
	normalizationTotalValue = 123456.0
	normalizationPrice      = 10.0
)

func normalizationInputs(symbol string) (domain.Signal, *domain.Portfolio, *domain.MarketRegime, []domain.OHLCV) {
	signal := domain.Signal{
		Symbol:    symbol,
		Direction: domain.DirectionLong,
		Strength:  1.0,
	}
	portfolio := &domain.Portfolio{TotalValue: normalizationTotalValue}
	regime := &domain.MarketRegime{Trend: "sideways", Volatility: "medium"}
	ohlcv := generateTestOHLCV(30, normalizationPrice, 0.02)
	return signal, portfolio, regime, ohlcv
}

func TestRiskManager_CalculatePosition_NormalizesToBoardLot(t *testing.T) {
	t.Parallel()

	rm, err := NewRiskManager(normalizationConfig(), zerolog.Nop())
	require.NoError(t, err)

	// Each board must produce its own answer from the same quotient:
	// main board 600, STAR/BSE 617. If the normalizer were missing or
	// board-blind, all three would be 617.
	cases := []struct {
		desc   string
		symbol string
		want   float64
	}{
		{"main board floors to 600", symMainSH, 600},
		{"ChiNext floors to 600", symChi300, 600},
		{"STAR keeps whole shares 617", symSTAR, 617},
		{"BSE keeps whole shares 617", symBSE8, 617},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			signal, portfolio, regime, ohlcv := normalizationInputs(tc.symbol)

			ps, err := rm.CalculatePosition(
				context.Background(), signal, portfolio, regime, normalizationPrice, ohlcv)
			require.NoError(t, err)

			assert.InDelta(t, tc.want, ps.Size, 1e-9,
				"%s: size should be normalized for the board", tc.symbol)
		})
	}
}

// TestRiskManager_CalculatePosition_MainBoardSizeIsAlwaysALot is the
// regression guard stated the way the exchange states it: whatever the
// inputs, a main-board order size must be a multiple of 100.
func TestRiskManager_CalculatePosition_MainBoardSizeIsAlwaysALot(t *testing.T) {
	t.Parallel()

	rm, err := NewRiskManager(normalizationConfig(), zerolog.Nop())
	require.NoError(t, err)

	// Sweep position values so the raw quotient lands all over the
	// place, including inside the first lot and below one share.
	for _, total := range []float64{
		1, 100, 500, 1000, 5000, 12345, 123456, 999999, 1000000,
	} {
		total := total
		t.Run("", func(t *testing.T) {
			t.Parallel()
			signal := domain.Signal{
				Symbol:    symMainSH,
				Direction: domain.DirectionLong,
				Strength:  1.0,
			}
			portfolio := &domain.Portfolio{TotalValue: total}
			regime := &domain.MarketRegime{Trend: "sideways", Volatility: "medium"}
			ohlcv := generateTestOHLCV(30, normalizationPrice, 0.02)

			ps, err := rm.CalculatePosition(
				context.Background(), signal, portfolio, regime, normalizationPrice, ohlcv)
			require.NoError(t, err)

			if ps.Size == 0 {
				return
			}
			assert.InDelta(t, 0, math.Mod(ps.Size, LotSize), 1e-9,
				"TotalValue=%v -> size %v is not a multiple of %v",
				total, ps.Size, LotSize)
			assert.GreaterOrEqual(t, ps.Size, float64(LotSize),
				"TotalValue=%v -> size %v is below one lot", total, ps.Size)
		})
	}
}

// TestRiskManager_CalculatePositionsBatch_NormalizesLikeSingle keeps
// the batch path (a second copy of the same arithmetic) from drifting
// away from the single path.
func TestRiskManager_CalculatePositionsBatch_NormalizesLikeSingle(t *testing.T) {
	t.Parallel()

	rm, err := NewRiskManager(normalizationConfig(), zerolog.Nop())
	require.NoError(t, err)

	for _, symbol := range []string{symMainSH, symChi300, symSTAR, symBSE8} {
		symbol := symbol
		t.Run(symbol, func(t *testing.T) {
			t.Parallel()
			signal, portfolio, regime, ohlcv := normalizationInputs(symbol)
			ohlcvData := map[string][]domain.OHLCV{symbol: ohlcv}
			prices := map[string]float64{symbol: normalizationPrice}

			single, err := rm.CalculatePosition(
				context.Background(), signal, portfolio, regime, normalizationPrice, ohlcv)
			require.NoError(t, err)

			batch, err := rm.CalculatePositionsBatch(
				context.Background(), []domain.Signal{signal}, portfolio, regime, prices, ohlcvData)
			require.NoError(t, err)

			batchPS, ok := batch[symbol]
			require.True(t, ok, "batch result missing for %s", symbol)
			assert.InDelta(t, single.Size, batchPS.Size, 1e-9,
				"%s: batch size must match single size", symbol)
		})
	}
}

// TestRiskManager_CalculatePosition_STARKeepsAboveMinimum guards the
// case a blanket "round to 100" would break: a STAR quotient that is
// legal as-is (617) must not be cut down to 600.
func TestRiskManager_CalculatePosition_STARKeepsAboveMinimum(t *testing.T) {
	t.Parallel()

	rm, err := NewRiskManager(normalizationConfig(), zerolog.Nop())
	require.NoError(t, err)

	signal, portfolio, regime, ohlcv := normalizationInputs(symSTAR)
	ps, err := rm.CalculatePosition(
		context.Background(), signal, portfolio, regime, normalizationPrice, ohlcv)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, ps.Size, float64(STARMinShares),
		"STAR order must be at least %d shares", STARMinShares)
	assert.InDelta(t, 617, ps.Size, 1e-9,
		"STAR keeps the whole-share quotient (617); a blanket "+
			"100-multiple rule would have cut it to 600")
}

// ── ValidateOrderQuantity (AUD-21) ────────────────────────────────────

// TestValidateOrderQuantity_BoardMatrix pins the per-board BUY rules at
// the validation boundary. It mirrors TestNormalizeOrderQuantity_
// BoardMatrix on purpose: whatever the normalizer emits for a board must
// validate clean on that same board.
//
// The registry's headline case is in here — 617 shares on STAR is a legal
// order that the old blanket `int(quantity)%100 != 0` in the XTP broker
// refused.
func TestValidateOrderQuantity_BoardMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      float64
		symbol  string
		wantErr bool
	}{
		{"main board lot", 100, symMainSH, false},
		{"main board 1000", 1000, symMainSH, false},
		{"main board odd 137", 137, symMainSH, true},
		{"ChiNext lot", 300, symChi300, false},
		{"ChiNext odd 301", 301, symChi300, true},
		{"STAR floor 200", 200, symSTAR, false},
		{"STAR 201 (1-share increment)", 201, symSTAR, false},
		{"STAR 617 (the registry's case)", 617, symSTAR, false},
		{"STAR 150 (below floor)", 150, symSTAR, true},
		{"STAR CDR 250", 250, symSTARCD, false},
		{"BSE floor 100", 100, symBSE8, false},
		{"BSE 150 (1-share increment)", 150, symBSE8, false},
		{"BSE 50 (below floor)", 50, symBSE8, true},
		{"BSE 4-segment 101", 101, symBSE4, false},
		{"unknown symbol falls back to the main-board rule", 137, "not-a-code", true},
		{"unknown symbol, valid lot", 500, "not-a-code", false},
		{"fractional 100.9 must NOT pass via truncation", 100.9, symMainSH, true},
		{"fractional STAR 250.5", 250.5, symSTAR, true},
		{"zero", 0, symMainSH, true},
		{"negative", -100, symMainSH, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateOrderQuantity(tc.in, tc.symbol, false)
			if tc.wantErr {
				assert.Error(t, err, "expected %v %s to be rejected", tc.in, tc.symbol)
				return
			}
			assert.NoError(t, err, "expected %v %s to be accepted", tc.in, tc.symbol)
		})
	}
}

// TestValidateOrderQuantity_NormalizerOutputAlwaysValidates is the
// cross-check that closes AUD-21 at the root.
//
// The defect was not that either half was wrong — NormalizeOrderQuantity
// was board-correct and the broker's check was a faithful implementation
// of the MAIN-BOARD rule. It was that the two disagreed, so the sizer
// produced quantities the next hop refused. This test states the
// invariant directly: normalizer output must always validate.
//
// It is a property test over (board × input) rather than a fixed table so
// that adding a board or changing a floor cannot silently re-open the
// gap — the failing pair is reported in the message.
func TestValidateOrderQuantity_NormalizerOutputAlwaysValidates(t *testing.T) {
	t.Parallel()

	symbols := []string{
		symMainSH, symMainSZ, symSME, symChi300, symChi301,
		symSTAR, symSTARCD, symBSE8, symBSE4, "not-a-code", "",
	}
	inputs := []float64{
		-1, 0, 0.5, 1, 50, 99, 100, 137, 199.9, 200, 201,
		250.5, 617, 5013.7, 99_999, 1_000_000,
	}

	for _, symbol := range symbols {
		for _, in := range inputs {
			got := NormalizeOrderQuantity(in, symbol)
			if got <= 0 {
				// "not orderable at all" is a legitimate normalizer
				// answer and is not the validator's business.
				continue
			}
			assert.NoError(t, ValidateOrderQuantity(got, symbol, false),
				"normalizer emitted %v for symbol %q (input %v) but the validator rejects it",
				got, symbol, in)
		}
	}
}

// TestValidateOrderQuantity_SellAllowsOddLots pins the sell-side
// asymmetry.
//
// 沪 3.3.8 / 深 3.3.8 allow an ODD-LOT sell when it is the whole
// remaining balance ("卖出证券时，余额不足100股（份）部分，应当一次性申报
// 卖出"). This function has no position state, so it cannot tell an
// illegal partial odd lot from a legal final one — and it must not guess,
// because guessing "not a multiple of 100 => reject" would re-create
// exactly the false rejection AUD-21 is about, just on the other side.
// Positivity and integrality still apply.
func TestValidateOrderQuantity_SellAllowsOddLots(t *testing.T) {
	t.Parallel()

	assert.NoError(t, ValidateOrderQuantity(137, symMainSH, true), "odd-lot sell allowed")
	assert.NoError(t, ValidateOrderQuantity(1, symMainSH, true), "1-share sell allowed")
	assert.NoError(t, ValidateOrderQuantity(150, symSTAR, true),
		"STAR sell below the BUY floor is allowed (may be the whole balance)")
	assert.NoError(t, ValidateOrderQuantity(50, symBSE8, true),
		"BSE sell below the BUY floor is allowed")

	assert.Error(t, ValidateOrderQuantity(0, symMainSH, true), "zero is still invalid")
	assert.Error(t, ValidateOrderQuantity(-100, symMainSH, true), "negative is still invalid")
	assert.Error(t, ValidateOrderQuantity(100.9, symMainSH, true),
		"a fractional sell is still invalid")
}
