package plugins

import (
	"strings"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

func TestValidateIntRange(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		min      int
		max      int
		expected bool
	}{
		{"valid middle", 50, 1, 100, true},
		{"valid min", 1, 1, 100, true},
		{"valid max", 100, 1, 100, true},
		{"too small", 0, 1, 100, false},
		{"too large", 101, 1, 100, false},
		{"negative", -5, 0, 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateIntRange("test_field", tt.value, tt.min, tt.max)
			if result.Valid != tt.expected {
				t.Errorf("validateIntRange(%d, %d, %d) expected valid=%v, got valid=%v, message=%s",
					tt.value, tt.min, tt.max, tt.expected, result.Valid, result.Message)
			}
		})
	}
}

func TestValidateFloatRange(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		min      float64
		max      float64
		expected bool
	}{
		{"valid middle", 0.5, 0.0, 1.0, true},
		{"valid min", 0.0, 0.0, 1.0, true},
		{"valid max", 1.0, 0.0, 1.0, true},
		{"too small", -0.1, 0.0, 1.0, false},
		{"too large", 1.1, 0.0, 1.0, false},
		{"negative range", -0.5, -1.0, 0.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateFloatRange("test_field", tt.value, tt.min, tt.max)
			if result.Valid != tt.expected {
				t.Errorf("validateFloatRange(%f, %f, %f) expected valid=%v, got valid=%v, message=%s",
					tt.value, tt.min, tt.max, tt.expected, result.Valid, result.Message)
			}
		})
	}
}

func TestValidateStringChoice(t *testing.T) {
	choices := []string{"daily", "weekly", "monthly"}

	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{"valid daily", "daily", true},
		{"valid weekly", "weekly", true},
		{"valid monthly", "monthly", true},
		{"invalid", "yearly", false},
		{"empty", "", false},
		{"case sensitive", "Daily", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateStringChoice("test_field", tt.value, choices)
			if result.Valid != tt.expected {
				t.Errorf("validateStringChoice(%s) expected valid=%v, got valid=%v, message=%s",
					tt.value, tt.expected, result.Valid, result.Message)
			}
		})
	}
}

func TestMomentumStrategy_ConfigureValidation(t *testing.T) {
	s := &momentumStrategy{BaseStrategy: strategy.NewBaseStrategy("momentum", "test")}

	// Test invalid lookback_days
	err := s.Configure(map[string]any{"lookback_days": 0})
	if err == nil {
		t.Error("expected error for lookback_days=0")
	}

	err = s.Configure(map[string]any{"lookback_days": 253})
	if err == nil {
		t.Error("expected error for lookback_days=253")
	}

	// Test invalid top_n
	err = s.Configure(map[string]any{"top_n": 0})
	if err == nil {
		t.Error("expected error for top_n=0")
	}

	err = s.Configure(map[string]any{"top_n": 101})
	if err == nil {
		t.Error("expected error for top_n=101")
	}

	// Test invalid rebalance_frequency
	err = s.Configure(map[string]any{"rebalance_frequency": "yearly"})
	if err == nil {
		t.Error("expected error for rebalance_frequency=yearly")
	}

	// Test valid parameters
	err = s.Configure(map[string]any{
		"lookback_days":       20,
		"top_n":               5,
		"rebalance_frequency": "weekly",
	})
	if err != nil {
		t.Errorf("expected no error for valid params, got: %v", err)
	}
}

func TestValueStrategy_ConfigureValidation(t *testing.T) {
	s := &valueStrategy{BaseStrategy: strategy.NewBaseStrategy("value", "test")}

	// Test invalid pe_max
	err := s.Configure(map[string]any{"pe_max": 0.5})
	if err == nil {
		t.Error("expected error for pe_max=0.5")
	}

	// Test valid parameters
	err = s.Configure(map[string]any{
		"lookback_days": 20,
		"top_n":         5,
		"pe_max":        30.0,
	})
	if err != nil {
		t.Errorf("expected no error for valid params, got: %v", err)
	}
}

