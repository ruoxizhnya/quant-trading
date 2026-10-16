package data

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ─── D3 ⑤1 数值回归红线：公式库版 vs 原代码版，同输入同输出 ───
//
// 夹具形态与 pkg/data/factor_equitydeep_test.go 的 eqdMockStore 同款
// （包内先例），对拍面只加 formulas 需要的 OHLCV/fundamentals 两条读
// 路径与 entries 捕获。

type d3MockStore struct {
	ohlcv        []domain.OHLCV
	fundamentals []domain.FundamentalData
	// activity 是 D5 市场活跃度夹具（turnover_rate 族）。范围查询按
	// `TradeDate <= end` 过滤——与存储侧 SQL 同口径，使「晚于计算日的
	// 行不可见」这一 PIT 属性在 mock 层也可被钉住。
	activity []domain.MarketActivity

	saved []*domain.FactorCacheEntry
}

var _ FactorStore = (*d3MockStore)(nil)

func (m *d3MockStore) GetOHLCVForDateRange(_ context.Context, _, _ time.Time) ([]domain.OHLCV, error) {
	return m.ohlcv, nil
}

func (m *d3MockStore) GetFundamentalsSnapshot(_ context.Context, _ time.Time) ([]domain.FundamentalData, error) {
	return m.fundamentals, nil
}

func (m *d3MockStore) GetFundamentalsDetailAsOf(_ context.Context, _ []string, _ time.Time) ([]domain.FundamentalsDetailRow, error) {
	return nil, nil
}

func (m *d3MockStore) GetMarketActivityForDateRange(_ context.Context, start, end time.Time) ([]domain.MarketActivity, error) {
	var out []domain.MarketActivity
	for _, r := range m.activity {
		if !r.TradeDate.Before(start) && !r.TradeDate.After(end) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *d3MockStore) GetTradingDays(_ context.Context, _, _ time.Time) ([]time.Time, error) {
	return nil, nil
}

func (m *d3MockStore) SaveFactorCacheBatch(_ context.Context, entries []*domain.FactorCacheEntry) error {
	m.saved = append(m.saved, entries...)
	return nil
}

// entriesBySymbol 把一次计算的落库 entries 折成 symbol → (raw, z, pct)。
func entriesBySymbol(entries []*domain.FactorCacheEntry) map[string][3]float64 {
	out := make(map[string][3]float64, len(entries))
	for _, e := range entries {
		out[e.Symbol] = [3]float64{e.RawValue, e.ZScore, e.Percentile}
	}
	return out
}

// closeSeries 生成 count 根日线：close 从 base 起 step 递增，日期从
// date 倒推（最后一根 = date）。
func closeSeries(symbol string, date time.Time, count int, base, step float64) []domain.OHLCV {
	bars := make([]domain.OHLCV, count)
	for i := 0; i < count; i++ {
		d := date.AddDate(0, 0, -(count - 1 - i))
		close := base + float64(i)*step
		bars[i] = domain.OHLCV{
			Symbol: symbol, Date: d,
			Open: close, High: close, Low: close, Close: close,
			Volume: 1000, Turnover: close * 1000, TradeDays: 1,
			Frequency: "1d",
		}
	}
	return bars
}

func mustLoadRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := NewRegistry(domain.BuiltinFactorDefinitions)
	if err != nil {
		t.Fatalf("load builtin registry: %v", err)
	}
	return r
}

func mustDef(t *testing.T, r *Registry, name domain.FactorType) domain.FactorDefinition {
	t.Helper()
	d, ok := r.Definition(name)
	if !ok {
		t.Fatalf("definition %q missing", name)
	}
	return d
}

// ─── momentum：公式版与原代码版对拍 + 手算钉死 ───

