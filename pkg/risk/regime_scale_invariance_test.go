package risk

// AUD-53 的一格：**regime 判定对价格水平缩放是什么行为**。
//
// 背景。P2-8 取证撞出「每只票的价格乘一个正数常数，回测收益动 21.6 个百分点」，
// 而「全体乘同一个常数」只动 1.1 个百分点。当时的**猜测**（写在
// pkg/data/tushare_adjustment_integration_test.go 的文件头与 TASKS.md 的
// AUD-53 行里，并明确标注「尚未用插桩证实」）是：通道在把各票价格拼成
// 一根长序列的环节，最像的是 `Engine.detectRegime`。
//
// 这个文件把那个猜测**钉死**。它是纯计算、无网络、无库：直接把引擎拼出来的
// 那根序列喂给 RegimeDetector，分别用统一缩放与每票各自缩放，比对判定结果。
//
// 读代码先能给出的预期（三条都要被这里的断言验证，而不是被当作前提）：
//
//   - `detectTrend` 用 `fastMA / slowMA` 之比 + 「OLS 斜率 / 均价」→ 对
//     **组内**统一缩放不变；
//   - `detectVolatility` 与情绪里的动量项用**对数收益** → 对组内统一缩放不变；
//   - 拼接后的序列尾部落在一只票的块内，所以「每票各自缩放」在尾部这一块
//     内部仍然是统一缩放 → **对每票各自缩放也不变**。
//
// 若第三条成立，则 AUD-53 登记里的 regime 猜测被**证伪**，21.6 个百分点
// 必须去别处找（整手离散化 × 决策分支）；若不成立，这里就给出了 regime 的
// 敏感性图景，修法也就有了着力点。

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

const (
	regimeProbeSymbols = 20
	regimeProbeDays    = 520
)

// regimeProbeBars 生成合成行情：每票一条确定性的对数随机游走，
// 起始价位按几何级数拉开量级（3 → 约 380），贴近 A 股前复权价的真实跨度。
func regimeProbeBars() map[string][]domain.OHLCV {
	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	out := make(map[string][]domain.OHLCV, regimeProbeSymbols)

	for i := 0; i < regimeProbeSymbols; i++ {
		sym := fmt.Sprintf("60%04d.SH", 1000+i)
		rng := rand.New(rand.NewSource(int64(2000 + i)))
		price := 3.0 * math.Pow(1.29, float64(i))
		bars := make([]domain.OHLCV, 0, regimeProbeDays)
		for d := 0; d < regimeProbeDays; d++ {
			drift := (rng.Float64() - 0.487) * 0.035
			open := price
			close := open * (1 + drift)
			bars = append(bars, domain.OHLCV{
				Symbol: sym,
				Date:   start.AddDate(0, 0, d),
				Open:   open,
				High:   math.Max(open, close) * 1.005,
				Low:    math.Min(open, close) * 0.995,
				Close:  close,
				Volume: 1_000_000,
			})
			price = close
		}
		out[sym] = bars
	}
	return out
}

// concatLikeEngine 复刻 Engine.detectRegime 的拼接方式：按 symbol 字典序，
// 把每票的 bar 依次追加。顺序必须确定（P1-14）。
func concatLikeEngine(data map[string][]domain.OHLCV, upto int, scale map[string]float64) []domain.OHLCV {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var all []domain.OHLCV
	for _, sym := range keys {
		bars := data[sym]
		if upto < len(bars) {
			bars = bars[:upto]
		}
		c := 1.0
		if scale != nil {
			c = scale[sym]
		}
		for _, b := range bars {
			if c != 1.0 {
				b.Open *= c
				b.High *= c
				b.Low *= c
				b.Close *= c
			}
			all = append(all, b)
		}
	}
	return all
}

// perSymbolScale 造一组「每票各自的常数」，跨度与真数据里的复权基准相当
// （实测 1.7 ~ 181）。
func perSymbolScale(data map[string][]domain.OHLCV) map[string]float64 {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make(map[string]float64, len(keys))
	for i, sym := range keys {
		out[sym] = 1.7 * math.Pow(181.0/1.7, float64(i)/float64(len(keys)-1))
	}
	return out
}

func regimeSign(rd *RegimeDetector, bars []domain.OHLCV) string {
	r, err := rd.DetectRegime(context.Background(), bars)
	if err != nil {
		return "error:" + err.Error()
	}
	return fmt.Sprintf("%s/%s/%.12f", r.Trend, r.Volatility, r.Sentiment)
}

// TestRegimeIsInvariantToUniformPriceScaling 钉住一条**数学上必须成立**的
// 不变量：全体价格乘同一个正数，regime 判定必须一模一样。
//
// 这条断言是下面那条的前提 —— 如果连它都不成立，说明 regime 里混进了
// 绝对价格，那才是真正要修的东西。
func TestRegimeIsInvariantToUniformPriceScaling(t *testing.T) {
	rd := NewRegimeDetector(RegimeConfig{
		FastMAPeriod: 50, SlowMAPeriod: 200, VolLookback: 120,
	}, zerolog.Nop())

	data := regimeProbeBars()

	uniform := make(map[string]float64, len(data))
	for sym := range data {
		uniform[sym] = 9.04
	}

	checked := 0
	for upto := 260; upto <= regimeProbeDays; upto += 20 {
		base := regimeSign(rd, concatLikeEngine(data, upto, nil))
		scaled := regimeSign(rd, concatLikeEngine(data, upto, uniform))
		if base != scaled {
			t.Fatalf("全体 ×9.04 后 regime 变了：base=%s scaled=%s（window=%d 天）—— 「等比例缩放不改变 regime」这条不成立，regime 里混进了绝对价格",
				base, scaled, upto)
		}
		checked++
	}
	t.Logf("全体统一缩放（×9.04）：%d 个时间窗、逐窗比对，regime 判定**完全一致**", checked)
}

// TestRegimeSensitivityToPerSymbolScaling 测量（并钉住）regime 对
// 「每票各自缩放」的敏感性 —— 这正是 hfq 腿与 qfq 腿的真实差别。
func TestRegimeSensitivityToPerSymbolScaling(t *testing.T) {
	rd := NewRegimeDetector(RegimeConfig{
		FastMAPeriod: 50, SlowMAPeriod: 200, VolLookback: 120,
	}, zerolog.Nop())

	data := regimeProbeBars()
	scale := perSymbolScale(data)

	var (
		checked   int
		different []string
	)
	for upto := 260; upto <= regimeProbeDays; upto += 20 {
		base := regimeSign(rd, concatLikeEngine(data, upto, nil))
		scaled := regimeSign(rd, concatLikeEngine(data, upto, scale))
		if base != scaled {
			different = append(different, fmt.Sprintf("window=%d: base=%s scaled=%s", upto, base, scaled))
		}
		checked++
	}

	t.Logf("每票各自缩放（1.7~181，与真数据复权基准同量级）：%d 个时间窗里 %d 个判定不同",
		checked, len(different))
	for _, d := range different {
		t.Logf("  %s", d)
	}

	if len(different) > 0 {
		t.Errorf("regime 对每票各自缩放**敏感**（%d/%d 个窗口判定不同）—— "+
			"AUD-53 登记里的「通道在 detectRegime」这条猜测成立，应当去修 regime",
			len(different), checked)
	}
}
