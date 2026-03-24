package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// redirectTransport creates an http.Transport that redirects all connections
// to the given test server's address. This lets us intercept the hardcoded
// localhost:8081 URL used by the strategies.
func redirectTransport(srv *httptest.Server) *http.Transport {
	return &http.Transport{
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			_ = addr
			return net.Dial("tcp", srv.Listener.Addr().String())
		},
	}
}

func httpClientFor(srv *httptest.Server) *http.Client {
	return &http.Client{
		Transport: redirectTransport(srv),
		Timeout:   5 * time.Second,
	}
}

func TestMultiFactorParameters(t *testing.T) {
	s := &multiFactorStrategy{
		params: MultiFactorConfig{
			ValueWeight:        0.4,
			QualityWeight:      0.3,
			MomentumWeight:     0.3,
			LookbackDays:       60,
			TopN:               10,
			RebalanceFrequency: "monthly",
		},
	}

	params := s.Parameters()

	expected := map[string]struct {
		def     any
		typeStr string
		min     float64
		max     float64
		desc    string
	}{
		"value_weight":        {0.4, "float", 0.0, 1.0, "Weight for value score (1/PE), normalized"},
		"quality_weight":      {0.3, "float", 0.0, 1.0, "Weight for quality score (ROE), normalized"},
		"momentum_weight":     {0.3, "float", 0.0, 1.0, "Weight for momentum score (return), normalized"},
		"lookback_days":      {60, "int", 5, 250, "Number of days for momentum lookback"},
		"top_n":              {10, "int", 1, 50, "Number of top stocks to buy"},
		"rebalance_frequency": {"monthly", "string", 0, 0, "Rebalance frequency: daily, weekly, monthly"},
	}

	if len(params) != len(expected) {
		t.Fatalf("expected %d parameters, got %d", len(expected), len(params))
	}

	for _, p := range params {
		e, ok := expected[p.Name]
		if !ok {
			t.Fatalf("unexpected parameter: %s", p.Name)
		}
		if p.Type != e.typeStr {
			t.Errorf("[%s] expected type %q, got %q", p.Name, e.typeStr, p.Type)
		}
		if p.Min != e.min {
			t.Errorf("[%s] expected min %v, got %v", p.Name, e.min, p.Min)
		}
		if p.Max != e.max {
			t.Errorf("[%s] expected max %v, got %v", p.Name, e.max, p.Max)
		}
		if p.Description != e.desc {
			t.Errorf("[%s] expected desc %q, got %q", p.Name, e.desc, p.Description)
		}
		switch d := e.def.(type) {
		case float64:
			if p.Default != d {
				t.Errorf("[%s] expected default %v, got %v", p.Name, d, p.Default)
			}
		case int:
			if p.Default != d {
				t.Errorf("[%s] expected default %v, got %v", p.Name, d, p.Default)
			}
		case string:
			if p.Default != d {
				t.Errorf("[%s] expected default %q, got %v", p.Name, d, p.Default)
			}
		}
	}
}

func TestMultiFactorNameDescription(t *testing.T) {
	s := &multiFactorStrategy{}
	if s.Name() != "multi_factor" {
		t.Errorf("expected name 'multi_factor', got %q", s.Name())
	}
	if s.Description() != "Multi-factor: rank stocks by weighted combination of value, quality, and momentum scores" {
		t.Errorf("unexpected description: %q", s.Description())
	}
}

func TestMultiFactorPercentileRank(t *testing.T) {
	tests := []struct {
		name   string
		input  []float64
		output []float64
	}{
		{
			name:   "simple three distinct values",
			input:  []float64{10.0, 20.0, 30.0},
			output: []float64{1.0 / 3.0, 2.0 / 3.0, 1.0},
		},
		{
			name:   "equal values get tied (averaged) rank",
			input:  []float64{1.0, 1.0, 1.0},
			output: []float64{2.0 / 3.0, 2.0 / 3.0, 2.0 / 3.0}, // avg of (1+2+3)/3 = 2/3
		},
		{
			name:   "two distinct values",
			input:  []float64{5.0, 10.0},
			output: []float64{0.5, 1.0},
		},
		{
			name:   "four values with ties at 20.0",
			input:  []float64{10.0, 20.0, 20.0, 30.0},
			output: []float64{0.25, 0.625, 0.625, 1.0}, // avg of 2/4+3/4 = 5/8 = 0.625
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ranks := rankPercentile(tc.input)
			if len(ranks) != len(tc.output) {
				t.Fatalf("expected %d ranks, got %d", len(tc.output), len(ranks))
			}
			for i := range tc.output {
				if math.Abs(ranks[i]-tc.output[i]) > 1e-9 {
					t.Errorf("rank[%d]: expected %v, got %v", i, tc.output[i], ranks[i])
				}
			}
		})
	}

	// Empty input
	if r := rankPercentile(nil); r != nil {
		t.Errorf("expected nil for nil input, got %v", r)
	}
}

