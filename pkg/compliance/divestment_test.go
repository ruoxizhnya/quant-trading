package compliance

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// ============================================================
// P2-7: 减持规则引擎 — 单元测试
//
// 覆盖:
//   - 5 类股东 + 3 种方式的规则默认值
//   - 限售期 (Pre-IPO 36M / 定增 6/18M) 命中即拒
//   - 滚动窗口 90 日 1% (集中竞价) / 2% (大宗) 容量计算
//   - 董监高年内 25% 上限
//   - 协议转让 ≥ 5% 单笔下限
//   - 减持后 1% / 5% 举牌义务告警
//   - 输入校验 (holder type / method / quantity 缺一即拒)
//   - Registry 模式 SetRule / ResetRules / 并发读写
// ============================================================

// fixedClock 为测试提供稳定的时间基准
func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// defaultProfile 给定 holder_type + pct 的标准股东
func defaultProfile(t ShareholderType, pct, shares float64) ShareholderProfile {
	return ShareholderProfile{
		UserID:        "u-1",
		Symbol:        "000001.SZ",
		HolderType:    t,
		HoldingsPct:   pct,
		HoldingsShare: shares,
		AcquiredAt:    time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// ============================================================
// Defaults / Registry
// ============================================================

func TestDefaultDivestmentRules_AllHolders(t *testing.T) {
	rules := DefaultDivestmentRules()
	for _, ht := range AllHolderTypes() {
		if _, ok := rules[ht]; !ok {
			t.Fatalf("missing default rule for %s", ht)
		}
	}
}

func TestAllHolderTypes_Coverage(t *testing.T) {
	// 5 类: controlling / director / major_5pct / pre_ipo / placement
	if got := len(AllHolderTypes()); got != 5 {
		t.Fatalf("expected 5 holder types, got %d", got)
	}
}

func TestAllReductionMethods_Coverage(t *testing.T) {
	if got := len(AllReductionMethods()); got != 3 {
		t.Fatalf("expected 3 methods, got %d", got)
	}
}

func TestNewDivestmentChecker_Defaults(t *testing.T) {
	c := NewDivestmentChecker(nil)
	rules := c.Rules()
	if len(rules) != 5 {
		t.Fatalf("expected 5 default rules, got %d", len(rules))
	}
}

func TestSetRule_Override(t *testing.T) {
	c := NewDivestmentChecker(nil)
	custom := &DivestmentRule{
		HolderType:          HolderTypeMajor5Pct,
		AuctionWindowDays:   60,
		AuctionWindowCapPct: 0.5,
		BlockWindowDays:     60,
		BlockWindowCapPct:   1.0,
		AgreementMinPct:     3.0,
		Source:              "test-override",
	}
	if err := c.SetRule(HolderTypeMajor5Pct, custom); err != nil {
		t.Fatalf("set: %v", err)
	}
	rules := c.Rules()
	got := rules[HolderTypeMajor5Pct]
	if got.AuctionWindowCapPct != 0.5 {
		t.Fatalf("expected override 0.5, got %.4f", got.AuctionWindowCapPct)
	}
}

func TestSetRule_NilRemoves(t *testing.T) {
	c := NewDivestmentChecker(nil)
	if err := c.SetRule(HolderTypePreIPO, nil); err != nil {
		t.Fatalf("set nil: %v", err)
	}
	if _, ok := c.Rules()[HolderTypePreIPO]; ok {
		t.Fatal("expected removal")
	}
}

func TestSetRule_TypeMismatch(t *testing.T) {
	c := NewDivestmentChecker(nil)
	err := c.SetRule(HolderTypeMajor5Pct, &DivestmentRule{HolderType: HolderTypeDirector})
	if err == nil {
		t.Fatal("expected type mismatch error")
	}
}

func TestSetRule_RejectsUnsetType(t *testing.T) {
	c := NewDivestmentChecker(nil)
	err := c.SetRule(HolderTypeUnset, &DivestmentRule{})
	if err == nil {
		t.Fatal("expected unset error")
	}
}

func TestResetRules(t *testing.T) {
	c := NewDivestmentChecker(nil)
	if err := c.SetRule(HolderTypeMajor5Pct, &DivestmentRule{
		HolderType: HolderTypeMajor5Pct, AuctionWindowCapPct: 99,
	}); err != nil {
		t.Fatalf("set: %v", err)
	}
	c.ResetRules()
	if got := c.Rules()[HolderTypeMajor5Pct].AuctionWindowCapPct; got != 1.0 {
		t.Fatalf("expected 1.0 after reset, got %.4f", got)
	}
}

func TestRules_DefensiveCopy(t *testing.T) {
	c := NewDivestmentChecker(nil)
	snap := c.Rules()
	snap[HolderTypeMajor5Pct].AuctionWindowCapPct = 99
	again := c.Rules()
	if again[HolderTypeMajor5Pct].AuctionWindowCapPct == 99 {
		t.Fatal("expected defensive copy")
	}
}

// ============================================================
// LockupPeriod helpers
// ============================================================

func TestLockupPeriod_IsActive(t *testing.T) {
	l := LockupPeriod{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
	}
	cases := map[time.Time]bool{
		time.Date(2023, 12, 31, 0, 0, 0, 0, time.UTC): false,
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC):   true,
		time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC):   true,
		time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC): true,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC):   false,
	}
	for at, want := range cases {
		if got := l.IsActive(at); got != want {
			t.Fatalf("IsActive(%s): expected %v, got %v", at.Format("2006-01-02"), want, got)
		}
	}
}

