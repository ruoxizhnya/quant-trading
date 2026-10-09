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
// ─── 2026-10-08 · K3 切片 1 契约变更（Update 标量化 + Save/Load 状态序列化） ───
//
// 变更一：Indicator.Update 由 Update(bar domain.OHLCV) 改为 **Update(x float64)**。
//   - 裁决理由：ADR-028 §7 的算子契约是 `Step(x float64, state *S)` ——
//     RMA / EWMA / Kalman 是**标量序列**上的递推核；bar 级抽取（Close /
//     TrueRange / Volume）是调用方（或薄适配）的职责。原 Update(bar) 让
//     「RMA over TrueRange」这类**复合输入在类型上无法表达**（TrueRange
//     本身依赖上一根 bar 的 Close，是一个 bar 级抽取，不属于标量递推核）。
//     标量化后，递推核只认 float64，复合输入的组装权回到调用方。
//
// 变更二：新增 SaveState() ([]byte, error) / LoadState([]byte) error。
//   - 裁决理由：ADR-028 §7 原文「这个不对称必须在算子接口设计时就承认：
//     **如果接口不预留状态序列化，后面补不进去**」—— Step-from-persisted
//     是三路一致性（Batch ≡ Step ≡ Step-from-persisted）的一路，契约必须
//     承载，否则实盘进程重启后 EWMA/ATR 从头算，与回测静默漂移。
//   - 语义照 strategy.BarHandler 先例（checkpoint.go / streaming.go）：
//     LoadState 对不合法输入返回 error、**原子提交**（失败不改动接收者）、
//     不静默重置为初始态。
package indicator

// Indicator 是有状态算子的统一抽象（蓝图 §5 indicators 行：
// Update / Value / Reset / Warmup，fail-loud）。
//
// 方法语义（冻结）：
//   - Update：喂一个标量 x 推进内部状态。**&mut self 语义**（有状态：
//     同一算子对象连续 Update 才得到正确序列）。Update 失败必须返回
//     error（fail-loud），不吞异常、不回退到上一值，且**原子**——任何
//     error 路径不得部分改动内部状态。
//   - Value：取当前值。**warmup 未完成（已喂标量数 < Warmup()）时
//     必须返回 error**，绝不返回未预热的部分值——返回半成品等于让策略
//     拿噪声下单。
//   - Warmup：需要多少根 bar 才能产出首个有效值——**可静态推导**
//     （ADR-028 §4：可以是参数的函数，如 ts_ewma 的
//     ln(1e-6)/ln(1-α)；**不能硬编码成一个常数**）。
//   - Reset：清空状态回到初始态（换 run / 换 symbol 时调用）。
//   - SaveState / LoadState：状态的序列化 / 反序列化（断点续跑、
//     回测-实盘迁移、三路一致性的第三路）。LoadState 不合法输入返回
//     error 且原子提交（失败不改动接收者）。
type Indicator interface {
	Name() string
	Update(x float64) error
	Value() (float64, error)
	Warmup() int
	Reset()
	SaveState() ([]byte, error)
	LoadState([]byte) error
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
// ─── 2026-10-09 · K3 切片 2 契约变更（Init 改型：float64 → string） ───
//
// 变更：Init 由 **float64 改为 string**（受控词表）。
//   - 裁决理由：ADR-028 §4 的 init 列填的是**规则**（「ts_rma 前 N 根 SMA」、
//     「ts_ewma 首值 = x[0]」、「ts_iir 零初始状态」…），float64 一个数都装
//     不下这些真实规则 —— 与 K3 切片 1 的 `Update(bar)→Update(x float64)` 同
//     一类问题（载体类型表达不了契约语义）。沿用 K0-P2-3 流程：改型 + 同步
//     守卫/合规测试 + 注释写裁决 + 破坏验证。
//   - 受控词表（本次落地的取值，未在词表内即违约）：
//     "none"           无初始化规则（无状态算子）
//     "x[0]"           首值 = 第一个观测（ts_ewma / ts_drawdown 的 peak）
//     "sma(first,N)"   前 N 根简单均值作种子（ts_rma = Wilder 定义）
//     "p0=r"           初始方差取观测噪声 r（ts_kalman 的弥散先验极限）
//     "zero"           零初始状态（ts_iir / ts_cumsum）
//     "inf(never)"     初始为「从未发生」（ts_since 的哨兵）
//
// K0 只冻结这 7 项的**载体**，不做注册表与校验器（属 K1+）。
type OperatorSpec struct {
	Name      string
	Signature string
	Lookback  int
	Causal    bool
	State     bool
	Warmup    int
	Init      string
	NaNPolicy string
}

// ─── Contract stubs（K1+ 实现替换，勿在此写实现逻辑） ───────────────

// BaseIndicator 是 L2 确定性有状态算子的契约 stub（如 ts_ewma / ts_rma
// / ts_kalman；它们走本包的确定性实现，不上 WASM）。
type BaseIndicator struct{}

// Name 返回算子名（与 OperatorSpec.Name 一致）。
func (i *BaseIndicator) Name() string { panic("contract stub: not implemented") }

// Update 喂一个标量 x 推进内部状态（&mut self）。
func (i *BaseIndicator) Update(x float64) error { panic("contract stub: not implemented") }

// Value 取当前值；warmup 未完成返回 error（fail-loud）。
func (i *BaseIndicator) Value() (float64, error) { panic("contract stub: not implemented") }

// Warmup 返回预热所需 bar 数（可静态推导）。
func (i *BaseIndicator) Warmup() int { panic("contract stub: not implemented") }

// Reset 清空状态回到初始态。
func (i *BaseIndicator) Reset() { panic("contract stub: not implemented") }

// SaveState 序列化内部状态（版本化；断点续跑 / 三路一致性第三路）。
func (i *BaseIndicator) SaveState() ([]byte, error) { panic("contract stub: not implemented") }

// LoadState 反序列化状态；不合法输入返回 error 且原子提交（失败不改动接收者）。
func (i *BaseIndicator) LoadState(b []byte) error { panic("contract stub: not implemented") }

// ─── 编译期合规检查 + 方法存在性守卫 ────────────────────────────────
//
// 第一行：stub 漂移出接口时 go build 失败（切片 1 既有样板）。
//
// 方法表达式守卫（其后各行）：从 Indicator 删除任一方法，
// Indicator.<Method> 即未定义，本包 go build 直接编译失败。
var (
	_ Indicator = (*BaseIndicator)(nil)

	_ func(Indicator) string           = Indicator.Name
	_ func(Indicator, float64) error   = Indicator.Update
	_ func(Indicator) (float64, error) = Indicator.Value
	_ func(Indicator) int              = Indicator.Warmup
	_ func(Indicator)                  = Indicator.Reset
	_ func(Indicator) ([]byte, error)  = Indicator.SaveState
	_ func(Indicator, []byte) error    = Indicator.LoadState
)
