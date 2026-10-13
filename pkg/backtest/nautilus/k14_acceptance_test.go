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
	"runtime"
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
	// K13-R1-R1 D · anchor the fixture to this source file (not the process cwd)
	// and fail (never skip) when it is missing. A gate that silently skips when it
	// cannot find its input is no gate at all — CI would report green while the
	// Go/Python fee parity is never actually checked.
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must resolve the test file path")
	path := filepath.Join(
		filepath.Dir(thisFile),
		"..", "..", "..", "nt-sidecar", "tests", "fixtures", "ashare_fees_parity.json",
	)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "the A-share parity fixture must be present and readable at %s", path)

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
