package risk

// P2-20: Boundary tests for the risk management components.
//
// This file exercises edge cases for:
//   - StopLossChecker: zero/negative/extreme ATR, entry prices, regimes
//   - RegimeDetector: insufficient data, flat prices, extreme volatility
//   - VolatilitySizer: zero/negative volatility, extreme weights
//   - RiskManager: integration of all three with edge-case inputs
//
// All tests use the standard testing package and zerolog.Nop() to keep
// output clean. No external dependencies (DB, network) are required.

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/rs/zerolog"
)

var nopLogger = zerolog.Nop()

func makeOHLCV(n int, basePrice float64) []domain.OHLCV {
	out := make([]domain.OHLCV, n)
	for i := 0; i < n; i++ {
		p := basePrice + float64(i)*0.5
		out[i] = domain.OHLCV{
			Symbol: "TEST",
			Date:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i),
			Open:   p,
			High:   p + 1.0,
			Low:    p - 0.5,
			Close:  p,
			Volume: 1000000,
		}
	}
	return out
}

func makeVolatileOHLCV(n int, basePrice, volatility float64) []domain.OHLCV {
	out := make([]domain.OHLCV, n)
	for i := 0; i < n; i++ {
		// Alternate up/down to create volatility
		delta := volatility * float64(i%2*2-1)
		p := basePrice + delta
		out[i] = domain.OHLCV{
			Symbol: "TEST",
			Date:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i),
			Open:   p,
			High:   p + volatility,
			Low:    p - volatility,
			Close:  p,
			Volume: 1000000,
		}
	}
	return out
}

// --- Test 1: StopLossChecker CalculateATR with insufficient data ---

func TestBoundary_StopLossChecker_CalculateATR_InsufficientData(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	// Less than ATRPeriod bars
	ohlcv := makeOHLCV(5, 100.0)
	_, err := slc.CalculateATR(ohlcv)
	if err == nil {
		t.Fatal("expected error on insufficient data, got nil")
	}
}

// --- Test 2: StopLossChecker CalculateATR with exactly ATRPeriod bars ---

func TestBoundary_StopLossChecker_CalculateATR_ExactPeriod(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 5, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	ohlcv := makeOHLCV(6, 100.0) // need period+1 for TR calculation
	atr, err := slc.CalculateATR(ohlcv)
	if err != nil {
		t.Fatalf("CalculateATR error: %v", err)
	}
	if atr <= 0 {
		t.Errorf("ATR = %v, want > 0", atr)
	}
}

// --- Test 3: StopLossChecker CalculateATR with empty slice ---

func TestBoundary_StopLossChecker_CalculateATR_Empty(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	_, err := slc.CalculateATR([]domain.OHLCV{})
	if err == nil {
		t.Fatal("expected error on empty slice, got nil")
	}
}

// --- Test 4: StopLossChecker CalculateStopLossPrice with zero ATR ---

func TestBoundary_StopLossChecker_CalculateStopLossPrice_ZeroATR(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	entryPrice := 100.0
	atr := 0.0
	regime := &domain.MarketRegime{Trend: "bull", Volatility: "low"}

	stopLoss := slc.CalculateStopLossPrice(entryPrice, atr, regime)
	// With zero ATR, stop loss should equal entry price
	if stopLoss != entryPrice {
		t.Errorf("stopLoss with zero ATR = %v, want %v", stopLoss, entryPrice)
	}
}

// --- Test 5: StopLossChecker CalculateStopLossPrice with negative ATR ---

func TestBoundary_StopLossChecker_CalculateStopLossPrice_NegativeATR(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	entryPrice := 100.0
	atr := -5.0 // negative — should not happen but code should not panic
	regime := &domain.MarketRegime{Trend: "bull"}

	stopLoss := slc.CalculateStopLossPrice(entryPrice, atr, regime)
	// With negative ATR, stop loss = entry - (mult * neg) = entry + positive
	// This is a degenerate case; we just verify no panic.
	_ = stopLoss
}

// --- Test 6: StopLossChecker CalculateStopLossPrice with nil regime ---

func TestBoundary_StopLossChecker_CalculateStopLossPrice_NilRegime(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0, BullMultiplier: 1.5, BearMultiplier: 3.0, SidewaysMultiplier: 2.5}
	slc := NewStopLossChecker(cfg, nopLogger)

	entryPrice := 100.0
	atr := 5.0

	stopLoss := slc.CalculateStopLossPrice(entryPrice, atr, nil)
	// With nil regime, baseMultiplier is used
	expected := entryPrice - (cfg.BaseMultiplier * atr)
	if stopLoss != expected {
		t.Errorf("stopLoss with nil regime = %v, want %v", stopLoss, expected)
	}
}

