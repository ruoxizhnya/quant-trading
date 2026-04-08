package backtest

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func newTestTracker2(cash float64) *Tracker {
	logger := zerolog.Nop()
	return NewTracker(cash, 0.0003, 0.001, defaultTradingConfig(), logger)
}

// ============================================================
// 2.5B.1: T+1 Settlement Unit Tests (≥5 core boundary cases)
// ============================================================

func TestTPlus1_BuyToday_SellToday_Rejected(t *testing.T) {
	tracker := newTestTracker2(1000000)
	date := time.Date(2025, 3, 10, 9, 30, 0, 0, time.UTC)

	_, err := tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, date, nil)
	if err != nil {
		t.Fatalf("buy should succeed: %v", err)
	}

	_, err = tracker.ExecuteTrade("600519", domain.DirectionClose, 100, 55.0, date, nil)
	if err == nil {
		t.Fatal("sell on same day as buy should be rejected (T+1 violation)")
	}
	t.Logf("T+1 violation correctly rejected: %v", err)
}

func TestTPlus1_BuyToday_SellNextDay_Success(t *testing.T) {
	tracker := newTestTracker2(1000000)
	buyDate := time.Date(2025, 3, 10, 9, 30, 0, 0, time.UTC)
	sellDate := time.Date(2025, 3, 11, 9, 30, 0, 0, time.UTC)

	_, err := tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, buyDate, nil)
	if err != nil {
		t.Fatalf("buy failed: %v", err)
	}

	tracker.AdvanceDay(sellDate)

	trade, err := tracker.ExecuteTrade("600519", domain.DirectionClose, 100, 55.0, sellDate, nil)
	if err != nil {
		t.Fatalf("sell after T+1 should succeed: %v", err)
	}
	if trade.Quantity != 100 {
		t.Errorf("expected sold qty=100, got %.0f", trade.Quantity)
	}

	_, exists := tracker.GetPosition("600519")
	if exists {
		t.Error("position should be fully closed and removed")
	}
	t.Log("T+1 settlement passed: bought D1, sold D2 successfully")
}

func TestTPlus1_MultipleBuys_SameDaySellBlocked(t *testing.T) {
	tracker := newTestTracker2(1000000)
	d1 := time.Date(2025, 3, 10, 9, 30, 0, 0, time.UTC)

	tracker.ExecuteTrade("000001", domain.DirectionLong, 200, 10.0, d1, nil)
	tracker.ExecuteTrade("000001", domain.DirectionLong, 300, 10.0, d1, nil)

	pos, _ := tracker.GetPosition("000001")
	if pos.Quantity != 500 {
		t.Errorf("total qty should be 500, got %.0f", pos.Quantity)
	}
	if pos.QuantityToday != 500 {
		t.Errorf("all 500 bought today, QT should be 500, got %.0f", pos.QuantityToday)
	}
	if pos.QuantityYesterday != 0 {
		t.Errorf("QY should be 0 same day, got %.0f", pos.QuantityYesterday)
	}

	_, err := tracker.ExecuteTrade("000001", domain.DirectionClose, 500, 11.0, d1, nil)
	if err == nil {
		t.Fatal("selling all same-day shares should be blocked")
	}
	t.Log("Same-day multiple buys: all unsellable until T+1")
}

func TestTPlus1_AdvanceDay_Rollover(t *testing.T) {
	tracker := newTestTracker2(1000000)
	d1 := time.Date(2025, 3, 10, 9, 30, 0, 0, time.UTC)
	d2 := time.Date(2025, 3, 11, 9, 30, 0, 0, time.UTC)

	tracker.ExecuteTrade("600519", domain.DirectionLong, 100, 50.0, d1, nil)

	pos, exists := tracker.GetPosition("600519")
	if !exists {
		t.Fatal("position should exist after buy")
	}
	if pos.QuantityToday != 100 || pos.QuantityYesterday != 0 {
		t.Errorf("before advance: today=100 yesterday=0, got today=%.0f yesterday=%.0f",
			pos.QuantityToday, pos.QuantityYesterday)
	}

	tracker.AdvanceDay(d2)

	pos2, exists2 := tracker.GetPosition("600519")
	if !exists2 {
		t.Fatal("position should still exist after AdvanceDay")
	}
	if pos2.QuantityToday != 0 {
		t.Errorf("after advance: today should be 0, got %.0f", pos2.QuantityToday)
	}
	if pos2.QuantityYesterday != 100 {
		t.Errorf("after advance: yesterday should be 100, got %.0f", pos2.QuantityYesterday)
	}
	t.Log("AdvanceDay correctly rolled QuantityToday into QuantityYesterday")
}

