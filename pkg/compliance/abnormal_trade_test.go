package compliance

import (
	"strings"
	"testing"
	"time"
)

// ============================================================
// P2-5: 异常交易检测 — 单元测试
//
// 6 个检测器 (频繁撒单 / 自成交 / 对倒 / 洗售 / 虚假申报 / 拉抬打压)
// 每个都覆盖: 命中、未命中、阈值边界、窗口外过滤
// ============================================================

// mkOrder 生成一个 OrderRecord, base 是基准时间 (后续调整)
func mkOrder(id, sym, dir string, qty, price float64, status string, at time.Time) OrderRecord {
	return OrderRecord{
		OrderID: id, Symbol: sym, Direction: dir, Quantity: qty, Price: price,
		FilledQty: 0, AvgFillPrice: 0, Status: status,
		SubmittedAt: at, UpdatedAt: at,
	}
}

func mkTrade(id, ordID, sym, dir string, qty, price float64, at time.Time) TradeRecord {
	return TradeRecord{
		TradeID: id, OrderID: ordID, Symbol: sym, Direction: dir,
		Quantity: qty, Price: price, Fee: 0, TradeTime: at,
	}
}

// ============================================================
// Detector 1: 频繁撒单
// ============================================================

func TestFrequentCancel_FiresOnHighRate(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	base := now.Add(-30 * time.Second)
	orders := []OrderRecord{
		mkOrder("o1", "000001.SZ", "buy", 1000, 10, "cancelled", base),
		mkOrder("o2", "000001.SZ", "buy", 1000, 10, "cancelled", base.Add(time.Second)),
		mkOrder("o3", "000001.SZ", "buy", 1000, 10, "cancelled", base.Add(2*time.Second)),
	}
	alerts := d.RunAll("acct", orders, nil, now)
	if got := countOf(alerts, CategoryFrequentCancel); got < 1 {
		t.Fatalf("expected ≥1 frequent_cancel alert, got %d", got)
	}
}

func TestFrequentCancel_NoFireBelowThreshold(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	base := now.Add(-30 * time.Second)
	// 2 cancels out of 5 orders = 40% < 50% threshold
	orders := []OrderRecord{
		mkOrder("o1", "000001.SZ", "buy", 1000, 10, "cancelled", base),
		mkOrder("o2", "000001.SZ", "buy", 1000, 10, "filled", base.Add(time.Second)),
		mkOrder("o3", "000001.SZ", "buy", 1000, 10, "cancelled", base.Add(2*time.Second)),
		mkOrder("o4", "000001.SZ", "buy", 1000, 10, "filled", base.Add(3*time.Second)),
		mkOrder("o5", "000001.SZ", "buy", 1000, 10, "filled", base.Add(4*time.Second)),
	}
	alerts := d.RunAll("acct", orders, nil, now)
	if got := countOf(alerts, CategoryFrequentCancel); got != 0 {
		t.Fatalf("expected 0 frequent_cancel alerts, got %d", got)
	}
}

func TestFrequentCancel_OutsideWindow(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	// 5 minutes ago — outside the 1-minute window
	base := now.Add(-5 * time.Minute)
	orders := []OrderRecord{
		mkOrder("o1", "000001.SZ", "buy", 1000, 10, "cancelled", base),
		mkOrder("o2", "000001.SZ", "buy", 1000, 10, "cancelled", base.Add(time.Second)),
		mkOrder("o3", "000001.SZ", "buy", 1000, 10, "cancelled", base.Add(2*time.Second)),
	}
	alerts := d.RunAll("acct", orders, nil, now)
	if got := countOf(alerts, CategoryFrequentCancel); got != 0 {
		t.Fatalf("expected 0 alerts (outside window), got %d", got)
	}
}

// ============================================================
// Detector 2: 自成交
// ============================================================

func TestSelfTrade_FiresOnOppositeSameQty(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	trades := []TradeRecord{
		mkTrade("t1", "o1", "000001.SZ", "buy", 1000, 10, now.Add(-2*time.Minute)),
		mkTrade("t2", "o2", "000001.SZ", "sell", 1000, 10.05, now.Add(-1*time.Minute)),
	}
	alerts := d.RunAll("acct-1", nil, trades, now)
	if got := countOf(alerts, CategorySelfTrade); got < 1 {
		t.Fatalf("expected ≥1 self_trade alert, got %d", got)
	}
}

