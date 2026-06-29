package live

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ============================================================
// 测试辅助: stub LiveTrader for ForcedLiquidator tests
// ============================================================

type stockStateStubTrader struct {
	mu sync.Mutex
	// positions 当前模拟持仓 (按 symbol → qty).
	positions map[string]float64
	// flattenCalls 记录每次 EmergencyFlatten 调用.
	flattenCalls []flattenCall
	// flattenErr 让测试模拟券商拒绝.
	flattenErr error
	// soldPerFlatten 控制每次 EmergencyFlatten 返回的 Sold 列表.
	soldPerFlatten []EmergencyFlattenOrder
	// positionsErr 让测试模拟 trader.GetPositions 失败.
	positionsErr error
}

type flattenCall struct {
	reason string
}

func (s *stockStateStubTrader) SubmitOrder(_ context.Context, _ string, _ domain.Direction, _ domain.OrderType, _ float64, _ float64) (*OrderResult, error) {
	return nil, errors.New("not implemented")
}
func (s *stockStateStubTrader) CancelOrder(_ context.Context, _ string) error {
	return nil
}
func (s *stockStateStubTrader) GetOrder(_ context.Context, _ string) (*OrderResult, error) {
	return nil, errors.New("not implemented")
}
func (s *stockStateStubTrader) GetPositions(_ context.Context) ([]PositionInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.positionsErr != nil {
		return nil, s.positionsErr
	}
	out := make([]PositionInfo, 0, len(s.positions))
	for sym, qty := range s.positions {
		out = append(out, PositionInfo{Symbol: sym, Quantity: qty})
	}
	return out, nil
}
func (s *stockStateStubTrader) GetAccount(_ context.Context) (*AccountInfo, error) {
	return &AccountInfo{Cash: 1_000_000}, nil
}
func (s *stockStateStubTrader) Name() string                        { return "stub_trader" }
func (s *stockStateStubTrader) HealthCheck(_ context.Context) error { return nil }
func (s *stockStateStubTrader) EmergencyFlatten(_ context.Context, reason string) (*EmergencyFlattenResult, error) {
	s.mu.Lock()
	s.flattenCalls = append(s.flattenCalls, flattenCall{reason: reason})
	sold := s.soldPerFlatten
	err := s.flattenErr
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &EmergencyFlattenResult{
		Sold:        sold,
		Reason:      reason,
		StartedAt:   time.Now(),
		CompletedAt: time.Now(),
	}, nil
}

// ============================================================
// StockState string + IsTerminal
// ============================================================

func TestStockState_String(t *testing.T) {
	if StockStateListed.String() != "listed" {
		t.Errorf("expected listed, got %q", StockStateListed.String())
	}
	if StockStateDelisted.IsTerminal() != true {
		t.Error("expected Delisted to be terminal")
	}
	if StockStateDelisting.IsTerminal() != false {
		t.Error("expected Delisting NOT to be terminal")
	}
}

// ============================================================
// isValidStockState / isLegalTransition
// ============================================================

