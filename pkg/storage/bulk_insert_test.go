package storage

// CR-18 (ODR-012): bulk_insert.go was 13KB of critical data-persistence code
// with 0 unit tests. The pure functions (buildUpsertSQL, buildInsertArgs,
// toFloat64, normalizeDate, joinCols) and the table mapper (NewTableMapper,
// TableFor, SupportedDataTypes) are exercised here. The transactional
// BulkInsert method itself requires a live Postgres connection — covered by
// the docker-compose integration suite documented in docs/TEST.md.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewTableMapper_CoversAllSupportedDataTypes(t *testing.T) {
	m := NewTableMapper()

	cases := []struct {
		dataType string
		wantTbl  string
		wantPK   []string
	}{
		{"ohlcv_daily", "ohlcv_daily_qfq", []string{"symbol", "trade_date"}},
		{"ohlcv_minute", "ohlcv_minute", []string{"symbol", "ts"}},
		{"realtime_quote", "realtime_quote", []string{"symbol", "ts"}},
		{"capital_flow", "capital_flow", []string{"symbol", "trade_date", "period"}},
		{"sectors", "sectors", []string{"sector_code"}},
		{"stock_sector", "stock_sector_map", []string{"symbol", "sector_code"}},
		{"top_list", "top_list", []string{"trade_date", "symbol"}},
		{"limit_up_pool", "limit_up_pool", []string{"trade_date", "symbol"}},
		{"announcements", "announcements", []string{"ann_id"}},
		{"news", "news", []string{"news_id"}},
		{"hot_search", "hot_search", []string{"rank", "snapshot_time"}},
		{"global_ohlcv", "global_ohlcv", []string{"symbol", "trade_date"}},
		{"fundamentals", "stock_fundamentals", []string{"ts_code", "trade_date"}},
	}
	for _, c := range cases {
		tbl, ok := m.TableFor(c.dataType)
		if !ok {
			t.Errorf("TableFor(%q) returned !ok", c.dataType)
			continue
		}
		if tbl != c.wantTbl {
			t.Errorf("TableFor(%q) = %q, want %q", c.dataType, tbl, c.wantTbl)
		}
		if got := m.pk[c.dataType]; !equalSlice(got, c.wantPK) {
			t.Errorf("pk[%q] = %v, want %v", c.dataType, got, c.wantPK)
		}
		if got := m.columns[c.dataType]; len(got) == 0 {
			t.Errorf("columns[%q] is empty", c.dataType)
		}
	}
}

func TestNewTableMapper_TableFor_Unknown(t *testing.T) {
	m := NewTableMapper()
	if _, ok := m.TableFor("does_not_exist"); ok {
		t.Error("TableFor on unknown data type should return !ok")
	}
}

func TestNewTableMapper_SupportedDataTypes_IncludesAll(t *testing.T) {
	m := NewTableMapper()
	got := m.SupportedDataTypes()
	if len(got) != len(m.tables) {
		t.Errorf("SupportedDataTypes returned %d entries, want %d", len(got), len(m.tables))
	}
	// Must include the data types we depend on
	want := map[string]bool{
		"ohlcv_daily": true, "capital_flow": true, "fundamentals": true,
		"realtime_quote": true, "top_list": true,
	}
	for _, dt := range got {
		if want[dt] {
			delete(want, dt)
		}
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for k := range want {
			missing = append(missing, k)
		}
		t.Errorf("SupportedDataTypes missing: %v", missing)
	}
}

func TestBuildUpsertSQL_StructureAndPKExclusion(t *testing.T) {
	got := buildUpsertSQL("ohlcv_daily_qfq",
		[]string{"symbol", "trade_date", "open", "close", "source", "ingest_time"},
		[]string{"symbol", "trade_date"},
	)
	// Must INSERT all columns
	if !strings.Contains(got, `INSERT INTO ohlcv_daily_qfq (symbol, trade_date, open, close, source, ingest_time)`) {
		t.Errorf("INSERT clause missing/incorrect: %s", got)
	}
	// Must use $1..$6 placeholders
	if !strings.Contains(got, "VALUES ($1, $2, $3, $4, $5, $6)") {
		t.Errorf("VALUES clause missing/incorrect: %s", got)
	}
	// Must ON CONFLICT on the PK columns
	if !strings.Contains(got, "ON CONFLICT (symbol, trade_date)") {
		t.Errorf("ON CONFLICT clause missing/incorrect: %s", got)
	}
	// Must UPDATE only non-PK columns (open, close, source, ingest_time — no symbol, no trade_date)
	updatePart := got[strings.Index(got, "DO UPDATE SET")+len("DO UPDATE SET"):]
	updates := strings.Split(updatePart, ",")
	if len(updates) != 4 {
		t.Errorf("expected 4 UPDATE columns (open, close, source, ingest_time), got %d in %q",
			len(updates), updatePart)
	}
	for _, col := range []string{"symbol", "trade_date"} {
		if strings.Contains(updatePart, col+" = EXCLUDED.") {
			t.Errorf("PK column %q should NOT appear in UPDATE SET: %s", col, got)
		}
	}
	// EXCLUDED references must point to non-PK columns only
	for _, want := range []string{"open", "close", "source", "ingest_time"} {
		if !strings.Contains(updatePart, want+" = EXCLUDED."+want) {
			t.Errorf("expected UPDATE %s = EXCLUDED.%s in: %s", want, want, got)
		}
	}
}