func TestLockupPeriod_Overlaps(t *testing.T) {
	l1 := LockupPeriod{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC),
	}
	cases := []struct {
		name  string
		other LockupPeriod
		want  bool
	}{
		{"touching-left", LockupPeriod{
			StartAt: time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC),
			EndAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		}, false},
		{"fully-inside", LockupPeriod{
			StartAt: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			EndAt:   time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		}, true},
		{"fully-outside", LockupPeriod{
			StartAt: time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
			EndAt:   time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC),
		}, false},
		{"right-edge", LockupPeriod{
			StartAt: time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC),
			EndAt:   time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC),
		}, false},
	}
	for _, tc := range cases {
		if got := l1.Overlaps(tc.other); got != tc.want {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}

// ============================================================
// Check: 输入校验
// ============================================================

func TestCheck_RejectsUnsetHolderType(t *testing.T) {
	c := NewDivestmentChecker(fixedClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)))
	p := defaultProfile(HolderTypeUnset, 6, 6_000_000)
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 100, At: time.Now()}
	res := c.Check(p, plan, nil)
	if res.Allowed {
		t.Fatal("expected reject")
	}
	if !containsReason(res.Reasons, "股东类型未设置") {
		t.Fatalf("missing reason: %v", res.Reasons)
	}
}

func TestCheck_RejectsUnsetMethod(t *testing.T) {
	c := NewDivestmentChecker(fixedClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)))
	p := defaultProfile(HolderTypeMajor5Pct, 6, 6_000_000)
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodUnset, Quantity: 100, At: time.Now()}
	res := c.Check(p, plan, nil)
	if res.Allowed {
		t.Fatal("expected reject")
	}
	if !containsReason(res.Reasons, "减持方式未设置") {
		t.Fatalf("missing reason: %v", res.Reasons)
	}
}

func TestCheck_RejectsZeroQuantity(t *testing.T) {
	c := NewDivestmentChecker(fixedClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)))
	p := defaultProfile(HolderTypeMajor5Pct, 6, 6_000_000)
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 0, At: time.Now()}
	res := c.Check(p, plan, nil)
	if res.Allowed {
		t.Fatal("expected reject")
	}
	if !containsReason(res.Reasons, "拟减持数量") {
		t.Fatalf("missing reason: %v", res.Reasons)
	}
}

func TestCheck_RejectsExcessiveQuantity(t *testing.T) {
	c := NewDivestmentChecker(fixedClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)))
	p := defaultProfile(HolderTypeMajor5Pct, 6, 1_000_000)
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 2_000_000, At: time.Now()}
	res := c.Check(p, plan, nil)
	if res.Allowed {
		t.Fatal("expected reject")
	}
	if !containsReason(res.Reasons, "超过实际持股") {
		t.Fatalf("missing reason: %v", res.Reasons)
	}
}

