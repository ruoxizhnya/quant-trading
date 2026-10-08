// pkg/indicator TrueRange + ATR 验收测试（K3 切片 1）。
package indicator_test

import (
	"math"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/indicator"
)

// atrSampleBars 是含一次跳空缺口的 20 根小样本 bar（i=7 高开 gap up）。
// 设计目标是让每根 TR 为可手算的整数（见下）。
func atrSampleBars() []domain.OHLCV {
	type hl struct{ h, l, c float64 }
	rows := []hl{
		{12, 10, 11}, // 0
		{14, 10, 12}, // 1
		{13, 11, 12}, // 2
		{15, 9, 12},  // 3
		{13, 11, 12}, // 4
		{14, 10, 12}, // 5
		{13, 11, 12}, // 6
		{24, 16, 20}, // 7  跳空缺口（低点 16 > 前收 12）
		{21, 19, 20}, // 8
		{22, 18, 20}, // 9
		{21, 19, 20}, // 10
		{23, 17, 20}, // 11
		{21, 19, 20}, // 12
		{22, 18, 20}, // 13
		{25, 15, 20}, // 14
		{21, 19, 20}, // 15
		{22, 18, 20}, // 16
		{21, 19, 20}, // 17
		{23, 17, 20}, // 18
		{21, 19, 20}, // 19
	}
	bars := make([]domain.OHLCV, len(rows))
	for i, r := range rows {
		bars[i] = domain.OHLCV{High: r.h, Low: r.l, Close: r.c}
	}
	return bars
}

// TestTrueRange_HandComputed 手算 TrueRange 序列（抓公式错）。
//
//	TR = max(H-L, |H-prevClose|, |L-prevClose|)
//
// i=0 无前收，约定传自身 close（=11）；i=7 跳空：前收 12，低 16 → |H-12|=12 占主导。
//
// 期望序列：2,4,2,6,2,4,2,12,2,4,2,6,2,4,10,2,4,2,6,2
func TestTrueRange_HandComputed(t *testing.T) {
	bars := atrSampleBars()
	want := []float64{2, 4, 2, 6, 2, 4, 2, 12, 2, 4, 2, 6, 2, 4, 10, 2, 4, 2, 6, 2}
	for i, bar := range bars {
		prevClose := bar.Close
		if i > 0 {
			prevClose = bars[i-1].Close
		}
		got := indicator.TrueRange(prevClose, bar)
		if got != want[i] {
			t.Errorf("TrueRange[%d] = %v, want %v", i, got, want[i])
		}
	}
}

// TestATR_WilderRMA14_HandComputed ATR 验收：TrueRange 序列经 RMABatch(tr,14)
// 与手算 Wilder ATR 逐点一致（容差 1e-12）。
//
// Wilder ATR(14)：
//
//	idx13 = SMA(TR[0..13]) = 54/14 = 3.8571428571428572   ← 种子
//	idx14 = prev + (10-prev)/14 = 4.295918367346939
//	idx15..19 同递推（期望值为独立计算的常量，不调用被测代码推算）
func TestATR_WilderRMA14_HandComputed(t *testing.T) {
	bars := atrSampleBars()
	tr := make([]float64, len(bars))
	for i, bar := range bars {
		prevClose := bar.Close
		if i > 0 {
			prevClose = bars[i-1].Close
		}
		tr[i] = indicator.TrueRange(prevClose, bar)
	}

	atr, err := indicator.RMABatch(tr, 14)
	if err != nil {
		t.Fatalf("RMABatch(tr,14): %v", err)
	}

	want := []float64{
		math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(),
		math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN(),
		math.NaN(), math.NaN(), math.NaN(),
		3.8571428571428572, // idx13 种子 = 54/14
		4.295918367346939,  // idx14
		4.1319241982507293, // idx15
		4.1225010412328205, // idx16
		3.9708938240019047, // idx17
		4.1158299794303401, // idx18
		3.9646992666138874, // idx19
	}
	if len(atr) != len(want) {
		t.Fatalf("ATR 长度 = %d, want %d", len(atr), len(want))
	}
	for i := range want {
		if !closeEnough(atr[i], want[i]) {
			t.Errorf("ATR[%d] = %.17g, want %.17g", i, atr[i], want[i])
		}
	}
}
