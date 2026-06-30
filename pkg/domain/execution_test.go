package domain

import (
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/stretchr/testify/assert"
)

// TestDefaultExecutionConfig_UsesCanonicalFees (S7-P1-4, D1)
// DefaultExecutionConfig previously hardcoded CommissionRate = 0.00025,
// which diverged from the canonical fees.DefaultCommissionRate (0.0003).
// The 0.00025 rate is neither the regulatory A-share commission ceiling
// (0.0003) nor any value defined in pkg/fees — it was a stale literal
// that caused backtest-vs-live P&L drift.
//
// After the fix, DefaultExecutionConfig must source its fee fields from
// pkg/fees so there is a single source of truth.
func TestDefaultExecutionConfig_UsesCanonicalFees(t *testing.T) {
	cfg := DefaultExecutionConfig()
	assert.Equal(t, fees.DefaultCommissionRate, cfg.CommissionRate,
		"DefaultExecutionConfig.CommissionRate must equal fees.DefaultCommissionRate (single source of truth)")
	assert.Equal(t, fees.DefaultMinCommission, cfg.MinCommission,
		"DefaultExecutionConfig.MinCommission must equal fees.DefaultMinCommission")
}
