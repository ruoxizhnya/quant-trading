package fees

import (
	"strings"
	"testing"
	"time"
)

// TestDefaultAShareFees_RegulatoryValues pins the 4 numbers
// that the rest of the system assumes. If a regulator change
// requires updating them, this test forces the developer to
// acknowledge the change (e.g. the 2023-08 stamp tax cut from
// 0.1% to 0.05% required a code change + AGENTS.md update).
func TestDefaultAShareFees_RegulatoryValues(t *testing.T) {
	got := DefaultAShareFees()
	if got.CommissionRate != 0.0003 {
		t.Errorf("CommissionRate = %f, want 0.0003 (regulatory ceiling)", got.CommissionRate)
	}
	// AUD-06 (ODR-065): this assertion used to pin 0.001 and label it
	// "post 2023-08 cut" — the label was right but the value was the
	// pre-cut rate. The cut halved 0.1% to 0.05%, not 0.2% to 0.1%.
	if got.StampTaxRate != 0.0005 {
		t.Errorf("StampTaxRate = %f, want 0.0005 (0.1%% halved 2023-08-28)", got.StampTaxRate)
	}
	if got.TransferFeeRate != 0.00001 {
		t.Errorf("TransferFeeRate = %f, want 0.00001", got.TransferFeeRate)
	}
	if got.MinCommission != 5.0 {
		t.Errorf("MinCommission = %f, want 5.0", got.MinCommission)
	}
	if got.SlippageRate != 0.0001 {
		t.Errorf("SlippageRate = %f, want 0.0001", got.SlippageRate)
	}
}

func TestApplyDefaults_FillsZeroFields(t *testing.T) {
	cfg := AShareFees{} // every field zero
	cfg.ApplyDefaults()
	if cfg.CommissionRate == 0 || cfg.StampTaxRate == 0 ||
		cfg.TransferFeeRate == 0 || cfg.MinCommission == 0 ||
		cfg.SlippageRate == 0 {
		t.Errorf("ApplyDefaults must fill every zero field; got %+v", cfg)
	}
	// Should equal DefaultAShareFees() exactly.
	if cfg != DefaultAShareFees() {
		t.Errorf("zero + ApplyDefaults must equal DefaultAShareFees()\n got: %+v\nwant: %+v",
			cfg, DefaultAShareFees())
	}
}

func TestApplyDefaults_PreservesOverrides(t *testing.T) {
	cfg := AShareFees{CommissionRate: 0.0001, StampTaxRate: 0.0005}
	cfg.ApplyDefaults()
	if cfg.CommissionRate != 0.0001 {
		t.Errorf("explicit CommissionRate must not be overwritten; got %f", cfg.CommissionRate)
	}
	if cfg.StampTaxRate != 0.0005 {
		t.Errorf("explicit StampTaxRate must not be overwritten; got %f", cfg.StampTaxRate)
	}
	// Unset fields still get defaults.
	if cfg.TransferFeeRate != DefaultTransferFeeRate {
		t.Errorf("TransferFeeRate must default; got %f", cfg.TransferFeeRate)
	}
}

func TestApplyDefaults_OnlyFillsTrueZeros(t *testing.T) {
	// ApplyDefaults called twice must be idempotent: the
	// second call sees no zero fields and changes nothing.
	cfg := DefaultAShareFees()
	first := cfg
	cfg.ApplyDefaults()
	if cfg != first {
		t.Errorf("ApplyDefaults on a fully-populated config must be a no-op\n got: %+v\norig: %+v",
			cfg, first)
	}
}

func TestValidate_AcceptsDefaults(t *testing.T) {
	if err := DefaultAShareFees().Validate(); err != nil {
		t.Errorf("DefaultAShareFees() must validate; got %v", err)
	}
}

func TestValidate_RejectsOutOfRange(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*AShareFees)
		want string
	}{
		{"negative commission", func(f *AShareFees) { f.CommissionRate = -0.001 }, "CommissionRate"},
		{"100% commission", func(f *AShareFees) { f.CommissionRate = 1.0 }, "CommissionRate"},
		{"negative stamp", func(f *AShareFees) { f.StampTaxRate = -0.01 }, "StampTaxRate"},
		{"huge stamp", func(f *AShareFees) { f.StampTaxRate = 0.5 }, "StampTaxRate"},
		{"huge transfer", func(f *AShareFees) { f.TransferFeeRate = 0.5 }, "TransferFeeRate"},
		{"negative transfer", func(f *AShareFees) { f.TransferFeeRate = -1 }, "TransferFeeRate"},
		{"huge min commission", func(f *AShareFees) { f.MinCommission = 9999 }, "MinCommission"},
		{"negative min commission", func(f *AShareFees) { f.MinCommission = -5 }, "MinCommission"},
		{"huge slippage", func(f *AShareFees) { f.SlippageRate = 0.5 }, "SlippageRate"},
		{"negative slippage", func(f *AShareFees) { f.SlippageRate = -0.001 }, "SlippageRate"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := DefaultAShareFees()
			c.mut(&f)
			err := f.Validate()
			if err == nil {
				t.Fatalf("expected error for %s, got nil", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error must name the bad field; want %q in %q", c.want, err.Error())
			}
		})
	}
}