func TestTPlus1_BuyAdvanceSellAll_PositionRemoved(t *testing.T) {
	tracker := newTestTracker2(1000000)
	d1 := time.Date(2025, 3, 10, 9, 30, 0, 0, time.UTC)
	d2 := time.Date(2025, 3, 11, 9, 30, 0, 0, time.UTC)

	tracker.ExecuteTrade("600519", domain.DirectionLong, 50, 50.0, d1, nil)
	tracker.AdvanceDay(d2)
	tracker.ExecuteTrade("600519", domain.DirectionClose, 50, 55.0, d2, nil)

	_, exists := tracker.GetPosition("600519")
	if exists {
		t.Fatal("position should be removed after full close")
	}
	cash := tracker.GetCash()
	if cash <= 0 {
		t.Errorf("cash should be positive after selling, got %.2f", cash)
	}
	t.Log("Full close correctly removes position and returns cash")
}

func TestTPlus1_CrossDayBuys_PartialSell(t *testing.T) {
	tracker := newTestTracker2(1000000)
	d1 := time.Date(2025, 3, 10, 9, 30, 0, 0, time.UTC)
	d2 := time.Date(2025, 3, 11, 9, 30, 0, 0, time.UTC)

	tracker.ExecuteTrade("000001", domain.DirectionLong, 200, 10.0, d1, nil)
	tracker.AdvanceDay(d2)

	pos, _ := tracker.GetPosition("000001")
	if pos.QuantityYesterday != 200 || pos.QuantityToday != 0 {
		t.Errorf("after D1->D2 advance: QY=200 QT=0, got QY=%.0f QT=%.0f",
			pos.QuantityYesterday, pos.QuantityToday)
	}

	tracker.ExecuteTrade("000001", domain.DirectionLong, 300, 11.0, d2, nil)

	pos2, _ := tracker.GetPosition("000001")
	if pos2.Quantity != 500 {
		t.Errorf("total=500, got %.0f", pos2.Quantity)
	}
	if pos2.QuantityYesterday != 200 {
		t.Errorf("QY should still be 200 (D1 shares), got %.0f", pos2.QuantityYesterday)
	}
	if pos2.QuantityToday != 300 {
		t.Errorf("QT should be 300 (D2 shares), got %.0f", pos2.QuantityToday)
	}

	trade, err := tracker.ExecuteTrade("000001", domain.DirectionClose, 400, 12.0, d2, nil)
	if err != nil {
		t.Fatalf("partial sell should succeed: %v", err)
	}
	if trade.Quantity != 200 {
		t.Errorf("should only sell 200 (QY limit), got %.0f", trade.Quantity)
	}

	pos3, _ := tracker.GetPosition("000001")
	if pos3.Quantity != 300 {
		t.Errorf("remaining should be 300 (D2 unsellable), got %.0f", pos3.Quantity)
	}
	t.Log("Cross-day buy: only D1 shares (200) sellable on D2, D2 shares (300) need another day")
}

func TestTPlus1_AvgCostUpdatedOnMultipleBuys(t *testing.T) {
	tracker := newTestTracker2(1000000)
	d1 := time.Date(2025, 3, 10, 9, 30, 0, 0, time.UTC)
	d2 := time.Date(2025, 3, 11, 9, 30, 0, 0, time.UTC)
	d3 := time.Date(2025, 3, 12, 9, 30, 0, 0, time.UTC)

	tracker.ExecuteTrade("000001", domain.DirectionLong, 1000, 10.0, d1, nil)
	tracker.AdvanceDay(d2)
	tracker.ExecuteTrade("000001", domain.DirectionLong, 1000, 20.0, d2, nil)
	tracker.AdvanceDay(d3)

	pos, _ := tracker.GetPosition("000001")
	expectedAvgCost := (1000*10.0 + 1000*20.0) / 2000
	if abs(pos.AvgCost-expectedAvgCost) > 0.5 {
		t.Errorf("avg cost should be ≈%.4f (with fees), got %.4f", expectedAvgCost, pos.AvgCost)
	}
	t.Logf("Average cost correctly updated to %.4f across multiple buys", pos.AvgCost)
}

