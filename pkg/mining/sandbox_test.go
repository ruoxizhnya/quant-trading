package mining

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestSandbox_AcceptsValidCandidate：合规候选通过并返回绑定版本哈希的凭证。
func TestSandbox_AcceptsValidCandidate(t *testing.T) {
	sb := NewSandbox()
	att, err := sb.Validate(goodCandidate(), testSnapshot())
	if err != nil {
		t.Fatalf("valid candidate rejected: %v", err)
	}
	def, err := goodCandidate().Definition()
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	if att.ContentHash != def.VersionHash() {
		t.Fatalf("attestation must bind the definition version hash: %s vs %s", att.ContentHash, def.VersionHash())
	}
	if len(att.Inputs) == 0 {
		t.Fatalf("attestation must record the canonical input fields")
	}
}

// TestSandbox_Rejects 覆盖各类 fail-closed 拒收：全部必须带 ErrSandboxRejected。
func TestSandbox_Rejects(t *testing.T) {
	sb := NewSandbox()
	cases := []struct {
		name string
		mut  func(c *Candidate)
		snap *Snapshot
	}{
		{"no generator", func(c *Candidate) { c.Provenance.Generator = "" }, testSnapshot()},
		{"no trials", func(c *Candidate) { c.Provenance.NumTrials = 0 }, testSnapshot()},
		{"unknown source", func(c *Candidate) { c.Provenance.SourceKind = "made_up" }, testSnapshot()},
		{"unparseable formula", func(c *Candidate) { c.Formula = "close + / 2" }, testSnapshot()},
		{"unknown operator", func(c *Candidate) { c.Formula = "bogus_op(close, 5)" }, testSnapshot()},
		{"unknown field", func(c *Candidate) { c.Formula = "not_a_real_field / 2" }, testSnapshot()},
		{"validity uses undeclared field", func(c *Candidate) { c.Validity = "volume > 0" }, testSnapshot()},
		{"hollow formula (all NaN)", func(c *Candidate) { c.Formula = "log(close - close)"; c.Validity = "" }, testSnapshot()},
		{"nil snapshot", func(c *Candidate) {}, nil},
		{"empty snapshot", func(c *Candidate) {}, NewSnapshot(nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := goodCandidate()
			tc.mut(&c)
			if _, err := sb.Validate(c, tc.snap); !errors.Is(err, ErrSandboxRejected) {
				t.Fatalf("case %q must be rejected with ErrSandboxRejected, got %v", tc.name, err)
			}
		})
	}
}

// TestSandbox_RejectsLookbackBeyondLimit：回看窗口超上限 → 拒收（安全约束不放宽）。
func TestSandbox_RejectsLookbackBeyondLimit(t *testing.T) {
	sb := &Sandbox{maxEvalBars: 5, now: time.Now}
	c := goodCandidate()
	c.Lookback = 6
	if _, err := sb.Validate(c, testSnapshot()); !errors.Is(err, ErrSandboxRejected) {
		t.Fatalf("lookback beyond the sandbox limit must be rejected, got %v", err)
	}
}

// TestCandidate_Definition_DerivesSortedInputs：InputFields 由公式派生且排序
// （与 NewRegistry 的 identifier↔InputFields 双向相等闸门天然一致）。
func TestCandidate_Definition_DerivesSortedInputs(t *testing.T) {
	c := Candidate{
		Name:          goodCandidate().Name,
		Formula:       "close * volume",
		Lookback:      5,
		EffectiveFrom: "2026-10-15",
		Provenance:    goodCandidate().Provenance,
	}
	def, err := c.Definition()
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	want := []string{"close", "volume"}
	if len(def.InputFields) != len(want) {
		t.Fatalf("want %v, got %v", want, def.InputFields)
	}
	for i := range want {
		if def.InputFields[i] != want[i] {
			t.Fatalf("inputs must be sorted: want %v, got %v", want, def.InputFields)
		}
	}
	if !strings.Contains(string(def.Kernel), "formula") {
		t.Fatalf("mined product kernel must be formula, got %s", def.Kernel)
	}
}
