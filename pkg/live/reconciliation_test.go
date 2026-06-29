package live

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// ============================================================
// 测试辅助: stub BrokerQuerier + LocalSnapshotter + Dispatcher
//
// 注意: pkg/live 包内已有 stubBroker (order_manager_cage_test.go),
// 这里用 recon 前缀避免冲突。
// ============================================================

type reconStubBroker struct {
	mu      sync.Mutex
	snap    AccountSnapshot
	err     error
	calls   int
	lastAs  time.Time
	lastAcc string
}

func (s *reconStubBroker) QuerySettlement(_ context.Context, accountID string, asOf time.Time) (*AccountSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastAs = asOf
	s.lastAcc = accountID
	if s.err != nil {
		return nil, s.err
	}
	snap := s.snap
	snap.AsOf = asOf
	return &snap, nil
}

type reconStubLocal struct {
	mu    sync.Mutex
	snap  LocalSnapshot
	err   error
	calls int
}

func (s *reconStubLocal) SnapshotLocal(_ context.Context, accountID string, asOf time.Time) (*LocalSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	snap := s.snap
	snap.AsOf = asOf
	// Honor the embedded snap.AccountID when set, otherwise the
	// caller's accountID. This lets tests pre-load a default account
	// identity and have the worker auto-detect it on probe calls.
	if s.snap.AccountID != "" {
		snap.AccountID = s.snap.AccountID
	} else {
		snap.AccountID = accountID
	}
	return &snap, nil
}

type reconStubDispatcher struct {
	mu     sync.Mutex
	calls  int
	lastID string
	last   []Discrepancy
}

func (d *reconStubDispatcher) DispatchReconciliationAlerts(_ context.Context, accountID string, discs []Discrepancy) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	d.lastID = accountID
	d.last = discs
}

// ============================================================
// TestReconcile_Healthy — local and broker match exactly
// ============================================================

func TestReconcile_Healthy(t *testing.T) {
	cfg := DefaultReconciliationConfig()
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return now }

	local := LocalSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 1_000_000.00},
		Positions: map[string]PositionSnap{
			"000001.SZ": {Symbol: "000001.SZ", Quantity: 1000, AvgCost: 10.0, MarketValue: 10500.0},
		},
		Fees: FeeSnap{Commission: 25.0, StampTax: 10.5, Transfer: 1.05},
	}
	broker := AccountSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 1_000_000.00},
		Positions: map[string]PositionSnap{
			"000001.SZ": {Symbol: "000001.SZ", Quantity: 1000, AvgCost: 10.0, MarketValue: 10500.0},
		},
		Fees: FeeSnap{Commission: 25.0, StampTax: 10.5, Transfer: 1.05},
	}
	discs := Reconcile(local, broker, cfg)
	if len(discs) != 0 {
		t.Fatalf("expected healthy (no discrepancies), got %+v", discs)
	}
}

// ============================================================
// TestReconcile_CashDelta — cash mismatch
// ============================================================

func TestReconcile_CashDelta(t *testing.T) {
	cfg := DefaultReconciliationConfig()
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return now }

	local := LocalSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 1_000_000.00},
		Positions: map[string]PositionSnap{},
	}
	broker := AccountSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 1_000_150.00}, // 150 CNY off
		Positions: map[string]PositionSnap{},
	}
	discs := Reconcile(local, broker, cfg)
	if len(discs) != 1 {
		t.Fatalf("expected 1 discrepancy, got %d: %+v", len(discs), discs)
	}
	d := discs[0]
	if d.Kind != KindCash {
		t.Errorf("expected kind=cash, got %q", d.Kind)
	}
	if d.Severity != SeverityCritical {
		t.Errorf("expected severity=critical (150 > 100), got %q", d.Severity)
	}
	if d.Delta != -150 {
		t.Errorf("expected delta=-150, got %v", d.Delta)
	}
}

// ============================================================
// TestReconcile_CashWarning — small but above tolerance
// ============================================================