func TestValidate_AcceptsBoundary(t *testing.T) {
	// Edge cases that ARE valid:
	//   - 0% commission (免佣 — some VIP clients)
	//   - 0% stamp (impossible today but the validator should
	//     not block it; that's a business policy decision, not
	//     a data error)
	//   - 0 slippage (deterministic test config)
	cases := []AShareFees{
		DefaultAShareFees(), // regulatory default
		{CommissionRate: 0, MinCommission: 0, SlippageRate: 0}, // zeroed is ok in range
		{CommissionRate: 0.0001, StampTaxRate: 0, TransferFeeRate: 0,
			MinCommission: 0, SlippageRate: 0.0001}, // mixed
	}
	for i, f := range cases {
		if err := f.Validate(); err != nil {
			t.Errorf("case %d must validate; got %v (cfg=%+v)", i, err, f)
		}
	}
}

// TestConstantsAreWiredToAShareFees confirms the
// `Default*` constants and `DefaultAShareFees()` agree
// bit-for-bit. This is the contract that lets code in
// pkg/backtest/execution.go safely write
// `fees.DefaultCommissionRate` instead of re-declaring
// the literal 0.0003.
// TestStampTaxRate_HistoricalTimeline encodes the rate history so that
// a future change to the constant has to confront the direction of the
// 2023 cut, not just its magnitude.
//
// AUD-06 (ODR-065): the bug being guarded was not a typo but a
// misunderstanding — the comment said "halved from 0.2% to 0.1%" while
// the true history is "halved from 0.1% to 0.05%". Both the before and
// after values were wrong by 2x, in the same direction, which made the
// error self-consistent and easy to miss. Spelling the timeline out
// here makes the next reader check the anchor point (0.1% from
// 2008-09-19) rather than trusting the comment.
func TestStampTaxRate_HistoricalTimeline(t *testing.T) {
	// Anchor: 2008-09-19 the rate became 0.1%, sell-side only
	// (previously 0.3% bilateral, then 0.1% bilateral).
	//
	// AUD-20: this used to be a local `const pre2023CutRate = 0.001`,
	// which meant the historical rate was pinned in two independent
	// places (here and DefaultStampTaxRateBefore) and could drift. Now
	// the constant is the single source and this test asserts the link.
	const pre2023CutRate = DefaultStampTaxRateBefore
	if pre2023CutRate != 0.001 {
		t.Errorf("DefaultStampTaxRateBefore = %f, want 0.001 (0.1%% before 2023-08-28)", pre2023CutRate)
	}
	// 2023-08-28: 财政部/税务总局公告 2023 年第 39 号 halved it.
	const post2023CutRate = 0.0005

	if DefaultStampTaxRate != post2023CutRate {
		t.Errorf("DefaultStampTaxRate = %f, want %f (post 2023-08-28)",
			DefaultStampTaxRate, post2023CutRate)
	}
	if DefaultStampTaxRate != pre2023CutRate/2 {
		t.Errorf("the 2023-08-28 change was a HALVING of %f, so the new rate must be %f; got %f",
			pre2023CutRate, pre2023CutRate/2, DefaultStampTaxRate)
	}

	// The acceptance criterion from the audit: 100,000 CNY sold => 50 CNY.
	const sellValue = 100_000.0
	if got := sellValue * DefaultStampTaxRate; got != 50.0 {
		t.Errorf("selling %.0f CNY must incur 50 CNY stamp tax; got %.2f", sellValue, got)
	}
}

