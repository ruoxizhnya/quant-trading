package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// ─── D3 公式化因子引擎：定义骨架（蓝图 §5.2） ─────────────────────────────
//
// 因子定义从 Go 代码迁入「库里公式」：名字 + 公式文本 + 输入字段 + 版本
// 哈希 + 生效日 + 口径 + 前视纪律，一条 FactorDefinition 一个因子。
//
// 三条铁律（蓝图 §5.2）在本类型上的落法：
//
//   - 缺失可见：产出侧由 FactorOutput.Value *float64 承载（nil=缺失），
//     定义侧的对应物是 Validity——公式算得出数、输入却落在数学定义域
//     外（PE<=0、价格<=0）的位置不算有效分数，必须缺失而不是垃圾值；
//   - 定义带版本指纹：VersionHash 由内容派生（canonical JSON 的
//     SHA-256），改公式=新哈希天然成立，老版本可复现；
//   - 前视纪律进 schema：Lookahead 是必填声明字段，不是注释。

// FactorKernel 是计算内核分型（蓝图 §5.2「一个外壳 + 三种内核」，
// D3 增补 pending 态承载「名册在册、内核待定」）。
type FactorKernel string

const (
	// KernelLeaf 叶子：原始字段直取（如 quality = roe）。
	KernelLeaf FactorKernel = "leaf"
	// KernelFormula 公式：叶子 + 算子表达式，由 pkg/expression 引擎求值。
	KernelFormula FactorKernel = "formula"
	// KernelCode 代码：复杂逻辑（跨期差分/趋势/TTM 桥接超出表达式引擎
	// 能力），保留 Go 出口。CodeRef 指向出口标识。
	KernelCode FactorKernel = "code"
	// KernelPending 名册在册、内核待定：冻结名册里有名字、库里已登记，
	// 但既无公式也无 Go 出口（size/volatility/growth 三个通用因子现状
	// —— 注册表常量 + 假设清单在，计算线从未实现）。查询时 fail-closed
	// 报「已登记未定义」，而不是 invent 一个数学定义充数。
	KernelPending FactorKernel = "pending"
)

// Valid reports whether the kernel is one of the declared kinds.
// An unknown kernel fails closed rather than defaulting to anything.
func (k FactorKernel) Valid() bool {
	switch k {
	case KernelLeaf, KernelFormula, KernelCode, KernelPending:
		return true
	default:
		return false
	}
}

// FactorDefinition 是一条因子定义的完整骨架。
//
// 字段分三组：骨架字段（所有内核型共有）、公式字段（Formula/Validity，
// leaf/formula 型）、代码字段（CodeRef，code 型）。VersionHash 不落
// 结构体——它是内容的方法，存进来就给了「篡改哈希字段」的入口。
type FactorDefinition struct {
	// Name 必须落在冻结名册内（ParseFactorType 可解析）。名册本身仍是
	// 冻结协议；D3 改变的是「定义从代码冻结变为库管理」，不是「谁可以
	// 是新因子」。
	Name FactorType `json:"name"`

	Kernel FactorKernel `json:"kernel"`

	// Formula 是 leaf/formula 型的公式文本，pkg/expression DSL 语法
	// （如 "close / ts_delay(close, 20) - 1"）。leaf 型同样用公式
	// 表达（单 identifier），不在类型上区分两种解析路径。
	Formula string `json:"formula,omitempty"`

	// Validity 是可选的域过滤公式（同 Formula 语法）：与 Formula 同形
	// 求值，逐点非零（且非 NaN）的位置才产出分数。
	//
	// 为什么需要它：原代码版因子自带输入域过滤（value 要求 PE>0、
	// quality 要求 ROE>0、momentum 要求两端价格>0），公式引擎没有
	// 三元表达式，直接在公式里乘 0/1 掩码会把域外值变成**有效分数 0**
	// ——那正是「缺失≠0」铁律要禁的。Validity 把「算得出数」和「数在
	// 定义域内」分成两路，域外即缺失。
	Validity string `json:"validity,omitempty"`

	// CodeRef 是 code 型内核的 Go 出口标识（如 "ComputeGrossMarginTrend
	// Factor"）。求值方按此标识查 closure 表；未知标识 fail-closed。
	CodeRef string `json:"code_ref,omitempty"`

	// Caliber 是口径标签：z-score / rank / raw。随分数走——苹果橘子
	// 不可加权混用（蓝图 §5.2 产出 schema）。
	Caliber FactorCaliber `json:"caliber"`

	// InputFields 是本因子消耗的规范层字段名（D1 字段词表 / expression
	// fieldRegistry 的合法名）。formula 型还会与公式里的 identifier
	// 双向核对；code 型供可用性探测与依赖盘点；pending 型无内核即无
	// 输入，允许为空。
	InputFields []string `json:"input_fields"`

	// EffectiveFrom 是生效日（YYYY-MM-DD）：该日起定义参与计算。
	EffectiveFrom string `json:"effective_from"`

	// Lookahead 是前视纪律声明：本因子的输入按什么时间点对齐。
	// 必填——「防偷看从约定变成结构」（蓝图 §5.2）。横截面行情因子
	// 为 "trade_date"；纵向财报因子为 "ann_date<=trade_date"。
	Lookahead string `json:"lookahead"`

	// Lookback 是公式型求值窗口（自然日回溯上限）。code 型留 0。
	Lookback int `json:"lookback,omitempty"`
}

