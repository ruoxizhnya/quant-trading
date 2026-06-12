package live

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ---------------------------------------------------------------------------
// P1-5 (ODR-018) — A-share price cage validator tests
//
// 覆盖 4 套核心规则:
//   1. 沪深主板 ±10% + ±2% 笼子
//   2. 创业板 ±20% (无 2% 笼子)
//   3. 科创板 ±20% (无 2% 笼子)
//   4. 北交所 ±30% (无 2% 笼子)
//
// 测试策略: 表格驱动 (table-driven), 每个 case 标注 (board, direction,
// 期望 reason), 用 assert + errors.As 精确检查错误类型与字段。
// ---------------------------------------------------------------------------

func mkLimitOrder(symbol string, dir domain.Direction, price float64) *domain.Order {
	return &domain.Order{
		Symbol:     symbol,
		Direction:  dir,
		OrderType:  domain.OrderTypeLimit,
		LimitPrice: price,
		Quantity:   100,
	}
}

func mkMarketOrder(symbol string, dir domain.Direction) *domain.Order {
	return &domain.Order{
		Symbol:    symbol,
		Direction: dir,
		OrderType: domain.OrderTypeMarket,
		Quantity:  100,
	}
}

func ref(bid, ask, last, prevClose float64) ReferencePrice {
	return ReferencePrice{BestBid: bid, BestAsk: ask, Last: last, PrevClose: prevClose}
}

// ─────────────────────────────────────────────────────────────────────────────
// Main board (沪深主板): ±10% + ±2% cage
// ─────────────────────────────────────────────────────────────────────────────

func TestPriceCage_MainBoard_BuyWithinCage_OK(t *testing.T) {
	// 600519.SH (贵州茅台), prev close 1700.00, 卖一 1710.00, 买一 1708.00
	// 买限价 1710.00 → 偏差 (1710 - 1710) / 1710 = 0% ≤ 2% ✓
	v := NewCageValidator()
	order := mkLimitOrder("600519.SH", domain.DirectionLong, 1710.0)
	err := v.Validate(order, ref(1708.0, 1710.0, 1709.0, 1700.0))
	assert.NoError(t, err)
}

func TestPriceCage_MainBoard_BuyAtCageCeiling_OK(t *testing.T) {
	// 卖一 10.00, cage 2% 顶 = 10.20. 申报 10.20 → 边界通过.
	v := NewCageValidator()
	order := mkLimitOrder("600000.SH", domain.DirectionLong, 10.20)
	err := v.Validate(order, ref(9.99, 10.00, 10.00, 9.50))
	require.NoError(t, err)
}

func TestPriceCage_MainBoard_BuyExceedsCageCeiling_Rejected(t *testing.T) {
	// 卖一 10.00, cage 2% 顶 = 10.20. 申报 10.30 → 超出 2% 笼子.
	v := NewCageValidator()
	order := mkLimitOrder("600000.SH", domain.DirectionLong, 10.30)
	err := v.Validate(order, ref(9.99, 10.00, 10.00, 9.50))
	require.Error(t, err)
	var cage *PriceCageError
	require.True(t, errors.As(err, &cage), "should be *PriceCageError, got %T", err)
	assert.Equal(t, "cage_violation", cage.Reason)
	assert.Equal(t, BoardMainBoardSH, cage.Board)
	assert.Equal(t, 10.20, cage.CageCeil)
	assert.Equal(t, 10.0, cage.Reference)
}

func TestPriceCage_MainBoard_SellWithinCage_OK(t *testing.T) {
	// 买一 9.99, cage 2% 底 = 9.99 × 0.98 = 9.7902 (round4).
	// 卖限价 9.7902 → 边界通过.
	v := NewCageValidator()
	order := mkLimitOrder("600000.SH", domain.DirectionClose, 9.7902)
	err := v.Validate(order, ref(9.99, 10.00, 10.00, 10.00))
	require.NoError(t, err)
}