func TestFormulaFactor_Momentum_MatchesLegacyAndHandComputed(t *testing.T) {
	date := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

	// 三个「健康」symbol：25 根 bar，lookback=20 可取。
	//   A: close 10→34（step1）→ momentum = (34-14)/14 = 20/14
	//   B: close 20→44（step1）→ momentum = (44-24)/24 = 20/24
	//   C: close 5→29（step1）  → momentum = (29-9)/9  = 20/9
	// D：只有 15 根 bar（历史不足 20）→ 两边都必须不产出。
	// E：25 根但第 5 根（lookback 起点）close=-1 → 原版「startPrice<=0
	//    跳过」，公式版 Validity「ts_delay(close,20)>0」复现同一语义。
	var ohlcv []domain.OHLCV
	ohlcv = append(ohlcv, closeSeries("A", date, 25, 10, 1)...)
	ohlcv = append(ohlcv, closeSeries("B", date, 25, 20, 1)...)
	ohlcv = append(ohlcv, closeSeries("C", date, 25, 5, 1)...)
	ohlcv = append(ohlcv, closeSeries("D", date, 15, 10, 1)...)
	eBars := closeSeries("E", date, 25, 10, 1)
	eBars[4].Close = -1
	eBars[4].Open, eBars[4].High, eBars[4].Low = -1, -1, -1
	ohlcv = append(ohlcv, eBars...)

	hand := map[string]float64{
		"A": 20.0 / 14.0,
		"B": 20.0 / 24.0,
		"C": 20.0 / 9.0,
	}

	// 原代码版
	legacyStore := &d3MockStore{ohlcv: ohlcv}
	legacy := NewFactorComputer(legacyStore)
	if err := legacy.ComputeMomentumFactor(context.Background(), date, 20); err != nil {
		t.Fatalf("legacy momentum: %v", err)
	}
	legacyOut := entriesBySymbol(legacyStore.saved)

	// 公式库版
	r := mustLoadRegistry(t)
	formulaStore := &d3MockStore{ohlcv: ohlcv}
	formula := NewFactorComputer(formulaStore)
	if err := formula.ComputeFormulaFactor(context.Background(), mustDef(t, r, domain.FactorMomentum), date); err != nil {
		t.Fatalf("formula momentum: %v", err)
	}
	formulaOut := entriesBySymbol(formulaStore.saved)

	// ① 手算值（含两位有效精度内的浮点比较）
	if len(formulaOut) != len(hand) {
		t.Fatalf("formula momentum produced %d symbols, want %d: %v", len(formulaOut), len(hand), formulaOut)
	}
	for symbol, want := range hand {
		got, ok := formulaOut[symbol]
		if !ok {
			t.Fatalf("formula momentum missing symbol %q", symbol)
		}
		if math.Abs(got[0]-want) > 1e-9 {
			t.Errorf("symbol %q: raw = %v, hand-computed %v", symbol, got[0], want)
		}
	}

	// ② 对拍：同输入同输出（raw/z/pct 三列全等；D/E 两边都不在）
	if len(legacyOut) != len(formulaOut) {
		t.Fatalf("symbol count differs: legacy=%d formula=%d (legacy=%v formula=%v)",
			len(legacyOut), len(formulaOut), keysOf(legacyOut), keysOf(formulaOut))
	}
	for symbol, lv := range legacyOut {
		fv, ok := formulaOut[symbol]
		if !ok {
			t.Fatalf("formula version dropped symbol %q that legacy computed", symbol)
		}
		if math.Abs(lv[0]-fv[0]) > 1e-12 || math.Abs(lv[1]-fv[1]) > 1e-12 || math.Abs(lv[2]-fv[2]) > 1e-12 {
			t.Errorf("symbol %q: legacy=%v formula=%v", symbol, lv, fv)
		}
	}
	for _, absent := range []string{"D", "E"} {
		if _, ok := legacyOut[absent]; ok {
			t.Errorf("legacy should have skipped %q", absent)
		}
		if _, ok := formulaOut[absent]; ok {
			t.Errorf("formula should have skipped %q (validity gate)", absent)
		}
	}
}

// ─── value：1/PE 对拍 + PE>0 域过滤复现 ───

func TestFormulaFactor_Value_MatchesLegacyWithDomainFilter(t *testing.T) {
	date := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	pe20, pe50, pe0 := 20.0, 50.0, 0.0
	roe := 0.15
	fundamentals := []domain.FundamentalData{
		{TsCode: "A", PE: &pe20, ROE: &roe},
		{TsCode: "B", PE: &pe0, ROE: &roe},  // PE=0：域外，两边跳过
		{TsCode: "C", PE: nil, ROE: &roe},   // PE nil：缺失，两边跳过
		{TsCode: "D", PE: &pe50, ROE: &roe}, // 1/50
	}

	legacyStore := &d3MockStore{fundamentals: fundamentals}
	legacy := NewFactorComputer(legacyStore)
	if err := legacy.ComputeValueFactor(context.Background(), date); err != nil {
		t.Fatalf("legacy value: %v", err)
	}
	legacyOut := entriesBySymbol(legacyStore.saved)

	r := mustLoadRegistry(t)
	formulaStore := &d3MockStore{fundamentals: fundamentals}
	formula := NewFactorComputer(formulaStore)
	if err := formula.ComputeFormulaFactor(context.Background(), mustDef(t, r, domain.FactorValue), date); err != nil {
		t.Fatalf("formula value: %v", err)
	}
	formulaOut := entriesBySymbol(formulaStore.saved)

	// 手算：1/20=0.05，1/50=0.02
	for symbol, want := range map[string]float64{"A": 0.05, "D": 0.02} {
		got, ok := formulaOut[symbol]
		if !ok {
			t.Fatalf("formula value missing %q", symbol)
		}
		if math.Abs(got[0]-want) > 1e-12 {
			t.Errorf("symbol %q: raw = %v, want %v", symbol, got[0], want)
		}
	}
	// 对拍全等
	if len(legacyOut) != len(formulaOut) || len(formulaOut) != 2 {
		t.Fatalf("value factor symbol sets differ: legacy=%v formula=%v", keysOf(legacyOut), keysOf(formulaOut))
	}
	for symbol, lv := range legacyOut {
		fv := formulaOut[symbol]
		if math.Abs(lv[0]-fv[0]) > 1e-12 || math.Abs(lv[1]-fv[1]) > 1e-12 || math.Abs(lv[2]-fv[2]) > 1e-12 {
			t.Errorf("symbol %q: legacy=%v formula=%v", symbol, lv, fv)
		}
	}
}

