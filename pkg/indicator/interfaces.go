// Package indicator — indicators 模块（K0 切片 2 契约冻结）。
//
// 模块职责（蓝图 §5 indicators 行）：L2 有状态算子（EWMA / RMA / IIR /
// Kalman）+ 无状态因子库，被策略调用，warmup 静态推导。
//
// **L2 优先（D2 已拍板采纳，蓝图 §6.3）**：能用 L2 表达的，不允许上 L3。
// 即——EWMA / RMA / IIR / Kalman 这类**确定性、可枚举、warmup 可静态
// 推导、可三路属性测试**（Batch ≡ Step ≡ Step-from-persisted）的算子，
// 一律走本包的**确定性算子**实现，**不上 WASM**。WASM（L3a）是图灵完备
// 兜底，只用于 L2 表达不了的（自定义状态机、非标准滤波）。这维持
// ADR-024 的「自由度是负债」：表达力逐层放大，但每层都先问「上一层
// 能不能做」。
//
// 新建包的理由：现有 pkg/ai/expression（operators.go / evaluator.go）
// 是 **L1 无状态表达式算子**（28 个算子全部 causal=true、state=false，
// 见 ADR-028 §4 存量算子表），没有 ts_ewma / ts_rma / ts_kalman 等
// L2 有状态算子。本包是它们的家，与 expression 分层而不混层。
//
// K0 切片 2：本文件只冻结接口契约，stub 方法体固定
// panic("contract stub: not implemented")，K1+ 实现直接替换 stub。
package indicator

import (
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// Indicator 是有状态算子的统一抽象（蓝图 §5 indicators 行：
// Update / Value / Reset / Warmup，fail-loud）。
//
// 方法语义（冻结）：
//   - Update：喂一根 bar 推进内部状态。**&mut self 语义**（有状态：
//     同一算子对象连续 Update 才得到正确序列）。Update 失败必须返回
//     error（fail-loud），不吞异常、不回退到上一值。
//   - Value：取当前值。**warmup 未完成（已喂 bar 数 < Warmup()）时
//     必须返回 error**，绝不返回未预热的部分值——返回半成品等于让策略
//     拿噪声下单。
//   - Warmup：需要多少根 bar 才能产出首个有效值——**可静态推导**
//     （ADR-028 §4：可以是参数的函数，如 ts_ewma 的
//     ln(1e-6)/ln(1-α)，α=0.3 约 20 根，α=0.05 约 60 根；
//     **不能硬编码成一个常数**）。
//   - Reset：清空状态回到初始态（换 run / 换 symbol 时调用）。
type Indicator interface {
	Name() string
	Update(bar domain.OHLCV) error
	Value() (float64, error)
	Warmup() int
	Reset()
}

// OperatorSpec 是算子声明契约——对齐 ADR-028 §4 的 7 项，缺一不可注册。
//
// 逐项语义（ADR-028 §4 原文）：
//   - Signature：类型签名，如 `ts_mean : Series × Scalar → Series`；
//   - Lookback：最大回看窗口（int | ∞；∞ 在本结构里以 0 表示，
//     由 State=true 配合理解——无限回看必须走递推状态）；
//   - Causal：是否只看过去。**false 者一律拒绝注册**（前视算子不许进内核）；
//   - State：是否有状态。true = L2 递推算子（ts_rma / ts_ewma /
//     ts_kalman / ts_iir / ts_cumsum 等），**必须提供 Batch + Step
//     双实现**，以满足 ADR-028 §7 的三路一致性；
//   - Warmup：预热期（可为参数的函数，见上）；
//   - Init：初始化规则（如 ts_rma 前 N 根 SMA = Wilder 定义；
//     ts_ewma 首值 = x[0]）；
//   - NaNPolicy：NaN / unknown 的传播规则（停牌 = unknown，见
//     ADR-028 §9）。
//
// K0 只冻结这 7 项的**载体**，不做注册表与校验器（属 K1+）。
type OperatorSpec struct {
	Name      string
	Signature string
	Lookback  int
	Causal    bool
	State     bool
	Warmup    int
	Init      float64
	NaNPolicy string
}

// ─── Contract stubs（K1+ 实现替换，勿在此写实现逻辑） ───────────────

// BaseIndicator 是 L2 确定性有状态算子的契约 stub（如 ts_ewma / ts_rma
// / ts_kalman；它们走本包的确定性实现，不上 WASM）。
type BaseIndicator struct{}

// Name 返回算子名（与 OperatorSpec.Name 一致）。
func (i *BaseIndicator) Name() string { panic("contract stub: not implemented") }

// Update 喂一根 bar 推进内部状态（&mut self）。
func (i *BaseIndicator) Update(bar domain.OHLCV) error { panic("contract stub: not implemented") }

// Value 取当前值；warmup 未完成返回 error（fail-loud）。
func (i *BaseIndicator) Value() (float64, error) { panic("contract stub: not implemented") }

// Warmup 返回预热所需 bar 数（可静态推导）。
func (i *BaseIndicator) Warmup() int { panic("contract stub: not implemented") }

// Reset 清空状态回到初始态。
func (i *BaseIndicator) Reset() { panic("contract stub: not implemented") }

// ─── 编译期合规检查 + 方法存在性守卫 ────────────────────────────────
//
// 第一行：stub 漂移出接口时 go build 失败（切片 1 既有样板）。
//
// 方法表达式守卫（其后各行）：从 Indicator 删除任一方法，
// Indicator.<Method> 即未定义，本包 go build 直接编译失败。
var (
	_ Indicator = (*BaseIndicator)(nil)

	_ func(Indicator) string              = Indicator.Name
	_ func(Indicator, domain.OHLCV) error = Indicator.Update
	_ func(Indicator) (float64, error)    = Indicator.Value
	_ func(Indicator) int                 = Indicator.Warmup
	_ func(Indicator)                     = Indicator.Reset
)