func TestPriceCage_MainBoard_SellBelowCageFloor_Rejected(t *testing.T) {
	// 买一 9.99, cage 2% 底 = 9.7902. 卖限价 9.79 → 低于 2% 笼子.
	v := NewCageValidator()
	order := mkLimitOrder("600000.SH", domain.DirectionClose, 9.79)
	err := v.Validate(order, ref(9.99, 10.00, 10.00, 10.00))
	require.Error(t, err)
	var cage *PriceCageError
	require.True(t, errors.As(err, &cage))
	assert.Equal(t, "cage_violation", cage.Reason)
	assert.Equal(t, 9.7902, cage.CageFloor)
}

func TestPriceCage_MainBoard_AtLimitUp_ExemptFromCage(t *testing.T) {
	// prev close 10.00, 涨停 11.00. 即便 ask 12.00, 涨停申报 11.00 必须允许.
	v := NewCageValidator()
	order := mkLimitOrder("600000.SH", domain.DirectionLong, 11.00)
	err := v.Validate(order, ref(10.50, 12.00, 10.50, 10.00))
	require.NoError(t, err, "at-limit-up orders must be exempt from cage rule")
}

func TestPriceCage_MainBoard_AtLimitDown_ExemptFromCage(t *testing.T) {
	// prev close 10.00, 跌停 9.00. 即便 bid 8.00, 跌停申报 9.00 必须允许.
	v := NewCageValidator()
	order := mkLimitOrder("600000.SH", domain.DirectionClose, 9.00)
	err := v.Validate(order, ref(8.00, 9.50, 8.50, 10.00))
	require.NoError(t, err, "at-limit-down orders must be exempt from cage rule")
}

func TestPriceCage_MainBoard_BuyAboveLimitUp_Rejected(t *testing.T) {
	// prev close 10.00, 涨停 11.00. 申报 11.05 → 超出 10% 涨跌幅.
	v := NewCageValidator()
	order := mkLimitOrder("600000.SH", domain.DirectionLong, 11.05)
	err := v.Validate(order, ref(10.50, 10.60, 10.55, 10.00))
	require.Error(t, err)
	var cage *PriceCageError
	require.True(t, errors.As(err, &cage))
	assert.Equal(t, "limit_up_violation", cage.Reason)
	assert.Equal(t, 11.00, cage.LimitUp)
}

func TestPriceCage_MainBoard_SellBelowLimitDown_Rejected(t *testing.T) {
	v := NewCageValidator()
	order := mkLimitOrder("600000.SH", domain.DirectionClose, 8.95)
	err := v.Validate(order, ref(9.00, 9.10, 9.05, 10.00))
	require.Error(t, err)
	var cage *PriceCageError
	require.True(t, errors.As(err, &cage))
	assert.Equal(t, "limit_down_violation", cage.Reason)
	assert.Equal(t, 9.00, cage.LimitDown)
}

// ─────────────────────────────────────────────────────────────────────────────
// ChiNext (300xxx.SZ): ±20% 但无 2% 笼子
// ─────────────────────────────────────────────────────────────────────────────

func TestPriceCage_ChiNext_NoCageRule(t *testing.T) {
	v := NewCageValidator()
	// 卖一 100.00, 笼子顶应是 100.00 (无 ±2%). 申报 105.00 (5% 偏离) → 允许.
	order := mkLimitOrder("300750.SZ", domain.DirectionLong, 105.00)
	err := v.Validate(order, ref(99.00, 100.00, 99.50, 100.00))
	require.NoError(t, err, "ChiNext has no ±2% cage rule")
}

func TestPriceCage_ChiNext_LimitUp_20pct(t *testing.T) {
	v := NewCageValidator()
	// prev close 100.00, 涨停 120.00.
	order := mkLimitOrder("300750.SZ", domain.DirectionLong, 120.00)
	err := v.Validate(order, ref(119.00, 119.50, 119.25, 100.00))
	require.NoError(t, err)
}