func TestIsValidStockState(t *testing.T) {
	cases := []struct {
		s    StockState
		want bool
	}{
		{StockStateListed, true},
		{StockStateSuspended, true},
		{StockStateDelisting, true},
		{StockStateDelisted, true},
		{"garbage", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isValidStockState(tc.s); got != tc.want {
			t.Errorf("isValidStockState(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestIsLegalTransition(t *testing.T) {
	cases := []struct {
		from StockState
		to   StockState
		want bool
	}{
		// Listed → *
		{StockStateListed, StockStateSuspended, true},
		{StockStateListed, StockStateDelisting, true},
		{StockStateListed, StockStateDelisted, true}, // 重大违法 jump-to-delisted
		{StockStateListed, StockStateListed, false},
		// Suspended → *
		{StockStateSuspended, StockStateListed, true}, // 复牌
		{StockStateSuspended, StockStateDelisting, true},
		{StockStateSuspended, StockStateSuspended, false},
		// Delisting → *
		{StockStateDelisting, StockStateDelisted, true},
		{StockStateDelisting, StockStateListed, false},
		{StockStateDelisting, StockStateSuspended, false},
		// Delisted (terminal)
		{StockStateDelisted, StockStateListed, false},
		{StockStateDelisted, StockStateDelisting, false},
	}
	for _, tc := range cases {
		if got := isLegalTransition(tc.from, tc.to); got != tc.want {
			t.Errorf("isLegalTransition(%s, %s) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

// ============================================================
// Registry.SetState
// ============================================================

func newTestRegistry(t *testing.T) (*StockStateRegistry, time.Time) {
	t.Helper()
	now := time.Date(2026, 6, 13, 9, 30, 0, 0, time.UTC)
	cfg := DefaultStockStateConfig()
	cfg.Now = func() time.Time { return now }
	reg := NewStockStateRegistry(cfg, zerolog.Nop())
	return reg, now
}

func TestSetState_FreshListed(t *testing.T) {
	reg, now := newTestRegistry(t)
	if err := reg.SetState("600000.SH", StockStateListed, "", time.Time{}); err != nil {
		t.Fatalf("SetState listed: %v", err)
	}
	rec, ok := reg.GetState("600000.SH")
	if !ok {
		t.Fatal("expected record to exist")
	}
	if rec.State != StockStateListed {
		t.Errorf("expected listed, got %s", rec.State)
	}
	if rec.UpdatedAt != now {
		t.Errorf("expected updated_at=%v, got %v", now, rec.UpdatedAt)
	}
}

func TestSetState_Delisting_AutoPopulatesTimeline(t *testing.T) {
	reg, _ := newTestRegistry(t)
	delisted := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)
	if err := reg.SetState("600000.SH", StockStateDelisting, "财务类退市-连续亏损", delisted); err != nil {
		t.Fatalf("SetState delisting: %v", err)
	}
	rec, _ := reg.GetState("600000.SH")
	if rec.State != StockStateDelisting {
		t.Errorf("expected delisting, got %s", rec.State)
	}
	if !rec.DelistedDate.Equal(delisted) {
		t.Errorf("expected delisted_date=%v, got %v", delisted, rec.DelistedDate)
	}
	// 整理期 21 天前 (15 trading days) → 摘牌前 1 天.
	if rec.DelistingPeriodEnd.IsZero() {
		t.Error("expected delisting_period_end to be set")
	}
	if !rec.DelistingPeriodEnd.Before(delisted) {
		t.Error("expected delisting_period_end < delisted_date")
	}
	if !rec.DelistingPeriodStart.Before(rec.DelistingPeriodEnd) {
		t.Error("expected delisting_period_start < delisting_period_end")
	}
}

func TestSetState_Delisting_RequiresDelistedDate(t *testing.T) {
	reg, _ := newTestRegistry(t)
	err := reg.SetState("600000.SH", StockStateDelisting, "test", time.Time{})
	if err == nil {
		t.Fatal("expected error when delisted_date is zero")
	}
}

func TestSetState_IllegalTransition(t *testing.T) {
	reg, _ := newTestRegistry(t)
	delisted := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)
	// Listed → Delisted (legal, 重大违法)
	if err := reg.SetState("600000.SH", StockStateDelisted, "重大违法", delisted); err != nil {
		t.Fatalf("first SetState: %v", err)
	}
	// Delisted → Listed (illegal)
	if err := reg.SetState("600000.SH", StockStateListed, "", time.Time{}); !errors.Is(err, ErrIllegalStateTransition) {
		t.Errorf("expected ErrIllegalStateTransition, got %v", err)
	}
}

func TestSetState_InvalidState(t *testing.T) {
	reg, _ := newTestRegistry(t)
	if err := reg.SetState("600000.SH", "garbage", "", time.Time{}); err == nil {
		t.Error("expected error for invalid state")
	}
}

func TestSetState_EmptySymbol(t *testing.T) {
	reg, _ := newTestRegistry(t)
	if err := reg.SetState("", StockStateListed, "", time.Time{}); err == nil {
		t.Error("expected error for empty symbol")
	}
}

func TestSetState_SuspendedToListedRestores(t *testing.T) {
	reg, _ := newTestRegistry(t)
	if err := reg.SetState("600000.SH", StockStateSuspended, "临时停牌", time.Time{}); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if err := reg.SetState("600000.SH", StockStateListed, "", time.Time{}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	rec, _ := reg.GetState("600000.SH")
	if rec.State != StockStateListed {
		t.Errorf("expected listed, got %s", rec.State)
	}
}

// ============================================================
// Registry.ListByState / AllSymbols / Count / Delete
// ============================================================

func TestListByState_SortedByDelistedDate(t *testing.T) {
	reg, _ := newTestRegistry(t)
	d1 := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 8, 5, 15, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 6, 30, 15, 0, 0, 0, time.UTC)
	_ = reg.SetState("AAA.SH", StockStateDelisting, "", d2)
	_ = reg.SetState("BBB.SH", StockStateDelisting, "", d1)
	_ = reg.SetState("CCC.SH", StockStateDelisting, "", d3)
	_ = reg.SetState("DDD.SH", StockStateListed, "", time.Time{})

	del := reg.ListByState(StockStateDelisting)
	if len(del) != 3 {
		t.Fatalf("expected 3 delisting, got %d", len(del))
	}
	// Sorted ascending by DelistedDate: CCC (6-30) < BBB (7-22) < AAA (8-5).
	if del[0].Symbol != "CCC.SH" || del[1].Symbol != "BBB.SH" || del[2].Symbol != "AAA.SH" {
		t.Errorf("sort order wrong: %+v", del)
	}

	all := reg.ListByState("")
	if len(all) != 4 {
		t.Errorf("expected 4 total, got %d", len(all))
	}
}

func TestAllSymbols_Sorted(t *testing.T) {
	reg, _ := newTestRegistry(t)
	_ = reg.SetState("C.SH", StockStateListed, "", time.Time{})
	_ = reg.SetState("A.SH", StockStateListed, "", time.Time{})
	_ = reg.SetState("B.SH", StockStateListed, "", time.Time{})
	syms := reg.AllSymbols()
	want := []string{"A.SH", "B.SH", "C.SH"}
	for i, s := range syms {
		if s != want[i] {
			t.Errorf("position %d: got %q, want %q", i, s, want[i])
		}
	}
}

func TestDelete(t *testing.T) {
	reg, _ := newTestRegistry(t)
	_ = reg.SetState("X.SH", StockStateListed, "", time.Time{})
	if reg.Count() != 1 {
		t.Errorf("expected 1, got %d", reg.Count())
	}
	reg.Delete("X.SH")
	if reg.Count() != 0 {
		t.Errorf("expected 0, got %d", reg.Count())
	}
	if _, ok := reg.GetState("X.SH"); ok {
		t.Error("expected X.SH to be gone")
	}
}

// ============================================================
// IsInDelistingWindow
// ============================================================

func TestIsInDelistingWindow(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 30, 0, 0, time.UTC)
	cases := []struct {
		name string
		rec  *StockStateRecord
		win  time.Duration
		want bool
	}{
		{
			name: "inside window (2 days before delisted_date, window=5d)",
			rec: &StockStateRecord{
				State:        StockStateDelisting,
				DelistedDate: now.Add(2 * 24 * time.Hour),
			},
			win:  5 * 24 * time.Hour,
			want: true,
		},
		{
			name: "outside window (10 days before, window=5d)",
			rec: &StockStateRecord{
				State:        StockStateDelisting,
				DelistedDate: now.Add(10 * 24 * time.Hour),
			},
			win:  5 * 24 * time.Hour,
			want: false,
		},
		{
			name: "already delisted (now > delisted_date)",
			rec: &StockStateRecord{
				State:        StockStateDelisting,
				DelistedDate: now.Add(-1 * time.Hour),
			},
			win:  5 * 24 * time.Hour,
			want: false,
		},
		{
			name: "wrong state (Listed)",
			rec: &StockStateRecord{
				State:        StockStateListed,
				DelistedDate: now.Add(2 * 24 * time.Hour),
			},
			win:  5 * 24 * time.Hour,
			want: false,
		},
		{
			name: "missing delisted_date",
			rec: &StockStateRecord{
				State: StockStateDelisting,
			},
			win:  5 * 24 * time.Hour,
			want: false,
		},
		{
			name: "nil rec",
			rec:  nil,
			win:  5 * 24 * time.Hour,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rec.IsInDelistingWindow(now, tc.win); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// ============================================================
// ForcedLiquidator.Scan
// ============================================================

func newTestLiquidator(t *testing.T) (*ForcedLiquidator, *StockStateRegistry, time.Time) {
	t.Helper()
	now := time.Date(2026, 7, 18, 9, 30, 0, 0, time.UTC)
	cfg := DefaultStockStateConfig()
	cfg.LiquidationWindow = 5 * 24 * time.Hour
	cfg.Now = func() time.Time { return now }
	reg := NewStockStateRegistry(cfg, zerolog.Nop())
	liq := NewForcedLiquidator(reg, cfg, zerolog.Nop())
	return liq, reg, now
}

func TestForcedLiquidator_Scan_TriggersFlattenForHeldSymbol(t *testing.T) {
	liq, reg, now := newTestLiquidator(t)
	delisted := now.Add(2 * 24 * time.Hour) // 2 天后摘牌 → in window
	if err := reg.SetState("600000.SH", StockStateDelisting, "财务类退市", delisted); err != nil {
		t.Fatalf("set state: %v", err)
	}

	trader := &stockStateStubTrader{
		positions:      map[string]float64{"600000.SH": 1000},
		soldPerFlatten: []EmergencyFlattenOrder{{Symbol: "600000.SH", OrderID: "O1", Quantity: 1000}},
	}

	res, err := liq.Scan(context.Background(), trader)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if res.TotalSold != 1 {
		t.Errorf("expected 1 sold, got %d", res.TotalSold)
	}
	if res.TotalSkipped != 0 {
		t.Errorf("expected 0 skipped, got %d", res.TotalSkipped)
	}
	if len(trader.flattenCalls) != 1 {
		t.Errorf("expected 1 flatten call, got %d", len(trader.flattenCalls))
	}
	if len(trader.flattenCalls) > 0 {
		call := trader.flattenCalls[0]
		if call.reason == "" {
			t.Error("expected non-empty reason in flatten call")
		}
		if !contains(call.reason, "600000.SH") {
			t.Errorf("expected reason to mention symbol, got %q", call.reason)
		}
	}
}

func TestForcedLiquidator_Scan_SkipsWhenNoPosition(t *testing.T) {
	liq, reg, now := newTestLiquidator(t)
	delisted := now.Add(2 * 24 * time.Hour)
	if err := reg.SetState("600000.SH", StockStateDelisting, "财务类退市", delisted); err != nil {
		t.Fatalf("set state: %v", err)
	}

	trader := &stockStateStubTrader{
		positions: map[string]float64{}, // 没有持仓
	}

	res, err := liq.Scan(context.Background(), trader)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if res.TotalSold != 0 || res.TotalSkipped != 1 {
		t.Errorf("expected 0 sold / 1 skipped, got %d/%d", res.TotalSold, res.TotalSkipped)
	}
	if len(trader.flattenCalls) != 0 {
		t.Errorf("expected 0 flatten calls, got %d", len(trader.flattenCalls))
	}
	if res.Actions[0].Reason != "no_position" {
		t.Errorf("expected reason=no_position, got %q", res.Actions[0].Reason)
	}
}

func TestForcedLiquidator_Scan_OutsideWindowNotForced(t *testing.T) {
	liq, reg, now := newTestLiquidator(t)
	delisted := now.Add(30 * 24 * time.Hour) // 30 天后摘牌 → 远在窗口外
	if err := reg.SetState("600000.SH", StockStateDelisting, "财务类退市", delisted); err != nil {
		t.Fatalf("set state: %v", err)
	}
	trader := &stockStateStubTrader{
		positions: map[string]float64{"600000.SH": 1000},
	}
	res, err := liq.Scan(context.Background(), trader)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// Outside window: action reported as skipped with reason="outside_window",
	// so the operator can audit "delisting stocks NOT yet in the forced window".
	if res.TotalSold != 0 {
		t.Errorf("expected 0 sold (outside window), got %d", res.TotalSold)
	}
	if res.TotalSkipped != 1 {
		t.Errorf("expected 1 skipped (outside_window audit), got %d", res.TotalSkipped)
	}
	if len(trader.flattenCalls) != 0 {
		t.Errorf("expected 0 flatten calls, got %d", len(trader.flattenCalls))
	}
	if len(res.Actions) != 1 || res.Actions[0].Reason != "outside_window" {
		t.Errorf("expected action reason=outside_window, got %+v", res.Actions)
	}
}

func TestForcedLiquidator_Scan_NilTraderReturnsError(t *testing.T) {
	liq, _, _ := newTestLiquidator(t)
	if _, err := liq.Scan(context.Background(), nil); err == nil {
		t.Error("expected error for nil trader")
	}
}

func TestForcedLiquidator_Scan_EmptyRegistry(t *testing.T) {
	liq, _, _ := newTestLiquidator(t)
	trader := &stockStateStubTrader{}
	res, err := liq.Scan(context.Background(), trader)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if res.TotalSold != 0 || res.TotalSkipped != 0 {
		t.Errorf("expected 0/0, got %d/%d", res.TotalSold, res.TotalSkipped)
	}
}

func TestForcedLiquidator_Scan_PositionsError(t *testing.T) {
	liq, reg, now := newTestLiquidator(t)
	delisted := now.Add(2 * 24 * time.Hour)
	if err := reg.SetState("600000.SH", StockStateDelisting, "财务类退市", delisted); err != nil {
		t.Fatalf("set state: %v", err)
	}
	trader := &stockStateStubTrader{positionsErr: fmt.Errorf("broker down")}
	if _, err := liq.Scan(context.Background(), trader); err == nil {
		t.Error("expected error from GetPositions")
	}
}

func TestForcedLiquidator_Scan_FlattenError(t *testing.T) {
	liq, reg, now := newTestLiquidator(t)
	delisted := now.Add(2 * 24 * time.Hour)
	if err := reg.SetState("600000.SH", StockStateDelisting, "财务类退市", delisted); err != nil {
		t.Fatalf("set state: %v", err)
	}
	trader := &stockStateStubTrader{
		positions:  map[string]float64{"600000.SH": 1000},
		flattenErr: fmt.Errorf("broker rejected"),
	}
	res, err := liq.Scan(context.Background(), trader)
	if err != nil {
		t.Fatalf("Scan should not error on per-symbol flatten failure: %v", err)
	}
	if res.TotalSold != 0 || res.TotalSkipped != 1 {
		t.Errorf("expected 0 sold / 1 skipped, got %d/%d", res.TotalSold, res.TotalSkipped)
	}
	if !contains(res.Actions[0].Reason, "flatten_error") {
		t.Errorf("expected reason to mention flatten_error, got %q", res.Actions[0].Reason)
	}
}

func TestForcedLiquidator_Scan_MultipleSymbols(t *testing.T) {
	liq, reg, now := newTestLiquidator(t)
	d1 := now.Add(2 * 24 * time.Hour)  // in window
	d2 := now.Add(3 * 24 * time.Hour)  // in window
	d3 := now.Add(20 * 24 * time.Hour) // out of window
	_ = reg.SetState("AAA.SH", StockStateDelisting, "财务", d1)
	_ = reg.SetState("BBB.SH", StockStateDelisting, "面值", d2)
	_ = reg.SetState("CCC.SH", StockStateDelisting, "重大违法", d3)
	trader := &stockStateStubTrader{
		positions: map[string]float64{
			"AAA.SH": 100,
			"BBB.SH": 200,
			"CCC.SH": 300,
		},
		soldPerFlatten: []EmergencyFlattenOrder{
			{Symbol: "AAA.SH", Quantity: 100},
			{Symbol: "BBB.SH", Quantity: 200},
		},
	}
	res, err := liq.Scan(context.Background(), trader)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if res.TotalSold != 2 {
		t.Errorf("expected 2 sold, got %d", res.TotalSold)
	}
	if res.TotalSkipped != 1 {
		t.Errorf("expected 1 skipped (CCC outside window), got %d", res.TotalSkipped)
	}
}

func TestForcedLiquidator_Scan_NegativeWindowDisables(t *testing.T) {
	cfg := DefaultStockStateConfig()
	cfg.LiquidationWindow = -1 // disabled
	cfg.Now = func() time.Time { return time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC) }
	reg := NewStockStateRegistry(cfg, zerolog.Nop())
	liq := NewForcedLiquidator(reg, cfg, zerolog.Nop())
	delisted := cfg.Now().Add(1 * 24 * time.Hour)
	_ = reg.SetState("X.SH", StockStateDelisting, "", delisted)
	trader := &stockStateStubTrader{positions: map[string]float64{"X.SH": 100}}
	res, _ := liq.Scan(context.Background(), trader)
	if res.TotalSold != 0 {
		t.Errorf("expected 0 sold (window disabled), got %d", res.TotalSold)
	}
}

// ============================================================
// helpers
// ============================================================

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
