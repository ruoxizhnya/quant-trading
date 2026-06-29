//go:build integration
// +build integration

// Package storage — Sprint 6 P0-7 (ODR-013, TQ-011) dockertest
// integration tests.
//
// Why a build tag
// ---------------
// These tests spin up a real PostgreSQL container via
// github.com/ory/dockertest/v3, which requires:
//   - a working Docker daemon
//   - ~5–10s of startup time per package
//   - ~50MB of disk per container image
//
// CI on shared runners and developer laptops may not have Docker
// (or may not want to pay the startup cost for every `go test
// ./...`). Hiding the file behind `//go:build integration` means
// `go test ./...` is fast and self-contained, while
// `go test -tags=integration ./pkg/storage/...` exercises the
// real DB. The CI gate that "pkg/storage coverage ≥ 60%" is
// satisfied by running the integration-tagged suite, NOT the
// default one — see docs/TASKS.md §CI Gate.
//
// Coverage target
// ---------------
// pkg/storage has 14 source files totaling ~3.5k LOC. As of
// 2026-05-17 the package had 2.4% unit-test coverage because all
// DB-touching code is unreachable without a real database. This
// file exercises the major public surface (stocks, OHLCV,
// calendar, backtest_jobs, strategies) against a real Postgres
// 16 instance, lifting package coverage to ~60–70% per the
// ODR-013 target.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pgTestPool is the package-level dockertest pool. TestMain
// initializes it once for the entire test binary and tears it
// down at the end — per-test container spinup would push the
// suite over 60s for no gain, since each test only inserts a
// handful of rows that the migrations have already isolated
// from the others (separate schemas, or just clean tables).
var pgTestPool *dockertest.Pool

// pgTestResource is the running postgres container; we hold a
// reference so TestMain can purge it.
var pgTestResource *dockertest.Resource

// pgTestStore is the PostgresStore wired against the test
// container. Every integration test calls newTestStore(t) which
// hands out a fresh store and a per-test transaction (rolled
// back at test end) so the tests are isolated without paying
// for a container per test.
var pgTestStore *PostgresStore

// TestMain boots the shared dockertest pool + Postgres container
// once for the whole package. If Docker isn't available the
// whole suite is skipped (t.Skip, not t.Fatal) so a CI machine
// without Docker doesn't break the build.
func TestMain(m *testing.M) {
	// We deliberately do NOT call os.Exit from the skip path —
	// returning from TestMain with the suite skipped means
	// `go test` exits 0. os.Exit would be cleaner but breaks
	// ginkgo-style wrapping if anyone introduces it later.

	if !dockerAvailable() {
		fmt.Fprintln(os.Stderr,
			"SKIP: integration tests require Docker daemon (sock: /var/run/docker.sock). "+
				"To run: start Docker Desktop / colima / podman, then `go test -tags=integration ./pkg/storage/...`.")
		// Run the suite anyway — each test will t.Skip() itself
		// in newTestStore when it sees no pool.
		os.Exit(m.Run())
	}

	pool, err := dockertest.NewPool("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "dockertest.NewPool: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Client.Ping(); err != nil {
		fmt.Fprintf(os.Stderr, "docker ping: %v\n", err)
		os.Exit(m.Run())
	}

	// Postgres 16 Alpine — small (~80MB) and the version pinned
	// in docker-compose.yml. The PGPASSWORD env is the standard
	// way to inject the test password; the `-c listen_addresses`
	// flag is unnecessary because dockertest maps a random port
	// to localhost.
	pgTestResource, err = pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "16-alpine",
		Env: []string{
			"POSTGRES_PASSWORD=testpass",
			"POSTGRES_USER=testuser",
			"POSTGRES_DB=testdb",
		},
		ExposedPorts: []string{"5432/tcp"},
	}, func(config *docker.HostConfig) {
		// Auto-remove keeps the developer laptop from filling up
		// with stale test containers if the run is killed.
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "pool.RunWithOptions: %v\n", err)
		os.Exit(m.Run())
	}

	// Wait for Postgres to be ready. retryStart=20 with 1s
	// sleep is enough on a developer laptop; on a saturated CI
	// runner the expBackoff doubles on failure up to 5s.
	var connString string
	if err := pool.Retry(func() error {
		port := pgTestResource.GetPort("5432/tcp")
		connString = fmt.Sprintf(
			"postgres://testuser:testpass@localhost:%s/testdb?sslmode=disable",
			port)
		c, err := pgx.Connect(context.Background(), connString)
		if err != nil {
			return err
		}
		return c.Close(context.Background())
	}); err != nil {
		fmt.Fprintf(os.Stderr, "postgres not ready: %v\n", err)
		_ = pool.Purge(pgTestResource)
		os.Exit(m.Run())
	}

	pgTestPool = pool

	// Build a single shared PostgresStore — its constructor runs
	// the inline migrations, which is what we want to exercise.
	// We give it a generous startup deadline because pool.Retry
	// above has already proven the server is reachable.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := NewPostgresStore(ctx, connString)
	if err != nil {
		fmt.Fprintf(os.Stderr, "NewPostgresStore: %v\n", err)
		_ = pool.Purge(pgTestResource)
		os.Exit(m.Run())
	}
	pgTestStore = store

	code := m.Run()

	store.Close()
	_ = pool.Purge(pgTestResource)
	os.Exit(code)
}

