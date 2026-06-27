package live

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// ============================================================
// 测试辅助
// ============================================================

func newTestMarginConfig() MarginConfig {
	return MarginConfig{
		InitialMarginRate:     0.5,
		MaintenanceRatioFloor:  1.3,
		WarningRatio:           1.5,
		FinancingRate:          0.06,
		SecuritiesLendingRate: 0.08,
		DaysPerYear:            365,
		Now: func() time.Time {
			return time.Date(2026, 6, 24, 15, 0, 0, 0, time.UTC)
		},
	}
}

func newTestMarginAccount(t *testing.T, initialCash float64) (*MarginAccount, *ShortableList) {
	t.Helper()
	cfg := newTestMarginConfig()
	sl := NewShortableList()
	// Pre-register some shortable symbols.
	now := cfg.Now()
	sl.Add("600000.SH", 10000, now)
	sl.Add("000001.SZ", 5000, now)
	sl.Add("300001.SZ", 0, now) // 0 = unlimited

	acc, err := NewMarginAccount("test-acct", initialCash, cfg, sl, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewMarginAccount: %v", err)
	}
	return acc, sl
}

func floatEq(a, b, tol float64) bool {
	return math.Abs(a-b) < tol
}

// ============================================================
// MarginConfig tests
// ============================================================

func TestMarginConfig_Defaults(t *testing.T) {
	cfg := DefaultMarginConfig()
	if cfg.InitialMarginRate != 0.5 {
		t.Errorf("InitialMarginRate = %f, want 0.5", cfg.InitialMarginRate)
	}
	if cfg.MaintenanceRatioFloor != 1.3 {
		t.Errorf("MaintenanceRatioFloor = %f, want 1.3", cfg.MaintenanceRatioFloor)
	}
	if cfg.WarningRatio != 1.5 {
		t.Errorf("WarningRatio = %f, want 1.5", cfg.WarningRatio)
	}
	if cfg.FinancingRate != 0.06 {
		t.Errorf("FinancingRate = %f, want 0.06", cfg.FinancingRate)
	}
	if cfg.SecuritiesLendingRate != 0.106 {
		t.Errorf("SecuritiesLendingRate = %f, want 0.106", cfg.SecuritiesLendingRate)
	}
	if cfg.DaysPerYear != 365 {
		t.Errorf("DaysPerYear = %d, want 365", cfg.DaysPerYear)
	}
}

func TestMarginConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     MarginConfig
		wantErr bool
	}{
		{"valid defaults", DefaultMarginConfig(), false},
		{"negative initial margin", MarginConfig{InitialMarginRate: -0.1, MaintenanceRatioFloor: 1.3, WarningRatio: 1.5, DaysPerYear: 365}, true},
		{"initial margin > 1", MarginConfig{InitialMarginRate: 1.5, MaintenanceRatioFloor: 1.3, WarningRatio: 1.5, DaysPerYear: 365}, true},
		{"floor < 1", MarginConfig{InitialMarginRate: 0.5, MaintenanceRatioFloor: 0.9, WarningRatio: 1.5, DaysPerYear: 365}, true},
		{"warning < floor", MarginConfig{InitialMarginRate: 0.5, MaintenanceRatioFloor: 1.5, WarningRatio: 1.3, DaysPerYear: 365}, true},
		{"negative financing rate", MarginConfig{InitialMarginRate: 0.5, MaintenanceRatioFloor: 1.3, WarningRatio: 1.5, FinancingRate: -0.01, DaysPerYear: 365}, true},
		{"zero days per year", MarginConfig{InitialMarginRate: 0.5, MaintenanceRatioFloor: 1.3, WarningRatio: 1.5, DaysPerYear: 0}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// ============================================================
// ShortableList tests
// ============================================================

func TestShortableList_AddAndIsShortable(t *testing.T) {
	sl := NewShortableList()
	now := time.Now()

	if sl.IsShortable("600000.SH") {
		t.Error("expected not shortable before Add")
	}

	sl.Add("600000.SH", 10000, now)
	if !sl.IsShortable("600000.SH") {
		t.Error("expected shortable after Add")
	}
	if sl.Count() != 1 {
		t.Errorf("Count = %d, want 1", sl.Count())
	}
}

func TestShortableList_Remove(t *testing.T) {
	sl := NewShortableList()
	now := time.Now()
	sl.Add("600000.SH", 10000, now)
	sl.Remove("600000.SH")
	if sl.IsShortable("600000.SH") {
		t.Error("expected not shortable after Remove")
	}
	if sl.Count() != 0 {
		t.Errorf("Count = %d, want 0", sl.Count())
	}
}

func TestShortableList_MaxShortableQty(t *testing.T) {
	sl := NewShortableList()
	now := time.Now()

	// Not registered.
	qty, ok := sl.MaxShortableQty("600000.SH")
	if ok {
		t.Error("expected not shortable")
	}
	if qty != -1 {
		t.Errorf("qty = %f, want -1 for non-shortable", qty)
	}

	// Registered with limit.
	sl.Add("600000.SH", 10000, now)
	qty, ok = sl.MaxShortableQty("600000.SH")
	if !ok {
		t.Error("expected shortable")
	}
	if qty != 10000 {
		t.Errorf("qty = %f, want 10000", qty)
	}

	// Registered with no limit (0).
	sl.Add("300001.SZ", 0, now)
	qty, ok = sl.MaxShortableQty("300001.SZ")
	if !ok {
		t.Error("expected shortable")
	}
	if qty != 0 {
		t.Errorf("qty = %f, want 0 (unlimited)", qty)
	}
}

func TestShortableList_Entry(t *testing.T) {
	sl := NewShortableList()
	now := time.Now()
	sl.Add("600000.SH", 10000, now)

	entry, ok := sl.Entry("600000.SH")
	if !ok {
		t.Fatal("expected entry to exist")
	}
	if entry.Symbol != "600000.SH" {
		t.Errorf("Symbol = %s, want 600000.SH", entry.Symbol)
	}
	if entry.MaxQty != 10000 {
		t.Errorf("MaxQty = %f, want 10000", entry.MaxQty)
	}
	if !entry.AddedAt.Equal(now) {
		t.Errorf("AddedAt = %v, want %v", entry.AddedAt, now)
	}
}

func TestShortableList_All(t *testing.T) {
	sl := NewShortableList()
	now := time.Now()
	sl.Add("600000.SH", 10000, now)
	sl.Add("000001.SZ", 5000, now)

	all := sl.All()
	if len(all) != 2 {
		t.Fatalf("len(All) = %d, want 2", len(all))
	}
}

func TestShortableList_ConcurrentAccess(t *testing.T) {
	sl := NewShortableList()
	now := time.Now()
	var wg sync.WaitGroup
	// Concurrently add and query.
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			sl.Add("600000.SH", float64(i), now)
		}(i)
		go func(i int) {
			defer wg.Done()
			sl.IsShortable("600000.SH")
			sl.MaxShortableQty("600000.SH")
		}(i)
	}
	wg.Wait()
}

