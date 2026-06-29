package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOHLCV_ZeroValues(t *testing.T) {
	var o OHLCV
	if o.Symbol != "" {
		t.Errorf("expected empty Symbol, got %q", o.Symbol)
	}
	if !o.Date.IsZero() {
		t.Error("expected zero Date")
	}
	if o.Open != 0 || o.High != 0 || o.Low != 0 || o.Close != 0 {
		t.Error("expected zero OHLC values")
	}
	if o.Volume != 0 || o.Turnover != 0 {
		t.Error("expected zero volume/turnover")
	}
	if o.TradeDays != 0 {
		t.Errorf("expected zero TradeDays, got %d", o.TradeDays)
	}
	if o.LimitUp || o.LimitDown {
		t.Error("expected false limit flags")
	}
}

func TestOHLCV_JSONRoundTrip(t *testing.T) {
	original := OHLCV{
		Symbol:    "000001.SZ",
		Date:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		Open:      10.5,
		High:      11.0,
		Low:       10.2,
		Close:     10.8,
		Volume:    1000000,
		Turnover:  10800000,
		TradeDays: 1,
		LimitUp:   false,
		LimitDown: false,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded OHLCV
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Symbol != original.Symbol {
		t.Errorf("Symbol: expected %q, got %q", original.Symbol, decoded.Symbol)
	}
	if !decoded.Date.Equal(original.Date) {
		t.Errorf("Date: expected %v, got %v", original.Date, decoded.Date)
	}
	if decoded.Open != original.Open {
		t.Errorf("Open: expected %f, got %f", original.Open, decoded.Open)
	}
	if decoded.High != original.High {
		t.Errorf("High: expected %f, got %f", original.High, decoded.High)
	}
	if decoded.LimitUp != original.LimitUp {
		t.Errorf("LimitUp: expected %v, got %v", original.LimitUp, decoded.LimitUp)
	}
}

func TestOHLCV_JSONFieldNames(t *testing.T) {
	o := OHLCV{Symbol: "TEST", Close: 1.5}
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	jsonStr := string(data)
	expectedFields := []string{`"symbol"`, `"date"`, `"open"`, `"high"`, `"low"`, `"close"`, `"volume"`, `"turnover"`, `"trade_days"`, `"limit_up"`, `"limit_down"`}
	for _, field := range expectedFields {
		if !contains(jsonStr, field) {
			t.Errorf("expected JSON to contain %s, got: %s", field, jsonStr)
		}
	}
}

func TestDirection_Constants(t *testing.T) {
	tests := []struct {
		name     string
		value    Direction
		expected string
	}{
		{"long", DirectionLong, "long"},
		{"short", DirectionShort, "short"},
		{"close", DirectionClose, "close"},
		{"hold", DirectionHold, "hold"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(tt.value))
			}
		})
	}
}

func TestOrderType_Constants(t *testing.T) {
	tests := []struct {
		name     string
		value    OrderType
		expected string
	}{
		{"market", OrderTypeMarket, "market"},
		{"limit", OrderTypeLimit, "limit"},
		{"stop", OrderTypeStop, "stop"},
		{"trailing", OrderTypeTrailing, "trailing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(tt.value))
			}
		})
	}
}

func TestSignal_ZeroValues(t *testing.T) {
	var s Signal
	if s.Symbol != "" {
		t.Errorf("expected empty Symbol, got %q", s.Symbol)
	}
	if !s.Date.IsZero() {
		t.Error("expected zero Date")
	}
	if s.Direction != "" {
		t.Errorf("expected empty Direction, got %q", s.Direction)
	}
	if s.Strength != 0 || s.CompositeScore != 0 {
		t.Error("expected zero strength/score")
	}
	if s.Factors != nil {
		t.Error("expected nil Factors")
	}
	if s.Metadata != nil {
		t.Error("expected nil Metadata")
	}
	if s.LimitPrice != 0 {
		t.Error("expected zero LimitPrice")
	}
	if s.OrderType != "" {
		t.Errorf("expected empty OrderType, got %q", s.OrderType)
	}
}

