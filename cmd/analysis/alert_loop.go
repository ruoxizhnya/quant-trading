package main

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/alert"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
)

// PeriodicAlertConfig controls how often the PeriodicAlertLoop
// evaluates the live portfolio against the alert rules, and how many
// recent alerts are retained for the HTTP layer to surface.
//
// Defaults (set by main.go when zero values are loaded):
//
//	Interval:     5 * time.Minute
//	HistoryLimit: 100
//	Enabled:      true
type PeriodicAlertConfig struct {
	// Interval is the cadence at which Evaluate runs. 5 minutes is
	// short enough to catch breach events before they cascade, long
	// enough to keep CPU pressure negligible. < 1s is not supported
	// and will be clamped to 1s.
	Interval time.Duration
	// HistoryLimit is the maximum number of alerts retained in the
	// in-memory ring buffer. When exceeded, the oldest alert is
	// evicted. The buffer is shared between the loop and the HTTP
	// layer; both access it under a single mutex.
	HistoryLimit int
	// Enabled lets operators disable the loop without removing the
	// configuration. The HTTP layer still serves the existing
	// history; new alerts are simply not generated.
	Enabled bool
}

// DefaultPeriodicAlertConfig returns sane production defaults.
func DefaultPeriodicAlertConfig() PeriodicAlertConfig {
	return PeriodicAlertConfig{
		Interval:     5 * time.Minute,
		HistoryLimit: 100,
		Enabled:      true,
	}
}

// AlertHistory is a bounded ring buffer of recent alerts. It is safe
// for concurrent readers (the HTTP layer) and a single writer (the
// PeriodicAlertLoop). Capacity is fixed at construction time.
type AlertHistory struct {
	mu      sync.RWMutex
	entries []alert.Alert
	idx     int  // next write position
	full    bool // whether entries has been filled at least once
	cap     int
}

// NewAlertHistory constructs a history buffer with the given
// capacity. A non-positive capacity is replaced with 1.
func NewAlertHistory(cap int) *AlertHistory {
	if cap <= 0 {
		cap = 1
	}
	return &AlertHistory{
		entries: make([]alert.Alert, cap),
		cap:     cap,
	}
}

// Append inserts a batch of alerts, evicting the oldest entries if
// the buffer is full. The order within the batch is preserved.
func (h *AlertHistory) Append(alerts []alert.Alert) {
	if len(alerts) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, a := range alerts {
		h.entries[h.idx] = a
		h.idx = (h.idx + 1) % h.cap
		if h.idx == 0 {
			h.full = true
		}
	}
}

// Snapshot returns a copy of the stored alerts in newest-first order.
// The returned slice is safe to mutate by the caller; the next call
// to Snapshot will return a fresh copy. Returns an empty slice when
// the buffer has never been written to.
func (h *AlertHistory) Snapshot() []alert.Alert {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.full && h.idx == 0 {
		return []alert.Alert{}
	}
	// Walk in newest-first order.
	n := h.idx
	if h.full {
		n = h.cap
	}
	out := make([]alert.Alert, 0, n)
	for i := 0; i < n; i++ {
		// newest = (idx-1) mod cap, then (idx-2), ...
		pos := (h.idx - 1 - i + h.cap*2) % h.cap
		out = append(out, h.entries[pos])
	}
	return out
}

// FilterBySeverity returns the most recent alerts whose Severity is
// at least `min`. Severities are ordered: info < warning < critical.
// An empty min returns the full snapshot.
func (h *AlertHistory) FilterBySeverity(min alert.Severity) []alert.Alert {
	rank := func(s alert.Severity) int {
		switch s {
		case alert.SeverityCritical:
			return 3
		case alert.SeverityWarning:
			return 2
		case alert.SeverityInfo:
			return 1
		default:
			return 0
		}
	}
	minRank := rank(min)
	snap := h.Snapshot()
	out := make([]alert.Alert, 0, len(snap))
	for _, a := range snap {
		if rank(a.Severity) >= minRank {
			out = append(out, a)
		}
	}
	return out
}

// Len returns the number of stored alerts (0 if never written to).
// Mainly useful for tests and metrics.
func (h *AlertHistory) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.full {
		return h.cap
	}
	return h.idx
}

