package data

import (
	"context"
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/expression"
)

// ─── D3 因子注册表：从「代码冻结」到「库管理」 ─────────────────────────────
//
// 蓝图 §6 三期①：因子定义从 Go 代码改为库里公式（名字 + 公式 + 输入字段 +
// 版本哈希），计算线读公式现算/缓存；因子名注册表从「代码冻结」改「库管理」。
//
// 本文件是「库」的进程内形态：BuiltinFactorDefinitions 是 11 个冻结因子
// 的公式化定义种子（唯一写入口），Registry 是读路径（单一事实源）。
// DB 侧形态见 pkg/storage/factor_definition.go（factor_definitions 表），
// 由 analysis 服务启动时 SyncFactorDefinitions 幂等 upsert——库与内存
// 同源，哈希防漂移。
//
// 分型依据（D3 ③3 判据）：
//
//   - momentum / value：公式型——DSL 现成（ts_delay + 算术）；
//   - quality：叶子型——字段直取；
//   - 纵向 5 因子：代码型——单季差分 / TTM 桥接 / 跨期趋势需要财报期
//     坐标（periodOf/shiftQuarters/ttm），表达引擎的求值模型是日频数字
//     序列，没有「季」这个维度，硬凑公式必然改变数学定义 → 保留 Go 出口；
//   - size / volatility / growth：pending 型——冻结名册里有名字，但计算
//     线从未实现（全仓无 Compute* 出口），没有可公式化的现算数值。
//     D3 不发明数学定义（那超出「不改任何因子数学定义」的边界），登记
//     为「名册在册、内核待定」，查询 fail-closed。

// ErrFactorNotRegistered 是未登记因子名的 fail-closed 报错（哨兵）。
// 调用方用 errors.Is 识别；报错文本带可用清单（见 Registry.FailClosed）。
var ErrFactorNotRegistered = fmt.Errorf("factor not in registry")

// ErrFactorKernelUndefined 是 pending 型因子的 fail-closed 报错：名册
// 在册、计算内核待定。与「未登记」区分——前者是库里有定义但没内核，
// 后者是库里根本没有这个名字。
var ErrFactorKernelUndefined = fmt.Errorf("factor kernel not yet defined")

// codeKernelFunc 是代码型内核的统一出口签名。参数（如 momentum 的
// lookback）在闭包绑定时固定，registry 只认两参形态。
type codeKernelFunc func(ctx context.Context, date time.Time) error

// factorCodeKernels 把 CodeRef 绑到 FactorComputer 的方法上。
//
// 新增代码型因子 = BuiltinFactorDefinitions 加一条 + 这里加一行——
// 「加数据类=映射加一行」的同款纪律（D1 翻译层）。
var factorCodeKernels = map[string]func(*FactorComputer) codeKernelFunc{
	"ComputeGrossMarginTrendFactor":       func(f *FactorComputer) codeKernelFunc { return f.ComputeGrossMarginTrendFactor },
	"ComputeContractLiabilityRatioFactor": func(f *FactorComputer) codeKernelFunc { return f.ComputeContractLiabilityRatioFactor },
	"ComputeOCFToNetProfitFactor":         func(f *FactorComputer) codeKernelFunc { return f.ComputeOCFToNetProfitFactor },
	"ComputeROEDuPontLeverageFactor":      func(f *FactorComputer) codeKernelFunc { return f.ComputeROEDuPontLeverageFactor },
	"ComputeInventoryTurnoverDeltaFactor": func(f *FactorComputer) codeKernelFunc { return f.ComputeInventoryTurnoverDeltaFactor },
}

// Registry 是因子定义的进程内注册表（「库管理」的读路径）。
//
// 不可变快照：构造时定稿（含跨层校验），之后只读——「同一次回测引用
// 固定版本哈希，重跑结果一致」的结构保证：回测全程持同一 Registry
// 实例，定义不可能在计算中途被换掉。
type Registry struct {
	defs        map[domain.FactorType]domain.FactorDefinition
	order       []domain.FactorType
	codeKernels map[string]func(*FactorComputer) codeKernelFunc
}

