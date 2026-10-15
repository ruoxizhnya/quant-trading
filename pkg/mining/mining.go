// Package mining 是 D8「AI 因子挖掘流水线 + 治理层」的可执行形态
// （蓝图 §5.5 / §6 三期③）。
//
// 闭环（D8 ③1）：
//
//	公式文本 ——(1) 沙箱验证——> 通过凭证 ——(2) 治理层检验——>
//	        ——(3) 按 D4 登记四样入库——> 策略按名即用（零代码改动）
//
// 三条命根子（可复现 / 防偷看 / 可比）在本包的落法（D8 ④）：
//
//   - 可复现：挖掘产物是**公式文本**不是代码，入库时按 D3 的 VersionHash
//     锁版本；同一次挖掘引用同一版本哈希，重跑结果一致。
//   - 防偷看：沙箱与治理求值只读**预加载快照**（Snapshot），全程不开
//     运行时查询入口——与策略侧「考前发资料」同一条纪律（蓝图 §7.2）。
//   - 可比：同一公式 + 同一快照，两次求值逐点相等（双侧可比的对拍内环）。
//
// 边界（D8 ④/⑥）：不调挖掘算法本体（LLM/MCTS 是实现细节，躲在 Proposer
// 接口之后）；不放宽沙箱与安全约束；不绕过 D4 登记纪律；挖掘产物只进公式
// 库（FactorDefinition 版本），**不进代码库**。
//
// ⚠️ 名册边界（继承 D3 语义，非本包放宽）：FactorDefinition.Name 必须落在
// 冻结名册内（domain.ParseFactorType）。故本流水线产出的是**已有因子名的新
// 公式版本**（改公式 = 新版本哈希），不动态新增因子名——新增名字属协议变更，
// 需走正式变更评审（D3/D4 已钉）。这一条是「产物只进公式库」的结构保证。
package mining

import (
	"fmt"
	"sort"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/expression"
)

// Provenance 是一条候选的来源留痕（审计「来源」）。
//
// SourceKind 复用 domain 的因子来源分类（literature / supply_chain /
// ai_hypothesis / ad_hoc）——「有经济学机制支撑」与「在数据上试出来的」
// 可信度不是一个量级，这条留痕让二者在库里可区分。
//
// NumTrials / FailedTrials 是过拟合校正的输入：**必须含失败**——只统计成功
// 次数等于把最有力的那部分选择偏差藏起来（先例 pkg/ai/validation 的
// StatisticalInput.NumTrials 教训）。
type Provenance struct {
	SourceKind   string `json:"source_kind"`
	Generator    string `json:"generator"`
	Generation   int    `json:"generation"`
	Rationale    string `json:"rationale,omitempty"`
	NumTrials    int    `json:"num_trials"`
	FailedTrials int    `json:"failed_trials"`
}

// Validate 校验来源留痕自洽（fail-closed）：生成器非空、来源分类已登记、
// 尝试次数为正且失败数不超过总次数。
func (p Provenance) Validate() error {
	if p.Generator == "" {
		return fmt.Errorf("mining provenance: generator is required (audit trail requires 'who generated')")
	}
	switch p.SourceKind {
	case domain.FactorSourceAI, domain.FactorSourceLiterature,
		domain.FactorSourceSupplyChain, domain.FactorSourceAdHoc:
	default:
		return fmt.Errorf("mining provenance: unknown source_kind %q", p.SourceKind)
	}
	if p.NumTrials <= 0 {
		return fmt.Errorf("mining provenance: num_trials must be positive (overfit correction needs it)")
	}
	if p.FailedTrials < 0 || p.FailedTrials > p.NumTrials {
		return fmt.Errorf("mining provenance: failed_trials %d out of range [0,%d]", p.FailedTrials, p.NumTrials)
	}
	return nil
}

// Candidate 是一条待入库的挖掘候选。
//
// 产物是**公式文本**（蓝图 §5.5）：Name + Formula + Validity 足以让表达式
// 引擎现算，故可动态加载、即挖即测。InputFields 不在此声明——由公式的
// identifier 集合派生（避免「声明了没用上」的死声明）。
type Candidate struct {
	Name          domain.FactorType    `json:"name"`
	Formula       string               `json:"formula"`
	Validity      string               `json:"validity,omitempty"`
	Caliber       domain.FactorCaliber `json:"caliber,omitempty"`
	Lookahead     string               `json:"lookahead"`
	Lookback      int                  `json:"lookback"`
	EffectiveFrom string               `json:"effective_from"`
	Provenance    Provenance           `json:"provenance"`
}

// Definition 把候选翻译成 D3 的 FactorDefinition（内核固定 formula）。
//
// InputFields 从公式 identifier 派生（排序去重）：与 NewRegistry 的
// 「identifier ↔ InputFields 双向相等」闸门天然一致。Caliber 缺省为
// z-score（生产尾部仍走 ZScore 标准化，口径标签随之走）。
//
// 解析失败即报错——这里不吞，沙箱层会把该错误作为拒收理由。
func (c Candidate) Definition() (domain.FactorDefinition, error) {
	parsed, err := expression.NewParser().Parse(c.Formula)
	if err != nil {
		return domain.FactorDefinition{}, fmt.Errorf("candidate %q: formula does not parse: %w", c.Name, err)
	}
	inputs := expression.ExtractInputs(parsed.AST)
	sort.Strings(inputs)
	caliber := c.Caliber
	if caliber == "" {
		caliber = domain.CaliberZScore
	}
	return domain.FactorDefinition{
		Name:          c.Name,
		Kernel:        domain.KernelFormula,
		Formula:       c.Formula,
		Validity:      c.Validity,
		Caliber:       caliber,
		InputFields:   inputs,
		EffectiveFrom: c.EffectiveFrom,
		Lookahead:     c.Lookahead,
		Lookback:      c.Lookback,
	}, nil
}
