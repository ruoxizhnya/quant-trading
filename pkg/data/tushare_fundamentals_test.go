package data

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestNormalizeFundamentalsData_CompleteData tests normalizeFundamentalsData
// with a complete data response from Tushare financial_data API.
func TestNormalizeFundamentalsData_CompleteData(t *testing.T) {
	client := &TushareClient{}

	resp := &TushareResponse{
		Code: 0,
		Msg:  "ok",
		Data: TushareData{
			Fields: []string{"ts_code", "ann_date", "end_date", "pe", "pb", "ps", "roe", "roa", "debt_to_equity", "gross_margin", "net_margin", "revenue", "net_profit", "total_assets", "total_liab"},
			Items: [][]any{
				{"600000.SH", "20241025", "20240930", 12.5, 1.2, 1.8, 0.15, 0.08, 0.5, 0.30, 0.15, 1000000000.0, 150000000.0, 5000000000.0, 2000000000.0},
				{"000001.SZ", "20241020", "20240930", 8.3, 1.1, 1.5, 0.12, 0.06, 0.4, 0.28, 0.12, 800000000.0, 96000000.0, 4000000000.0, 1600000000.0},
				{"600036.SH", "20241022", "20240930", 6.5, 1.0, 1.2, 0.18, 0.10, 0.3, 0.35, 0.18, 2000000000.0, 360000000.0, 9000000000.0, 3000000000.0},
			},
		},
	}

	result := client.normalizeFundamentalsData(resp)

	assert.Equal(t, 3, len(result), "should return 3 records")

	// Verify first record
	r := result[0]
	assert.Equal(t, "600000.SH", r.TsCode)
	expectedTradeDate, _ := time.Parse("20060102", "20240930")
	expectedAnnDate, _ := time.Parse("20060102", "20241025")
	assert.Equal(t, expectedTradeDate, r.TradeDate)
	assert.Equal(t, expectedAnnDate, r.AnnDate)
	assert.Equal(t, expectedTradeDate, r.EndDate)

	// Verify all factor fields are non-nil and correct
	assert.NotNil(t, r.PE)
	assert.NotNil(t, r.PB)
	assert.NotNil(t, r.PS)
	assert.NotNil(t, r.ROE)
	assert.NotNil(t, r.ROA)
	assert.NotNil(t, r.DebtToEquity)
	assert.NotNil(t, r.GrossMargin)
	assert.NotNil(t, r.NetMargin)
	assert.NotNil(t, r.Revenue)
	assert.NotNil(t, r.NetProfit)
	assert.NotNil(t, r.TotalAssets)
	assert.NotNil(t, r.TotalLiab)

	assert.InDelta(t, 12.5, *r.PE, 0.001)
	assert.InDelta(t, 1.2, *r.PB, 0.001)
	assert.InDelta(t, 1.8, *r.PS, 0.001)
	assert.InDelta(t, 0.15, *r.ROE, 0.001)
	assert.InDelta(t, 0.08, *r.ROA, 0.001)
	assert.InDelta(t, 0.5, *r.DebtToEquity, 0.001)
	assert.InDelta(t, 0.30, *r.GrossMargin, 0.001)
	assert.InDelta(t, 0.15, *r.NetMargin, 0.001)
	assert.InDelta(t, 1000000000.0, *r.Revenue, 0.001)
	assert.InDelta(t, 150000000.0, *r.NetProfit, 0.001)
	assert.InDelta(t, 5000000000.0, *r.TotalAssets, 0.001)
	assert.InDelta(t, 2000000000.0, *r.TotalLiab, 0.001)

	// Verify second record
	r2 := result[1]
	assert.Equal(t, "000001.SZ", r2.TsCode)
	assert.NotNil(t, r2.PE)
	assert.InDelta(t, 8.3, *r2.PE, 0.001)
}

