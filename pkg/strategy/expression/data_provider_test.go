package expression

import (
	"strings"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// makeBarsN builds a bars map with one symbol having n bars whose Close
// values are 1.0, 2.0, ..., n.0 (i*1.0). Dates are ascending so the
// provider's lookback slice (which takes the tail) is predictable.
func makeBarsN(symbol string, n int) []domain.OHLCV {
	bars := make([]domain.OHLCV, n)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		v := float64(i + 1)
		bars[i] = domain.OHLCV{
			Symbol: symbol,
			Date:   base.AddDate(0, 0, i),
			Open:   v,
			High:   v,
			Low:    v,
			Close:  v,
			Volume: v * 100,
		}
	}
	return bars
}

// ─── GetField ──────────────────────────────────────────────────────────

func TestGetField_Close_10Bars(t *testing.T) {
	p := NewOHLCVDataProvider(map[string][]domain.OHLCV{
		"A": makeBarsN("A", 10),
	})
	got, err := p.GetField("A", "close", 0)
	if err != nil {
		t.Fatalf("GetField error: %v", err)
	}
	if len(got) != 10 {
		t.Fatalf("expected 10 values, got %d", len(got))
	}
	for i, v := range got {
		want := float64(i + 1)
		if !approxEqual(v, want) {
			t.Errorf("got[%d] = %.4f, want %.4f", i, v, want)
		}
	}
}

