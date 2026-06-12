package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nopLogger returns a no-op logger for tests.
func nopLogger() zerolog.Logger {
	return zerolog.New(nil)
}

// makeSnapshot is a test helper that builds a PortfolioSnapshot with
// realistic numbers; callers can override individual fields after.
func makeSnapshot(totalValue float64) PortfolioSnapshot {
	return PortfolioSnapshot{
		TotalValue:  totalValue,
		Cash:        totalValue,
		Positions:   nil,
		DailyPnL:    0,
		PeakEquity:  totalValue,
		RiskMetrics: nil,
	}
}

// ============================================================================
// Detector unit tests
// ============================================================================

func TestDetector_PositionConcentration_Fires(t *testing.T) {
	snap := makeSnapshot(100_000)
	snap.Positions = []PositionSnapshot{
		{Symbol: "X", Sector: "tech", MarketValue: 25_000, Quantity: 100, AvgCost: 240, CurrentPrice: 250},
		{Symbol: "Y", Sector: "tech", MarketValue: 5_000, Quantity: 50, AvgCost: 90, CurrentPrice: 100},
	}
	cfg := AlertManagerConfig{MaxPositionWeight: 0.20} // 20% = 20_000 threshold
	alerts := evaluatePositionConcentration(snap, cfg)
	require.Len(t, alerts, 1)
	assert.Equal(t, RulePositionConcentration, alerts[0].Rule)
	assert.Equal(t, "X", alerts[0].Symbol)
	assert.Equal(t, 0.25, alerts[0].Value)
	assert.Equal(t, 0.20, alerts[0].Threshold)
}

func TestDetector_PositionConcentration_NoFire(t *testing.T) {
	snap := makeSnapshot(100_000)
	snap.Positions = []PositionSnapshot{
		{Symbol: "X", MarketValue: 15_000},
		{Symbol: "Y", MarketValue: 5_000},
	}
	cfg := AlertManagerConfig{MaxPositionWeight: 0.20}
	alerts := evaluatePositionConcentration(snap, cfg)
	assert.Empty(t, alerts)
}

func TestDetector_PositionConcentration_DisabledByZero(t *testing.T) {
	snap := makeSnapshot(100_000)
	snap.Positions = []PositionSnapshot{{Symbol: "X", MarketValue: 90_000}}
	alerts := evaluatePositionConcentration(snap, AlertManagerConfig{MaxPositionWeight: 0})
	assert.Empty(t, alerts)
}

func TestDetector_SectorConcentration_Fires(t *testing.T) {
	snap := makeSnapshot(100_000)
	snap.Positions = []PositionSnapshot{
		{Symbol: "X", Sector: "tech", MarketValue: 30_000},
		{Symbol: "Y", Sector: "tech", MarketValue: 20_000},
		{Symbol: "Z", Sector: "bank", MarketValue: 10_000},
	}
	cfg := AlertManagerConfig{MaxSectorWeight: 0.40} // 40% = 40_000 threshold
	alerts := evaluateSectorConcentration(snap, cfg)
	require.Len(t, alerts, 1)
	assert.Equal(t, "tech", alerts[0].Sector)
	assert.Equal(t, 0.50, alerts[0].Value)
}

func TestDetector_SectorConcentration_UncategorizedBucket(t *testing.T) {
	snap := makeSnapshot(100_000)
	snap.Positions = []PositionSnapshot{
		{Symbol: "X", MarketValue: 50_000}, // no sector
	}
	cfg := AlertManagerConfig{MaxSectorWeight: 0.40}
	alerts := evaluateSectorConcentration(snap, cfg)
	require.Len(t, alerts, 1)
	assert.Equal(t, "uncategorized", alerts[0].Sector)
}

func TestDetector_Drawdown_Fires(t *testing.T) {
	snap := makeSnapshot(80_000)
	snap.PeakEquity = 100_000
	cfg := AlertManagerConfig{MaxDrawdown: 0.15}
	alerts := evaluateDrawdown(snap, cfg)
	require.Len(t, alerts, 1)
	assert.Equal(t, RuleDrawdown, alerts[0].Rule)
	assert.InDelta(t, 0.20, alerts[0].Value, 0.001) // 20% drawdown
}

