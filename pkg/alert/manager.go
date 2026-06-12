// Package alert provides a centralized alerting framework for the
// quant-trading platform. The AlertManager evaluates a portfolio snapshot
// against a set of rule-based detectors and dispatches triggered alerts
// to one or more channels (log, webhook).
//
// Architecture (Sprint 6 P1-29):
//
//	AlertManager
//	  ├── Detectors (6 built-in, rule-based)
//	  │   ├── PositionConcentrationDetector    single position > X% of portfolio
//	  │   ├── SectorConcentrationDetector      sector exposure > Y% of portfolio
//	  │   ├── DrawdownDetector                 drawdown > Z% from peak
//	  │   ├── DailyLossLimitDetector           daily realized P&L < limit
//	  │   ├── OrderFailureRateDetector         order failure rate spike
//	  │   └── RiskMetricBreachDetector         any risk metric crosses threshold
//	  └── Channels (pluggable sinks)
//	      ├── LogChannel                        always-on (zerolog)
//	      └── WebhookChannel                    HTTP POST to configured URL
//
// Usage:
//
//	cfg := alert.AlertManagerConfig{
//	    MaxPositionWeight:  0.20,
//	    MaxSectorWeight:    0.40,
//	    MaxDrawdown:        0.15,
//	    DailyLossLimit:    -50000,
//	    FailureRateLimit:   0.10,
//	    WebhookURL:         "https://hooks.example.com/quant",
//	}
//	am := alert.NewAlertManager(cfg, logger)
//	am.Evaluate(ctx, snapshot)
//	am.WebhookURL = ""  // disable webhook
//	am.Close()
//
// Snapshot is a value type; the manager does not retain references to
// the caller's slices. This makes it safe to call Evaluate concurrently
// with the same manager instance as long as the underlying channels'
// Send methods are safe for concurrent use.
package alert

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Severity ranks the urgency of a triggered alert. Channel implementations
// use Severity to decide routing, throttling, and UI styling.
type Severity string

const (
	SeverityInfo     Severity = "info"     // informational only, no action
	SeverityWarning  Severity = "warning"  // attention required
	SeverityCritical Severity = "critical" // immediate action required
)

