package fees

import (
	"strings"
	"testing"
)

// TestDefaultAShareFees_RegulatoryValues pins the 4 numbers
// that the rest of the system assumes. If a regulator change
// requires updating them, this test forces the developer to
// acknowledge the change (e.g. the 2023-08 stamp tax cut from
// 0.2% to 0.1% required a code change + AGENTS.md update).
func TestDefaultAShareFees_RegulatoryValues(t *testing.T) {
	got := DefaultAShareFees()
	if got.CommissionRate != 0.0003 {
		t.Errorf("CommissionRate = %f, want 0.0003 (regulatory ceiling)", got.CommissionRate)
	}
	if got.StampTaxRate != 0.001 {
		t.Errorf("StampTaxRate = %f, want 0.001 (post 2023-08 cut)", got.StampTaxRate)
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
