package compliance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================
// P2-6: 大额交易报告 — 单元测试
//
// 覆盖:
//   - DefaultLargeTradeConfig / NewLargeTraderReporter 默认值
//   - BuildReport 阈值边界 (单笔 / 累计 / 都不达)
//   - Whitelist 排除白名单账户
//   - 跨日过滤 (out-of-day trades 忽略)
//   - Cumulative-only 账户生成 aggregate 条目
//   - WriteReport 落盘格式 (路径、0600 权限、JSON 结构)
//   - SetAccountWhitelist 防御性拷贝
// ============================================================

func mkTradeAt(id, acct, sym, dir string, qty, price float64, t time.Time) TradeRecord {
	return TradeRecord{
		TradeID: id, OrderID: acct + ":ord-1", Symbol: sym, Direction: dir,
		Quantity: qty, Price: price, Fee: 0, TradeTime: t,
	}
}

func TestDefaultLargeTradeConfig_Defaults(t *testing.T) {
	c := DefaultLargeTradeConfig()
	if c.SingleThresholdCNY != 2_000_000 {
		t.Fatalf("expected 2M, got %.0f", c.SingleThresholdCNY)
	}
	if c.CumulativeThresholdCNY != 5_000_000 {
		t.Fatalf("expected 5M, got %.0f", c.CumulativeThresholdCNY)
	}
}

func TestNewLargeTraderReporter_AppliesDefaults(t *testing.T) {
	r := NewLargeTraderReporter(LargeTradeConfig{})
	if r.config.SingleThresholdCNY != 2_000_000 {
		t.Fatalf("expected 2M, got %.0f", r.config.SingleThresholdCNY)
	}
	if r.config.CumulativeThresholdCNY != 5_000_000 {
		t.Fatalf("expected 5M, got %.0f", r.config.CumulativeThresholdCNY)
	}
}

func TestBuildReport_NoTrades(t *testing.T) {
	r := NewLargeTraderReporter(DefaultLargeTradeConfig())
	day := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	report := r.BuildReport(nil, day)
	if report.TotalTrades != 0 {
		t.Fatalf("expected 0 trades, got %d", report.TotalTrades)
	}
	if len(report.LargeTrades) != 0 {
		t.Fatalf("expected 0 large trades, got %d", len(report.LargeTrades))
	}
	if report.TradingDate != "2026-06-15" {
		t.Fatalf("expected 2026-06-15, got %s", report.TradingDate)
	}
}

func TestBuildReport_FiresSingleThreshold(t *testing.T) {
	r := NewLargeTraderReporter(DefaultLargeTradeConfig())
	day := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	trades := []TradeRecord{
		mkTradeAt("t1", "acct-A", "000001.SZ", "buy", 100_000, 20.0, day.Add(1*time.Hour)),
		// amount = 2_000_000 → exactly at single threshold
	}
	report := r.BuildReport(trades, day)
	if len(report.LargeTrades) != 1 {
		t.Fatalf("expected 1 large trade, got %d", len(report.LargeTrades))
	}
	if report.LargeTrades[0].Flag != "single" {
		t.Fatalf("expected flag=single, got %s", report.LargeTrades[0].Flag)
	}
	if report.LargeTrades[0].AmountCNY != 2_000_000 {
		t.Fatalf("expected 2M, got %.2f", report.LargeTrades[0].AmountCNY)
	}
}

func TestBuildReport_BelowThresholdNoFire(t *testing.T) {
	r := NewLargeTraderReporter(DefaultLargeTradeConfig())
	day := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	trades := []TradeRecord{
		mkTradeAt("t1", "acct-A", "000001.SZ", "buy", 50_000, 10.0, day.Add(1*time.Hour)),
		// amount = 500_000 → below single threshold
	}
	report := r.BuildReport(trades, day)
	if len(report.LargeTrades) != 0 {
		t.Fatalf("expected 0 large trades, got %d", len(report.LargeTrades))
	}
}

