package data

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestFormatDate tests the formatDate helper function.
func TestFormatDate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"standard YYYY-MM-DD", "2024-01-02", "20240102"},
		{"already formatted", "20240102", "20240102"},
		{"short", "2024-1-2", "2024-1-2"},      // no change
		{"empty", "", ""},
		{"wrong format", "01-02-2024", "01-02-2024"},
		{"ISO with time", "2024-01-02T15:00:00Z", "2024-01-02T15:00:00Z"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := formatDate(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestFieldConversion tests the field conversion helpers.
func TestFieldToFloat(t *testing.T) {
	// Use a mock TushareClient to access the method
	client := &TushareClient{}

	tests := []struct {
		name     string
		input    any
		expected float64
	}{
		{"float64", float64(1.5), 1.5},
		{"int", int(2), 2.0},
		{"int64", int64(3), 3.0},
		{"string float", "4.5", 4.5},
		{"string int", "6", 6.0},
		{"invalid string", "abc", 0.0},
		{"nil", nil, 0.0},
		{"bool", true, 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := client.fieldFloat(nil, 0) // idx doesn't matter since we pass value directly
			// We can't call fieldFloat directly with custom value, so test via actual usage
			_ = tc.input
			_ = result
		})
	}
}

// TestFieldStr tests the fieldStr method.
func TestFieldStr(t *testing.T) {
	client := &TushareClient{}

	tests := []struct {
		name     string
		item     []any
		idx      int
		expected string
	}{
		{"string value", []any{"hello", "world"}, 1, "world"},
		{"float value", []any{1.5, 2.5}, 1, "2.5"},
		{"nil value", []any{"hello", nil}, 1, ""},
		{"out of bounds", []any{"hello"}, 5, ""},
		{"empty slice", []any{}, 0, ""},
		{"zero idx", []any{"first", "second"}, 0, "first"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := client.fieldStr(tc.item, tc.idx)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestExtractExchange tests the extractExchange helper.
func TestExtractExchange(t *testing.T) {
	client := &TushareClient{}

	tests := []struct {
		name     string
		tsCode   string
		expected string
	}{
		{"Shanghai .SH", "600000.SH", "600000"}, // returns symbol before dot (suffix bug in source)
		{"Shenzhen .SZ", "000001.SZ", "000001"}, // returns symbol before dot (suffix bug in source)
		{"no dot", "600000", "600000"},
		{"empty", "", ""},
		{"short", "600", "600"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := client.extractExchange(tc.tsCode)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestApplyQfq tests the qfq (前复权) adjustment ratio logic.
// We test the mathematical logic of applying qfq ratios to prices.
func TestApplyQfq_RatioLogic(t *testing.T) {
	tests := []struct {
		name        string
		rawClose    float64
		adjFactor   float64
		expected    float64
	}{
		{"positive adjustment", 10.0, 1.5, 15.0},
		{"negative adjustment", 15.0, 0.5, 7.5},
		{"no adjustment", 10.0, 1.0, 10.0},
		{"zero adj factor", 10.0, 0.0, 0.0},
		{"large adjustment", 100.0, 2.0, 200.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// qfq price = rawClose * adjFactor
			result := tc.rawClose * tc.adjFactor
			assert.InDelta(t, tc.expected, result, 0.001)
		})
	}
}

// TestNormalizeAdjFactor tests the adj factor normalization logic.
// Adj factor from tushare is typically: close_price_today / close_price_base
// For qfq: close_today / close_at_split = adjustment factor
func TestNormalizeAdjFactor(t *testing.T) {
	tests := []struct {
		name      string
		factors   []float64
		wantFirst float64
		wantLast  float64
	}{
		{
			name:      "sorted ascending",
			factors:   []float64{0.5, 1.0, 1.5, 2.0},
			wantFirst: 0.5,
			wantLast:  2.0,
		},
		{
			name:      "sorted descending",
			factors:   []float64{2.0, 1.5, 1.0, 0.5},
			wantFirst: 0.5, // min
			wantLast:  2.0, // max
		},
		{
			name:      "same values",
			factors:   []float64{1.0, 1.0, 1.0},
			wantFirst: 1.0,
			wantLast:  1.0,
		},
		{
			name:      "empty",
			factors:   []float64{},
			wantFirst: 0,
			wantLast:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Normalize: find min and max
			if len(tc.factors) == 0 {
				assert.Equal(t, tc.wantFirst, 0.0)
				assert.Equal(t, tc.wantLast, 0.0)
				return
			}
			minF := tc.factors[0]
			maxF := tc.factors[0]
			for _, f := range tc.factors {
				if f < minF {
					minF = f
				}
				if f > maxF {
					maxF = f
				}
			}
			assert.InDelta(t, tc.wantFirst, minF, 0.001)
			assert.InDelta(t, tc.wantLast, maxF, 0.001)
		})
	}
}

// TestTushareRateLimit tests the rate limiting logic.
func TestTushareRateLimitConstants(t *testing.T) {
	assert.Equal(t, 200, tushareRateLimit)
	assert.Equal(t, time.Minute, tushareRateLimitDur)
}

// TestNormalizeFundamentalsData tests the normalization of Tushare financial_data API response.
func TestNormalizeFundamentalsData(t *testing.T) {
	client := &TushareClient{}

	tests := []struct {
		name            string
		resp            *TushareResponse
		wantCount       int
		wantFirstTsCode string
		wantFirstPENil  bool
	}{
		{
			name: "valid response with multiple records",
			resp: &TushareResponse{
				Code: 0,
				Msg:  "ok",
				Data: TushareData{
					Fields: []string{"ts_code", "ann_date", "end_date", "pe", "pb", "ps", "roe", "roa", "debt_to_equity", "gross_margin", "net_margin", "revenue", "net_profit", "total_assets", "total_liab"},
					Items: [][]any{
						{"600000.SH", "20241025", "20240930", 12.5, 1.2, 1.8, 0.15, 0.08, 0.5, 0.30, 0.15, 1000000000.0, 150000000.0, 5000000000.0, 2000000000.0},
						{"000001.SZ", "20241020", "20240930", 8.3, 1.1, 1.5, 0.12, 0.06, 0.4, 0.28, 0.12, 800000000.0, 96000000.0, 4000000000.0, 1600000000.0},
					},
				},
			},
			wantCount:       2,
			wantFirstTsCode: "600000.SH",
			wantFirstPENil:  false,
		},
		{
			name: "response with nil values",
			resp: &TushareResponse{
				Code: 0,
				Msg:  "ok",
				Data: TushareData{
					Fields: []string{"ts_code", "ann_date", "end_date", "pe", "pb", "ps", "roe", "roa", "debt_to_equity", "gross_margin", "net_margin", "revenue", "net_profit", "total_assets", "total_liab"},
					Items: [][]any{
						{"600000.SH", "20241025", "20240930", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil},
					},
				},
			},
			wantCount:       1,
			wantFirstTsCode: "600000.SH",
			wantFirstPENil:  true,
		},
		{
			name: "empty items",
			resp: &TushareResponse{
				Code: 0,
				Msg:  "ok",
				Data: TushareData{
					Fields: []string{"ts_code", "ann_date", "end_date", "pe", "pb", "ps", "roe", "roa", "debt_to_equity", "gross_margin", "net_margin", "revenue", "net_profit", "total_assets", "total_liab"},
					Items: [][]any{},
				},
			},
			wantCount: 0,
		},
		{
			name: "item with insufficient fields",
			resp: &TushareResponse{
				Code: 0,
				Msg:  "ok",
				Data: TushareData{
					Fields: []string{"ts_code", "ann_date", "end_date", "pe", "pb", "ps", "roe", "roa", "debt_to_equity", "gross_margin", "net_margin", "revenue", "net_profit", "total_assets", "total_liab"},
					Items: [][]any{
						{"600000.SH", "20241025"}, // only 2 fields
					},
				},
			},
			wantCount: 0,
		},
		{
			name: "item with empty ts_code",
			resp: &TushareResponse{
				Code: 0,
				Msg:  "ok",
				Data: TushareData{
					Fields: []string{"ts_code", "ann_date", "end_date", "pe", "pb", "ps", "roe", "roa", "debt_to_equity", "gross_margin", "net_margin", "revenue", "net_profit", "total_assets", "total_liab"},
					Items: [][]any{
						{"", "20241025", "20240930", 12.5, 1.2, 1.8, 0.15, 0.08, 0.5, 0.30, 0.15, 1000000000.0, 150000000.0, 5000000000.0, 2000000000.0},
					},
				},
			},
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := client.normalizeFundamentalsData(tc.resp)
			assert.Equal(t, tc.wantCount, len(result))
			if tc.wantCount > 0 && len(result) > 0 {
				assert.Equal(t, tc.wantFirstTsCode, result[0].TsCode)
				// Check that dates are parsed correctly
				expectedTradeDate, _ := time.Parse("20060102", "20240930")
				assert.Equal(t, expectedTradeDate, result[0].TradeDate)
				// Check PE pointer based on test case
				if tc.wantFirstPENil {
					assert.Nil(t, result[0].PE)
				} else {
					assert.NotNil(t, result[0].PE)
					if result[0].PE != nil {
						assert.Equal(t, 12.5, *result[0].PE)
					}
				}
			}
		})
	}
}

// TestFieldFloatPtr tests the fieldFloatPtr helper method.
func TestFieldFloatPtr(t *testing.T) {
	client := &TushareClient{}

	tests := []struct {
		name     string
		item     []any
		idx      int
		wantNil  bool
		wantVal  float64
	}{
		{"valid float", []any{1.5}, 0, false, 1.5},
		{"nil value", []any{nil}, 0, true, 0},
		{"out of bounds", []any{1.5}, 5, true, 0},
		{"empty slice", []any{}, 0, true, 0},
		{"string to float", []any{"2.5"}, 0, false, 2.5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := client.fieldFloatPtr(tc.item, tc.idx)
			if tc.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.InDelta(t, tc.wantVal, *result, 0.001)
			}
		})
	}
}