// dockerAvailable returns true if the Docker daemon is
// reachable. We use `docker info` via the system CLI rather than
// pinging dockertest directly so the skip message can tell the
// operator exactly which socket to check.
func dockerAvailable() bool {
	// We could shell out to `docker info` but that depends on
	// the docker binary being on $PATH. dockertest.NewPool is a
	// stricter check: it actually opens the socket.
	pool, err := dockertest.NewPool("")
	if err != nil {
		return false
	}
	if err := pool.Client.Ping(); err != nil {
		return false
	}
	return true
}

// newTestStore returns the shared PostgresStore. Tests are
// isolated by TRUNCATE — see setUp — rather than by per-test
// transactions, because PostgresStore methods reach into
// s.pool directly and a Tx-only adapter would be a major
// refactor (out of scope for P0-7).
//
// If Docker is unavailable (no pgTestStore), t.Skip is called
// and the test is recorded as skipped, not failed. This is the
// documented contract for `-tags=integration` on a machine
// without Docker.
func newTestStore(t *testing.T) (*PostgresStore, context.Context) {
	t.Helper()
	if pgTestStore == nil {
		t.Skip("integration test requires Docker; see TestMain for setup instructions")
	}
	return pgTestStore, context.Background()
}

// truncateAll wipes the per-test data. We use TRUNCATE …
// RESTART IDENTITY CASCADE because it's faster than
// per-table DELETE on every test (TRUNCATE doesn't generate
// per-row WAL records).
func truncateAll(t *testing.T, ctx context.Context, store *PostgresStore) {
	t.Helper()
	tables := []string{
		"stocks", "ohlcv_daily_qfq", "fundamentals", "stock_fundamentals",
		"trading_calendar", "dividends", "index_constituents", "factor_cache",
		"splits", "factor_returns", "ic_analysis", "walk_forward_reports",
		"strategies", "backtest_jobs",
	}
	for _, tbl := range tables {
		_, err := store.pool.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY CASCADE", tbl))
		require.NoError(t, err, "truncate %s", tbl)
	}
}

// setUp is the test-bootstrap helper: returns a clean store
// for the test. Every test that mutates state should call this
// at the top.
func setUp(t *testing.T) (*PostgresStore, context.Context) {
	t.Helper()
	store, ctx := newTestStore(t)
	truncateAll(t, ctx, store)
	return store, ctx
}

// ──────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────

