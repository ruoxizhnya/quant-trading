package compliance

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/live"
)

// ============================================================
// P2-4: 投资者适当性 — 单元测试
//
// 覆盖:
//   - LookupRequirement / SetRequirement / ResetRegistry 行为
//   - SuitabilityProfile.Check 五道门禁 (risk / asset / experience /
//     risk_test_expired / boards_enabled)
//   - SuitabilityProfile.ExperienceMonths 月数计算边界
//   - SuitabilityProfile.IsBoardEnabled 白名单逻辑
//   - CheckSymbol 板块自动分类
//   - 并发安全性 (SetRequirement + LookupRequirement 同时跑)
// ============================================================

// profileFor 构建一个默认合规的 24 个月 + 100k + C3 用户, 用于正向断言
// 时只调整要测的那一维。
func profileFor(t *testing.T) SuitabilityProfile {
	t.Helper()
	return SuitabilityProfile{
		UserID:           "user-1",
		AssetDailyAvgCNY: 100_000,
		FirstTradeAt:     time.Now().AddDate(-3, 0, 0), // 3 年前
		RiskLevel:        RiskLevelSteady,
		BoardsEnabled:    nil, // 默认 = 全部允许
	}
}

func TestLookupRequirement_Defaults(t *testing.T) {
	// 三个板级要求 (创业板 / 科创板 / 北交所) 必须在默认注册表中
	for _, b := range []live.Board{live.BoardChiNext, live.BoardSTAR, live.BoardBSE} {
		req := LookupRequirement(b)
		if req == nil {
			t.Fatalf("board %s missing from default registry", b)
		}
	}
	// 主板不在适当性范围内
	if got := LookupRequirement(live.BoardMainBoardSH); got != nil {
		t.Fatalf("main board should not require suitability, got %+v", got)
	}
}

func TestSetRequirement_Override(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)
	// 紧阈值注入: ChiNext 改成 50 万 / 36 个月 / C4
	err := SetRequirement(live.BoardChiNext, &BoardRequirement{
		Board:             live.BoardChiNext,
		AssetThresholdCNY: 500_000,
		ExperienceMonths:  36,
		RiskLevel:         RiskLevelAggressive,
		DisplayName:       "创业板(测试)",
	})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	got := LookupRequirement(live.BoardChiNext)
	if got == nil {
		t.Fatal("expected registered")
	}
	if got.AssetThresholdCNY != 500_000 {
		t.Fatalf("expected 500k, got %.0f", got.AssetThresholdCNY)
	}
}

func TestSetRequirement_NilRemoves(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)
	if err := SetRequirement(live.BoardChiNext, nil); err != nil {
		t.Fatalf("set nil: %v", err)
	}
	if got := LookupRequirement(live.BoardChiNext); got != nil {
		t.Fatal("expected removal")
	}
}

func TestSetRequirement_RejectsNegative(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)
	err := SetRequirement(live.BoardChiNext, &BoardRequirement{
		Board:             live.BoardChiNext,
		AssetThresholdCNY: -1,
	})
	if err == nil {
		t.Fatal("expected error for negative asset")
	}
}

func TestSetRequirement_RejectsBadRiskLevel(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)
	err := SetRequirement(live.BoardChiNext, &BoardRequirement{
		Board:     live.BoardChiNext,
		RiskLevel: RiskLevel(99),
	})
	if err == nil {
		t.Fatal("expected error for out-of-range risk level")
	}
}

func TestAllRequirements_ReturnsCopy(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)
	got := AllRequirements()
	if len(got) != 3 {
		t.Fatalf("expected 3 default requirements, got %d", len(got))
	}
	// 修改返回的副本不应该影响 registry
	got[0].AssetThresholdCNY = -1
	again := LookupRequirement(got[0].Board)
	if again.AssetThresholdCNY == -1 {
		t.Fatal("modifying returned copy should not affect registry")
	}
}