func TestPriceCage_ChiNext_RejectsAbove20pct(t *testing.T) {
	v := NewCageValidator()
	// prev close 100.00, 涨停 120.00. 申报 121.00 → 超出 20%.
	order := mkLimitOrder("300750.SZ", domain.DirectionLong, 121.00)
	err := v.Validate(order, ref(119.00, 120.00, 119.50, 100.00))
	require.Error(t, err)
	var cage *PriceCageError
	require.True(t, errors.As(err, &cage))
	assert.Equal(t, "limit_up_violation", cage.Reason)
	assert.Equal(t, 120.00, cage.LimitUp)
}

// ─────────────────────────────────────────────────────────────────────────────
// STAR (688xxx.SH): ±20% 但无 2% 笼子
// ─────────────────────────────────────────────────────────────────────────────

func TestPriceCage_STAR_NoCageRule(t *testing.T) {
	v := NewCageValidator()
	// 同样, 卖一 50.00, 申报 53.00 (6% 偏离) → 允许 (无 2% 笼子).
	order := mkLimitOrder("688981.SH", domain.DirectionLong, 53.00)
	err := v.Validate(order, ref(49.50, 50.00, 49.75, 50.00))
	require.NoError(t, err, "STAR has no ±2% cage rule")
}

func TestPriceCage_STAR_LimitUp_20pct(t *testing.T) {
	v := NewCageValidator()
	order := mkLimitOrder("688981.SH", domain.DirectionLong, 60.00) // prev 50 × 1.20
	err := v.Validate(order, ref(58.00, 59.00, 58.50, 50.00))
	require.NoError(t, err)
}

// ─────────────────────────────────────────────────────────────────────────────
// BSE (8xxxxx.BJ): ±30% 但无 2% 笼子
// ─────────────────────────────────────────────────────────────────────────────

func TestPriceCage_BSE_NoCageRule(t *testing.T) {
	v := NewCageValidator()
	// 卖一 20.00, 申报 22.00 (10% 偏离) → 允许 (无 2% 笼子).
	order := mkLimitOrder("830799.BJ", domain.DirectionLong, 22.00)
	err := v.Validate(order, ref(19.50, 20.00, 19.75, 20.00))
	require.NoError(t, err)
}

func TestPriceCage_BSE_LimitUp_30pct(t *testing.T) {
	v := NewCageValidator()
	order := mkLimitOrder("830799.BJ", domain.DirectionLong, 26.00) // prev 20 × 1.30
	err := v.Validate(order, ref(25.00, 25.50, 25.25, 20.00))
	require.NoError(t, err)
}

func TestPriceCage_BSE_RejectsAbove30pct(t *testing.T) {
	v := NewCageValidator()
	// prev 20, 涨停 26. 申报 27 → 超出 30%.
	order := mkLimitOrder("830799.BJ", domain.DirectionLong, 27.00)
	err := v.Validate(order, ref(25.00, 26.00, 25.50, 20.00))
	require.Error(t, err)
	var cage *PriceCageError
	require.True(t, errors.As(err, &cage))
	assert.Equal(t, "limit_up_violation", cage.Reason)
	assert.Equal(t, 26.00, cage.LimitUp)
}

// ─────────────────────────────────────────────────────────────────────────────
// 通用: Market 单 / 边界 / 防御
// ─────────────────────────────────────────────────────────────────────────────

func TestPriceCage_MarketOrder_AlwaysPass(t *testing.T) {
	v := NewCageValidator()
	order := mkMarketOrder("600000.SH", domain.DirectionLong)
	// 即便没有参考价, market 单总通过.
	err := v.Validate(order, ref(0, 0, 0, 0))
	require.NoError(t, err)
}

func TestPriceCage_NonPositivePrice_Rejected(t *testing.T) {
	v := NewCageValidator()
	order := mkLimitOrder("600000.SH", domain.DirectionLong, -1.0)
	err := v.Validate(order, ref(10.0, 10.0, 10.0, 10.0))
	require.Error(t, err)
	var cage *PriceCageError
	require.True(t, errors.As(err, &cage))
	assert.Equal(t, "non_positive_price", cage.Reason)
}