func TestSelfTrade_NoFireSameDirection(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	trades := []TradeRecord{
		mkTrade("t1", "o1", "000001.SZ", "buy", 1000, 10, now.Add(-2*time.Minute)),
		mkTrade("t2", "o2", "000001.SZ", "buy", 1000, 10.05, now.Add(-1*time.Minute)),
	}
	alerts := d.RunAll("acct-1", nil, trades, now)
	if got := countOf(alerts, CategorySelfTrade); got != 0 {
		t.Fatalf("expected 0 self_trade alerts, got %d", got)
	}
}

func TestSelfTrade_NoAccountID(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	trades := []TradeRecord{
		mkTrade("t1", "o1", "000001.SZ", "buy", 1000, 10, now.Add(-1*time.Minute)),
		mkTrade("t2", "o2", "000001.SZ", "sell", 1000, 10, now.Add(-30*time.Second)),
	}
	// Empty account → self-trade detector skips (we don't know the
	// cross-account context).
	alerts := d.RunAll("", nil, trades, now)
	if got := countOf(alerts, CategorySelfTrade); got != 0 {
		t.Fatalf("expected 0 self_trade (no account), got %d", got)
	}
}

// ============================================================
// Detector 3: 对倒
// ============================================================

func TestWashTrade_FiresOnBuySellMatch(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	trades := []TradeRecord{
		mkTrade("t1", "acct-A:o1", "000001.SZ", "buy", 5000, 10.00, now.Add(-4*time.Minute)),
		mkTrade("t2", "acct-B:o2", "000001.SZ", "sell", 5000, 10.00, now.Add(-3*time.Minute)),
	}
	alerts := d.RunAll("", nil, trades, now)
	if got := countOf(alerts, CategoryWashTrade); got < 1 {
		t.Fatalf("expected ≥1 wash_trade alert, got %d", got)
	}
}

func TestWashTrade_NoFireOnOneDirection(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	trades := []TradeRecord{
		mkTrade("t1", "acct-A:o1", "000001.SZ", "buy", 5000, 10.00, now.Add(-3*time.Minute)),
		mkTrade("t2", "acct-B:o2", "000001.SZ", "buy", 5000, 10.00, now.Add(-2*time.Minute)),
	}
	alerts := d.RunAll("", nil, trades, now)
	if got := countOf(alerts, CategoryWashTrade); got != 0 {
		t.Fatalf("expected 0 wash_trade, got %d", got)
	}
}

func TestWashTrade_PriceToleranceFilter(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	// Same qty + same symbol but prices differ by 5% > 0.5% tolerance
	trades := []TradeRecord{
		mkTrade("t1", "acct-A:o1", "000001.SZ", "buy", 5000, 10.00, now.Add(-3*time.Minute)),
		mkTrade("t2", "acct-B:o2", "000001.SZ", "sell", 5000, 10.50, now.Add(-2*time.Minute)),
	}
	alerts := d.RunAll("", nil, trades, now)
	if got := countOf(alerts, CategoryWashTrade); got != 0 {
		t.Fatalf("expected 0 wash_trade (price diff too large), got %d", got)
	}
}

// ============================================================
// Detector 4: 洗售
// ============================================================

func TestMatchedFlipping_FiresOnFlipFlop(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	trades := []TradeRecord{
		mkTrade("t1", "o1", "000001.SZ", "buy", 500, 10, now.Add(-25*time.Minute)),
		mkTrade("t2", "o2", "000001.SZ", "sell", 500, 10, now.Add(-20*time.Minute)),
		mkTrade("t3", "o3", "000001.SZ", "buy", 500, 10, now.Add(-15*time.Minute)),
		mkTrade("t4", "o4", "000001.SZ", "sell", 500, 10, now.Add(-10*time.Minute)),
	}
	alerts := d.RunAll("acct-1", nil, trades, now)
	if got := countOf(alerts, CategoryMatchedFlipping); got < 1 {
		t.Fatalf("expected ≥1 matched_flipping alert, got %d", got)
	}
}

func TestMatchedFlipping_NoFireOnOneDirection(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	trades := []TradeRecord{
		mkTrade("t1", "o1", "000001.SZ", "buy", 500, 10, now.Add(-25*time.Minute)),
		mkTrade("t2", "o2", "000001.SZ", "buy", 500, 10, now.Add(-20*time.Minute)),
		mkTrade("t3", "o3", "000001.SZ", "buy", 500, 10, now.Add(-15*time.Minute)),
	}
	alerts := d.RunAll("acct-1", nil, trades, now)
	if got := countOf(alerts, CategoryMatchedFlipping); got != 0 {
		t.Fatalf("expected 0 matched_flipping, got %d", got)
	}
}

