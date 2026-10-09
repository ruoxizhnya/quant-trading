// K4：市场冲击滑点模型（slippageModel == "impact"）撮合测试 + "fixed"
// 回归腿。
//
// 核心正证据（D5）：同一票、同日、同价，两笔单量 10:1 ⇒
// slippage_big > slippage_small（严格大于）。
package execution

import (
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedQuote 返回一个典型报价：收盘价 10，成交量 100000（作 ADV 代理）。
func fixedQuote(volume float64) Quote {
	return Quote{
		Symbol: "000001.SZ", Open: 10.0, High: 11.0, Low: 9.0,
		Close: 10.0, Volume: volume,
		Date: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
	}
}

// impactService 构造一个启用 impact 模型的执行服务。
func impactService(t *testing.T, sigma float64) *BacktestExecutionService {
	t.Helper()
	cfg := domain.DefaultExecutionConfig()
	cfg.SlippageModel = "impact"
	cfg.ImpactSigma = sigma
	s := NewBacktestExecutionService(cfg)
	require.Equal(t, "impact", s.GetSlippageModel())
	return s
}

// TestImpact_LargeOrderPaysMoreSlippage 是 K4 的**核心正证据**：
// 大单滑点严格 > 小单滑点。
func TestImpact_LargeOrderPaysMoreSlippage(t *testing.T) {
	quote := fixedQuote(100000) // ADV 代理 = 10w
	s := impactService(t, 0.02) // 日波动率 2%

	smallQty := 1000.0 // 1% of ADV
	bigQty := 10000.0  // 10% of ADV（10:1）

	small := domain.Order{Symbol: "000001.SZ", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: smallQty}
	big := small
	big.Quantity = bigQty

	tSmall, err := s.ExecuteOrder(small, quote)
	require.NoError(t, err)
	tBig, err := s.ExecuteOrder(big, quote)
	require.NoError(t, err)

	slippageSmall := tSmall.Price - quote.Close
	slippageBig := tBig.Price - quote.Close

	t.Logf("小单 qty=%v 成交价=%.6f 滑点=%.6f", smallQty, tSmall.Price, slippageSmall)
	t.Logf("大单 qty=%v 成交价=%.6f 滑点=%.6f", bigQty, tBig.Price, slippageBig)
	t.Logf("冲击比（大/小）= %.4f（理论 sqrt(10)=3.1623）", slippageBig/slippageSmall)

	assert.Greater(t, slippageBig, slippageSmall, "大单滑点必须严格大于小单滑点")
	assert.Greater(t, slippageBig, 0.0)
	// 平方根模型：10:1 单量 ⇒ 滑点比 ≈ sqrt(10)。
	assert.InDelta(t, 3.16227, slippageBig/slippageSmall, 1e-3)
}

// TestImpact_SellSide 断言卖出方向冲击为负向（卖压压价）。
func TestImpact_SellSide(t *testing.T) {
	quote := fixedQuote(100000)
	s := impactService(t, 0.02)
	big := domain.Order{Symbol: "000001.SZ", Direction: domain.DirectionShort, OrderType: domain.OrderTypeMarket, Quantity: 10000}
	tr, err := s.ExecuteOrder(big, quote)
	require.NoError(t, err)
	assert.Less(t, tr.Price, quote.Close, "大额卖单成交价应低于参考价")
}

// TestImpact_ZeroVolumeDegradesToFixed 断言 Volume<=0 → 退化为 fixed
// 滑点（不静默为零冲击），数值等于 fixed 路径。
func TestImpact_ZeroVolumeDegradesToFixed(t *testing.T) {
	quote := fixedQuote(0) // 无成交量数据
	order := domain.Order{Symbol: "000001.SZ", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 10000}

	impactSvc := impactService(t, 0.02)
	got, err := impactSvc.ExecuteOrder(order, quote)
	require.NoError(t, err)

	fixedCfg := domain.DefaultExecutionConfig() // SlippageModel == "fixed"
	fixedSvc := NewBacktestExecutionService(fixedCfg)
	want, err := fixedSvc.ExecuteOrder(order, quote)
	require.NoError(t, err)

	assert.Equal(t, want.Price, got.Price, "Volume=0 时 impact 必须退化为 fixed 滑点")
	t.Logf("Volume=0 impact 价=%.6f  fixed 价=%.6f（相等）", got.Price, want.Price)
}

// TestImpact_SigmaZeroNoImpact 文档化边界：Sigma=0 → 冲击为 0（模型禁用，
// 需显式配置波动率才有效）。
func TestImpact_SigmaZeroNoImpact(t *testing.T) {
	quote := fixedQuote(100000)
	s := impactService(t, 0.0) // 未配置波动率
	order := domain.Order{Symbol: "000001.SZ", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 100000}
	tr, err := s.ExecuteOrder(order, quote)
	require.NoError(t, err)
	assert.Equal(t, quote.Close, tr.Price, "Sigma=0 冲击为 0，成交价 == 参考价")
}

// TestImpact_LiquidityFactorZeroIsOne 断言 LiquidityFactor 零值按既有约定
// 视为 1.0（与 marketimpact 单测一致）。
func TestImpact_LiquidityFactorZeroIsOne(t *testing.T) {
	quote := fixedQuote(100000)
	cfg := domain.DefaultExecutionConfig()
	cfg.SlippageModel = "impact"
	cfg.ImpactSigma = 0.02
	cfg.ImpactLiquidityFactor = 0 // 默认 1.0
	s := NewBacktestExecutionService(cfg)

	order := domain.Order{Symbol: "000001.SZ", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 1000}
	tr, err := s.ExecuteOrder(order, quote)
	require.NoError(t, err)
	// impact = 0.02*sqrt(1000/100000)*1.0 = 0.002 → price = 10.002
	assert.InDelta(t, 10.0*(1+0.002), tr.Price, 1e-9)
}

// TestFixed_RegressionLeg 是 "fixed" 回归腿：默认配置（SlippageModel
// "fixed"）下成交价逐位等于改动前的语义 Close*(1+FixedSlippageRate)。
// 任何顺手改动 "fixed" 分支的行为都会让本腿变红。
func TestFixed_RegressionLeg(t *testing.T) {
	cfg := domain.DefaultExecutionConfig()
	assert.Equal(t, "fixed", cfg.SlippageModel, "默认滑点模型必须仍是 fixed")
	s := NewBacktestExecutionService(cfg)

	quote := fixedQuote(1000)
	order := domain.Order{Symbol: "000001.SZ", Direction: domain.DirectionLong, OrderType: domain.OrderTypeMarket, Quantity: 100}

	tr, err := s.ExecuteOrder(order, quote)
	require.NoError(t, err)

	wantBuy := quote.Close * (1 + fees.FixedSlippageRate)
	assert.InDelta(t, wantBuy, tr.Price, 1e-12, "fixed 买入价必须 == Close*(1+FixedSlippageRate)")

	sell := order
	sell.Direction = domain.DirectionShort
	trSell, err := s.ExecuteOrder(sell, quote)
	require.NoError(t, err)
	wantSell := quote.Close * (1 - fees.FixedSlippageRate)
	assert.InDelta(t, wantSell, trSell.Price, 1e-12, "fixed 卖出价必须 == Close*(1-FixedSlippageRate)")
	t.Logf("fixed 回归腿：买入价=%.10f 卖出价=%.10f", tr.Price, trSell.Price)
}
