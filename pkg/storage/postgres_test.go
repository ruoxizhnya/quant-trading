package storage

import (
	"context"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testStore(t *testing.T) *PostgresStore {
	t.Helper()
	ctx := context.Background()
	store, err := NewPostgresStore(ctx,
		"postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable")
	if err != nil {
		t.Skipf("skipping test: cannot connect to DB: %v", err)
	}
	return store
}

func TestNewPostgresStore(t *testing.T) {
	// Use SkipIfNoDB convention — if docker compose postgres is not running,
	// skip the test rather than fail. Matches testStore() helper pattern.
	store := testStore(t)
	require.NotNil(t, store)
	store.Close()
}

func TestPostgresStore_Ping(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	ctx := context.Background()
	err := store.Ping(ctx)
	assert.NoError(t, err)
}

func TestPostgresStore_DB(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	db := store.DB()
	assert.NotNil(t, db)
}

func TestSaveOHLCVBatch_and_GetOHLCV(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	symbol := "TEST_OHLCV_001.SH"
	records := []*domain.OHLCV{
		{Symbol: symbol, Date: parseDate("2024-01-02"), Open: 10.0, High: 10.5, Low: 9.8, Close: 10.2, Volume: 1000000},
		{Symbol: symbol, Date: parseDate("2024-01-03"), Open: 10.2, High: 10.8, Low: 10.1, Close: 10.6, Volume: 1200000},
		{Symbol: symbol, Date: parseDate("2024-01-04"), Open: 10.6, High: 11.0, Low: 10.5, Close: 10.9, Volume: 900000},
	}

	err := store.SaveOHLCVBatch(ctx, records)
	require.NoError(t, err)

	// Query back
	bars, err := store.GetOHLCV(ctx, symbol, parseDate("2024-01-01"), parseDate("2024-01-05"))
	require.NoError(t, err)
	assert.Len(t, bars, 3)
	assert.Equal(t, symbol, bars[0].Symbol)
	// Verify data integrity: check any bar has expected values
	var hasClose10_2, hasClose10_6, hasClose10_9 bool
	for _, b := range bars {
		if abs(b.Close-10.2) < 0.01 { hasClose10_2 = true }
		if abs(b.Close-10.6) < 0.01 { hasClose10_6 = true }
		if abs(b.Close-10.9) < 0.01 { hasClose10_9 = true }
	}
	assert.True(t, hasClose10_2, "should have bar with close 10.2")
	assert.True(t, hasClose10_6, "should have bar with close 10.6")
	assert.True(t, hasClose10_9, "should have bar with close 10.9")

	// Cleanup
	store.DB().Exec(ctx, "DELETE FROM ohlcv_daily_qfq WHERE symbol=$1", symbol)
}

func TestSaveStock_and_GetStock(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	symbol := "TEST_STK_001.SH"
	stock := &domain.Stock{
		Symbol:   symbol,
		Name:     "测试股票",
		Exchange: "SSE",
		Industry: "科技",
	}

	err := store.SaveStock(ctx, stock)
	require.NoError(t, err)

	// Query back
	result, err := store.GetStock(ctx, symbol)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, symbol, result.Symbol)
	assert.Equal(t, "测试股票", result.Name)
	assert.Equal(t, "SSE", result.Exchange)

	// Cleanup
	store.DB().Exec(ctx, "DELETE FROM stocks WHERE symbol=$1", symbol)
}

func TestSaveStockBatch(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	stocks := []domain.Stock{
		{Symbol: "TEST_BATCH_001.SH", Name: "批测1", Exchange: "SSE"},
		{Symbol: "TEST_BATCH_002.SH", Name: "批测2", Exchange: "SZSE"},
	}

	err := store.SaveStockBatch(ctx, stocks)
	require.NoError(t, err)

	// Verify both exist
	for _, s := range stocks {
		result, err := store.GetStock(ctx, s.Symbol)
		require.NoError(t, err)
		assert.Equal(t, s.Name, result.Name)
	}

	// Cleanup
	store.DB().Exec(ctx, "DELETE FROM stocks WHERE symbol LIKE 'TEST_BATCH_%'")
}

func TestGetAllStocks(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	stocks, err := store.GetAllStocks(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(stocks), 1)
}

