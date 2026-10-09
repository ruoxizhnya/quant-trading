// EWMA（指数加权移动平均）。
//
// ─── 声明（K3 切片 2：由 Spec() 产出 OperatorSpec） ────────────────────
//
//	causal  = true
//	state   = true
//	warmup  = max(1, ceil(ln(1e-6)/ln(1-α)))   （ADR-028 §4）
//	init    = 首值 = x[0]                        （ADR-028 §4）
//	lookback= ∞（以递推状态表达）
//
// ─── 递推定义 ──────────────────────────────────────────────────────
//
//	首值：prev = x[0]
//	此后：prev = α·x + (1-α)·prev
//
// α ∈ (0, 1]；α=1 退化为恒等（prev = x）。
//
// ─── ⚠️ ADR-028 §4 订正记录（数字与公式矛盾，以公式 + tol=1e-6 为准） ──
//
// ADR-028 §4 正文写「ts_ewma(x, α) 的 warmup ≈ ln(tol)/ln(1-α)，α=0.3 时
// 约 20 根，α=0.05 时约 60 根」。但用该 ADR 自己给的公式、tol 取本表 init/
// warmup 契约隐含的 1e-6，实算：
//
//	α=0.30 → ln(1e-6)/ln(0.70) ≈ 38.7  → 39 根   （非 20）
//	α=0.05 → ln(1e-6)/ln(0.95) ≈ 269.1 → 270 根  （非 60）
//	α=0.50 → 19.93 → 20 根                        （≈20 对应的是 α=0.5）
//	「约 60」对应 tol ≈ 5e-2（0.95^60 ≈ 0.046）
//
// 即 §4 的两个「约 N 根」样例数字与公式不自洽（疑似把 α=0.5 的 20 根与
// 某个较大 tol 的 60 根混入了 α=0.3/0.05 的行）。**本实现以公式
// max(1, ceil(ln(1e-6)/ln(1-α))) 为唯一真相**，即：
//   - α=0.5  → warmup=20（与 §4 样例数值巧合一致）
//   - α=0.9  → warmup=6
//   - α=1    → warmup=1（特判）
//
// 此矛盾供后续 ADR-028 修订时更正 §4 的样例数字。
//
// ─── Batch 对偶 ────────────────────────────────────────────────────
//
// EWMABatch 与 Step 共享同一份递推核 ewmaCore。前 warmup-1 位为 NaN。
package indicator

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

const ewmaStateVersion = 1

// ewmaCore 是 EWMA 的递推核——Step 与 Batch 共用。
type ewmaCore struct {
	alpha float64
	warm  int     // 构造期静态推导的 warmup（供 ready() 与 Warmup() 一致使用）
	count int     // 已喂标量数
	prev  float64 // 当前 EWMA 值（count >= 1 后有效）
}

func newEWMACore(alpha float64) ewmaCore {
	return ewmaCore{alpha: alpha, warm: ewmaWarmup(alpha)}
}

// step 推进一格。调用方必须已校验 x 有限。
func (c *ewmaCore) step(x float64) {
	c.count++
	if c.count == 1 {
		c.prev = x
		return
	}
	c.prev = c.alpha*x + (1-c.alpha)*c.prev
}

func (c *ewmaCore) ready() bool    { return c.count >= c.warm }
func (c *ewmaCore) value() float64 { return c.prev }

// ewmaWarmup 按 ADR-028 §4 公式静态推导 warmup：ceil(ln(1e-6)/ln(1-α))；
// α=1 特判为 1；下界 1（首个观测即有值）。
func ewmaWarmup(alpha float64) int {
	if alpha >= 1 {
		return 1
	}
	w := int(math.Ceil(math.Log(1e-6) / math.Log(1-alpha)))
	if w < 1 {
		return 1
	}
	return w
}

// ewmaState 是 EWMA 的版本化序列化载体（含全部内部量）。
type ewmaState struct {
	Version int     `json:"version"`
	Count   int     `json:"count"`
	Prev    float64 `json:"prev"`
}

// EWMA 是指数加权移动平均算子（有状态）。
type EWMA struct{ core ewmaCore }