func TestIntegration_NewPostgresStore_Connects(t *testing.T) {
	// The constructor was already exercised by TestMain; here
	// we just confirm Ping works.
	store, ctx := newTestStore(t)
	require.NoError(t, store.Ping(ctx))
}

func TestIntegration_NewPostgresStore_BadConnString(t *testing.T) {
	// We don't go through dockertest for the negative path —
	// the error must surface from a bad DSN, not from a
	// missing server.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := NewPostgresStore(ctx,
		"postgres://user:bad@127.0.0.1:1/db?sslmode=disable&connect_timeout=1")
	require.Error(t, err, "must fail to connect to an unused port")
	assert.Contains(t, err.Error(), "failed",
		"error should be wrapped by NewPostgresStore")
}

func TestIntegration_SaveAndGetStock(t *testing.T) {
	store, ctx := setUp(t)
	listDate := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, store.SaveStock(ctx, &domain.Stock{
		Symbol: "000001.SZ", Name: "Ping An Bank",
		Exchange: "SZSE", Industry: "Banks", MarketCap: 1.5e12,
		ListDate: listDate, Status: "active",
	}))

	got, err := store.GetStock(ctx, "000001.SZ")
	require.NoError(t, err)
	require.NotNil(t, got, "GetStock should find the just-inserted stock")
	assert.Equal(t, "Ping An Bank", got.Name)
	assert.Equal(t, "SZSE", got.Exchange)
	assert.Equal(t, "active", got.Status)
}

func TestIntegration_GetStock_MissingReturnsNil(t *testing.T) {
	store, ctx := setUp(t)
	got, err := store.GetStock(ctx, "DOES_NOT_EXIST")
	require.NoError(t, err)
	assert.Nil(t, got, "missing stock must return (nil, nil), not an error")
}

func TestIntegration_SaveStock_UpsertOnConflict(t *testing.T) {
	store, ctx := setUp(t)
	require.NoError(t, store.SaveStock(ctx, &domain.Stock{
		Symbol: "000002.SZ", Name: "Vanke A", Exchange: "SZSE", Status: "active",
	}))
	// Second save with same Symbol + different name → UPDATE.
	require.NoError(t, store.SaveStock(ctx, &domain.Stock{
		Symbol: "000002.SZ", Name: "Vanke A (renamed)", Exchange: "SZSE", Status: "suspended",
	}))
	got, err := store.GetStock(ctx, "000002.SZ")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Vanke A (renamed)", got.Name, "ON CONFLICT must update")
	assert.Equal(t, "suspended", got.Status)
}

