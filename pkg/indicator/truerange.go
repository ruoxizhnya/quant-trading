// indicator 的 TrueRange 助手（K3 切片 1）。
//
// TrueRange 是 ADR-028 ⑥⑧ 原动力链的入口：ATR = RMA(TrueRange, N)，
// 而 RMA 需要标量序列输入（见 interfaces.go 的 Update 标量化裁决）——
// TrueRange 就是「bar 级抽取」这一环，把 OHLCV 降维成标量送回递推核。
//
// 本切片只提供助手本身，**不组装 ATR 复合体**，也不改 pkg/risk/stoploss.go
// （那是后续任务）。
package indicator

import (
	"math"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// TrueRange 计算单根 bar 的真实波幅：max(H-L, |H-prevClose|, |L-prevClose|)。
//
// prevClose 是上一根 bar 的收盘价。首根 bar 无前收，约定传该 bar 自身
// 的 Close：因 Close ∈ [Low, High]，此时另两项 |H-Close|、|L-Close| 均
// ≤ (High-Low)，故 max 退化为 High-Low（= 首根 TR 的标准约定）。
func TrueRange(prevClose float64, bar domain.OHLCV) float64 {
	hl := bar.High - bar.Low
	hpc := math.Abs(bar.High - prevClose)
	lpc := math.Abs(bar.Low - prevClose)
	return math.Max(hl, math.Max(hpc, lpc))
}