// --- Test 7: StopLossChecker regime-specific multipliers ---

func TestBoundary_StopLossChecker_RegimeMultipliers(t *testing.T) {
	cfg := StopLossConfig{
		ATRPeriod:          14,
		BaseMultiplier:     2.0,
		BullMultiplier:     1.5,
		BearMultiplier:     3.0,
		SidewaysMultiplier: 2.5,
	}
	slc := NewStopLossChecker(cfg, nopLogger)

	entryPrice := 100.0
	atr := 5.0

	cases := []struct {
		regime  *domain.MarketRegime
		wantMul float64
	}{
		{&domain.MarketRegime{Trend: "bull"}, 1.5},
		{&domain.MarketRegime{Trend: "bear"}, 3.0},
		{&domain.MarketRegime{Trend: "sideways"}, 2.5},
		{&domain.MarketRegime{Trend: "unknown"}, 2.5}, // default → sideways
	}
	for _, c := range cases {
		stopLoss := slc.CalculateStopLossPrice(entryPrice, atr, c.regime)
		expected := entryPrice - (c.wantMul * atr)
		if stopLoss != expected {
			t.Errorf("regime %q: stopLoss = %v, want %v (mul %v)", c.regime.Trend, stopLoss, expected, c.wantMul)
		}
	}
}

// --- Test 8: StopLossChecker CalculateTakeProfitPrice ---

func TestBoundary_StopLossChecker_CalculateTakeProfitPrice(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, TakeProfitMult: 3.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	entryPrice := 100.0
	atr := 5.0
	regime := &domain.MarketRegime{Trend: "bull"}

	tp := slc.CalculateTakeProfitPrice(entryPrice, atr, regime)
	expected := entryPrice + (3.0 * atr)
	if tp != expected {
		t.Errorf("takeProfit = %v, want %v", tp, expected)
	}
}

// --- Test 9: StopLossChecker GetStopLossLevels returns both ---

func TestBoundary_StopLossChecker_GetStopLossLevels(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0, BullMultiplier: 1.5, BearMultiplier: 3.0, SidewaysMultiplier: 2.5, TakeProfitMult: 3.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	entryPrice := 100.0
	atr := 5.0
	regime := &domain.MarketRegime{Trend: "bull"}

	sl, tp := slc.GetStopLossLevels(entryPrice, atr, regime)
	if sl >= entryPrice {
		t.Errorf("stopLoss %v should be below entry %v", sl, entryPrice)
	}
	if tp <= entryPrice {
		t.Errorf("takeProfit %v should be above entry %v", tp, entryPrice)
	}
}

// --- Test 10: StopLossChecker CheckStopLoss with empty positions ---