func TestGetField_Lookback5_ReturnsLast5(t *testing.T) {
	p := NewOHLCVDataProvider(map[string][]domain.OHLCV{
		"A": makeBarsN("A", 10),
	})
	got, err := p.GetField("A", "close", 5)
	if err != nil {
		t.Fatalf("GetField error: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("expected 5 values (lookback), got %d", len(got))
	}
	// Last 5 of [1..10] = [6,7,8,9,10]
	for i, v := range got {
		want := float64(i + 6)
		if !approxEqual(v, want) {
			t.Errorf("got[%d] = %.4f, want %.4f", i, v, want)
		}
	}
}

func TestGetField_Lookback0_ReturnsAll(t *testing.T) {
	p := NewOHLCVDataProvider(map[string][]domain.OHLCV{
		"A": makeBarsN("A", 7),
	})
	got, err := p.GetField("A", "close", 0)
	if err != nil {
		t.Fatalf("GetField error: %v", err)
	}
	if len(got) != 7 {
		t.Fatalf("lookback=0 should return all 7 bars, got %d", len(got))
	}
}

// ─── GetSymbols ────────────────────────────────────────────────────────

func TestGetSymbols_Sorted(t *testing.T) {
	// Insert in non-alphabetical order.
	bars := map[string][]domain.OHLCV{
		"CCC": makeBarsN("CCC", 3),
		"aaa": makeBarsN("aaa", 3),
		"BBB": makeBarsN("BBB", 3),
	}
	p := NewOHLCVDataProvider(bars)
	got := p.GetSymbols()
	if len(got) != 3 {
		t.Fatalf("expected 3 symbols, got %d: %v", len(got), got)
	}
	// sort.Strings is case-sensitive (uppercase < lowercase in ASCII).
	want := []string{"BBB", "CCC", "aaa"}
	for i, s := range got {
		if s != want[i] {
			t.Errorf("got[%d] = %q, want %q (sorted)", i, s, want[i])
		}
	}
}

func TestGetField_EmptyBars_EmptySymbols(t *testing.T) {
	p := NewOHLCVDataProvider(map[string][]domain.OHLCV{})
	if syms := p.GetSymbols(); len(syms) != 0 {
		t.Errorf("empty bars: expected 0 symbols, got %d: %v", len(syms), syms)
	}
	// GetField on an unknown symbol in an empty provider should return
	// an empty slice (not an error) — the evaluator handles NaN
	// propagation for missing symbols.
	got, err := p.GetField("A", "close", 0)
	if err != nil {
		t.Errorf("empty bars: unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty bars: expected empty slice, got %d values", len(got))
	}
}

// ─── Error paths ───────────────────────────────────────────────────────

func TestGetField_UnknownSymbol_Empty(t *testing.T) {
	p := NewOHLCVDataProvider(map[string][]domain.OHLCV{
		"A": makeBarsN("A", 5),
	})
	got, err := p.GetField("UNKNOWN", "close", 0)
	if err != nil {
		t.Fatalf("unknown symbol: unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("unknown symbol: expected empty slice, got %d values", len(got))
	}
}

func TestGetField_FundamentalField_Error(t *testing.T) {
	p := NewOHLCVDataProvider(map[string][]domain.OHLCV{
		"A": makeBarsN("A", 5),
	})
	_, err := p.GetField("A", "pe", 0)
	if err == nil {
		t.Fatal("fundamental field 'pe': expected error, got nil")
	}
	// 错误信息必须点名是哪个字段 —— 用户看到它才知道要去接财报数据。
	if !strings.Contains(err.Error(), "pe") {
		t.Errorf("error should name the field, got: %v", err)
	}
}

// TestFundamentalSeries_PITAlignment：财报按**可用日**对齐，不是报告期。
//
// 三季报 9/30 截止、10/25 披露：10/01 那根 K 线必须还看不见它。这就是
// P0-1 修的前视偏差，换个入口很容易再犯一次。
func TestFundamentalSeries_PITAlignment(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := make([]domain.OHLCV, 5)
	for i := range bars {
		bars[i] = domain.OHLCV{
			Symbol: "A", Date: base.AddDate(0, 0, i), Close: float64(i + 1),
		}
	}
	// 第一期 1/01 可用（PE=10）；第二期 1/03 才可用（PE=20）。
	records := map[string][]domain.Fundamental{
		"A": {
			{Symbol: "A", Date: base, PE: fptr(10)},
			{Symbol: "A", Date: base.AddDate(0, 0, 2), PE: fptr(20)},
		},
	}
	p := NewOHLCVDataProviderWithFundamentals(
		map[string][]domain.OHLCV{"A": bars}, records)

	got, err := p.GetField("A", "pe", 0)
	if err != nil {
		t.Fatalf("GetField(pe): %v", err)
	}
	want := []float64{10, 10, 20, 20, 20}
	for i, w := range want {
		if !approxEqual(got[i], w) {
			t.Errorf("pe[%d] (bar date %s) = %.2f, want %.2f",
				i, bars[i].Date.Format("2006-01-02"), got[i], w)
		}
	}
}

// TestFundamentalSeries_MissingIsNaN：没披露的项必须是 NaN，不能是 0。
// PE=0 会被读成「白送的股票」（P2-10）。
func TestFundamentalSeries_MissingIsNaN(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := []domain.OHLCV{
		{Symbol: "A", Date: base, Close: 1},
		{Symbol: "A", Date: base.AddDate(0, 0, 1), Close: 2},
	}
	records := map[string][]domain.Fundamental{
		// 这一期只披露了 PB，PE 没披露
		"A": {{Symbol: "A", Date: base, PB: fptr(1.5)}},
	}
	p := NewOHLCVDataProviderWithFundamentals(
		map[string][]domain.OHLCV{"A": bars}, records)

	got, err := p.GetField("A", "pe", 0)
	if err != nil {
		t.Fatalf("GetField(pe): %v", err)
	}
	for i, v := range got {
		if v == 0 {
			t.Fatalf("pe[%d] = 0 —— 缺失被当成了 0，会被读成「极便宜」", i)
		}
		if !mathIsNaN(v) {
			t.Errorf("pe[%d] = %v, want NaN", i, v)
		}
	}
}

// TestFundamentalSeries_NegativeMultipleIsNaN：PE 为负不是「便宜」，是亏损。
//
// 若原样返回 -5，`neg(pe)` 会把它排成全场最便宜 —— 经典价值陷阱。
func TestFundamentalSeries_NegativeMultipleIsNaN(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := []domain.OHLCV{{Symbol: "A", Date: base, Close: 1}}
	records := map[string][]domain.Fundamental{
		"A": {{Symbol: "A", Date: base, PE: fptr(-5), PB: fptr(-2), ROE: fptr(-8)}},
	}
	p := NewOHLCVDataProviderWithFundamentals(
		map[string][]domain.OHLCV{"A": bars}, records)

	for _, f := range []string{"pe", "pb"} {
		got, err := p.GetField("A", f, 0)
		if err != nil {
			t.Fatalf("GetField(%s): %v", f, err)
		}
		if !mathIsNaN(got[0]) {
			t.Errorf("%s = %v, want NaN（负值不能进入「越便宜越好」的排名）", f, got[0])
		}
	}
	// ROE 为负是有意义的差，原样返回。
	got, err := p.GetField("A", "roe", 0)
	if err != nil {
		t.Fatalf("GetField(roe): %v", err)
	}
	if !approxEqual(got[0], -8) {
		t.Errorf("roe = %.2f, want -8（盈利率为负是真实的差，不该抹掉）", got[0])
	}
}

func fptr(v float64) *float64 { return &v }

func mathIsNaN(v float64) bool { return v != v }

// ─── OBS-08 切片 1：revenue / profit 接线 + 两类「不可用」报错 ──────────

// TestFundamentalValue_RevenueProfit：revenue→f.Revenue、profit→f.NetProfit。
// domain.Fundamental 里这两个字段早已存在，只是此前没在 fundamentalValue
// 里接线 —— 这个测试证明补上后能真的求出值（不是 NaN、不是 0）。
func TestFundamentalValue_RevenueProfit(t *testing.T) {
	f := domain.Fundamental{Revenue: fptr(1234.5), NetProfit: fptr(-67.8)}

	gotRev, ok := fundamentalValue(f, "revenue")
	if !ok {
		t.Fatal("revenue 应可取值")
	}
	if !approxEqual(gotRev, 1234.5) {
		t.Errorf("revenue = %.2f, want 1234.5（应来自 f.Revenue）", gotRev)
	}

	gotProfit, ok := fundamentalValue(f, "profit")
	if !ok {
		t.Fatal("profit 应可取值")
	}
	// 净利润为负是有意义的（亏损），必须原样返回，不能被抹成 NaN/0。
	if !approxEqual(gotProfit, -67.8) {
		t.Errorf("profit = %.2f, want -67.8（应来自 f.NetProfit，且负值保留）", gotProfit)
	}
}

// TestGetField_RevenueProfit_EndToEnd 走一遍 GetField 端到端（PIT 对齐）。
func TestGetField_RevenueProfit_EndToEnd(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := []domain.OHLCV{
		{Symbol: "A", Date: base, Close: 1},
		{Symbol: "A", Date: base.AddDate(0, 0, 1), Close: 2},
	}
	records := map[string][]domain.Fundamental{
		"A": {{Symbol: "A", Date: base, Revenue: fptr(9), NetProfit: fptr(3)}},
	}
	p := NewOHLCVDataProviderWithFundamentals(
		map[string][]domain.OHLCV{"A": bars}, records)

	for f, want := range map[string]float64{"revenue": 9, "profit": 3} {
		got, err := p.GetField("A", f, 0)
		if err != nil {
			t.Fatalf("GetField(%s): %v", f, err)
		}
		for i, v := range got {
			if !approxEqual(v, want) {
				t.Errorf("%s[%d] = %.2f, want %.2f", f, i, v, want)
			}
		}
	}
}

// TestGetField_SectorIsGroup_NotSupplied：sector 在注册表里（来源 group），
// 但本 provider 不供应 —— 报错必须说「属于 group 数据源、当前 provider 不
// 供应」，而不是笼统的 "unknown field"（OBS-08 3.4）。
func TestGetField_SectorIsGroup_NotSupplied(t *testing.T) {
	p := NewOHLCVDataProvider(map[string][]domain.OHLCV{"A": makeBarsN("A", 3)})
	_, err := p.GetField("A", "sector", 0)
	if err == nil {
		t.Fatal("sector：provider 不供应，应报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, "group") || !strings.Contains(msg, "不供应") {
		t.Errorf("sector 报错应说明「属于 group 数据源、当前 provider 不供应」，got: %v", err)
	}
	if strings.Contains(msg, "未知字段") || strings.Contains(msg, "unknown field") {
		t.Errorf("sector 是注册表里的 group 标签，不应被报成「未知字段」，got: %v", err)
	}
}

// TestGetField_TrulyUnknownField：完全没登记的字段报「未知字段」并附可用清单。
func TestGetField_TrulyUnknownField(t *testing.T) {
	p := NewOHLCVDataProvider(map[string][]domain.OHLCV{"A": makeBarsN("A", 3)})
	_, err := p.GetField("A", "definitely_not_a_field", 0)
	if err == nil {
		t.Fatal("未知字段应报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, "未知字段") {
		t.Errorf("完全未登记的字段应报「未知字段」，got: %v", err)
	}
	if !strings.Contains(msg, "可用字段") {
		t.Errorf("未知字段报错应附可用字段清单，got: %v", err)
	}
}