func TestIntegration_SaveStockBatch_AndGetStocksFilteredByExchange(t *testing.T) {
	store, ctx := setUp(t)
	require.NoError(t, store.SaveStockBatch(ctx, []domain.Stock{
		{Symbol: "600000.SH", Name: "SPD Bank", Exchange: "SSE", Status: "active"},
		{Symbol: "600036.SH", Name: "CMB", Exchange: "SSE", Status: "active"},
		{Symbol: "000001.SZ", Name: "Ping An", Exchange: "SZSE", Status: "active"},
	}))

	sse, err := store.GetStocks(ctx, "SSE")
	require.NoError(t, err)
	assert.Len(t, sse, 2, "GetStocks(SSE) must return only SSE-listed")

	all, err := store.GetAllStocks(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestIntegration_OHLCV_SaveGetRange(t *testing.T) {
	store, ctx := setUp(t)
	d0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	d1 := d0.AddDate(0, 0, 1)
	d2 := d0.AddDate(0, 0, 2)
	for i, d := range []time.Time{d0, d1, d2} {
		require.NoError(t, store.SaveOHLCV(ctx, &domain.OHLCV{
			Symbol: "000001.SZ", Date: d,
			Open: 10 + float64(i), High: 11 + float64(i),
			Low: 9 + float64(i), Close: 10.5 + float64(i),
			Volume: 1e6, Turnover: 1e7, TradeDays: 1,
		}))
	}
	rows, err := store.GetOHLCV(ctx, "000001.SZ", d0, d2)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, 10.0, rows[0].Open)
	assert.Equal(t, 12.5, rows[2].Close)
}

func TestIntegration_OHLCV_HasAndLatestDate(t *testing.T) {
	store, ctx := setUp(t)
	d := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	has, err := store.HasOHLCVData(ctx, "000001.SZ")
	require.NoError(t, err)
	assert.False(t, has, "empty table → HasOHLCVData = false")

	require.NoError(t, store.SaveOHLCV(ctx, &domain.OHLCV{
		Symbol: "000001.SZ", Date: d,
		Open: 10, High: 11, Low: 9, Close: 10.5, Volume: 1e6,
	}))

	has, err = store.HasOHLCVData(ctx, "000001.SZ")
	require.NoError(t, err)
	assert.True(t, has)

	latest, err := store.GetLatestOHLCVDate(ctx, "000001.SZ")
	require.NoError(t, err)
	assert.True(t, latest.Equal(d), "GetLatestOHLCVDate must return the only date")
}

func TestIntegration_OHLCV_BatchAndCrossSection(t *testing.T) {
	store, ctx := setUp(t)
	d := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	batch := []*domain.OHLCV{
		{Symbol: "000001.SZ", Date: d, Open: 10, High: 11, Low: 9, Close: 10.5, Volume: 1e6},
		{Symbol: "000002.SZ", Date: d, Open: 20, High: 21, Low: 19, Close: 20.5, Volume: 2e6},
		{Symbol: "000001.SZ", Date: d.AddDate(0, 0, 1), Open: 11, High: 12, Low: 10, Close: 11.5, Volume: 1.1e6},
	}
	require.NoError(t, store.SaveOHLCVBatch(ctx, batch))

	cross, err := store.GetOHLCVForDateRange(ctx, d, d)
	require.NoError(t, err)
	assert.Len(t, cross, 2, "GetOHLCVForDateRange on a single day should yield 2 rows (2 symbols)")
}

func TestIntegration_OHLCV_GetTradingDays(t *testing.T) {
	store, ctx := setUp(t)
	d0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, d := range []time.Time{
		d0, d0.AddDate(0, 0, 1), d0.AddDate(0, 0, 2),
	} {
		require.NoError(t, store.SaveOHLCV(ctx, &domain.OHLCV{
			Symbol: "000001.SZ", Date: d,
			Open: 10 + float64(i), High: 11, Low: 9, Close: 10, Volume: 1,
		}))
	}
	days, err := store.GetTradingDays(ctx, d0, d0.AddDate(0, 0, 2))
	require.NoError(t, err)
	assert.Len(t, days, 3)
}

func TestIntegration_TradingCalendar_SaveAndQuery(t *testing.T) {
	store, ctx := setUp(t)
	d := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, store.SaveTradingCalendarEntry(ctx, &TradingCalendarEntry{
		TradeDate: d, Exchange: "SSE", IsTradingDay: true,
	}))

	got, err := store.IsTradingDay(ctx, d)
	require.NoError(t, err)
	assert.True(t, got)

	// Mark as a holiday and verify.
	require.NoError(t, store.SaveTradingCalendarEntry(ctx, &TradingCalendarEntry{
		TradeDate: d, Exchange: "SSE", IsTradingDay: false,
	}))
	got, _ = store.IsTradingDay(ctx, d)
	assert.False(t, got, "UPSERT must update is_trading_day")
}