func TestReconcile_CashWarning(t *testing.T) {
	cfg := DefaultReconciliationConfig()
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return now }

	local := LocalSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 1_000_050.00},
		Positions: map[string]PositionSnap{},
	}
	broker := AccountSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 1_000_000.00}, // 50 CNY off
		Positions: map[string]PositionSnap{},
	}
	discs := Reconcile(local, broker, cfg)
	if len(discs) != 1 {
		t.Fatalf("expected 1, got %d", len(discs))
	}
	if discs[0].Severity != SeverityWarning {
		t.Errorf("expected warning (50 in 1-100 range), got %q", discs[0].Severity)
	}
}

// ============================================================
// TestReconcile_CashWithinTolerance — 1-cent rounding
// ============================================================

func TestReconcile_CashWithinTolerance(t *testing.T) {
	cfg := DefaultReconciliationConfig()
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return now }

	local := LocalSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 1_000_000.005}, // 0.5 cent off
		Positions: map[string]PositionSnap{},
	}
	broker := AccountSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 1_000_000.00},
		Positions: map[string]PositionSnap{},
	}
	discs := Reconcile(local, broker, cfg)
	if len(discs) != 0 {
		t.Fatalf("expected no discrepancies (within 1 cent), got %+v", discs)
	}
}

// ============================================================
// TestReconcile_QuantityMissing — broker has, local doesn't
// ============================================================

func TestReconcile_QuantityMissing(t *testing.T) {
	cfg := DefaultReconciliationConfig()
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return now }

	local := LocalSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 0},
		Positions: map[string]PositionSnap{},
	}
	broker := AccountSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 0},
		Positions: map[string]PositionSnap{
			"000002.SZ": {Symbol: "000002.SZ", Quantity: 500, MarketValue: 5000},
		},
	}
	discs := Reconcile(local, broker, cfg)
	if len(discs) != 1 {
		t.Fatalf("expected 1, got %d", len(discs))
	}
	d := discs[0]
	if d.Kind != KindMissingLocal {
		t.Errorf("expected kind=missing_local, got %q", d.Kind)
	}
	if d.Severity != SeverityCritical {
		t.Errorf("expected critical, got %q", d.Severity)
	}
	if d.Symbol != "000002.SZ" {
		t.Errorf("expected symbol=000002.SZ, got %q", d.Symbol)
	}
}

// ============================================================
// TestReconcile_QuantityGhost — local has, broker doesn't
// ============================================================

func TestReconcile_QuantityGhost(t *testing.T) {
	cfg := DefaultReconciliationConfig()
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return now }

	local := LocalSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 0},
		Positions: map[string]PositionSnap{
			"000003.SZ": {Symbol: "000003.SZ", Quantity: 200, MarketValue: 2000},
		},
	}
	broker := AccountSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 0},
		Positions: map[string]PositionSnap{},
	}
	discs := Reconcile(local, broker, cfg)
	if len(discs) != 1 {
		t.Fatalf("expected 1, got %d", len(discs))
	}
	if discs[0].Kind != KindMissingBroker {
		t.Errorf("expected kind=missing_broker, got %q", discs[0].Kind)
	}
}

// ============================================================
// TestReconcile_QuantityDelta — both have, qty differs
// ============================================================

func TestReconcile_QuantityDelta(t *testing.T) {
	cfg := DefaultReconciliationConfig()
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return now }

	local := LocalSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 0},
		Positions: map[string]PositionSnap{
			"600000.SH": {Symbol: "600000.SH", Quantity: 1000, MarketValue: 10000},
		},
	}
	broker := AccountSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 0},
		Positions: map[string]PositionSnap{
			"600000.SH": {Symbol: "600000.SH", Quantity: 998, MarketValue: 9980},
		},
	}
	discs := Reconcile(local, broker, cfg)
	if len(discs) != 1 {
		t.Fatalf("expected 1, got %d", len(discs))
	}
	d := discs[0]
	if d.Kind != KindQuantity {
		t.Errorf("expected kind=quantity, got %q", d.Kind)
	}
	if d.Delta != 2 {
		t.Errorf("expected delta=2, got %v", d.Delta)
	}
	if d.Severity != SeverityWarning {
		t.Errorf("expected warning (2 shares < 1000), got %q", d.Severity)
	}
}

