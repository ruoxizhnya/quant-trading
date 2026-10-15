package mining

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ─── 公共测试夹具 ───────────────────────────────────────────────────────

// testSnapshot 是「启动前预加载」的只读快照：close 字段，两只股票，40 根
// 升序日频 K 线（末点 = as-of）。
func testSnapshot() *Snapshot {
	series := func(base float64) []float64 {
		out := make([]float64, 40)
		for i := range out {
			out[i] = base + float64(i) // 单调递增，保证 ts_delay 比值良定义
		}
		return out
	}
	return NewSnapshot(map[string]map[string][]float64{
		"close": {
			"AAA": series(10),
			"BBB": series(20),
		},
	})
}

// goodCandidate 是一条合规候选（momentum 的一个新公式版本：10 日动量而非
// 种子的 20 日——改公式 = 新版本哈希）。
func goodCandidate() Candidate {
	return Candidate{
		Name:          domain.FactorMomentum,
		Formula:       "close / ts_delay(close, 10) - 1",
		Validity:      "close > 0 and ts_delay(close, 10) > 0",
		Caliber:       domain.CaliberZScore,
		Lookahead:     "trade_date",
		Lookback:      20,
		EffectiveFrom: "2026-10-15",
		Provenance: Provenance{
			SourceKind:   domain.FactorSourceAI,
			Generator:    "unit-test-generator",
			Generation:   1,
			Rationale:    "10 日动量在样例快照上稳定",
			NumTrials:    10,
			FailedTrials: 2,
		},
	}
}

type staticProposer struct {
	cands []Candidate
	err   error
}

func (p staticProposer) Propose(context.Context) ([]Candidate, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.cands, nil
}

type stubEvaluator struct{ m Metrics }

func (e stubEvaluator) Evaluate(context.Context, Candidate, *Snapshot) (Metrics, error) {
	return e.m, nil
}

// strongMetrics 是「研究验证」侧给出的真实强信号（样本内 0.30 / 样本外 0.20）。
func strongMetrics() Metrics {
	return Metrics{InSampleIC: 0.30, OutSampleIC: 0.20, NumPeriods: 252}
}

// ─── 端到端：挖一条 → 测一条 → 用一条（D8 ⑤1） ─────────────────────────

func TestPipeline_EndToEnd_MineTestUse(t *testing.T) {
	snap := testSnapshot()
	book := data.NewRegistrationBook()
	p := NewPipeline(staticProposer{cands: []Candidate{goodCandidate()}}, stubEvaluator{m: strongMetrics()}, snap, book, "unit-test-registrar")
	p.now = func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) }

	records, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("want 1 record, got %d", len(records))
	}
	rec := records[0]

	// 挖：产物是公式文本，带版本哈希（可复现的锁版本键）。
	if rec.VersionHash == "" {
		t.Fatalf("mined record must carry a version hash")
	}
	if rec.Candidate.Formula == "" {
		t.Fatalf("mined product must be a formula, not code")
	}

	// 测：沙箱凭证绑定该版本；治理两件都通过。
	if rec.Attestation == nil {
		t.Fatalf("sandbox attestation must be present after admission")
	}
	if rec.Attestation.ContentHash != rec.VersionHash {
		t.Fatalf("attestation must bind the version: %s vs %s", rec.Attestation.ContentHash, rec.VersionHash)
	}
	if rec.Overfit.Overfit || !rec.OOS.Pass {
		t.Fatalf("strong candidate must pass governance, got overfit=%+v oos=%+v", rec.Overfit, rec.OOS)
	}

	// 用：按 D4 登记入库，留痕四样齐全。
	if !rec.Admitted {
		t.Fatalf("candidate must be admitted, reject=%q", rec.RejectReason)
	}
	ev, ok := rec.Evidence()
	if !ok {
		t.Fatalf("admitted record must project to a persistable evidence row")
	}
	if err := ev.Validate(); err != nil {
		t.Fatalf("evidence row must be self-consistent: %v", err)
	}
	if !ev.Admitted || ev.SandboxHash != ev.VersionHash {
		t.Fatalf("admitted evidence must carry the sandbox hash bound to the version: %+v", ev)
	}

	// 用一条：策略按名解析到刚登记的版本，并在只读快照上算出值（零代码改动）。
	def, hash, ok := p.Effective(domain.FactorMomentum)
	if !ok || hash != rec.VersionHash {
		t.Fatalf("effective resolution must return the just-registered version, got ok=%v hash=%s", ok, hash)
	}
	// 登记的定义能被 D3 注册表接纳（即「策略可引用」的前置）。
	reg, err := data.NewRegistry([]domain.FactorDefinition{def})
	if err != nil {
		t.Fatalf("registered definition must pass registry gates: %v", err)
	}
	if reg.VersionHash(domain.FactorMomentum) != rec.VersionHash {
		t.Fatalf("registry must resolve the same version hash")
	}
	got, err := EvaluateOnSnapshot(def, snap)
	if err != nil {
		t.Fatalf("use-one-step: evaluate on snapshot failed: %v", err)
	}
	// 求值窗口 = 定义声明的 Lookback（与生产 provider 同款窗口语义）：快照
	// 有 40 根、Lookback=20，故取最近 20 根。
	if len(got["AAA"]) != def.Lookback {
		t.Fatalf("use-one-step: expected the Lookback window (%d points), got %d", def.Lookback, len(got["AAA"]))
	}
}

