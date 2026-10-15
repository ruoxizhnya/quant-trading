package mining

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ErrSandboxNotRun：登记前必须有沙箱凭证。流水线的登记入口据此把关，
// 「跳过沙箱直接入库」不可绕过（D8 ⑤3 破坏腿之一）。
var ErrSandboxNotRun = errors.New("mining: sandbox attestation required before registration")

// ErrAttestationMismatch：凭证没绑定这条定义（内容改过=哈希变，凭证失效）。
var ErrAttestationMismatch = errors.New("mining: sandbox attestation does not bind this definition")

// Metrics 是一条候选在快照上的研究验证结果（IC 分段）。
//
// IC 是「研究验证」侧的事（蓝图 §5.4）：引擎对拍给不了，只有分组收益 /
// IC 能给。样本内 / 样本外两段是治理层样本外验证的输入。
type Metrics struct {
	InSampleIC  float64
	OutSampleIC float64
	NumPeriods  int
}

// Proposer 产出候选。LLM / MCTS / 基因池都是实现细节，躲在本接口之后
// （D8 ④：不调挖掘算法本体）。
type Proposer interface {
	Propose(ctx context.Context) ([]Candidate, error)
}

// Evaluator 在**只读快照**上评估候选（防偷看：不给运行时查询入口）。
type Evaluator interface {
	Evaluate(ctx context.Context, c Candidate, snap *Snapshot) (Metrics, error)
}

// Record 是「挖一条 → 测一条 → 用一条」的完整留痕。
type Record struct {
	// Candidate 是候选原文（产物是公式文本，不是代码）。
	Candidate   Candidate
	VersionHash string
	// Attestation 通过沙箱才有值；它是登记的前置凭证。
	Attestation  *Attestation
	Metrics      Metrics
	Overfit      OverfitResult
	OOS          OOSResult
	Admitted     bool
	RejectReason string
	At           time.Time
}

// Evidence 把记录投影成可落库的治理留痕。
//
// VersionHash 为空（公式连解析都没过）时返回 false——那不是一个「定义
// 版本」，不进以版本为键的留痕表；这类候选只留在 Run 的返回里。
func (r Record) Evidence() (domain.FactorMiningRecord, bool) {
	if r.VersionHash == "" {
		return domain.FactorMiningRecord{}, false
	}
	rec := domain.FactorMiningRecord{
		FactorName:   r.Candidate.Name,
		VersionHash:  r.VersionHash,
		SourceKind:   r.Candidate.Provenance.SourceKind,
		Generator:    r.Candidate.Provenance.Generator,
		Generation:   r.Candidate.Provenance.Generation,
		Rationale:    r.Candidate.Provenance.Rationale,
		NumTrials:    r.Candidate.Provenance.NumTrials,
		FailedTrials: r.Candidate.Provenance.FailedTrials,
		Admitted:     r.Admitted,
		RejectReason: r.RejectReason,
		OverfitRawP:  r.Overfit.RawPValue,
		OverfitAdjP:  r.Overfit.AdjustedPValue,
		OverfitFlag:  r.Overfit.Overfit,
		OOSInSample:  r.Metrics.InSampleIC,
		OOSOutSample: r.Metrics.OutSampleIC,
		OOSPass:      r.OOS.Pass,
		CreatedAt:    r.At,
	}
	if r.Attestation != nil {
		rec.SandboxHash = r.Attestation.ContentHash
	}
	return rec, true
}

// Pipeline 是挖掘流水线：候选 → 沙箱 → 治理 → D4 登记。
type Pipeline struct {
	sandbox   *Sandbox
	book      *data.RegistrationBook
	proposer  Proposer
	evaluator Evaluator
	// snapshot 是预加载数据，流水线全程只读它（防偷看）。
	snapshot  *Snapshot
	registrar string
	now       func() time.Time
}

// NewPipeline 装配流水线。registrar 是有权登记的人（D4 四样之一，非空）。
func NewPipeline(proposer Proposer, evaluator Evaluator, snap *Snapshot, book *data.RegistrationBook, registrar string) *Pipeline {
	return &Pipeline{
		sandbox:   NewSandbox(),
		book:      book,
		proposer:  proposer,
		evaluator: evaluator,
		snapshot:  snap,
		registrar: registrar,
		now:       time.Now,
	}
}