// ============================================================
// TestReconcile_MarketValueDelta — qty matches, price differs
// ============================================================

func TestReconcile_MarketValueDelta(t *testing.T) {
	cfg := DefaultReconciliationConfig()
	cfg.MarketValueTol = 0.005
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return now }

	local := LocalSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 0},
		Positions: map[string]PositionSnap{
			"000001.SZ": {Symbol: "000001.SZ", Quantity: 1000, MarketValue: 10000}, // 10.0
		},
	}
	broker := AccountSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 0},
		Positions: map[string]PositionSnap{
			"000001.SZ": {Symbol: "000001.SZ", Quantity: 1000, MarketValue: 10250}, // 10.25 = 2.5% off
		},
	}
	discs := Reconcile(local, broker, cfg)
	if len(discs) != 1 {
		t.Fatalf("expected 1, got %d", len(discs))
	}
	if discs[0].Kind != KindMarketValue {
		t.Errorf("expected kind=market_value, got %q", discs[0].Kind)
	}
	if discs[0].Severity != SeverityWarning {
		t.Errorf("expected warning (2.5%% > 2*tol), got %q (rel would be ~%v)", discs[0].Severity, 0.025/cfg.MarketValueTol)
	}
}

// ============================================================
// TestReconcile_FeeDelta — each fee bucket checked
// ============================================================

func TestReconcile_FeeDelta(t *testing.T) {
	cfg := DefaultReconciliationConfig()
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return now }

	local := LocalSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 0},
		Positions: map[string]PositionSnap{},
		Fees: FeeSnap{
			Commission: 30.0,
			StampTax:   10.5,
			Transfer:   1.05,
			Other:      0.5,
		},
	}
	broker := AccountSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 0},
		Positions: map[string]PositionSnap{},
		Fees: FeeSnap{
			Commission: 30.0,
			StampTax:   10.5,
			Transfer:   1.10, // 0.05 CNY off
			Other:      0.5,
		},
	}
	discs := Reconcile(local, broker, cfg)
	if len(discs) != 1 {
		t.Fatalf("expected 1, got %d: %+v", len(discs), discs)
	}
	if discs[0].Kind != KindFee {
		t.Errorf("expected kind=fee, got %q", discs[0].Kind)
	}
	if discs[0].Field != "transfer" {
		t.Errorf("expected field=transfer, got %q", discs[0].Field)
	}
	if discs[0].Delta >= -0.04 || discs[0].Delta <= -0.06 {
		t.Errorf("expected delta ≈ -0.05, got %v", discs[0].Delta)
	}
}

// ============================================================
// TestReconcile_AsOfDrift — timestamp drift > 60s fires info
// ============================================================

func TestReconcile_AsOfDrift(t *testing.T) {
	cfg := DefaultReconciliationConfig()
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return now }

	local := LocalSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 100.0},
		Positions: map[string]PositionSnap{},
	}
	broker := AccountSnapshot{
		AccountID: "ACC-1",
		AsOf:      now.Add(-5 * time.Minute),
		Cash:      CashSnap{TotalCNY: 100.0},
		Positions: map[string]PositionSnap{},
	}
	discs := Reconcile(local, broker, cfg)
	if len(discs) != 1 {
		t.Fatalf("expected 1 (asof drift), got %d", len(discs))
	}
	if discs[0].Severity != SeverityInfo {
		t.Errorf("expected info, got %q", discs[0].Severity)
	}
	if discs[0].Field != "as_of_drift_seconds" {
		t.Errorf("expected field=as_of_drift_seconds, got %q", discs[0].Field)
	}
}