// ============================================================
// Detector 5: 虚假申报
// ============================================================

func TestSpoofing_FiresOnFastCancels(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	base := now.Add(-30 * time.Second)
	orders := []OrderRecord{
		{OrderID: "o1", Symbol: "000001.SZ", Direction: "buy", Quantity: 20_000, Price: 10, Status: "cancelled",
			SubmittedAt: base, UpdatedAt: base.Add(100 * time.Millisecond)},
		{OrderID: "o2", Symbol: "000001.SZ", Direction: "buy", Quantity: 20_000, Price: 10, Status: "cancelled",
			SubmittedAt: base.Add(time.Second), UpdatedAt: base.Add(time.Second + 200*time.Millisecond)},
	}
	alerts := d.RunAll("acct", orders, nil, now)
	if got := countOf(alerts, CategorySpoofing); got < 1 {
		t.Fatalf("expected ≥1 spoofing alert, got %d", got)
	}
}

func TestSpoofing_NoFireOnSlowCancels(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	base := now.Add(-30 * time.Second)
	// Latency = 2s > 500ms threshold
	orders := []OrderRecord{
		{OrderID: "o1", Symbol: "000001.SZ", Direction: "buy", Quantity: 20_000, Price: 10, Status: "cancelled",
			SubmittedAt: base, UpdatedAt: base.Add(2 * time.Second)},
		{OrderID: "o2", Symbol: "000001.SZ", Direction: "buy", Quantity: 20_000, Price: 10, Status: "cancelled",
			SubmittedAt: base.Add(time.Second), UpdatedAt: base.Add(3 * time.Second)},
	}
	alerts := d.RunAll("acct", orders, nil, now)
	if got := countOf(alerts, CategorySpoofing); got != 0 {
		t.Fatalf("expected 0 spoofing (latency too high), got %d", got)
	}
}

func TestSpoofing_NoFireOnSmallQty(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	base := now.Add(-30 * time.Second)
	// Qty = 1,000 < 10,000 threshold
	orders := []OrderRecord{
		{OrderID: "o1", Symbol: "000001.SZ", Direction: "buy", Quantity: 1000, Price: 10, Status: "cancelled",
			SubmittedAt: base, UpdatedAt: base.Add(100 * time.Millisecond)},
		{OrderID: "o2", Symbol: "000001.SZ", Direction: "buy", Quantity: 1000, Price: 10, Status: "cancelled",
			SubmittedAt: base.Add(time.Second), UpdatedAt: base.Add(time.Second + 100*time.Millisecond)},
	}
	alerts := d.RunAll("acct", orders, nil, now)
	if got := countOf(alerts, CategorySpoofing); got != 0 {
		t.Fatalf("expected 0 spoofing (qty too small), got %d", got)
	}
}

// ============================================================
// Detector 6: 拉抬打压
// ============================================================

func TestManipulation_FiresOnPump(t *testing.T) {
	d := NewAbnormalDetector()
	// Tighten VWAPLookback to 3 so the test can construct a small
	// pump scenario without needing 20+ baseline trades.
	thresholds := DefaultThresholds()
	thresholds.Manipulation.VWAPLookback = 3
	thresholds.Manipulation.PriceDeviation = 0.02
	d.SetThresholds(thresholds)
	now := time.Now()
	// Baseline of 3 trades at 10.00, then 3 trades at 10.50
	// (偏离 +5% > 2% threshold) should trigger pump.
	trades := []TradeRecord{
		mkTrade("t1", "o1", "000001.SZ", "buy", 100, 10.0, now.Add(-4*time.Minute)),
		mkTrade("t2", "o2", "000001.SZ", "buy", 100, 10.0, now.Add(-3*time.Minute)),
		mkTrade("t3", "o3", "000001.SZ", "buy", 100, 10.0, now.Add(-2*time.Minute)),
		mkTrade("t4", "o4", "000001.SZ", "buy", 100, 10.5, now.Add(-1*time.Minute)),
		mkTrade("t5", "o5", "000001.SZ", "buy", 100, 10.5, now.Add(-30*time.Second)),
		mkTrade("t6", "o6", "000001.SZ", "buy", 100, 10.5, now.Add(-15*time.Second)),
	}
	alerts := d.RunAll("", nil, trades, now)
	if got := countOf(alerts, CategoryManipulation); got < 1 {
		t.Fatalf("expected ≥1 manipulation alert, got %d", got)
	}
}