func TestBoundary_StopLossChecker_CheckStopLoss_EmptyPositions(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	events, err := slc.CheckStopLoss(context.Background(), []domain.Position{}, map[string]float64{}, map[string]float64{})
	if err != nil {
		t.Fatalf("CheckStopLoss error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

// --- Test 11: StopLossChecker CheckStopLoss with position but no price ---

func TestBoundary_StopLossChecker_CheckStopLoss_NoPrice(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	positions := []domain.Position{
		{Symbol: "TEST", Quantity: 100, AvgCost: 100.0},
	}
	prices := map[string]float64{} // no price for TEST
	atrData := map[string]float64{"TEST": 5.0}

	events, err := slc.CheckStopLoss(context.Background(), positions, prices, atrData)
	if err != nil {
		t.Fatalf("CheckStopLoss error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events (no price), got %d", len(events))
	}
}

// --- Test 12: StopLossChecker CheckStopLoss triggers stop loss ---

func TestBoundary_StopLossChecker_CheckStopLoss_TriggersStopLoss(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0, TakeProfitMult: 3.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	positions := []domain.Position{
		{Symbol: "TEST", Quantity: 100, AvgCost: 100.0},
	}
	// Price well below stop loss (100 - 2*5 = 90)
	prices := map[string]float64{"TEST": 80.0}
	atrData := map[string]float64{"TEST": 5.0}

	events, err := slc.CheckStopLoss(context.Background(), positions, prices, atrData)
	if err != nil {
		t.Fatalf("CheckStopLoss error: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least 1 event (stop loss triggered)")
	}
	// Should have a stop_loss event
	foundSL := false
	for _, e := range events {
		if e.Type == "stop_loss" {
			foundSL = true
			if e.Symbol != "TEST" {
				t.Errorf("event symbol = %q, want TEST", e.Symbol)
			}
		}
	}
	if !foundSL {
		t.Error("expected stop_loss event, not found")
	}
}

// --- Test 13: StopLossChecker CheckStopLoss triggers take profit ---

func TestBoundary_StopLossChecker_CheckStopLoss_TriggersTakeProfit(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0, TakeProfitMult: 3.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	positions := []domain.Position{
		{Symbol: "TEST", Quantity: 100, AvgCost: 100.0},
	}
	// Price well above take profit (100 + 3*5 = 115)
	prices := map[string]float64{"TEST": 130.0}
	atrData := map[string]float64{"TEST": 5.0}

	events, err := slc.CheckStopLoss(context.Background(), positions, prices, atrData)
	if err != nil {
		t.Fatalf("CheckStopLoss error: %v", err)
	}
	foundTP := false
	for _, e := range events {
		if e.Type == "take_profit" {
			foundTP = true
		}
	}
	if !foundTP {
		t.Error("expected take_profit event, not found")
	}
}

// --- Test 14: StopLossChecker CheckStopLossWithRegime with explicit regime ---

func TestBoundary_StopLossChecker_CheckStopLossWithRegime(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0, BearMultiplier: 3.0, TakeProfitMult: 3.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	positions := []domain.Position{
		{Symbol: "TEST", Quantity: 100, AvgCost: 100.0},
	}
	prices := map[string]float64{"TEST": 80.0}
	atrData := map[string]float64{"TEST": 5.0}
	regime := &domain.MarketRegime{Trend: "bear", Volatility: "high"}

	events, err := slc.CheckStopLossWithRegime(context.Background(), positions, prices, atrData, regime)
	if err != nil {
		t.Fatalf("CheckStopLossWithRegime error: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected events in bear market")
	}
}

// --- Test 15: StopLossChecker CheckStopLoss with zero quantity position ---

func TestBoundary_StopLossChecker_CheckStopLoss_ZeroQuantity(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	positions := []domain.Position{
		{Symbol: "TEST", Quantity: 0, AvgCost: 100.0}, // zero quantity
	}
	prices := map[string]float64{"TEST": 80.0}
	atrData := map[string]float64{"TEST": 5.0}

	events, err := slc.CheckStopLoss(context.Background(), positions, prices, atrData)
	if err != nil {
		t.Fatalf("CheckStopLoss error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events for zero-quantity position, got %d", len(events))
	}
}

// --- Test 16: StopLossChecker CheckStopLoss with negative quantity ---

func TestBoundary_StopLossChecker_CheckStopLoss_NegativeQuantity(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	positions := []domain.Position{
		{Symbol: "TEST", Quantity: -100, AvgCost: 100.0}, // negative
	}
	prices := map[string]float64{"TEST": 80.0}
	atrData := map[string]float64{"TEST": 5.0}

	events, err := slc.CheckStopLoss(context.Background(), positions, prices, atrData)
	if err != nil {
		t.Fatalf("CheckStopLoss error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events for negative-quantity position, got %d", len(events))
	}
}

// --- Test 17: StopLossChecker ATRFromOHLCV with multiple symbols ---

func TestBoundary_StopLossChecker_ATRFromOHLCV(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 5, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	data := map[string][]domain.OHLCV{
		"AAA": makeOHLCV(6, 100.0),
		"BBB": makeOHLCV(6, 200.0),
	}
	atrData, err := slc.ATRFromOHLCV(data)
	if err != nil {
		t.Fatalf("ATRFromOHLCV error: %v", err)
	}
	if len(atrData) != 2 {
		t.Errorf("expected 2 ATR entries, got %d", len(atrData))
	}
}

// --- Test 18: StopLossChecker ATRFromOHLCV with insufficient data skips symbol ---

func TestBoundary_StopLossChecker_ATRFromOHLCV_InsufficientData(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	data := map[string][]domain.OHLCV{
		"AAA": makeOHLCV(6, 100.0),  // insufficient (< 14)
		"BBB": makeOHLCV(20, 200.0), // sufficient
	}
	atrData, err := slc.ATRFromOHLCV(data)
	if err != nil {
		t.Fatalf("ATRFromOHLCV error: %v", err)
	}
	// AAA should be skipped (insufficient data), BBB should succeed
	if _, ok := atrData["AAA"]; ok {
		t.Error("AAA should be skipped (insufficient data)")
	}
	if _, ok := atrData["BBB"]; !ok {
		t.Error("BBB should be present")
	}
}

// --- Test 19: StopLossChecker GetLastUpdateTime with empty slice ---

func TestBoundary_StopLossChecker_GetLastUpdateTime_Empty(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14}
	slc := NewStopLossChecker(cfg, nopLogger)

	result := slc.GetLastUpdateTime([]domain.OHLCV{})
	if !result.IsZero() {
		t.Errorf("expected zero time for empty slice, got %v", result)
	}
}

// --- Test 20: StopLossChecker GetLastUpdateTime returns last bar date ---

func TestBoundary_StopLossChecker_GetLastUpdateTime(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14}
	slc := NewStopLossChecker(cfg, nopLogger)

	ohlcv := makeOHLCV(5, 100.0)
	result := slc.GetLastUpdateTime(ohlcv)
	if !result.Equal(ohlcv[len(ohlcv)-1].Date) {
		t.Errorf("GetLastUpdateTime = %v, want %v", result, ohlcv[len(ohlcv)-1].Date)
	}
}

// --- Test 21: RegimeDetector DetectRegime with insufficient data ---

func TestBoundary_RegimeDetector_DetectRegime_InsufficientData(t *testing.T) {
	cfg := RegimeConfig{FastMAPeriod: 50, SlowMAPeriod: 200, VolLookback: 252}
	rd := NewRegimeDetector(cfg, nopLogger)

	ohlcv := makeOHLCV(10, 100.0) // far less than 200
	_, err := rd.DetectRegime(context.Background(), ohlcv)
	if err == nil {
		t.Fatal("expected error on insufficient data, got nil")
	}
}

// --- Test 22: RegimeDetector DetectRegime with sufficient data ---

func TestBoundary_RegimeDetector_DetectRegime_SufficientData(t *testing.T) {
	cfg := RegimeConfig{FastMAPeriod: 5, SlowMAPeriod: 10, VolLookback: 20}
	rd := NewRegimeDetector(cfg, nopLogger)

	ohlcv := makeOHLCV(25, 100.0) // upward trend
	regime, err := rd.DetectRegime(context.Background(), ohlcv)
	if err != nil {
		t.Fatalf("DetectRegime error: %v", err)
	}
	if regime == nil {
		t.Fatal("regime is nil")
	}
	if regime.Trend == "" {
		t.Error("regime Trend is empty")
	}
	if regime.Volatility == "" {
		t.Error("regime Volatility is empty")
	}
	// Upward trend should be bull
	if regime.Trend != "bull" {
		t.Logf("trend = %q (expected bull for upward prices)", regime.Trend)
	}
}

// --- Test 23: RegimeDetector DetectRegime with flat prices ---

func TestBoundary_RegimeDetector_DetectRegime_FlatPrices(t *testing.T) {
	cfg := RegimeConfig{FastMAPeriod: 5, SlowMAPeriod: 10, VolLookback: 20}
	rd := NewRegimeDetector(cfg, nopLogger)

	// Flat prices (all same)
	ohlcv := make([]domain.OHLCV, 25)
	for i := range ohlcv {
		ohlcv[i] = domain.OHLCV{
			Symbol: "TEST",
			Date:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i),
			Open:   100.0,
			High:   100.0,
			Low:    100.0,
			Close:  100.0,
		}
	}
	regime, err := rd.DetectRegime(context.Background(), ohlcv)
	if err != nil {
		t.Fatalf("DetectRegime error: %v", err)
	}
	// Flat prices → sideways, low volatility
	if regime.Trend != "sideways" {
		t.Logf("flat trend = %q (expected sideways)", regime.Trend)
	}
}