// ============================================================
// TestReconcile_MultipleDiscrepancies — all categories fire
// ============================================================

func TestReconcile_MultipleDiscrepancies(t *testing.T) {
	cfg := DefaultReconciliationConfig()
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg.Now = func() time.Time { return now }

	local := LocalSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 1_000_000.00},
		Positions: map[string]PositionSnap{
			"000001.SZ": {Symbol: "000001.SZ", Quantity: 1000, MarketValue: 10000},
		},
		Fees: FeeSnap{Commission: 50.0},
	}
	broker := AccountSnapshot{
		AccountID: "ACC-1",
		AsOf:      now,
		Cash:      CashSnap{TotalCNY: 999_900.00}, // 100 CNY off
		Positions: map[string]PositionSnap{
			"000001.SZ": {Symbol: "000001.SZ", Quantity: 998, MarketValue: 9980}, // 2 off
		},
		Fees: FeeSnap{Commission: 60.0}, // 10 off
	}
	discs := Reconcile(local, broker, cfg)
	// Expect: cash + quantity + fee = 3
	if len(discs) != 3 {
		t.Fatalf("expected 3, got %d: %+v", len(discs), discs)
	}
	// Verify sort order: critical first
	if discs[0].Severity != SeverityCritical {
		t.Errorf("expected first disc to be critical, got %q", discs[0].Severity)
	}
}

// ============================================================
// TestReconcileWorker_HappyPath — full cycle, healthy
// ============================================================

func TestReconcileWorker_HappyPath(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg := ReconciliationConfig{
		Interval:          100 * time.Millisecond,
		CashTolerance:     0.01,
		QuantityTolerance: 0,
		MarketValueTol:    0.005,
		FeeTolerance:      0.01,
		ReportPath:        t.TempDir(),
		HistoryLimit:      5,
		Enabled:           true,
		Now:               func() time.Time { return now },
	}

	broker := &reconStubBroker{
		snap: AccountSnapshot{
			AccountID:        "ACC-1",
			ReconciliationID: "RECON-2026-06-13-001",
			Cash:             CashSnap{TotalCNY: 1_000_000.00},
			Positions: map[string]PositionSnap{
				"000001.SZ": {Symbol: "000001.SZ", Quantity: 1000, AvgCost: 10.0, MarketValue: 10000},
			},
			Fees: FeeSnap{Commission: 25.0},
		},
	}
	local := &reconStubLocal{
		snap: LocalSnapshot{
			AccountID: "ACC-1",
			Cash:      CashSnap{TotalCNY: 1_000_000.00},
			Positions: map[string]PositionSnap{
				"000001.SZ": {Symbol: "000001.SZ", Quantity: 1000, AvgCost: 10.0, MarketValue: 10000},
			},
			Fees: FeeSnap{Commission: 25.0},
		},
	}
	dispatcher := &reconStubDispatcher{}
	persister, err := NewFSReportPersister(cfg.ReportPath)
	if err != nil {
		t.Fatalf("NewFSReportPersister: %v", err)
	}

	worker := NewReconciliationWorker(cfg, broker, local, persister, dispatcher, nil, zerolog.Nop())

	rep, err := worker.ReconcileOnce(context.Background(), "ACC-1")
	if err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if !rep.Healthy {
		t.Errorf("expected healthy, got critical=%d warning=%d", rep.CriticalCount, rep.WarningCount)
	}
	if rep.AccountID != "ACC-1" {
		t.Errorf("expected account ACC-1, got %q", rep.AccountID)
	}
	if rep.BridgeReconcID != "RECON-2026-06-13-001" {
		t.Errorf("expected bridge id, got %q", rep.BridgeReconcID)
	}
	if broker.calls != 1 {
		t.Errorf("expected 1 broker call, got %d", broker.calls)
	}
	if local.calls != 1 {
		t.Errorf("expected 1 local call, got %d", local.calls)
	}
	if dispatcher.calls != 0 {
		t.Errorf("expected 0 dispatcher calls (healthy), got %d", dispatcher.calls)
	}

	// Verify the report was persisted.
	files, _ := os.ReadDir(cfg.ReportPath)
	if len(files) != 1 {
		t.Fatalf("expected 1 persisted file, got %d", len(files))
	}
	if filepath.Ext(files[0].Name()) != ".json" {
		t.Errorf("expected .json file, got %q", files[0].Name())
	}
	// Verify the file is valid JSON.
	raw, _ := os.ReadFile(filepath.Join(cfg.ReportPath, files[0].Name()))
	var got ReconciliationReport
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("persisted file not valid JSON: %v", err)
	}
	if got.AccountID != "ACC-1" {
		t.Errorf("persisted report account_id mismatch: %q", got.AccountID)
	}
}

