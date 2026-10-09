// Package expression tests for cs_neutralize cross-sectional operator.
//
// S7-P3-1 Phase 1 (ODR-043): The cs_neutralize operator was declared in
// IsCrossSectionalOp and advertised in the LLM prompt as a 2-arg form
// `cs_neutralize(x, group)`, but:
//   - the parser enforced 1-arg arity for ALL cross-sectional ops,
//   - the evaluator had no cs_neutralize case (silently returned input),
//   - the AST's CrossSectionalNode had no Group field.
//
// These tests verify the fix: 2-arg parsing, group-aware evaluation
// (per-group mean subtraction), and AST traversal correctness.
package expression

import (
	"math"
	"testing"
)

// mockProvider implements DataProvider for cross-sectional tests.
// It returns pre-canned per-symbol, per-field slices so tests can
// construct deterministic cross-sectional snapshots.
type mockProvider struct {
	symbols []string
	// data[symbol][field] = values; missing symbol/field → empty slice.
	data map[string]map[string][]float64
}

func (m *mockProvider) GetField(symbol, field string, lookback int) ([]float64, error) {
	fields, ok := m.data[symbol]
	if !ok {
		return []float64{}, nil
	}
	vals, ok := fields[field]
	if !ok {
		return []float64{}, nil
	}
	if lookback > 0 && len(vals) > lookback {
		return vals[len(vals)-lookback:], nil
	}
	return vals, nil
}

func (m *mockProvider) GetSymbols() []string { return m.symbols }

// newGroupedProvider builds a provider with 4 symbols where `close`
// is [1,2,3,4] and `sector` groups them as [A,A,B,B] (encoded as 1,1,2,2).
func newGroupedProvider() *mockProvider {
	return &mockProvider{
		symbols: []string{"S1", "S2", "S3", "S4"},
		data: map[string]map[string][]float64{
			"S1": {"close": {1}, "sector": {1}},
			"S2": {"close": {2}, "sector": {1}},
			"S3": {"close": {3}, "sector": {2}},
			"S4": {"close": {4}, "sector": {2}},
		},
	}
}

// ─── Parser tests ──────────────────────────────────────────────────────

// TestParse_CSNeutralize_2Arg verifies cs_neutralize parses with exactly
// 2 arguments and the Group field is populated.
func TestParse_CSNeutralize_2Arg(t *testing.T) {
	p := NewParser()
	expr, err := p.Parse("cs_neutralize(close, sector)")
	if err != nil {
		t.Fatalf("Parse cs_neutralize(close, sector) error: %v", err)
	}
	cs, ok := expr.AST.(*CrossSectionalNode)
	if !ok {
		t.Fatalf("expected *CrossSectionalNode, got %T", expr.AST)
	}
	if cs.Op != "cs_neutralize" {
		t.Errorf("Op = %q, want %q", cs.Op, "cs_neutralize")
	}
	if cs.Group == nil {
		t.Fatal("Group is nil; expected non-nil group expression")
	}
	// Group should be an IdentifierNode "sector"
	gid, ok := cs.Group.(*IdentifierNode)
	if !ok {
		t.Fatalf("Group expected *IdentifierNode, got %T", cs.Group)
	}
	if gid.Name != "sector" {
		t.Errorf("Group.Name = %q, want %q", gid.Name, "sector")
	}
}

// TestParse_CSNeutralize_1Arg_Error verifies 1-arg form is rejected.
func TestParse_CSNeutralize_1Arg_Error(t *testing.T) {
	p := NewParser()
	_, err := p.Parse("cs_neutralize(close)")
	if err == nil {
		t.Fatal("expected error for 1-arg cs_neutralize, got nil")
	}
}

// TestParse_CSNeutralize_3Arg_Error verifies 3-arg form is rejected.
func TestParse_CSNeutralize_3Arg_Error(t *testing.T) {
	p := NewParser()
	_, err := p.Parse("cs_neutralize(close, sector, market)")
	if err == nil {
		t.Fatal("expected error for 3-arg cs_neutralize, got nil")
	}
}

// TestParse_CSRank_1Arg_Regression verifies 1-arg cs ops still parse
// and have Group == nil (backward compatibility).
func TestParse_CSRank_1Arg_Regression(t *testing.T) {
	p := NewParser()
	expr, err := p.Parse("cs_rank(close)")
	if err != nil {
		t.Fatalf("Parse cs_rank(close) error: %v", err)
	}
	cs, ok := expr.AST.(*CrossSectionalNode)
	if !ok {
		t.Fatalf("expected *CrossSectionalNode, got %T", expr.AST)
	}
	if cs.Group != nil {
		t.Errorf("Group = %v, want nil for 1-arg cs op", cs.Group)
	}
}