func TestBuildReport_FiresCumulativeOnly(t *testing.T) {
	r := NewLargeTraderReporter(DefaultLargeTradeConfig())
	day := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	// 5 trades × 100_000 shares × ¥12 = 6M total, each trade = 1.2M < 2M single
	// Cumulative 6M > 5M → should fire as cumulative-only
	var trades []TradeRecord
	for i := 0; i < 5; i++ {
		trades = append(trades, mkTradeAt(
			"t"+string(rune('1'+i)),
			"acct-A", "000001.SZ", "buy",
			100_000, 12.0, day.Add(time.Duration(i+1)*time.Hour),
		))
	}
	report := r.BuildReport(trades, day)
	if len(report.LargeTrades) != 1 {
		t.Fatalf("expected 1 large trade (cumulative aggregate), got %d", len(report.LargeTrades))
	}
	if report.LargeTrades[0].Flag != "cumulative" {
		t.Fatalf("expected flag=cumulative, got %s", report.LargeTrades[0].Flag)
	}
	if report.LargeTrades[0].AccountID != "acct-A" {
		t.Fatalf("expected acct-A, got %s", report.LargeTrades[0].AccountID)
	}
	if len(report.CumulativeByAccount) != 1 {
		t.Fatalf("expected 1 cumulative account, got %d", len(report.CumulativeByAccount))
	}
}

func TestBuildReport_FiresBothSingleAndCumulative(t *testing.T) {
	r := NewLargeTraderReporter(DefaultLargeTradeConfig())
	day := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	trades := []TradeRecord{
		mkTradeAt("t1", "acct-A", "000001.SZ", "buy", 100_000, 20.0, day.Add(1*time.Hour)),
		mkTradeAt("t2", "acct-A", "000001.SZ", "buy", 100_000, 20.0, day.Add(2*time.Hour)),
		mkTradeAt("t3", "acct-A", "000001.SZ", "buy", 100_000, 20.0, day.Add(3*time.Hour)),
		// 3 × 2M = 6M cumulative; each 2M hits single threshold
	}
	report := r.BuildReport(trades, day)
	if len(report.LargeTrades) != 3 {
		t.Fatalf("expected 3 large trades, got %d", len(report.LargeTrades))
	}
	for _, e := range report.LargeTrades {
		if e.Flag != "both" {
			t.Fatalf("expected flag=both, got %s", e.Flag)
		}
	}
}

func TestBuildReport_FiltersOutOfDayTrades(t *testing.T) {
	r := NewLargeTraderReporter(DefaultLargeTradeConfig())
	day := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	trades := []TradeRecord{
		// 6/14 23:00 — outside the 6/15 trading day
		mkTradeAt("t1", "acct-A", "000001.SZ", "buy", 100_000, 30.0,
			time.Date(2026, 6, 14, 23, 0, 0, 0, time.UTC)),
		// 6/16 01:00 — outside the 6/15 trading day
		mkTradeAt("t2", "acct-A", "000001.SZ", "buy", 100_000, 30.0,
			time.Date(2026, 6, 16, 1, 0, 0, 0, time.UTC)),
	}
	report := r.BuildReport(trades, day)
	if len(report.LargeTrades) != 0 {
		t.Fatalf("expected 0 large trades (out-of-day filtered), got %d", len(report.LargeTrades))
	}
}

func TestBuildReport_WhitelistExcludes(t *testing.T) {
	cfg := DefaultLargeTradeConfig()
	cfg.AccountWhitelist = map[string]bool{"institutional-A": true}
	r := NewLargeTraderReporter(cfg)
	day := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	trades := []TradeRecord{
		mkTradeAt("t1", "institutional-A", "000001.SZ", "buy", 100_000, 30.0, day.Add(1*time.Hour)),
		mkTradeAt("t2", "acct-B", "000001.SZ", "buy", 100_000, 30.0, day.Add(1*time.Hour)),
	}
	report := r.BuildReport(trades, day)
	// institutional-A excluded; acct-B fires
	if len(report.LargeTrades) != 1 {
		t.Fatalf("expected 1 large trade (whitelisted excluded), got %d", len(report.LargeTrades))
	}
	if report.LargeTrades[0].AccountID != "acct-B" {
		t.Fatalf("expected acct-B, got %s", report.LargeTrades[0].AccountID)
	}
	if len(report.ExcludedAccounts) != 1 || report.ExcludedAccounts[0] != "institutional-A" {
		t.Fatalf("expected institutional-A in excluded list, got %v", report.ExcludedAccounts)
	}
}

