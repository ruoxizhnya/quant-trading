// K14 · acceptance legs for the weight-replay work that live on the Go side.
//
//  1. Kernel routing fails closed for an *unregistered* kernel — no silent
//     fallback to go/nautilus (the brief's 反证：未注册内核零回退).
//  2. The A-share fee fixture (consumed by the Python parity test) still matches
//     the Go implementation, so the two sides cannot drift apart silently.

package nautilus

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/ruoxizhnya/quant-trading/pkg/portfolio"
)

// nautilusStubRunner is a no-op EngineRunner for registry tests.
type nautilusStubRunner struct{}

func (nautilusStubRunner) RunBacktest(context.Context, contracts.BacktestRequest) (*contracts.BacktestResponse, error) {
	return &contracts.BacktestResponse{Status: contracts.StatusCompleted}, nil
}

func TestKernelRouting_UnknownKernelFailsClosedNoFallback(t *testing.T) {
	reg := contracts.NewEngineRegistry()
	reg.Register(KernelName, nautilusStubRunner{}) // only nautilus is wired

	// An unregistered kernel must error — never fall back to a registered one.
	_, err := reg.Resolve("definitely-not-a-kernel")
	require.Error(t, err, "an unregistered kernel must not resolve")
	assert.ErrorIs(t, err, contracts.ErrKernelNotRegistered)

	// The registered kernel still resolves (the guard is not over-broad).
	resolved, err := reg.Resolve(KernelName)
	require.NoError(t, err)
	assert.NotNil(t, resolved)
}

func TestAShareFeeParityFixtureMatchesGo(t *testing.T) {
	path := filepath.Join("..", "..", "..", "nt-sidecar", "tests", "fixtures", "ashare_fees_parity.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("parity fixture not present (run from repo root): %v", err)
	}

	type row struct {
		TradeValue  float64 `json:"trade_value"`
		IsSell      bool    `json:"is_sell"`
		Commission  float64 `json:"commission"`
		TransferFee float64 `json:"transfer_fee"`
		StampTax    float64 `json:"stamp_tax"`
		Total       float64 `json:"total"`
	}
	var rows []row
	require.NoError(t, json.Unmarshal(data, &rows))
	require.NotEmpty(t, rows, "the parity fixture must not be empty")

	f := fees.DefaultAShareFees()
	for _, r := range rows {
		fb := portfolio.ComputeFees(r.TradeValue, r.IsSell, f)
		assert.Equal(t, r.Commission, fb.Commission, "commission drift at value=%v sell=%v", r.TradeValue, r.IsSell)
		assert.Equal(t, r.TransferFee, fb.TransferFee, "transfer fee drift at value=%v", r.TradeValue)
		assert.Equal(t, r.StampTax, fb.StampTax, "stamp tax drift at value=%v sell=%v", r.TradeValue, r.IsSell)
		assert.Equal(t, r.Total, fb.Total(), "total drift at value=%v sell=%v", r.TradeValue, r.IsSell)
	}
}