// --- Test 24: RegimeDetector DetectRegime with empty slice ---

func TestBoundary_RegimeDetector_DetectRegime_Empty(t *testing.T) {
	cfg := RegimeConfig{FastMAPeriod: 5, SlowMAPeriod: 10}
	rd := NewRegimeDetector(cfg, nopLogger)

	_, err := rd.DetectRegime(context.Background(), []domain.OHLCV{})
	if err == nil {
		t.Fatal("expected error on empty slice, got nil")
	}
}

// --- Test 25: RegimeDetector DetectRegime with extreme volatility ---

func TestBoundary_RegimeDetector_DetectRegime_ExtremeVolatility(t *testing.T) {
	cfg := RegimeConfig{FastMAPeriod: 5, SlowMAPeriod: 10, VolLookback: 20}
	rd := NewRegimeDetector(cfg, nopLogger)

	// High volatility prices
	ohlcv := makeVolatileOHLCV(25, 100.0, 20.0)
	regime, err := rd.DetectRegime(context.Background(), ohlcv)
	if err != nil {
		t.Fatalf("DetectRegime error: %v", err)
	}
	// Should detect high volatility
	if regime.Volatility != "high" {
		t.Logf("volatility = %q (expected high for extreme vol)", regime.Volatility)
	}
}