func TestSetAccountWhitelist_DefensiveCopy(t *testing.T) {
	r := NewLargeTraderReporter(DefaultLargeTradeConfig())
	wl := map[string]bool{"acct-X": true}
	r.SetAccountWhitelist(wl)
	// Mutate the original; reporter should retain its own copy.
	wl["acct-Y"] = true
	cfg := r.Config()
	if cfg.AccountWhitelist["acct-Y"] {
		t.Fatal("expected defensive copy; external mutation leaked")
	}
}

func TestSetAccountWhitelist_NilClears(t *testing.T) {
	r := NewLargeTraderReporter(DefaultLargeTradeConfig())
	r.SetAccountWhitelist(map[string]bool{"acct-X": true})
	r.SetAccountWhitelist(nil)
	cfg := r.Config()
	if _, ok := cfg.AccountWhitelist["acct-X"]; ok {
		t.Fatal("expected nil to clear whitelist")
	}
}

func TestWriteReport_CreatesFile(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultLargeTradeConfig()
	cfg.OutputPath = tmp
	r := NewLargeTraderReporter(cfg)
	day := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	trades := []TradeRecord{
		mkTradeAt("t1", "acct-A", "000001.SZ", "buy", 100_000, 30.0, day.Add(1*time.Hour)),
	}
	report := r.BuildReport(trades, day)
	path, err := r.WriteReport(report)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	expected := filepath.Join(tmp, "large-trades-2026-06-15.json")
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}
	// File exists & perms are 0600
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("expected non-empty file")
	}
	// JSON parseable
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got LargeTradeReport
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.SchemaVersion != "large-trades/v1" {
		t.Fatalf("expected schema_version=large-trades/v1, got %s", got.SchemaVersion)
	}
	if got.TradingDate != "2026-06-15" {
		t.Fatalf("expected 2026-06-15, got %s", got.TradingDate)
	}
	if len(got.LargeTrades) != 1 {
		t.Fatalf("expected 1 large trade, got %d", len(got.LargeTrades))
	}
}

func TestWriteReport_NilReportErrors(t *testing.T) {
	r := NewLargeTraderReporter(DefaultLargeTradeConfig())
	_, err := r.WriteReport(nil)
	if err == nil {
		t.Fatal("expected error on nil report")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestWriteReport_CreatesMissingDir(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultLargeTradeConfig()
	cfg.OutputPath = filepath.Join(tmp, "nested", "dir", "that", "doesnt", "exist")
	r := NewLargeTraderReporter(cfg)
	day := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	report := r.BuildReport(nil, day)
	_, err := r.WriteReport(report)
	if err != nil {
		t.Fatalf("expected mkdir to succeed, got %v", err)
	}
}

func TestAccountFromTrade_AcctPrefix(t *testing.T) {
	tr := TradeRecord{OrderID: "acct-XYZ:ord-123"}
	if got := accountFromTrade(tr); got != "acct-XYZ" {
		t.Fatalf("expected acct-XYZ, got %s", got)
	}
}

func TestAccountFromTrade_NoPrefix(t *testing.T) {
	tr := TradeRecord{OrderID: "ord-123"}
	got := accountFromTrade(tr)
	if !strings.HasPrefix(got, "default-acct-") {
		t.Fatalf("expected default-acct- prefix, got %s", got)
	}
}

func TestBuildReport_TotalAmountSum(t *testing.T) {
	r := NewLargeTraderReporter(DefaultLargeTradeConfig())
	day := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	trades := []TradeRecord{
		mkTradeAt("t1", "acct-A", "000001.SZ", "buy", 100, 100.0, day.Add(1*time.Hour)),
		mkTradeAt("t2", "acct-A", "000001.SZ", "buy", 200, 50.0, day.Add(2*time.Hour)),
	}
	report := r.BuildReport(trades, day)
	want := 100*100.0 + 200*50.0 // 10_000 + 10_000 = 20_000
	if report.TotalAmountCNY != want {
		t.Fatalf("expected total %.2f, got %.2f", want, report.TotalAmountCNY)
	}
	if report.TotalTrades != 2 {
		t.Fatalf("expected 2 trades, got %d", report.TotalTrades)
	}
}