// DrainAndReset returns every stored alert in newest-first order
// and resets the buffer to an empty state. It is intended for
// operators who want to consume the full history once (e.g. on
// shutdown, or before exporting the snapshot to a database).
//
// Note: Snapshot() remains the right primitive for the read-only
// HTTP path; DrainAndReset is destructive and only used by tests
// or batch consumers.
func (h *AlertHistory) DrainAndReset() []alert.Alert {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Compute the newest-first slice in place. We do NOT call
	// Snapshot() here because that takes the read lock, which
	// would deadlock with our write lock.
	if !h.full && h.idx == 0 {
		// Reset and return empty.
		h.entries = make([]alert.Alert, h.cap)
		return []alert.Alert{}
	}
	n := h.idx
	if h.full {
		n = h.cap
	}
	out := make([]alert.Alert, 0, n)
	for i := 0; i < n; i++ {
		pos := (h.idx - 1 - i + h.cap*2) % h.cap
		out = append(out, h.entries[pos])
	}
	// Reset internal state to a fresh, empty buffer.
	h.entries = make([]alert.Alert, h.cap)
	h.idx = 0
	h.full = false
	return out
}

// PeriodicAlertLoop polls the live portfolio state on a fixed
// interval and dispatches any triggered alerts through the supplied
// AlertManager. Detected alerts are also recorded in History for
// later HTTP inspection.
//
// The loop runs a single goroutine that ticks on cfg.Interval. It
// exits cleanly when ctx is cancelled (drain budget: 5s for the
// in-flight evaluation to finish, but Evaluate itself is sub-second
// when no channels block).
type PeriodicAlertLoop struct {
	cfg     PeriodicAlertConfig
	am      *alert.AlertManager
	trader  live.LiveTrader
	riskMgr *risk.RiskManager
	history *AlertHistory
	logger  zerolog.Logger
}

// NewPeriodicAlertLoop wires the loop to its dependencies. The
// returned struct is ready to Start; the loop does not begin until
// Start is called.
func NewPeriodicAlertLoop(
	cfg PeriodicAlertConfig,
	am *alert.AlertManager,
	trader live.LiveTrader,
	riskMgr *risk.RiskManager,
	history *AlertHistory,
	logger zerolog.Logger,
) *PeriodicAlertLoop {
	if cfg.Interval < time.Second {
		cfg.Interval = time.Second
	}
	if cfg.HistoryLimit <= 0 {
		cfg.HistoryLimit = 100
	}
	return &PeriodicAlertLoop{
		cfg:     cfg,
		am:      am,
		trader:  trader,
		riskMgr: riskMgr,
		history: history,
		logger:  logger.With().Str("component", "alert_loop").Logger(),
	}
}

// History returns the alert history buffer. Callers (the HTTP
// layer) read it under the buffer's own mutex; concurrent reads
// with loop-driven writes are safe.
func (l *PeriodicAlertLoop) History() *AlertHistory {
	return l.history
}

// AlertManager returns the underlying AlertManager so the HTTP
// layer can expose configuration / add custom channels at runtime.
func (l *PeriodicAlertLoop) AlertManager() *alert.AlertManager {
	return l.am
}

// Start begins the periodic evaluation. It blocks until ctx is
// cancelled. The first evaluation runs after the configured
// interval (NOT immediately) so that startup-time configuration
// changes have a chance to take effect.
//
// To force an immediate evaluation, call TriggerOnce directly.
func (l *PeriodicAlertLoop) Start(ctx context.Context) {
	if !l.cfg.Enabled {
		l.logger.Info().Msg("PeriodicAlertLoop disabled by config; not starting")
		return
	}
	l.logger.Info().
		Dur("interval", l.cfg.Interval).
		Int("history_limit", l.cfg.HistoryLimit).
		Msg("PeriodicAlertLoop starting")

	ticker := time.NewTicker(l.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.logger.Info().Msg("PeriodicAlertLoop stopping (context cancelled)")
			return
		case <-ticker.C:
			if _, err := l.TriggerOnce(ctx); err != nil {
				l.logger.Warn().Err(err).Msg("PeriodicAlertLoop evaluation failed")
			}
		}
	}
}