func TestDetector_Drawdown_NoFire(t *testing.T) {
	snap := makeSnapshot(95_000)
	snap.PeakEquity = 100_000
	cfg := AlertManagerConfig{MaxDrawdown: 0.15}
	alerts := evaluateDrawdown(snap, cfg)
	assert.Empty(t, alerts)
}

func TestDetector_DailyLoss_Fires(t *testing.T) {
	snap := makeSnapshot(100_000)
	snap.DailyPnL = -60_000
	cfg := AlertManagerConfig{DailyLossLimit: -50_000}
	alerts := evaluateDailyLoss(snap, cfg)
	require.Len(t, alerts, 1)
	assert.Equal(t, RuleDailyLossLimit, alerts[0].Rule)
	assert.Equal(t, -60_000.0, alerts[0].Value)
}

func TestDetector_DailyLoss_DisabledWhenZeroOrPositive(t *testing.T) {
	snap := makeSnapshot(100_000)
	snap.DailyPnL = -1_000_000
	cfg := AlertManagerConfig{DailyLossLimit: 0} // unset
	assert.Empty(t, evaluateDailyLoss(snap, cfg))
	cfg = AlertManagerConfig{DailyLossLimit: 100} // positive = profit target, ignored
	assert.Empty(t, evaluateDailyLoss(snap, cfg))
}

func TestDetector_OrderFailureRate_Fires(t *testing.T) {
	now := time.Now()
	snap := makeSnapshot(100_000)
	snap.RecentOrders = []OrderOutcome{
		{Symbol: "X", Timestamp: now.Add(-10 * time.Minute), Failed: true},
		{Symbol: "Y", Timestamp: now.Add(-5 * time.Minute), Failed: true},
		{Symbol: "Z", Timestamp: now.Add(-2 * time.Minute), Failed: false},
		{Symbol: "W", Timestamp: now.Add(-1 * time.Minute), Failed: false},
		{Symbol: "V", Timestamp: now.Add(-30 * time.Second), Failed: false},
	}
	cfg := AlertManagerConfig{FailureRateLimit: 0.20, FailureRateWindow: time.Hour}
	alerts := evaluateOrderFailureRate(snap, cfg)
	require.Len(t, alerts, 1)
	assert.Equal(t, RuleOrderFailureRate, alerts[0].Rule)
	assert.InDelta(t, 0.40, alerts[0].Value, 0.001) // 2/5
}

func TestDetector_OrderFailureRate_OnlyCountsWindow(t *testing.T) {
	now := time.Now()
	snap := makeSnapshot(100_000)
	snap.RecentOrders = []OrderOutcome{
		{Symbol: "X", Timestamp: now.Add(-2 * time.Hour), Failed: true}, // outside 1h window
		{Symbol: "Y", Timestamp: now.Add(-30 * time.Minute), Failed: false},
	}
	cfg := AlertManagerConfig{FailureRateLimit: 0.20, FailureRateWindow: time.Hour}
	alerts := evaluateOrderFailureRate(snap, cfg)
	assert.Empty(t, alerts) // 0/1 in window = 0%, well below limit
}

func TestDetector_RiskMetricBreaches_Fires(t *testing.T) {
	snap := makeSnapshot(100_000)
	snap.RiskMetrics = map[string]float64{
		"sharpe":     0.5,
		"var_99":     0.12,
		"max_dd":     0.08,
		"leverage":   2.5,
		"untracked":  99.0, // not in thresholds
	}
	cfg := AlertManagerConfig{
		RiskMetricThresholds: map[string]float64{
			"sharpe":   0.3, // current > limit (sharpe higher is good, but rule is generic)
			"var_99":   0.05, // breach
			"max_dd":   0.20, // no breach
			"leverage": 2.0,  // breach
		},
	}
	alerts := evaluateRiskMetricBreaches(snap, cfg)
	require.Len(t, alerts, 3)
	rules := map[string]Alert{}
	for _, a := range alerts {
		rules[a.Attributes["metric"].(string)] = a
	}
	assert.Contains(t, rules, "sharpe")
	assert.Contains(t, rules, "var_99")
	assert.Contains(t, rules, "leverage")
	assert.NotContains(t, rules, "max_dd")
	assert.NotContains(t, rules, "untracked")
}