// TestNormalizeFundamentalsData_NilFields tests normalizeFundamentalsData
// when PE, PB and other fields can be nil in the response.
func TestNormalizeFundamentalsData_NilFields(t *testing.T) {
	client := &TushareClient{}

	tests := []struct {
		name            string
		resp            *TushareResponse
		wantCount       int
		wantPENil       bool
		wantPBNil       bool
		wantROENil      bool
		wantRevNil      bool
	}{
		{
			name: "all numeric fields nil",
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
			wantCount:  1,
			wantPENil:  true,
			wantPBNil:  true,
			wantROENil: true,
			wantRevNil: true,
		},
		{
			name: "only PE and PB nil, others valid",
			resp: &TushareResponse{
				Code: 0,
				Msg:  "ok",
				Data: TushareData{
					Fields: []string{"ts_code", "ann_date", "end_date", "pe", "pb", "ps", "roe", "roa", "debt_to_equity", "gross_margin", "net_margin", "revenue", "net_profit", "total_assets", "total_liab"},
					Items: [][]any{
						{"600000.SH", "20241025", "20240930", nil, nil, 1.8, 0.15, 0.08, 0.5, 0.30, 0.15, 1000000000.0, 150000000.0, 5000000000.0, 2000000000.0},
					},
				},
			},
			wantCount:  1,
			wantPENil:  true,
			wantPBNil:  true,
			wantROENil: false,
			wantRevNil: false,
		},
		{
			name: "mixed nil and valid values",
			resp: &TushareResponse{
				Code: 0,
				Msg:  "ok",
				Data: TushareData{
					Fields: []string{"ts_code", "ann_date", "end_date", "pe", "pb", "ps", "roe", "roa", "debt_to_equity", "gross_margin", "net_margin", "revenue", "net_profit", "total_assets", "total_liab"},
					Items: [][]any{
						{"600000.SH", "20241025", "20240930", 12.5, nil, nil, 0.15, nil, 0.5, nil, 0.15, nil, 150000000.0, 5000000000.0, nil},
					},
				},
			},
			wantCount:  1,
			wantPENil:  false,
			wantPBNil:  true,
			wantROENil: false,
			wantRevNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := client.normalizeFundamentalsData(tc.resp)
			assert.Equal(t, tc.wantCount, len(result))
			if tc.wantCount > 0 {
				r := result[0]
				if tc.wantPENil {
					assert.Nil(t, r.PE, "PE should be nil")
				} else {
					assert.NotNil(t, r.PE, "PE should not be nil")
				}
				if tc.wantPBNil {
					assert.Nil(t, r.PB, "PB should be nil")
				} else {
					assert.NotNil(t, r.PB, "PB should not be nil")
				}
				if tc.wantROENil {
					assert.Nil(t, r.ROE, "ROE should be nil")
				} else {
					assert.NotNil(t, r.ROE, "ROE should not be nil")
				}
				if tc.wantRevNil {
					assert.Nil(t, r.Revenue, "Revenue should be nil")
				} else {
					assert.NotNil(t, r.Revenue, "Revenue should not be nil")
				}
			}
		})
	}
}

// TestNormalizeFundamentalsData_EmptyResponse tests normalizeFundamentalsData
// with empty items or no data.
func TestNormalizeFundamentalsData_EmptyResponse(t *testing.T) {
	client := &TushareClient{}

	tests := []struct {
		name      string
		resp      *TushareResponse
		wantCount int
	}{
		{
			name: "empty items slice",
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
						{"600000.SH", "20241025"}, // only 2 fields, need 15
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
		{
			name: "nil items",
			resp: &TushareResponse{
				Code: 0,
				Msg:  "ok",
				Data: TushareData{
					Fields: []string{"ts_code", "ann_date", "end_date", "pe", "pb", "ps", "roe", "roa", "debt_to_equity", "gross_margin", "net_margin", "revenue", "net_profit", "total_assets", "total_liab"},
					Items: nil,
				},
			},
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := client.normalizeFundamentalsData(tc.resp)
			assert.Equal(t, tc.wantCount, len(result))
		})
	}
}

// TestFieldFloatPtr_NilValue tests that fieldFloatPtr returns nil when given a nil value.
func TestFieldFloatPtr_NilValue(t *testing.T) {
	client := &TushareClient{}

	tests := []struct {
		name  string
		item  []any
		idx   int
		desc  string
	}{
		{"nil at index 0", []any{nil}, 0, "nil value at index 0"},
		{"nil at index 3", []any{1.0, 2.0, 3.0, nil}, 3, "nil value at index 3"},
		{"out of bounds", []any{1.5}, 5, "index beyond slice length"},
		{"empty slice", []any{}, 0, "empty slice"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := client.fieldFloatPtr(tc.item, tc.idx)
			assert.Nil(t, result, "fieldFloatPtr(%v, %d) should return nil for %s", tc.item, tc.idx, tc.desc)
		})
	}
}

// TestFieldFloatPtr_ValidValue tests that fieldFloatPtr returns a pointer to the correct value.
func TestFieldFloatPtr_ValidValue(t *testing.T) {
	client := &TushareClient{}

	tests := []struct {
		name    string
		item    []any
		idx     int
		wantVal float64
	}{
		{"float64 value", []any{1.5}, 0, 1.5},
		// Note: fieldFloat only handles float64 and string, not int/int64
		{"string float", []any{"3.14159"}, 0, 3.14159},
		{"string int", []any{"99"}, 0, 99.0},
		{"negative float", []any{-2.5}, 0, -2.5},
		{"zero", []any{0.0}, 0, 0.0},
		{"large number", []any{1e10}, 0, 1e10},
		{"middle index", []any{1.0, 2.0, 3.0, 4.5}, 3, 4.5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := client.fieldFloatPtr(tc.item, tc.idx)
			assert.NotNil(t, result, "fieldFloatPtr should not return nil for valid value")
			assert.InDelta(t, tc.wantVal, *result, 0.0001, "fieldFloatPtr should return pointer to correct value")
		})
	}
}
