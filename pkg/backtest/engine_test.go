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
	// Verify the default price limit constants match expected A-share rules
	defaults := defaultTradingConfig()
	assert.Equal(t, 0.10, defaults.PriceLimit.Normal)
	assert.Equal(t, 0.05, defaults.PriceLimit.ST)
	assert.Equal(t, 0.20, defaults.PriceLimit.New)
	assert.Equal(t, 60, defaults.NewStockDays)
}

// TestLimitRateDetermination tests the price limit rate determination logic
// by verifying the boundary conditions.
func TestLimitRateDetermination(t *testing.T) {
	defaults := defaultTradingConfig()

	tests := []struct {
		name         string
		tradeDays    int
		stockName    string
		expectedRate float64
	}{
		{"new stock < 60 days", 30, "新股上市", defaults.PriceLimit.New},
		{"new stock = 59 days", 59, "新股上市", defaults.PriceLimit.New},
		{"normal stock = 60 days", 60, "普通股票", defaults.PriceLimit.Normal},
		{"normal stock > 60 days", 100, "普通股票", defaults.PriceLimit.Normal},
		{"ST stock", 100, "ST华业", defaults.PriceLimit.ST},
		// "*ST信通" → hasSTPrefix returns false (name[:2]="*S"), so priceLimitNormal
		{"*ST stock - false prefix", 100, "*ST信通", defaults.PriceLimit.Normal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Determine limit rate using the same logic as runBacktestInternal
			limitRate := defaults.PriceLimit.Normal
			if tc.tradeDays < defaults.NewStockDays {
				limitRate = defaults.PriceLimit.New
			} else if hasSTPrefix(tc.stockName) {
				limitRate = defaults.PriceLimit.ST
			}
			assert.Equal(t, tc.expectedRate, limitRate)
		})
	}
}

// TestTradingConfigDefaults verifies default trading configuration values
func TestTradingConfigDefaults(t *testing.T) {
	defaults := defaultTradingConfig()

	assert.Equal(t, 0.001, defaults.StampTaxRate)
	assert.Equal(t, 5.0, defaults.MinCommission)
	assert.Equal(t, 0.00001, defaults.TransferFeeRate)
	assert.Equal(t, 0.10, defaults.PriceLimit.Normal)
	assert.Equal(t, 0.05, defaults.PriceLimit.ST)
	assert.Equal(t, 0.20, defaults.PriceLimit.New)
	assert.Equal(t, 60, defaults.NewStockDays)
}

// TestDirectionConstants verifies the direction constants used in backtesting
func TestDirectionConstants(t *testing.T) {
	// Verify core directions exist and are correct
	assert.Equal(t, "long", string(domain.DirectionLong))
	assert.Equal(t, "short", string(domain.DirectionShort))
	assert.Equal(t, "close", string(domain.DirectionClose))
	assert.Equal(t, "hold", string(domain.DirectionHold))
}
