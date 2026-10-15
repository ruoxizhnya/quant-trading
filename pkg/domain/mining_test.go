package domain

import (
	"strings"
	"testing"
	"time"
)

const testMiningHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func baseMiningRecord() FactorMiningRecord {
	return FactorMiningRecord{
		FactorName:   FactorMomentum,
		VersionHash:  testMiningHash,
		SourceKind:   FactorSourceAI,
		Generator:    "unit-test-generator",
		Generation:   1,
		NumTrials:    10,
		FailedTrials: 2,
		SandboxHash:  testMiningHash,
		Admitted:     true,
		CreatedAt:    time.Now(),
	}
}

func TestFactorMiningRecord_AdmittedIsValid(t *testing.T) {
	if err := baseMiningRecord().Validate(); err != nil {
		t.Fatalf("a well-formed admitted record must validate: %v", err)
	}
}

// 破坏腿①（库侧兜底）：已入库必须带绑定该版本的沙箱凭证——缺凭证的
// 「跳过沙箱直接入库」在这里被拒。
func TestFactorMiningRecord_AdmittedRequiresSandboxAttestation(t *testing.T) {
	r := baseMiningRecord()
	r.SandboxHash = "" // 跳过沙箱：没有凭证
	if err := r.Validate(); err == nil {
		t.Fatalf("admitted record without a sandbox attestation must be rejected")
	}

	r2 := baseMiningRecord()
	r2.SandboxHash = strings.Repeat("f", 64) // 凭证没绑定这条定义
	if err := r2.Validate(); err == nil {
		t.Fatalf("admitted record whose attestation does not bind the version must be rejected")
	}
}

func TestFactorMiningRecord_RejectedRequiresReason(t *testing.T) {
	r := baseMiningRecord()
	r.Admitted = false
	r.SandboxHash = ""
	r.RejectReason = ""
	if err := r.Validate(); err == nil {
		t.Fatalf("a rejected record must state why (reject_reason)")
	}
	r.RejectReason = "governance: overfit"
	if err := r.Validate(); err != nil {
		t.Fatalf("a rejected record with a reason must validate: %v", err)
	}
}

func TestFactorMiningRecord_FailClosedFields(t *testing.T) {
	cases := []struct {
		name string
		mut  func(r *FactorMiningRecord)
	}{
		{"name not in roster", func(r *FactorMiningRecord) { r.FactorName = FactorType("made_up") }},
		{"hash not sha-256", func(r *FactorMiningRecord) { r.VersionHash = "short"; r.SandboxHash = "short" }},
		{"generator missing", func(r *FactorMiningRecord) { r.Generator = "" }},
		{"unknown source", func(r *FactorMiningRecord) { r.SourceKind = "made_up" }},
		{"non-positive trials", func(r *FactorMiningRecord) { r.NumTrials = 0 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := baseMiningRecord()
			c.mut(&r)
			if err := r.Validate(); err == nil {
				t.Fatalf("case %q must fail validation", c.name)
			}
		})
	}
}