// factorDefCanonical 是版本哈希的规范化投影。
//
// 与 FactorDefinition 分开的原因：JSON tag 的 omitempty 行为面向存储，
// 而哈希必须对「同一份定义的任何序列化差异」免疫——故用一张全字段
// （无 omitempty）的固定表，InputFields 先排序。
type factorDefCanonical struct {
	Name          string   `json:"name"`
	Kernel        string   `json:"kernel"`
	Formula       string   `json:"formula"`
	Validity      string   `json:"validity"`
	CodeRef       string   `json:"code_ref"`
	Caliber       string   `json:"caliber"`
	InputFields   []string `json:"input_fields"`
	EffectiveFrom string   `json:"effective_from"`
	Lookahead     string   `json:"lookahead"`
	Lookback      int      `json:"lookback"`
}

func (d FactorDefinition) canonical() factorDefCanonical {
	fields := make([]string, len(d.InputFields))
	copy(fields, d.InputFields)
	sort.Strings(fields)
	return factorDefCanonical{
		Name:          string(d.Name),
		Kernel:        string(d.Kernel),
		Formula:       d.Formula,
		Validity:      d.Validity,
		CodeRef:       d.CodeRef,
		Caliber:       string(d.Caliber),
		InputFields:   fields,
		EffectiveFrom: d.EffectiveFrom,
		Lookahead:     d.Lookahead,
		Lookback:      d.Lookback,
	}
}

// VersionHash 返回本定义的版本指纹：canonical JSON 的 SHA-256（hex）。
//
// 改公式 = 新哈希天然成立（公式进 canonical 投影）；老版本定义对象
// 只要还在（DB/测试夹具），重放即复现。方法而非字段：没有可篡改的
// 存储入口，「库里存的哈希」在加载时重新 derive 校验。
func (d FactorDefinition) VersionHash() string {
	b, err := json.Marshal(d.canonical())
	if err != nil {
		// Unreachable：canonical 结构全为 string/int/[]string。
		panic(fmt.Sprintf("factor definition canonical marshal: %v", err))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Validate 对定义做结构校验（fail-closed）。
//
// 这里只查「定义自身」是否自洽；「公式是否可解析」「输入字段是否在
// 语言注册表内」属于跨层校验，由 data 包 registry 加载时执行
// （domain 不 import expression，保持依赖方向干净）。
func (d FactorDefinition) Validate() error {
	if _, ok := ParseFactorType(string(d.Name)); !ok {
		return fmt.Errorf("factor definition %q: name not in frozen registry", d.Name)
	}
	if !d.Kernel.Valid() {
		return fmt.Errorf("factor definition %q: unknown kernel %q", d.Name, d.Kernel)
	}
	if !d.Caliber.Valid() {
		return fmt.Errorf("factor definition %q: unknown caliber %q", d.Name, d.Caliber)
	}
	if d.Lookahead == "" {
		return fmt.Errorf("factor definition %q: lookahead rule is mandatory (blueprint §5.2)", d.Name)
	}
	if _, err := time.Parse("2006-01-02", d.EffectiveFrom); err != nil {
		return fmt.Errorf("factor definition %q: effective_from must be YYYY-MM-DD: %w", d.Name, err)
	}
	switch d.Kernel {
	case KernelLeaf, KernelFormula:
		if d.Formula == "" {
			return fmt.Errorf("factor definition %q: kernel %s requires formula", d.Name, d.Kernel)
		}
		if d.CodeRef != "" {
			return fmt.Errorf("factor definition %q: kernel %s must not carry code_ref", d.Name, d.Kernel)
		}
	case KernelCode:
		if d.CodeRef == "" {
			return fmt.Errorf("factor definition %q: kernel code requires code_ref", d.Name)
		}
		if d.Formula != "" {
			return fmt.Errorf("factor definition %q: kernel code must not carry formula", d.Name)
		}
	case KernelPending:
		// 名册在册、内核待定：公式与出口都必须为空（有任何一个就不该
		// 停在 pending）；输入字段随内核缺席，允许为空。
		if d.Formula != "" || d.CodeRef != "" {
			return fmt.Errorf("factor definition %q: kernel pending must carry neither formula nor code_ref", d.Name)
		}
		if len(d.InputFields) > 0 {
			return fmt.Errorf("factor definition %q: kernel pending must not declare input fields", d.Name)
		}
	}
	if len(d.InputFields) == 0 && d.Kernel != KernelPending {
		return fmt.Errorf("factor definition %q: input_fields must name at least one canonical field", d.Name)
	}
	seen := make(map[string]bool, len(d.InputFields))
	for _, f := range d.InputFields {
		if f == "" {
			return fmt.Errorf("factor definition %q: empty field name in input_fields", d.Name)
		}
		if seen[f] {
			return fmt.Errorf("factor definition %q: duplicate input field %q", d.Name, f)
		}
		seen[f] = true
	}
	return nil
}