// ============================================================
// MarginCalculator tests
// ============================================================

func TestMarginCalculator_RequiredMarginForBuy(t *testing.T) {
	calc := NewMarginCalculator(DefaultMarginConfig())
	// 50% of 10000 = 5000
	got := calc.RequiredMarginForBuy(10000)
	if !floatEq(got, 5000, 0.01) {
		t.Errorf("RequiredMarginForBuy(10000) = %f, want 5000", got)
	}
	// Zero trade value.
	if got := calc.RequiredMarginForBuy(0); got != 0 {
		t.Errorf("RequiredMarginForBuy(0) = %f, want 0", got)
	}
}

func TestMarginCalculator_RequiredMarginForShort(t *testing.T) {
	calc := NewMarginCalculator(DefaultMarginConfig())
	// 50% of 10000 = 5000
	got := calc.RequiredMarginForShort(10000)
	if !floatEq(got, 5000, 0.01) {
		t.Errorf("RequiredMarginForShort(10000) = %f, want 5000", got)
	}
}

func TestMarginCalculator_DailyInterest(t *testing.T) {
	calc := NewMarginCalculator(DefaultMarginConfig())
	// Financing: 100000 * 0.06 / 365 = 16.438...
	got := calc.DailyFinancingInterest(100000)
	want := 100000 * 0.06 / 365
	if !floatEq(got, want, 0.001) {
		t.Errorf("DailyFinancingInterest(100000) = %f, want %f", got, want)
	}
	// Lending: 100000 * 0.106 / 365 = 29.041...
	got = calc.DailyLendingInterest(100000)
	want = 100000 * 0.106 / 365
	if !floatEq(got, want, 0.001) {
		t.Errorf("DailyLendingInterest(100000) = %f, want %f", got, want)
	}
}

func TestMarginCalculator_AccruedInterest(t *testing.T) {
	calc := NewMarginCalculator(DefaultMarginConfig())
	// 30 days of financing interest.
	got := calc.AccruedFinancingInterest(100000, 30)
	want := 100000 * 0.06 / 365 * 30
	if !floatEq(got, want, 0.001) {
		t.Errorf("AccruedFinancingInterest(100000, 30) = %f, want %f", got, want)
	}
	// Lending.
	got = calc.AccruedLendingInterest(100000, 30)
	want = 100000 * 0.106 / 365 * 30
	if !floatEq(got, want, 0.001) {
		t.Errorf("AccruedLendingInterest(100000, 30) = %f, want %f", got, want)
	}
}

func TestMarginCalculator_MaintenanceRatio(t *testing.T) {
	calc := NewMarginCalculator(DefaultMarginConfig())
	// 150000 / 100000 = 1.5
	got := calc.MaintenanceRatio(150000, 100000)
	if !floatEq(got, 1.5, 0.001) {
		t.Errorf("MaintenanceRatio(150000, 100000) = %f, want 1.5", got)
	}
	// No debt → infinity.
	got = calc.MaintenanceRatio(100000, 0)
	if !math.IsInf(got, 1) {
		t.Errorf("MaintenanceRatio(100000, 0) = %f, want +Inf", got)
	}
	// Zero assets → 0.
	got = calc.MaintenanceRatio(0, 100000)
	if got != 0 {
		t.Errorf("MaintenanceRatio(0, 100000) = %f, want 0", got)
	}
}