func TestBuildInsertArgs_StandardColumns(t *testing.T) {
	cols := []string{"symbol", "trade_date", "open", "close", "source", "ingest_time"}
	pk := []string{"symbol", "trade_date"}
	numeric := []string{"open", "close"}
	tt := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	p := UnifiedDataPoint{
		Symbol:     "000001.SZ",
		TradeTime:  tt,
		Source:     "tushare",
		Data:       map[string]interface{}{"open": 10.5, "close": int64(11)},
		IngestTime: tt,
	}
	args, err := buildInsertArgs(p, cols, pk, numeric)
	if err != nil {
		t.Fatalf("buildInsertArgs: %v", err)
	}
	if len(args) != len(cols) {
		t.Fatalf("expected %d args, got %d", len(cols), len(args))
	}
	if args[0] != "000001.SZ" {
		t.Errorf("args[0] (symbol) = %v, want 000001.SZ", args[0])
	}
	if args[1] != tt {
		t.Errorf("args[1] (trade_date) = %v, want %v", args[1], tt)
	}
	// Numeric columns must be float64
	if v, ok := args[2].(float64); !ok || v != 10.5 {
		t.Errorf("args[2] (open) = %v (%T), want float64 10.5", args[2], args[2])
	}
	if v, ok := args[3].(float64); !ok || v != 11 {
		t.Errorf("args[3] (close) = %v (%T), want float64 11", args[3], args[3])
	}
	if args[4] != "tushare" {
		t.Errorf("args[4] (source) = %v, want tushare", args[4])
	}
	if args[5] != tt {
		t.Errorf("args[5] (ingest_time) = %v, want %v", args[5], tt)
	}
}

func TestBuildInsertArgs_FundamentalsTsCodePrecedence(t *testing.T) {
	// When Data has ts_code, that should take precedence over Symbol
	cols := []string{"ts_code", "trade_date", "pe", "source", "ingest_time"}
	pk := []string{"ts_code", "trade_date"}
	numeric := []string{"pe"}
	p := UnifiedDataPoint{
		Symbol: "FALLBACK",
		Data:   map[string]interface{}{"ts_code": "000001.SZ", "pe": 12.5},
	}
	args, err := buildInsertArgs(p, cols, pk, numeric)
	if err != nil {
		t.Fatalf("buildInsertArgs: %v", err)
	}
	if args[0] != "000001.SZ" {
		t.Errorf("ts_code arg = %v, want 000001.SZ (from Data, not Symbol fallback)", args[0])
	}
}

func TestBuildInsertArgs_MissingColumnBecomesNull(t *testing.T) {
	cols := []string{"symbol", "trade_date", "open", "source", "ingest_time"}
	pk := []string{"symbol", "trade_date"}
	numeric := []string{"open"}
	p := UnifiedDataPoint{
		Symbol: "000001.SZ",
		Data:   map[string]interface{}{}, // 'open' missing
	}
	args, err := buildInsertArgs(p, cols, pk, numeric)
	if err != nil {
		t.Fatalf("buildInsertArgs: %v", err)
	}
	if args[2] != nil {
		t.Errorf("missing 'open' should be nil, got %v", args[2])
	}
}

func TestBuildInsertArgs_BadNumericType(t *testing.T) {
	cols := []string{"open"}
	pk := []string{"open"}
	numeric := []string{"open"}
	p := UnifiedDataPoint{
		Data: map[string]interface{}{"open": "not-a-number"},
	}
	if _, err := buildInsertArgs(p, cols, pk, numeric); err == nil {
		t.Error("expected error when coercing string to numeric, got nil")
	}
}

func TestToFloat64_AllSupportedTypes(t *testing.T) {
	cases := []struct {
		in   interface{}
		want float64
	}{
		{float64(1.5), 1.5},
		{float32(2.5), 2.5},
		{int(3), 3},
		{int32(4), 4},
		{int64(5), 5},
		{uint(6), 6},
		{uint32(7), 7},
		{uint64(8), 8},
		{json.Number("3.14"), 3.14},
	}
	for _, c := range cases {
		got, err := toFloat64(c.in)
		if err != nil {
			t.Errorf("toFloat64(%v %T): %v", c.in, c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("toFloat64(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestToFloat64_UnsupportedTypes(t *testing.T) {
	bad := []interface{}{
		"123",                  // string with valid number is still rejected
		[]int{1, 2},            // slice
		map[string]int{"k": 1}, // map
		nil,                    // nil
	}
	for _, v := range bad {
		if _, err := toFloat64(v); err == nil {
			t.Errorf("toFloat64(%v) should return error, got nil", v)
		}
	}
}

func TestNormalizeDate_ZeroBecomesNil(t *testing.T) {
	if got := normalizeDate(time.Time{}); got != nil {
		t.Errorf("normalizeDate(zero) = %v, want nil", got)
	}
	tt := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	if got := normalizeDate(tt); got != tt {
		t.Errorf("normalizeDate(tt) = %v, want %v", got, tt)
	}
}

func TestJoinCols_EmptyAndSingle(t *testing.T) {
	if got := joinCols(nil); got != "" {
		t.Errorf("joinCols(nil) = %q, want \"\"", got)
	}
	if got := joinCols([]string{"a"}); got != "a" {
		t.Errorf("joinCols([a]) = %q, want \"a\"", got)
	}
	if got := joinCols([]string{"a", "b", "c"}); got != "a, b, c" {
		t.Errorf("joinCols([a,b,c]) = %q, want \"a, b, c\"", got)
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