// NewRegistry 用种子定义构造注册表，并做全量 fail-closed 校验：
//
//  1. 每条定义过 domain 结构校验（Validate）；
//  2. leaf/formula 型公式必须可解析、过引擎 L1 闸门（算子名/参数个数/
//     identifier 合法）；
//  3. 公式里的 identifier 必须与 InputFields **互相等于**（声明了没用
//     上 = 死声明；用了没声明 = 隐藏依赖，都拒收）；
//  4. InputFields 必须落在表达引擎字段注册表内（D1 规范层字段词表）；
//  5. code 型 CodeRef 必须有对应 Go 出口；
//  6. 全量名字去重。
//
// 任何一条不过即返回 error——注册表加载失败是启动级故障，不允许
// 「带着坏定义跑」。
func NewRegistry(defs []domain.FactorDefinition) (*Registry, error) {
	r := &Registry{
		defs:        make(map[domain.FactorType]domain.FactorDefinition, len(defs)),
		codeKernels: factorCodeKernels,
	}
	for _, d := range defs {
		if _, dup := r.defs[d.Name]; dup {
			return nil, fmt.Errorf("factor %q: duplicate definition in registry", d.Name)
		}
		if err := d.Validate(); err != nil {
			return nil, err
		}
		switch d.Kernel {
		case domain.KernelLeaf, domain.KernelFormula:
			if err := r.validateFormulaDef(d); err != nil {
				return nil, err
			}
		case domain.KernelCode:
			if _, ok := factorCodeKernels[d.CodeRef]; !ok {
				return nil, fmt.Errorf("factor %q: code_ref %q has no Go kernel registered", d.Name, d.CodeRef)
			}
		case domain.KernelPending:
			// 结构校验已覆盖（无公式无出口无输入）。
		}
		r.defs[d.Name] = d
		r.order = append(r.order, d.Name)
	}
	// order 保持定义传入序（种子序 = 原 ComputeAllFactors 硬编码列表序）：
	// 报告顺序是可观测行为，D3 不改它——只把清单来源从硬编码换成库投影。
	return r, nil
}

// validateFormulaDef 做公式型定义的跨层校验（fail-closed ②：公式引用
// 不存在的输入字段 → 加载期报错，而不是求值期算出垃圾分）。
func (r *Registry) validateFormulaDef(d domain.FactorDefinition) error {
	expr, err := expression.NewParser().Parse(d.Formula)
	if err != nil {
		return fmt.Errorf("factor %q: formula does not parse: %w", d.Name, err)
	}
	if err := expr.Validate(); err != nil {
		return fmt.Errorf("factor %q: formula fails L1 gate: %w", d.Name, err)
	}
	// identifier 集合 vs InputFields：双向相等。
	used := make(map[string]bool)
	for _, id := range expression.ExtractInputs(expr.AST) {
		used[id] = true
	}
	declared := make(map[string]bool, len(d.InputFields))
	for _, f := range d.InputFields {
		declared[f] = true
	}
	for id := range used {
		if !declared[id] {
			return fmt.Errorf("factor %q: formula uses field %q not declared in input_fields", d.Name, id)
		}
	}
	for f := range declared {
		if !used[f] {
			return fmt.Errorf("factor %q: input field %q declared but formula never uses it", d.Name, f)
		}
	}
	// InputFields 必须在引擎字段注册表（D1 规范层字段词表）内。
	known := make(map[string]bool)
	for _, f := range expression.AvailableFields() {
		known[f] = true
	}
	for _, f := range d.InputFields {
		if !known[f] {
			return fmt.Errorf("factor %q: input field %q not in canonical field registry", d.Name, f)
		}
	}
	// Validity（域过滤）同 Formula 规则：可解析、过闸门、identifier 已声明。
	if d.Validity != "" {
		vExpr, err := expression.NewParser().Parse(d.Validity)
		if err != nil {
			return fmt.Errorf("factor %q: validity does not parse: %w", d.Name, err)
		}
		if err := vExpr.Validate(); err != nil {
			return fmt.Errorf("factor %q: validity fails L1 gate: %w", d.Name, err)
		}
		for _, id := range expression.ExtractInputs(vExpr.AST) {
			if !declared[id] {
				return fmt.Errorf("factor %q: validity uses field %q not declared in input_fields", d.Name, id)
			}
		}
	}
	return nil
}

