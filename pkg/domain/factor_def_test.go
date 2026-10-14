package domain

import (
	"strings"
	"testing"
)

func validFormulaDef() FactorDefinition {
	return FactorDefinition{
		Name:          FactorMomentum,
		Kernel:        KernelFormula,
		Formula:       "close / ts_delay(close, 20) - 1",
		Caliber:       CaliberZScore,
		InputFields:   []string{"close"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
		Lookback:      30,
	}
}

// ─── 版本哈希纪律（蓝图 §5.2 三条铁律之二：定义带版本指纹） ───

func TestFactorDefinition_VersionHash_StableForSameContent(t *testing.T) {
	d := validFormulaDef()
	if got := d.VersionHash(); got != d.VersionHash() {
		t.Fatalf("VersionHash not deterministic: %s vs %s", got, d.VersionHash())
	}
	// 形态自检：SHA-256 hex = 64 字符
	if len(d.VersionHash()) != 64 {
		t.Fatalf("VersionHash should be 64 hex chars, got %d", len(d.VersionHash()))
	}
}

func TestFactorDefinition_VersionHash_InputFieldOrderIndependent(t *testing.T) {
	a := validFormulaDef()
	a.InputFields = []string{"close", "high", "low"}
	b := a
	b.InputFields = []string{"low", "close", "high"} // 同集合，不同顺序
	if a.VersionHash() != b.VersionHash() {
		t.Fatalf("VersionHash must not depend on input_fields order:\n a=%s\n b=%s",
			a.VersionHash(), b.VersionHash())
	}
}

func TestFactorDefinition_VersionHash_ChangesWithEverySkeletonField(t *testing.T) {
	base := validFormulaDef()
	mutations := map[string]func(d *FactorDefinition){
		"name":         func(d *FactorDefinition) { d.Name = FactorValue },
		"kernel":       func(d *FactorDefinition) { d.Kernel = KernelLeaf },
		"formula":      func(d *FactorDefinition) { d.Formula = "close / ts_delay(close, 10) - 1" },
		"code_ref":     func(d *FactorDefinition) { d.CodeRef = "SomeKernel" },
		"caliber":      func(d *FactorDefinition) { d.Caliber = CaliberRank },
		"input_fields": func(d *FactorDefinition) { d.InputFields = []string{"close", "high"} },
		"effective":    func(d *FactorDefinition) { d.EffectiveFrom = "2026-10-15" },
		"lookahead":    func(d *FactorDefinition) { d.Lookahead = "ann_date<=trade_date" },
		"lookback":     func(d *FactorDefinition) { d.Lookback = 60 },
		"validity":     func(d *FactorDefinition) { d.Validity = "close > 0" },
	}
	for field, mutate := range mutations {
		d := base
		mutate(&d)
		if d.VersionHash() == base.VersionHash() {
			t.Errorf("mutating %s did not change VersionHash — the fingerprint is not a fingerprint", field)
		}
	}
}

// ─── fail-closed 校验（D3 ⑤1：未登记/非法定义必须显式报错） ───

func TestFactorDefinition_Validate_FailClosed(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(d *FactorDefinition)
		wantErr string // 期望错误信息包含的子串
	}{
		{
			name:    "未登记因子名",
			mutate:  func(d *FactorDefinition) { d.Name = "not_a_factor" },
			wantErr: "not in frozen registry",
		},
		{
			name:    "未知内核型",
			mutate:  func(d *FactorDefinition) { d.Kernel = "quantum" },
			wantErr: "unknown kernel",
		},
		{
			name:    "未知口径",
			mutate:  func(d *FactorDefinition) { d.Caliber = "vibes" },
			wantErr: "unknown caliber",
		},
		{
			name:    "缺前视纪律声明",
			mutate:  func(d *FactorDefinition) { d.Lookahead = "" },
			wantErr: "lookahead rule is mandatory",
		},
		{
			name:    "生效日格式错",
			mutate:  func(d *FactorDefinition) { d.EffectiveFrom = "14/10/2026" },
			wantErr: "effective_from must be YYYY-MM-DD",
		},
		{
			name:    "公式型缺公式",
			mutate:  func(d *FactorDefinition) { d.Formula = "" },
			wantErr: "requires formula",
		},
		{
			name:    "公式型带 code_ref",
			mutate:  func(d *FactorDefinition) { d.CodeRef = "SomeKernel" },
			wantErr: "must not carry code_ref",
		},
		{
			name: "代码型缺 code_ref",
			mutate: func(d *FactorDefinition) {
				d.Kernel = KernelCode
				d.Formula = ""
			},
			wantErr: "requires code_ref",
		},
		{
			name: "代码型带公式",
			mutate: func(d *FactorDefinition) {
				d.Kernel = KernelCode
				d.CodeRef = "SomeKernel"
			},
			wantErr: "must not carry formula",
		},
		{
			name:    "空输入字段清单",
			mutate:  func(d *FactorDefinition) { d.InputFields = nil },
			wantErr: "at least one canonical field",
		},
		{
			name:    "空字段名",
			mutate:  func(d *FactorDefinition) { d.InputFields = []string{"close", ""} },
			wantErr: "empty field name",
		},
		{
			name:    "重复输入字段",
			mutate:  func(d *FactorDefinition) { d.InputFields = []string{"close", "close"} },
			wantErr: "duplicate input field",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := validFormulaDef()
			tc.mutate(&d)
			err := d.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestFactorDefinition_Validate_CodeKernelOK(t *testing.T) {
	d := FactorDefinition{
		Name:          FactorGrossMarginTrend,
		Kernel:        KernelCode,
		CodeRef:       "ComputeGrossMarginTrendFactor",
		Caliber:       CaliberZScore,
		InputFields:   []string{"total_revenue", "operating_cost"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "ann_date<=trade_date",
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("code-kernel definition should validate: %v", err)
	}
}

func TestFactorDefinition_Validate_PendingKernel(t *testing.T) {
	// pending 态：名册在册、内核待定 —— 无公式无出口无输入字段，但骨架
	// 字段（口径/前视/生效日）仍必填。
	ok := FactorDefinition{
		Name:          FactorSize,
		Kernel:        KernelPending,
		Caliber:       CaliberZScore,
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("pending definition should validate: %v", err)
	}

	withFormula := ok
	withFormula.Formula = "1 / total_market_cap"
	if err := withFormula.Validate(); err == nil {
		t.Fatal("pending kernel carrying a formula must fail closed")
	}

	withInputs := ok
	withInputs.InputFields = []string{"total_market_cap"}
	if err := withInputs.Validate(); err == nil {
		t.Fatal("pending kernel declaring input fields must fail closed")
	}

	missingLookahead := ok
	missingLookahead.Lookahead = ""
	if err := missingLookahead.Validate(); err == nil {
		t.Fatal("pending definition without lookahead must fail closed")
	}
}
