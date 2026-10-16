package data

// market_activity 因子（D5）的单元测试：数学定义手算钉死、缺失≠0、
// 历史不足、域过滤、以及「晚于计算日的行不可见」的 PIT 属性。
//
// 为什么用手算夹具而不是「重跑实现自己的公式」：与代码同构的断言在两侧
// 一起错时仍然通过（同 factor_equitydeep_test.go 的立场）。期望值都在注释
// 里独立算一遍。

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// maBuild 生成某 symbol 的活跃度序列：从 date 倒推 count 个自然日，
// 第 i 天（0 = 最早）换手率取 turnovers[i]。count 与 turnovers 等长。
func maBuild(symbol string, date time.Time, turnovers []float64) []domain.MarketActivity {
	out := make([]domain.MarketActivity, len(turnovers))
	for i, t := range turnovers {
		d := date.AddDate(0, 0, -(len(turnovers) - 1 - i))
		v := t
		out[i] = domain.MarketActivity{Symbol: symbol, TradeDate: d, TurnoverRate: &v, Source: "test"}
	}
	return out
}

// maConst 生成 count 个相同换手率（最后一天替换为 last）。
func maConst(count int, base float64) []float64 {
	out := make([]float64, count)
	for i := range out {
		out[i] = base
	}
	return out
}

// TestMarketActivityFactor_HandComputedAbnormalTurnover 钉住数学定义：
//
//	value = turnover_rate / ts_mean(turnover_rate, 20) - 1
//
// 夹具：19 天换手率 1.0 + 计算日 2.0。
// 手算：均值（含计算日）= (19×1.0 + 2.0) / 20 = 21/20 = 1.05
//
//	value = 2.0 / 1.05 − 1 = 0.9047619047619048
func TestMarketActivityFactor_HandComputedAbnormalTurnover(t *testing.T) {
	date := time.Date(2200, 3, 20, 0, 0, 0, 0, time.UTC)
	turnovers := maConst(20, 1.0)
	turnovers[19] = 2.0

	store := &d3MockStore{activity: maBuild("AAA.SH", date, turnovers)}
	fc := NewFactorComputer(store)
	fc.SetLogger(nopLogger())

	require.NoError(t, fc.ComputeFactorByName(context.Background(), domain.FactorMarketActivity, date))

	entries := entriesBySymbol(store.saved)
	got, ok := entries["AAA.SH"]
	require.True(t, ok, "该股票应有分数（历史充足且有效）")
	const want = 2.0/1.05 - 1 // = 0.9047619047619048
	require.InDelta(t, want, got[0], 1e-12, "异常换手率手算值")
	// 单标的横截面：z-score 无方差 → 0（statistics 的既有语义），百分位 0.5 级别。
	require.InDelta(t, 0.0, got[1], 1e-12, "单标的横截面 z-score 无方差 → 0")
}

// TestMarketActivityFactor_InsufficientHistoryIsMissing ts_mean(·,20) 位置
// 不足 → NaN → 不产出（历史不足 = 缺失，绝不拿部分窗口凑近似值）。
func TestMarketActivityFactor_InsufficientHistoryIsMissing(t *testing.T) {
	date := time.Date(2200, 4, 20, 0, 0, 0, 0, time.UTC)
	// 只有 19 个观测：第 19 个位置仍 < window-1 → ts_mean 给 NaN。
	turnovers := maConst(19, 1.0)
	store := &d3MockStore{activity: maBuild("AAA.SH", date, turnovers)}
	fc := NewFactorComputer(store)
	fc.SetLogger(nopLogger())

	require.NoError(t, fc.ComputeFactorByName(context.Background(), domain.FactorMarketActivity, date))
	require.Empty(t, entriesBySymbol(store.saved), "窗口不足 20 个观测 → 不产出（不是 0 分）")
}

// TestMarketActivityFactor_MissingObservationIsNotZero 缺失≠0：
// 计算日无观测（停牌/未发布，NULL）→ 不产出，且**不得**以 0 顶包。
// 同一批里另一只有完整数据的股票仍正常产出（缺失是逐股的）。
func TestMarketActivityFactor_MissingObservationIsNotZero(t *testing.T) {
	date := time.Date(2200, 5, 20, 0, 0, 0, 0, time.UTC)

	full := maConst(20, 1.0)
	full[19] = 2.0
	rows := maBuild("AAA.SH", date, full)

	// BBB.SH：前 19 天有数据，计算日缺观测。
	partial := maBuild("BBB.SH", date, maConst(20, 1.0))
	partial[19].TurnoverRate = nil // 计算日 NULL
	rows = append(rows, partial...)

	store := &d3MockStore{activity: rows}
	fc := NewFactorComputer(store)
	fc.SetLogger(nopLogger())

	require.NoError(t, fc.ComputeFactorByName(context.Background(), domain.FactorMarketActivity, date))

	entries := entriesBySymbol(store.saved)
	_, ok := entries["BBB.SH"]
	require.False(t, ok, "计算日缺失 → 不产出（缺失≠0，绝不落 0 分）")
	_, ok = entries["AAA.SH"]
	require.True(t, ok, "同批里有数据的股票照常产出（缺失逐股判定）")
}

