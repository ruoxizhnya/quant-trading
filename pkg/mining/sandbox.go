package mining

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/expression"
)

// ErrSandboxRejected 是沙箱拒收候选的哨兵：公式解析失败 / L1 闸门不过 /
// 字段越界 / 有界求值失败——任何一条不通过都不得入库。
var ErrSandboxRejected = errors.New("mining sandbox rejected candidate")

// DefaultMaxEvalBars 是沙箱允许的最大回看窗口（安全约束不放宽：拒绝无界
// 回看这类「看起来能跑其实爆内存」的定义）。
const DefaultMaxEvalBars = 4096

// minSampleSymbols 是沙箱求值至少需要覆盖的股票数（低于此判为「不可证」，
// 防止在空快照上把恒 NaN 的公式误判为通过）。
const minSampleSymbols = 1

// Attestation 是沙箱验证通过凭证：绑定候选定义的**内容指纹**（VersionHash）。
//
// 流水线在登记前必须核对此凭证，于是「跳过沙箱直接入库」不可绕过
// （D8 ⑤3 破坏腿之一）。凭证不落库时是流水线内部状态，落库的是它的
// ContentHash（= 该定义版本哈希，见 domain.FactorMiningRecord.SandboxHash）。
type Attestation struct {
	// ContentHash = Candidate.Definition().VersionHash()：改一个字符即失效。
	ContentHash string
	// Inputs 是派生出的规范层字段（审计用）。
	Inputs []string
	At     time.Time
	Detail string
}

// Sandbox 是公式候选的验证沙箱。
//
// 与 pkg/sandbox/runner（进程隔离跑 **Go 代码**）的分工：挖掘产物是**公式
// 文本**不是代码，不会编译、不会执行任意 Go——它的隔离边界就是 expression
// DSL 本身（无 IO、无循环、算子白名单）。故本沙箱复用表达引擎的解析器 +
// L1 闸门作为安全边界，再加「有界求值」，而不是去开子进程编译公式。
// 「不放宽安全约束」在这里 = 闸门一条不少、求值窗口有上限、panic 兜底。
type Sandbox struct {
	maxEvalBars int
	now         func() time.Time
}

// NewSandbox 构造默认沙箱。
func NewSandbox() *Sandbox {
	return &Sandbox{maxEvalBars: DefaultMaxEvalBars, now: time.Now}
}

// Validate 在沙箱里验证一条候选，通过则返回凭证，否则返回带哨兵的拒收错误。
//
// 顺序即纪律（每一步都是 fail-closed）：
//
//  1. 来源留痕自洽（生成器/来源分类/尝试次数——过拟合校正的输入必须齐全）；
//  2. 定义结构自洽 + 名字落在冻结名册（继承 D3）；
//  3. 公式可解析 + 过 L1 闸门（算子名/参数个数/identifier 合法）；
//  4. identifier ⊆ 规范层字段词表（D1）；
//  5. Validity 同规则且其 identifier ⊆ 已声明字段；
//  6. 有界求值：在**只读快照**上求值，窗口不超上限，panic 兜底，
//     且至少产出一个有限值（否则是空洞公式，拒收——不产垃圾分）。
func (s *Sandbox) Validate(c Candidate, snap *Snapshot) (*Attestation, error) {
	if err := c.Provenance.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSandboxRejected, err)
	}
	def, err := c.Definition()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSandboxRejected, err)
	}
	if err := def.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSandboxRejected, err)
	}

	parsed, err := expression.NewParser().Parse(def.Formula)
	if err != nil {
		return nil, fmt.Errorf("%w: formula does not parse: %v", ErrSandboxRejected, err)
	}
	if err := parsed.Validate(); err != nil {
		return nil, fmt.Errorf("%w: formula fails L1 gate: %v", ErrSandboxRejected, err)
	}

	known := make(map[string]bool)
	for _, f := range expression.AvailableFields() {
		known[f] = true
	}
	for _, f := range def.InputFields {
		if !known[f] {
			return nil, fmt.Errorf("%w: input field %q not in canonical field registry", ErrSandboxRejected, f)
		}
	}
	if def.Validity != "" {
		vExpr, err := expression.NewParser().Parse(def.Validity)
		if err != nil {
			return nil, fmt.Errorf("%w: validity does not parse: %v", ErrSandboxRejected, err)
		}
		if err := vExpr.Validate(); err != nil {
			return nil, fmt.Errorf("%w: validity fails L1 gate: %v", ErrSandboxRejected, err)
		}
		declared := make(map[string]bool, len(def.InputFields))
		for _, f := range def.InputFields {
			declared[f] = true
		}
		for _, id := range expression.ExtractInputs(vExpr.AST) {
			if !declared[id] {
				return nil, fmt.Errorf("%w: validity uses field %q not used by the formula", ErrSandboxRejected, id)
			}
		}
	}

	if snap == nil {
		return nil, fmt.Errorf("%w: no snapshot loaded (evaluation reads only the preloaded snapshot)", ErrSandboxRejected)
	}
	if def.Lookback > s.maxEvalBars {
		return nil, fmt.Errorf("%w: lookback %d exceeds sandbox limit %d", ErrSandboxRejected, def.Lookback, s.maxEvalBars)
	}
	if len(snap.symbols()) < minSampleSymbols {
		return nil, fmt.Errorf("%w: snapshot has no symbols to evaluate on (%s)", ErrSandboxRejected, snap.describe())
	}
	if err := evalBounded(parsed, def, snap); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSandboxRejected, err)
	}

	return &Attestation{
		ContentHash: def.VersionHash(),
		Inputs:      def.InputFields,
		At:          s.now(),
		Detail:      fmt.Sprintf("parse+L1+fields+bounded-eval ok on snapshot{%s}", snap.describe()),
	}, nil
}

