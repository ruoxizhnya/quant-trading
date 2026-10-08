// pkg/indicator 测试共享工具（K3 切片 1）。
package indicator_test

import (
	"math"
	"math/rand"
	"strconv"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/indicator"
)

const tol = 1e-12

func itoa(n int) string     { return strconv.Itoa(n) }
func ftoa(x float64) string { return strconv.FormatFloat(x, 'g', -1, 64) }

// closeEnough 报告 |a-b| <= tol；NaN 与 NaN 视为相等（Batch 未预热位用 NaN 表达）。
func closeEnough(a, b float64) bool {
	if math.IsNaN(a) && math.IsNaN(b) {
		return true
	}
	return math.Abs(a-b) <= tol
}

// randomSeries 用固定种子 RNG 生成 n 个随机标量（确定性，便于复现）。
func randomSeries(seed int64, n int) []float64 {
	rng := rand.New(rand.NewSource(seed))
	xs := make([]float64, n)
	for i := range xs {
		// 混合量级与正负，避免退化输入掩盖递推差异。
		xs[i] = rng.NormFloat64()*10 + float64(i%7)*0.5
	}
	return xs
}

// assertThreeWay 是 ADR-028 §7 三路一致性的通用断言：
//
//	Batch(xs)[t]  ≡  Step 从零累积到 t  ≡  Step-from-persisted 恢复到 t   （容差 tol）
//
// 其中第三路跑一半 SaveState → 新实例 LoadState → 续跑。
// warmup 之前的位：Batch 为 NaN，Step/Step-from-persisted 的 Value() 必须 error。
func assertThreeWay(t *testing.T, name string, newInd func() (indicator.Indicator, error), batch func([]float64) ([]float64, error), xs []float64, warmup int) {
	t.Helper()

	want, err := batch(xs)
	if err != nil {
		t.Fatalf("%s Batch: %v", name, err)
	}
	if len(want) != len(xs) {
		t.Fatalf("%s Batch 长度 = %d, want %d", name, len(want), len(xs))
	}

	// ── 路径 1 vs 路径 2：Batch ≡ Step 累积 ──
	s1, err := newInd()
	if err != nil {
		t.Fatalf("%s New: %v", name, err)
	}
	for i, x := range xs {
		if err := s1.Update(x); err != nil {
			t.Fatalf("%s Step.Update t=%d: %v", name, i, err)
		}
		got, verr := s1.Value()
		if i < warmup-1 {
			if verr == nil {
				t.Fatalf("%s t=%d warmup(<%d) 未完成却返回值 %v", name, i, warmup, got)
			}
			if !math.IsNaN(want[i]) {
				t.Fatalf("%s t=%d Batch 应为 NaN，得 %v", name, i, want[i])
			}
			continue
		}
		if verr != nil {
			t.Fatalf("%s Step.Value t=%d: %v", name, i, verr)
		}
		if !closeEnough(got, want[i]) {
			t.Fatalf("%s 路径分裂 Batch[t=%d]=%.17g vs Step=%.17g（diff=%.3g）", name, i, want[i], got, math.Abs(want[i]-got))
		}
	}

	// ── 路径 3：Step-from-persisted ──
	half := len(xs) / 2
	if half < warmup {
		half = warmup
	}
	if half >= len(xs) {
		t.Fatalf("%s 序列太短，无法在中途存断点", name)
	}
	s2, err := newInd()
	if err != nil {
		t.Fatalf("%s New#2: %v", name, err)
	}
	for i := 0; i < half; i++ {
		if err := s2.Update(xs[i]); err != nil {
			t.Fatalf("%s 断点前 Update t=%d: %v", name, i, err)
		}
	}
	blob, err := s2.SaveState()
	if err != nil {
		t.Fatalf("%s SaveState: %v", name, err)
	}
	s3, err := newInd()
	if err != nil {
		t.Fatalf("%s New#3: %v", name, err)
	}
	if err := s3.LoadState(blob); err != nil {
		t.Fatalf("%s LoadState: %v", name, err)
	}
	for i := half; i < len(xs); i++ {
		if err := s3.Update(xs[i]); err != nil {
			t.Fatalf("%s 续跑 Update t=%d: %v", name, i, err)
		}
		got, verr := s3.Value()
		if verr != nil {
			t.Fatalf("%s 续跑 Value t=%d: %v", name, i, verr)
		}
		if !closeEnough(got, want[i]) {
			t.Fatalf("%s 路径分裂 Batch[t=%d]=%.17g vs Step-from-persisted=%.17g（diff=%.3g）", name, i, want[i], got, math.Abs(want[i]-got))
		}
	}
}

// assertWarmupBoundary 断言：喂 Warmup()-1 个 → Value() 报 error；喂第 Warmup() 个 → 有值。
func assertWarmupBoundary(t *testing.T, name string, ind indicator.Indicator, feed func(i int) float64) {
	t.Helper()
	w := ind.Warmup()
	if w < 1 {
		t.Fatalf("%s Warmup()=%d 非法（应 ≥ 1）", name, w)
	}
	for i := 0; i < w-1; i++ {
		if err := ind.Update(feed(i)); err != nil {
			t.Fatalf("%s 喂第 %d 个失败: %v", name, i+1, err)
		}
		if _, err := ind.Value(); err == nil {
			t.Fatalf("%s 已喂 %d 根（< warmup %d）却返回了值", name, i+1, w)
		}
	}
	if err := ind.Update(feed(w - 1)); err != nil {
		t.Fatalf("%s 喂第 %d 个失败: %v", name, w, err)
	}
	if _, err := ind.Value(); err != nil {
		t.Fatalf("%s 已喂 %d 根（= warmup %d）却报错: %v", name, w, w, err)
	}
}