func TestCheck_RejectsSymbolMismatch(t *testing.T) {
	c := NewDivestmentChecker(fixedClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)))
	p := defaultProfile(HolderTypeMajor5Pct, 6, 1_000_000)
	p.Symbol = "000001.SZ"
	plan := ReductionPlan{Symbol: "000002.SZ", Method: MethodAuction, Quantity: 100, At: time.Now()}
	res := c.Check(p, plan, nil)
	if res.Allowed {
		t.Fatal("expected reject")
	}
	if !containsReason(res.Reasons, "不一致") {
		t.Fatalf("missing reason: %v", res.Reasons)
	}
}

// ============================================================
// Check: 限售期
// ============================================================

func TestCheck_BlocksDuringPreIPOLockup(t *testing.T) {
	// 2024-01-01 上市 → 36 月锁 → 2027-01-01 解禁
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypePreIPO, 8, 8_000_000)
	p.AcquiredAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	// 36 月锁: 2024-01-01 → 2027-01-01
	p.Lockups = []LockupPeriod{{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Reason:  "Pre-IPO 36 月限售",
		Code:    "CSRC-2024-5",
	}}
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 100_000, At: now}
	res := c.Check(p, plan, nil)
	if res.Allowed {
		t.Fatal("expected reject (in lockup)")
	}
	if !containsReason(res.Reasons, "限售期") {
		t.Fatalf("expected lockup reason, got %v", res.Reasons)
	}
	if len(res.Lockups) != 1 {
		t.Fatalf("expected 1 active lockup, got %d", len(res.Lockups))
	}
}

func TestCheck_BlocksDuringPlacementLockup(t *testing.T) {
	now := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypePlacement, 5, 5_000_000)
	// 6 月定增锁: 2024-09-01 → 2025-03-01 (在 now 仍在期内)
	p.Lockups = []LockupPeriod{{
		StartAt: time.Date(2024, 9, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
		Reason:  "定增 6 月限售",
		Code:    "CSRC-2023-59",
	}}
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 10_000, At: now}
	res := c.Check(p, plan, nil)
	if res.Allowed {
		t.Fatal("expected reject (in placement lockup)")
	}
}

func TestCheck_AllowsAfterLockup(t *testing.T) {
	// 锁定期已过
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypePlacement, 5, 5_000_000)
	p.Lockups = []LockupPeriod{{
		StartAt: time.Date(2024, 9, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
		Reason:  "定增 6 月限售 (已解禁)",
		Code:    "CSRC-2023-59",
	}}
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 1_000, At: now}
	res := c.Check(p, plan, nil)
	if !res.Allowed {
		t.Fatalf("expected allowed after lockup, got %v", res.Reasons)
	}
}

func TestCheck_PreIPO_HardRejectNoLockup(t *testing.T) {
	// Pre-IPO 股东即使没有 lockup 条目, 默认规则窗口 cap=0
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypePreIPO, 8, 8_000_000)
	p.Lockups = nil
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 100, At: now}
	res := c.Check(p, plan, nil)
	if res.Allowed {
		t.Fatal("expected reject (Pre-IPO window cap = 0)")
	}
	if !containsReason(res.Reasons, "窗口容量为 0") {
		t.Fatalf("missing reason: %v", res.Reasons)
	}
}

// ============================================================
// Check: 协议转让 ≥ 5% 单笔下限
// ============================================================

func TestCheck_AgreementBelowMinPct(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypeMajor5Pct, 30, 30_000_000)
	// 拟减持 100_000 股 / 30% 总股 = 0.333% < 5%
	plan := ReductionPlan{
		Symbol:   "000001.SZ",
		Method:   MethodAgreement,
		Quantity: 100_000,
		At:       now,
	}
	res := c.Check(p, plan, nil)
	if res.Allowed {
		t.Fatal("expected reject (below 5%)")
	}
	if !containsReason(res.Reasons, "协议转让") {
		t.Fatalf("missing reason: %v", res.Reasons)
	}
}