// Run 跑一轮挖掘：逐条候选过 沙箱 → 治理 → 登记，返回完整留痕。
//
// 单条候选的任一环节不过只拒收该条（记录 reject_reason），不中断整轮——
// 「挖一批、坏的挡在外面、好的进库」是流水线的正常形态。
func (p *Pipeline) Run(ctx context.Context) ([]Record, error) {
	if p.proposer == nil {
		return nil, fmt.Errorf("mining: proposer is required")
	}
	if p.book == nil {
		return nil, fmt.Errorf("mining: registration book is required")
	}
	if strings.TrimSpace(p.registrar) == "" {
		return nil, fmt.Errorf("mining: registrar is required (four-required: registrar)")
	}
	cands, err := p.proposer.Propose(ctx)
	if err != nil {
		return nil, fmt.Errorf("mining: proposer failed: %w", err)
	}
	records := make([]Record, 0, len(cands))
	for _, c := range cands {
		records = append(records, p.runOne(ctx, c))
	}
	return records, nil
}

// runOne 是一条候选的完整流水（沙箱 → 治理 → 登记）。
func (p *Pipeline) runOne(ctx context.Context, c Candidate) Record {
	at := p.now()
	rec := Record{Candidate: c, At: at}

	// 先取定义拿版本哈希；解析不过 = 连「定义」都不是，到此为止。
	def, err := c.Definition()
	if err != nil {
		rec.RejectReason = err.Error()
		return rec
	}
	rec.VersionHash = def.VersionHash()

	// (1) 沙箱：没凭证不放行。
	att, err := p.sandbox.Validate(c, p.snapshot)
	if err != nil {
		rec.RejectReason = err.Error()
		return rec
	}
	rec.Attestation = att

	// (2) 治理：过拟合检测 + 样本外验证。
	m, err := p.evaluator.Evaluate(ctx, c, p.snapshot)
	if err != nil {
		rec.RejectReason = fmt.Sprintf("governance: evaluation failed: %v", err)
		return rec
	}
	rec.Metrics = m
	rec.Overfit = DetectOverfit(OverfitInput{
		IC:           m.InSampleIC,
		NumPeriods:   m.NumPeriods,
		NumTrials:    c.Provenance.NumTrials,
		FailedTrials: c.Provenance.FailedTrials,
	})
	rec.OOS = ValidateOutOfSample(OOSInput{InSampleIC: m.InSampleIC, OutSampleIC: m.OutSampleIC})
	if rec.Overfit.Overfit {
		rec.RejectReason = "governance: " + strings.Join(rec.Overfit.Reasons, "; ")
		return rec
	}
	if !rec.OOS.Pass {
		rec.RejectReason = "governance: " + strings.Join(rec.OOS.Reasons, "; ")
		return rec
	}

	// (3) 登记：必须带凭证（admit 内校验）。
	if _, err := p.admit(c, att, at); err != nil {
		rec.RejectReason = fmt.Sprintf("registration failed: %v", err)
		return rec
	}
	rec.Admitted = true
	return rec
}

// admit 是登记入口：**必须**带沙箱凭证，且凭证要绑定这条定义。
//
// 这是「跳过沙箱直接入库」的拦截点——凭证缺失或哈希不符即拒收，绝不因为
// 「反正调用方说验过了」就放行（D8 ⑤3 破坏腿）。
func (p *Pipeline) admit(c Candidate, att *Attestation, at time.Time) (*domain.FactorRegistration, error) {
	if att == nil {
		return nil, ErrSandboxNotRun
	}
	def, err := c.Definition()
	if err != nil {
		return nil, err
	}
	if att.ContentHash != def.VersionHash() {
		return nil, fmt.Errorf("%w: attestation %s vs definition %s", ErrAttestationMismatch, att.ContentHash, def.VersionHash())
	}
	reason := fmt.Sprintf("mined: source=%s generator=%s gen=%d trials=%d/%d",
		c.Provenance.SourceKind, c.Provenance.Generator, c.Provenance.Generation,
		c.Provenance.NumTrials, c.Provenance.FailedTrials)
	return p.book.Register(def, p.registrar, reason, at)
}

// Effective 解析某因子名当前应命中的版本（「用一条」的接线点）：新引用取
// 最新 active 版本，返回 (定义, 版本哈希, ok)。回滚语义继承 D4。
func (p *Pipeline) Effective(name domain.FactorType) (domain.FactorDefinition, string, bool) {
	return p.book.ResolveEffective(name)
}
