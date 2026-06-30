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
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error should mention 'not available', got: %v", err)
	}
}