func TestMarginCalculator_RiskLevelChecks(t *testing.T) {
	calc := NewMarginCalculator(DefaultMarginConfig())
	// Safe: ratio >= 1.5
	if !calc.IsSafe(1.5) {
		t.Error("IsSafe(1.5) should be true")
	}
	if calc.IsSafe(1.49) {
		t.Error("IsSafe(1.49) should be false")
	}
	// Warning: 1.3 <= ratio < 1.5
	if !calc.IsWarning(1.4) {
		t.Error("IsWarning(1.4) should be true")
	}
	if calc.IsWarning(1.5) {
		t.Error("IsWarning(1.5) should be false")
	}
	// Forced liquidation: ratio < 1.3
	if !calc.IsForcedLiquidation(1.2) {
		t.Error("IsForcedLiquidation(1.2) should be true")
	}
	if calc.IsForcedLiquidation(1.3) {
		t.Error("IsForcedLiquidation(1.3) should be false (at floor, not below)")
	}
}

func TestMarginCalculator_AvailableMargin(t *testing.T) {
	calc := NewMarginCalculator(DefaultMarginConfig())
	// total_assets=200000, total_debt=100000, used_margin=25000
	// maintenance_margin = 100000 * (1 - 1/1.3) = 100000 * 0.23077 = 23077
	// available = 200000 - 25000 - 23077 = 151923
	got := calc.AvailableMargin(200000, 100000, 25000)
	want := 200000 - 25000 - 100000*(1-1/1.3)
	if !floatEq(got, want, 0.01) {
		t.Errorf("AvailableMargin = %f, want %f", got, want)
	}
	// No debt → no maintenance margin.
	got = calc.AvailableMargin(100000, 0, 0)
	if !floatEq(got, 100000, 0.01) {
		t.Errorf("AvailableMargin (no debt) = %f, want 100000", got)
	}
}

func TestMarginCalculator_HasSufficientMargin(t *testing.T) {
	calc := NewMarginCalculator(DefaultMarginConfig())
	if !calc.HasSufficientMargin(10000, 5000) {
		t.Error("HasSufficientMargin(10000, 5000) should be true")
	}
	if calc.HasSufficientMargin(4000, 5000) {
		t.Error("HasSufficientMargin(4000, 5000) should be false")
	}
}

// ============================================================
// MarginAccount creation tests
// ============================================================

func TestNewMarginAccount_Success(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	if acc.AccountID() != "test-acct" {
		t.Errorf("AccountID = %s, want test-acct", acc.AccountID())
	}
	if acc.Cash() != 1_000_000 {
		t.Errorf("Cash = %f, want 1000000", acc.Cash())
	}
	if acc.FinancingBalance() != 0 {
		t.Errorf("FinancingBalance = %f, want 0", acc.FinancingBalance())
	}
	if len(acc.LongPositions()) != 0 {
		t.Errorf("LongPositions len = %d, want 0", len(acc.LongPositions()))
	}
}

func TestNewMarginAccount_InvalidParams(t *testing.T) {
	cfg := DefaultMarginConfig()
	// Empty account ID.
	_, err := NewMarginAccount("", 100000, cfg, nil, zerolog.Nop())
	if err == nil {
		t.Error("expected error for empty account ID")
	}
	// Negative cash.
	_, err = NewMarginAccount("acct", -100, cfg, nil, zerolog.Nop())
	if err == nil {
		t.Error("expected error for negative cash")
	}
	// Invalid config.
	badCfg := MarginConfig{InitialMarginRate: -1, DaysPerYear: 365}
	_, err = NewMarginAccount("acct", 100000, badCfg, nil, zerolog.Nop())
	if err == nil {
		t.Error("expected error for invalid config")
	}
}

// ============================================================
// MarginBuy tests
// ============================================================

func TestMarginAccount_MarginBuy_Success(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Buy 1000 shares at 10 CNY → trade_value = 10000
	// required_margin = 5000, available should be plenty.
	result, err := acc.MarginBuy(ctx, "600000.SH", 1000, 10.0)
	if err != nil {
		t.Fatalf("MarginBuy: %v", err)
	}
	if result.Operation != OpMarginBuy {
		t.Errorf("Operation = %s, want %s", result.Operation, OpMarginBuy)
	}
	if result.TradeValue != 10000 {
		t.Errorf("TradeValue = %f, want 10000", result.TradeValue)
	}
	if result.FinancingDelta != 10000 {
		t.Errorf("FinancingDelta = %f, want 10000", result.FinancingDelta)
	}
	// Cash should NOT change (borrowed money bought the stock).
	if result.CashDelta != 0 {
		t.Errorf("CashDelta = %f, want 0", result.CashDelta)
	}
	// Financing balance increased.
	if acc.FinancingBalance() != 10000 {
		t.Errorf("FinancingBalance = %f, want 10000", acc.FinancingBalance())
	}
	// Long position created.
	pos, ok := acc.GetLongPosition("600000.SH")
	if !ok {
		t.Fatal("expected long position to exist")
	}
	if pos.Quantity != 1000 {
		t.Errorf("Quantity = %f, want 1000", pos.Quantity)
	}
	if !floatEq(pos.AvgCost, 10.0, 0.01) {
		t.Errorf("AvgCost = %f, want 10.0", pos.AvgCost)
	}
	if pos.FinancingAmount != 10000 {
		t.Errorf("FinancingAmount = %f, want 10000", pos.FinancingAmount)
	}
}