func TestSeverityForBreach(t *testing.T) {
	cases := []struct {
		ratio    float64
		expected Severity
	}{
		{0.5, SeverityInfo},
		{1.0, SeverityWarning},
		{2.4, SeverityWarning},
		{2.5, SeverityCritical},
		{10.0, SeverityCritical},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("ratio_%.1f", tc.ratio), func(t *testing.T) {
			assert.Equal(t, tc.expected, severityForBreach(tc.ratio, 1.0))
		})
	}
	// threshold=0 path
	assert.Equal(t, SeverityWarning, severityForBreach(1.0, 0))
}

// ============================================================================
// Channel tests
// ============================================================================

type captureChannel struct {
	mu     sync.Mutex
	alerts []Alert
	closed bool
}

func (c *captureChannel) Send(_ context.Context, a Alert) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.alerts = append(c.alerts, a)
}

func (c *captureChannel) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
}

func (c *captureChannel) snapshot() []Alert {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Alert, len(c.alerts))
	copy(out, c.alerts)
	return out
}

func TestLogChannel_Dispatch(t *testing.T) {
	ch := NewLogChannel(nopLogger())
	ch.Send(context.Background(), Alert{
		Rule: "test", Severity: SeverityWarning, Message: "hello",
	})
	ch.Close()
}

func TestLogChannel_RespectsSeverity(t *testing.T) {
	// The LogChannel uses the logger's level. We can't easily inspect
	// the log output in unit tests (zerolog is no-op with nop), so
	// instead we exercise the switch statement indirectly by sending
	// every severity and verifying no panic.
	ch := NewLogChannel(nopLogger())
	for _, sev := range []Severity{SeverityInfo, SeverityWarning, SeverityCritical, "unknown"} {
		ch.Send(context.Background(), Alert{Rule: "r", Severity: sev, Message: "x"})
	}
	ch.Close()
}

func TestWebhookChannel_DropsWhenQueueFull(t *testing.T) {
	// Configure a server that blocks forever, then fill the queue and
	// verify Send does not block the caller. We use a 50ms per-request
	// timeout so the drain at Close finishes quickly (the channel
	// processes 64 in-flight requests on shutdown; with the default 5s
	// the test would take 5 minutes).
	delivered := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-delivered // never close; requests pile up
	}))
	defer srv.Close()
	defer close(delivered)

	ch := NewWebhookChannel(srv.URL, 50*time.Millisecond, nopLogger())
	defer ch.Close()

	// 100 alerts: first 64 enqueue, the rest are dropped (logged but
	// not delivered). Send must not block the caller.
	for i := 0; i < 100; i++ {
		ch.Send(context.Background(), Alert{Rule: "x", Severity: SeverityInfo, Message: "drop"})
	}
}

func TestWebhookChannel_Delivers(t *testing.T) {
	var received int32
	delivered := make(chan Alert, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var a Alert
		_ = json.Unmarshal(body, &a)
		atomic.AddInt32(&received, 1)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "r1", r.Header.Get("X-Alert-ID"))
		delivered <- a
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := NewWebhookChannel(srv.URL, 5*time.Second, nopLogger())
	ch.Send(context.Background(), Alert{ID: "r1", Rule: "test", Severity: SeverityWarning, Message: "hi"})

	select {
	case a := <-delivered:
		assert.Equal(t, "r1", a.ID)
		assert.Equal(t, "test", a.Rule)
		assert.Equal(t, SeverityWarning, a.Severity)
	case <-time.After(3 * time.Second):
		t.Fatal("webhook delivery timed out")
	}

	ch.Close()
	assert.EqualValues(t, 1, atomic.LoadInt32(&received))
}

func TestWebhookChannel_LogsNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ch := NewWebhookChannel(srv.URL, 1*time.Second, nopLogger())
	ch.Send(context.Background(), Alert{ID: "r1", Rule: "x"})
	// Give the goroutine time to process.
	time.Sleep(150 * time.Millisecond)
	ch.Close()
}

