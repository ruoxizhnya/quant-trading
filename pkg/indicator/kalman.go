// Kalman（一维线性标量局部水平模型 / local level model）。
//
// ─── 声明（供切片 2 填 OperatorSpec，本切片不产出 Spec()） ────────────
//
//	causal  = true
//	state   = true
//	warmup  = 1          （见下「为什么 warmup=1」）
//	init    = 首观测 x = z；p0 = r（见下「p0 裁决」）
//	lookback= ∞（以递推状态表达）
//
// ─── 模型与递推（标准 1D Kalman，局部水平） ─────────────────────────
//
//	状态：level 估计 x，及其方差 p；
//	观测 z = level + v,  v ~ N(0, r)     （r > 0 观测噪声）
//	水平随机游走 level += w, w ~ N(0, q) （q ≥ 0 状态噪声）
//
//	预热：首观测直接取 x = z、p = p0（见 p0 裁决）；
//	此后（每来一个观测 z）：
//	    p += q                    预测步：先验方差 = 后验 + 状态噪声
//	    k  = p / (p + r)          Kalman 增益
//	    x += k · (z - x)          用新息修正水平
//	    p  = (1 - k) · p          后验方差
//
// ─── p0 裁决：p0 = r ────────────────────────────────────────────────
//
// 首观测之前对 level 无任何信息，即弥散（diffuse / 无信息）先验 p₀⁻ → ∞。
// 在弥散极限下，首观测把后验方差精确压到观测噪声 r：
//
//	k = p⁻/(p⁻+r) → 1 ；  p⁺ = (1-k)·p⁻ = p⁻·r/(p⁻+r) → r
//
// 故取 p0 = r 是「无信息先验 + 一个观测」这一极限的自洽结果，且只用模型
// 已有参数 r，**不引入任何魔法常数**（这是本裁决的核心原则：确定性 +
// 不引入额外自由参数）。等价地：首观测之后「我们对水平的把握」等于
// 「单次观测的噪声方差」——这正是只有一次观测时信息量的物理含义。
//
// ─── 为什么 warmup = 1 ──────────────────────────────────────────────
//
// RMA/EWMA 需要一段平均窗来让初始条件权重衰减，故 warmup > 1。Kalman 是
// 递推最优线性估计器：收到首观测即给出当前状态的最优（最小均方误差）线性
// 估计，无需平均窗。故 warmup = 1，第一根 bar 即有值。
//
// ─── Batch 对偶 ────────────────────────────────────────────────────
//
// KalmanBatch 与 Step 共享同一份递推核 kalmanCore。warmup=1，故无 NaN 前导。
package indicator

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

const kalmanStateVersion = 1

// kalmanCore 是 Kalman 的递推核——Step 与 Batch 共用。
type kalmanCore struct {
	q, r  float64
	p0    float64 // 首观测后的初始方差 = r（见文件头 p0 裁决）
	count int     // 已喂标量数
	x     float64 // 当前水平估计（count >= 1 后有效）
	p     float64 // 当前方差（count >= 1 后有效）
}

func newKalmanCore(q, r float64) kalmanCore {
	return kalmanCore{q: q, r: r, p0: r}
}

// step 推进一格。调用方必须已校验 z 有限。
func (c *kalmanCore) step(z float64) {
	c.count++
	if c.count == 1 {
		c.x = z
		c.p = c.p0
		return
	}
	c.p += c.q
	k := c.p / (c.p + c.r)
	c.x += k * (z - c.x)
	c.p = (1 - k) * c.p
}

func (c *kalmanCore) ready() bool    { return c.count >= 1 }
func (c *kalmanCore) value() float64 { return c.x }

// kalmanState 是 Kalman 的版本化序列化载体（含全部内部量）。
type kalmanState struct {
	Version int     `json:"version"`
	Count   int     `json:"count"`
	X       float64 `json:"x"`
	P       float64 `json:"p"`
}

// Kalman 是一维标量局部水平 Kalman 滤波器（有状态）。
type Kalman struct{ core kalmanCore }