func TestMarginAccount_MarginBuy_InsufficientMargin(t *testing.T) {
	// Small account that can't afford the margin.
	acc, _ := newTestMarginAccount(t, 1000)
	ctx := context.Background()

	// Try to buy 10000 shares at 10 → trade_value = 100000, required = 50000.
	_, err := acc.MarginBuy(ctx, "600000.SH", 10000, 10.0)
	if err == nil {
		t.Fatal("expected insufficient margin error")
	}
}

func TestMarginAccount_MarginBuy_InvalidParams(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	cases := []struct {
		name   string
		symbol string
		qty    float64
		price  float64
	}{
		{"empty symbol", "", 100, 10},
		{"zero qty", "600000.SH", 0, 10},
		{"negative qty", "600000.SH", -100, 10},
		{"zero price", "600000.SH", 100, 0},
		{"negative price", "600000.SH", 100, -10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := acc.MarginBuy(ctx, tc.symbol, tc.qty, tc.price)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestMarginAccount_MarginBuy_MultipleBids(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// First buy: 1000 @ 10
	if _, err := acc.MarginBuy(ctx, "600000.SH", 1000, 10.0); err != nil {
		t.Fatalf("first MarginBuy: %v", err)
	}
	// Second buy: 1000 @ 12
	if _, err := acc.MarginBuy(ctx, "600000.SH", 1000, 12.0); err != nil {
		t.Fatalf("second MarginBuy: %v", err)
	}

	pos, _ := acc.GetLongPosition("600000.SH")
	if pos.Quantity != 2000 {
		t.Errorf("Quantity = %f, want 2000", pos.Quantity)
	}
	// Weighted avg cost = (1000*10 + 1000*12) / 2000 = 11
	if !floatEq(pos.AvgCost, 11.0, 0.01) {
		t.Errorf("AvgCost = %f, want 11.0", pos.AvgCost)
	}
	// Financing = 10000 + 12000 = 22000
	if pos.FinancingAmount != 22000 {
		t.Errorf("FinancingAmount = %f, want 22000", pos.FinancingAmount)
	}
}

// ============================================================
// ShortSell tests
// ============================================================

func TestMarginAccount_ShortSell_Success(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Short 1000 shares at 10 → trade_value = 10000
	result, err := acc.ShortSell(ctx, "600000.SH", 1000, 10.0)
	if err != nil {
		t.Fatalf("ShortSell: %v", err)
	}
	if result.Operation != OpShortSell {
		t.Errorf("Operation = %s, want %s", result.Operation, OpShortSell)
	}
	if result.TradeValue != 10000 {
		t.Errorf("TradeValue = %f, want 10000", result.TradeValue)
	}
	// Cash should increase by proceeds.
	if result.CashDelta != 10000 {
		t.Errorf("CashDelta = %f, want 10000", result.CashDelta)
	}
	if acc.Cash() != 1_010_000 {
		t.Errorf("Cash = %f, want 1010000", acc.Cash())
	}
	// Short position created.
	pos, ok := acc.GetShortPosition("600000.SH")
	if !ok {
		t.Fatal("expected short position to exist")
	}
	if pos.Quantity != 1000 {
		t.Errorf("Quantity = %f, want 1000", pos.Quantity)
	}
	if !floatEq(pos.SalePrice, 10.0, 0.01) {
		t.Errorf("SalePrice = %f, want 10.0", pos.SalePrice)
	}
	if pos.Proceeds != 10000 {
		t.Errorf("Proceeds = %f, want 10000", pos.Proceeds)
	}
}

func TestMarginAccount_ShortSell_NotShortable(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// "999999.SZ" is not in the shortable list.
	_, err := acc.ShortSell(ctx, "999999.SZ", 1000, 10.0)
	if err == nil {
		t.Fatal("expected not-shortable error")
	}
}

func TestMarginAccount_ShortSell_ExceedsMaxQty(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// "600000.SH" has max 10000 shares. Try to short 15000.
	_, err := acc.ShortSell(ctx, "600000.SH", 15000, 10.0)
	if err == nil {
		t.Fatal("expected exceeds-max-qty error")
	}
}

func TestMarginAccount_ShortSell_UnlimitedQty(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 10_000_000)
	ctx := context.Background()

	// "300001.SZ" has max 0 (unlimited). Short a large amount.
	_, err := acc.ShortSell(ctx, "300001.SZ", 50000, 10.0)
	if err != nil {
		t.Fatalf("ShortSell unlimited: %v", err)
	}
}

func TestMarginAccount_ShortSell_InsufficientMargin(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1000)
	ctx := context.Background()

	// Try to short 10000 shares at 10 → trade_value = 100000, required = 50000.
	_, err := acc.ShortSell(ctx, "600000.SH", 10000, 10.0)
	if err == nil {
		t.Fatal("expected insufficient margin error")
	}
}

// ============================================================
// BuyToCover tests
// ============================================================

func TestMarginAccount_BuyToCover_Success(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Short 1000 @ 10
	if _, err := acc.ShortSell(ctx, "600000.SH", 1000, 10.0); err != nil {
		t.Fatalf("ShortSell: %v", err)
	}
	// Cover 500 @ 8 (price dropped, profit)
	result, err := acc.BuyToCover(ctx, "600000.SH", 500, 8.0)
	if err != nil {
		t.Fatalf("BuyToCover: %v", err)
	}
	if result.Operation != OpBuyToCover {
		t.Errorf("Operation = %s, want %s", result.Operation, OpBuyToCover)
	}
	if result.TradeValue != 4000 {
		t.Errorf("TradeValue = %f, want 4000", result.TradeValue)
	}
	if result.CashDelta != -4000 {
		t.Errorf("CashDelta = %f, want -4000", result.CashDelta)
	}
	// Short position reduced.
	pos, ok := acc.GetShortPosition("600000.SH")
	if !ok {
		t.Fatal("expected short position to still exist")
	}
	if pos.Quantity != 500 {
		t.Errorf("Quantity = %f, want 500", pos.Quantity)
	}
}

func TestMarginAccount_BuyToCover_NoShortPosition(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	_, err := acc.BuyToCover(ctx, "600000.SH", 100, 10.0)
	if err == nil {
		t.Fatal("expected no short position error")
	}
}

func TestMarginAccount_BuyToCover_ExceedsShortQty(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Short 1000 @ 10
	if _, err := acc.ShortSell(ctx, "600000.SH", 1000, 10.0); err != nil {
		t.Fatalf("ShortSell: %v", err)
	}
	// Try to cover 2000 (more than shorted).
	_, err := acc.BuyToCover(ctx, "600000.SH", 2000, 10.0)
	if err == nil {
		t.Fatal("expected exceeds short qty error")
	}
}

func TestMarginAccount_BuyToCover_FullCover(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Short 1000 @ 10
	if _, err := acc.ShortSell(ctx, "600000.SH", 1000, 10.0); err != nil {
		t.Fatalf("ShortSell: %v", err)
	}
	// Cover all 1000 @ 10
	if _, err := acc.BuyToCover(ctx, "600000.SH", 1000, 10.0); err != nil {
		t.Fatalf("BuyToCover: %v", err)
	}
	// Position should be deleted.
	if _, ok := acc.GetShortPosition("600000.SH"); ok {
		t.Error("expected short position to be deleted after full cover")
	}
}

// ============================================================
// MarginSell tests
// ============================================================

func TestMarginAccount_MarginSell_Success(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Margin buy 1000 @ 10
	if _, err := acc.MarginBuy(ctx, "600000.SH", 1000, 10.0); err != nil {
		t.Fatalf("MarginBuy: %v", err)
	}
	// Sell 500 @ 12 (profit)
	result, err := acc.MarginSell(ctx, "600000.SH", 500, 12.0)
	if err != nil {
		t.Fatalf("MarginSell: %v", err)
	}
	if result.Operation != OpMarginSell {
		t.Errorf("Operation = %s, want %s", result.Operation, OpMarginSell)
	}
	if result.TradeValue != 6000 {
		t.Errorf("TradeValue = %f, want 6000", result.TradeValue)
	}
	if result.CashDelta != 6000 {
		t.Errorf("CashDelta = %f, want 6000", result.CashDelta)
	}
	// Long position reduced.
	pos, ok := acc.GetLongPosition("600000.SH")
	if !ok {
		t.Fatal("expected long position to still exist")
	}
	if pos.Quantity != 500 {
		t.Errorf("Quantity = %f, want 500", pos.Quantity)
	}
	// Financing should be reduced (5000 of 10000).
	if pos.FinancingAmount != 5000 {
		t.Errorf("FinancingAmount = %f, want 5000", pos.FinancingAmount)
	}
}

func TestMarginAccount_MarginSell_NoLongPosition(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	_, err := acc.MarginSell(ctx, "600000.SH", 100, 10.0)
	if err == nil {
		t.Fatal("expected no long position error")
	}
}

func TestMarginAccount_MarginSell_ExceedsLongQty(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Buy 1000 @ 10
	if _, err := acc.MarginBuy(ctx, "600000.SH", 1000, 10.0); err != nil {
		t.Fatalf("MarginBuy: %v", err)
	}
	// Try to sell 2000 (more than held).
	_, err := acc.MarginSell(ctx, "600000.SH", 2000, 10.0)
	if err == nil {
		t.Fatal("expected exceeds long qty error")
	}
}

func TestMarginAccount_MarginSell_FullSell(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Buy 1000 @ 10
	if _, err := acc.MarginBuy(ctx, "600000.SH", 1000, 10.0); err != nil {
		t.Fatalf("MarginBuy: %v", err)
	}
	// Sell all 1000 @ 10
	if _, err := acc.MarginSell(ctx, "600000.SH", 1000, 10.0); err != nil {
		t.Fatalf("MarginSell: %v", err)
	}
	// Position should be deleted.
	if _, ok := acc.GetLongPosition("600000.SH"); ok {
		t.Error("expected long position to be deleted after full sell")
	}
	// Financing balance should be 0.
	if acc.FinancingBalance() != 0 {
		t.Errorf("FinancingBalance = %f, want 0", acc.FinancingBalance())
	}
}

// ============================================================
// Interest accrual tests
// ============================================================

func TestMarginAccount_AccrueInterest(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Margin buy 100000 worth → financing = 100000
	if _, err := acc.MarginBuy(ctx, "600000.SH", 10000, 10.0); err != nil {
		t.Fatalf("MarginBuy: %v", err)
	}
	// Short 10000 worth → lending = 10000
	if _, err := acc.ShortSell(ctx, "000001.SZ", 1000, 10.0); err != nil {
		t.Fatalf("ShortSell: %v", err)
	}

	prices := map[string]float64{
		"600000.SH": 10.0,
		"000001.SZ": 10.0,
	}
	finInt, lendInt, err := acc.AccrueInterest(ctx, 30, prices)
	if err != nil {
		t.Fatalf("AccrueInterest: %v", err)
	}
	// Financing: 100000 * 0.06 / 365 * 30 = 493.15...
	wantFin := 100000 * 0.06 / 365 * 30
	if !floatEq(finInt, wantFin, 0.01) {
		t.Errorf("financing interest = %f, want %f", finInt, wantFin)
	}
	// Lending: 10000 * 0.08 / 365 * 30 = 65.75...
	wantLend := 10000 * 0.08 / 365 * 30
	if !floatEq(lendInt, wantLend, 0.01) {
		t.Errorf("lending interest = %f, want %f", lendInt, wantLend)
	}
	// Accrued interest should be sum.
	if !floatEq(acc.AccruedInterest(), wantFin+wantLend, 0.01) {
		t.Errorf("AccruedInterest = %f, want %f", acc.AccruedInterest(), wantFin+wantLend)
	}
}

func TestMarginAccount_AccrueInterest_ZeroDays(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	finInt, lendInt, err := acc.AccrueInterest(ctx, 0, nil)
	if err != nil {
		t.Fatalf("AccrueInterest: %v", err)
	}
	if finInt != 0 || lendInt != 0 {
		t.Errorf("expected zero interest for 0 days, got fin=%f lend=%f", finInt, lendInt)
	}
}

func TestMarginAccount_AccrueInterest_NegativeDays(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	_, _, err := acc.AccrueInterest(ctx, -1, nil)
	if err == nil {
		t.Fatal("expected error for negative days")
	}
}

// ============================================================
// Maintenance ratio and risk status tests
// ============================================================

func TestMarginAccount_MaintenanceRatio_NoDebt(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	// No positions → no debt → ratio = +Inf
	ratio := acc.MaintenanceRatio(nil)
	if !math.IsInf(ratio, 1) {
		t.Errorf("MaintenanceRatio (no debt) = %f, want +Inf", ratio)
	}
}

func TestMarginAccount_MaintenanceRatio_WithPositions(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Buy 50000 shares @ 10 → financing = 500000
	if _, err := acc.MarginBuy(ctx, "600000.SH", 50000, 10.0); err != nil {
		t.Fatalf("MarginBuy: %v", err)
	}
	prices := map[string]float64{"600000.SH": 10.0}
	ratio := acc.MaintenanceRatio(prices)
	// total_assets = 1000000 + 500000 = 1500000
	// total_debt = 500000
	// ratio = 1500000 / 500000 = 3.0
	if !floatEq(ratio, 3.0, 0.01) {
		t.Errorf("MaintenanceRatio = %f, want 3.0", ratio)
	}
}

func TestMarginAccount_RiskStatus_Safe(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Small position → high ratio.
	acc.MarginBuy(ctx, "600000.SH", 1000, 10.0)
	status := acc.RiskStatus(map[string]float64{"600000.SH": 10.0})
	if status.Status != MarginStatusSafe {
		t.Errorf("Status = %s, want %s", status.Status, MarginStatusSafe)
	}
	if status.ForcedLiquidation {
		t.Error("ForcedLiquidation should be false")
	}
}

func TestMarginAccount_RiskStatus_Warning(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Large position → ratio between 1.3 and 1.5.
	// Buy 60000 @ 10 → financing = 600000
	// assets = 1000000 + 600000 = 1600000, debt = 600000
	// ratio = 1600000 / 600000 = 2.67 (still safe)
	// Need to push it lower. Buy more.
	acc.MarginBuy(ctx, "600000.SH", 60000, 10.0)  // financing = 600000
	acc.MarginBuy(ctx, "600000.SH", 60000, 10.0)  // financing = 1200000
	// assets = 1000000 + 1200000 = 2200000, debt = 1200000
	// ratio = 2200000 / 1200000 = 1.833 (safe)
	// Buy more.
	acc.MarginBuy(ctx, "600000.SH", 60000, 10.0)  // financing = 1800000
	// assets = 1000000 + 1800000 = 2800000, debt = 1800000
	// ratio = 2800000 / 1800000 = 1.556 (safe, just above 1.5)
	// Buy more to get below 1.5.
	acc.MarginBuy(ctx, "600000.SH", 60000, 10.0)  // financing = 2400000
	// assets = 1000000 + 2400000 = 3400000, debt = 2400000
	// ratio = 3400000 / 2400000 = 1.417 (warning)

	prices := map[string]float64{"600000.SH": 10.0}
	status := acc.RiskStatus(prices)
	if status.Status != MarginStatusWarning {
		t.Errorf("Status = %s, want %s (ratio=%.4f)", status.Status, MarginStatusWarning, status.MaintenanceRatio)
	}
}

func TestMarginAccount_RiskStatus_ForcedLiquidation(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Build up a large position, then drop the price to trigger liquidation.
	// Buy 100000 @ 10 → financing = 1000000
	acc.MarginBuy(ctx, "600000.SH", 100000, 10.0)
	// assets = 1000000 + 1000000 = 2000000, debt = 1000000
	// ratio = 2.0 (safe)

	// Price drops to 5: assets = 1000000 + 500000 = 1500000, debt = 1000000
	// ratio = 1.5 (at warning line)
	// Price drops to 4: assets = 1000000 + 400000 = 1400000, debt = 1000000
	// ratio = 1.4 (warning)
	// Price drops to 2: assets = 1000000 + 200000 = 1200000, debt = 1000000
	// ratio = 1.2 (forced liquidation!)
	prices := map[string]float64{"600000.SH": 2.0}
	status := acc.RiskStatus(prices)
	if status.Status != MarginStatusForcedLiquidation {
		t.Errorf("Status = %s, want %s (ratio=%.4f)", status.Status, MarginStatusForcedLiquidation, status.MaintenanceRatio)
	}
	if !status.ForcedLiquidation {
		t.Error("ForcedLiquidation should be true")
	}
}

// ============================================================
// ForceLiquidate tests
// ============================================================

func TestMarginAccount_ForceLiquidate(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Build positions.
	acc.MarginBuy(ctx, "600000.SH", 1000, 10.0)   // financing = 10000
	acc.ShortSell(ctx, "000001.SZ", 1000, 10.0)   // lending = 10000

	prices := map[string]float64{
		"600000.SH": 10.0,
		"000001.SZ": 10.0,
	}
	result, err := acc.ForceLiquidate(ctx, prices, "maintenance ratio breach")
	if err != nil {
		t.Fatalf("ForceLiquidate: %v", err)
	}
	if result.Reason != "maintenance ratio breach" {
		t.Errorf("Reason = %s, want 'maintenance ratio breach'", result.Reason)
	}
	if len(result.LongSold) != 1 {
		t.Errorf("LongSold len = %d, want 1", len(result.LongSold))
	}
	if len(result.ShortCovered) != 1 {
		t.Errorf("ShortCovered len = %d, want 1", len(result.ShortCovered))
	}
	// All positions cleared.
	if len(acc.LongPositions()) != 0 {
		t.Errorf("LongPositions len = %d, want 0", len(acc.LongPositions()))
	}
	if len(acc.ShortPositions()) != 0 {
		t.Errorf("ShortPositions len = %d, want 0", len(acc.ShortPositions()))
	}
	// Financing balance should be 0 (repaid by selling stock).
	if acc.FinancingBalance() != 0 {
		t.Errorf("FinancingBalance = %f, want 0", acc.FinancingBalance())
	}
	// Accrued interest cleared.
	if acc.AccruedInterest() != 0 {
		t.Errorf("AccruedInterest = %f, want 0", acc.AccruedInterest())
	}
}

// ============================================================
// Deposit / Withdraw tests
// ============================================================

func TestMarginAccount_Deposit(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	if err := acc.Deposit(ctx, 500000); err != nil {
		t.Fatalf("Deposit: %v", err)
	}
	if acc.Cash() != 1_500_000 {
		t.Errorf("Cash = %f, want 1500000", acc.Cash())
	}
}

func TestMarginAccount_Deposit_InvalidAmount(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	if err := acc.Deposit(ctx, 0); err == nil {
		t.Error("expected error for zero deposit")
	}
	if err := acc.Deposit(ctx, -100); err == nil {
		t.Error("expected error for negative deposit")
	}
}

func TestMarginAccount_Withdraw_Success(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	if err := acc.Withdraw(ctx, 100000, nil); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if acc.Cash() != 900000 {
		t.Errorf("Cash = %f, want 900000", acc.Cash())
	}
}

func TestMarginAccount_Withdraw_BreachMaintenanceRatio(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Build a position that uses most of the margin.
	// Buy 60000 @ 10 → financing = 600000
	// assets = 1600000, debt = 600000, ratio = 2.667
	acc.MarginBuy(ctx, "600000.SH", 60000, 10.0)
	// Buy more → financing = 1200000
	// assets = 2200000, debt = 1200000, ratio = 1.833
	acc.MarginBuy(ctx, "600000.SH", 60000, 10.0)
	// Buy more → financing = 1500000
	// assets = 2500000, debt = 1500000, ratio = 1.667
	acc.MarginBuy(ctx, "600000.SH", 30000, 10.0)

	prices := map[string]float64{"600000.SH": 10.0}
	// Try to withdraw 500000 → cash = 500000
	// assets = 500000 + 1500000 = 2000000, debt = 1500000
	// ratio = 2000000 / 1500000 = 1.333 (above 1.3, should be OK)
	if err := acc.Withdraw(ctx, 500000, prices); err != nil {
		t.Fatalf("Withdraw should succeed: %v", err)
	}
	// Try to withdraw more → cash = 0
	// assets = 0 + 1500000 = 1500000, debt = 1500000
	// ratio = 1.0 (below 1.3, should fail)
	if err := acc.Withdraw(ctx, 500000, prices); err == nil {
		t.Fatal("expected withdrawal to fail (breach maintenance ratio)")
	}
}

func TestMarginAccount_Withdraw_InsufficientCash(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 100000)
	ctx := context.Background()

	if err := acc.Withdraw(ctx, 200000, nil); err == nil {
		t.Fatal("expected insufficient cash error")
	}
}