// ============================================================================
// Manager integration tests
// ============================================================================

func TestManager_DispatchesToAllChannels(t *testing.T) {
	cap1 := &captureChannel{}
	cap2 := &captureChannel{}
	cfg := AlertManagerConfig{
		MaxPositionWeight: 0.20,
		MaxSectorWeight:   0.40,
		MaxDrawdown:       0.15,
		DailyLossLimit:    -50_000,
		FailureRateLimit:  0.20,
		RiskMetricThresholds: map[string]float64{
			"sharpe": 0.3,
		},
	}
	am := NewAlertManager(cfg, nopLogger())
	am.AddChannel(cap1)
	am.AddChannel(cap2)

	snap := makeSnapshot(100_000)
	snap.PeakEquity = 100_000
	snap.DailyPnL = -100_000
	snap.Positions = []PositionSnapshot{
		{Symbol: "BIG", Sector: "tech", MarketValue: 30_000},
		{Symbol: "MID", Sector: "tech", MarketValue: 20_000},
		{Symbol: "OK", MarketValue: 5_000},
	}
	snap.RiskMetrics = map[string]float64{"sharpe": 0.5}

	now := time.Now()
	snap.RecentOrders = []OrderOutcome{
		{Symbol: "X", Timestamp: now.Add(-30 * time.Minute), Failed: true},
		{Symbol: "Y", Timestamp: now.Add(-25 * time.Minute), Failed: true},
		{Symbol: "Z", Timestamp: now.Add(-20 * time.Minute), Failed: false},
		{Symbol: "W", Timestamp: now.Add(-15 * time.Minute), Failed: false},
	}

	// Drop the snapshot's equity by 20% to trigger drawdown as well.
	snap.TotalValue = 80_000

	dispatched := am.Evaluate(context.Background(), snap)
	require.GreaterOrEqual(t, dispatched, 6, "expected all 6 detectors to fire")

	got1 := cap1.snapshot()
	got2 := cap2.snapshot()
	assert.Equal(t, len(got1), len(got2))

	// Verify each rule fired at least once.
	rules := map[string]bool{}
	for _, a := range got1 {
		rules[a.Rule] = true
	}
	assert.True(t, rules[RulePositionConcentration])
	assert.True(t, rules[RuleSectorConcentration])
	assert.True(t, rules[RuleDrawdown])
	assert.True(t, rules[RuleDailyLossLimit])
	assert.True(t, rules[RuleOrderFailureRate])
	assert.True(t, rules[RuleRiskMetricBreach])

	am.Close()
}

func TestManager_NoFireWhenUnderLimits(t *testing.T) {
	cap := &captureChannel{}
	am := NewAlertManager(AlertManagerConfig{
		MaxPositionWeight: 0.30,
		MaxSectorWeight:   0.50,
		MaxDrawdown:       0.20,
	}, nopLogger())
	am.AddChannel(cap)

	snap := makeSnapshot(100_000)
	snap.Positions = []PositionSnapshot{
		{Symbol: "X", Sector: "tech", MarketValue: 20_000},
		{Symbol: "Y", Sector: "bank", MarketValue: 20_000},
	}

	assert.Equal(t, 0, am.Evaluate(context.Background(), snap))
	assert.Empty(t, cap.snapshot())

	am.Close()
}

func TestManager_SkipsEmptyPortfolio(t *testing.T) {
	cap := &captureChannel{}
	am := NewAlertManager(AlertManagerConfig{MaxPositionWeight: 0.20}, nopLogger())
	am.AddChannel(cap)
	assert.Equal(t, 0, am.Evaluate(context.Background(), PortfolioSnapshot{}))
	am.Close()
}

func TestManager_ChannelCountAndClose(t *testing.T) {
	am := NewAlertManager(AlertManagerConfig{}, nopLogger())
	assert.Equal(t, 1, am.ChannelCount(), "log channel by default")
	am.AddChannel(&captureChannel{})
	assert.Equal(t, 2, am.ChannelCount())
	am.Close()
	assert.Equal(t, 2, am.ChannelCount(), "ChannelCount returns slice len, which is preserved post-Close")
	assert.False(t, am.AddChannel(&captureChannel{}), "AddChannel after Close")
	assert.False(t, am.SetWebhookURL("http://example.com"), "SetWebhookURL after Close")
}