// ─── Evaluator tests ──────────────────────────────────────────────────

// TestEvaluate_CSNeutralize_GroupMeanSubtraction verifies that
// cs_neutralize subtracts the per-group mean. With close=[1,2,3,4]
// and sector groups [A,A,B,B], expected result is [-0.5, 0.5, -0.5, 0.5].
func TestEvaluate_CSNeutralize_GroupMeanSubtraction(t *testing.T) {
	p := NewParser()
	expr, err := p.Parse("cs_neutralize(close, sector)")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	ev := NewEvaluator(newGroupedProvider())
	result, err := ev.Evaluate(expr.AST, 1)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	expected := map[string]float64{
		"S1": -0.5, // 1 - mean(1,2)
		"S2": 0.5,  // 2 - mean(1,2)
		"S3": -0.5, // 3 - mean(3,4)
		"S4": 0.5,  // 4 - mean(3,4)
	}
	for symbol, want := range expected {
		vals, ok := result[symbol]
		if !ok || len(vals) == 0 {
			t.Errorf("symbol %s: no result", symbol)
			continue
		}
		if math.Abs(vals[0]-want) > 1e-9 {
			t.Errorf("symbol %s: got %.6f, want %.6f", symbol, vals[0], want)
		}
	}
}

// TestEvaluate_CSNeutralize_SingleGroup_GlobalDemean verifies that when
// all symbols share one group, the result equals global demeaning.
func TestEvaluate_CSNeutralize_SingleGroup_GlobalDemean(t *testing.T) {
	provider := &mockProvider{
		symbols: []string{"S1", "S2", "S3"},
		data: map[string]map[string][]float64{
			"S1": {"close": {10}, "sector": {1}},
			"S2": {"close": {20}, "sector": {1}},
			"S3": {"close": {30}, "sector": {1}},
		},
	}
	p := NewParser()
	expr, err := p.Parse("cs_neutralize(close, sector)")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	ev := NewEvaluator(provider)
	result, err := ev.Evaluate(expr.AST, 1)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	// Global mean = 20; expected [-10, 0, 10]
	expected := map[string]float64{"S1": -10, "S2": 0, "S3": 10}
	for symbol, want := range expected {
		vals := result[symbol]
		if len(vals) == 0 {
			t.Errorf("symbol %s: no result", symbol)
			continue
		}
		if math.Abs(vals[0]-want) > 1e-9 {
			t.Errorf("symbol %s: got %.6f, want %.6f", symbol, vals[0], want)
		}
	}
}

// TestEvaluate_CSNeutralize_NaNExclusion verifies NaN values do not
// pollute the group mean (they're excluded from the mean calculation
// and the result for NaN positions is NaN).
func TestEvaluate_CSNeutralize_NaNExclusion(t *testing.T) {
	provider := &mockProvider{
		symbols: []string{"S1", "S2", "S3"},
		data: map[string]map[string][]float64{
			"S1": {"close": {10}, "sector": {1}},
			"S2": {"close": {math.NaN()}, "sector": {1}},
			"S3": {"close": {20}, "sector": {1}},
		},
	}
	p := NewParser()
	expr, err := p.Parse("cs_neutralize(close, sector)")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	ev := NewEvaluator(provider)
	result, err := ev.Evaluate(expr.AST, 1)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	// Group 1: non-NaN values are [10, 20], mean = 15.
	// S1: 10 - 15 = -5; S2: NaN; S3: 20 - 15 = 5.
	if v := result["S1"][0]; math.Abs(v-(-5)) > 1e-9 {
		t.Errorf("S1: got %.6f, want -5", v)
	}
	if v := result["S3"][0]; math.Abs(v-5) > 1e-9 {
		t.Errorf("S3: got %.6f, want 5", v)
	}
	if !math.IsNaN(result["S2"][0]) {
		t.Errorf("S2: got %.6f, want NaN", result["S2"][0])
	}
}

// TestEvaluate_CSNeutralize_EmptyInput verifies empty symbol set returns
// empty result without error.
func TestEvaluate_CSNeutralize_EmptyInput(t *testing.T) {
	provider := &mockProvider{
		symbols: []string{},
		data:    map[string]map[string][]float64{},
	}
	p := NewParser()
	expr, err := p.Parse("cs_neutralize(close, sector)")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	ev := NewEvaluator(provider)
	result, err := ev.Evaluate(expr.AST, 1)
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}
}

// ─── csNeutralize helper unit tests ───────────────────────────────────

