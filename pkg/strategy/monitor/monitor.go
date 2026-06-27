// Package monitor provides rolling performance monitoring for deployed
// trading strategies. The StrategyMonitor tracks daily returns and
// equity curves, computes rolling Sharpe ratio and max drawdown, and
// emits alerts when performance degrades beyond configured thresholds.
//
// P1-E: Strategy monitoring end-to-end pipeline.
//
// The monitor is intentionally self-contained: it does not depend on
// the alert manager or the drift detector at construction time. Drift
// integration is opt-in via SetDriftDetector; the alert manager can
// consume StrategyAlert values through its own channel wiring.
package monitor

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/drift"
	"github.com/ruoxizhnya/quant-trading/pkg/statistics"
)

// Default threshold and window values. Operators override these by
// passing a custom AlertThresholds to NewStrategyMonitor.
const (
	// DefaultWindowSize is the rolling window length in trading days.
	// 60 days ≈ one quarter: long enough to smooth daily noise, short
	// enough to react to regime changes within a season.
	DefaultWindowSize = 60

	// DefaultMinSharpe is the floor for the annualized rolling Sharpe
	// ratio. Below this, a Warning alert fires.
	DefaultMinSharpe = 0.5

	// DefaultMaxDrawdown is the ceiling for rolling max drawdown
	// (a positive fraction; 0.15 = 15%). Above this, a Critical alert
	// fires.
	DefaultMaxDrawdown = 0.15

	// DefaultMinWinRate is the floor for the rolling win rate. Not
	// enforced by CheckStatus; reserved for future rule additions.
	DefaultMinWinRate = 0.4

	// DefaultConsecutiveLossLimit is the maximum number of consecutive
	// losing days before a Critical alert fires.
	DefaultConsecutiveLossLimit = 5

	// TradingDaysPerYear annualizes daily-return Sharpe via sqrt(N).
	TradingDaysPerYear = 252

	// MinSamplesForSharpe is the minimum returns count required before
	// the rolling Sharpe ratio is treated as meaningful. Below this,
	// the Sharpe check is skipped (other checks still run).
	MinSamplesForSharpe = 5
)

// AlertType identifies the kind of strategy performance alert.
type AlertType string

const (
	AlertSharpeDegradation AlertType = "sharpe_degradation"
	AlertDrawdownBreach    AlertType = "drawdown_breach"
	AlertConsecutiveLoss   AlertType = "consecutive_loss"
	AlertDriftDetected     AlertType = "drift_detected"
)

// Severity ranks the urgency of a strategy alert.
type Severity string