// TriggerOnce runs a single evaluation cycle synchronously. It is
// exposed so the HTTP layer can offer a "force-check" endpoint, and
// so tests can drive the loop without a ticker.
//
// Returns the number of alerts dispatched, plus any error from
// reading the snapshot. The error is non-nil only when the trader
// or risk manager fails; the alert evaluation itself is pure and
// infallible.
func (l *PeriodicAlertLoop) TriggerOnce(ctx context.Context) (int, error) {
	snap, err := l.buildSnapshot(ctx)
	if err != nil {
		return 0, err
	}
	n := l.am.Evaluate(ctx, snap)
	// Record alerts in history. Evaluate dispatches synchronously to
	// the registered channels, but does not return the alerts it
	// produced; we therefore reconstruct the list from the manager
	// by re-evaluating. To avoid double-dispatch, we instead track
	// alerts via a small hook: the manager accepts custom channels,
	// and we install an in-process recorder channel for this purpose.
	//
	// However, to keep this method cheap we just snapshot whatever
	// the channels recorded this cycle by re-running evaluate against
	// the same snapshot and reading the channel's internal buffer.
	// For now, we accept the simpler contract: TriggerOnce returns
	// the count, and operators query the history via the HTTP API.
	//
	// (Future improvement: have Evaluate return the alerts slice
	// alongside the count. Until then, callers can call Snapshot()
	// on history to see what was dispatched in the last cycle.)
	if n > 0 {
		// The recorder channel is added once at construction time in
		// main.go; we just need to drain it into the history here.
		if rec, ok := l.am.Recorder(); ok {
			l.history.Append(rec.DrainAndReset())
		}
	}
	return n, nil
}

// buildSnapshot assembles a PortfolioSnapshot from the live trader
// and risk manager. Failures to read the trader (e.g. transient
// error from the data provider) propagate up; failure to read the
// risk manager falls back to an empty metrics map so the detectors
// that don't depend on it still run.
//
// Note: PositionInfo in pkg/live does not yet expose a Sector
// field, so the SectorConcentrationDetector will bucket every
// position into "uncategorized" until that field is added (P2-3
// or later). This is acceptable: the operator can still set a
// MaxSectorWeight to catch the pathological case where every
// holding ends up in the same bucket.
func (l *PeriodicAlertLoop) buildSnapshot(ctx context.Context) (alert.PortfolioSnapshot, error) {
	var snap alert.PortfolioSnapshot

	if l.trader != nil {
		positions, err := l.trader.GetPositions(ctx)
		if err != nil {
			return snap, err
		}
		account, err := l.trader.GetAccount(ctx)
		if err != nil {
			return snap, err
		}
		snap.Positions = make([]alert.PositionSnapshot, 0, len(positions))
		for _, p := range positions {
			snap.Positions = append(snap.Positions, alert.PositionSnapshot{
				Symbol:        p.Symbol,
				Quantity:      p.Quantity,
				AvgCost:       p.AvgCost,
				CurrentPrice:  p.CurrentPrice,
				MarketValue:   p.MarketValue,
				UnrealizedPnL: p.UnrealizedPnL,
				// Sector intentionally empty until pkg/live.PositionInfo
				// gains a Sector field.
			})
		}
		snap.TotalValue = account.TotalAssets
		snap.Cash = account.Cash
		// DailyPnL = unrealized + realized (realized not tracked here).
		// For now, we use UnrealizedPnL as a proxy and rely on the
		// DailyLossLimit being a generous floor (e.g. -50_000) so
		// that day-to-day unrealized swings don't generate false
		// alerts. When realized-PnL tracking lands, swap this for
		// the realized value.
		snap.DailyPnL = account.UnrealizedPnL
	}

	// Risk metrics: best-effort, fall back to empty map on failure.
	snap.RiskMetrics = map[string]float64{}
	if l.riskMgr != nil {
		// Read whatever the risk manager exposes. Each call is
		// individually nil-safe; the helpers return zero values
		// when the underlying data is not yet available.
		cfg := l.riskMgr.GetConfig()
		snap.RiskMetrics["target_volatility"] = cfg.TargetVolatility
	}

	// Peak equity is not yet tracked by the live trader; the
	// downstream drawdown detector will simply not fire until the
	// runner populates this field. We leave it at zero so the
	// detector's PeakEquity > 0 guard kicks in correctly.
	snap.PeakEquity = 0

	return snap, nil
}
