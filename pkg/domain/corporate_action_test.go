package domain

import (
	"testing"
	"time"
)

// ============================================================
// helpers
// ============================================================

func d(y, m, day int) time.Time {
	return time.Date(y, time.Month(m), day, 0, 0, 0, 0, time.UTC)
}

func makePos(symbol string, qty, cost float64) Position {
	return Position{
		Symbol:       symbol,
		Quantity:     qty,
		AvgCost:      cost,
		CurrentPrice: cost,
		Metadata:     map[string]any{},
	}
}

// ============================================================
// 1. CashDividend
// ============================================================

func TestCashDividend_ApplyPaysCash(t *testing.T) {
	a := NewCashDividend("600000.SH", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 5), 0.50)
	if a.Type() != ActionCashDividend {
		t.Errorf("expected cash_dividend, got %s", a.Type())
	}
	pos := makePos("600000.SH", 1000, 10.00)
	newPos, cash := a.Apply(pos, 10.00)
	if cash != 500.00 {
		t.Errorf("expected cash=500, got %.2f", cash)
	}
	if newPos.Quantity != 1000 {
		t.Errorf("expected qty unchanged, got %.0f", newPos.Quantity)
	}
}

func TestCashDividend_EmptyPositionNoCash(t *testing.T) {
	a := NewCashDividend("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 5), 0.50)
	pos := makePos("X", 0, 10.00)
	_, cash := a.Apply(pos, 10.00)
	if cash != 0 {
		t.Errorf("expected cash=0 (empty pos), got %.2f", cash)
	}
}

func TestCashDividend_ZeroPerShare(t *testing.T) {
	a := NewCashDividend("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 5), 0)
	pos := makePos("X", 1000, 10.00)
	_, cash := a.Apply(pos, 10.00)
	if cash != 0 {
		t.Errorf("expected cash=0 (zero per share), got %.2f", cash)
	}
}

// ============================================================
// 2. BonusShare
// ============================================================

func TestBonusShare_ApplyIncreasesQtyAndLowersCost(t *testing.T) {
	a := NewBonusShare("600000.SH", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 2), 5)
	// 10送5 → ratio = 1.5
	pos := makePos("600000.SH", 1000, 10.00)
	newPos, cash := a.Apply(pos, 10.00)
	if cash != 0 {
		t.Errorf("expected cash=0, got %.2f", cash)
	}
	if newPos.Quantity != 1500 {
		t.Errorf("expected qty=1500, got %.0f", newPos.Quantity)
	}
	// avg_cost 应该 10 / 1.5 = 6.6667
	if newPos.AvgCost < 6.66 || newPos.AvgCost > 6.67 {
		t.Errorf("expected avg_cost ≈ 6.67, got %.4f", newPos.AvgCost)
	}
}

func TestBonusShare_TenBonusTen(t *testing.T) {
	a := NewBonusShare("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 2), 10)
	// 10送10 → ratio = 2
	pos := makePos("X", 1000, 20.00)
	newPos, _ := a.Apply(pos, 20.00)
	if newPos.Quantity != 2000 {
		t.Errorf("expected qty=2000, got %.0f", newPos.Quantity)
	}
	if newPos.AvgCost != 10.00 {
		t.Errorf("expected avg_cost=10, got %.4f", newPos.AvgCost)
	}
}

// ============================================================
// 3. CorporateActionSplit
// ============================================================

func TestSplit_ForwardSplit(t *testing.T) {
	a := NewSplit("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 2), 2.0)
	// 1→2
	pos := makePos("X", 1000, 20.00)
	newPos, _ := a.Apply(pos, 20.00)
	if newPos.Quantity != 2000 {
		t.Errorf("expected qty=2000, got %.0f", newPos.Quantity)
	}
	if newPos.AvgCost != 10.00 {
		t.Errorf("expected avg_cost=10, got %.4f", newPos.AvgCost)
	}
}

func TestSplit_ReverseSplit(t *testing.T) {
	a := NewSplit("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 2), 0.5)
	// 2→1
	pos := makePos("X", 1000, 5.00)
	newPos, _ := a.Apply(pos, 5.00)
	if newPos.Quantity != 500 {
		t.Errorf("expected qty=500, got %.0f", newPos.Quantity)
	}
	if newPos.AvgCost != 10.00 {
		t.Errorf("expected avg_cost=10, got %.4f", newPos.AvgCost)
	}
}

func TestSplit_ZeroRatio(t *testing.T) {
	a := NewSplit("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 2), 0)
	pos := makePos("X", 1000, 10.00)
	newPos, cash := a.Apply(pos, 10.00)
	if newPos.Quantity != 1000 || cash != 0 {
		t.Errorf("expected no change on zero ratio, got qty=%.0f cash=%.2f",
			newPos.Quantity, cash)
	}
}

// ============================================================
// 4. RightsIssue
// ============================================================

func TestRightsIssue_ApplyDefaultDeclines(t *testing.T) {
	a := NewRightsIssue("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 10), 3, 5.00)
	// 10配3 @ 5.00. 默认不缴款 → 持仓不变, cash 不变.
	pos := makePos("X", 1000, 10.00)
	newPos, cash := a.Apply(pos, 10.00)
	if newPos.Quantity != 1000 || cash != 0 {
		t.Errorf("expected no change, got qty=%.0f cash=%.2f",
			newPos.Quantity, cash)
	}
}

func TestRightsIssue_ExRefPrice(t *testing.T) {
	a := NewRightsIssue("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 10), 3, 5.00)
	// (10 + 5 * 0.3) / (1 + 0.3) = (10 + 1.5) / 1.3 = 8.846
	ref := a.ExRefPrice(10.00)
	if ref < 8.84 || ref > 8.85 {
		t.Errorf("expected ref ≈ 8.846, got %.4f", ref)
	}
}

func TestRightsIssue_ApplyPaid(t *testing.T) {
	a := NewRightsIssue("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 10), 3, 5.00)
	pos := makePos("X", 1000, 10.00)
	newPos, cash := a.ApplyPaid(pos, 10.00)
	// 新增 300 股, 成本 300 * 5 = 1500 CNY.
	if newPos.Quantity != 1300 {
		t.Errorf("expected qty=1300, got %.0f", newPos.Quantity)
	}
	// 新 avg_cost = (1000*10 + 1500) / 1300 = 11500/1300 = 8.846
	if newPos.AvgCost < 8.84 || newPos.AvgCost > 8.85 {
		t.Errorf("expected avg_cost ≈ 8.846, got %.4f", newPos.AvgCost)
	}
	if cash != -1500 {
		t.Errorf("expected cash=-1500, got %.2f", cash)
	}
}

func TestRightsIssue_ExRefPriceZeroRights(t *testing.T) {
	a := NewRightsIssue("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 10), 0, 5.00)
	if a.ExRefPrice(10.00) != 10.00 {
		t.Errorf("expected ref=prevClose when rights=0, got %.4f", a.ExRefPrice(10.00))
	}
}

// ============================================================
// 5. Placement
// ============================================================

func TestPlacement_ApplyNoChange(t *testing.T) {
	a := NewPlacement("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 10), 100000, 8.00)
	pos := makePos("X", 1000, 10.00)
	newPos, cash := a.Apply(pos, 10.00)
	if newPos.Quantity != 1000 || cash != 0 {
		t.Errorf("expected no change, got qty=%.0f cash=%.2f",
			newPos.Quantity, cash)
	}
}

// ============================================================
// ActionEngine
// ============================================================

func TestActionEngine_ApplyAll_CashDividend(t *testing.T) {
	e := NewActionEngine()
	positions := []Position{makePos("600000.SH", 1000, 10.00)}
	actions := []CorporateAction{
		NewCashDividend("600000.SH", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 5), 0.50),
	}
	asOf := d(2026, 7, 2)
	newPos, cash, outcomes := e.ApplyAll(asOf, positions, map[string]float64{"600000.SH": 10.00}, actions)
	if cash != 500.00 {
		t.Errorf("expected cash=500, got %.2f", cash)
	}
	if len(newPos) != 1 || newPos[0].Quantity != 1000 {
		t.Errorf("expected pos unchanged, got %+v", newPos)
	}
	if len(outcomes) != 1 || !outcomes[0].Position.Applied {
		t.Errorf("expected 1 applied outcome, got %+v", outcomes)
	}
}

func TestActionEngine_ApplyAll_BonusThenDividend(t *testing.T) {
	e := NewActionEngine()
	positions := []Position{makePos("X", 1000, 10.00)}
	actions := []CorporateAction{
		NewBonusShare("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 2), 5),  // 先送
		NewCashDividend("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 5), 0.10), // 后分 (同日)
	}
	asOf := d(2026, 7, 2)
	newPos, cash, _ := e.ApplyAll(asOf, positions, map[string]float64{"X": 10.00}, actions)
	// Bonus: 1000 * 1.5 = 1500 股. 然后 CashDividend 在 1500 股上派 0.10 = 150 CNY.
	if newPos[0].Quantity != 1500 {
		t.Errorf("expected qty=1500, got %.0f", newPos[0].Quantity)
	}
	if cash != 150.00 {
		t.Errorf("expected cash=150, got %.2f", cash)
	}
}

func TestActionEngine_ApplyAll_FutureExDateSkipped(t *testing.T) {
	e := NewActionEngine()
	positions := []Position{makePos("X", 1000, 10.00)}
	actions := []CorporateAction{
		NewCashDividend("X", d(2026, 8, 1), d(2026, 7, 31), d(2026, 8, 5), 0.50),
	}
	asOf := d(2026, 7, 2) // 早于 ex-date
	_, cash, outcomes := e.ApplyAll(asOf, positions, map[string]float64{"X": 10.00}, actions)
	if cash != 0 {
		t.Errorf("expected cash=0 (future ex-date), got %.2f", cash)
	}
	if len(outcomes) != 1 || outcomes[0].Position.Applied {
		t.Errorf("expected not applied, got %+v", outcomes)
	}
	if outcomes[0].Position.SkipReason != "ex_date_in_future" {
		t.Errorf("expected skip reason=ex_date_in_future, got %q",
			outcomes[0].Position.SkipReason)
	}
}

func TestActionEngine_ApplyAll_NoPosition(t *testing.T) {
	e := NewActionEngine()
	positions := []Position{} // 0 持仓
	actions := []CorporateAction{
		NewCashDividend("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 5), 0.50),
	}
	asOf := d(2026, 7, 2)
	_, cash, outcomes := e.ApplyAll(asOf, positions, map[string]float64{"X": 10.00}, actions)
	if cash != 0 {
		t.Errorf("expected cash=0 (no position), got %.2f", cash)
	}
	if outcomes[0].Position.SkipReason != "no_position" {
		t.Errorf("expected skip reason=no_position, got %q",
			outcomes[0].Position.SkipReason)
	}
}

func TestActionEngine_ApplyAll_AlreadyApplied(t *testing.T) {
	e := NewActionEngine()
	positions := []Position{makePos("X", 1000, 10.00)}
	actions := []CorporateAction{
		NewCashDividend("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 5), 0.50),
	}
	asOf := d(2026, 7, 2)
	// 第一次: 应用.
	_, cash1, _ := e.ApplyAll(asOf, positions, map[string]float64{"X": 10.00}, actions)
	if cash1 != 500.00 {
		t.Fatalf("expected first call cash=500, got %.2f", cash1)
	}
	// 第二次: 跳过 (已 applied).
	_, cash2, outcomes2 := e.ApplyAll(asOf, positions, map[string]float64{"X": 10.00}, actions)
	if cash2 != 0 {
		t.Errorf("expected second call cash=0 (already applied), got %.2f", cash2)
	}
	if outcomes2[0].Position.SkipReason != "already_applied" {
		t.Errorf("expected skip reason=already_applied, got %q",
			outcomes2[0].Position.SkipReason)
	}
}

func TestActionEngine_ApplyAll_MultipleSymbols(t *testing.T) {
	e := NewActionEngine()
	positions := []Position{
		makePos("A.SH", 1000, 10.00),
		makePos("B.SZ", 2000, 20.00),
	}
	actions := []CorporateAction{
		NewCashDividend("A.SH", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 5), 0.50),
		NewCashDividend("B.SZ", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 5), 0.30),
	}
	asOf := d(2026, 7, 2)
	_, cash, _ := e.ApplyAll(asOf, positions, map[string]float64{"A.SH": 10.00, "B.SZ": 20.00}, actions)
	// 1000*0.5 + 2000*0.3 = 500 + 600 = 1100.
	if cash != 1100.00 {
		t.Errorf("expected cash=1100, got %.2f", cash)
	}
}

func TestActionEngine_MarkApplied(t *testing.T) {
	e := NewActionEngine()
	a := NewCashDividend("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 5), 0.50)
	if e.IsApplied(a) {
		t.Error("expected not applied initially")
	}
	e.MarkApplied(a)
	if !e.IsApplied(a) {
		t.Error("expected applied after MarkApplied")
	}
}

func TestActionEngine_ApplyAll_DoesNotMutateInput(t *testing.T) {
	e := NewActionEngine()
	original := makePos("X", 1000, 10.00)
	positions := []Position{original}
	actions := []CorporateAction{
		NewBonusShare("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 2), 5),
	}
	_, _, _ = e.ApplyAll(d(2026, 7, 2), positions, map[string]float64{"X": 10.00}, actions)
	if original.Quantity != 1000 {
		t.Errorf("expected input pos unchanged, got qty=%.0f", original.Quantity)
	}
}

// ============================================================
// 综合场景
// ============================================================

func TestScenario_StockSplitThenCashDividend(t *testing.T) {
	// 真实场景: 1→2 拆股后, 立即派 0.10 CNY/股现金.
	e := NewActionEngine()
	positions := []Position{makePos("X", 1000, 20.00)}
	actions := []CorporateAction{
		NewSplit("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 2), 2.0),
		NewCashDividend("X", d(2026, 7, 1), d(2026, 6, 30), d(2026, 7, 5), 0.10),
	}
	asOf := d(2026, 7, 2)
	newPos, cash, _ := e.ApplyAll(asOf, positions, map[string]float64{"X": 20.00}, actions)
	// Split: 1000 → 2000, 20 → 10. CashDiv: 2000 * 0.10 = 200.
	if newPos[0].Quantity != 2000 {
		t.Errorf("expected qty=2000, got %.0f", newPos[0].Quantity)
	}
	if newPos[0].AvgCost != 10.00 {
		t.Errorf("expected avg_cost=10, got %.4f", newPos[0].AvgCost)
	}
	if cash != 200.00 {
		t.Errorf("expected cash=200, got %.2f", cash)
	}
}