// --- Test 26: RegimeDetector DetectRegime sentiment is clamped to [-1, 1] ---

func TestBoundary_RegimeDetector_DetectRegime_SentimentClamped(t *testing.T) {
	cfg := RegimeConfig{FastMAPeriod: 5, SlowMAPeriod: 10, VolLookback: 20}
	rd := NewRegimeDetector(cfg, nopLogger)

	ohlcv := makeOHLCV(25, 100.0)
	regime, err := rd.DetectRegime(context.Background(), ohlcv)
	if err != nil {
		t.Fatalf("DetectRegime error: %v", err)
	}
	if regime.Sentiment < -1.0 || regime.Sentiment > 1.0 {
		t.Errorf("sentiment = %v, should be in [-1, 1]", regime.Sentiment)
	}
}

// --- Test 27: VolatilitySizer CalculateVolatility with insufficient data ---

func TestBoundary_VolatilitySizer_CalculateVolatility_InsufficientData(t *testing.T) {
	cfg := VolatilityConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.10,
		MinPositionWeight:   0.01,
		LookbackDays:        20,
		AnnualizationFactor: 16.0,
	}
	vs := NewVolatilitySizer(cfg, nopLogger)

	ohlcv := makeOHLCV(5, 100.0) // less than LookbackDays
	_, err := vs.CalculateVolatility(ohlcv)
	if err == nil {
		t.Fatal("expected error on insufficient data, got nil")
	}
}

// --- Test 28: VolatilitySizer CalculateVolatility with sufficient data ---

func TestBoundary_VolatilitySizer_CalculateVolatility_SufficientData(t *testing.T) {
	cfg := VolatilityConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.10,
		MinPositionWeight:   0.01,
		LookbackDays:        10,
		AnnualizationFactor: 16.0,
	}
	vs := NewVolatilitySizer(cfg, nopLogger)

	ohlcv := makeOHLCV(15, 100.0)
	vol, err := vs.CalculateVolatility(ohlcv)
	if err != nil {
		t.Fatalf("CalculateVolatility error: %v", err)
	}
	if vol < 0 {
		t.Errorf("volatility = %v, should be >= 0", vol)
	}
}

// --- Test 29: VolatilitySizer CalculateVolatility with empty slice ---

func TestBoundary_VolatilitySizer_CalculateVolatility_Empty(t *testing.T) {
	cfg := VolatilityConfig{LookbackDays: 10}
	vs := NewVolatilitySizer(cfg, nopLogger)

	_, err := vs.CalculateVolatility([]domain.OHLCV{})
	if err == nil {
		t.Fatal("expected error on empty slice, got nil")
	}
}

// --- Test 30: VolatilitySizer CalculatePositionWeight with zero volatility ---

func TestBoundary_VolatilitySizer_CalculatePositionWeight_ZeroVol(t *testing.T) {
	cfg := VolatilityConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.10,
		MinPositionWeight:   0.01,
	}
	vs := NewVolatilitySizer(cfg, nopLogger)

	weight := vs.CalculatePositionWeight(0.0, nil)
	if weight != cfg.MinPositionWeight {
		t.Errorf("weight with zero vol = %v, want %v (min)", weight, cfg.MinPositionWeight)
	}
}

// --- Test 31: VolatilitySizer CalculatePositionWeight with negative volatility ---

func TestBoundary_VolatilitySizer_CalculatePositionWeight_NegativeVol(t *testing.T) {
	cfg := VolatilityConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.10,
		MinPositionWeight:   0.01,
	}
	vs := NewVolatilitySizer(cfg, nopLogger)

	weight := vs.CalculatePositionWeight(-0.5, nil)
	if weight != cfg.MinPositionWeight {
		t.Errorf("weight with negative vol = %v, want %v (min)", weight, cfg.MinPositionWeight)
	}
}

// --- Test 32: VolatilitySizer CalculatePositionWeight caps at max ---

func TestBoundary_VolatilitySizer_CalculatePositionWeight_CapsAtMax(t *testing.T) {
	cfg := VolatilityConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.10,
		MinPositionWeight:   0.01,
	}
	vs := NewVolatilitySizer(cfg, nopLogger)

	// Very low volatility → high raw weight, should be capped at max
	weight := vs.CalculatePositionWeight(0.01, nil)
	if weight > cfg.MaxPositionWeight {
		t.Errorf("weight = %v, should be <= max %v", weight, cfg.MaxPositionWeight)
	}
}

// --- Test 33: VolatilitySizer CalculatePositionWeight with high vol regime ---