// NewEWMA 构造 EWMA，α ∈ (0, 1]。参数非法返回 error（fail-loud）。
func NewEWMA(alpha float64) (*EWMA, error) {
	if !(alpha > 0 && alpha <= 1) {
		return nil, fmt.Errorf("indicator: NewEWMA α=%v 非法（要求 0 < α ≤ 1）", alpha)
	}
	return &EWMA{core: newEWMACore(alpha)}, nil
}

// Name 返回算子名（与切片 2 的 OperatorSpec.Name 一致）。
func (e *EWMA) Name() string { return "ts_ewma" }

// Update 喂一个标量 x 推进内部状态。非有限输入 → error 且状态逐位不变（原子）。
func (e *EWMA) Update(x float64) error {
	if !isFinite(x) {
		return fmt.Errorf("indicator: EWMA.Update 收到非有限输入 %v（拒绝，状态不变）", x)
	}
	e.core.step(x)
	return nil
}

// Value 取当前值；warmup 未完成（已喂 < warmup）返回 error，绝不返回半成品。
func (e *EWMA) Value() (float64, error) {
	if !e.core.ready() {
		return 0, fmt.Errorf("indicator: EWMA.Value warmup 未完成（已喂 %d 根，需 %d 根）", e.core.count, e.core.warm)
	}
	return e.core.value(), nil
}

// Warmup 返回预热所需标量数（= max(1, ceil(ln(1e-6)/ln(1-α)))）。
func (e *EWMA) Warmup() int { return e.core.warm }

// Spec 产出本实例的算子声明（ADR-028 §4 七项）。
//
// 参数化算子：Warmup 随 α 变化（EWMA(α).Spec().Warmup ==
// ceil(ln(1e-6)/ln(1-α))），由实例给出。
func (e *EWMA) Spec() OperatorSpec {
	return OperatorSpec{
		Name:      "ts_ewma",
		Signature: "ts_ewma : Series × Scalar → Series",
		Lookback:  0, // ∞：递推状态表达（K0 约定：0 + State=true）
		Causal:    true,
		State:     true,
		Warmup:    e.Warmup(),
		Init:      "x[0]", // 首值 = 第一个观测
		NaNPolicy: "unknown→NaN",
	}
}

// Reset 清空状态回到初始态（保留参数 α）。
func (e *EWMA) Reset() { e.core = newEWMACore(e.core.alpha) }

// SaveState 序列化内部状态为版本化 JSON。
func (e *EWMA) SaveState() ([]byte, error) {
	return json.Marshal(ewmaState{
		Version: ewmaStateVersion,
		Count:   e.core.count,
		Prev:    e.core.prev,
	})
}

// LoadState 反序列化状态；不合法输入返回 error，原子提交（失败不改动接收者）。
func (e *EWMA) LoadState(b []byte) error {
	var s ewmaState
	if err := decodeVersionedState(b, &s, ewmaStateVersion, "version", "count", "prev"); err != nil {
		return err
	}
	if s.Count < 0 {
		return fmt.Errorf("indicator: EWMA.LoadState count=%d 非法", s.Count)
	}
	if !isFinite(s.Prev) {
		return errors.New("indicator: EWMA.LoadState 状态含非有限值")
	}
	// 原子提交：以上校验全部通过才改动接收者。
	e.core.count = s.Count
	e.core.prev = s.Prev
	return nil
}

// EWMABatch 是 EWMA 的批量对偶实现——与 Step 共享同一份递推核 ewmaCore。
//
//	返回值长度 == len(xs)；前 warmup-1 位为 NaN（=未预热/unknown）。
//	空 / nil 输入 → 返回空、无错。
//	任一 x 非有限 → error（与 Step 同规）。
func EWMABatch(xs []float64, alpha float64) ([]float64, error) {
	if !(alpha > 0 && alpha <= 1) {
		return nil, fmt.Errorf("indicator: EWMABatch α=%v 非法（要求 0 < α ≤ 1）", alpha)
	}
	out := make([]float64, len(xs))
	c := newEWMACore(alpha)
	for i, x := range xs {
		if !isFinite(x) {
			return nil, fmt.Errorf("indicator: EWMABatch 第 %d 个输入非有限 %v", i, x)
		}
		c.step(x)
		if c.ready() {
			out[i] = c.value()
		} else {
			out[i] = math.NaN()
		}
	}
	return out, nil
}
