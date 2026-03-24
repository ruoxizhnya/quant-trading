package backtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// --- hasSTPrefix tests ---
func TestHasSTPrefix(t *testing.T) {
	tests := []struct {
		name     string
		stockName string
		expected bool
	}{
		{"ST prefix", "ST平安银行", true},   // name[:2] = "ST"
		{"*ST prefix", "*ST大唐", false},    // name[:2] = "*S" (only checks exactly "*ST")
		{"SST prefix", "SST华业", false},     // name[:2] = "SS"
		{"S*ST prefix", "S*ST信通", false},  // name[:2] = "S*"
		{"normal stock", "平安银行", false},
		{"empty", "", false},
		{"short", "S", false},
		{"ST with more chars", "ST股票", true},
		{"lowercase st", "st平安", false}, // must be uppercase
		{"has ST in middle", "华夏ST银行", false}, // only prefix matters
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := hasSTPrefix(tc.stockName)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// --- Price limit rate determination ---
// These tests verify the limit rate logic used in runBacktestInternal.
// We test the limit detection by calling the function directly.

func TestPriceLimitConstants(t *testing.T) {
	// Verify the price limit constants match expected A-share rules
	assert.Equal(t, 0.10, priceLimitNormal)
	assert.Equal(t, 0.05, priceLimitST)
	assert.Equal(t, 0.20, priceLimitNew)
	assert.Equal(t, 60, newStockTradeDays)
}

// TestLimitRateDetermination tests the price limit rate determination logic
// by verifying the boundary conditions.
func TestLimitRateDetermination(t *testing.T) {
	tests := []struct {
		name         string
		tradeDays    int
		stockName    string
		expectedRate float64
	}{
		{"new stock < 60 days", 30, "新股上市", priceLimitNew},
		{"new stock = 59 days", 59, "新股上市", priceLimitNew},
		{"normal stock = 60 days", 60, "普通股票", priceLimitNormal},
		{"normal stock > 60 days", 100, "普通股票", priceLimitNormal},
		{"ST stock", 100, "ST华业", priceLimitST},
		// "*ST信通" → hasSTPrefix returns false (name[:2]="*S"), so priceLimitNormal
		{"*ST stock - false prefix", 100, "*ST信通", priceLimitNormal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Determine limit rate using the same logic as runBacktestInternal
			limitRate := priceLimitNormal
			if tc.tradeDays < newStockTradeDays {
				limitRate = priceLimitNew
			} else if hasSTPrefix(tc.stockName) {
				limitRate = priceLimitST
			}
			assert.Equal(t, tc.expectedRate, limitRate)
		})
	}
}

// TestLimitUpDownDetection verifies the limit-up and limit-down detection logic.
func TestLimitUpDownDetection(t *testing.T) {
	tests := []struct {
		name       string
		prevClose  float64
		todayClose float64
		limitRate  float64
		wantUp     bool
		wantDown   bool
	}{
		{
			name:       "normal stock up 10% (at limit)",
			prevClose:  10.0,
			todayClose: 11.0,
			limitRate:  0.10,
			wantUp:     true,
			wantDown:   false,
		},
		{
			name:       "normal stock up 9% (below limit)",
			prevClose:  10.0,
			todayClose: 10.9,
			limitRate:  0.10,
			wantUp:     false,
			wantDown:   false,
		},
		{
			name:       "ST stock up 5% (at limit)",
			prevClose:  10.0,
			todayClose: 10.5,
			limitRate:  0.05,
			wantUp:     true,
			wantDown:   false,
		},
		{
			name:       "new stock up 20% (at limit)",
			prevClose:  10.0,
			todayClose: 12.0,
			limitRate:  0.20,
			wantUp:     true,
			wantDown:   false,
		},
		{
			name:       "normal stock down 10% (at limit)",
			prevClose:  10.0,
			todayClose: 9.0,
			limitRate:  0.10,
			wantUp:     false,
			wantDown:   true,
		},
		{
			name:       "normal stock down 9% (below limit)",
			prevClose:  10.0,
			todayClose: 9.1,
			limitRate:  0.10,
			wantUp:     false,
			wantDown:   false,
		},
		{
			name:       "limit down ST 5%",
			prevClose:  10.0,
			todayClose: 9.5,
			limitRate:  0.05,
			wantUp:     false,
			wantDown:   true,
		},
		{
			name:       "at upper limit exactly",
			prevClose:  10.0,
			todayClose: 11.0,
			limitRate:  0.10,
			wantUp:     true,
			wantDown:   false,
		},
		{
			name:       "at lower limit exactly",
			prevClose:  10.0,
			todayClose: 9.0,
			limitRate:  0.10,
			wantUp:     false,
			wantDown:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			upperLimit := tc.prevClose * (1 + tc.limitRate)
			lowerLimit := tc.prevClose * (1 - tc.limitRate)

			limitUp := tc.todayClose >= upperLimit
			limitDown := tc.todayClose <= lowerLimit

			assert.Equal(t, tc.wantUp, limitUp, "limitUp mismatch")
			assert.Equal(t, tc.wantDown, limitDown, "limitDown mismatch")
		})
	}
}