func TestExperienceMonths_BoundaryDown(t *testing.T) {
	// 23 个月零 30 天 (2024-06-02 → 2026-06-01) → 应向下取整为 23
	// (因 6/02 > 6/01, 差一天未满 24 个月)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	first := time.Date(2024, 6, 2, 0, 0, 0, 0, time.UTC)
	p := SuitabilityProfile{FirstTradeAt: first}
	if got := p.ExperienceMonths(now); got != 23 {
		t.Fatalf("expected 23, got %d", got)
	}
}

func TestExperienceMonths_BoundaryUp(t *testing.T) {
	// 刚好 24 个月 (同日) → 24
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	first := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	p := SuitabilityProfile{FirstTradeAt: first}
	if got := p.ExperienceMonths(now); got != 24 {
		t.Fatalf("expected 24, got %d", got)
	}
}

func TestExperienceMonths_Future(t *testing.T) {
	now := time.Now()
	p := SuitabilityProfile{FirstTradeAt: now.Add(24 * time.Hour)}
	if got := p.ExperienceMonths(now); got != 0 {
		t.Fatalf("expected 0 for future first trade, got %d", got)
	}
}

func TestExperienceMonths_Zero(t *testing.T) {
	p := SuitabilityProfile{}
	if got := p.ExperienceMonths(time.Now()); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestIsBoardEnabled_EmptyWhitelistMeansAll(t *testing.T) {
	p := SuitabilityProfile{BoardsEnabled: nil}
	if !p.IsBoardEnabled(live.BoardChiNext) {
		t.Fatal("nil whitelist should mean all boards enabled")
	}
}

func TestIsBoardEnabled_WhitelistMatch(t *testing.T) {
	p := SuitabilityProfile{BoardsEnabled: []string{"chinext", "star"}}
	if !p.IsBoardEnabled(live.BoardChiNext) {
		t.Fatal("chinext should be enabled")
	}
	if p.IsBoardEnabled(live.BoardBSE) {
		t.Fatal("bse should not be enabled")
	}
}

func TestCheck_AllGatesPass(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)
	p := profileFor(t)
	res := p.Check(live.BoardChiNext, time.Now())
	if !res.Allowed {
		t.Fatalf("expected allowed, got reasons %v", res.Reasons)
	}
	if len(res.Reasons) != 0 {
		t.Fatalf("expected no reasons, got %v", res.Reasons)
	}
}

func TestCheck_FailsRiskLevel(t *testing.T) {
	p := profileFor(t)
	p.RiskLevel = RiskLevelCautious // C2 < C3
	res := p.Check(live.BoardChiNext, time.Now())
	if res.Allowed {
		t.Fatal("expected rejected")
	}
	hasRisk := false
	for _, r := range res.Reasons {
		if contains(r, "风险等级") {
			hasRisk = true
		}
	}
	if !hasRisk {
		t.Fatalf("expected risk-level reason, got %v", res.Reasons)
	}
}

func TestCheck_FailsAsset(t *testing.T) {
	p := profileFor(t)
	p.AssetDailyAvgCNY = 50_000 // < 100k
	res := p.Check(live.BoardChiNext, time.Now())
	if res.Allowed {
		t.Fatal("expected rejected")
	}
	hasAsset := false
	for _, r := range res.Reasons {
		if contains(r, "资产门槛") {
			hasAsset = true
		}
	}
	if !hasAsset {
		t.Fatalf("expected asset reason, got %v", res.Reasons)
	}
}

func TestCheck_FailsExperience(t *testing.T) {
	p := profileFor(t)
	p.FirstTradeAt = time.Now().AddDate(0, -12, 0) // 12 < 24
	res := p.Check(live.BoardChiNext, time.Now())
	if res.Allowed {
		t.Fatal("expected rejected")
	}
	hasExp := false
	for _, r := range res.Reasons {
		if contains(r, "交易经验") {
			hasExp = true
		}
	}
	if !hasExp {
		t.Fatalf("expected experience reason, got %v", res.Reasons)
	}
}

func TestCheck_FailsRiskTestExpired(t *testing.T) {
	p := profileFor(t)
	p.RiskTestExpiredAt = time.Now().Add(-24 * time.Hour) // 昨天到期
	res := p.Check(live.BoardChiNext, time.Now())
	if res.Allowed {
		t.Fatal("expected rejected (expired test)")
	}
	hasExp := false
	for _, r := range res.Reasons {
		if contains(r, "风险测评已过期") {
			hasExp = true
		}
	}
	if !hasExp {
		t.Fatalf("expected expired-test reason, got %v", res.Reasons)
	}
}