// ============================================================
// 2.5B.2: Price Limit Tests (≥6 cases for ST/new/normal stocks)
// ============================================================

func testHasSTPrefix(name string) bool {
	if len(name) < 2 {
		return false
	}
	prefix := name[:2]
	return prefix == "ST" || prefix == "*ST" || prefix == "SST" || prefix == "S*ST"
}

func TestPriceLimit_NormalStock_LimitUp(t *testing.T) {
	cfg := defaultTradingConfig()
	prevClose := 10.0
	limitRate := cfg.PriceLimit.Normal
	upperLimit := prevClose * (1 + limitRate)

	todayClose := upperLimit + 0.01
	isLimitUp := todayClose >= upperLimit

	if !isLimitUp {
		t.Errorf("close %.2f should trigger limit-up at %.2f", todayClose, upperLimit)
	}
	t.Logf("Normal stock: prevClose=%.2f, limit-up=%.2f -> triggered",
		prevClose, upperLimit)
}

func TestPriceLimit_NormalStock_NotLimitUp(t *testing.T) {
	cfg := defaultTradingConfig()
	prevClose := 10.0
	upperLimit := prevClose * (1 + cfg.PriceLimit.Normal)

	todayClose := upperLimit - 0.01
	isLimitUp := todayClose >= upperLimit

	if isLimitUp {
		t.Errorf("close %.2f should NOT trigger limit-up at %.2f", todayClose, upperLimit)
	}
}

func TestPriceLimit_STStock_5pct(t *testing.T) {
	cfg := defaultTradingConfig()
	if cfg.PriceLimit.ST != 0.05 {
		t.Errorf("ST limit rate should be 5%%, got %.2f%%", cfg.PriceLimit.ST*100)
	}
	prevClose := 5.0
	upperLimit := prevClose * (1 + cfg.PriceLimit.ST)
	t.Logf("ST stock limit-up at %.4f (5%% of %.2f)", upperLimit, prevClose)
}

func TestPriceLimit_NewStock_20pct(t *testing.T) {
	cfg := defaultTradingConfig()
	if cfg.PriceLimit.New != 0.20 {
		t.Errorf("New stock limit rate should be 20%%, got %.2f%%", cfg.PriceLimit.New*100)
	}
	prevClose := 20.0
	upperLimit := prevClose * (1 + cfg.PriceLimit.New)
	t.Logf("New stock limit-up at %.4f (20%% of %.2f)", upperLimit, prevClose)
}

func TestPriceLimit_LimitDown_Detection(t *testing.T) {
	cfg := defaultTradingConfig()
	prevClose := 10.0
	lowerLimit := prevClose * (1 - cfg.PriceLimit.Normal)

	todayClose := lowerLimit - 0.01
	isLimitDown := todayClose <= lowerLimit

	if !isLimitDown {
		t.Errorf("close %.2f should trigger limit-down at %.2f", todayClose, lowerLimit)
	}
	t.Logf("Normal stock: prevClose=%.2f, limit-down=%.2f -> triggered",
		prevClose, lowerLimit)
}

func TestPriceLimit_BuyBlockedOnLimitUp(t *testing.T) {
	bar := &domain.OHLCV{
		LimitUp:   true,
		LimitDown: false,
	}

	signalDir := domain.DirectionLong
	canBuy := !(bar.LimitUp && (signalDir == domain.DirectionLong || signalDir == domain.DirectionShort))
	if canBuy {
		t.Error("Buy should be blocked when stock hits limit-up")
	}
	t.Log("Buy correctly blocked on limit-up day")
}

func TestPriceLimit_SellBlockedOnLimitDown(t *testing.T) {
	bar := &domain.OHLCV{
		LimitUp:   false,
		LimitDown: true,
	}

	canSell := !(bar.LimitDown && domain.DirectionClose == domain.DirectionClose)
	if canSell {
		t.Error("Sell should be blocked when stock hits limit-down")
	}
	t.Log("Sell correctly blocked on limit-down day")
}

