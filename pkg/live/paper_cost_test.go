package live_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/execution"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件在**外部测试包** live_test 里：它要 import pkg/backtest/execution 做
// 成本同构比对，而 pkg/backtest/execution 又 import pkg/live（live_bridge.go）。
// 若用内部包 live，就会构成 test→backtest/execution→live 的 import cycle。
// 外部测试包 live_test 是独立的包，因此不成环。

const (
	isoSymbol   = "000001.SZ"
	isoRefPrice = 10.0
	isoHigh     = 11.0
	isoLow      = 9.0
	isoADV      = 100000.0
	isoQty      = 10000.0
)

func isoQuote(volume float64) execution.Quote {
	return execution.Quote{
		Symbol: isoSymbol, Open: 10.0, High: isoHigh, Low: isoLow, Close: isoRefPrice,
		Volume: volume, Date: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
	}
}

// TestCostIsomorphism_Fixed 是成本同构的**核心正证据（fixed）**：同一笔
// (symbol, side, qty, config)、同一参考价与 ADV，paper 侧 ComputePaperCost
// 与回测 pkg/backtest/execution 算出的成交价/佣金逐位相等（1e-9）。
func TestCostIsomorphism_Fixed(t *testing.T) {
	cfg := domain.DefaultExecutionConfig() // SlippageModel == "fixed"
	require.Equal(t, "fixed", cfg.SlippageModel)

	back := execution.NewBacktestExecutionService(cfg)
	order := domain.Order{Symbol: isoSymbol, Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: isoQty}
	quote := isoQuote(isoADV)

	tr, err := back.ExecuteOrder(order, quote)
	require.NoError(t, err)

	pc, err := live.ComputePaperCost(cfg, order, quote.Close, quote.High, quote.Low, quote.Volume)
	require.NoError(t, err)

	t.Logf("fixed  回测: fill=%.10f commission=%.10f", tr.Price, tr.Commission)
	t.Logf("fixed  paper: fill=%.10f commission=%.10f", pc.FillPrice, pc.Commission)
	assert.InDelta(t, tr.Price, pc.FillPrice, 1e-9, "成交价必须同构")
	assert.InDelta(t, tr.Commission, pc.Commission, 1e-9, "佣金必须同构")

	// 独立解析锚点：两处（paper/回测）都走共享核，若核本身把 fixed 参数
	// 改错，两边会同步变、同构断言仍绿 —— 故再用「文档公式」独立钉死数值，
	// 让成本核被改坏时本用例必红。
	wantFill := isoRefPrice * (1 + fees.FixedSlippageRate)
	wantCommission := wantFill * isoQty * cfg.CommissionRate
	assert.InDelta(t, wantFill, pc.FillPrice, 1e-12, "fixed 买单成交价 == refPrice*(1+FixedSlippageRate)")
	assert.InDelta(t, wantCommission, pc.Commission, 1e-9, "fixed 佣金 == fill*qty*CommissionRate")
}

// TestCostIsomorphism_Impact 是成本同构的**核心正证据（impact）**。
func TestCostIsomorphism_Impact(t *testing.T) {
	cfg := domain.DefaultExecutionConfig()
	cfg.SlippageModel = "impact"
	cfg.ImpactSigma = 0.02        // 日波动率 2%
	cfg.ImpactLiquidityFactor = 0 // 文档默认 1.0

	back := execution.NewBacktestExecutionService(cfg)
	order := domain.Order{Symbol: isoSymbol, Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: isoQty}
	quote := isoQuote(isoADV)

	tr, err := back.ExecuteOrder(order, quote)
	require.NoError(t, err)

	pc, err := live.ComputePaperCost(cfg, order, quote.Close, quote.High, quote.Low, quote.Volume)
	require.NoError(t, err)

	t.Logf("impact 回测: fill=%.10f commission=%.10f", tr.Price, tr.Commission)
	t.Logf("impact paper: fill=%.10f commission=%.10f", pc.FillPrice, pc.Commission)
	assert.InDelta(t, tr.Price, pc.FillPrice, 1e-9, "成交价必须同构")
	assert.InDelta(t, tr.Commission, pc.Commission, 1e-9, "佣金必须同构")
	// 顺带确认 impact 确实生效：成交价高于参考价（买单吃冲击）。
	assert.Greater(t, pc.FillPrice, isoRefPrice)

	// 独立解析锚点：impact = sigma*sqrt(qty/adv)*liquidityFactor(=1.0)。
	// 成本核若把 Sigma 等参数改错，本断言必红（同构断言会因两边同步变而仍绿）。
	impact := cfg.ImpactSigma * math.Sqrt(isoQty/isoADV) * 1.0
	wantFill := isoRefPrice * (1 + impact)
	assert.InDelta(t, wantFill, pc.FillPrice, 1e-12, "impact 买单成交价 == refPrice*(1+sigma*sqrt(qty/adv))")
}