func TestManipulation_FiresOnDump(t *testing.T) {
	d := NewAbnormalDetector()
	thresholds := DefaultThresholds()
	thresholds.Manipulation.VWAPLookback = 3
	thresholds.Manipulation.PriceDeviation = 0.02
	d.SetThresholds(thresholds)
	now := time.Now()
	trades := []TradeRecord{
		mkTrade("t1", "o1", "000001.SZ", "sell", 100, 10.0, now.Add(-4*time.Minute)),
		mkTrade("t2", "o2", "000001.SZ", "sell", 100, 10.0, now.Add(-3*time.Minute)),
		mkTrade("t3", "o3", "000001.SZ", "sell", 100, 10.0, now.Add(-2*time.Minute)),
		mkTrade("t4", "o4", "000001.SZ", "sell", 100, 9.0, now.Add(-1*time.Minute)),
		mkTrade("t5", "o5", "000001.SZ", "sell", 100, 9.0, now.Add(-30*time.Second)),
		mkTrade("t6", "o6", "000001.SZ", "sell", 100, 9.0, now.Add(-15*time.Second)),
	}
	alerts := d.RunAll("", nil, trades, now)
	if got := countOf(alerts, CategoryManipulation); got < 1 {
		t.Fatalf("expected ≥1 manipulation alert (dump), got %d", got)
	}
}

func TestManipulation_NoFireNearVWAP(t *testing.T) {
	d := NewAbnormalDetector()
	now := time.Now()
	trades := []TradeRecord{
		mkTrade("t1", "o1", "000001.SZ", "buy", 100, 10.0, now.Add(-4*time.Minute)),
		mkTrade("t2", "o2", "000001.SZ", "buy", 100, 10.05, now.Add(-3*time.Minute)),
		mkTrade("t3", "o3", "000001.SZ", "buy", 100, 10.10, now.Add(-2*time.Minute)),
	}
	alerts := d.RunAll("", nil, trades, now)
	if got := countOf(alerts, CategoryManipulation); got != 0 {
		t.Fatalf("expected 0 manipulation (within 2%% of VWAP), got %d", got)
	}
}

// ============================================================
// Orchestrator
// ============================================================

func TestOrchestrator_SetThresholds(t *testing.T) {
	d := NewAbnormalDetector()
	custom := DefaultThresholds()
	custom.FrequentCancel.MinCancelCount = 99 // 紧到几乎不可能触发
	d.SetThresholds(custom)
	got := d.Thresholds()
	if got.FrequentCancel.MinCancelCount != 99 {
		t.Fatalf("expected 99, got %d", got.FrequentCancel.MinCancelCount)
	}
}

func TestOrchestrator_RunAll_Empty(t *testing.T) {
	d := NewAbnormalDetector()
	alerts := d.RunAll("acct", nil, nil, time.Now())
	if len(alerts) != 0 {
		t.Fatalf("expected 0 alerts, got %d", len(alerts))
	}
}

func TestCategory_String(t *testing.T) {
	// 中文名用作监管 log / UI 文案
	cases := map[AbnormalCategory]string{
		CategoryFrequentCancel:  "频繁撒单",
		CategorySelfTrade:       "自成交",
		CategoryWashTrade:       "对倒",
		CategoryMatchedFlipping: "洗售",
		CategorySpoofing:        "虚假申报",
		CategoryManipulation:    "拉抬打压",
	}
	for c, expected := range cases {
		if got := c.String(); !strings.Contains(got, expected) {
			t.Fatalf("category %s: expected %q, got %q", c, expected, got)
		}
	}
	// 未知类别回退到自身
	unknown := AbnormalCategory("xyz")
	if got := unknown.String(); got != "xyz" {
		t.Fatalf("expected fallback, got %q", got)
	}
}

// ============================================================
// helpers
// ============================================================

func countOf(alerts []AbnormalAlert, cat AbnormalCategory) int {
	n := 0
	for _, a := range alerts {
		if a.Category == cat {
			n++
		}
	}
	return n
}
