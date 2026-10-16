package data

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/data/equitydeep"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ─── D3 ⑤1：注册表库管理 + fail-closed ───

func TestNewRegistry_BuiltinSeedsAllValid(t *testing.T) {
	r, err := NewRegistry(domain.BuiltinFactorDefinitions)
	if err != nil {
		t.Fatalf("builtin seed definitions must load: %v", err)
	}
	// D5（2026-10-15）名册协议变更新增 market_activity，种子数 11 → 12。
	if got := len(r.Names()); got != 12 {
		t.Fatalf("registry should hold all 12 frozen factors, got %d: %v", got, r.Names())
	}
	// 与冻结名册（ParseFactorType）逐一对照：库管理不丢名册里的任何一个
	roster := []domain.FactorType{
		domain.FactorMomentum, domain.FactorValue, domain.FactorQuality,
		domain.FactorSize, domain.FactorVolatility, domain.FactorGrowth,
		domain.FactorGrossMarginTrend, domain.FactorContractLiabilityRatio,
		domain.FactorOCFToNetProfit, domain.FactorROEDuPontLeverage,
		domain.FactorInventoryTurnoverDelta,
		domain.FactorMarketActivity, // D5 新增（daily_basic 活跃度）
	}
	for _, name := range roster {
		if _, ok := r.Definition(name); !ok {
			t.Errorf("factor %q missing from registry", name)
		}
	}
}

func TestNewRegistry_KernelSplit(t *testing.T) {
	r, err := NewRegistry(domain.BuiltinFactorDefinitions)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := map[domain.FactorType]domain.FactorKernel{
		domain.FactorMomentum:               domain.KernelFormula,
		domain.FactorValue:                  domain.KernelFormula,
		domain.FactorQuality:                domain.KernelLeaf,
		domain.FactorGrossMarginTrend:       domain.KernelCode,
		domain.FactorContractLiabilityRatio: domain.KernelCode,
		domain.FactorOCFToNetProfit:         domain.KernelCode,
		domain.FactorROEDuPontLeverage:      domain.KernelCode,
		domain.FactorInventoryTurnoverDelta: domain.KernelCode,
		domain.FactorSize:                   domain.KernelPending,
		domain.FactorVolatility:             domain.KernelPending,
		domain.FactorGrowth:                 domain.KernelPending,
	}
	for name, kernel := range want {
		d, ok := r.Definition(name)
		if !ok {
			t.Fatalf("factor %q missing", name)
		}
		if d.Kernel != kernel {
			t.Errorf("factor %q: kernel = %s, want %s", name, d.Kernel, kernel)
		}
	}
}

// ─── D3 ⑤2②：公式引用不存在的输入字段 → 加载期 fail-closed ───