func TestPriceCage_MissingReference_Rejected(t *testing.T) {
	v := NewCageValidator()
	order := mkLimitOrder("600000.SH", domain.DirectionLong, 10.0)
	err := v.Validate(order, ref(0, 0, 0, 0)) // prev_close = 0
	require.Error(t, err)
	var cage *PriceCageError
	require.True(t, errors.As(err, &cage))
	assert.Equal(t, "missing_reference", cage.Reason)
}

func TestPriceCage_NilOrder_ReturnsError(t *testing.T) {
	v := NewCageValidator()
	err := v.Validate(nil, ref(10.0, 10.0, 10.0, 10.0))
	require.Error(t, err)
}

func TestPriceCage_MainBoard_NoQuoteButHasLast_FallsBackToLast(t *testing.T) {
	// 卖一/买一都无, 用 last 作参考. prev close 10, last 10, cage 2% 顶 = 10.20.
	v := NewCageValidator()
	order := mkLimitOrder("600000.SH", domain.DirectionLong, 10.15)
	err := v.Validate(order, ref(0, 0, 10.0, 10.0))
	require.NoError(t, err, "should fall back to last when bid/ask missing")
}

func TestPriceCage_CustomBoardResolver(t *testing.T) {
	// 测试 OverrideBoard: 把 600000.SH 强制标记为 BSE (用于 ST 股票等).
	v := NewCageValidatorWithBoard(func(sym string) Board {
		if sym == "600000.SH" {
			return BoardBSE
		}
		return ClassifySymbol(sym)
	})
	// 600000.SH 通常 ±10%, 强制 BSE → ±30%.
	// prev close 10, 涨停 13. 申报 12 → 主板会被拒, BSE 允许.
	order := mkLimitOrder("600000.SH", domain.DirectionLong, 12.0)
	err := v.Validate(order, ref(11.0, 11.5, 11.25, 10.0))
	require.NoError(t, err, "custom resolver overrides to BSE ±30%")
}

func TestPriceCageError_Is(t *testing.T) {
	// 验证 errors.Is(err, &PriceCageError{}) 匹配.
	v := NewCageValidator()
	order := mkLimitOrder("600000.SH", domain.DirectionLong, 100.0) // way above 10% limit
	err := v.Validate(order, ref(10, 10, 10, 10))
	require.Error(t, err)
	assert.True(t, errors.Is(err, &PriceCageError{}), "errors.Is should match PriceCageError")
}

func TestPriceCageError_Message(t *testing.T) {
	e := &PriceCageError{
		Symbol: "600000.SH", Board: BoardMainBoardSH,
		OrderPrice: 11.05, PrevClose: 10.0,
		LimitUp: 11.0, LimitDown: 9.0,
		Reason: "limit_up_violation",
	}
	msg := e.Error()
	assert.Contains(t, msg, "limit-up")
	assert.Contains(t, msg, "11.0500")
}

func TestLimitUpDown(t *testing.T) {
	// 沪/深主板 ±10%
	up, down := LimitUpDown(BoardMainBoardSH, 10.0)
	assert.Equal(t, 11.0, up)
	assert.Equal(t, 9.0, down)

	// 创业板 ±20%
	up, down = LimitUpDown(BoardChiNext, 100.0)
	assert.Equal(t, 120.0, up)
	assert.Equal(t, 80.0, down)

	// 北交所 ±30%
	up, down = LimitUpDown(BoardBSE, 20.0)
	assert.Equal(t, 26.0, up)
	assert.Equal(t, 14.0, down)

	// prev close = 0 → all zeros
	up, down = LimitUpDown(BoardMainBoardSH, 0)
	assert.Zero(t, up)
	assert.Zero(t, down)
}