// TestCSNeutralize_GroupMeanSubtraction is a direct unit test of the
// helper, independent of the evaluator pipeline.
func TestCSNeutralize_GroupMeanSubtraction(t *testing.T) {
	values := []float64{1, 2, 3, 4}
	group := []float64{1, 1, 2, 2}
	result := csNeutralize(values, group)
	expected := []float64{-0.5, 0.5, -0.5, 0.5}
	for i, want := range expected {
		if math.Abs(result[i]-want) > 1e-9 {
			t.Errorf("index %d: got %.6f, want %.6f", i, result[i], want)
		}
	}
}

// TestCSNeutralize_LengthMismatch_FallbackGlobalDemean verifies that
// when group length != values length, the helper falls back to global
// demeaning (defensive — shouldn't happen via evaluator but protects
// direct callers).
func TestCSNeutralize_LengthMismatch_FallbackGlobalDemean(t *testing.T) {
	values := []float64{1, 2, 3, 4}
	group := []float64{1, 1, 2} // shorter than values
	result := csNeutralize(values, group)
	// Global mean = 2.5; expected [-1.5, -0.5, 0.5, 1.5]
	expected := []float64{-1.5, -0.5, 0.5, 1.5}
	for i, want := range expected {
		if math.Abs(result[i]-want) > 1e-9 {
			t.Errorf("index %d: got %.6f, want %.6f", i, result[i], want)
		}
	}
}

// TestCSNeutralize_NilGroup_GlobalDemean verifies nil group triggers
// global demeaning.
func TestCSNeutralize_NilGroup_GlobalDemean(t *testing.T) {
	values := []float64{1, 2, 3, 4}
	result := csNeutralize(values, nil)
	// Global mean = 2.5; expected [-1.5, -0.5, 0.5, 1.5]
	expected := []float64{-1.5, -0.5, 0.5, 1.5}
	for i, want := range expected {
		if math.Abs(result[i]-want) > 1e-9 {
			t.Errorf("index %d: got %.6f, want %.6f", i, result[i], want)
		}
	}
}

// TestCSNeutralize_EmptyInput verifies empty input returns empty output.
func TestCSNeutralize_EmptyInput(t *testing.T) {
	result := csNeutralize([]float64{}, []float64{})
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d elements", len(result))
	}
}

// ─── AST traversal tests ──────────────────────────────────────────────

// TestCrossSectionalNode_String_WithGroup verifies String() includes
// the group argument when present.
func TestCrossSectionalNode_String_WithGroup(t *testing.T) {
	p := NewParser()
	expr, err := p.Parse("cs_neutralize(close, sector)")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	got := expr.AST.String()
	want := "cs_neutralize(close, sector)"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestCrossSectionalNode_String_WithoutGroup verifies 1-arg cs ops
// still render without a group.
func TestCrossSectionalNode_String_WithoutGroup(t *testing.T) {
	p := NewParser()
	expr, err := p.Parse("cs_rank(close)")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	got := expr.AST.String()
	want := "cs_rank(close)"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestCrossSectionalNode_Children verifies Children() returns [Expr, Group]
// when Group is non-nil and [Expr] when nil.
func TestCrossSectionalNode_Children(t *testing.T) {
	t.Run("with_group", func(t *testing.T) {
		p := NewParser()
		expr, _ := p.Parse("cs_neutralize(close, sector)")
		cs := expr.AST.(*CrossSectionalNode)
		children := cs.Children()
		if len(children) != 2 {
			t.Fatalf("expected 2 children, got %d", len(children))
		}
	})
	t.Run("without_group", func(t *testing.T) {
		p := NewParser()
		expr, _ := p.Parse("cs_rank(close)")
		cs := expr.AST.(*CrossSectionalNode)
		children := cs.Children()
		if len(children) != 1 {
			t.Fatalf("expected 1 child, got %d", len(children))
		}
	})
}

// TestExtractInputs_RecursesIntoGroup verifies ExtractInputs collects
// fields referenced in the group expression (e.g. "sector").
func TestExtractInputs_RecursesIntoGroup(t *testing.T) {
	p := NewParser()
	expr, err := p.Parse("cs_neutralize(close, sector)")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	inputs := ExtractInputs(expr.AST)
	// Both "close" and "sector" should be collected.
	found := map[string]bool{}
	for _, in := range inputs {
		found[in] = true
	}
	if !found["close"] {
		t.Error("ExtractInputs missing 'close'")
	}
	if !found["sector"] {
		t.Error("ExtractInputs missing 'sector' (group field not recursed)")
	}
}