// ─── 破坏腿①：跳过沙箱直接入库 → 拦截（D8 ⑤3） ─────────────────────────

func TestPipeline_Admit_RejectsWhenSandboxNotRun(t *testing.T) {
	snap := testSnapshot()
	p := NewPipeline(staticProposer{}, stubEvaluator{}, snap, data.NewRegistrationBook(), "reg")
	c := goodCandidate()
	// 直接把凭证置空调用登记入口——模拟「跳过沙箱直接入库」。
	if _, err := p.admit(c, nil, time.Now()); !errors.Is(err, ErrSandboxNotRun) {
		t.Fatalf("admit without attestation must be rejected with ErrSandboxNotRun, got %v", err)
	}
	// 且库里不得留下任何登记。
	if _, _, ok := p.Effective(domain.FactorMomentum); ok {
		t.Fatalf("no registration must land when the sandbox was skipped")
	}
}

func TestPipeline_Admit_RejectsAttestationMismatch(t *testing.T) {
	snap := testSnapshot()
	p := NewPipeline(staticProposer{}, stubEvaluator{}, snap, data.NewRegistrationBook(), "reg")
	c := goodCandidate()
	// 凭证哈希与定义版本不符（模拟「凭证没绑定这条定义」）——登记必须拒收。
	forged := &Attestation{ContentHash: strings.Repeat("0", 64)}
	if _, err := p.admit(c, forged, time.Now()); !errors.Is(err, ErrAttestationMismatch) {
		t.Fatalf("mismatched attestation must be rejected with ErrAttestationMismatch, got %v", err)
	}
}

// ─── 破坏腿②：去掉哈希锁版本 → 复现测试红（D8 ⑤3） ───────────────────

func TestCandidate_VersionHash_LocksContent(t *testing.T) {
	c := goodCandidate()
	d1, err := c.Definition()
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	d2, err := c.Definition()
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	if d1.VersionHash() != d2.VersionHash() {
		t.Fatalf("same content must derive the same version hash (reproducibility)")
	}

	// 改一个字符 = 新版本哈希（内容锁版本：改公式必然可见）。
	c2 := goodCandidate()
	c2.Formula = "close / ts_delay(close, 11) - 1"
	d3, err := c2.Definition()
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	if d3.VersionHash() == d1.VersionHash() {
		t.Fatalf("a content change must change the version hash")
	}
}

func TestPipeline_Record_EvidenceBindsHash(t *testing.T) {
	c := goodCandidate()
	def, err := c.Definition()
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	rec := Record{
		Candidate:   c,
		VersionHash: def.VersionHash(),
		Attestation: &Attestation{ContentHash: def.VersionHash()},
		Admitted:    true,
		At:          time.Now(),
	}
	ev, ok := rec.Evidence()
	if !ok {
		t.Fatalf("record with a version hash must project to evidence")
	}
	if ev.VersionHash != def.VersionHash() || ev.SandboxHash != ev.VersionHash {
		t.Fatalf("evidence must pin the exact version hash: %+v", ev)
	}
	// 没有版本哈希（连解析都没过）→ 不产出以版本为键的留痕。
	if _, ok := (Record{Candidate: c}).Evidence(); ok {
		t.Fatalf("a record without a version hash must not project to evidence")
	}
}

// ─── 三条命根子逐条验证（D8 ④） ────────────────────────────────────────

// 命根子②防偷看：快照缺字段一律 NaN（缺失≠0），且求值只读快照。
func TestSnapshot_MissingFieldIsNaN(t *testing.T) {
	snap := NewSnapshot(map[string]map[string][]float64{"close": {"AAA": {1, 2, 3}}})
	c := goodCandidate()
	c.Formula = "close - ts_delay(close, 1)"
	def, err := c.Definition()
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	// 直接经 provider 取一个快照里根本没有的字段：必须是 NaN，而不是 0。
	pv := snap.provider(def.InputFields)
	vals, err := pv.GetField("AAA", "not_a_field", 0)
	if err != nil {
		t.Fatalf("missing field must not error (missing != 0), got %v", err)
	}
	if len(vals) != 1 || !isNaN(vals[0]) {
		t.Fatalf("missing field must read as NaN, got %v", vals)
	}
}