// EvaluateOnSnapshot 在只读快照上求一条定义的值——「用一条」（即挖即测）的
// 求值入口。
//
// 全程只读预加载快照，无运行时查询入口（防偷看，D8 ④）。返回每 symbol 的
// 因子**原始**序列（未做横截面标准化——标准化由产出侧 caliber 口径决定）。
// 同一公式 + 同一快照两次调用逐点相等（可比性内环）。
func EvaluateOnSnapshot(def domain.FactorDefinition, snap *Snapshot) (map[string][]float64, error) {
	if _, ok := domain.ParseFactorType(string(def.Name)); !ok {
		return nil, fmt.Errorf("factor %q not in frozen registry", def.Name)
	}
	parsed, err := expression.NewParser().Parse(def.Formula)
	if err != nil {
		return nil, fmt.Errorf("factor %q: formula does not parse: %w", def.Name, err)
	}
	if err := parsed.Validate(); err != nil {
		return nil, fmt.Errorf("factor %q: formula fails L1 gate: %w", def.Name, err)
	}
	return evaluateParsed(parsed, def, snap)
}

// evaluateParsed 在只读快照上解析后的 AST 求值。快照缺字段一律 NaN（缺失≠0）。
func evaluateParsed(parsed *expression.Expression, def domain.FactorDefinition, snap *Snapshot) (map[string][]float64, error) {
	if snap == nil {
		return nil, fmt.Errorf("no snapshot loaded (evaluation reads only the preloaded snapshot)")
	}
	evaluator := expression.NewEvaluator(snap.provider(def.InputFields))
	return evaluator.Evaluate(parsed.AST, def.Lookback)
}

// evalBounded 在只读快照上有界求值，并回收 panic（不可信输入兜底）。
//
// 产出要求：至少一个 symbol 的末点值非 NaN——公式在样例快照上恒空 = 空洞
// 公式，拒收（D8 ⑤「不算出垃圾分」）。这与产出语义「缺失≠0」一致：工具有
// 值才叫通过，全缺失不是通过。
func evalBounded(parsed *expression.Expression, def domain.FactorDefinition, snap *Snapshot) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("expression evaluation panicked: %v", r)
		}
	}()

	got, err := evaluateParsed(parsed, def, snap)
	if err != nil {
		return err
	}
	for _, sym := range snap.symbols() {
		for _, v := range got[sym] {
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				return nil
			}
		}
	}
	return fmt.Errorf("formula produced no finite value on the snapshot")
}