// NewKalman 构造 Kalman，q ≥ 0、r > 0。参数非法返回 error（fail-loud）。
func NewKalman(q, r float64) (*Kalman, error) {
	if !isFinite(q) || q < 0 {
		return nil, fmt.Errorf("indicator: NewKalman q=%v 非法（要求 q ≥ 0 且有限）", q)
	}
	if !isFinite(r) || r <= 0 {
		return nil, fmt.Errorf("indicator: NewKalman r=%v 非法（要求 r > 0 且有限）", r)
	}
	return &Kalman{core: newKalmanCore(q, r)}, nil
}

// Name 返回算子名（与切片 2 的 OperatorSpec.Name 一致）。
func (k *Kalman) Name() string { return "ts_kalman" }

// Update 喂一个标量 z 推进内部状态。非有限输入 → error 且状态逐位不变（原子）。
func (k *Kalman) Update(z float64) error {
	if !isFinite(z) {
		return fmt.Errorf("indicator: Kalman.Update 收到非有限输入 %v（拒绝，状态不变）", z)
	}
	k.core.step(z)
	return nil
}

// Value 取当前值；warmup 未完成（已喂 < 1）返回 error。
func (k *Kalman) Value() (float64, error) {
	if !k.core.ready() {
		return 0, fmt.Errorf("indicator: Kalman.Value warmup 未完成（已喂 %d 根，需 1 根）", k.core.count)
	}
	return k.core.value(), nil
}

// Warmup 返回预热所需标量数 = 1（首观测即产出最优线性估计，无平均窗）。
func (k *Kalman) Warmup() int { return 1 }

// Reset 清空状态回到初始态（保留参数 q、r）。
func (k *Kalman) Reset() { k.core = newKalmanCore(k.core.q, k.core.r) }

// SaveState 序列化内部状态为版本化 JSON。
func (k *Kalman) SaveState() ([]byte, error) {
	return json.Marshal(kalmanState{
		Version: kalmanStateVersion,
		Count:   k.core.count,
		X:       k.core.x,
		P:       k.core.p,
	})
}

// LoadState 反序列化状态；不合法输入返回 error，原子提交（失败不改动接收者）。
func (k *Kalman) LoadState(b []byte) error {
	var s kalmanState
	if err := decodeVersionedState(b, &s, kalmanStateVersion, "version", "count", "x", "p"); err != nil {
		return err
	}
	if s.Count < 0 {
		return fmt.Errorf("indicator: Kalman.LoadState count=%d 非法", s.Count)
	}
	if !isFinite(s.X) || !isFinite(s.P) {
		return errors.New("indicator: Kalman.LoadState 状态含非有限值")
	}
	// 原子提交：以上校验全部通过才改动接收者。
	k.core.count = s.Count
	k.core.x = s.X
	k.core.p = s.P
	return nil
}

// KalmanBatch 是 Kalman 的批量对偶实现——与 Step 共享同一份递推核 kalmanCore。
//
//	返回值长度 == len(xs)；warmup=1，故无 NaN 前导。
//	空 / nil 输入 → 返回空、无错。
//	任一 z 非有限 → error（与 Step 同规）。
func KalmanBatch(xs []float64, q, r float64) ([]float64, error) {
	if !isFinite(q) || q < 0 {
		return nil, fmt.Errorf("indicator: KalmanBatch q=%v 非法（要求 q ≥ 0 且有限）", q)
	}
	if !isFinite(r) || r <= 0 {
		return nil, fmt.Errorf("indicator: KalmanBatch r=%v 非法（要求 r > 0 且有限）", r)
	}
	out := make([]float64, len(xs))
	c := newKalmanCore(q, r)
	for i, z := range xs {
		if !isFinite(z) {
			return nil, fmt.Errorf("indicator: KalmanBatch 第 %d 个输入非有限 %v", i, z)
		}
		c.step(z)
		if c.ready() {
			out[i] = c.value()
		} else {
			out[i] = math.NaN()
		}
	}
	return out, nil
}