func TestCheck_AgreementMeetsMinPct(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypeMajor5Pct, 30, 30_000_000) // 30% × 1亿股
	// 拟减 5_000_000 / 1亿股 = 5% (刚好够 ≥ 5%)
	plan := ReductionPlan{
		Symbol:   "000001.SZ",
		Method:   MethodAgreement,
		Quantity: 5_000_000,
		At:       now,
	}
	res := c.Check(p, plan, nil)
	if !res.Allowed {
		t.Fatalf("expected allowed (≥5%%), got %v", res.Reasons)
	}
}

// ============================================================
// Check: 滚动窗口 (90 日 / 1% / 2%)
// ============================================================

func TestCheck_WindowCap_Controlling_Auction(t *testing.T) {
	// 控股股东: 集中竞价 90 日 ≤ 1%
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypeControlling, 10, 10_000_000) // 10% × 1亿股
	plan := ReductionPlan{
		Symbol: "000001.SZ", Method: MethodAuction,
		Quantity: 500_000, // = 0.5%
		At:       now,
	}
	res := c.Check(p, plan, nil)
	if !res.Allowed {
		t.Fatalf("expected allowed, got %v", res.Reasons)
	}
	if res.WindowCapPct != 1.0 {
		t.Fatalf("expected 1%% cap, got %.4f", res.WindowCapPct)
	}
}

func TestCheck_WindowCap_BlockTrade(t *testing.T) {
	// 大宗交易 90 日 ≤ 2%
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypeMajor5Pct, 10, 10_000_000)
	// 拟减 1.5% (大宗上限 2%) → 应通过
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodBlockTrade, Quantity: 1_500_000, At: now}
	res := c.Check(p, plan, nil)
	if !res.Allowed {
		t.Fatalf("expected allowed, got %v", res.Reasons)
	}
	if res.WindowCapPct != 2.0 {
		t.Fatalf("expected 2%% cap, got %.4f", res.WindowCapPct)
	}
}

func TestCheck_WindowCap_Accumulates(t *testing.T) {
	// 30 天前已减 0.7%, 拟再减 0.5% → 累计 1.2% > 1% (cap)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypeControlling, 10, 10_000_000)
	recent := []Reduction{{
		Symbol:   "000001.SZ",
		Method:   MethodAuction,
		Quantity: 700_000, // 0.7% (30 天前, 在 90 日窗内)
		At:       now.AddDate(0, 0, -30),
		Price:    10,
	}}
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 500_000, At: now}
	res := c.Check(p, plan, recent)
	// 0.7% + 0.5% = 1.2% > 1% cap → 截短 approved_qty
	if !res.Allowed {
		t.Fatalf("expected partial-allowed, got reject: %v", res.Reasons)
	}
	// 剩余容量 0.3% × 1 亿股 = 300_000
	if res.ApprovedQty != 300_000 {
		t.Fatalf("expected approved 300000, got %.0f", res.ApprovedQty)
	}
	if res.WindowUsedPct < 0.7 {
		t.Fatalf("expected used ~0.7%%, got %.4f", res.WindowUsedPct)
	}
}

func TestCheck_WindowCap_DifferentMethodNoAccumulates(t *testing.T) {
	// 30 天前已用集中竞价 0.7%, 拟用大宗 1.5% → 大宗独立窗 (2% cap)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypeMajor5Pct, 10, 10_000_000)
	recent := []Reduction{{
		Symbol: "000001.SZ", Method: MethodAuction, Quantity: 700_000,
		At: now.AddDate(0, 0, -30), Price: 10,
	}}
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodBlockTrade, Quantity: 1_500_000, At: now}
	res := c.Check(p, plan, recent)
	if !res.Allowed {
		t.Fatalf("expected allowed (different method window), got %v", res.Reasons)
	}
}