// ============================================================
// TestReconcileWorker_DiscrepanciesDispatched
// ============================================================

func TestReconcileWorker_DiscrepanciesDispatched(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg := ReconciliationConfig{
		Interval:          100 * time.Millisecond,
		CashTolerance:     0.01,
		QuantityTolerance: 0,
		MarketValueTol:    0.005,
		FeeTolerance:      0.01,
		ReportPath:        t.TempDir(),
		HistoryLimit:      5,
		Enabled:           true,
		Now:               func() time.Time { return now },
	}

	broker := &reconStubBroker{
		snap: AccountSnapshot{
			AccountID: "ACC-1",
			Cash:      CashSnap{TotalCNY: 999_850.00}, // 150 off
			Positions: map[string]PositionSnap{},
		},
	}
	local := &reconStubLocal{
		snap: LocalSnapshot{
			AccountID: "ACC-1",
			Cash:      CashSnap{TotalCNY: 1_000_000.00},
			Positions: map[string]PositionSnap{},
		},
	}
	dispatcher := &reconStubDispatcher{}
	persister, _ := NewFSReportPersister(cfg.ReportPath)

	worker := NewReconciliationWorker(cfg, broker, local, persister, dispatcher, nil, zerolog.Nop())
	rep, err := worker.ReconcileOnce(context.Background(), "ACC-1")
	if err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if rep.Healthy {
		t.Errorf("expected unhealthy, got healthy=true")
	}
	if rep.CriticalCount != 1 {
		t.Errorf("expected 1 critical, got %d", rep.CriticalCount)
	}
	if dispatcher.calls != 1 {
		t.Errorf("expected 1 dispatcher call, got %d", dispatcher.calls)
	}
	if dispatcher.lastID != "ACC-1" {
		t.Errorf("expected dispatcher.account ACC-1, got %q", dispatcher.lastID)
	}
	if len(dispatcher.last) != 1 {
		t.Errorf("expected 1 dispatched disc, got %d", len(dispatcher.last))
	}
}

// ============================================================
// TestReconcileWorker_EmptyAccountID — uses local probe
// ============================================================

func TestReconcileWorker_EmptyAccountID(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg := DefaultReconciliationConfig()
	cfg.Interval = 100 * time.Millisecond
	cfg.Now = func() time.Time { return now }
	cfg.ReportPath = t.TempDir()

	broker := &reconStubBroker{
		snap: AccountSnapshot{
			AccountID: "AUTO-DETECT",
			Cash:      CashSnap{TotalCNY: 100},
			Positions: map[string]PositionSnap{},
		},
	}
	local := &reconStubLocal{
		snap: LocalSnapshot{
			AccountID: "AUTO-DETECT",
			Cash:      CashSnap{TotalCNY: 100},
			Positions: map[string]PositionSnap{},
		},
	}
	worker := NewReconciliationWorker(cfg, broker, local, NullReportPersister{}, &reconStubDispatcher{}, nil, zerolog.Nop())

	// Pass empty accountID; worker should probe local.
	rep, err := worker.ReconcileOnce(context.Background(), "")
	if err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if rep.AccountID != "AUTO-DETECT" {
		t.Errorf("expected auto-detected ACC-1, got %q", rep.AccountID)
	}
}