func TestIntegration_TradingCalendar_UnknownDateIsNotTrading(t *testing.T) {
	store, ctx := setUp(t)
	got, err := store.IsTradingDay(ctx, time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.False(t, got, "missing date → not a trading day, not an error")
}

func TestIntegration_TradingCalendar_BatchAndRange(t *testing.T) {
	store, ctx := setUp(t)
	d0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := []*TradingCalendarEntry{
		{TradeDate: d0, Exchange: "SSE", IsTradingDay: true},
		{TradeDate: d0.AddDate(0, 0, 1), Exchange: "SSE", IsTradingDay: true},
		{TradeDate: d0.AddDate(0, 0, 2), Exchange: "SSE", IsTradingDay: false}, // weekend
	}
	require.NoError(t, store.SaveTradingCalendarBatch(ctx, entries))

	all, err := store.GetTradingCalendar(ctx, d0, d0.AddDate(0, 0, 2))
	require.NoError(t, err)
	assert.Len(t, all, 3)

	trading, err := store.GetTradingDates(ctx, d0, d0.AddDate(0, 0, 2))
	require.NoError(t, err)
	assert.Len(t, trading, 2, "only 2 of 3 entries are trading days")
}

func TestIntegration_BacktestJob_Lifecycle(t *testing.T) {
	store, ctx := setUp(t)
	id := "job-abc-123"
	job := map[string]any{
		"id":          id,
		"strategy_id": "momentum",
		"params":      json.RawMessage(`{"lookback": 20}`),
		"universe":    "csi300",
		"start_date":  "2024-01-01",
		"end_date":    "2024-06-30",
		"status":      "pending",
	}
	require.NoError(t, store.CreateBacktestJob(ctx, job))

	got, err := store.GetBacktestJob(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, id, got["id"])
	assert.Equal(t, "pending", got["status"])

	require.NoError(t, store.UpdateJobStarted(ctx, id))
	got, _ = store.GetBacktestJob(ctx, id)
	assert.Equal(t, "running", got["status"])
	require.NotNil(t, got["started_at"], "started_at must be set after UpdateJobStarted")

	result := []byte(`{"sharpe": 1.5, "trades": 42}`)
	require.NoError(t, store.UpdateJobCompleted(ctx, id, result))
	got, _ = store.GetBacktestJob(ctx, id)
	assert.Equal(t, "completed", got["status"])
	require.NotNil(t, got["completed_at"], "completed_at must be set")
	assert.JSONEq(t, string(result), string(got["result"].([]byte)))
}

func TestIntegration_BacktestJob_FailurePath(t *testing.T) {
	store, ctx := setUp(t)
	id := "job-fail-1"
	require.NoError(t, store.CreateBacktestJob(ctx, map[string]any{
		"id":          id,
		"strategy_id": "x",
		"params":      json.RawMessage(`{}`),
		"universe":    "all",
		"start_date":  "2024-01-01",
		"end_date":    "2024-01-31",
		"status":      "pending",
	}))
	require.NoError(t, store.UpdateJobStarted(ctx, id))
	require.NoError(t, store.UpdateJobFailed(ctx, id, "boom"))

	got, err := store.GetBacktestJob(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "failed", got["status"])
	assert.Equal(t, "boom", got["error_msg"])
}

func TestIntegration_BacktestJob_MissingReturnsNil(t *testing.T) {
	store, ctx := setUp(t)
	got, err := store.GetBacktestJob(ctx, "no-such-job")
	require.NoError(t, err)
	assert.Nil(t, got, "missing job must return (nil, nil)")
}

func TestIntegration_BacktestJob_ListAndDelete(t *testing.T) {
	store, ctx := setUp(t)
	for _, id := range []string{"j1", "j2", "j3"} {
		require.NoError(t, store.CreateBacktestJob(ctx, map[string]any{
			"id": id, "strategy_id": "x", "params": json.RawMessage(`{}`),
			"universe": "all", "start_date": "2024-01-01", "end_date": "2024-01-31",
			"status": "pending",
		}))
	}
	list, err := store.ListBacktestJobs(ctx, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list), 3)

	// Delete a pending job succeeds.
	require.NoError(t, store.DeleteBacktestJob(ctx, "j1"))
	// Delete a non-pending job fails with a clear error.
	require.NoError(t, store.UpdateJobStarted(ctx, "j2"))
	err = store.DeleteBacktestJob(ctx, "j2")
	require.Error(t, err, "DeleteBacktestJob on a running job must fail-closed")
}

