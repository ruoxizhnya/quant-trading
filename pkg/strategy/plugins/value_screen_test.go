package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

func TestValueScreenParameters(t *testing.T) {
	s := &valueScreeningStrategy{
		params: ValueScreeningConfig{
			PEMax:               30.0,
			PBMax:               3.0,
			ROEMin:              0.1,
			MomentumDays:        60,
			TopN:                10,
			RebalanceFrequency:  "monthly",
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
		"pe_max":              {30.0, "float", 1.0, 100.0, "Maximum PE ratio to include"},
		"pb_max":              {3.0, "float", 0.1, 20.0, "Maximum PB ratio to include"},
		"roe_min":             {0.1, "float", -1.0, 1.0, "Minimum ROE to include (e.g., 0.1 = 10%)"},
		"momentum_days":       {60, "int", 5, 250, "Number of days for momentum lookback"},
		"top_n":               {10, "int", 1, 50, "Number of top stocks to buy"},
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

func TestValueScreenNameDescription(t *testing.T) {
	s := &valueScreeningStrategy{}
	if s.Name() != "value_screening" {
		t.Errorf("expected name 'value_screening', got %q", s.Name())
	}
	if s.Description() != "Value screening: filter stocks by PE, PB, ROE criteria then rank by momentum" {
		t.Errorf("unexpected description: %q", s.Description())
	}
}

func TestValueScreenMomentumRanking(t *testing.T) {
	var reqCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/screen", func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		if r.Method != http.MethodPost {
			http.Error(w, "expected POST", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_ = body
		pe := 10.0
		roe := 0.15
		json.NewEncoder(w).Encode(struct {
			Count   int                   `json:"count"`
			Results []domain.ScreenResult `json:"results"`
		}{
			Count: 3,
			Results: []domain.ScreenResult{
				{TsCode: "A", PE: &pe, ROE: &roe},
				{TsCode: "B", PE: &pe, ROE: &roe},
				{TsCode: "C", PE: &pe, ROE: &roe},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	httpClient := httpClientFor(srv)

	now := time.Now()
	basePrice := 100.0
	momentumDays := 60

	makeBars := func(symbol string, endPrice float64) []domain.OHLCV {
		startPrice := basePrice
		bars := make([]domain.OHLCV, momentumDays+2)
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

	// A: +10%, B: +5%, C: +1%
	// TopN=2 → A and B should get buy signals
	bars := map[string][]domain.OHLCV{
		"A": makeBars("A", basePrice*1.10),
		"B": makeBars("B", basePrice*1.05),
		"C": makeBars("C", basePrice*1.01),
	}

	s := &valueScreeningStrategy{
		params: ValueScreeningConfig{
			PEMax:              30.0,
			PBMax:              3.0,
			ROEMin:             0.05,
			MomentumDays:       momentumDays,
			TopN:               2,
			RebalanceFrequency: "daily",
		},
		httpClient: httpClient,
		cacheLimit: 10,
	}
	s.cache = sync.Map{}

	portfolio := &domain.Portfolio{
		UpdatedAt: now,
		Positions: map[string]domain.Position{},
	}

	signals, err := s.GenerateSignals(context.Background(), bars, portfolio)
	if err != nil {
		t.Fatalf("GenerateSignals failed: %v", err)
	}
	if reqCount == 0 {
		t.Fatal("expected at least 1 API call")
	}

	buySymbols := make(map[string]bool)
	for _, sig := range signals {
		if sig.Action == "buy" {
			buySymbols[sig.Symbol] = true
		}
	}

	if !buySymbols["A"] {
		t.Error("expected stock A (highest momentum) to get buy signal")
	}
	if !buySymbols["B"] {
		t.Error("expected stock B (second highest momentum) to get buy signal")
	}
	if buySymbols["C"] {
		t.Error("stock C (lowest momentum) should NOT get buy signal when topN=2")
	}

	// Verify A's strength > B's strength
	var strengthA, strengthB float64
	for _, sig := range signals {
		if sig.Symbol == "A" {
			strengthA = sig.Strength
		}
		if sig.Symbol == "B" {
			strengthB = sig.Strength
		}
	}
	if strengthA <= strengthB {
		t.Errorf("expected A's strength (%v) > B's (%v) since A has higher momentum", strengthA, strengthB)
	}
}

func TestValueScreenTopN(t *testing.T) {
	momentumDays := 60
	now := time.Now()
	basePrice := 100.0

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

	// Build bars: higher index = higher momentum (S09 best, S00 worst)
	makeBars := func(symbol string, endPrice float64) []domain.OHLCV {
		startPrice := basePrice
		bars := make([]domain.OHLCV, momentumDays+2)
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

	// Build a RoundTripper that redirects localhost:8081 to srv.URL
	transport := &http.Transport{
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			// Redirect any connection to our test server
			_, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			return net.Dial("tcp", srv.Listener.Addr().String())
		},
	}
	httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}

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
				ret := 1.0 + float64(10-i)*0.01 // S00 highest return, S09 lowest
				bars[fmt.Sprintf("S%02d", i)] = makeBars(fmt.Sprintf("S%02d", i), basePrice*ret)
			}

			s := &valueScreeningStrategy{
				params: ValueScreeningConfig{
					PEMax:              30.0,
					PBMax:              3.0,
					ROEMin:             0.01,
					MomentumDays:       momentumDays,
					TopN:               tc.topN,
					RebalanceFrequency: "daily",
				},
				httpClient: httpClient,
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

func TestValueScreenSellSignal(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/screen", func(w http.ResponseWriter, r *http.Request) {
		pe := 10.0
		roe := 0.15
		json.NewEncoder(w).Encode(struct {
			Count   int                   `json:"count"`
			Results []domain.ScreenResult `json:"results"`
		}{
			Count: 3,
			Results: []domain.ScreenResult{
				{TsCode: "A", PE: &pe, ROE: &roe},
				{TsCode: "B", PE: &pe, ROE: &roe},
				{TsCode: "C", PE: &pe, ROE: &roe},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("tcp", srv.Listener.Addr().String())
		},
	}
	httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}

	momentumDays := 60
	now := time.Now()
	basePrice := 100.0
	makeBars := func(symbol string, endPrice float64) []domain.OHLCV {
		startPrice := basePrice
		bars := make([]domain.OHLCV, momentumDays+2)
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

	// A: +10%, B: +5%, C: +1%
	// TopN=2 → C should get sell signal (not in top N)
	bars := map[string][]domain.OHLCV{
		"A": makeBars("A", basePrice*1.10),
		"B": makeBars("B", basePrice*1.05),
		"C": makeBars("C", basePrice*1.01),
	}

	portfolio := &domain.Portfolio{
		UpdatedAt: now,
		Positions: map[string]domain.Position{
			"A": {Symbol: "A"},
			"B": {Symbol: "B"},
			"C": {Symbol: "C"},
		},
	}

	s := &valueScreeningStrategy{
		params: ValueScreeningConfig{
			PEMax:              30.0,
			PBMax:              3.0,
			ROEMin:             0.05,
			MomentumDays:       momentumDays,
			TopN:               2,
			RebalanceFrequency: "daily",
		},
		httpClient: httpClient,
		cacheLimit: 10,
	}
	s.cache = sync.Map{}

	signals, err := s.GenerateSignals(context.Background(), bars, portfolio)
	if err != nil {
		t.Fatalf("GenerateSignals failed: %v", err)
	}

	sellSymbols := make(map[string]bool)
	for _, sig := range signals {
		if sig.Action == "sell" {
			sellSymbols[sig.Symbol] = true
		}
	}

	if !sellSymbols["C"] {
		t.Error("expected stock C (out of top N) to get sell signal")
	}
	if sellSymbols["A"] {
		t.Error("stock A (in top N) should NOT get sell signal")
	}
	if sellSymbols["B"] {
		t.Error("stock B (in top N) should NOT get sell signal")
	}
}

func TestValueScreenCache(t *testing.T) {
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
	httpClient := httpClientFor(srv)

	now := time.Now()
	basePrice := 100.0
	makeBars := func() []domain.OHLCV {
		bars := make([]domain.OHLCV, 62)
		for i := range bars {
			ratio := float64(i) / float64(len(bars)-1)
			bars[i] = domain.OHLCV{
				Symbol: "A",
				Date:   now.AddDate(0, 0, -len(bars)+i),
				Close:  basePrice + basePrice*0.05*ratio,
			}
		}
		return bars
	}

	s := &valueScreeningStrategy{
		params: ValueScreeningConfig{
			PEMax:              30.0,
			PBMax:              3.0,
			ROEMin:             0.05,
			MomentumDays:       60,
			TopN:               10,
			RebalanceFrequency: "daily",
		},
		httpClient: httpClient,
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

func TestValueScreenEmptyBars(t *testing.T) {
	s := &valueScreeningStrategy{}
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

func TestValueScreenZeroMomentum(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/screen", func(w http.ResponseWriter, r *http.Request) {
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
	httpClient := httpClientFor(srv)

	now := time.Now()
	price := 100.0
	momentumDays := 60

	makeFlatBars := func(symbol string) []domain.OHLCV {
		bars := make([]domain.OHLCV, momentumDays+2)
		for i := range bars {
			bars[i] = domain.OHLCV{
				Symbol: symbol,
				Date:   now.AddDate(0, 0, -len(bars)+i),
				Close:  price,
			}
		}
		return bars
	}

	bars := map[string][]domain.OHLCV{
		"A": makeFlatBars("A"), // zero momentum
		"B": makeFlatBars("B"), // zero momentum
	}

	s := &valueScreeningStrategy{
		params: ValueScreeningConfig{
			PEMax:              30.0,
			PBMax:              3.0,
			ROEMin:             0.05,
			MomentumDays:       momentumDays,
			TopN:               10,
			RebalanceFrequency: "daily",
		},
		httpClient: httpClient,
		cacheLimit: 10,
	}
	s.cache = sync.Map{}

	portfolio := &domain.Portfolio{UpdatedAt: now, Positions: map[string]domain.Position{}}
	signals, err := s.GenerateSignals(context.Background(), bars, portfolio)
	if err != nil {
		t.Fatalf("GenerateSignals failed: %v", err)
	}

	// Zero or negative momentum stocks should not get buy signals
	for _, sig := range signals {
		if sig.Action == "buy" && sig.Strength <= 0 {
			t.Errorf("stock %s has zero/negative momentum, should not get buy signal", sig.Symbol)
		}
	}
}

func TestValueScreenImplementsStrategyInterface(t *testing.T) {
	var iface interface{} = &valueScreeningStrategy{}
	_, ok := iface.(strategy.Strategy)
	if !ok {
		t.Error("valueScreeningStrategy does not implement strategy.Strategy")
	}
}

func TestValueScreenAutoRegister(t *testing.T) {
	// Register directly (init already ran, so we use GlobalRegister)
	strategy.DefaultRegistry = strategy.NewRegistry()

	s := &valueScreeningStrategy{
		params: ValueScreeningConfig{
			PEMax:               30.0,
			PBMax:               3.0,
			ROEMin:              0.1,
			MomentumDays:        60,
			TopN:                10,
			RebalanceFrequency:  "monthly",
		},
		httpClient: &http.Client{Timeout: 10 * time.Second},
		cacheLimit: 30,
	}
	if err := strategy.GlobalRegister(s); err != nil {
		t.Fatalf("failed to register value_screening: %v", err)
	}

	got, err := strategy.GlobalGet("value_screening")
	if err != nil {
		t.Fatalf("value_screening not found in registry: %v", err)
	}
	if got.Name() != "value_screening" {
		t.Errorf("expected name 'value_screening', got %q", got.Name())
	}
}
