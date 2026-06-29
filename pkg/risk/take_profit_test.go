package risk

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ============================================================
// helpers
// ============================================================

func makePos(symbol string, qty, avgCost float64) domain.Position {
	return domain.Position{
		Symbol:   symbol,
		Quantity: qty,
		AvgCost:  avgCost,
		Metadata: map[string]any{},
	}
}

func newTestChecker() *TakeProfitChecker {
	return NewTakeProfitChecker(zerolog.Nop())
}

// ============================================================
// FixedTakeProfit
// ============================================================

func TestFixedTakeProfit_NotTriggered(t *testing.T) {
	rule := NewFixedTakeProfit(0.20) // +20%
	pos := makePos("600000.SH", 1000, 10.00)
	if _, fired := rule.Evaluate(pos, 11.50); fired {
		t.Error("expected not triggered (11.50 < 12.00)")
	}
}

func TestFixedTakeProfit_TriggeredAtExactThreshold(t *testing.T) {
	rule := NewFixedTakeProfit(0.20)
	pos := makePos("600000.SH", 1000, 10.00)
	act, fired := rule.Evaluate(pos, 12.00)
	if !fired {
		t.Fatal("expected triggered at 12.00")
	}
	if act.SellQuantity != 1000 {
		t.Errorf("expected full sell, got %.0f", act.SellQuantity)
	}
	if act.SellFraction != 1.0 {
		t.Errorf("expected fraction=1.0, got %.2f", act.SellFraction)
	}
	if act.Rule != "fixed" {
		t.Errorf("expected rule=fixed, got %s", act.Rule)
	}
}

func TestFixedTakeProfit_TriggeredAboveThreshold(t *testing.T) {
	rule := NewFixedTakeProfit(0.20)
	pos := makePos("600000.SH", 1000, 10.00)
	if _, fired := rule.Evaluate(pos, 15.00); !fired {
		t.Error("expected triggered at 15.00")
	}
}

func TestFixedTakeProfit_EmptyPosition(t *testing.T) {
	rule := NewFixedTakeProfit(0.20)
	pos := makePos("X", 0, 10.00) // 持仓为 0
	if _, fired := rule.Evaluate(pos, 100.0); fired {
		t.Error("expected not triggered for empty position")
	}
}

func TestFixedTakeProfit_ZeroAvgCost(t *testing.T) {
	rule := NewFixedTakeProfit(0.20)
	pos := makePos("X", 1000, 0) // 异常数据
	if _, fired := rule.Evaluate(pos, 100.0); fired {
		t.Error("expected not triggered for zero avg_cost")
	}
}

func TestFixedTakeProfit_NegativeProfitClampedToZero(t *testing.T) {
	rule := NewFixedTakeProfit(-0.5) // 异常输入
	pos := makePos("X", 1000, 10.00)
	// 强制 0% 即可触发.
	if _, fired := rule.Evaluate(pos, 10.00); !fired {
		t.Error("expected triggered (ProfitPct clamped to 0)")
	}
}

// ============================================================
// TrailingTakeProfit
// ============================================================

func TestTrailingTakeProfit_NotActivated(t *testing.T) {
	rule := NewTrailingTakeProfit(0.05, 0.08) // 5% 激活, 8% 回撤
	pos := makePos("X", 1000, 10.00)
	// 当前 10.50 → 涨幅 5%, 但 < 5% 严格大于不激活, 实际上 ≥ 5% 才是激活阈值.
	// 重新选个低于阈值的: 10.40.
	if _, fired := rule.Evaluate(pos, 10.40); fired {
		t.Error("expected not triggered (below activation threshold)")
	}
	// 元数据应仍未激活.
	if activated, _ := pos.Metadata["trailing_activated"].(bool); activated {
		t.Error("expected not activated")
	}
}

func TestTrailingTakeProfit_ActivateThenTrigger(t *testing.T) {
	rule := NewTrailingTakeProfit(0.05, 0.08) // 5% 激活, 8% 回撤
	pos := makePos("X", 1000, 10.00)
	// 第一次: 11.00 → 应激活 (但这次 Evaluate 不直接触发卖出).
	if _, fired := rule.Evaluate(pos, 11.00); fired {
		t.Error("expected first call to NOT fire (activation only)")
	}
	// 模拟调用方在 pos.Metadata 上设置激活标志.
	pos.Metadata["trailing_activated"] = true
	pos.Metadata["trailing_high"] = 11.00

	// 11.50 → 新高, 跟踪中, 不应触发.
	if _, fired := rule.Evaluate(pos, 11.50); fired {
		t.Error("expected not triggered at new high")
	}
	if high, _ := pos.Metadata["trailing_high"].(float64); high != 11.50 {
		t.Errorf("expected high=11.50, got %.2f", high)
	}

	// 11.50 * (1 - 0.08) = 10.58. 跌到 10.50 → 触发!
	act, fired := rule.Evaluate(pos, 10.50)
	if !fired {
		t.Fatal("expected triggered at 10.50 (8% drop from 11.50 high)")
	}
	if act.SellQuantity != 1000 {
		t.Errorf("expected full sell, got %.0f", act.SellQuantity)
	}
	if act.TriggerPrice < 10.57 || act.TriggerPrice > 10.59 {
		t.Errorf("expected trigger ≈10.58, got %.4f", act.TriggerPrice)
	}
}