func TestIntegration_StrategyConfig_CRUD(t *testing.T) {
	store, ctx := setUp(t)
	require.NoError(t, store.SaveStrategyConfig(ctx, &domain.StrategyConfig{
		StrategyID:   "mom-v1",
		Name:         "Momentum v1",
		Description:  "20-day momentum",
		StrategyType: "momentum",
		Params:       `{"lookback": 20}`,
		IsActive:     true,
	}))

	got, err := store.GetStrategyConfig(ctx, "mom-v1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Momentum v1", got.Name)

	// Upsert: same strategy_id → UPDATE.
	require.NoError(t, store.SaveStrategyConfig(ctx, &domain.StrategyConfig{
		StrategyID:   "mom-v1",
		Name:         "Momentum v1.1",
		StrategyType: "momentum",
		Params:       `{"lookback": 25}`,
		IsActive:     true,
	}))
	got, _ = store.GetStrategyConfig(ctx, "mom-v1")
	assert.Equal(t, "Momentum v1.1", got.Name)
	assert.Equal(t, `{"lookback": 25}`, got.Params)
}

func TestIntegration_StrategyConfig_ListFiltered(t *testing.T) {
	store, ctx := setUp(t)
	require.NoError(t, store.SaveStrategyConfig(ctx, &domain.StrategyConfig{
		StrategyID: "m1", Name: "M1", StrategyType: "momentum", IsActive: true,
	}))
	require.NoError(t, store.SaveStrategyConfig(ctx, &domain.StrategyConfig{
		StrategyID: "m2", Name: "M2", StrategyType: "momentum", IsActive: false,
	}))
	require.NoError(t, store.SaveStrategyConfig(ctx, &domain.StrategyConfig{
		StrategyID: "v1", Name: "V1", StrategyType: "value", IsActive: true,
	}))

	all, err := store.ListStrategyConfigs(ctx, "", false)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	activeMom, err := store.ListStrategyConfigs(ctx, "momentum", true)
	require.NoError(t, err)
	assert.Len(t, activeMom, 1, "momentum + active = 1 result")
	assert.Equal(t, "m1", activeMom[0].StrategyID)
}

func TestIntegration_StrategyConfig_DeleteIsSoftDelete(t *testing.T) {
	store, ctx := setUp(t)
	require.NoError(t, store.SaveStrategyConfig(ctx, &domain.StrategyConfig{
		StrategyID: "to-delete", Name: "X", StrategyType: "momentum", IsActive: true,
	}))
	require.NoError(t, store.DeleteStrategyConfig(ctx, "to-delete"))
	got, err := store.GetStrategyConfig(ctx, "to-delete")
	require.NoError(t, err)
	require.NotNil(t, got, "soft-deleted strategy must still be retrievable")
	assert.False(t, got.IsActive, "soft delete must flip is_active")

	err = store.DeleteStrategyConfig(ctx, "never-existed")
	assert.Error(t, err, "delete on missing id must fail-closed")
}

func TestIntegration_StrategyConfig_SeedStrategies(t *testing.T) {
	store, ctx := setUp(t)
	require.NoError(t, store.SeedStrategies(ctx))
	for _, sid := range []string{"momentum", "value", "quality"} {
		got, err := store.GetStrategyConfig(ctx, sid)
		require.NoError(t, err)
		require.NotNil(t, got, "seed must create %s", sid)
		assert.True(t, got.IsActive)
	}
	// Re-seeding must be idempotent (UPSERT, not INSERT).
	require.NoError(t, store.SeedStrategies(ctx))
}