// Definition 取一个因子的定义。未登记 → ok=false。
func (r *Registry) Definition(name domain.FactorType) (domain.FactorDefinition, bool) {
	d, ok := r.defs[name]
	return d, ok
}

// Names 返回全部已登记因子名（定义序——种子序即原硬编码列表序，
// 批处理报告顺序兼容）。
func (r *Registry) Names() []domain.FactorType {
	out := make([]domain.FactorType, len(r.order))
	copy(out, r.order)
	return out
}

// VersionHash 返回某因子定义的版本指纹（未登记 → 空串）。
func (r *Registry) VersionHash(name domain.FactorType) string {
	d, ok := r.defs[name]
	if !ok {
		return ""
	}
	return d.VersionHash()
}

// CodeKernel 取代码型因子的计算出口。非代码型或 CodeRef 无绑定 →
// error（fail-closed）。
func (r *Registry) CodeKernel(f *FactorComputer, name domain.FactorType) (codeKernelFunc, error) {
	d, ok := r.defs[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s (available: %v)", ErrFactorNotRegistered, name, r.Names())
	}
	if d.Kernel != domain.KernelCode {
		return nil, fmt.Errorf("factor %q: kernel %s has no Go code kernel", name, d.Kernel)
	}
	bind, ok := r.codeKernels[d.CodeRef]
	if !ok {
		return nil, fmt.Errorf("factor %q: code_ref %q unbound", name, d.CodeRef)
	}
	return bind(f), nil
}

// ComputeFactorByName 按名字分派计算一个因子（registry 驱动入口）。
//
// 分派规则同 registryComputations：code 型走 Go 出口、leaf/formula 型
// 走表达式引擎、pending 型 fail-closed（ErrFactorKernelUndefined）、
// 未登记 fail-closed（ErrFactorNotRegistered，报错带可用清单）。
// handler 与批处理都走这一个入口——「加因子 = 库里加一条定义」，
// 调用方零改动。
func (f *FactorComputer) ComputeFactorByName(ctx context.Context, name domain.FactorType, date time.Time) error {
	def, ok := f.registry.Definition(name)
	if !ok {
		return f.registry.FailClosedError(name)
	}
	switch def.Kernel {
	case domain.KernelCode:
		kernel, err := f.registry.CodeKernel(f, name)
		if err != nil {
			return err
		}
		return kernel(ctx, date)
	case domain.KernelLeaf, domain.KernelFormula:
		return f.ComputeFormulaFactor(ctx, def, date)
	default:
		return f.registry.FailClosedError(name)
	}
}

// FailClosedError 为「按名取因子」的调用方生成带可用清单的报错：
// 未登记 → ErrFactorNotRegistered；pending → ErrFactorKernelUndefined。
// 继承既有语义（ParseFactorType 时代的报错带清单）。
func (r *Registry) FailClosedError(name domain.FactorType) error {
	if _, ok := r.defs[name]; !ok {
		return fmt.Errorf("%w: %q (available: %v)", ErrFactorNotRegistered, name, r.Names())
	}
	d := r.defs[name]
	if d.Kernel == domain.KernelPending {
		return fmt.Errorf("%w: %q (registered in name roster; formula/code kernel not yet defined)", ErrFactorKernelUndefined, name)
	}
	return nil
}
