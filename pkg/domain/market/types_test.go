package market

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestOHLCV_JSONRoundTrip verifies OHLCV serialization preserves all fields.
func TestOHLCV_JSONRoundTrip(t *testing.T) {
	original := OHLCV{
		Symbol:    "000001.SZ",
		Date:      time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		Open:      10.5,
		High:      11.2,
		Low:       10.3,
		Close:     10.8,
		Volume:    1000000.0,
		Turnover:  10800000.0,
		TradeDays: 1,
		LimitUp:   false,
		LimitDown: true,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var decoded OHLCV
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded != original {
		t.Fatalf("round-trip mismatch:\n  original=%+v\n  decoded =%+v", original, decoded)
	}
}

// TestStock_JSONRoundTrip verifies Stock serialization preserves all fields.
func TestStock_JSONRoundTrip(t *testing.T) {
	original := Stock{
		Symbol:    "600000.SH",
		Name:      "浦发银行",
		Exchange:  "SH",
		Industry:  "银行",
		MarketCap: 3.5e10,
		ListDate:  time.Date(1999, 11, 10, 0, 0, 0, 0, time.UTC),
		Status:    "active",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var decoded Stock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded != original {
		t.Fatalf("round-trip mismatch:\n  original=%+v\n  decoded =%+v", original, decoded)
	}
}

// TestIndexConstituent_JSONRoundTrip verifies IndexConstituent serialization.
func TestIndexConstituent_JSONRoundTrip(t *testing.T) {
	original := IndexConstituent{
		ID:        42,
		IndexCode: "000300.SH",
		Symbol:    "000001.SZ",
		InDate:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		OutDate:   time.Time{}, // zero value (still in index)
		Weight:    0.025,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var decoded IndexConstituent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded != original {
		t.Fatalf("round-trip mismatch:\n  original=%+v\n  decoded =%+v", original, decoded)
	}
}

// TestSplit_JSONRoundTrip verifies Split serialization.
func TestSplit_JSONRoundTrip(t *testing.T) {
	original := Split{
		ID:           1,
		Symbol:       "000002.SZ",
		TradeDate:    time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		AnnDate:      time.Date(2025, 5, 15, 0, 0, 0, 0, time.UTC),
		StkDivRatio:  0.1,
		CashDivRatio: 0.05,
		Currency:     "CNY",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var decoded Split
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded != original {
		t.Fatalf("round-trip mismatch:\n  original=%+v\n  decoded =%+v", original, decoded)
	}
}

// TestDividend_JSONRoundTrip verifies Dividend serialization.
func TestDividend_JSONRoundTrip(t *testing.T) {
	original := Dividend{
		ID:        7,
		Symbol:    "600519.SH",
		AnnDate:   time.Date(2025, 3, 28, 0, 0, 0, 0, time.UTC),
		RecDate:   time.Date(2025, 4, 15, 0, 0, 0, 0, time.UTC),
		PayDate:   time.Date(2025, 4, 16, 0, 0, 0, 0, time.UTC),
		DivAmt:    25.91,
		StkDiv:    0.0,
		StkRatio:  1.0,
		CashRatio: 0.25,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var decoded Dividend
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded != original {
		t.Fatalf("round-trip mismatch:\n  original=%+v\n  decoded =%+v", original, decoded)
	}
}

// f64 把常量转成 *float64 —— Fundamental 的数值字段是指针，
// nil 表示"未披露"，非 nil 才是真的数。
func f64(v float64) *float64 { return &v }

// TestFundamental_JSONRoundTrip verifies Fundamental serialization.
func TestFundamental_JSONRoundTrip(t *testing.T) {
	original := Fundamental{
		Symbol:       "000001.SZ",
		Date:         time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC),
		PE:           f64(8.5),
		PB:           f64(0.7),
		PS:           f64(1.2),
		ROE:          f64(12.3),
		ROA:          f64(1.1),
		DebtToEquity: f64(95.4),
		GrossMargin:  f64(50.2),
		NetMargin:    f64(25.1),
		Revenue:      f64(1.2e11),
		NetProfit:    f64(3.0e10),
		TotalAssets:  f64(8.5e12),
		TotalLiab:    f64(7.8e12),
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var decoded Fundamental
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	// 指针字段不能用 == 比（比的是地址，永远不等）—— 比序列化后的字节，
	// 那里比的才是值。
	redata, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("Re-marshal failed: %v", err)
	}
	if string(redata) != string(data) {
		t.Fatalf("round-trip mismatch:\n  original=%s\n  decoded =%s", data, redata)
	}
}

// TestFundamentalData_JSONRoundTrip verifies FundamentalData serialization
// with all *float64 pointers populated.
func TestFundamentalData_JSONRoundTrip(t *testing.T) {
	pe, pb, ps := 8.5, 0.7, 1.2
	roe, roa := 12.3, 1.1
	d2e, gm, nm := 95.4, 50.2, 25.1
	rev, np, ta, tl := 1.2e11, 3.0e10, 8.5e12, 7.8e12
	original := FundamentalData{
		ID:           99,
		TsCode:       "000001.SZ",
		TradeDate:    time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC),
		AnnDate:      time.Date(2025, 4, 28, 0, 0, 0, 0, time.UTC),
		EndDate:      time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC),
		PE:           &pe,
		PB:           &pb,
		PS:           &ps,
		ROE:          &roe,
		ROA:          &roa,
		DebtToEquity: &d2e,
		GrossMargin:  &gm,
		NetMargin:    &nm,
		Revenue:      &rev,
		NetProfit:    &np,
		TotalAssets:  &ta,
		TotalLiab:    &tl,
		CreatedAt:    time.Date(2025, 4, 29, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var decoded FundamentalData
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	// Compare non-pointer fields directly
	if decoded.ID != original.ID {
		t.Fatalf("ID mismatch: %d != %d", decoded.ID, original.ID)
	}
	if decoded.TsCode != original.TsCode {
		t.Fatalf("TsCode mismatch: %q != %q", decoded.TsCode, original.TsCode)
	}
	// Compare each *float64 by dereferencing
	for _, c := range []struct {
		name      string
		got, want *float64
	}{
		{"PE", decoded.PE, original.PE},
		{"PB", decoded.PB, original.PB},
		{"PS", decoded.PS, original.PS},
		{"ROE", decoded.ROE, original.ROE},
		{"ROA", decoded.ROA, original.ROA},
		{"DebtToEquity", decoded.DebtToEquity, original.DebtToEquity},
		{"GrossMargin", decoded.GrossMargin, original.GrossMargin},
		{"NetMargin", decoded.NetMargin, original.NetMargin},
		{"Revenue", decoded.Revenue, original.Revenue},
		{"NetProfit", decoded.NetProfit, original.NetProfit},
		{"TotalAssets", decoded.TotalAssets, original.TotalAssets},
		{"TotalLiab", decoded.TotalLiab, original.TotalLiab},
	} {
		if c.got == nil || c.want == nil {
			t.Fatalf("%s: pointer lost (got=%v, want=%v)", c.name, c.got, c.want)
		}
		if *c.got != *c.want {
			t.Fatalf("%s: value mismatch: %v != %v", c.name, *c.got, *c.want)
		}
	}
}

// TestFundamentalData_NilPointers verifies that nil *float64 fields
// serialize to JSON `null` (not 0) and round-trip back to nil.
// This is the persistence-view property of FundamentalData: "missing"
// must be distinguishable from "zero".
func TestFundamentalData_NilPointers(t *testing.T) {
	original := FundamentalData{
		ID:        1,
		TsCode:    "000001.SZ",
		TradeDate: time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC),
		// All *float64 fields left nil
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	// JSON output MUST contain "pe":null, not "pe":0
	jsonStr := string(data)
	for _, fieldName := range []string{`"pe":null`, `"pb":null`, `"ps":null`, `"roe":null`, `"roa":null`} {
		if !contains(jsonStr, fieldName) {
			t.Fatalf("expected %s in JSON, got: %s", fieldName, jsonStr)
		}
	}
	var decoded FundamentalData
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	for _, c := range []struct {
		name string
		got  *float64
	}{
		{"PE", decoded.PE},
		{"PB", decoded.PB},
		{"PS", decoded.PS},
		{"ROE", decoded.ROE},
		{"ROA", decoded.ROA},
		{"DebtToEquity", decoded.DebtToEquity},
		{"GrossMargin", decoded.GrossMargin},
		{"NetMargin", decoded.NetMargin},
		{"Revenue", decoded.Revenue},
		{"NetProfit", decoded.NetProfit},
		{"TotalAssets", decoded.TotalAssets},
		{"TotalLiab", decoded.TotalLiab},
	} {
		if c.got != nil {
			t.Fatalf("%s: nil did not round-trip (got %v, want nil)", c.name, c.got)
		}
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestProvider_IsInterface verifies Provider is declared as an interface
// and a nil implementation satisfies it (compile-time check).
type nilProvider struct{}

func (nilProvider) GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]OHLCV, error) {
	return nil, nil
}
func (nilProvider) GetFundamental(ctx context.Context, symbol string, date time.Time) (*Fundamental, error) {
	return nil, nil
}
func (nilProvider) GetStocks(ctx context.Context, exchange string) ([]Stock, error) {
	return nil, nil
}
func (nilProvider) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}
func (nilProvider) GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error) {
	return nil, nil
}

func TestProvider_IsInterface(t *testing.T) {
	var p Provider = nilProvider{}
	if p == nil {
		t.Fatal("nilProvider should satisfy Provider interface")
	}
}