func TestMultiFactorCompositeScore(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/screen", func(w http.ResponseWriter, r *http.Request) {
		pe1, pe2, pe3 := 5.0, 15.0, 25.0
		roe1, roe2, roe3 := 0.20, 0.10, 0.05
		json.NewEncoder(w).Encode(struct {
			Count   int                   `json:"count"`
			Results []domain.ScreenResult `json:"results"`
		}{
			Count: 3,
			Results: []domain.ScreenResult{
				{TsCode: "A", PE: &pe1, ROE: &roe1}, // best PE, best ROE
				{TsCode: "B", PE: &pe2, ROE: &roe2},
				{TsCode: "C", PE: &pe3, ROE: &roe3}, // worst PE, worst ROE
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	now := time.Now()
	basePrice := 100.0
	lookback := 60
	makeBars := func(symbol string, endPrice float64) []domain.OHLCV {
		startPrice := basePrice
		bars := make([]domain.OHLCV, lookback+2)
		for i := range bars {
			ratio := float64(i) / float64(len(bars)-1)
			bars[i] = domain.OHLCV{
				Symbol: symbol,
				Date:   now.AddDate(0, 0, -len(bars)+i),
				Close:  startPrice + (endPrice-startPrice)*ratio,
			}
		}
		return bars
	}

	// A: best fundamentals + highest momentum
	// B: medium fundamentals + medium momentum
	// C: worst fundamentals + lowest momentum
	bars := map[string][]domain.OHLCV{
		"A": makeBars("A", basePrice*1.10), // +10%
		"B": makeBars("B", basePrice*1.05), // +5%
		"C": makeBars("C", basePrice*1.01), // +1%
	}

	s := &multiFactorStrategy{
		params: MultiFactorConfig{
			ValueWeight:        0.4,
			QualityWeight:      0.3,
			MomentumWeight:     0.3,
			LookbackDays:       lookback,
			TopN:               10,
			RebalanceFrequency: "daily",
		},
		httpClient: httpClientFor(srv),
		cacheLimit: 10,
	}
	s.cache = sync.Map{}

	portfolio := &domain.Portfolio{UpdatedAt: now, Positions: map[string]domain.Position{}}
	signals, err := s.GenerateSignals(context.Background(), bars, portfolio)
	if err != nil {
		t.Fatalf("GenerateSignals failed: %v", err)
	}

	if len(signals) == 0 {
		t.Fatal("expected at least one buy signal")
	}

	// A should be ranked first (best on all three factors)
	if signals[0].Symbol != "A" {
		t.Errorf("expected stock A to be ranked first, got %q (signals: %+v)", signals[0].Symbol, signals)
	}

	var strengthA float64
	for _, sig := range signals {
		if sig.Symbol == "A" {
			strengthA = sig.Strength
		}
	}
	if strengthA <= 0 {
		t.Errorf("expected positive composite score for A, got %v", strengthA)
	}
}

func TestMultiFactorTopN(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/screen", func(w http.ResponseWriter, r *http.Request) {
		results := make([]domain.ScreenResult, 10)
		for i := range results {
			pe := float64(10 + i)
			roe := 0.05 + float64(i)*0.01
			results[i] = domain.ScreenResult{
				TsCode: fmt.Sprintf("S%02d", i),
				PE:     &pe,
				ROE:    &roe,
			}
		}
		json.NewEncoder(w).Encode(struct {
			Count   int                   `json:"count"`
			Results []domain.ScreenResult `json:"results"`
		}{Count: 10, Results: results})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	now := time.Now()
	basePrice := 100.0
	lookback := 60

	makeBars := func(symbol string, endPrice float64) []domain.OHLCV {
		startPrice := basePrice
		bars := make([]domain.OHLCV, lookback+2)
		for i := range bars {
			ratio := float64(i) / float64(len(bars)-1)
			bars[i] = domain.OHLCV{
				Symbol: symbol,
				Date:   now.AddDate(0, 0, -len(bars)+i),
				Close:  startPrice + (endPrice-startPrice)*ratio,
			}
		}
		return bars
	}

	tests := []struct {
		name     string
		topN     int
		wantBuys int
	}{
		{"topN=3 from 10 stocks", 3, 3},
		{"topN=5 from 10 stocks", 5, 5},
		{"topN=1 from 10 stocks", 1, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bars := map[string][]domain.OHLCV{}
			for i := 0; i < 10; i++ {
				// S00 has highest PE (worst value) but highest ROE and highest momentum
				// We'll assign momentum in reverse so best momentum aligns with best fundamentals
				ret := 1.0 + float64(10-i)*0.01
				bars[fmt.Sprintf("S%02d", i)] = makeBars(fmt.Sprintf("S%02d", i), basePrice*ret)
			}

			s := &multiFactorStrategy{
				params: MultiFactorConfig{
					ValueWeight:        0.4,
					QualityWeight:      0.3,
					MomentumWeight:     0.3,
					LookbackDays:       lookback,
					TopN:               tc.topN,
					RebalanceFrequency: "daily",
				},
				httpClient: httpClientFor(srv),
				cacheLimit: 10,
			}
			s.cache = sync.Map{}

			portfolio := &domain.Portfolio{UpdatedAt: now, Positions: map[string]domain.Position{}}
			signals, err := s.GenerateSignals(context.Background(), bars, portfolio)
			if err != nil {
				t.Fatalf("GenerateSignals failed: %v", err)
			}

			buyCount := 0
			for _, sig := range signals {
				if sig.Action == "buy" {
					buyCount++
				}
			}
			if buyCount != tc.wantBuys {
				t.Errorf("expected %d buy signals, got %d (signals: %+v)", tc.wantBuys, buyCount, signals)
			}
		})
	}
}

func TestMultiFactorZeroPE(t *testing.T) {
	zeroPE := 0.0
	positivePE := 10.0
	roe := 0.15

	mux := http.NewServeMux()
	mux.HandleFunc("/screen", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(struct {
			Count   int                   `json:"count"`
			Results []domain.ScreenResult `json:"results"`
		}{
			Count: 3,
			Results: []domain.ScreenResult{
				{TsCode: "A", PE: &zeroPE, ROE: &roe},         // zero PE — should not crash
				{TsCode: "B", PE: &positivePE, ROE: &roe},
				{TsCode: "C", PE: nil, ROE: nil},               // nil PE — should not crash
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	now := time.Now()
	basePrice := 100.0
	lookback := 60
	makeBars := func() []domain.OHLCV {
		bars := make([]domain.OHLCV, lookback+2)
		for i := range bars {
			ratio := float64(i) / float64(len(bars)-1)
			bars[i] = domain.OHLCV{
				Symbol: "X",
				Date:   now.AddDate(0, 0, -len(bars)+i),
				Close:  basePrice + basePrice*0.05*ratio,
			}
		}
		return bars
	}

	bars := map[string][]domain.OHLCV{
		"A": makeBars(), "B": makeBars(), "C": makeBars(),
	}

	s := &multiFactorStrategy{
		params: MultiFactorConfig{
			ValueWeight:        0.4,
			QualityWeight:      0.3,
			MomentumWeight:     0.3,
			LookbackDays:       lookback,
			TopN:               10,
			RebalanceFrequency: "daily",
		},
		httpClient: httpClientFor(srv),
		cacheLimit: 10,
	}
	s.cache = sync.Map{}

	portfolio := &domain.Portfolio{UpdatedAt: now, Positions: map[string]domain.Position{}}

	// Should not crash with zero/nil PE
	_, err := s.GenerateSignals(context.Background(), bars, portfolio)
	if err != nil {
		t.Fatalf("GenerateSignals should not fail with zero/nil PE: %v", err)
	}
}

func TestMultiFactorNilPEAndROE(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/screen", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(struct {
			Count   int                   `json:"count"`
			Results []domain.ScreenResult `json:"results"`
		}{
			Count: 4,
			Results: []domain.ScreenResult{
				{TsCode: "A", PE: nil, ROE: nil},
				{TsCode: "B", PE: nil, ROE: nil},
				{TsCode: "C", PE: nil, ROE: nil},
				{TsCode: "D", PE: nil, ROE: nil},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	now := time.Now()
	basePrice := 100.0
	lookback := 60
	makeBars := func() []domain.OHLCV {
		bars := make([]domain.OHLCV, lookback+2)
		for i := range bars {
			ratio := float64(i) / float64(len(bars)-1)
			bars[i] = domain.OHLCV{
				Symbol: "X",
				Date:   now.AddDate(0, 0, -len(bars)+i),
				Close:  basePrice + basePrice*0.05*ratio,
			}
		}
		return bars
	}

	bars := map[string][]domain.OHLCV{
		"A": makeBars(), "B": makeBars(), "C": makeBars(), "D": makeBars(),
	}

	s := &multiFactorStrategy{
		params: MultiFactorConfig{
			ValueWeight:        0.4,
			QualityWeight:      0.3,
			MomentumWeight:     0.3,
			LookbackDays:       lookback,
			TopN:               2,
			RebalanceFrequency: "daily",
		},
		httpClient: httpClientFor(srv),
		cacheLimit: 10,
	}
	s.cache = sync.Map{}

	portfolio := &domain.Portfolio{UpdatedAt: now, Positions: map[string]domain.Position{}}

	// Should not crash with all-nil fundamentals
	_, err := s.GenerateSignals(context.Background(), bars, portfolio)
	if err != nil {
		t.Fatalf("GenerateSignals should not fail with nil PE/ROE: %v", err)
	}
}

func TestMultiFactorEmptyBars(t *testing.T) {
	s := &multiFactorStrategy{}
	signals, err := s.GenerateSignals(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("expected no error with nil bars, got %v", err)
	}
	if signals != nil {
		t.Errorf("expected nil signals for nil bars, got %v", signals)
	}

	signals, err = s.GenerateSignals(context.Background(), map[string][]domain.OHLCV{}, nil)
	if err != nil {
		t.Fatalf("expected no error with empty bars, got %v", err)
	}
	if signals != nil {
		t.Errorf("expected nil signals for empty bars, got %v", signals)
	}
}

func TestMultiFactorImplementsStrategyInterface(t *testing.T) {
	var iface interface{} = &multiFactorStrategy{}
	_, ok := iface.(strategy.Strategy)
	if !ok {
		t.Error("multiFactorStrategy does not implement strategy.Strategy")
	}
}

func TestMultiFactorAutoRegister(t *testing.T) {
	strategy.DefaultRegistry = strategy.NewRegistry()

	s := &multiFactorStrategy{
		params: MultiFactorConfig{
			ValueWeight:        0.4,
			QualityWeight:      0.3,
			MomentumWeight:     0.3,
			LookbackDays:       60,
			TopN:               10,
			RebalanceFrequency: "monthly",
		},
		httpClient: &http.Client{Timeout: 10 * time.Second},
		cacheLimit: 30,
	}
	if err := strategy.GlobalRegister(s); err != nil {
		t.Fatalf("failed to register multi_factor: %v", err)
	}

	got, err := strategy.GlobalGet("multi_factor")
	if err != nil {
		t.Fatalf("multi_factor not found in registry: %v", err)
	}
	if got.Name() != "multi_factor" {
		t.Errorf("expected name 'multi_factor', got %q", got.Name())
	}
}

func TestMultiFactorCache(t *testing.T) {
	var reqCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/screen", func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		pe := 10.0
		roe := 0.15
		json.NewEncoder(w).Encode(struct {
			Count   int                   `json:"count"`
			Results []domain.ScreenResult `json:"results"`
		}{
			Count: 2,
			Results: []domain.ScreenResult{
				{TsCode: "A", PE: &pe, ROE: &roe},
				{TsCode: "B", PE: &pe, ROE: &roe},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	now := time.Now()
	basePrice := 100.0
	lookback := 60
	makeBars := func() []domain.OHLCV {
		bars := make([]domain.OHLCV, lookback+2)
		for i := range bars {
			ratio := float64(i) / float64(len(bars)-1)
			bars[i] = domain.OHLCV{
				Symbol: "X",
				Date:   now.AddDate(0, 0, -len(bars)+i),
				Close:  basePrice + basePrice*0.05*ratio,
			}
		}
		return bars
	}

	s := &multiFactorStrategy{
		params: MultiFactorConfig{
			ValueWeight:        0.4,
			QualityWeight:      0.3,
			MomentumWeight:     0.3,
			LookbackDays:       lookback,
			TopN:               10,
			RebalanceFrequency: "daily",
		},
		httpClient: httpClientFor(srv),
		cacheLimit: 10,
	}
	s.cache = sync.Map{}

	bars := map[string][]domain.OHLCV{"A": makeBars(), "B": makeBars()}
	dateStr := now.Format("20060102")
	portfolio := &domain.Portfolio{UpdatedAt: now, Positions: map[string]domain.Position{}}

	_, err := s.GenerateSignals(context.Background(), bars, portfolio)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if reqCount != 1 {
		t.Errorf("first call: expected 1 request, got %d", reqCount)
	}

	cached, ok := s.cache.Load(dateStr)
	if !ok {
		t.Fatal("cache should be populated after first call")
	}
	results := cached.([]domain.ScreenResult)
	if len(results) != 2 {
		t.Errorf("expected 2 cached results, got %d", len(results))
	}

	_, err = s.GenerateSignals(context.Background(), bars, portfolio)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if reqCount != 1 {
		t.Errorf("second call (same date): expected 1 request total, got %d", reqCount)
	}
}

// compile-time interface checks
var _ strategy.Strategy = (*multiFactorStrategy)(nil)
var _ strategy.Strategy = (*valueScreeningStrategy)(nil)