// 命根子②的窗口面：快照 provider 必须与生产 provider 同款窗口语义
// （lookback>0 时只取最近 lookback 根）——否则沙箱会在比生产**更长**的窗口上
// 判定「公式能跑」，窗口取窄的定义在沙箱放行、到生产侧变形。
func TestSnapshot_ProviderHonorsLookback(t *testing.T) {
	pv := NewSnapshot(map[string]map[string][]float64{
		"close": {"AAA": {1, 2, 3, 4, 5}},
	}).provider([]string{"close"})

	got, err := pv.GetField("AAA", "close", 2)
	if err != nil {
		t.Fatalf("GetField: %v", err)
	}
	if len(got) != 2 || got[0] != 4 || got[1] != 5 {
		t.Fatalf("lookback>0 must return the most recent bars, got %v", got)
	}

	all, err := pv.GetField("AAA", "close", 0)
	if err != nil {
		t.Fatalf("GetField: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("lookback<=0 must return the whole series, got %v", all)
	}
}

// 命根子③可比：同一公式 + 同一快照两次求值逐点相等。
func TestEvaluateOnSnapshot_BilateralComparability(t *testing.T) {
	snap := testSnapshot()
	c := goodCandidate()
	def, err := c.Definition()
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	a, err := EvaluateOnSnapshot(def, snap)
	if err != nil {
		t.Fatalf("first eval: %v", err)
	}
	b, err := EvaluateOnSnapshot(def, snap)
	if err != nil {
		t.Fatalf("second eval: %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("symbol counts differ: %d vs %d", len(a), len(b))
	}
	for sym, av := range a {
		bv := b[sym]
		if len(av) != len(bv) {
			t.Fatalf("%s: series lengths differ", sym)
		}
		for i := range av {
			// NaN 感知比较：两点同为 NaN 也算一致（缺失点不破坏可比性）。
			if !sameFloat(av[i], bv[i]) {
				t.Fatalf("%s[%d]: %v != %v (same formula + same snapshot must reproduce)", sym, i, av[i], bv[i])
			}
		}
	}
}

// 命根子①可复现：同一次挖掘重复运行，产出的版本哈希逐条一致。
func TestPipeline_Run_Reproducible(t *testing.T) {
	snap := testSnapshot()
	mk := func() *Pipeline {
		return NewPipeline(staticProposer{cands: []Candidate{goodCandidate()}}, stubEvaluator{m: strongMetrics()}, snap, data.NewRegistrationBook(), "reg")
	}
	r1, err := mk().Run(context.Background())
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	r2, err := mk().Run(context.Background())
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if r1[0].VersionHash != r2[0].VersionHash {
		t.Fatalf("re-running the same mining input must reproduce the same version hash: %s vs %s", r1[0].VersionHash, r2[0].VersionHash)
	}
}

// ─── 治理拦截：弱信号被拒但仍留痕 ──────────────────────────────────────

func TestPipeline_RejectsWeakSignalButKeepsEvidence(t *testing.T) {
	snap := testSnapshot()
	c := goodCandidate()
	c.Provenance.NumTrials = 5000
	c.Provenance.FailedTrials = 4999
	p := NewPipeline(staticProposer{cands: []Candidate{c}}, stubEvaluator{m: Metrics{InSampleIC: 0.03, OutSampleIC: 0.03, NumPeriods: 100}}, snap, data.NewRegistrationBook(), "reg")
	recs, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	rec := recs[0]
	if rec.Admitted {
		t.Fatalf("weak signal across 5000 trials must be rejected")
	}
	if rec.RejectReason == "" {
		t.Fatalf("rejected candidate must state why")
	}
	ev, ok := rec.Evidence()
	if !ok {
		t.Fatalf("rejected candidate with a version hash must still keep evidence")
	}
	if ev.Admitted || ev.RejectReason == "" {
		t.Fatalf("rejected evidence must be marked rejected with a reason: %+v", ev)
	}
	if err := ev.Validate(); err != nil {
		t.Fatalf("rejected evidence must still be self-consistent: %v", err)
	}
}

func isNaN(v float64) bool { return v != v }

// sameFloat 是 NaN 感知的相等判定：同为 NaN 视为相等。
func sameFloat(a, b float64) bool {
	if isNaN(a) && isNaN(b) {
		return true
	}
	return a == b
}
