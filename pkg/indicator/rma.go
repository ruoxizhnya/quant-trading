// RMA（Wilder 平滑 / Wilder's Moving Average）。
//
// ─── 声明（K3 切片 2：由 Spec() 产出 OperatorSpec） ────────────────────
//
//	causal  = true
//	state   = true
//	warmup  = N                      （ADR-028 §4：ts_rma(x, N) warmup=N）
//	init    = 前 N 根 SMA            （= Wilder 定义，第 N 根给出种子）
//	lookback= ∞（以递推状态表达）
//
// ─── 递推定义（Wilder 1978） ────────────────────────────────────────
//
//	未满 N 根：累计简单均值（预热）；
//	第 N 根   ：prev = SMA(first N)         ← 种子
//	此后      ：prev += (x - prev) / N
//
// n=1 退化为恒等（seed = x[0]，之后 prev += (x-prev)/1 = x）。
//
// ─── Batch 对偶 ────────────────────────────────────────────────────
//
// RMABatch 与 Step 共享同一份递推核 rmaCore（**双实现共享核**，不是第二份
// 实现）。前 warmup-1 位为 NaN（=未预热）；空/nil 输入返回空、无错。
package indicator

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

const rmaStateVersion = 1

// rmaCore 是 RMA 的递推核——Step 与 Batch 共用。
type rmaCore struct {
	n     int
	count int     // 已喂标量数
	sum   float64 // 前 n 根的累加和（仅预热期内有效）
	prev  float64 // 当前 RMA 值（count >= n 后有效）
}

func newRMACore(n int) rmaCore { return rmaCore{n: n} }

// step 推进一格。调用方必须已校验 x 有限（原子性由调用方在 step 之前完成）。
func (c *rmaCore) step(x float64) {
	c.count++
	if c.count <= c.n {
		c.sum += x
		if c.count == c.n {
			c.prev = c.sum / float64(c.n)
		}
		return
	}
	c.prev += (x - c.prev) / float64(c.n)
}

func (c *rmaCore) ready() bool    { return c.count >= c.n }
func (c *rmaCore) value() float64 { return c.prev }

// rmaState 是 RMA 的版本化序列化载体（含全部内部量）。
type rmaState struct {
	Version int     `json:"version"`
	Count   int     `json:"count"`
	Sum     float64 `json:"sum"`
	Prev    float64 `json:"prev"`
}

// RMA 是 Wilder 平滑算子（有状态）。
type RMA struct{ core rmaCore }

// NewRMA 构造 RMA，n ≥ 1（n=1 为恒等）。参数非法返回 error（fail-loud，不静默纠正）。
func NewRMA(n int) (*RMA, error) {
	if n < 1 {
		return nil, fmt.Errorf("indicator: NewRMA n=%d 非法（要求 n ≥ 1）", n)
	}
	return &RMA{core: newRMACore(n)}, nil
}

// Name 返回算子名（与切片 2 的 OperatorSpec.Name 一致）。
func (r *RMA) Name() string { return "ts_rma" }

// Update 喂一个标量 x 推进内部状态。非有限输入 → error 且状态逐位不变（原子）。
func (r *RMA) Update(x float64) error {
	if !isFinite(x) {
		return fmt.Errorf("indicator: RMA.Update 收到非有限输入 %v（拒绝，状态不变）", x)
	}
	r.core.step(x)
	return nil
}

// Value 取当前值；warmup 未完成（已喂 < N）返回 error，绝不返回半成品。
func (r *RMA) Value() (float64, error) {
	if !r.core.ready() {
		return 0, fmt.Errorf("indicator: RMA.Value warmup 未完成（已喂 %d 根，需 %d 根）", r.core.count, r.core.n)
	}
	return r.core.value(), nil
}

// Warmup 返回预热所需标量数 = N（可静态推导）。
func (r *RMA) Warmup() int { return r.core.n }

// Spec 产出本实例的算子声明（ADR-028 §4 七项）。
//
// 参数化算子的 Spec 由**实例**给出：Warmup 随构造参数 n 变化（RMA(14) 的
// Warmup == 14），静态注册表装不下 —— 故 expression 侧注册表对 ts_rma 只记
// 静态项，Warmup 走本方法 / 同款公式推导。
func (r *RMA) Spec() OperatorSpec {
	return OperatorSpec{
		Name:      "ts_rma",
		Signature: "ts_rma : Series × Scalar → Series",
		Lookback:  0, // ∞：递推状态表达（K0 约定：0 + State=true）
		Causal:    true,
		State:     true,
		Warmup:    r.Warmup(),
		Init:      "sma(first,N)", // 前 N 根 SMA = Wilder 定义
		NaNPolicy: "unknown→NaN",
	}
}

// Reset 清空状态回到初始态（保留参数 n）。
func (r *RMA) Reset() { r.core = newRMACore(r.core.n) }

// SaveState 序列化内部状态为版本化 JSON。
func (r *RMA) SaveState() ([]byte, error) {
	return json.Marshal(rmaState{
		Version: rmaStateVersion,
		Count:   r.core.count,
		Sum:     r.core.sum,
		Prev:    r.core.prev,
	})
}

// LoadState 反序列化状态；不合法输入返回 error，原子提交（失败不改动接收者）。
func (r *RMA) LoadState(b []byte) error {
	var s rmaState
	if err := decodeVersionedState(b, &s, rmaStateVersion, "version", "count", "sum", "prev"); err != nil {
		return err
	}
	if s.Count < 0 {
		return fmt.Errorf("indicator: RMA.LoadState count=%d 非法", s.Count)
	}
	if !isFinite(s.Sum) || !isFinite(s.Prev) {
		return errors.New("indicator: RMA.LoadState 状态含非有限值")
	}
	// 原子提交：以上校验全部通过才改动接收者。
	r.core.count = s.Count
	r.core.sum = s.Sum
	r.core.prev = s.Prev
	return nil
}

// RMABatch 是 RMA 的批量对偶实现——与 Step 共享同一份递推核 rmaCore。
//
//	返回值长度 == len(xs)；前 warmup-1 位为 NaN（=未预热/unknown，与 Step
//	的 Value() 未预热即 error 对偶：Batch 用 NaN 表达 unknown，Step 用 error
//	表达 fail-loud）。
//	空 / nil 输入 → 返回空、无错（没有任何元素可算，不算错误）。
//	任一 x 非有限 → error（与 Step 同规）。
func RMABatch(xs []float64, n int) ([]float64, error) {
	if n < 1 {
		return nil, fmt.Errorf("indicator: RMABatch n=%d 非法（要求 n ≥ 1）", n)
	}
	out := make([]float64, len(xs))
	c := newRMACore(n)
	for i, x := range xs {
		if !isFinite(x) {
			return nil, fmt.Errorf("indicator: RMABatch 第 %d 个输入非有限 %v", i, x)
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