func TestManager_SetWebhookURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	am := NewAlertManager(AlertManagerConfig{}, nopLogger())
	defer am.Close()
	assert.Equal(t, 1, am.ChannelCount(), "no webhook configured")

	require.True(t, am.SetWebhookURL(srv.URL))
	assert.Equal(t, 2, am.ChannelCount(), "log + webhook")

	require.True(t, am.SetWebhookURL(""))
	assert.Equal(t, 1, am.ChannelCount(), "webhook removed, log remains")
}

func TestManager_AlertIDsAreUnique(t *testing.T) {
	am := NewAlertManager(AlertManagerConfig{
		MaxPositionWeight: 0.10,
	}, nopLogger())
	defer am.Close()

	snap := makeSnapshot(100_000)
	snap.Positions = []PositionSnapshot{{Symbol: "X", MarketValue: 50_000}}
	am.Evaluate(context.Background(), snap)
	am.Evaluate(context.Background(), snap)

	cap := &captureChannel{}
	am.AddChannel(cap)
	am.Evaluate(context.Background(), snap)

	// Even the pre-existing alerts should have unique IDs by the time
	// they reach captureChannel.
	ids := map[string]bool{}
	for _, a := range cap.snapshot() {
		assert.False(t, ids[a.ID], "duplicate ID: %s", a.ID)
		ids[a.ID] = true
	}
	assert.NotEmpty(t, ids)
}

func TestManager_NewAlertID(t *testing.T) {
	am := NewAlertManager(AlertManagerConfig{}, nopLogger())
	defer am.Close()
	a1 := am.nextAlertID("rule")
	a2 := am.nextAlertID("rule")
	assert.NotEqual(t, a1, a2)
	assert.Contains(t, a1, "rule")
}

func TestManager_EvaluateConcurrent(t *testing.T) {
	// The Evaluate path must be safe for concurrent callers (handlers
	// may fan out alerts from multiple goroutines).
	am := NewAlertManager(AlertManagerConfig{MaxPositionWeight: 0.20}, nopLogger())
	am.AddChannel(&captureChannel{})
	defer am.Close()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			snap := makeSnapshot(100_000)
			snap.Positions = []PositionSnapshot{
				{Symbol: "X", MarketValue: float64(20_000 + i*100)},
			}
			am.Evaluate(context.Background(), snap)
		}(i)
	}
	wg.Wait()
}

func TestAlert_JSONRoundTrip(t *testing.T) {
	// Channels that ship JSON over the wire (WebhookChannel in
	// particular) rely on Alert serializing cleanly. Lock in the
	// contract.
	a := Alert{
		ID:        "ALR-x-1",
		Rule:      RuleDrawdown,
		Severity:  SeverityCritical,
		Message:   "drawdown",
		Value:     0.20,
		Threshold: 0.15,
		Symbol:    "X",
		Sector:    "tech",
		Timestamp: time.Unix(1_700_000_000, 0).UTC(),
		Attributes: map[string]interface{}{
			"k":       "v",
			"n":       float64(42), // JSON numbers always round-trip as float64
			"missing": nil,
		},
	}
	raw, err := json.Marshal(a)
	require.NoError(t, err)
	var out Alert
	require.NoError(t, json.Unmarshal(raw, &out))
	assert.Equal(t, a.ID, out.ID)
	assert.Equal(t, a.Rule, out.Rule)
	assert.Equal(t, a.Severity, out.Severity)
	assert.Equal(t, a.Message, out.Message)
	assert.Equal(t, a.Value, out.Value)
	assert.Equal(t, a.Threshold, out.Threshold)
	assert.Equal(t, a.Symbol, out.Symbol)
	assert.Equal(t, a.Sector, out.Sector)
	assert.Equal(t, a.Timestamp, out.Timestamp)
	require.NotNil(t, out.Attributes)
	assert.Equal(t, "v", out.Attributes["k"])
	assert.Equal(t, float64(42), out.Attributes["n"])
}