// Alert is the value type produced by detectors and consumed by channels.
type Alert struct {
	ID         string                 `json:"id"`
	Rule       string                 `json:"rule"`
	Severity   Severity               `json:"severity"`
	Message    string                 `json:"message"`
	Value      float64                `json:"value"`
	Threshold  float64                `json:"threshold"`
	Symbol     string                 `json:"symbol,omitempty"`   // for single-symbol rules
	Sector     string                 `json:"sector,omitempty"`   // for sector rules
	Timestamp  time.Time              `json:"timestamp"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// PortfolioSnapshot is the input contract for AlertManager.Evaluate.
// All fields are read-only; the manager does not mutate the caller's data.
type PortfolioSnapshot struct {
	// TotalValue is the current portfolio market value (cash + market value).
	TotalValue float64
	// Cash is the current cash balance.
	Cash float64
	// Positions is the live position list keyed by symbol.
	Positions []PositionSnapshot
	// DailyPnL is today's realized + unrealized P&L.
	DailyPnL float64
	// PeakEquity is the running all-time-high portfolio value, used by
	// the DrawdownDetector to compute drawdown.
	PeakEquity float64
	// RecentOrders is the trailing window of orders for failure-rate
	// evaluation. Order timestamps are read for windowing.
	RecentOrders []OrderOutcome
	// RiskMetrics is a free-form bag of named risk metrics that the
	// RiskMetricBreachDetector compares against Thresholds.
	// Names must match between calls for the breach detector to compare
	// meaningful historical values.
	RiskMetrics map[string]float64
}

// PositionSnapshot is a single position within PortfolioSnapshot.
type PositionSnapshot struct {
	Symbol   string
	Sector   string  // sector classification (empty = uncategorized)
	Quantity float64
	AvgCost  float64
	CurrentPrice float64
	MarketValue  float64
	UnrealizedPnL float64
}

// OrderOutcome is the per-order record consumed by OrderFailureRateDetector.
type OrderOutcome struct {
	Symbol    string
	Timestamp time.Time
	Failed    bool
}

// AlertManagerConfig is the static configuration for AlertManager.
type AlertManagerConfig struct {
	// MaxPositionWeight is the upper bound on a single position's share of
	// the portfolio (0.20 = 20%). Used by PositionConcentrationDetector.
	MaxPositionWeight float64
	// MaxSectorWeight is the upper bound on a single sector's aggregate
	// exposure (0.40 = 40%). Used by SectorConcentrationDetector.
	MaxSectorWeight float64
	// MaxDrawdown is the upper bound on peak-to-trough drawdown (0.15 = 15%).
	// Used by DrawdownDetector.
	MaxDrawdown float64
	// DailyLossLimit is the lower bound on acceptable daily P&L (negative
	// number, e.g. -50000). Used by DailyLossLimitDetector.
	DailyLossLimit float64
	// FailureRateLimit is the upper bound on the order failure rate over
	// the trailing window (0.10 = 10%). Used by OrderFailureRateDetector.
	FailureRateLimit float64
	// FailureRateWindow is the trailing duration over which failures are
	// counted. Defaults to 1h when zero.
	FailureRateWindow time.Duration
	// RiskMetricThresholds is the rule-set for RiskMetricBreachDetector.
	// Each key is a metric name (matched against PortfolioSnapshot.RiskMetrics);
	// each value is the maximum acceptable value for that metric.
	RiskMetricThresholds map[string]float64
	// WebhookURL is the destination for the WebhookChannel. Empty = channel
	// disabled.
	WebhookURL string
	// WebhookTimeout is the per-call timeout for webhook delivery. Defaults
	// to 5s when zero.
	WebhookTimeout time.Duration
}

// AlertManager is the central rule evaluator and channel dispatcher.
//
// Concurrent safety: Evaluate is safe to call from multiple goroutines.
// Send/Close are safe to call concurrently with Evaluate. Setting
// WebhookURL after construction is NOT safe; configure the URL via
// AlertManagerConfig or call SetWebhookURL while holding no other
// operations.
type AlertManager struct {
	cfg     AlertManagerConfig
	logger  zerolog.Logger
	mu      sync.RWMutex
	closed  bool

	// channels is the list of registered sinks. Iteration is read-only
	// after construction; a copy is taken under mu in Evaluate.
	channels []Channel

	// alertSeq is a monotonic counter for Alert.ID generation.
	alertSeq uint64
}

// NewAlertManager constructs an AlertManager with the standard channel set
// (log + optional webhook). To register custom channels, call AddChannel
// after construction.
func NewAlertManager(cfg AlertManagerConfig, logger zerolog.Logger) *AlertManager {
	channels := []Channel{NewLogChannel(logger)}

	if cfg.WebhookURL != "" {
		timeout := cfg.WebhookTimeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		channels = append(channels, NewWebhookChannel(cfg.WebhookURL, timeout, logger))
	}

	return &AlertManager{
		cfg:      cfg,
		logger:   logger.With().Str("component", "alert_manager").Logger(),
		channels: channels,
	}
}

// AddChannel registers an additional channel at runtime. Use this to plug
// in custom sinks (Slack, PagerDuty, in-process queue) without modifying
// the package. Returns false if the manager is already closed.
func (am *AlertManager) AddChannel(ch Channel) bool {
	am.mu.Lock()
	defer am.mu.Unlock()
	if am.closed {
		return false
	}
	am.channels = append(am.channels, ch)
	return true
}

// SetWebhookURL swaps the webhook destination at runtime. The previous
// WebhookChannel (if any) is replaced; a fresh one is constructed when
// the new URL is non-empty. Returns false if the manager is already closed.
func (am *AlertManager) SetWebhookURL(url string) bool {
	am.mu.Lock()
	defer am.mu.Unlock()
	if am.closed {
		return false
	}
	am.cfg.WebhookURL = url
	// Rebuild channels: keep the log channel, swap webhook.
	next := make([]Channel, 0, len(am.channels))
	for _, ch := range am.channels {
		if _, isWebhook := ch.(*WebhookChannel); isWebhook {
			continue
		}
		next = append(next, ch)
	}
	if url != "" {
		timeout := am.cfg.WebhookTimeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		next = append(next, NewWebhookChannel(url, timeout, am.logger))
	}
	am.channels = next
	return true
}

// Evaluate runs the 6 built-in detectors against the snapshot and
// dispatches any triggered alerts to all registered channels.
//
// Detectors are pure (no I/O); only channel dispatch is async / best-effort.
// Evaluate returns the number of alerts dispatched; callers can use this
// for metrics or as a heartbeat signal.
func (am *AlertManager) Evaluate(ctx context.Context, snap PortfolioSnapshot) int {
	if snap.TotalValue <= 0 {
		// No portfolio yet — nothing to evaluate. Log at debug so empty
		// startups don't spam the operator.
		am.logger.Debug().Msg("AlertManager.Evaluate skipped: total value <= 0")
		return 0
	}

	alerts := make([]Alert, 0, 6)
	alerts = append(alerts, evaluatePositionConcentration(snap, am.cfg)...)
	alerts = append(alerts, evaluateSectorConcentration(snap, am.cfg)...)
	alerts = append(alerts, evaluateDrawdown(snap, am.cfg)...)
	alerts = append(alerts, evaluateDailyLoss(snap, am.cfg)...)
	alerts = append(alerts, evaluateOrderFailureRate(snap, am.cfg)...)
	alerts = append(alerts, evaluateRiskMetricBreaches(snap, am.cfg)...)

	// Stamp timestamps and sequence IDs.
	now := time.Now()
	for i := range alerts {
		alerts[i].Timestamp = now
		alerts[i].ID = am.nextAlertID(alerts[i].Rule)
	}

	// Snapshot channel list under read lock to avoid races with AddChannel.
	am.mu.RLock()
	channels := make([]Channel, len(am.channels))
	copy(channels, am.channels)
	am.mu.RUnlock()

	for _, ch := range channels {
		for i := range alerts {
			ch.Send(ctx, alerts[i])
		}
	}

	if n := len(alerts); n > 0 {
		am.logger.Info().Int("count", n).Msg("AlertManager dispatched alerts")
	}
	return len(alerts)
}

// Close shuts down all channels and marks the manager as closed. After
// Close, Evaluate is a no-op and AddChannel/SetWebhookURL return false.
func (am *AlertManager) Close() {
	am.mu.Lock()
	defer am.mu.Unlock()
	if am.closed {
		return
	}
	am.closed = true
	for _, ch := range am.channels {
		ch.Close()
	}
}

// ChannelCount returns the number of registered channels. Useful for tests
// and for the metrics endpoint.
func (am *AlertManager) ChannelCount() int {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return len(am.channels)
}

func (am *AlertManager) nextAlertID(rule string) string {
	am.mu.Lock()
	am.alertSeq++
	seq := am.alertSeq
	am.mu.Unlock()
	return fmt.Sprintf("ALR-%s-%d-%d", rule, time.Now().Unix(), seq)
}