func TestMeanReversionStrategy_ConfigureValidation(t *testing.T) {
	s := &meanReversionStrategy{BaseStrategy: strategy.NewBaseStrategy("mean_reversion", "test")}

	// Test invalid ma_period
	err := s.Configure(map[string]any{"ma_period": 0})
	if err == nil {
		t.Error("expected error for ma_period=0")
	}

	// Test invalid buy_threshold_pct (should be negative)
	err = s.Configure(map[string]any{"buy_threshold_pct": 5.0})
	if err == nil {
		t.Error("expected error for buy_threshold_pct=5.0")
	}

	// Test invalid sell_threshold_pct (should be positive)
	err = s.Configure(map[string]any{"sell_threshold_pct": -5.0})
	if err == nil {
		t.Error("expected error for sell_threshold_pct=-5.0")
	}

	// Test valid parameters
	err = s.Configure(map[string]any{
		"ma_period":           20,
		"buy_threshold_pct":   -3.0,
		"sell_threshold_pct":  3.0,
	})
	if err != nil {
		t.Errorf("expected no error for valid params, got: %v", err)
	}
}

// TestValidateIntRange_Bounds covers the standard-library-backed
// message formatting. The original itoa was a 20-line hand-rolled
// routine; P1-27 (ODR-013) replaced it with strconv.Itoa. These
// tests pin the user-visible behavior of the replacement: a
// value outside [min, max] produces a Message that contains the
// literal min/max as decimal text.
func TestValidateIntRange_Bounds(t *testing.T) {
	r := validateIntRange("lookback", 0, 5, 50)
	if r.Valid {
		t.Fatalf("expected invalid; got %+v", r)
	}
	if !strings.Contains(r.Message, ">= 5") || !strings.Contains(r.Message, "lookback") {
		t.Errorf("message must name the field and the bound; got %q", r.Message)
	}

	r = validateIntRange("lookback", 99, 5, 50)
	if r.Valid {
		t.Fatalf("expected invalid; got %+v", r)
	}
	if !strings.Contains(r.Message, "<= 50") {
		t.Errorf("message must name the upper bound; got %q", r.Message)
	}

	if !validateIntRange("lookback", 20, 5, 50).Valid {
		t.Errorf("value within [min,max] must be Valid")
	}
}

// TestValidateFloatRange_Bounds covers the strconv.FormatFloat
// replacement for the hand-rolled ftoa. We pin the exact 2-decimal
// formatting (P1-27 keeps the original precision contract).
func TestValidateFloatRange_Bounds(t *testing.T) {
	r := validateFloatRange("alpha", -0.1, 0.0, 1.0)
	if r.Valid {
		t.Fatalf("expected invalid; got %+v", r)
	}
	if !strings.Contains(r.Message, ">= 0.00") {
		t.Errorf("lower bound must be formatted as '0.00'; got %q", r.Message)
	}

	r = validateFloatRange("alpha", 1.5, 0.0, 1.0)
	if r.Valid {
		t.Fatalf("expected invalid; got %+v", r)
	}
	if !strings.Contains(r.Message, "<= 1.00") {
		t.Errorf("upper bound must be formatted as '1.00'; got %q", r.Message)
	}

	if !validateFloatRange("alpha", 0.5, 0.0, 1.0).Valid {
		t.Errorf("value within [min,max] must be Valid")
	}
}

// TestValidateStringChoice_JoinFormat pins the strings.Join
// replacement for the hand-rolled joinStrings. The separator is
// ", " (comma + space) and an empty input must not panic.
func TestValidateStringChoice_JoinFormat(t *testing.T) {
	r := validateStringChoice("side", "sideways", []string{"long", "short"})
	if r.Valid {
		t.Fatalf("expected invalid; got %+v", r)
	}
	if !strings.Contains(r.Message, "long, short") {
		t.Errorf("choices must be joined with ', '; got %q", r.Message)
	}

	if !validateStringChoice("side", "long", []string{"long", "short"}).Valid {
		t.Errorf("matching value must be Valid")
	}
	// Empty choices is a degenerate but valid call site.
	r = validateStringChoice("side", "long", []string{})
	if r.Valid {
		t.Errorf("empty choices must always be invalid")
	}
}