// TestMarketActivityFactor_ZeroTurnoverIsOutOfDomain 域过滤（Validity）：
// 换手率恰为 0 是有意义观测（当日的真实 0），但因子定义域是 turnover_rate > 0
// ——域外 → 缺失（不产出），不是有效分数 0。
func TestMarketActivityFactor_ZeroTurnoverIsOutOfDomain(t *testing.T) {
	date := time.Date(2200, 6, 20, 0, 0, 0, 0, time.UTC)
	turnovers := maConst(20, 1.0)
	turnovers[19] = 0.0 // 计算日换手率为 0

	store := &d3MockStore{activity: maBuild("AAA.SH", date, turnovers)}
	fc := NewFactorComputer(store)
	fc.SetLogger(nopLogger())

	require.NoError(t, fc.ComputeFactorByName(context.Background(), domain.FactorMarketActivity, date))
	require.Empty(t, entriesBySymbol(store.saved), "换手率 0 落在定义域外 → 缺失（不是 0 分）")
}

// TestMarketActivityFactor_FutureRowsAreInvisible 是 PIT 属性在因子链路的
// 落点：「收盘后可见」的数据在计算日之前不可能存在，晚于计算日的行必须
// 结构上不可见。
//
// 夹具含一行 date+1 的异常值（1000）。若它泄漏进来，序列末点会变成未来日，
// 「date 当日的分数」实际会算出未来日的值（这正是前视作弊的形态）。
// 断言：结果与不含未来行的基准**逐位相等**。
func TestMarketActivityFactor_FutureRowsAreInvisible(t *testing.T) {
	date := time.Date(2200, 7, 20, 0, 0, 0, 0, time.UTC)
	base := maConst(20, 1.0)
	base[19] = 2.0
	const want = 2.0/1.05 - 1

	// 基准：不含未来行。
	baseStore := &d3MockStore{activity: maBuild("AAA.SH", date, base)}
	fcBase := NewFactorComputer(baseStore)
	fcBase.SetLogger(nopLogger())
	require.NoError(t, fcBase.ComputeFactorByName(context.Background(), domain.FactorMarketActivity, date))
	baseVal := entriesBySymbol(baseStore.saved)["AAA.SH"][0]
	require.InDelta(t, want, baseVal, 1e-12)

	// 含未来行（date+1，异常值 1000）：结果必须一模一样。
	leaky := maBuild("AAA.SH", date, base)
	future := 1000.0
	leaky = append(leaky, domain.MarketActivity{
		Symbol: "AAA.SH", TradeDate: date.AddDate(0, 0, 1), TurnoverRate: &future, Source: "test",
	})
	leakStore := &d3MockStore{activity: leaky}
	fcLeak := NewFactorComputer(leakStore)
	fcLeak.SetLogger(nopLogger())
	require.NoError(t, fcLeak.ComputeFactorByName(context.Background(), domain.FactorMarketActivity, date))

	got := entriesBySymbol(leakStore.saved)["AAA.SH"][0]
	require.InDelta(t, want, got, 1e-12,
		"晚于计算日的行必须不可见（取了未来值即前视作弊）")
	require.InDelta(t, baseVal, got, 0.0, "含未来行的结果与基准逐位相等")
}

// TestMarketActivityFactor_RegisteredInSeedRoster 钉住名册接入：
// market_activity 在种子注册表内、内核为 formula、输入字段引用 D1 规范层
// 字段 turnover_rate（D5 名册协议变更的落点）。
func TestMarketActivityFactor_RegisteredInSeedRoster(t *testing.T) {
	fc := NewFactorComputer(&d3MockStore{})
	def, ok := fc.registry.Definition(domain.FactorMarketActivity)
	require.True(t, ok, "market_activity 必须在库里（D5 名册协议变更）")
	require.Equal(t, domain.KernelFormula, def.Kernel)
	require.Equal(t, []string{"turnover_rate"}, def.InputFields)
	require.Equal(t, "turnover_rate / ts_mean(turnover_rate, 20) - 1", def.Formula)
	require.NotEmpty(t, def.VersionHash(), "定义带版本指纹")
	require.Equal(t, "trade_date", def.Lookahead, "前视纪律进 schema")

	// 冻结名册（ParseFactorType）必须认这个名字——否则 Definition.Validate 拒收。
	_, ok = domain.ParseFactorType("market_activity")
	require.True(t, ok, "名册协议变更后 ParseFactorType 必须认 market_activity")
}

// nopLogger 返回静默 logger（既有因子测试不设 logger 即用 zerolog.Nop，
// 这里显式设置以保持日志路径初始化一致）。
func nopLogger() zerolog.Logger { return zerolog.Nop() }