func TestCheck_WindowCap_OutOfRangeNoAccumulates(t *testing.T) {
	// 100 天前的历史 (在 90 日窗外) 不计入
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypeControlling, 10, 10_000_000)
	recent := []Reduction{{
		Symbol: "000001.SZ", Method: MethodAuction, Quantity: 5_000_000, // 5% (远超 1% cap)
		At:    now.AddDate(0, 0, -100), // 100 天前
		Price: 10,
	}}
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 500_000, At: now}
	res := c.Check(p, plan, recent)
	if !res.Allowed {
		t.Fatalf("expected allowed (100d ago out of window), got %v", res.Reasons)
	}
}

// ============================================================
// Check: 董监高年度 25% 上限
// ============================================================

func TestCheck_Director_AnnualCap_Hit(t *testing.T) {
	// 一年内已减 24%, 拟再减 2% → 累计 26% > 25%
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypeDirector, 30, 3_000_000)
	recent := []Reduction{
		{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 2_400_000, // 24%
			At: now.AddDate(0, -3, 0), Price: 10},
	}
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 200_000, At: now}
	res := c.Check(p, plan, recent)
	if res.Allowed {
		t.Fatal("expected reject (annual cap)")
	}
	if !containsReason(res.Reasons, "年度 25%") {
		t.Fatalf("missing annual cap reason: %v", res.Reasons)
	}
}

func TestCheck_Director_AnnualCap_JustBelow(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypeDirector, 30, 3_000_000)
	recent := []Reduction{
		{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 500_000, // 5%
			At: now.AddDate(0, -3, 0), Price: 10},
	}
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 200_000, At: now}
	res := c.Check(p, plan, recent)
	if !res.Allowed {
		t.Fatalf("expected allowed, got %v", res.Reasons)
	}
}

// ============================================================
// Check: 举牌义务告警
// ============================================================

func TestCheck_Warning_Cross5Pct(t *testing.T) {
	// 减持后跌穿 5% → 触发举牌义务
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypeMajor5Pct, 5.5, 5_500_000) // 5.5% × 1亿股
	// 拟减 1_200_000 → 截短到 1% cap (1M) → 剩余 5.5% - 1% = 4.5% (< 5%) → warning
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 1_200_000, At: now}
	res := c.Check(p, plan, nil)
	if !res.Allowed {
		t.Fatalf("expected allowed, got %v", res.Reasons)
	}
	found := false
	for _, w := range res.Warnings {
		if contains(w, "举牌") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 举牌义务 warning, got %v", res.Warnings)
	}
}

func TestCheck_Warning_Cross1Pct(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	c := NewDivestmentChecker(fixedClock(now))
	p := defaultProfile(HolderTypeMajor5Pct, 1.5, 1_500_000) // 1.5% × 1亿股
	// 拟减 800_000 → 0.8% (在 1% cap 内) → 剩余 1.5% - 0.8% = 0.7% (< 1%) → warning
	plan := ReductionPlan{Symbol: "000001.SZ", Method: MethodAuction, Quantity: 800_000, At: now}
	res := c.Check(p, plan, nil)
	if !res.Allowed {
		t.Fatalf("expected allowed, got %v", res.Reasons)
	}
	found := false
	for _, w := range res.Warnings {
		if contains(w, "1%") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 1%% warning, got %v", res.Warnings)
	}
}

// ============================================================
// 并发安全
// ============================================================

func TestSetRule_ConcurrentWithCheck(t *testing.T) {
	c := NewDivestmentChecker(fixedClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = c.Rules()
			}
		}()
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = c.SetRule(HolderTypeMajor5Pct, &DivestmentRule{
					HolderType:          HolderTypeMajor5Pct,
					AuctionWindowDays:   90,
					AuctionWindowCapPct: float64(i%5+1) * 0.1,
					BlockWindowDays:     90,
					BlockWindowCapPct:   2.0,
					Source:              fmt.Sprintf("test-%d-%d", i, j),
				})
			}
		}(i)
	}
	wg.Wait()
}

// ============================================================
// helpers
// ============================================================

func containsReason(reasons []string, substr string) bool {
	for _, r := range reasons {
		if contains(r, substr) {
			return true
		}
	}
	return false
}