func TestTrailingTakeProfit_ActivationTrigger(t *testing.T) {
	rule := NewTrailingTakeProfit(0.05, 0.08)
	pos := makePos("X", 1000, 10.00)
	trig := rule.ActivationTrigger(pos)
	if trig != 10.50 {
		t.Errorf("expected activation=10.50, got %.2f", trig)
	}
}

func TestTrailingTakeProfit_NotYetFiredBeforeDrawdown(t *testing.T) {
	rule := NewTrailingTakeProfit(0.10, 0.05) // 10% 激活, 5% 回撤
	pos := makePos("X", 1000, 10.00)
	pos.Metadata["trailing_activated"] = true
	pos.Metadata["trailing_high"] = 12.00
	// 12.00 * (1 - 0.05) = 11.40. 11.45 → 还在高位.
	if _, fired := rule.Evaluate(pos, 11.45); fired {
		t.Error("expected not triggered at 11.45 (> 11.40 threshold)")
	}
}

// ============================================================
// TieredTakeProfit
// ============================================================

// S7-P0-7 (ODR-043): NewTieredTakeProfit must return errors instead
// of panicking. Production code must never panic (AGENTS.md §6).
func TestNewTieredTakeProfit_ErrorOnEmpty(t *testing.T) {
	rule, err := NewTieredTakeProfit(nil)
	if err == nil {
		t.Error("expected error on empty tiers, got nil")
	}
	if rule != nil {
		t.Errorf("expected nil rule on error, got %+v", rule)
	}
}

func TestNewTieredTakeProfit_ErrorOnBadFraction(t *testing.T) {
	rule, err := NewTieredTakeProfit([]TakeProfitTier{
		{SellFraction: 0, ProfitPct: 0.10},
	})
	if err == nil {
		t.Error("expected error on SellFraction=0, got nil")
	}
	if rule != nil {
		t.Errorf("expected nil rule on error, got %+v", rule)
	}
}

func TestNewTieredTakeProfit_ErrorOnNegativeProfitPct(t *testing.T) {
	rule, err := NewTieredTakeProfit([]TakeProfitTier{
		{SellFraction: 0.5, ProfitPct: -0.10},
	})
	if err == nil {
		t.Error("expected error on ProfitPct<0, got nil")
	}
	if rule != nil {
		t.Errorf("expected nil rule on error, got %+v", rule)
	}
}