// ============================================================
// Context cancellation tests
// ============================================================

func TestMarginAccount_MarginBuy_CancelledContext(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := acc.MarginBuy(ctx, "600000.SH", 1000, 10.0)
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

// ============================================================
// Concurrent access test (race detector)
// ============================================================

func TestMarginAccount_ConcurrentAccess(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 10_000_000)
	ctx := context.Background()

	var wg sync.WaitGroup
	// Concurrent margin buys.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acc.MarginBuy(ctx, "600000.SH", 100, 10.0)
		}()
	}
	// Concurrent short sells.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acc.ShortSell(ctx, "000001.SZ", 100, 10.0)
		}()
	}
	// Concurrent reads.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acc.MaintenanceRatio(map[string]float64{
				"600000.SH": 10.0,
				"000001.SZ": 10.0,
			})
			acc.RiskStatus(map[string]float64{
				"600000.SH": 10.0,
				"000001.SZ": 10.0,
			})
			acc.Cash()
			acc.FinancingBalance()
		}()
	}
	wg.Wait()
}

// ============================================================
// AvailableMargin and TotalAssets/TotalDebt tests
// ============================================================

func TestMarginAccount_TotalAssetsAndDebt(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// No positions.
	if got := acc.TotalAssets(nil); got != 1_000_000 {
		t.Errorf("TotalAssets (empty) = %f, want 1000000", got)
	}
	if got := acc.TotalDebt(nil); got != 0 {
		t.Errorf("TotalDebt (empty) = %f, want 0", got)
	}

	// Margin buy 1000 @ 10 → financing = 10000.
	acc.MarginBuy(ctx, "600000.SH", 1000, 10.0)
	prices := map[string]float64{"600000.SH": 10.0}
	// assets = 1000000 + 10000 = 1010000
	if got := acc.TotalAssets(prices); !floatEq(got, 1010000, 0.01) {
		t.Errorf("TotalAssets = %f, want 1010000", got)
	}
	// debt = 10000
	if got := acc.TotalDebt(prices); !floatEq(got, 10000, 0.01) {
		t.Errorf("TotalDebt = %f, want 10000", got)
	}
}