func TestHasOHLCVData(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	// Use a real symbol that should exist
	exists, err := store.HasOHLCVData(ctx, "600000.SH")
	require.NoError(t, err)
	assert.True(t, exists)

	// Non-existent symbol
	exists, err = store.HasOHLCVData(ctx, "NONEXISTENT_999.XYZ")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestGetLatestOHLCVDate(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	// Insert test data to ensure we have a known latest date
	symbol := "TEST_LATEST_DATE.SH"
	records := []*domain.OHLCV{
		{Symbol: symbol, Date: parseDate("2024-06-01"), Open: 10.0, High: 10.5, Low: 9.8, Close: 10.2, Volume: 1000000},
		{Symbol: symbol, Date: parseDate("2024-06-02"), Open: 10.2, High: 10.8, Low: 10.1, Close: 10.6, Volume: 1200000},
		{Symbol: symbol, Date: parseDate("2024-06-03"), Open: 10.6, High: 11.0, Low: 10.5, Close: 10.9, Volume: 900000},
	}

	err := store.SaveOHLCVBatch(ctx, records)
	require.NoError(t, err)

	date, err := store.GetLatestOHLCVDate(ctx, symbol)
	require.NoError(t, err)
	assert.False(t, date.IsZero())
	assert.Equal(t, 2024, date.Year())
	assert.Equal(t, 6, int(date.Month()))
	assert.Equal(t, 3, date.Day())

	// Cleanup
	store.DB().Exec(ctx, "DELETE FROM ohlcv_daily_qfq WHERE symbol=$1", symbol)
}

func TestSaveTradingCalendarEntry_and_GetTradingCalendar(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	entry := &TradingCalendarEntry{
		Exchange:     "TESTEX",
		TradeDate:   parseDate("2024-12-31"),
		IsTradingDay: false, // holiday
	}

	err := store.SaveTradingCalendarEntry(ctx, entry)
	require.NoError(t, err)

	entries, err := store.GetTradingCalendar(ctx, parseDate("2024-12-01"), parseDate("2024-12-31"))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(entries), 1)

	// Cleanup
	store.DB().Exec(ctx, "DELETE FROM trading_calendar WHERE exchange='TESTEX' AND trade_date='2024-12-31'")
}

func TestGetTradingDays(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	days, err := store.GetTradingDays(ctx, parseDate("2024-01-01"), parseDate("2024-01-31"))
	require.NoError(t, err)
	assert.Greater(t, len(days), 0)
	// Should be trading days only (weekends excluded for most)
	assert.True(t, len(days) <= 22) // max ~22 trading days in Jan
}

func TestIsTradingDay(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	// 2024-01-02 was a Tuesday (should be trading day)
	isTrading, err := store.IsTradingDay(ctx, parseDate("2024-01-02"))
	require.NoError(t, err)
	assert.True(t, isTrading)
}

func parseDate(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestSaveFundamentalData_and_GetFundamentalDataLatest(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	pe := 15.0
	fd := &domain.FundamentalData{
		TsCode:    "TEST_FD_001.SH",
		TradeDate: parseDate("2024-03-31"),
		AnnDate:   parseDate("2024-04-15"),
		EndDate:   parseDate("2024-03-31"),
		PE:        &pe,
		PB:        floatPtr(1.2),
		PS:        floatPtr(0.8),
		ROE:       floatPtr(0.12),
	}

	err := store.SaveFundamentalData(ctx, fd)
	require.NoError(t, err)

	result, err := store.GetFundamentalDataLatest(ctx, "TEST_FD_001.SH")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "TEST_FD_001.SH", result.TsCode)
	assert.NotNil(t, result.PE)
	assert.Equal(t, 15.0, *result.PE)

	// Cleanup
	store.DB().Exec(ctx, "DELETE FROM stock_fundamentals WHERE ts_code='TEST_FD_001.SH'")
}

func TestSaveFundamentalDataBatch(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	records := []*domain.FundamentalData{
		{TsCode: "TEST_FD_BATCH.SH", TradeDate: parseDate("2024-03-31"), AnnDate: parseDate("2024-04-15"), EndDate: parseDate("2024-03-31")},
		{TsCode: "TEST_FD_BATCH.SH", TradeDate: parseDate("2024-06-30"), AnnDate: parseDate("2024-07-15"), EndDate: parseDate("2024-06-30")},
	}

	err := store.SaveFundamentalDataBatch(ctx, records)
	require.NoError(t, err)

	history, err := store.GetFundamentalDataHistory(ctx, "TEST_FD_BATCH.SH",
		timePtr(parseDate("2024-01-01")), timePtr(parseDate("2024-12-31")))
	require.NoError(t, err)
	assert.Len(t, history, 2)

	// Cleanup
	store.DB().Exec(ctx, "DELETE FROM stock_fundamentals WHERE ts_code='TEST_FD_BATCH.SH'")
}

func TestGetFundamentalDataHistory(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	// Use a real symbol with data
	history, err := store.GetFundamentalDataHistory(ctx, "600000.SH",
		timePtr(parseDate("2023-01-01")), timePtr(parseDate("2024-12-31")))
	require.NoError(t, err)
	// Real data might have 4+ quarterly records
	assert.GreaterOrEqual(t, len(history), 0) // just check it runs without error
}

func TestScreenFundamentals_Integration(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	// Insert test data first
	pe := 12.0
	roe := 0.15
	fd := &domain.FundamentalData{
		TsCode:    "TEST_SCREEN_001.SH",
		TradeDate: parseDate("2024-03-31"),
		AnnDate:   parseDate("2024-04-15"),
		EndDate:   parseDate("2024-03-31"),
		PE:        &pe,
		ROE:       &roe,
	}
	store.SaveFundamentalData(ctx, fd)

	// Run screen
	peMax := 20.0
	roeMin := 0.10
	filters := domain.ScreenFilters{PE_max: &peMax, ROE_min: &roeMin}
	results, err := store.ScreenFundamentals(ctx, filters, nil, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)

	// Cleanup
	store.DB().Exec(ctx, "DELETE FROM stock_fundamentals WHERE ts_code='TEST_SCREEN_001.SH'")
}