func TestNewTieredTakeProfit_SortsByProfitPct(t *testing.T) {
	rule, err := NewTieredTakeProfit([]TakeProfitTier{
		{SellFraction: 0.4, ProfitPct: 0.30},
		{SellFraction: 0.3, ProfitPct: 0.10},
		{SellFraction: 0.3, ProfitPct: 0.20},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rule.Tiers[0].ProfitPct != 0.10 || rule.Tiers[1].ProfitPct != 0.20 || rule.Tiers[2].ProfitPct != 0.30 {
		t.Errorf("expected sorted ascending, got %+v", rule.Tiers)
	}
}

func TestTieredTakeProfit_FirstLevelTriggers(t *testing.T) {
	rule, err := NewTieredTakeProfit([]TakeProfitTier{
		{SellFraction: 0.3, ProfitPct: 0.10},
		{SellFraction: 0.3, ProfitPct: 0.20},
		{SellFraction: 0.4, ProfitPct: 0.30},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pos := makePos("X", 1000, 10.00)
	pos.Metadata[TieredOriginalQtyKey] = 1000.0
	// 11.00 → +10% → 触发 level 1.
	act, fired := rule.Evaluate(pos, 11.00)
	if !fired {
		t.Fatal("expected level 1 triggered")
	}
	if act.Level != 1 {
		t.Errorf("expected level=1, got %d", act.Level)
	}
	if act.SellQuantity != 300 { // 1000 * 0.3
		t.Errorf("expected sell=300, got %.0f", act.SellQuantity)
	}
}

func TestTieredTakeProfit_SecondLevelTriggers(t *testing.T) {
	rule, err := NewTieredTakeProfit([]TakeProfitTier{
		{SellFraction: 0.3, ProfitPct: 0.10},
		{SellFraction: 0.3, ProfitPct: 0.20},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pos := makePos("X", 700, 10.00) // 模拟 level 1 已卖出, 剩 700.
	pos.Metadata[TieredOriginalQtyKey] = 1000.0
	pos.Metadata[TieredLastTriggeredKey] = 0.0 // tier 0 (level 1) 已触发
	// 12.00 → +20% → 触发 level 2.
	act, fired := rule.Evaluate(pos, 12.00)
	if !fired {
		t.Fatal("expected level 2 triggered")
	}
	if act.Level != 2 {
		t.Errorf("expected level=2, got %d", act.Level)
	}
	if act.SellQuantity != 300 { // 1000 * 0.3 (按原始)
		t.Errorf("expected sell=300 (based on original qty), got %.0f", act.SellQuantity)
	}
}

func TestTieredTakeProfit_AllTiersExhausted(t *testing.T) {
	rule, err := NewTieredTakeProfit([]TakeProfitTier{
		{SellFraction: 1.0, ProfitPct: 0.10},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pos := makePos("X", 1000, 10.00)
	pos.Metadata[TieredOriginalQtyKey] = 1000.0
	pos.Metadata[TieredLastTriggeredKey] = 0.0 // tier 0 已触发 (1 层)
	if _, fired := rule.Evaluate(pos, 100.0); fired {
		t.Error("expected not triggered (all tiers exhausted)")
	}
}

func TestTieredTakeProfit_BelowFirstLevel(t *testing.T) {
	rule, err := NewTieredTakeProfit([]TakeProfitTier{
		{SellFraction: 0.3, ProfitPct: 0.10},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pos := makePos("X", 1000, 10.00)
	pos.Metadata[TieredOriginalQtyKey] = 1000.0
	// 10.50 → 5%, 未达 10%.
	if _, fired := rule.Evaluate(pos, 10.50); fired {
		t.Error("expected not triggered (below first level)")
	}
}

func TestTieredTakeProfit_LotRounding(t *testing.T) {
	// 原始 333 股, SellFraction 0.3 = 99.9 股 → 圆整到 100.
	// 99.9 < 100 圆整到 0 → 回退到全部.
	rule, err := NewTieredTakeProfit([]TakeProfitTier{
		{SellFraction: 0.3, ProfitPct: 0.10},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pos := makePos("X", 333, 10.00)
	pos.Metadata[TieredOriginalQtyKey] = 333.0
	act, _ := rule.Evaluate(pos, 11.00)
	// 333 * 0.3 = 99.9, 圆整到 100.
	if act.SellQuantity != 100 {
		t.Errorf("expected sell=100 (lot rounded), got %.0f", act.SellQuantity)
	}
}

// ============================================================
// TakeProfitChecker
// ============================================================

func TestChecker_SetAndGetRule(t *testing.T) {
	c := newTestChecker()
	c.SetRule("X.SH", NewFixedTakeProfit(0.20))
	if c.GetRule("X.SH") == nil {
		t.Fatal("expected rule to be set")
	}
	if c.GetRule("Y.SH") != nil {
		t.Error("expected nil for unbound symbol")
	}
}

func TestChecker_DefaultRule(t *testing.T) {
	c := newTestChecker()
	c.SetRule("*", NewFixedTakeProfit(0.20))
	if c.GetRule("ANYTHING.SH") == nil {
		t.Fatal("expected default rule")
	}
}

func TestChecker_RemoveRule(t *testing.T) {
	c := newTestChecker()
	c.SetRule("X.SH", NewFixedTakeProfit(0.20))
	c.SetRule("X.SH", nil)
	if c.GetRule("X.SH") != nil {
		t.Error("expected rule to be removed")
	}
}

func TestChecker_RulesReturnsSnapshot(t *testing.T) {
	c := newTestChecker()
	c.SetRule("A.SH", NewFixedTakeProfit(0.20))
	c.SetRule("B.SH", NewTrailingTakeProfit(0.05, 0.08))
	bindings := c.Rules()
	if len(bindings) != 2 {
		t.Errorf("expected 2 bindings, got %d", len(bindings))
	}
}

func TestChecker_Check_FixedTriggered(t *testing.T) {
	c := newTestChecker()
	c.SetRule("X.SH", NewFixedTakeProfit(0.20))
	positions := []domain.Position{makePos("X.SH", 1000, 10.00)}
	prices := map[string]float64{"X.SH": 12.00}
	actions := c.Check(positions, prices)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Symbol != "X.SH" {
		t.Errorf("expected X.SH, got %s", actions[0].Symbol)
	}
}

func TestChecker_Check_MultiplePositions(t *testing.T) {
	c := newTestChecker()
	c.SetRule("A.SH", NewFixedTakeProfit(0.20))
	c.SetRule("B.SH", NewFixedTakeProfit(0.10))
	c.SetRule("C.SH", NewFixedTakeProfit(0.50)) // 50% — 不触发
	positions := []domain.Position{
		makePos("A.SH", 1000, 10.00),
		makePos("B.SH", 1000, 10.00),
		makePos("C.SH", 1000, 10.00),
	}
	prices := map[string]float64{
		"A.SH": 12.00, // +20% → 触发
		"B.SH": 11.00, // +10% → 触发
		"C.SH": 12.00, // +20% < 50% → 不触发
	}
	actions := c.Check(positions, prices)
	if len(actions) != 2 {
		t.Errorf("expected 2 actions, got %d", len(actions))
	}
	// 排序: by symbol asc.
	if actions[0].Symbol != "A.SH" || actions[1].Symbol != "B.SH" {
		t.Errorf("expected sorted A,B, got %s,%s", actions[0].Symbol, actions[1].Symbol)
	}
}

func TestChecker_Check_SkipsEmptyPosition(t *testing.T) {
	c := newTestChecker()
	c.SetRule("X.SH", NewFixedTakeProfit(0.20))
	positions := []domain.Position{makePos("X.SH", 0, 10.00)}
	prices := map[string]float64{"X.SH": 12.00}
	if actions := c.Check(positions, prices); len(actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(actions))
	}
}

func TestChecker_Check_SkipsMissingPrice(t *testing.T) {
	c := newTestChecker()
	c.SetRule("X.SH", NewFixedTakeProfit(0.20))
	positions := []domain.Position{makePos("X.SH", 1000, 10.00)}
	// No price.
	if actions := c.Check(positions, map[string]float64{}); len(actions) != 0 {
		t.Errorf("expected 0 actions (no price), got %d", len(actions))
	}
}

func TestChecker_Check_SkipsUnboundSymbol(t *testing.T) {
	c := newTestChecker()
	c.SetRule("X.SH", NewFixedTakeProfit(0.20))
	positions := []domain.Position{makePos("Y.SH", 1000, 10.00)} // Y 没有 rule
	prices := map[string]float64{"Y.SH": 100.00}
	if actions := c.Check(positions, prices); len(actions) != 0 {
		t.Errorf("expected 0 actions (no rule), got %d", len(actions))
	}
}

func TestChecker_Check_TieredMultiLevelInOneCall(t *testing.T) {
	c := newTestChecker()
	rule, err := NewTieredTakeProfit([]TakeProfitTier{
		{SellFraction: 0.3, ProfitPct: 0.10},
		{SellFraction: 0.3, ProfitPct: 0.20},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c.SetRule("X.SH", rule)
	pos := makePos("X.SH", 1000, 10.00)
	pos.Metadata[TieredOriginalQtyKey] = 1000.0
	// 一次 Evaluate 只触发一个层级, 即使价格已经超过 level 2.
	act, fired := rule.Evaluate(pos, 13.00)
	if !fired {
		t.Fatal("expected triggered (level 1)")
	}
	if act.Level != 1 {
		t.Errorf("expected level=1 (only one tier per call), got %d", act.Level)
	}
}

// ============================================================
// 并发安全 (race detector)
// ============================================================

func TestChecker_ConcurrentSetAndCheck(t *testing.T) {
	c := newTestChecker()
	c.SetRule("*", NewFixedTakeProfit(0.20))
	var wg sync.WaitGroup
	positions := []domain.Position{makePos("X.SH", 1000, 10.00)}
	prices := map[string]float64{"X.SH": 12.00}

	// 8 个 goroutine 并发 SetRule + Check, 用 race detector 检测.
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			c.SetRule("X.SH", NewFixedTakeProfit(float64(i%5)*0.05))
		}(i)
		go func() {
			defer wg.Done()
			_ = c.Check(positions, prices)
		}()
	}
	wg.Wait()
	_ = time.Second
}

// TestNewTieredTakeProfit_SourceHasNoPanic is a regression guard for
// S7-P0-7 (ODR-043): the constructor must not call panic() — production
// code must return errors (AGENTS.md §6).
func TestNewTieredTakeProfit_SourceHasNoPanic(t *testing.T) {
	source, err := os.ReadFile("take_profit.go")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if s := string(source); containsPanicInConstructor(s) {
		t.Errorf("take_profit.go must not call panic() in NewTieredTakeProfit (S7-P0-7)")
	}
}

// containsPanicInConstructor checks that the NewTieredTakeProfit
// function body does not contain a panic call. It scans from the func
// declaration to the next top-level "func " keyword.
func containsPanicInConstructor(source string) bool {
	idx := strings.Index(source, "func NewTieredTakeProfit(")
	if idx < 0 {
		return false // function not found; nothing to guard
	}
	rest := source[idx:]
	end := strings.Index(rest, "\nfunc ")
	if end < 0 {
		end = len(rest)
	}
	return strings.Contains(rest[:end], "panic(")
}
