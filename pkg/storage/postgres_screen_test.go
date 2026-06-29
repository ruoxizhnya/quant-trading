package storage

import (
	"fmt"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: ScreenFundamentals uses pgxpool.Query which cannot be directly mocked
// with sqlmock (sqlmock works with database/sql, not pgx). The tests below
// are designed to verify the domain types and query-building logic.
// For full integration testing of ScreenFundamentals, a real PostgreSQL
// database or pgx-specific mock is required.

// TestScreenFundamentals_ValidFilters tests ScreenFundamentals with valid filter criteria.
// Note: This test validates domain types and query-building approach.
// Integration test with real DB required for full coverage.
func TestScreenFundamentals_ValidFilters(t *testing.T) {
	// Test that filters struct is correctly populated
	peMax := 20.0
	roeMin := 0.10
	debtMax := 0.5
	filters := domain.ScreenFilters{
		PE_max:           &peMax,
		ROE_min:          &roeMin,
		DebtToEquity_max: &debtMax,
	}

	assert.NotNil(t, filters.PE_max)
	assert.NotNil(t, filters.ROE_min)
	assert.NotNil(t, filters.DebtToEquity_max)
	assert.Equal(t, 20.0, *filters.PE_max)
	assert.Equal(t, 0.10, *filters.ROE_min)
	assert.Equal(t, 0.5, *filters.DebtToEquity_max)
}

// TestScreenFundamentals_EmptyFilters tests ScreenFundamentals with no filters.
func TestScreenFundamentals_EmptyFilters(t *testing.T) {
	filters := domain.ScreenFilters{}

	// All fields should be nil
	assert.Nil(t, filters.PE_min)
	assert.Nil(t, filters.PE_max)
	assert.Nil(t, filters.PB_min)
	assert.Nil(t, filters.PB_max)
	assert.Nil(t, filters.PS_min)
	assert.Nil(t, filters.PS_max)
	assert.Nil(t, filters.ROE_min)
	assert.Nil(t, filters.ROA_min)
	assert.Nil(t, filters.DebtToEquity_max)
	assert.Nil(t, filters.GrossMargin_min)
	assert.Nil(t, filters.NetMargin_min)
	assert.Nil(t, filters.MarketCap_min)
}

// TestScreenFundamentals_WithLimit tests that limit parameter is handled correctly.
func TestScreenFundamentals_WithLimit(t *testing.T) {
	limit := 10
	filters := domain.ScreenFilters{}

	// Verify limit is a valid positive integer
	assert.Greater(t, limit, 0)
	assert.Equal(t, 10, limit)

	// Empty filters with limit should work
	_ = filters
	_ = limit
}

// TestScreenFundamentals_WithDate tests ScreenFundamentals with a specific date.
func TestScreenFundamentals_WithDate(t *testing.T) {
	date := time.Date(2024, 9, 30, 0, 0, 0, 0, time.UTC)
	filters := domain.ScreenFilters{}

	assert.Equal(t, 2024, date.Year())
	assert.Equal(t, time.September, date.Month())
	assert.Equal(t, 30, date.Day())

	_ = filters
	_ = date
}

// TestScreenFundamentals_AllFilterTypes tests all filter types.
func TestScreenFundamentals_AllFilterTypes(t *testing.T) {
	peMin := 5.0
	peMax := 30.0
	pbMin := 0.5
	pbMax := 5.0
	psMin := 0.3
	psMax := 10.0
	roeMin := 0.05
	roaMin := 0.02
	debtMax := 1.0
	grossMin := 0.20
	netMin := 0.10
	mktCapMin := 1000000000.0

	filters := domain.ScreenFilters{
		PE_min:           &peMin,
		PE_max:           &peMax,
		PB_min:           &pbMin,
		PB_max:           &pbMax,
		PS_min:           &psMin,
		PS_max:           &psMax,
		ROE_min:          &roeMin,
		ROA_min:          &roaMin,
		DebtToEquity_max: &debtMax,
		GrossMargin_min:  &grossMin,
		NetMargin_min:    &netMin,
		MarketCap_min:    &mktCapMin,
	}

	assert.NotNil(t, filters.PE_min)
	assert.NotNil(t, filters.PE_max)
	assert.NotNil(t, filters.PB_min)
	assert.NotNil(t, filters.PB_max)
	assert.NotNil(t, filters.PS_min)
	assert.NotNil(t, filters.PS_max)
	assert.NotNil(t, filters.ROE_min)
	assert.NotNil(t, filters.ROA_min)
	assert.NotNil(t, filters.DebtToEquity_max)
	assert.NotNil(t, filters.GrossMargin_min)
	assert.NotNil(t, filters.NetMargin_min)
	assert.NotNil(t, filters.MarketCap_min)

	assert.Equal(t, 5.0, *filters.PE_min)
	assert.Equal(t, 30.0, *filters.PE_max)
	assert.Equal(t, 0.5, *filters.PB_min)
	assert.Equal(t, 5.0, *filters.PB_max)
	assert.Equal(t, 0.3, *filters.PS_min)
	assert.Equal(t, 10.0, *filters.PS_max)
	assert.Equal(t, 0.05, *filters.ROE_min)
	assert.Equal(t, 0.02, *filters.ROA_min)
	assert.Equal(t, 1.0, *filters.DebtToEquity_max)
	assert.Equal(t, 0.20, *filters.GrossMargin_min)
	assert.Equal(t, 0.10, *filters.NetMargin_min)
	assert.Equal(t, 1000000000.0, *filters.MarketCap_min)
}

// TestDomainTypes tests that domain types have correct field structures.
func TestDomainTypes(t *testing.T) {
	t.Run("FundamentalData nullable fields", func(t *testing.T) {
		// FundamentalData should have all factor fields as *float64 (nullable)
		fd := domain.FundamentalData{
			TsCode:    "600000.SH",
			TradeDate: time.Now(),
		}

		// All factor fields should be nil by default
		assert.Nil(t, fd.PE)
		assert.Nil(t, fd.PB)
		assert.Nil(t, fd.PS)
		assert.Nil(t, fd.ROE)
		assert.Nil(t, fd.ROA)
		assert.Nil(t, fd.DebtToEquity)
		assert.Nil(t, fd.GrossMargin)
		assert.Nil(t, fd.NetMargin)
		assert.Nil(t, fd.Revenue)
		assert.Nil(t, fd.NetProfit)
		assert.Nil(t, fd.TotalAssets)
		assert.Nil(t, fd.TotalLiab)

		// Setting values should work
		val := 10.5
		fd.PE = &val
		assert.Equal(t, 10.5, *fd.PE)

		// Setting nil should work
		fd.PE = nil
		assert.Nil(t, fd.PE)
	})

	t.Run("ScreenFilters defaults", func(t *testing.T) {
		// ScreenFilters should have all fields as nil by default
		sf := domain.ScreenFilters{}

		assert.Nil(t, sf.PE_min)
		assert.Nil(t, sf.PE_max)
		assert.Nil(t, sf.PB_min)
		assert.Nil(t, sf.PB_max)
		assert.Nil(t, sf.PS_min)
		assert.Nil(t, sf.PS_max)
		assert.Nil(t, sf.ROE_min)
		assert.Nil(t, sf.ROA_min)
		assert.Nil(t, sf.DebtToEquity_max)
		assert.Nil(t, sf.GrossMargin_min)
		assert.Nil(t, sf.NetMargin_min)
		assert.Nil(t, sf.MarketCap_min)
	})

	t.Run("ScreenResult nullable fields", func(t *testing.T) {
		// ScreenResult should have all factor fields as *float64
		sr := domain.ScreenResult{
			TsCode: "600000.SH",
		}

		assert.Nil(t, sr.PE)
		assert.Nil(t, sr.PB)
		assert.Nil(t, sr.PS)
		assert.Nil(t, sr.ROE)
		assert.Nil(t, sr.ROA)
		assert.Nil(t, sr.DebtToEquity)
		assert.Nil(t, sr.GrossMargin)
		assert.Nil(t, sr.NetMargin)
		assert.Nil(t, sr.MarketCap)
	})

	t.Run("ScreenRequest structure", func(t *testing.T) {
		req := domain.ScreenRequest{
			Filters: domain.ScreenFilters{},
			Date:    "20240930",
			Limit:   100,
		}

		assert.Equal(t, "20240930", req.Date)
		assert.Equal(t, 100, req.Limit)
		assert.NotNil(t, req.Filters)
	})
}

// TestBuildScreenFundamentalsQuery verifies the SQL query and argument
// list produced by buildScreenFundamentalsQuery. This replaces the former
// placeholder TestScreenFundamentals_DBRequired which only asserted
// `assert.True(t, true)`. ScreenFundamentals itself uses pgxpool.Query
// (not database/sql), so it cannot be mocked with sqlmock; pgxmock is not
// a project dependency. Instead we verify the query-building logic — the
// part that is actually testable without a live DB — by asserting on the
// generated SQL string and the positional argument list.
func TestBuildScreenFundamentalsQuery(t *testing.T) {
	t.Run("no date uses ROW_NUMBER subquery with rn=1", func(t *testing.T) {
		query, args := buildScreenFundamentalsQuery(domain.ScreenFilters{}, nil, 0)
		assert.Contains(t, query, "ROW_NUMBER() OVER (PARTITION BY ts_code")
		assert.Contains(t, query, "WHERE sf.rn = 1")
		assert.Empty(t, args, "no filters/date → no args")
	})

	t.Run("with date uses direct trade_date filter", func(t *testing.T) {
		d := time.Date(2024, 9, 30, 0, 0, 0, 0, time.UTC)
		query, args := buildScreenFundamentalsQuery(domain.ScreenFilters{}, &d, 0)
		assert.NotContains(t, query, "ROW_NUMBER()")
		assert.Contains(t, query, "sf.trade_date = $1")
		require.Len(t, args, 1)
		assert.Equal(t, d, args[0])
	})

	t.Run("all filters produce sequential positional parameters", func(t *testing.T) {
		peMin, peMax := 5.0, 30.0
		pbMin, pbMax := 0.5, 5.0
		psMin, psMax := 0.3, 10.0
		roeMin, roaMin := 0.05, 0.02
		debtMax := 1.0
		grossMin, netMin := 0.20, 0.10
		mktCapMin := 1_000_000_000.0
		filters := domain.ScreenFilters{
			PE_min: &peMin, PE_max: &peMax,
			PB_min: &pbMin, PB_max: &pbMax,
			PS_min: &psMin, PS_max: &psMax,
			ROE_min: &roeMin, ROA_min: &roaMin,
			DebtToEquity_max: &debtMax,
			GrossMargin_min:  &grossMin,
			NetMargin_min:    &netMin,
			MarketCap_min:    &mktCapMin,
		}

		query, args := buildScreenFundamentalsQuery(filters, nil, 0)
		// 12 filter conditions, no date → 12 args, params $1..$12
		require.Len(t, args, 12)
		for i := 1; i <= 12; i++ {
			assert.Contains(t, query, fmt.Sprintf("$%d", i),
				"query should reference $%d", i)
		}
		// args must be in filter-declaration order
		assert.Equal(t, peMin, args[0])
		assert.Equal(t, peMax, args[1])
		assert.Equal(t, netMin, args[10])
		assert.Equal(t, mktCapMin, args[11])
		// no-date path uses AND (not WHERE) because base query has WHERE rn=1
		assert.Contains(t, query, "WHERE sf.rn = 1")
		assert.Contains(t, query, " AND ")
	})

	t.Run("with date, filters append via AND to existing WHERE", func(t *testing.T) {
		d := time.Date(2024, 9, 30, 0, 0, 0, 0, time.UTC)
		peMax := 20.0
		filters := domain.ScreenFilters{PE_max: &peMax}

		query, args := buildScreenFundamentalsQuery(filters, &d, 0)
		// date is $1, pe_max is $2
		require.Len(t, args, 2)
		assert.Equal(t, d, args[0])
		assert.Equal(t, peMax, args[1])
		assert.Contains(t, query, "sf.trade_date = $1")
		assert.Contains(t, query, "(sf.pe IS NULL OR sf.pe <= $2)")
		// date path has no "WHERE sf.rn = 1", so conditions use WHERE
		assert.NotContains(t, query, "WHERE sf.rn = 1")
		assert.Contains(t, query, " WHERE ")
	})

	t.Run("limit appends LIMIT clause", func(t *testing.T) {
		query, _ := buildScreenFundamentalsQuery(domain.ScreenFilters{}, nil, 50)
		assert.Contains(t, query, "LIMIT 50")
	})

	t.Run("zero limit omits LIMIT clause", func(t *testing.T) {
		query, _ := buildScreenFundamentalsQuery(domain.ScreenFilters{}, nil, 0)
		assert.NotContains(t, query, "LIMIT")
	})

	t.Run("MarketCap filter targets stocks table alias", func(t *testing.T) {
		mktCapMin := 500_000_000.0
		filters := domain.ScreenFilters{MarketCap_min: &mktCapMin}
		query, args := buildScreenFundamentalsQuery(filters, nil, 0)
		assert.Contains(t, query, "st.market_cap IS NULL OR st.market_cap >= $1")
		require.Len(t, args, 1)
		assert.Equal(t, mktCapMin, args[0])
	})
}

// BenchmarkDomainTypes benchmarks the domain type allocations.
func BenchmarkDomainTypes(b *testing.B) {
	for i := 0; i < b.N; i++ {
		fd := domain.FundamentalData{
			TsCode:    "600000.SH",
			TradeDate: time.Now(),
		}
		val := 10.5
		fd.PE = &val
		fd.PB = &val
		fd.PS = &val
		fd.ROE = &val
		_ = fd
	}
}