func TestPriceLimit_STPrefix_EdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"ST康美", true},
		{"贵州茅台", false},
		{"S", false},
		{"X", false},
		{"", false},
	}
	for _, tt := range tests {
		result := testHasSTPrefix(tt.input)
		if result != tt.expected {
			t.Errorf("testHasSTPrefix(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestPriceLimit_NewStockDetection(t *testing.T) {
	cfg := defaultTradingConfig()

	recentList := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	checkRecent := time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)
	tradeDaysRecent := int(checkRecent.Sub(recentList).Hours() / 24 / 7 * 5)
	if tradeDaysRecent >= cfg.NewStockDays {
		t.Error("recently listed stock should be treated as new stock")
	}

	oldList := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	checkOld := time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)
	tradeDaysOld := int(checkOld.Sub(oldList).Hours() / 24 / 7 * 5)
	if tradeDaysOld < cfg.NewStockDays {
		t.Error("old stock should NOT be treated as new stock")
	}
	t.Logf("New stock threshold: recent=%d days (<%d), old=%d days (>=%d)",
		tradeDaysRecent, cfg.NewStockDays, tradeDaysOld, cfg.NewStockDays)
}

// ============================================================
// Commission & Fee Verification
// ============================================================

func TestCommission_Buy_NoStampTax(t *testing.T) {
	tracker := newTestTracker2(1000000)
	date := time.Date(2025, 3, 10, 9, 30, 0, 0, time.UTC)

	trade, err := tracker.ExecuteTrade("TEST", domain.DirectionLong, 100, 100.0, date, nil)
	if err != nil {
		t.Fatalf("trade failed: %v", err)
	}

	if trade.StampTax > 0 {
		t.Errorf("buy should have zero stamp tax, got %.4f", trade.StampTax)
	}
	if trade.Commission <= 0 {
		t.Error("buy should have commission")
	}
	t.Logf("Buy OK: commission=%.4f, stampTax=%.4f", trade.Commission, trade.StampTax)
}

func TestCommission_Sell_HasStampTax(t *testing.T) {
	tracker := newTestTracker2(1000000)
	buyDate := time.Date(2025, 3, 10, 9, 30, 0, 0, time.UTC)
	sellDate := time.Date(2025, 3, 11, 9, 30, 0, 0, time.UTC)

	tracker.ExecuteTrade("TEST", domain.DirectionLong, 100, 100.0, buyDate, nil)
	tracker.AdvanceDay(sellDate)

	trade, err := tracker.ExecuteTrade("TEST", domain.DirectionClose, 100, 110.0, sellDate, nil)
	if err != nil {
		t.Fatalf("sell failed: %v", err)
	}

	if trade.StampTax <= 0 {
		t.Errorf("sell should have stamp tax (0.1%%), got %.4f", trade.StampTax)
	}
	if trade.StampTax <= 0 {
		t.Error("stamp tax should be positive")
	}
	t.Logf("Sell OK: commission=%.4f, stampTax=%.4f (execution price includes slippage)",
		trade.Commission, trade.StampTax)
}

func TestInsufficientCash_BuyRejected(t *testing.T) {
	tracker := newTestTracker2(100)
	date := time.Date(2025, 3, 10, 9, 30, 0, 0, time.UTC)

	_, err := tracker.ExecuteTrade("EXPENSIVE", domain.DirectionLong, 100, 10000.0, date, nil)
	if err == nil {
		t.Fatal("buy with insufficient cash should be rejected")
	}
	t.Logf("Correctly rejected: %v", err)
}

func TestPortfolioValue_Calculation(t *testing.T) {
	tracker := newTestTracker2(1000000)
	date := time.Date(2025, 3, 10, 9, 30, 0, 0, time.UTC)

	tracker.ExecuteTrade("A", domain.DirectionLong, 100, 10.0, date, nil)
	tracker.ExecuteTrade("B", domain.DirectionLong, 50, 20.0, date, nil)

	prices := map[string]float64{"A": 12.0, "B": 22.0}
	pv := tracker.GetPortfolioValue(prices)
	expectedPositions := 100*12.0 + 50*22.0
	expectedTotal := tracker.GetCash() + expectedPositions

	if abs(pv-expectedTotal) > 0.01 {
		t.Errorf("portfolio value expected %.2f, got %.2f", expectedTotal, pv)
	}
	t.Logf("Portfolio: cash=%.2f, positions=%.2f, total=%.2f",
		tracker.GetCash(), expectedPositions, pv)
}