const (
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// StrategyStatus tracks the operational state of a monitored strategy.
// CheckStatus uses this to suppress repeated alerts once a strategy has
// been halted by the operator.
type StrategyStatus int

const (
	StatusActive StrategyStatus = iota
	StatusWarning
	StatusStopped
)

// String returns a lowercase human-readable status name.
func (s StrategyStatus) String() string {
	switch s {
	case StatusActive:
		return "active"
	case StatusWarning:
		return "warning"
	case StatusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// AlertThresholds configures when CheckStatus emits alerts. A zero
// value for any field disables that specific check. Use
// DefaultAlertThresholds to obtain the standard configuration.
type AlertThresholds struct {
	MinSharpe            float64 // floor for rolling Sharpe (e.g. 0.5); 0 disables
	MaxDrawdown          float64 // ceiling for rolling max drawdown (e.g. 0.15); 0 disables
	MinWinRate           float64 // floor for rolling win rate (e.g. 0.4); 0 disables (reserved)
	ConsecutiveLossLimit int     // max consecutive losing days (e.g. 5); 0 disables
}

// DefaultAlertThresholds returns the standard threshold configuration.
func DefaultAlertThresholds() AlertThresholds {
	return AlertThresholds{
		MinSharpe:            DefaultMinSharpe,
		MaxDrawdown:          DefaultMaxDrawdown,
		MinWinRate:           DefaultMinWinRate,
		ConsecutiveLossLimit: DefaultConsecutiveLossLimit,
	}
}

// StrategyState tracks a deployed strategy's rolling metrics. Fields
// are mutated only under the owning StrategyMonitor's mutex.
type StrategyState struct {
	Name              string          `json:"name"`
	Returns           []float64       `json:"returns"`
	Equities          []float64       `json:"equities"`
	LastUpdate        time.Time       `json:"last_update"`
	RollingSharpe     float64         `json:"rolling_sharpe"`
	RollingMaxDD      float64         `json:"rolling_max_dd"`
	ConsecutiveLosses int             `json:"consecutive_losses"`
	Wins              int             `json:"wins"`
	TotalTrades       int             `json:"total_trades"`
	Status            StrategyStatus `json:"status"`
	PeakEquity        float64         `json:"peak_equity"`
}

// StrategyAlert is the value type produced by CheckStatus.
type StrategyAlert struct {
	StrategyName string    `json:"strategy_name"`
	Type         AlertType `json:"type"`
	Severity     Severity  `json:"severity"`
	Message      string    `json:"message"`
	Value        float64   `json:"value"`
	Threshold    float64   `json:"threshold"`
	Timestamp    time.Time `json:"timestamp"`
}

// StrategyMonitor monitors deployed strategies for performance
// degradation. It is safe for concurrent use by multiple goroutines:
// all public methods take the monitor's RWMutex.
type StrategyMonitor struct {
	windowSize    int
	thresholds    AlertThresholds
	logger        zerolog.Logger
	mu            sync.RWMutex
	strategies    map[string]*StrategyState
	driftDetector *drift.Detector
}

// NewStrategyMonitor creates a new monitor with the supplied thresholds
// and logger. The rolling window defaults to DefaultWindowSize (60);
// call SetWindowSize to override.
func NewStrategyMonitor(thresholds AlertThresholds, logger zerolog.Logger) *StrategyMonitor {
	return &StrategyMonitor{
		windowSize: DefaultWindowSize,
		thresholds: thresholds,
		logger:     logger.With().Str("component", "strategy_monitor").Logger(),
		strategies: make(map[string]*StrategyState),
	}
}

// SetWindowSize updates the rolling window length. Existing strategies
// are trimmed on their next Update call. Must be > 0.
func (m *StrategyMonitor) SetWindowSize(size int) {
	if size <= 0 {
		return
	}
	m.mu.Lock()
	m.windowSize = size
	m.mu.Unlock()
}

// SetThresholds updates the alert thresholds at runtime.
func (m *StrategyMonitor) SetThresholds(thresholds AlertThresholds) {
	m.mu.Lock()
	m.thresholds = thresholds
	m.mu.Unlock()
}

// SetDriftDetector installs an optional drift detector. When set,
// CheckStatus runs drift detection on each active strategy's rolling
// returns and emits a DriftDetected alert when adverse drift is found.
// Pass nil to disable drift integration.
func (m *StrategyMonitor) SetDriftDetector(d *drift.Detector) {
	m.mu.Lock()
	m.driftDetector = d
	m.mu.Unlock()
}

// Register adds a strategy to the monitor. Idempotent: re-registering
// an existing name is a no-op (the prior state is preserved).
func (m *StrategyMonitor) Register(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.strategies[name]; exists {
		return
	}
	m.strategies[name] = &StrategyState{
		Name:   name,
		Status: StatusActive,
	}
	m.logger.Info().Str("strategy", name).Msg("strategy registered for monitoring")
}

// Update records a daily return for a strategy and recomputes rolling
// metrics. Returns an error if the strategy is not registered.
//
// dailyReturn is the fractional return for the day (0.01 = +1%).
// equity is the strategy's ending equity (or NAV) for the day; the
// rolling max drawdown is computed over the equity slice.
func (m *StrategyMonitor) Update(name string, dailyReturn float64, equity float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.strategies[name]
	if !ok {
		return fmt.Errorf("strategy %q not registered", name)
	}

	// Append return and trim to the rolling window. We allocate a
	// fresh slice on overflow to avoid unbounded backing-array growth
	// over long monitoring periods.
	s.Returns = append(s.Returns, dailyReturn)
	if len(s.Returns) > m.windowSize {
		trimmed := make([]float64, m.windowSize)
		copy(trimmed, s.Returns[len(s.Returns)-m.windowSize:])
		s.Returns = trimmed
	}

	// Append equity (drives rolling max drawdown).
	s.Equities = append(s.Equities, equity)
	if len(s.Equities) > m.windowSize {
		trimmed := make([]float64, m.windowSize)
		copy(trimmed, s.Equities[len(s.Equities)-m.windowSize:])
		s.Equities = trimmed
	}

	// Track all-time peak equity. RollingMaxDD uses only the windowed
	// slice so the alert reflects the current regime, but PeakEquity
	// is exposed for operators who want the absolute drawdown view.
	if equity > s.PeakEquity {
		s.PeakEquity = equity
	}

	// Consecutive loss counter: strictly negative = loss, >= 0 resets.
	if dailyReturn < 0 {
		s.ConsecutiveLosses++
	} else {
		s.ConsecutiveLosses = 0
	}

	// Win/loss tally (0 return counts as neither; total still increments).
	s.TotalTrades++
	if dailyReturn > 0 {
		s.Wins++
	}

	// Recompute rolling metrics eagerly; CheckStatus stays read-only.
	s.RollingSharpe = computeRollingSharpe(s.Returns)
	s.RollingMaxDD = computeMaxDrawdown(s.Equities)
	s.LastUpdate = time.Now()

	return nil
}

// CheckStatus evaluates all active strategies and returns alerts for
// threshold breaches. Stopped strategies are skipped (alerts suppressed
// after Stop). Strategies with no recorded returns are also skipped.
//
// Alert rules (a zero threshold disables the corresponding check):
//   - RollingSharpe < MinSharpe                  → Warning (needs ≥ MinSamplesForSharpe returns)
//   - RollingMaxDD  > MaxDrawdown                → Critical (needs ≥ 2 equities)
//   - ConsecutiveLosses >= ConsecutiveLossLimit  → Critical
//   - drift detector reports adverse drift      → Warning (or Critical when severity is "high")
//
// Side effects: an Active strategy that triggers any alert is
// transitioned to Warning. The monitor never auto-reverts from Warning
// to Active; that requires an explicit Restart call.
func (m *StrategyMonitor) CheckStatus() []StrategyAlert {
	m.mu.Lock()
	defer m.mu.Unlock()

	alerts := make([]StrategyAlert, 0)
	now := time.Now()

	for name, s := range m.strategies {
		// Stopped strategies: suppress alert generation entirely.
		if s.Status == StatusStopped {
			continue
		}
		// Nothing to evaluate yet.
		if len(s.Returns) == 0 {
			continue
		}

		hadAlert := false

		// 1. Sharpe degradation (Warning). Needs enough samples for a
		// stable standard deviation.
		if len(s.Returns) >= MinSamplesForSharpe &&
			m.thresholds.MinSharpe > 0 &&
			s.RollingSharpe < m.thresholds.MinSharpe {
			alerts = append(alerts, StrategyAlert{
				StrategyName: name,
				Type:         AlertSharpeDegradation,
				Severity:     SeverityWarning,
				Message: fmt.Sprintf(
					"rolling Sharpe %.4f below threshold %.4f",
					s.RollingSharpe, m.thresholds.MinSharpe,
				),
				Value:     s.RollingSharpe,
				Threshold: m.thresholds.MinSharpe,
				Timestamp: now,
			})
			hadAlert = true
		}

		// 2. Drawdown breach (Critical). Needs at least 2 equity
		// points to define a peak-to-trough decline.
		if len(s.Equities) >= 2 &&
			m.thresholds.MaxDrawdown > 0 &&
			s.RollingMaxDD > m.thresholds.MaxDrawdown {
			alerts = append(alerts, StrategyAlert{
				StrategyName: name,
				Type:         AlertDrawdownBreach,
				Severity:     SeverityCritical,
				Message: fmt.Sprintf(
					"rolling max drawdown %.2f%% exceeds threshold %.2f%%",
					s.RollingMaxDD*100, m.thresholds.MaxDrawdown*100,
				),
				Value:     s.RollingMaxDD,
				Threshold: m.thresholds.MaxDrawdown,
				Timestamp: now,
			})
			hadAlert = true
		}

		// 3. Consecutive losses (Critical).
		if m.thresholds.ConsecutiveLossLimit > 0 &&
			s.ConsecutiveLosses >= m.thresholds.ConsecutiveLossLimit {
			alerts = append(alerts, StrategyAlert{
				StrategyName: name,
				Type:         AlertConsecutiveLoss,
				Severity:     SeverityCritical,
				Message: fmt.Sprintf(
					"consecutive loss days %d >= limit %d",
					s.ConsecutiveLosses, m.thresholds.ConsecutiveLossLimit,
				),
				Value:     float64(s.ConsecutiveLosses),
				Threshold: float64(m.thresholds.ConsecutiveLossLimit),
				Timestamp: now,
			})
			hadAlert = true
		}

		// 4. Optional drift detection. The drift detector splits the
		// returns into reference / current windows and flags shifts;
		// we surface only adverse drift (degradation, variance change,
		// distribution shift), never "improvement".
		if m.driftDetector != nil {
			if results, err := m.driftDetector.DetectAll(s.Returns); err == nil {
				for _, r := range results {
					if !r.DriftDetected || r.DriftType == "improvement" {
						continue
					}
					alerts = append(alerts, StrategyAlert{
						StrategyName: name,
						Type:         AlertDriftDetected,
						Severity:     driftSeverityToMonitorSeverity(r.Severity),
						Message:      r.Message,
						Value:        r.Statistic,
						Threshold:    0,
						Timestamp:    now,
					})
					hadAlert = true
					break // one drift alert per strategy per check
				}
			}
		}

		// Escalate Active → Warning on any alert. We deliberately do
		// not auto-escalate to Stopped; halting a strategy is an
		// operator decision (see Stop / Restart).
		if hadAlert && s.Status == StatusActive {
			s.Status = StatusWarning
		}
	}

	return alerts
}

// GetState returns a deep copy of the current state for a strategy.
// The returned pointer is safe for the caller to mutate without
// affecting the monitor's internal state. Returns an error if the
// strategy is not registered.
func (m *StrategyMonitor) GetState(name string) (*StrategyState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.strategies[name]
	if !ok {
		return nil, fmt.Errorf("strategy %q not registered", name)
	}
	return s.copy(), nil
}

// ListStrategies returns the names of all monitored strategies in
// lexicographic order.
func (m *StrategyMonitor) ListStrategies() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.strategies))
	for name := range m.strategies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Stop marks a strategy as stopped. CheckStatus suppresses alerts for
// stopped strategies. The transition is logged once. Idempotent:
// calling Stop on an already-stopped strategy is a no-op.
func (m *StrategyMonitor) Stop(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.strategies[name]
	if !ok {
		return fmt.Errorf("strategy %q not registered", name)
	}
	if s.Status == StatusStopped {
		return nil
	}
	prev := s.Status
	s.Status = StatusStopped
	m.logger.Info().
		Str("strategy", name).
		Str("prev_status", prev.String()).
		Msg("strategy monitoring stopped; alerts suppressed")
	return nil
}

// Restart resumes monitoring for a stopped strategy, resetting its
// status to Active. Returns an error if not registered.
func (m *StrategyMonitor) Restart(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.strategies[name]
	if !ok {
		return fmt.Errorf("strategy %q not registered", name)
	}
	s.Status = StatusActive
	m.logger.Info().Str("strategy", name).Msg("strategy monitoring resumed")
	return nil
}

// copy returns a deep copy of the state, safe for external mutation.
func (s *StrategyState) copy() *StrategyState {
	cp := *s
	if s.Returns != nil {
		cp.Returns = make([]float64, len(s.Returns))
		copy(cp.Returns, s.Returns)
	}
	if s.Equities != nil {
		cp.Equities = make([]float64, len(s.Equities))
		copy(cp.Equities, s.Equities)
	}
	return &cp
}

// computeRollingSharpe calculates the annualized rolling Sharpe ratio:
//
//	Sharpe = mean(returns) * sqrt(252) / sample_std(returns)
//
// Returns 0 when there are fewer than 2 samples or when std is 0
// (constant returns — Sharpe is undefined; we return 0 rather than
// ±Inf to keep alerting well-defined).
func computeRollingSharpe(returns []float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	mean := statistics.Mean(returns)
	std := statistics.SampleStdDev(returns)
	if std == 0 {
		return 0
	}
	return mean * math.Sqrt(TradingDaysPerYear) / std
}

// computeMaxDrawdown returns the maximum peak-to-trough drawdown over
// the equity curve, as a positive fraction (0.15 = 15% decline).
// Returns 0 for an empty or monotonically non-decreasing curve.
// A non-positive peak is treated as no-drawdown for that step to
// avoid divide-by-zero on degenerate equity paths.
func computeMaxDrawdown(equities []float64) float64 {
	if len(equities) == 0 {
		return 0
	}
	peak := equities[0]
	maxDD := 0.0
	for _, e := range equities {
		if e > peak {
			peak = e
		}
		if peak > 0 {
			dd := (peak - e) / peak
			if dd > maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}

// driftSeverityToMonitorSeverity maps the drift detector's string
// severity ("low" / "medium" / "high") to the monitor's Severity type.
// "high" escalates to Critical; anything else is Warning.
func driftSeverityToMonitorSeverity(s string) Severity {
	if s == "high" {
		return SeverityCritical
	}
	return SeverityWarning
}
