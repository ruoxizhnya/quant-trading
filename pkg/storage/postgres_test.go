package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// skipIfNoSeedData skips tests whose assertion is literally "this table is not
// empty".
//
// 用法只有一个前提：**前置读的东西必须就是断言读的东西**。留在这里的例子是
// TestGetAllStocks —— 它查 stocks 表、断言 len(stocks) >= 1，与前置一致。
//
// AUD-51 的教训：同一族的另外四个测试此前也用它，但前置与断言读的**不是同一个
// 东西**，于是前置给了一个它保证不了的承诺：
//
//   - TestHasOHLCVData：前置查「ohlcv_daily_qfq 非空」，断言却要 600000.SH 有数据；
//   - TestGetTradingDays：前置查「表非空」，断言却要 2024 年 1 月那一个月有数据；
//   - TestIsTradingDay / TestGetTradingDates：前置查 ohlcv_daily_qfq，而这俩函数
//     读的是 **trading_calendar** —— 表都错了（calendar 已灌而行情未灌时它会白跳过；
//     反过来行情有而 calendar 没灌时它会红）。
//
// 一个局部或中途的同步就足以让它们红，而红出来的信息不指向任何代码问题 ——
// 「跑红」这个信号本身因此不可信。那四个已改为**自灌自证**（自己写数据、
// 自己断言、自己清理），不再需要前置。
func skipIfNoSeedData(t *testing.T, store *PostgresStore, table string) {
	t.Helper()
	var n int
	err := store.DB().QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n)
	if err != nil || n == 0 {
		t.Skipf("需要预置数据：%s 表为空（新库？先跑一次数据同步）", table)
	}
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
		if abs(b.Close-10.2) < 0.01 {
			hasClose10_2 = true
		}
		if abs(b.Close-10.6) < 0.01 {
			hasClose10_6 = true
		}
		if abs(b.Close-10.9) < 0.01 {
			hasClose10_9 = true
		}
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
	skipIfNoSeedData(t, store, "stocks")

	stocks, err := store.GetAllStocks(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(stocks), 1)
}