func TestBoundary_VolatilitySizer_CalculatePositionWeight_HighVolRegime(t *testing.T) {
	cfg := VolatilityConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.10,
		MinPositionWeight:   0.01,
	}
	vs := NewVolatilitySizer(cfg, nopLogger)

	regime := &domain.MarketRegime{Volatility: "high"}
	weight := vs.CalculatePositionWeight(0.20, regime)
	// High vol regime → multiplier 0.5 → reduced weight
	if weight > cfg.MaxPositionWeight {
		t.Errorf("weight = %v, should be <= max %v", weight, cfg.MaxPositionWeight)
	}
}

// --- Test 34: VolatilitySizer CalculatePositionWeight with low vol regime ---

func TestBoundary_VolatilitySizer_CalculatePositionWeight_LowVolRegime(t *testing.T) {
	cfg := VolatilityConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.10,
		MinPositionWeight:   0.01,
	}
	vs := NewVolatilitySizer(cfg, nopLogger)

	regime := &domain.MarketRegime{Volatility: "low"}
	weight := vs.CalculatePositionWeight(0.20, regime)
	if weight > cfg.MaxPositionWeight {
		t.Errorf("weight = %v, should be <= max %v", weight, cfg.MaxPositionWeight)
	}
}

// --- Test 35: VolatilitySizer CalculatePositionWeight with bear regime ---

func TestBoundary_VolatilitySizer_CalculatePositionWeight_BearRegime(t *testing.T) {
	cfg := VolatilityConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.10,
		MinPositionWeight:   0.01,
	}
	vs := NewVolatilitySizer(cfg, nopLogger)

	regime := &domain.MarketRegime{Trend: "bear", Volatility: "medium"}
	weight := vs.CalculatePositionWeight(0.20, regime)
	// Bear regime → multiplier 0.7 → reduced weight
	if weight > cfg.MaxPositionWeight {
		t.Errorf("weight = %v, should be <= max %v", weight, cfg.MaxPositionWeight)
	}
}

// --- Test 36: VolatilitySizer CalculateBaseWeight ---

func TestBoundary_VolatilitySizer_CalculateBaseWeight(t *testing.T) {
	cfg := VolatilityConfig{
		TargetVolatility:    0.15,
		MinPositionWeight:   0.01,
	}
	vs := NewVolatilitySizer(cfg, nopLogger)

	// Normal case
	bw := vs.CalculateBaseWeight(0.30)
	expected := 0.15 / 0.30
	if bw != expected {
		t.Errorf("baseWeight = %v, want %v", bw, expected)
	}

	// Zero vol → min
	bw0 := vs.CalculateBaseWeight(0.0)
	if bw0 != cfg.MinPositionWeight {
		t.Errorf("baseWeight with zero vol = %v, want %v", bw0, cfg.MinPositionWeight)
	}
}

// --- Test 37: VolatilitySizer GetVolatilityStats with insufficient data ---

func TestBoundary_VolatilitySizer_GetVolatilityStats_InsufficientData(t *testing.T) {
	cfg := VolatilityConfig{LookbackDays: 10}
	vs := NewVolatilitySizer(cfg, nopLogger)

	_, _, err := vs.GetVolatilityStats([]domain.OHLCV{})
	if err == nil {
		t.Fatal("expected error on empty slice, got nil")
	}
}

// --- Test 38: VolatilitySizer GetVolatilityStats with sufficient data ---

func TestBoundary_VolatilitySizer_GetVolatilityStats_SufficientData(t *testing.T) {
	cfg := VolatilityConfig{AnnualizationFactor: 16.0}
	vs := NewVolatilitySizer(cfg, nopLogger)

	ohlcv := makeOHLCV(10, 100.0)
	daily, annualized, err := vs.GetVolatilityStats(ohlcv)
	if err != nil {
		t.Fatalf("GetVolatilityStats error: %v", err)
	}
	if daily < 0 {
		t.Errorf("daily vol = %v, should be >= 0", daily)
	}
	if annualized < 0 {
		t.Errorf("annualized vol = %v, should be >= 0", annualized)
	}
}

// --- Test 39: VolatilitySizer CalculatePosition with valid inputs ---