// ─── quality：叶子直取对拍 + ROE>0 域过滤复现 ───

func TestFormulaFactor_Quality_LeafMatchesLegacy(t *testing.T) {
	date := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	pe := 20.0
	roeA, roe0 := 0.15, 0.0
	fundamentals := []domain.FundamentalData{
		{TsCode: "A", PE: &pe, ROE: &roeA},
		{TsCode: "B", PE: &pe, ROE: &roe0}, // ROE=0：域外
		{TsCode: "C", PE: &pe, ROE: nil},   // ROE nil：缺失
	}

	legacyStore := &d3MockStore{fundamentals: fundamentals}
	legacy := NewFactorComputer(legacyStore)
	if err := legacy.ComputeQualityFactor(context.Background(), date); err != nil {
		t.Fatalf("legacy quality: %v", err)
	}
	legacyOut := entriesBySymbol(legacyStore.saved)

	r := mustLoadRegistry(t)
	formulaStore := &d3MockStore{fundamentals: fundamentals}
	formula := NewFactorComputer(formulaStore)
	if err := formula.ComputeFormulaFactor(context.Background(), mustDef(t, r, domain.FactorQuality), date); err != nil {
		t.Fatalf("formula quality: %v", err)
	}
	formulaOut := entriesBySymbol(formulaStore.saved)

	if len(formulaOut) != 1 {
		t.Fatalf("quality should produce exactly A, got %v", keysOf(formulaOut))
	}
	if got := formulaOut["A"][0]; math.Abs(got-0.15) > 1e-12 {
		t.Errorf("A raw = %v, want 0.15 (leaf passthrough)", got)
	}
	for symbol, lv := range legacyOut {
		fv, ok := formulaOut[symbol]
		if !ok {
			t.Fatalf("formula dropped %q", symbol)
		}
		if math.Abs(lv[0]-fv[0]) > 1e-12 || math.Abs(lv[1]-fv[1]) > 1e-12 || math.Abs(lv[2]-fv[2]) > 1e-12 {
			t.Errorf("symbol %q: legacy=%v formula=%v", symbol, lv, fv)
		}
	}
}

// ─── fail-closed：绕过 registry 直接构造的坏定义，求值期也要拦住 ───

func TestFormulaFactor_FailClosed_UnknownFieldAtEvaluation(t *testing.T) {
	date := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	store := &d3MockStore{ohlcv: closeSeries("A", date, 25, 10, 1)}
	fc := NewFactorComputer(store)

	bad := domain.FactorDefinition{
		Name: domain.FactorMomentum, Kernel: domain.KernelFormula,
		Formula:       "total_market_cap / 2", // 不在规范层字段词表
		Caliber:       domain.CaliberZScore,
		InputFields:   []string{"total_market_cap"},
		EffectiveFrom: "2026-10-14",
		Lookahead:     "trade_date",
		Lookback:      30,
	}
	if err := fc.ComputeFormulaFactor(context.Background(), bad, date); err == nil {
		t.Fatal("evaluation of a formula referencing an unknown field must fail closed")
	}
	if len(store.saved) != 0 {
		t.Fatal("fail-closed path must not persist anything")
	}
}

func TestFormulaFactor_FailClosed_NonFormulaKernel(t *testing.T) {
	date := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	fc := NewFactorComputer(&d3MockStore{})
	r := mustLoadRegistry(t)
	// 代码型因子走 CodeKernel，不能误入公式路径
	if err := fc.ComputeFormulaFactor(context.Background(), mustDef(t, r, domain.FactorGrossMarginTrend), date); err == nil {
		t.Fatal("code-kernel factor must be rejected by the formula path")
	}
}

func keysOf(m map[string][3]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