// TestHasOHLCVData 自灌自证。
//
// AUD-51：旧写法前置只检查 `ohlcv_daily_qfq` **非空**，断言却要 `600000.SH`
// 有数据 —— 前置与断言读的不是同一个东西。任何局部/中途同步（表里有若干票、
// 但没有 600000.SH）都会让它红，而红出来的信息不指向任何代码问题。
func TestHasOHLCVData(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	symbol := "TEST_HASDATA_001.SH"
	require.NoError(t, store.SaveOHLCVBatch(ctx, []*domain.OHLCV{
		{Symbol: symbol, Date: parseDate("2024-02-01"), Open: 10.0, High: 10.5, Low: 9.8, Close: 10.2, Volume: 1000000},
	}))
	defer store.DB().Exec(ctx, "DELETE FROM ohlcv_daily_qfq WHERE symbol=$1", symbol)

	// 刚写进去的票必须被查到
	exists, err := store.HasOHLCVData(ctx, symbol)
	require.NoError(t, err)
	assert.True(t, exists, "HasOHLCVData(%s) 必须为 true", symbol)

	// 不存在的票必须为 false
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
		TradeDate:    parseDate("2024-12-31"),
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

// TestGetTradingDays 自灌自证：GetTradingDays 读的是 `ohlcv_daily_qfq` 的
// DISTINCT trade_date，所以这里就灌那个表的那个区间。
//
// AUD-51：旧写法只用「表非空」当前置，却断言 2024 年 1 月**那一个月**有数据 ——
// 同步到 2023 年就会红，而那不是代码问题。
func TestGetTradingDays(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	symbol := "TEST_TRADINGDAYS.SH"
	want := []string{"2024-01-02", "2024-01-03", "2024-01-04"}
	records := make([]*domain.OHLCV, 0, len(want))
	for _, d := range want {
		records = append(records, &domain.OHLCV{
			Symbol: symbol, Date: parseDate(d),
			Open: 10.0, High: 10.5, Low: 9.8, Close: 10.2, Volume: 1000000,
		})
	}
	require.NoError(t, store.SaveOHLCVBatch(ctx, records))
	defer store.DB().Exec(ctx, "DELETE FROM ohlcv_daily_qfq WHERE symbol=$1", symbol)

	days, err := store.GetTradingDays(ctx, parseDate("2024-01-01"), parseDate("2024-01-31"))
	require.NoError(t, err)
	require.NotEmpty(t, days)

	got := make(map[string]bool, len(days))
	for _, d := range days {
		got[d.Format("2006-01-02")] = true
	}
	for _, w := range want {
		assert.True(t, got[w], "刚灌进去的交易日 %s 必须出现在结果里", w)
	}
	// 区间边界：结果不得越界（这是 DISTINCT trade_date + WHERE 的真实不变量）
	for _, d := range days {
		assert.False(t, d.Before(parseDate("2024-01-01")) || d.After(parseDate("2024-01-31")),
			"结果 %s 落在查询区间之外", d.Format("2006-01-02"))
	}
}

// TestIsTradingDay 自灌自证：IsTradingDay 读的是 **trading_calendar**。
//
// AUD-51：旧写法的前置查的却是 `ohlcv_daily_qfq` —— 表都错了。calendar 已灌
// 而行情未灌时它会白跳过（漏检），行情有而 calendar 没灌时它会红（假警报）。
func TestIsTradingDay(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	seedIsolatedCalendar(t, store)
	defer store.DB().Exec(ctx, "DELETE FROM trading_calendar WHERE exchange='TESTEX_IS'")

	isTrading, err := store.IsTradingDay(ctx, parseDate(testCalTradingDay))
	require.NoError(t, err)
	assert.True(t, isTrading, "标为交易日的 %s 必须返回 true", testCalTradingDay)

	isTrading, err = store.IsTradingDay(ctx, parseDate(testCalHoliday))
	require.NoError(t, err)
	assert.False(t, isTrading, "标为非交易日的 %s 必须返回 false", testCalHoliday)
}

// 自灌日历用的日期固定在 1990 年 1 月初：任何现实同步区间（默认 10 年）都不会
// 覆盖到那里，所以自灌既不会与真实数据打架，清理时也不会误删真实数据。
const (
	testCalTradingDay = "1990-01-02"
	testCalHoliday    = "1990-01-03"
)

// seedIsolatedCalendar 灌进两个交易日历条目（一真一假）：一个标为交易日、
// 一个标为非交易日，好让调用方把 IsTradingDay / GetTradingDates 的两个分支
// 都真的走一遍。trading_calendar 的主键是 trade_date（一格日期一行，不带
// exchange），所以这两个日期在库里各只有一行。
//
// 清理由**调用方**用 defer 做（而不是这里 t.Cleanup）—— t.Cleanup 跑在测试
// 函数的所有 defer 之后，那时 `defer store.Close()` 已经把连接池关了，
// 清理会静默失败、把测试数据留在库里。
func seedIsolatedCalendar(t *testing.T, store *PostgresStore) {
	t.Helper()
	require.NoError(t, store.SaveTradingCalendarBatch(context.Background(), []*TradingCalendarEntry{
		{Exchange: "TESTEX_IS", TradeDate: parseDate(testCalTradingDay), IsTradingDay: true},
		{Exchange: "TESTEX_IS", TradeDate: parseDate(testCalHoliday), IsTradingDay: false},
	}))
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

// TestGetTradingDates 自灌自证：GetTradingDates 读的是 **trading_calendar**
// 且带 `is_trading_day = TRUE` 过滤（AUD-51 之前这里前置查的是 ohlcv_daily_qfq）。
func TestGetTradingDates(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	seedIsolatedCalendar(t, store)
	defer store.DB().Exec(ctx, "DELETE FROM trading_calendar WHERE exchange='TESTEX_IS'")

	dates, err := store.GetTradingDates(ctx, parseDate("1990-01-01"), parseDate("1990-01-15"))
	require.NoError(t, err)
	require.Len(t, dates, 1, "区间内只灌了一个交易日，非交易日必须被 is_trading_day=TRUE 过滤掉")
	assert.Equal(t, testCalTradingDay, dates[0].Format("2006-01-02"))
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

// ──────────────────────────────────────────────────────────────────────
// fundamentals_detail DDL parity — 契约 C1 (TASKS.md EQD-P1-1)
// ──────────────────────────────────────────────────────────────────────

// The frozen contract (contracts/fundamentals_detail.schema.sql) declares three
// copies of this DDL that must agree:
//
//	contracts/fundamentals_detail.schema.sql       — 契约副本（分歧时以此为准）
//	docs/migrations/022_equitydeep_fundamentals.sql — 文档副本
//	pkg/storage/postgres.go inline migrate()       — 实际执行路径
//
// Drift here is silent: only the inline copy runs, so a stale contract or docs
// copy would describe a table that does not exist. ODR-052 mitigated this with
// a header comment and explicitly recorded the missing automated check; the
// tests below are that check. Paths are relative to this package directory.

// fundamentalsDetailCopy is one of the three same-source DDL copies.
type fundamentalsDetailCopy struct {
	label string
	path  string
}

var fundamentalsDetailCopies = []fundamentalsDetailCopy{
	{"契约副本", filepath.Join("..", "..", "contracts", "fundamentals_detail.schema.sql")},
	{"文档副本", filepath.Join("..", "..", "docs", "migrations", "022_equitydeep_fundamentals.sql")},
	{"执行路径", filepath.Join("..", "..", "pkg", "storage", "postgres.go")},
}

// TestFundamentalsDetailSchema_ThreeCopiesAgree fails when any of the three
// copies diverges in column set, types, nullability, primary key or index.
func TestFundamentalsDetailSchema_ThreeCopiesAgree(t *testing.T) {
	var wantTable, wantIndex, wantLabel string
	for _, c := range fundamentalsDetailCopies {
		src := readRepoFile(t, c.path)
		table := normalizeDDL(extractFundamentalsDetailTable(t, c.path, src))
		index := normalizeDDL(extractFundamentalsDetailIndex(t, c.path, src))
		require.NotEmpty(t, table, "%s: extracted table DDL is empty", c.label)
		require.NotEmpty(t, index, "%s: extracted index DDL is empty", c.label)

		if wantTable == "" {
			wantTable, wantIndex, wantLabel = table, index, c.label
			continue
		}
		assert.Equal(t, wantTable, table,
			"%s 与 %s 的 fundamentals_detail 建表 DDL 已漂移；以契约副本为准并同时修正三处", c.label, wantLabel)
		assert.Equal(t, wantIndex, index,
			"%s 与 %s 的 idx_fund_detail_lookup 定义已漂移；以契约副本为准并同时修正三处", c.label, wantLabel)
	}
}

// TestFundamentalsDetailSchema_FrozenSemantics pins the properties the contract
// calls non-negotiable, so an edit applied consistently to all three copies but
// semantically wrong still fails:
//   - ann_date NOT NULL      → PIT alignment; without it factor computation
//     would read results before their announcement (look-ahead bias)
//   - fetched_at in the PK   → restatements coexist instead of overwriting
//   - snapshot_uri NOT NULL  → every number is traceable to its snapshot
func TestFundamentalsDetailSchema_FrozenSemantics(t *testing.T) {
	contract := fundamentalsDetailCopies[0]
	ddl := normalizeDDL(extractFundamentalsDetailTable(t, contract.path, readRepoFile(t, contract.path)))

	assert.Contains(t, ddl, "ann_date DATE NOT NULL")
	assert.Contains(t, ddl, "fetched_at TIMESTAMPTZ NOT NULL")
	assert.Contains(t, ddl, "snapshot_uri TEXT NOT NULL")
	assert.Contains(t, ddl, "PRIMARY KEY (ts_code, end_date, ann_date, field_code, fetched_at)")
	// value is the only nullable column: a missing reading must stay
	// distinguishable from a reading of zero.
	assert.Contains(t, ddl, "value NUMERIC(24,4),")

	// The look-up index is part of the contract as well: factor computation
	// reads the latest ann_date-aware reading per (ts_code, field_code).
	index := normalizeDDL(extractFundamentalsDetailIndex(t, contract.path, readRepoFile(t, contract.path)))
	assert.Contains(t, index, "ON fundamentals_detail (ts_code, field_code, end_date DESC)")
}

// TestFundamentalsDetailSchema_AppliedByMigrate checks the inline migration
// really lands the table in a live database. testStore() runs migrate() and
// skips when no Postgres is reachable (the default on a machine without
// Docker), matching the rest of this package's convention.
func TestFundamentalsDetailSchema_AppliedByMigrate(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	var nullable string
	err := store.DB().QueryRow(context.Background(),
		`SELECT is_nullable FROM information_schema.columns
		 WHERE table_name = 'fundamentals_detail' AND column_name = 'ann_date'`).Scan(&nullable)
	require.NoError(t, err, "fundamentals_detail.ann_date must exist after migrate()")
	assert.Equal(t, "NO", nullable, "ann_date must be NOT NULL for PIT alignment")
}

// readRepoFile reads a path relative to the package directory (the working
// directory Go uses for tests).
func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "cannot read %s", path)
	return string(b)
}

// extractFundamentalsDetailTable returns the table's column list, from the
// opening paren through the closing paren of its primary key.
func extractFundamentalsDetailTable(t *testing.T, path, src string) string {
	t.Helper()
	return extractParenGroup(t, path, src,
		"CREATE TABLE IF NOT EXISTS fundamentals_detail (", "PRIMARY KEY")
}

// extractFundamentalsDetailIndex returns the look-up index statement.
func extractFundamentalsDetailIndex(t *testing.T, path, src string) string {
	t.Helper()
	return extractParenGroup(t, path, src,
		"CREATE INDEX IF NOT EXISTS idx_fund_detail_lookup", "end_date DESC")
}

// extractParenGroup returns the text after startMarker up to and including the
// closing paren of the group that anchor sits in. anchor must come after any
// nested parens of that group (e.g. NUMERIC(24,4), or the PRIMARY KEY clause
// that CONTRACT keeps last), so it identifies the terminating paren unambiguously
// whether the source is a .sql file or a Go raw string literal.
func extractParenGroup(t *testing.T, path, src, startMarker, anchor string) string {
	t.Helper()
	start := strings.Index(src, startMarker)
	require.GreaterOrEqual(t, start, 0, "%s: %q not found", path, startMarker)
	rest := src[start+len(startMarker):]

	at := strings.Index(rest, anchor)
	require.GreaterOrEqual(t, at, 0, "%s: %q not found", path, anchor)
	close := strings.Index(rest[at:], ")")
	require.GreaterOrEqual(t, close, 0, "%s: %q group unterminated", path, anchor)

	return rest[:at+close+1]
}

// normalizeDDL strips SQL line comments and collapses whitespace so the three
// copies compare by content rather than by their differing alignment.
func normalizeDDL(ddl string) string {
	var b strings.Builder
	for _, line := range strings.Split(ddl, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString(" ")
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