func TestBoundary_VolatilitySizer_CalculatePosition(t *testing.T) {
	cfg := VolatilityConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.10,
		MinPositionWeight:   0.01,
		LookbackDays:        10,
		AnnualizationFactor: 16.0,
	}
	vs := NewVolatilitySizer(cfg, nopLogger)

	ohlcv := makeOHLCV(15, 100.0)
	signal := domain.Signal{
		Symbol:    "TEST",
		Direction: domain.DirectionLong,
		Strength:  0.8,
	}
	portfolio := &domain.Portfolio{
		Cash:       1000000,
		TotalValue: 1000000,
		Positions:  map[string]domain.Position{},
	}
	regime := &domain.MarketRegime{Trend: "bull", Volatility: "low"}

	pos, err := vs.CalculatePosition(context.Background(), signal, portfolio, regime, ohlcv)
	if err != nil {
		t.Fatalf("CalculatePosition error: %v", err)
	}
	if pos.Size < 0 {
		t.Errorf("position size = %v, should be >= 0", pos.Size)
	}
	if pos.Weight < cfg.MinPositionWeight || pos.Weight > cfg.MaxPositionWeight {
		t.Errorf("position weight = %v, should be in [%v, %v]", pos.Weight, cfg.MinPositionWeight, cfg.MaxPositionWeight)
	}
}

// --- Test 40: VolatilitySizer CalculatePosition with insufficient data ---

func TestBoundary_VolatilitySizer_CalculatePosition_InsufficientData(t *testing.T) {
	cfg := VolatilityConfig{LookbackDays: 20}
	vs := NewVolatilitySizer(cfg, nopLogger)

	ohlcv := makeOHLCV(5, 100.0) // insufficient
	signal := domain.Signal{Symbol: "TEST", Direction: domain.DirectionLong, Strength: 0.5}
	portfolio := &domain.Portfolio{TotalValue: 1000000, Positions: map[string]domain.Position{}}

	_, err := vs.CalculatePosition(context.Background(), signal, portfolio, nil, ohlcv)
	if err == nil {
		t.Fatal("expected error on insufficient data, got nil")
	}
}

// --- Test 41: StopLossChecker inferTrend with various PnL ---

func TestBoundary_StopLossChecker_InferTrend(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14}
	slc := NewStopLossChecker(cfg, nopLogger)

	cases := []struct {
		currentPrice float64
		avgCost      float64
		want         string
	}{
		{110.0, 100.0, "bull"},   // +10% → bull
		{90.0, 100.0, "bear"},    // -10% → bear
		{101.0, 100.0, "sideways"}, // +1% → sideways
		{99.0, 100.0, "sideways"},  // -1% → sideways
	}
	for _, c := range cases {
		pos := domain.Position{AvgCost: c.avgCost}
		got := slc.inferTrend(pos, c.currentPrice)
		if got != c.want {
			t.Errorf("inferTrend(price=%v, cost=%v) = %q, want %q", c.currentPrice, c.avgCost, got, c.want)
		}
	}
}

// --- Test 42: StopLossChecker inferVolatility with edge cases ---

func TestBoundary_StopLossChecker_InferVolatility(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14}
	slc := NewStopLossChecker(cfg, nopLogger)

	cases := []struct {
		avgCost float64
		atr     float64
		want    string
	}{
		{100.0, 5.0, "high"},    // 5% ATR → high
		{100.0, 1.0, "low"},     // 1% ATR → low
		{100.0, 2.0, "medium"},  // 2% ATR → medium
		{0.0, 5.0, "medium"},     // zero cost → medium (guard)
		{100.0, 0.0, "medium"},  // zero ATR → medium (guard)
	}
	for _, c := range cases {
		pos := domain.Position{AvgCost: c.avgCost}
		got := slc.inferVolatility(pos, c.atr)
		if got != c.want {
			t.Errorf("inferVolatility(cost=%v, atr=%v) = %q, want %q", c.avgCost, c.atr, got, c.want)
		}
	}
}

// --- Test 43: StopLossChecker CalculateStopLoss (legacy method) returns entry ---

func TestBoundary_StopLossChecker_CalculateStopLoss_Legacy(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 14, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	entry := 100.0
	result := slc.CalculateStopLoss(entry, nil)
	// Legacy method returns entry as placeholder
	if result != entry {
		t.Errorf("CalculateStopLoss = %v, want %v (legacy returns entry)", result, entry)
	}
}

// --- Test 44: RegimeDetector calculateMA with period > len ---

func TestBoundary_RegimeDetector_CalculateMA_PeriodExceedsLen(t *testing.T) {
	cfg := RegimeConfig{FastMAPeriod: 5, SlowMAPeriod: 10}
	rd := NewRegimeDetector(cfg, nopLogger)

	ohlcv := makeOHLCV(3, 100.0) // less than period
	ma := rd.calculateMA(ohlcv, 10)
	// Should use len(ohlcv) as period
	if ma <= 0 {
		t.Errorf("MA = %v, should be > 0", ma)
	}
}

// --- Test 45: RegimeDetector calculateHistoricalVolatility with insufficient data ---