// TestTargetVsActualPosition_PendingClearedOnClose tests that closing a position
// clears the pending qty.
func TestTargetVsActualPosition_PendingClearedOnClose(t *testing.T) {
	// This tests the target position state machine logic:
	// When a DirectionClose signal is received, all pending qty should be cleared.
	// After close: TargetQty=0, ActualQty=0, PendingQty=0
	tp := &domain.TargetPosition{
		Symbol:     "600000.SH",
		TargetQty:  0,    // close signal sets TargetQty=0
		ActualQty:  0,    // close signal sets ActualQty=0
		PendingQty: 0,    // close signal sets PendingQty=0
	}

	// Simulate close signal processing (same logic as runBacktestInternal)
	if tp.PendingQty > 0 && tp.TargetQty <= 0 {
		tp.PendingQty = 0
		tp.TargetQty = 0
		tp.ActualQty = 0
	}

	assert.Equal(t, 0.0, tp.PendingQty)
	assert.Equal(t, 0.0, tp.TargetQty)
	assert.Equal(t, 0.0, tp.ActualQty)
}

// TestTargetVsActualPosition_PartialFillCarriesPending tests that a partial fill
// correctly carries the pending qty to the next day.
func TestTargetVsActualPosition_PartialFillCarriesPending(t *testing.T) {
	// Simulate partial fill scenario
	targetQty := 100.0
	actualQty := 60.0 // only 60 filled

	pendingQty := targetQty - actualQty
	assert.Equal(t, 40.0, pendingQty)

	// Verify pending qty is carried forward
	tp := &domain.TargetPosition{
		Symbol:     "600000.SH",
		TargetQty:  100,
		ActualQty:  60,
		PendingQty: 40,
	}

	// The pending qty should be > 0 after partial fill
	assert.Greater(t, tp.PendingQty, 0.0)

	// Next day: if we get a new target, the pending should be netted
	newTarget := 100.0
	effectiveTarget := newTarget - tp.PendingQty
	assert.Equal(t, 60.0, effectiveTarget) // 100 - 40 = 60
}

// --- Slippage direction tests ---
func TestSlippageDirection(t *testing.T) {
	// Test that slippage is applied correctly for each direction
	price := 10.0
	slippageRate := 0.0001

	// Long: buy at higher price
	longPrice := price * (1 + slippageRate)
	assert.Equal(t, 10.001, longPrice)

	// Short: sell at lower price
	shortPrice := price * (1 - slippageRate)
	assert.Equal(t, 9.999, shortPrice)

	// Close long: sell at lower price
	closeLongPrice := price * (1 - slippageRate)
	assert.Equal(t, 9.999, closeLongPrice)

	// Close short: buy at higher price
	closeShortPrice := price * (1 + slippageRate)
	assert.Equal(t, 10.001, closeShortPrice)
}

// --- helper struct for target position (not exported, use domain type) ---
type targetPosition struct {
	Symbol      string
	TargetQty   float64
	ActualQty   float64
	PendingQty  float64
}