func TestNewRegistry_FailClosed_FormulaUsesUndeclaredField(t *testing.T) {
	bad := domain.FactorDefinition{
		Name:          domain.FactorMomentum,
		Kernel:        domain.KernelFormula,
		Formula:       "close / volume", // volume 用了没声明
		Caliber:       domain.CaliberZScore,
		InputFields:   []string{"close"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
	}
	_, err := NewRegistry([]domain.FactorDefinition{bad})
	if err == nil {
		t.Fatal("formula using an undeclared field must fail closed")
	}
	if !strings.Contains(err.Error(), "not declared in input_fields") {
		t.Fatalf("error should name the undeclared field, got: %v", err)
	}
}

func TestNewRegistry_FailClosed_DeclaredFieldUnused(t *testing.T) {
	bad := domain.FactorDefinition{
		Name:          domain.FactorMomentum,
		Kernel:        domain.KernelFormula,
		Formula:       "close / ts_delay(close, 20) - 1",
		Caliber:       domain.CaliberZScore,
		InputFields:   []string{"close", "volume"}, // volume 声明了没用
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
	}
	_, err := NewRegistry([]domain.FactorDefinition{bad})
	if err == nil {
		t.Fatal("declared-but-unused input field must fail closed (dead declaration)")
	}
	if !strings.Contains(err.Error(), "never uses it") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewRegistry_FailClosed_FieldNotInCanonicalRegistry(t *testing.T) {
	bad := domain.FactorDefinition{
		Name:          domain.FactorMomentum,
		Kernel:        domain.KernelLeaf,
		Formula:       "total_market_cap", // 规范层字段词表里没有
		Caliber:       domain.CaliberZScore,
		InputFields:   []string{"total_market_cap"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
	}
	_, err := NewRegistry([]domain.FactorDefinition{bad})
	if err == nil {
		t.Fatal("field outside the canonical registry must fail closed")
	}
	// 引擎 L1 闸门（白名单=规范层字段词表）会先拦下未知字段并自带可用
	// 清单；registry 的第三层校验（InputFields ⊆ AvailableFields）是同
	// 源冗余防御。两者任一生效即 fail-closed。
	if !strings.Contains(err.Error(), "unknown data field") && !strings.Contains(err.Error(), "not in canonical field registry") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewRegistry_FailClosed_UnparseableFormula(t *testing.T) {
	bad := domain.FactorDefinition{
		Name:          domain.FactorMomentum,
		Kernel:        domain.KernelFormula,
		Formula:       "close / ts_delay(close,", // 语法残缺
		Caliber:       domain.CaliberZScore,
		InputFields:   []string{"close"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
	}
	if _, err := NewRegistry([]domain.FactorDefinition{bad}); err == nil {
		t.Fatal("unparseable formula must fail closed at load")
	}
}

func TestNewRegistry_FailClosed_UnboundCodeRef(t *testing.T) {
	bad := domain.FactorDefinition{
		Name:          domain.FactorGrossMarginTrend,
		Kernel:        domain.KernelCode,
		CodeRef:       "ComputeNoSuchFactor",
		Caliber:       domain.CaliberZScore,
		InputFields:   []string{"total_revenue", "operating_cost"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "ann_date<=trade_date",
	}
	_, err := NewRegistry([]domain.FactorDefinition{bad})
	if err == nil {
		t.Fatal("code_ref without a Go kernel must fail closed")
	}
	if !strings.Contains(err.Error(), "no Go kernel registered") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewRegistry_FailClosed_DuplicateName(t *testing.T) {
	_, err := NewRegistry([]domain.FactorDefinition{domain.BuiltinFactorDefinitions[0], domain.BuiltinFactorDefinitions[0]})
	if err == nil || !strings.Contains(err.Error(), "duplicate definition") {
		t.Fatalf("duplicate names must fail closed, got: %v", err)
	}
}

// ─── 按名取因子的 fail-closed 报错（继承「报错带可用清单」语义） ───

func TestRegistry_FailClosedError(t *testing.T) {
	r, err := NewRegistry(domain.BuiltinFactorDefinitions)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// 未登记 → ErrFactorNotRegistered，且带可用清单
	err = r.FailClosedError("not_a_factor")
	if !errors.Is(err, ErrFactorNotRegistered) {
		t.Fatalf("unregistered name should map to ErrFactorNotRegistered, got: %v", err)
	}
	if !strings.Contains(err.Error(), "momentum") {
		t.Fatalf("error should carry the available roster, got: %v", err)
	}
	// pending → ErrFactorKernelUndefined（与未登记区分）
	for _, name := range []domain.FactorType{domain.FactorSize, domain.FactorVolatility, domain.FactorGrowth} {
		err = r.FailClosedError(name)
		if !errors.Is(err, ErrFactorKernelUndefined) {
			t.Fatalf("pending factor %q should map to ErrFactorKernelUndefined, got: %v", name, err)
		}
	}
	// 已登记且有内核 → nil
	for _, name := range []domain.FactorType{domain.FactorMomentum, domain.FactorValue, domain.FactorQuality, domain.FactorGrossMarginTrend} {
		if err := r.FailClosedError(name); err != nil {
			t.Fatalf("factor %q should not fail closed: %v", name, err)
		}
	}
}

func TestRegistry_CodeKernel_FailClosed(t *testing.T) {
	r, err := NewRegistry(domain.BuiltinFactorDefinitions)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fc := NewFactorComputer(nil)
	// 公式型没有 Go 出口
	if _, err := r.CodeKernel(fc, domain.FactorMomentum); err == nil {
		t.Fatal("formula kernel must not expose a Go code kernel")
	}
	// pending 没有 Go 出口
	if _, err := r.CodeKernel(fc, domain.FactorSize); err == nil {
		t.Fatal("pending kernel must not expose a Go code kernel")
	}
	// 代码型有出口
	if _, err := r.CodeKernel(fc, domain.FactorGrossMarginTrend); err != nil {
		t.Fatalf("code kernel should bind: %v", err)
	}
	// 未登记
	if _, err := r.CodeKernel(fc, "not_a_factor"); !errors.Is(err, ErrFactorNotRegistered) {
		t.Fatalf("unregistered name should fail closed, got: %v", err)
	}
}

// ─── D3 ⑤3：版本复现 —— 同定义同哈希；改公式哈希变 ───

func TestRegistry_VersionHash_Reproducible(t *testing.T) {
	r1, err := NewRegistry(domain.BuiltinFactorDefinitions)
	if err != nil {
		t.Fatalf("load r1: %v", err)
	}
	r2, err := NewRegistry(domain.BuiltinFactorDefinitions)
	if err != nil {
		t.Fatalf("load r2: %v", err)
	}
	for _, name := range r1.Names() {
		if r1.VersionHash(name) != r2.VersionHash(name) {
			t.Errorf("factor %q: same seed must give same version hash across registry instances", name)
		}
		if h := r1.VersionHash(name); len(h) != 64 {
			t.Errorf("factor %q: version hash should be 64 hex chars, got %q", name, h)
		}
	}
}

func TestRegistry_VersionHash_FormulaChangeInvalidates(t *testing.T) {
	base, err := NewRegistry(domain.BuiltinFactorDefinitions)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	mutated := make([]domain.FactorDefinition, len(domain.BuiltinFactorDefinitions))
	copy(mutated, domain.BuiltinFactorDefinitions)
	// 改 momentum 公式：20 日 → 10 日。数值口径不变（同为动量），但定义
	// 内容变了 → 版本指纹必须变，否则「同哈希同定义」的复现承诺是空话。
	mutated[0].Formula = "close / ts_delay(close, 10) - 1"
	changed, err := NewRegistry(mutated)
	if err != nil {
		t.Fatalf("load mutated: %v", err)
	}
	if base.VersionHash(domain.FactorMomentum) == changed.VersionHash(domain.FactorMomentum) {
		t.Fatal("changing the formula must change the version hash")
	}
	// 未改的因子哈希不动
	if base.VersionHash(domain.FactorQuality) != changed.VersionHash(domain.FactorQuality) {
		t.Fatal("untouched factors must keep their version hash")
	}
}

// 种子定义的财报字段码是字面量（domain 不能 import equitydeep——后者反向
// 依赖 domain）。这里把字面量与 equitydeep 常量逐一对齐：契约字典改了
// 常量值，本测试即红，种子定义不许静默漂移。
func TestBuiltinSeeds_FieldCodesMatchEquitydeepConstants(t *testing.T) {
	want := map[string][]string{
		string(domain.FactorGrossMarginTrend):       {equitydeep.FieldTotalRevenue, equitydeep.FieldOperatingCost},
		string(domain.FactorContractLiabilityRatio): {equitydeep.FieldContractLiability, equitydeep.FieldTotalRevenue},
		string(domain.FactorOCFToNetProfit):         {equitydeep.FieldOCFNet, equitydeep.FieldNetProfitAttr},
		string(domain.FactorROEDuPontLeverage):      {equitydeep.FieldTotalAssets, equitydeep.FieldEquityAttr},
		string(domain.FactorInventoryTurnoverDelta): {equitydeep.FieldInventory, equitydeep.FieldOperatingCost},
	}
	for _, d := range domain.BuiltinFactorDefinitions {
		w, ok := want[string(d.Name)]
		if !ok {
			continue
		}
		w, got := append([]string(nil), w...), append([]string(nil), d.InputFields...)
		sort.Strings(w)
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(w, ",") {
			t.Errorf("factor %q: input fields %v, want %v (equitydeep contract constants)", d.Name, got, w)
		}
	}
}