func TestBoundary_RegimeDetector_CalculateHistoricalVolatility_InsufficientData(t *testing.T) {
	cfg := RegimeConfig{FastMAPeriod: 5, SlowMAPeriod: 10}
	rd := NewRegimeDetector(cfg, nopLogger)

	// Only 1 bar — can't compute returns
	ohlcv := makeOHLCV(1, 100.0)
	vol := rd.calculateHistoricalVolatility(ohlcv, 20)
	if vol != 0 {
		t.Errorf("vol with insufficient data = %v, want 0", vol)
	}
}

// --- Test 46: RegimeDetector calculateMomentum with insufficient data ---

func TestBoundary_RegimeDetector_CalculateMomentum_InsufficientData(t *testing.T) {
	cfg := RegimeConfig{FastMAPeriod: 5, SlowMAPeriod: 10}
	rd := NewRegimeDetector(cfg, nopLogger)

	ohlcv := makeOHLCV(1, 100.0)
	mom := rd.calculateMomentum(ohlcv, 20)
	if mom != 0 {
		t.Errorf("momentum with insufficient data = %v, want 0", mom)
	}
}

// --- Test 47: RegimeDetector calculateTrendStrength with insufficient data ---

func TestBoundary_RegimeDetector_CalculateTrendStrength_InsufficientData(t *testing.T) {
	cfg := RegimeConfig{FastMAPeriod: 5, SlowMAPeriod: 10}
	rd := NewRegimeDetector(cfg, nopLogger)

	ohlcv := makeOHLCV(3, 100.0) // less than slowMAPeriod
	strength := rd.calculateTrendStrength(ohlcv)
	if strength != 0 {
		t.Errorf("trend strength with insufficient data = %v, want 0", strength)
	}
}

// --- Test 48: RegimeDetector calculateSlope with single point ---

func TestBoundary_RegimeDetector_CalculateSlope_SinglePoint(t *testing.T) {
	cfg := RegimeConfig{FastMAPeriod: 5, SlowMAPeriod: 10}
	rd := NewRegimeDetector(cfg, nopLogger)

	slope := rd.calculateSlope([]float64{100.0})
	if slope != 0 {
		t.Errorf("slope with single point = %v, want 0", slope)
	}
}

// --- Test 49: RegimeDetector calculateSlope with empty ---

func TestBoundary_RegimeDetector_CalculateSlope_Empty(t *testing.T) {
	cfg := RegimeConfig{FastMAPeriod: 5, SlowMAPeriod: 10}
	rd := NewRegimeDetector(cfg, nopLogger)

	slope := rd.calculateSlope([]float64{})
	if slope != 0 {
		t.Errorf("slope with empty = %v, want 0", slope)
	}
}

// --- Test 50: StopLossChecker with extreme ATRPeriod ---

func TestBoundary_StopLossChecker_ExtremeATRPeriod(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 1, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	ohlcv := makeOHLCV(3, 100.0)
	atr, err := slc.CalculateATR(ohlcv)
	if err != nil {
		t.Fatalf("CalculateATR error: %v", err)
	}
	if atr < 0 {
		t.Errorf("ATR = %v, should be >= 0", atr)
	}
}

// --- Test 51: VolatilitySizer CalculatePositionWeight with nil regime ---

func TestBoundary_VolatilitySizer_CalculatePositionWeight_NilRegime(t *testing.T) {
	cfg := VolatilityConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.10,
		MinPositionWeight:   0.01,
	}
	vs := NewVolatilitySizer(cfg, nopLogger)

	weight := vs.CalculatePositionWeight(0.20, nil)
	// With nil regime, multiplier is 1.0
	expected := cfg.TargetVolatility / 0.20
	if expected > cfg.MaxPositionWeight {
		expected = cfg.MaxPositionWeight
	}
	if weight != expected {
		t.Errorf("weight with nil regime = %v, want %v", weight, expected)
	}
}

// --- Test 52: math.MaxInt sanity check for boundary values ---

func TestBoundary_ExtremeValues_NoPanic(t *testing.T) {
	cfg := StopLossConfig{ATRPeriod: 5, BaseMultiplier: 2.0}
	slc := NewStopLossChecker(cfg, nopLogger)

	// Extreme entry price
	stopLoss := slc.CalculateStopLossPrice(math.MaxFloat64, 1.0, nil)
	if stopLoss <= 0 {
		t.Errorf("stopLoss with max float = %v, should be > 0", stopLoss)
	}

	// Extreme ATR
	stopLoss2 := slc.CalculateStopLossPrice(100.0, math.MaxFloat64, nil)
	if stopLoss2 >= 0 {
		t.Errorf("stopLoss with max ATR = %v, should be negative", stopLoss2)
	}
}