func TestMarginAccount_AvailableMargin(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	// No positions → available = 1000000 (all cash is available).
	got := acc.AvailableMargin(nil)
	if !floatEq(got, 1_000_000, 0.01) {
		t.Errorf("AvailableMargin (empty) = %f, want 1000000", got)
	}
}

func TestMarginAccount_LendingBalance(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 1_000_000)
	ctx := context.Background()

	// Short 1000 @ 10 → lending = 10000.
	acc.ShortSell(ctx, "600000.SH", 1000, 10.0)
	prices := map[string]float64{"600000.SH": 10.0}
	if got := acc.LendingBalance(prices); !floatEq(got, 10000, 0.01) {
		t.Errorf("LendingBalance = %f, want 10000", got)
	}
	// Price changes → lending balance changes.
	prices["600000.SH"] = 12.0
	if got := acc.LendingBalance(prices); !floatEq(got, 12000, 0.01) {
		t.Errorf("LendingBalance (price up) = %f, want 12000", got)
	}
}

// ============================================================
// TradeID uniqueness test
// ============================================================

func TestMarginAccount_TradeIDUniqueness(t *testing.T) {
	acc, _ := newTestMarginAccount(t, 10_000_000)
	ctx := context.Background()

	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		result, err := acc.MarginBuy(ctx, "600000.SH", 100, 10.0)
		if err != nil {
			t.Fatalf("MarginBuy[%d]: %v", i, err)
		}
		if ids[result.TradeID] {
			t.Errorf("duplicate TradeID: %s", result.TradeID)
		}
		ids[result.TradeID] = true
	}
}