func TestSaveTradingCalendarBatch(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	entries := []*TradingCalendarEntry{
		{Exchange: "TESTEX2", TradeDate: parseDate("2025-01-01"), IsTradingDay: false},
		{Exchange: "TESTEX2", TradeDate: parseDate("2025-01-02"), IsTradingDay: true},
		{Exchange: "TESTEX2", TradeDate: parseDate("2025-01-03"), IsTradingDay: true},
	}

	err := store.SaveTradingCalendarBatch(ctx, entries)
	require.NoError(t, err)

	// Cleanup
	store.DB().Exec(ctx, "DELETE FROM trading_calendar WHERE exchange='TESTEX2'")
}

func TestGetTradingDates(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	dates, err := store.GetTradingDates(ctx, parseDate("2024-01-01"), parseDate("2024-01-15"))
	require.NoError(t, err)
	assert.Greater(t, len(dates), 0)
}

func floatPtr(v float64) *float64 { return &v }

func timePtr(t time.Time) *time.Time { return &t }

func TestSaveIndexConstituentBatch_and_GetIndexConstituents(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	indexCode := "000300.SH"
	records := []*domain.IndexConstituent{
		{IndexCode: indexCode, Symbol: "TEST_IC_001.SH", InDate: parseDate("2020-01-02"), OutDate: time.Time{}},
		{IndexCode: indexCode, Symbol: "TEST_IC_002.SH", InDate: parseDate("2020-01-02"), OutDate: parseDate("2024-12-01")},
		{IndexCode: indexCode, Symbol: "TEST_IC_003.SH", InDate: parseDate("2021-06-15"), OutDate: time.Time{}},
	}

	err := store.SaveIndexConstituentBatch(ctx, records)
	require.NoError(t, err)

	// Query back
	result, err := store.GetIndexConstituents(ctx, indexCode)
	require.NoError(t, err)
	assert.Equal(t, 3, len(result), "should return 3 constituents")

	// Verify symbols are present
	symbols := make(map[string]bool)
	for _, c := range result {
		symbols[c.Symbol] = true
	}
	assert.True(t, symbols["TEST_IC_001.SH"])
	assert.True(t, symbols["TEST_IC_002.SH"])
	assert.True(t, symbols["TEST_IC_003.SH"])

	// Verify dates for one record
	var c2 domain.IndexConstituent
	for _, c := range result {
		if c.Symbol == "TEST_IC_002.SH" {
			c2 = c
			break
		}
	}
	assert.Equal(t, 2020, c2.InDate.Year())
	assert.Equal(t, 1, int(c2.InDate.Month()))
	assert.Equal(t, 2, c2.InDate.Day())
	assert.Equal(t, 2024, c2.OutDate.Year())
	assert.Equal(t, 12, int(c2.OutDate.Month()))
	assert.Equal(t, 1, c2.OutDate.Day())

	// Cleanup
	store.DB().Exec(ctx, "DELETE FROM index_constituents WHERE symbol LIKE 'TEST_IC_%'")
}

func TestSaveIndexConstituentBatch_EmptySlice(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	err := store.SaveIndexConstituentBatch(ctx, []*domain.IndexConstituent{})
	assert.NoError(t, err, "should not error on empty slice")
}

func TestSaveIndexConstituentBatch_Upsert(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	indexCode := "000500.SH"
	record := &domain.IndexConstituent{
		IndexCode: indexCode,
		Symbol:    "TEST_IC_UPSERT.SH",
		InDate:    parseDate("2022-01-01"),
		OutDate:   time.Time{},
	}

	// Insert
	err := store.SaveIndexConstituentBatch(ctx, []*domain.IndexConstituent{record})
	require.NoError(t, err)

	// Update with new in_date
	updated := &domain.IndexConstituent{
		IndexCode: indexCode,
		Symbol:    "TEST_IC_UPSERT.SH",
		InDate:    parseDate("2023-06-01"), // changed
		OutDate:   parseDate("2025-01-01"), // newly exited
	}
	err = store.SaveIndexConstituentBatch(ctx, []*domain.IndexConstituent{updated})
	require.NoError(t, err)

	result, err := store.GetIndexConstituents(ctx, indexCode)
	require.NoError(t, err)

	var found domain.IndexConstituent
	for _, c := range result {
		if c.Symbol == "TEST_IC_UPSERT.SH" {
			found = c
			break
		}
	}
	assert.Equal(t, "TEST_IC_UPSERT.SH", found.Symbol)
	assert.Equal(t, 2023, found.InDate.Year(), "in_date should be updated")
	assert.Equal(t, 2025, found.OutDate.Year(), "out_date should be updated")

	// Cleanup
	store.DB().Exec(ctx, "DELETE FROM index_constituents WHERE symbol='TEST_IC_UPSERT.SH'")
}
