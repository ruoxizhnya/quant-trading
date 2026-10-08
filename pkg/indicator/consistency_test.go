// pkg/indicator 三路一致性属性测试（K3 切片 1，ADR-028 §7）。
//
// 抓「路径分裂」：Batch ≡ Step ≡ Step-from-persisted（容差 1e-12）。
// 注意：三路共享同一份递推核，故**公式写错时三路仍互相一致**——本测试只
// 抓路径分裂，公式正确性由各算子的手算 fixture 抓（两类测试缺一不可）。
package indicator_test

import (
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/indicator"
)

// TestConsistency_RMA 多组参数下 RMA 三路一致。
func TestConsistency_RMA(t *testing.T) {
	xs := randomSeries(1, 200)
	for _, n := range []int{1, 2, 3, 7, 14, 20, 60} {
		n := n
		t.Run("N="+itoa(n), func(t *testing.T) {
			assertThreeWay(t, "RMA",
				func() (indicator.Indicator, error) { return indicator.NewRMA(n) },
				func(in []float64) ([]float64, error) { return indicator.RMABatch(in, n) },
				xs, n)
		})
	}
}

// TestConsistency_EWMA 多组参数下 EWMA 三路一致（序列取 500 以容纳
// α=0.05 的大 warmup ≈ 269）。
func TestConsistency_EWMA(t *testing.T) {
	xs := randomSeries(2, 500)
	for _, a := range []float64{1, 0.9, 0.5, 0.3, 0.1, 0.05} {
		a := a
		t.Run("alpha="+ftoa(a), func(t *testing.T) {
			w := mustEWMAWarmup(t, a)
			assertThreeWay(t, "EWMA",
				func() (indicator.Indicator, error) { return indicator.NewEWMA(a) },
				func(in []float64) ([]float64, error) { return indicator.EWMABatch(in, a) },
				xs, w)
		})
	}
}

// TestConsistency_Kalman 多组参数下 Kalman 三路一致。
func TestConsistency_Kalman(t *testing.T) {
	xs := randomSeries(3, 200)
	cases := []struct{ q, r float64 }{
		{0, 1}, {0.1, 0.5}, {1, 1}, {0.01, 2}, {5, 0.001}, {0, 0.25},
	}
	for _, c := range cases {
		c := c
		t.Run("q="+ftoa(c.q)+",r="+ftoa(c.r), func(t *testing.T) {
			assertThreeWay(t, "Kalman",
				func() (indicator.Indicator, error) { return indicator.NewKalman(c.q, c.r) },
				func(in []float64) ([]float64, error) { return indicator.KalmanBatch(in, c.q, c.r) },
				xs, 1)
		})
	}
}

func mustEWMAWarmup(t *testing.T, alpha float64) int {
	t.Helper()
	ind, err := indicator.NewEWMA(alpha)
	if err != nil {
		t.Fatalf("NewEWMA(%v): %v", alpha, err)
	}
	return ind.Warmup()
}