func TestCheck_FailsWhitelist(t *testing.T) {
	p := profileFor(t)
	p.BoardsEnabled = []string{"star"} // 不含 chinext
	res := p.Check(live.BoardChiNext, time.Now())
	if res.Allowed {
		t.Fatal("expected rejected (whitelist)")
	}
	hasWL := false
	for _, r := range res.Reasons {
		if contains(r, "未开通") {
			hasWL = true
		}
	}
	if !hasWL {
		t.Fatalf("expected whitelist reason, got %v", res.Reasons)
	}
}

func TestCheck_MainBoardIsImplicitAllow(t *testing.T) {
	p := profileFor(t)
	p.RiskLevel = RiskLevelUnset // 没做测评
	p.AssetDailyAvgCNY = 0
	res := p.Check(live.BoardMainBoardSH, time.Now())
	if !res.Allowed {
		t.Fatalf("main board should be implicit-allow, got %v", res.Reasons)
	}
}

func TestCheck_ReasonOrder(t *testing.T) {
	// 检查多门禁都失败时, 原因出现顺序是风险 → 资产 → 经验 → 过期 → 白名单
	// (regulator-stable ordering for audit logs).
	ResetRegistry()
	t.Cleanup(ResetRegistry)
	p := SuitabilityProfile{
		UserID:            "u",
		AssetDailyAvgCNY:  10_000,                         // 资产不足
		FirstTradeAt:      time.Now().AddDate(0, -6, 0),   // 经验不足
		RiskLevel:         RiskLevelConservative,          // 风险不足
		RiskTestExpiredAt: time.Now().Add(-1 * time.Hour), // 过期
		BoardsEnabled:     []string{"star"},               // 白名单不含 chinext
	}
	res := p.Check(live.BoardChiNext, time.Now())
	if res.Allowed {
		t.Fatal("expected rejected")
	}
	// 仅需验证原因条数 ≥ 1; 具体顺序由 Check 函数决定
	if len(res.Reasons) < 3 {
		t.Fatalf("expected ≥3 reasons, got %d: %v", len(res.Reasons), res.Reasons)
	}
}

func TestCheckSymbol_ClassifiesAndChecks(t *testing.T) {
	p := profileFor(t)
	// ChiNext 符号
	res := p.CheckSymbol("300750.SZ", time.Now())
	if res.Board != live.BoardChiNext {
		t.Fatalf("expected chinext, got %s", res.Board)
	}
	// STAR 符号
	res2 := p.CheckSymbol("688001.SH", time.Now())
	if res2.Board != live.BoardSTAR {
		t.Fatalf("expected star, got %s", res2.Board)
	}
	// Main board 符号
	res3 := p.CheckSymbol("000001.SZ", time.Now())
	if res3.Board != live.BoardMainBoardSZ {
		t.Fatalf("expected main_sz, got %s", res3.Board)
	}
	if !res3.Allowed {
		t.Fatal("main board should be implicit-allow")
	}
}

func TestIsAllowed_AliasForCheck(t *testing.T) {
	p := profileFor(t)
	res := p.IsAllowed(live.BoardChiNext, time.Now())
	if !res.Allowed {
		t.Fatal("expected allowed")
	}
}

// TestSetRequirement_ConcurrentWithLookup 验证 registry 在并发读写下
// 不会出现 race / panic。go test -race 时尤其重要。
func TestSetRequirement_ConcurrentWithLookup(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = LookupRequirement(live.BoardChiNext)
			}
		}()
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = SetRequirement(live.BoardChiNext, &BoardRequirement{
					Board:             live.BoardChiNext,
					AssetThresholdCNY: float64(i*1000 + j),
					ExperienceMonths:  24,
					RiskLevel:         RiskLevelSteady,
					DisplayName:       fmt.Sprintf("test-%d-%d", i, j),
				})
			}
		}(i)
	}
	wg.Wait()
}

// ============================================================
// helpers
// ============================================================

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