// ============================================================
// TestReconcileWorker_LocalFetchError
// ============================================================

func TestReconcileWorker_LocalFetchError(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg := DefaultReconciliationConfig()
	cfg.Interval = 100 * time.Millisecond
	cfg.Now = func() time.Time { return now }
	cfg.ReportPath = t.TempDir()

	broker := &reconStubBroker{snap: AccountSnapshot{}}
	local := &reconStubLocal{err: fmt.Errorf("db connection lost")}
	dispatcher := &reconStubDispatcher{}
	worker := NewReconciliationWorker(cfg, broker, local, NullReportPersister{}, dispatcher, nil, zerolog.Nop())

	_, err := worker.ReconcileOnce(context.Background(), "ACC-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if dispatcher.calls != 0 {
		t.Errorf("expected 0 dispatcher calls on error, got %d", dispatcher.calls)
	}
}

// ============================================================
// TestReconcileWorker_BrokerFetchError
// ============================================================

func TestReconcileWorker_BrokerFetchError(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg := DefaultReconciliationConfig()
	cfg.Interval = 100 * time.Millisecond
	cfg.Now = func() time.Time { return now }
	cfg.ReportPath = t.TempDir()

	broker := &reconStubBroker{err: fmt.Errorf("broker timeout")}
	local := &reconStubLocal{snap: LocalSnapshot{AccountID: "ACC-1"}}
	worker := NewReconciliationWorker(cfg, broker, local, NullReportPersister{}, &reconStubDispatcher{}, nil, zerolog.Nop())

	_, err := worker.ReconcileOnce(context.Background(), "ACC-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ============================================================
// TestReconcileWorker_Disabled — Start returns immediately
// ============================================================

func TestReconcileWorker_Disabled(t *testing.T) {
	cfg := DefaultReconciliationConfig()
	cfg.Enabled = false
	worker := NewReconciliationWorker(cfg, &reconStubBroker{}, &reconStubLocal{}, NullReportPersister{}, &reconStubDispatcher{}, nil, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// Start should return immediately; ctx cancellation is a no-op
	// because the loop never began.
	worker.Start(ctx)
}

// ============================================================
// TestReconcileWorker_StartRunsPeriodically
// ============================================================

func TestReconcileWorker_StartRunsPeriodically(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	cfg := ReconciliationConfig{
		Interval:      20 * time.Millisecond,
		CashTolerance: 0.01,
		ReportPath:    t.TempDir(),
		HistoryLimit:  10,
		Enabled:       true,
		Now:           func() time.Time { return now },
	}
	broker := &reconStubBroker{
		snap: AccountSnapshot{
			AccountID: "ACC-1",
			Cash:      CashSnap{TotalCNY: 100},
			Positions: map[string]PositionSnap{},
		},
	}
	local := &reconStubLocal{
		snap: LocalSnapshot{
			AccountID: "ACC-1",
			Cash:      CashSnap{TotalCNY: 100},
			Positions: map[string]PositionSnap{},
		},
	}
	dispatcher := &reconStubDispatcher{}
	worker := NewReconciliationWorker(cfg, broker, local, NullReportPersister{}, dispatcher, nil, zerolog.Nop())

	// Sanity: drive one cycle synchronously to confirm wiring works.
	if _, err := worker.ReconcileOnce(context.Background(), "ACC-1"); err != nil {
		t.Fatalf("ReconcileOnce sanity: %v", err)
	}
	if broker.calls != 1 {
		t.Fatalf("expected 1 broker call after sanity, got %d", broker.calls)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	worker.Start(ctx)

	// After 200ms with 20ms interval, expect at least 3 additional cycles
	// (first tick at 20ms, then 40/60/80/100/120/140/160/180 = 8 more).
	if broker.calls < 4 {
		t.Errorf("expected ≥4 broker calls total, got %d", broker.calls)
	}
	// History should have at least 1 report.
	if worker.History().Len() < 1 {
		t.Errorf("expected ≥1 history entry, got %d", worker.History().Len())
	}
}

// ============================================================
// TestHistoryBuffer
// ============================================================

func TestHistoryBuffer_AppendSnapshot(t *testing.T) {
	h := NewHistoryBuffer(3)
	if h.Len() != 0 {
		t.Errorf("expected empty, got %d", h.Len())
	}
	for i := 0; i < 5; i++ {
		h.Append(ReconciliationReport{AccountID: fmt.Sprintf("A-%d", i)})
	}
	if h.Len() != 3 {
		t.Errorf("expected capped at 3, got %d", h.Len())
	}
	snap := h.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected 3 in snap, got %d", len(snap))
	}
	// Newest first.
	if snap[0].AccountID != "A-4" {
		t.Errorf("expected newest A-4, got %q", snap[0].AccountID)
	}
	if snap[2].AccountID != "A-2" {
		t.Errorf("expected oldest A-2, got %q", snap[2].AccountID)
	}
}

func TestHistoryBuffer_Latest(t *testing.T) {
	h := NewHistoryBuffer(5)
	if h.Latest() != nil {
		t.Error("expected nil on empty")
	}
	h.Append(ReconciliationReport{AccountID: "A"})
	h.Append(ReconciliationReport{AccountID: "B"})
	latest := h.Latest()
	if latest == nil || latest.AccountID != "B" {
		t.Errorf("expected latest B, got %+v", latest)
	}
}

func TestHistoryBuffer_DefaultCapacity(t *testing.T) {
	h := NewHistoryBuffer(0)
	if h.cap != 1 {
		t.Errorf("expected default cap 1, got %d", h.cap)
	}
}

// ============================================================
// TestFSReportPersister
// ============================================================

func TestFSReportPersister_PersistAndRead(t *testing.T) {
	dir := t.TempDir()
	p, err := NewFSReportPersister(dir)
	if err != nil {
		t.Fatalf("NewFSReportPersister: %v", err)
	}
	rep := ReconciliationReport{
		SchemaVersion: "rec-1.0",
		GeneratedAt:   time.Date(2026, 6, 13, 10, 15, 0, 0, time.UTC),
		AccountID:     "ACC-1",
		Discrepancies: []Discrepancy{},
		Healthy:       true,
	}
	if err := p.PersistReport(context.Background(), rep); err != nil {
		t.Fatalf("PersistReport: %v", err)
	}
	// Verify file exists.
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Name() != "rec-20260613-101500.json" {
		t.Errorf("expected filename rec-20260613-101500.json, got %q", files[0].Name())
	}
}

func TestFSReportPersister_EmptyDirError(t *testing.T) {
	_, err := NewFSReportPersister("")
	if err == nil {
		t.Error("expected error on empty dir")
	}
}

// ============================================================
// TestFeeSnapSum
// ============================================================

func TestFeeSnapSum(t *testing.T) {
	f := FeeSnap{Commission: 10, StampTax: 5, Transfer: 1, Other: 0.5}
	if got := f.Sum(); got != 16.5 {
		t.Errorf("expected 16.5, got %v", got)
	}
}

// ============================================================
// TestUnionSymbols
// ============================================================

func TestUnionSymbols(t *testing.T) {
	a := map[string]PositionSnap{
		"000001.SZ": {},
		"000002.SZ": {},
	}
	b := map[string]PositionSnap{
		"000002.SZ": {},
		"000003.SZ": {},
	}
	got := unionSymbols(a, b)
	if len(got) != 3 {
		t.Errorf("expected 3 symbols, got %d: %+v", len(got), got)
	}
	expected := []string{"000001.SZ", "000002.SZ", "000003.SZ"}
	for i, s := range expected {
		if got[i] != s {
			t.Errorf("position %d: expected %q, got %q", i, s, got[i])
		}
	}
}

// ============================================================
// TestSeverityEscalation — verify severityFor* helpers
// ============================================================

func TestSeverityForCash(t *testing.T) {
	cases := []struct {
		delta, tol float64
		want       DiscrepancySeverity
	}{
		{0.005, 0.01, SeverityInfo},
		{50, 0.01, SeverityWarning},
		{500, 0.01, SeverityCritical},
	}
	for _, c := range cases {
		got := severityForCash(c.delta, c.tol)
		if got != c.want {
			t.Errorf("severityForCash(%v, %v) = %q, want %q", c.delta, c.tol, got, c.want)
		}
	}
}

func TestSeverityForQty(t *testing.T) {
	cases := []struct {
		delta float64
		want  DiscrepancySeverity
	}{
		{0, SeverityInfo},
		{500, SeverityWarning},
		{5000, SeverityCritical},
	}
	for _, c := range cases {
		got := severityForQty(c.delta)
		if got != c.want {
			t.Errorf("severityForQty(%v) = %q, want %q", c.delta, got, c.want)
		}
	}
}

func TestSeverityForMV(t *testing.T) {
	cases := []struct {
		rel, tol float64
		want     DiscrepancySeverity
	}{
		{0.003, 0.005, SeverityInfo},     // < tol
		{0.015, 0.005, SeverityWarning},  // 3x tol
		{0.030, 0.005, SeverityCritical}, // 6x tol
	}
	for _, c := range cases {
		got := severityForMV(c.rel, c.tol)
		if got != c.want {
			t.Errorf("severityForMV(%v, %v) = %q, want %q", c.rel, c.tol, got, c.want)
		}
	}
}

func TestSeverityForFee(t *testing.T) {
	cases := []struct {
		delta, tol float64
		want       DiscrepancySeverity
	}{
		{0.005, 0.01, SeverityInfo},
		{0.05, 0.01, SeverityWarning},
		{1.0, 0.01, SeverityCritical}, // 100x tol
	}
	for _, c := range cases {
		got := severityForFee(c.delta, c.tol)
		if got != c.want {
			t.Errorf("severityForFee(%v, %v) = %q, want %q", c.delta, c.tol, got, c.want)
		}
	}
}

// ============================================================
// TestAbsFloat
// ============================================================

func TestAbsFloat(t *testing.T) {
	if absFloat(0) != 0 {
		t.Error("expected 0")
	}
	if absFloat(-5.5) != 5.5 {
		t.Error("expected 5.5")
	}
	if absFloat(3.2) != 3.2 {
		t.Error("expected 3.2")
	}
}

// ============================================================
// TestAllInfoOnly
// ============================================================

func TestAllInfoOnly(t *testing.T) {
	if !allInfoOnly(nil) {
		t.Error("expected empty = all info")
	}
	if !allInfoOnly([]Discrepancy{{Severity: SeverityInfo}}) {
		t.Error("expected single info = all info")
	}
	if allInfoOnly([]Discrepancy{{Severity: SeverityCritical}}) {
		t.Error("expected critical != all info")
	}
	mixed := []Discrepancy{{Severity: SeverityInfo}, {Severity: SeverityWarning}}
	if allInfoOnly(mixed) {
		t.Error("expected mixed != all info")
	}
}

// ============================================================
// TestCashNote
// ============================================================

func TestCashNote(t *testing.T) {
	if cashNote(50) == "" {
		t.Error("positive delta should have a note")
	}
	if cashNote(-50) == "" {
		t.Error("negative delta should have a note")
	}
	if cashNote(50) == cashNote(-50) {
		t.Error("positive and negative deltas should have different notes")
	}
}

// ============================================================
// TestRankSeverity
// ============================================================

func TestRankSeverity(t *testing.T) {
	if rankSeverity(SeverityCritical) <= rankSeverity(SeverityWarning) {
		t.Error("critical should rank higher than warning")
	}
	if rankSeverity(SeverityWarning) <= rankSeverity(SeverityInfo) {
		t.Error("warning should rank higher than info")
	}
}