// TestMockTrader_FillPriceIsSharedCore 证明**生产路径**（MockTrader）真正
// 走的是共享成本核：配置 SlippageModel="impact" + 当根 bar 后，它回填到
// OrderResult.FillPrice 的成交价与回测对同一输入给出的成交价逐位相等。
func TestMockTrader_FillPriceIsSharedCore(t *testing.T) {
	cfg := domain.DefaultExecutionConfig()
	cfg.SlippageModel = "impact"
	cfg.ImpactSigma = 0.02

	mt := live.NewMockTrader(live.MockTraderConfig{
		InitialCash:           1e8,
		CommissionRate:        cfg.CommissionRate,
		MinCommission:         cfg.MinCommission,
		SlippageModel:         "impact",
		ImpactSigma:           0.02,
		ImpactLiquidityFactor: 0,
	}, zerolog.Nop())

	bar := marketdata.Quote{
		Symbol: isoSymbol, Open: 10, High: isoHigh, Low: isoLow, Close: isoRefPrice,
		Volume: int64(isoADV), Timestamp: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	mt.SetCurrentBarProvider(func(symbol string) (marketdata.Quote, bool) {
		if symbol == isoSymbol {
			return bar, true
		}
		return marketdata.Quote{}, false
	})

	res, err := mt.SubmitOrder(context.Background(), isoSymbol, domain.DirectionLong, domain.OrderTypeMarket, isoQty, 0)
	require.NoError(t, err)

	// 回测侧同一输入。
	back := execution.NewBacktestExecutionService(cfg)
	tr, err := back.ExecuteOrder(
		domain.Order{Symbol: isoSymbol, Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: isoQty},
		execution.Quote{Symbol: isoSymbol, Open: 10, High: isoHigh, Low: isoLow, Close: isoRefPrice, Volume: isoADV, Date: bar.Timestamp},
	)
	require.NoError(t, err)

	t.Logf("MockTrader.FillPrice=%.10f  回测.Price=%.10f", res.FillPrice, tr.Price)
	assert.InDelta(t, tr.Price, res.FillPrice, 1e-9,
		"MockTrader 生产路径的成交价必须等于回测同构价")

	// 现金一致性旁证：买入后现金 = 初始 - (qty*fillPrice + 总费用)。
	expectedCash := 1e8 - (isoQty*res.FillPrice + res.Fee)
	assert.InDelta(t, expectedCash, mt.GetCash(), 1e-6)

	// 费用是同构佣金 + A 股过户费（买入无印花税）。佣金分量与回测一致。
	fb := liveFeeBreakdown(isoQty*res.FillPrice, false)
	assert.InDelta(t, tr.Commission, fb.Commission, 1e-9, "佣金分量与回测同构")
	assert.InDelta(t, fb.Total(), res.Fee, 1e-9, "回填的 Fee 等于该笔完整费用")
}

// liveFeeBreakdown 用默认 A 股费率复算 MockTrader 的完整费用口径（测试侧
// 独立复算，避免直接读 MockTrader 未导出的内部量）。
func liveFeeBreakdown(tradeValue float64, isSell bool) feeTotals {
	f := fees.DefaultAShareFees()
	commission := tradeValue * f.CommissionRate
	if commission < f.MinCommission {
		commission = f.MinCommission
	}
	transfer := tradeValue * f.TransferFeeRate
	var stamp float64
	if isSell {
		stamp = tradeValue * f.StampTaxRate
	}
	return feeTotals{Commission: commission, TransferFee: transfer, StampTax: stamp}
}

type feeTotals struct {
	Commission, TransferFee, StampTax float64
}

func (f feeTotals) Total() float64 { return f.Commission + f.TransferFee + f.StampTax }
