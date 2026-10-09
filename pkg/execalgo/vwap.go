// K4：VWAP（Volume-Weighted Average Price）执行算法。
//
// 与 TWAP 同构，区别只在**数量分配**：TWAP 等量，VWAP 按窗口内成交量
// 分布加权——第 i 片的数量占比 == 第 i 段成交量占比，使成交节奏贴合
// 市场真实换手，降低与市场 VWAP 基准的偏离。
//
// 本文件只实现算法本身，不接发单主路径（同 twap.go）。
package execalgo

import (
	"context"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// VWAP 是成交量加权拆单算法。
//
// profile 形状（裁决）：`[]float64`，第 i 个元素是窗口内第 i 段的**成交量
// 权重**（原始量即可，不要求归一化——算法内部归一化）。选 []float64 而非
// []domain.OHLCV 的理由：VWAP 只需要「每段相对成交量」，OHLCV 的
// Open/High/Low/Close 全是无关字段，传 OHLCV 会诱导算法去读价格（成交量
// 预测属非目标）。由调用方负责把 OHLCV 序列折算成 profile。
//
// 片数策略：NewVWAP(slices, profile) 中 slices > 0 → 固定片数；否则取
// len(profile)；profile 也为空 → 按窗口时长推导（同 TWAP）。
type VWAP struct {
	slices   int
	profile  []float64
	degraded bool // 最近一次 Schedule 是否退化为等分
	st       planState
}

// NewVWAP 构造 VWAP 算法。
//
// profile 为空 / 全 0 / 长度与最终片数不匹配 / 含负值 → Schedule 退化为
// 等分（等价 TWAP），并通过 Degraded() 暴露退化事实（不静默产垃圾）。
func NewVWAP(slices int, profile []float64) *VWAP {
	return &VWAP{slices: slices, profile: profile}
}

// Name 返回注册表键。
func (a *VWAP) Name() string { return "vwap" }

// Schedule 把父订单排成按成交量分布加权的子订单计划。
//
// 除数量分配外的语义与 TWAP 完全同构：Σ 精确、SubmitAt 等差、ID 幂等、
// 非法输入 error。
func (a *VWAP) Schedule(ctx context.Context, parent ParentOrder) ([]ChildOrder, error) {
	if err := validateParent(parent); err != nil {
		return nil, err
	}

	n := a.slices
	if n <= 0 {
		if len(a.profile) > 0 {
			n = len(a.profile)
		} else {
			n = deriveSlices(parent.EndAt.Sub(parent.StartAt))
		}
	}
	n = effectiveSlices(n, parent.Qty)

	weights, degraded := vwapWeights(n, a.profile)
	a.degraded = degraded

	qtys := allocateQty(parent.Qty, weights)
	times := splitWindow(parent.StartAt, parent.EndAt, n)
	plan := buildPlan(parent, qtys, times)

	a.st.reset(parent, plan)
	return plan, nil
}

// vwapWeights 由原始成交量 profile 计算归一化权重。
//
// 退化规则（裁决）：profile 与片数不匹配、profile 为空、权重和为 0、
// 或含负值（成交量不可能为负，属调用方 bug）→ 全部等权 1/n 并置
// degraded=true。退化**必须可观测**，故由 Schedule 记录到 a.degraded。
func vwapWeights(n int, profile []float64) (weights []float64, degraded bool) {
	weights = make([]float64, n)
	if len(profile) == n && n > 0 {
		var sum float64
		ok := true
		for _, v := range profile {
			if v < 0 {
				ok = false
				break
			}
			sum += v
		}
		if ok && sum > 0 {
			for i := range profile {
				weights[i] = profile[i] / sum
			}
			return weights, false
		}
	}
	for i := range weights {
		weights[i] = 1.0 / float64(n)
	}
	return weights, true
}

// Degraded 返回最近一次 Schedule 是否因 profile 非法 / 不匹配而退化为等分。
func (a *VWAP) Degraded() bool { return a.degraded }

// OnBar 推进拆单节奏（同 TWAP）。
func (a *VWAP) OnBar(ctx context.Context, bar domain.OHLCV) error {
	return a.st.onBar(bar)
}

// OnFill 累计成交进度（同 TWAP）。
func (a *VWAP) OnFill(ctx context.Context, f Fill) error {
	return a.st.onFill(f)
}

// DueChildOrders 取走并清空已到点、待报送的子单。
func (a *VWAP) DueChildOrders() []ChildOrder { return a.st.dueChildOrders() }

// Progress 返回 (已成交量, 总量, 是否全部成交)。
func (a *VWAP) Progress() (filled, total float64, done bool) { return a.st.progress() }

// 编译期守卫：VWAP 满足 ExecAlgorithm（K0 冻结契约）。
var _ ExecAlgorithm = (*VWAP)(nil)