func TestSignal_JSONRoundTrip(t *testing.T) {
	original := Signal{
		Symbol:         "000001.SZ",
		Date:           time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		Direction:      DirectionLong,
		Strength:       0.85,
		CompositeScore: 0.72,
		Factors:        map[string]float64{"momentum": 0.8, "value": 0.6},
		Metadata:       map[string]any{"source": "test"},
		LimitPrice:     10.5,
		OrderType:      OrderTypeLimit,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Signal
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Symbol != original.Symbol {
		t.Errorf("Symbol mismatch")
	}
	if decoded.Direction != original.Direction {
		t.Errorf("Direction mismatch")
	}
	if decoded.Strength != original.Strength {
		t.Errorf("Strength mismatch")
	}
	if decoded.Factors["momentum"] != original.Factors["momentum"] {
		t.Errorf("Factors mismatch")
	}
	if decoded.OrderType != original.OrderType {
		t.Errorf("OrderType mismatch")
	}
}

func TestPosition_ZeroValues(t *testing.T) {
	var p Position
	if p.Symbol != "" {
		t.Errorf("expected empty Symbol")
	}
	if p.Quantity != 0 || p.AvgCost != 0 || p.CurrentPrice != 0 {
		t.Error("expected zero numeric fields")
	}
	if p.MarketValue != 0 || p.UnrealizedPnL != 0 || p.RealizedPnL != 0 {
		t.Error("expected zero PnL fields")
	}
	if p.Weight != 0 {
		t.Error("expected zero Weight")
	}
	if !p.EntryDate.IsZero() {
		t.Error("expected zero EntryDate")
	}
	if !p.BuyDate.IsZero() {
		t.Error("expected zero BuyDate")
	}
	if p.QuantityToday != 0 || p.QuantityYesterday != 0 {
		t.Error("expected zero T+1 fields")
	}
}

func TestPosition_JSONRoundTrip(t *testing.T) {
	original := Position{
		Symbol:            "000001.SZ",
		Quantity:          1000,
		AvgCost:           10.5,
		CurrentPrice:      11.0,
		MarketValue:       11000,
		UnrealizedPnL:     500,
		Weight:            0.15,
		EntryDate:         time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
		BuyDate:           time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
		QuantityToday:     500,
		QuantityYesterday: 500,
		Metadata:          map[string]any{"trailing_high": 11.5},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Position
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Symbol != original.Symbol {
		t.Errorf("Symbol mismatch")
	}
	if decoded.Quantity != original.Quantity {
		t.Errorf("Quantity mismatch")
	}
	if decoded.QuantityToday != original.QuantityToday {
		t.Errorf("QuantityToday mismatch")
	}
}

func TestPosition_MetadataOmitEmpty(t *testing.T) {
	p := Position{Symbol: "TEST", Quantity: 100}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	// Metadata has omitempty, so it should not appear in JSON when nil
	if contains(string(data), `"metadata"`) {
		t.Errorf("expected metadata to be omitted when nil, got: %s", string(data))
	}
}

func TestPortfolio_ZeroValues(t *testing.T) {
	var p Portfolio
	if p.Cash != 0 {
		t.Error("expected zero Cash")
	}
	if p.Positions != nil {
		t.Error("expected nil Positions")
	}
	if p.TotalValue != 0 {
		t.Error("expected zero TotalValue")
	}
	if p.DailyReturn != 0 {
		t.Error("expected zero DailyReturn")
	}
	if !p.UpdatedAt.IsZero() {
		t.Error("expected zero UpdatedAt")
	}
}

func TestPortfolio_JSONRoundTrip(t *testing.T) {
	original := Portfolio{
		Cash:        100000,
		Positions:   map[string]Position{"000001.SZ": {Symbol: "000001.SZ", Quantity: 100}},
		TotalValue:  101000,
		DailyReturn: 0.01,
		UpdatedAt:   time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Portfolio
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Cash != original.Cash {
		t.Errorf("Cash mismatch")
	}
	if decoded.TotalValue != original.TotalValue {
		t.Errorf("TotalValue mismatch")
	}
	if len(decoded.Positions) != 1 {
		t.Errorf("expected 1 position, got %d", len(decoded.Positions))
	}
}

func TestMarketRegime_ZeroValues(t *testing.T) {
	var m MarketRegime
	if m.Trend != "" {
		t.Errorf("expected empty Trend")
	}
	if m.Volatility != "" {
		t.Errorf("expected empty Volatility")
	}
	if m.Sentiment != 0 {
		t.Error("expected zero Sentiment")
	}
	if !m.Timestamp.IsZero() {
		t.Error("expected zero Timestamp")
	}
}

func TestMarketRegime_JSONRoundTrip(t *testing.T) {
	original := MarketRegime{
		Trend:      "bull",
		Volatility: "low",
		Sentiment:  0.65,
		Timestamp:  time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded MarketRegime
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Trend != original.Trend {
		t.Errorf("Trend mismatch")
	}
	if decoded.Sentiment != original.Sentiment {
		t.Errorf("Sentiment mismatch")
	}
}

func TestPositionSize_ZeroValues(t *testing.T) {
	var ps PositionSize
	if ps.Size != 0 || ps.Weight != 0 || ps.StopLoss != 0 || ps.TakeProfit != 0 || ps.RiskScore != 0 {
		t.Error("expected all zero values")
	}
}

func TestPositionSize_JSONRoundTrip(t *testing.T) {
	original := PositionSize{
		Size:       1000,
		Weight:     0.15,
		StopLoss:   9.5,
		TakeProfit: 12.0,
		RiskScore:  0.3,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded PositionSize
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Size != original.Size {
		t.Errorf("Size mismatch")
	}
	if decoded.Weight != original.Weight {
		t.Errorf("Weight mismatch")
	}
}

func TestStopLossEvent_ZeroValues(t *testing.T) {
	var e StopLossEvent
	if e.Symbol != "" {
		t.Error("expected empty Symbol")
	}
	if e.Type != "" {
		t.Error("expected empty Type")
	}
	if e.Price != 0 || e.Quantity != 0 {
		t.Error("expected zero Price/Quantity")
	}
	if e.Reason != "" {
		t.Error("expected empty Reason")
	}
}

func TestStopLossEvent_JSONRoundTrip(t *testing.T) {
	original := StopLossEvent{
		Symbol:   "000001.SZ",
		Type:     "stop_loss",
		Price:    9.5,
		Quantity: 1000,
		Reason:   "ATR-based stop loss triggered",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded StopLossEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Symbol != original.Symbol {
		t.Errorf("Symbol mismatch")
	}
	if decoded.Type != original.Type {
		t.Errorf("Type mismatch")
	}
}

func TestOHLCV_NegativeValues(t *testing.T) {
	o := OHLCV{
		Symbol: "TEST",
		Open:   -1.0,
		High:   -0.5,
		Low:    -2.0,
		Close:  -1.5,
		Volume: -100,
	}
	// Domain types don't validate; they just hold data.
	// Verify JSON round-trip preserves negative values.
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	var decoded OHLCV
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Open != o.Open {
		t.Errorf("Open: expected %f, got %f", o.Open, decoded.Open)
	}
}

func TestOHLCV_ExtremeValues(t *testing.T) {
	o := OHLCV{
		Symbol: "EXTREME",
		Open:   1e15,
		High:   1e15,
		Low:    1e-15,
		Close:  1e15,
		Volume: 1e18,
	}
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	var decoded OHLCV
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Open != o.Open {
		t.Errorf("Open: expected %e, got %e", o.Open, decoded.Open)
	}
}

func TestSignal_EmptyFactors(t *testing.T) {
	s := Signal{
		Symbol:    "TEST",
		Direction: DirectionLong,
		Factors:   map[string]float64{},
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	var decoded Signal
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Factors == nil {
		t.Error("expected non-nil empty Factors map")
	}
	if len(decoded.Factors) != 0 {
		t.Errorf("expected 0 factors, got %d", len(decoded.Factors))
	}
}

func TestPortfolio_EmptyPositions(t *testing.T) {
	p := Portfolio{
		Cash:       100000,
		Positions:  map[string]Position{},
		TotalValue: 100000,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	var decoded Portfolio
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Positions == nil {
		t.Error("expected non-nil empty Positions map")
	}
	if len(decoded.Positions) != 0 {
		t.Errorf("expected 0 positions, got %d", len(decoded.Positions))
	}
}

func TestConfig_Types(t *testing.T) {
	c := Config{
		Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "test"},
		Redis:    RedisConfig{URL: "redis://localhost:6379"},
		Services: map[string]ServiceConfig{"analysis": {Host: "localhost", Port: 8085}},
		Tushare:  TushareConfig{Token: "test-token", BaseURL: "http://api.tushare.pro", MaxRetries: 3},
	}
	if c.Database.Host != "localhost" {
		t.Errorf("expected localhost, got %s", c.Database.Host)
	}
	if c.Database.Port != 5432 {
		t.Errorf("expected 5432, got %d", c.Database.Port)
	}
	if c.Redis.URL != "redis://localhost:6379" {
		t.Errorf("expected redis URL, got %s", c.Redis.URL)
	}
	if c.Services["analysis"].Port != 8085 {
		t.Errorf("expected 8085, got %d", c.Services["analysis"].Port)
	}
	if c.Tushare.MaxRetries != 3 {
		t.Errorf("expected 3 retries, got %d", c.Tushare.MaxRetries)
	}
}

func TestStock_JSONRoundTrip(t *testing.T) {
	original := Stock{
		Symbol:    "000001.SZ",
		Name:      "平安银行",
		Exchange:  "SZ",
		Industry:  "银行",
		MarketCap: 30000000000,
		ListDate:  time.Date(1991, 4, 3, 0, 0, 0, 0, time.UTC),
		Status:    "active",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	var decoded Stock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Symbol != original.Symbol {
		t.Errorf("Symbol mismatch")
	}
	if decoded.MarketCap != original.MarketCap {
		t.Errorf("MarketCap mismatch")
	}
}

func TestRiskMetrics_ZeroValues(t *testing.T) {
	var rm RiskMetrics
	if rm.Volatility != 0 || rm.Beta != 0 || rm.SharpeRatio != 0 {
		t.Error("expected zero values")
	}
	if rm.MaxDrawdown != 0 || rm.VaR95 != 0 || rm.CVaR95 != 0 {
		t.Error("expected zero risk values")
	}
}

func TestRiskMetrics_JSONRoundTrip(t *testing.T) {
	original := RiskMetrics{
		Volatility:   0.15,
		Beta:         1.2,
		SharpeRatio:  1.5,
		SortinoRatio: 2.0,
		MaxDrawdown:  -0.2,
		VaR95:        -0.03,
		CVaR95:       -0.04,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	var decoded RiskMetrics
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.SharpeRatio != original.SharpeRatio {
		t.Errorf("SharpeRatio mismatch")
	}
}

func TestOrder_JSONRoundTrip(t *testing.T) {
	original := Order{
		ID:         "order-1",
		Symbol:     "000001.SZ",
		Direction:  DirectionLong,
		OrderType:  OrderTypeLimit,
		Quantity:   1000,
		LimitPrice: 10.5,
		Timestamp:  time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		Status:     "filled",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	var decoded Order
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Symbol != original.Symbol {
		t.Errorf("Symbol mismatch")
	}
	if decoded.OrderType != original.OrderType {
		t.Errorf("OrderType mismatch")
	}
}

func TestOrder_OptionalFieldsOmitEmpty(t *testing.T) {
	o := Order{ID: "test", Symbol: "TEST", Direction: DirectionLong, OrderType: OrderTypeMarket}
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	// StopPrice, TrailAmount, TrailPercent, HighWaterMark have omitempty
	jsonStr := string(data)
	for _, field := range []string{`"stop_price"`, `"trail_amount"`, `"trail_percent"`, `"high_water_mark"`} {
		if contains(jsonStr, field) {
			t.Errorf("expected %s to be omitted, got: %s", field, jsonStr)
		}
	}
}

func TestBacktestResult_ZeroValues(t *testing.T) {
	var br BacktestResult
	if br.TotalReturn != 0 || br.SharpeRatio != 0 || br.MaxDrawdown != 0 {
		t.Error("expected zero values")
	}
	if br.TotalTrades != 0 || br.WinTrades != 0 || br.LoseTrades != 0 {
		t.Error("expected zero trade counts")
	}
	if br.PortfolioValues != nil || br.Trades != nil {
		t.Error("expected nil slices")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