// TestStampTaxRateFor_DateSegmented is the AUD-20 guardrail for the
// resolver itself.
//
// The defect it guards: DefaultStampTaxRate is a POINT-IN-TIME value, so
// using it for a whole backtest window charges the post-cut 0.05% to
// pre-cut sells — understating cost and overstating return. The boundary
// cases matter as much as the middle ones: the cut took effect ON
// 2023-08-28, so a trade dated that day pays the NEW rate, and a zero asOf
// must resolve to current rules rather than silently picking the
// historical rate.
func TestStampTaxRateFor_DateSegmented(t *testing.T) {
	cut := StampTaxCutDate
	if cut != time.Date(2023, 8, 28, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("StampTaxCutDate = %s, want 2023-08-28 (公告 2023 年第 39 号)",
			cut.Format("2006-01-02"))
	}

	cases := []struct {
		name string
		asOf time.Time
		want float64
	}{
		{"day before the cut", cut.AddDate(0, 0, -1), DefaultStampTaxRateBefore},
		{"cut day itself takes the NEW rate", cut, DefaultStampTaxRate},
		{"day after the cut", cut.AddDate(0, 0, 1), DefaultStampTaxRate},
		{"2015 (pre-cut)", time.Date(2015, 6, 1, 0, 0, 0, 0, time.UTC), DefaultStampTaxRateBefore},
		{"2026 (post-cut)", time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC), DefaultStampTaxRate},
		{"zero asOf -> current rules, never a silent historical rate", time.Time{}, DefaultStampTaxRate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StampTaxRateFor(tc.asOf, DefaultStampTaxRate, DefaultStampTaxRateBefore)
			if got != tc.want {
				t.Fatalf("StampTaxRateFor(%s) = %f, want %f",
					tc.asOf.Format(time.RFC3339), got, tc.want)
			}
		})
	}
}

// TestStampTaxRateFor_ZeroAndNegativeFallBack pins the fallback policy: a
// caller that supplies only one side of the pair (or neither) gets the
// package constants rather than a free sell. A NON-POSITIVE override falls
// back too — a negative stamp tax would be a credit, which is never what a
// caller means, so it must not be honoured.
//
// Policy matches resolvePriceLimit's handling of an unset STBefore in
// pkg/backtest/pricelimit.go, deliberately: the same shape of bug should
// have the same shape of answer.
func TestStampTaxRateFor_ZeroAndNegativeFallBack(t *testing.T) {
	pre := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	post := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)

	checks := []struct {
		name                  string
		asOf                  time.Time
		current, before, want float64
	}{
		{"both zero, pre-cut", pre, 0, 0, DefaultStampTaxRateBefore},
		{"both zero, post-cut", post, 0, 0, DefaultStampTaxRate},
		{"before unset, pre-cut", pre, DefaultStampTaxRate, 0, DefaultStampTaxRateBefore},
		{"current unset, post-cut", post, 0, DefaultStampTaxRateBefore, DefaultStampTaxRate},
		{"negative before, pre-cut", pre, DefaultStampTaxRate, -0.001, DefaultStampTaxRateBefore},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if got := StampTaxRateFor(c.asOf, c.current, c.before); got != c.want {
				t.Fatalf("StampTaxRateFor(%s, %f, %f) = %f, want %f",
					c.asOf.Format("2006-01-02"), c.current, c.before, got, c.want)
			}
		})
	}
}

// TestConstantsAreWiredToAShareFees (existing) verifies the re-export
// chain — kept separate from the value assertions above so a wiring
// break and a value regression are distinguishable.
func TestConstantsAreWiredToAShareFees(t *testing.T) {
	d := DefaultAShareFees()
	if d.CommissionRate != DefaultCommissionRate {
		t.Errorf("DefaultAShareFees.CommissionRate (%f) != DefaultCommissionRate (%f)",
			d.CommissionRate, DefaultCommissionRate)
	}
	if d.StampTaxRate != DefaultStampTaxRate {
		t.Errorf("DefaultAShareFees.StampTaxRate (%f) != DefaultStampTaxRate (%f)",
			d.StampTaxRate, DefaultStampTaxRate)
	}
	if d.TransferFeeRate != DefaultTransferFeeRate {
		t.Errorf("DefaultAShareFees.TransferFeeRate (%f) != DefaultTransferFeeRate (%f)",
			d.TransferFeeRate, DefaultTransferFeeRate)
	}
	if d.MinCommission != DefaultMinCommission {
		t.Errorf("DefaultAShareFees.MinCommission (%f) != DefaultMinCommission (%f)",
			d.MinCommission, DefaultMinCommission)
	}
	if d.SlippageRate != DefaultSlippageRate {
		t.Errorf("DefaultAShareFees.SlippageRate (%f) != DefaultSlippageRate (%f)",
			d.SlippageRate, DefaultSlippageRate)
	}
}
