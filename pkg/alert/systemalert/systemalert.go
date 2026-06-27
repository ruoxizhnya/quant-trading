// Package systemalert provides system-level operational alerting for the
// quant-trading platform. Unlike the parent pkg/alert package (which
// evaluates portfolio-risk rules against a PortfolioSnapshot and dispatches
// risk Alerts), this package handles operational alerts such as data-sync
// stalls, backtest failures, strategy degradation, and system errors.
//
// An AlertManager routes Alert values to one or more AlertChannel sinks
// (LogChannel is always available; WebhookChannel is optional). Rules
// keyed by AlertType enforce per-rule cooldowns so the same condition
// cannot spam channels within its Cooldown window.
//
// The package is intentionally storage-free: history is retained in
// memory (bounded) for observability via GetHistory. Callers wanting
// durable history should attach a custom AlertChannel that persists.
//
// All public operations are safe for concurrent use.
package systemalert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Severity ranks the urgency of an operational alert. Channel
// implementations use Severity to decide routing, throttling, and
// log level.
type Severity string

const (
	SeverityInfo     Severity = "info"     // informational only, no action
	SeverityWarning  Severity = "warning"  // attention required
	SeverityCritical Severity = "critical" // immediate action required
)

// AlertType categorizes the operational domain of an alert.
type AlertType string

const (
	// AlertDataSyncStall fires when the data-sync pipeline stalls
	// (e.g. no new OHLCV bars for longer than the expected cadence).
	AlertDataSyncStall AlertType = "data_sync_stall"
	// AlertBacktestFailure fires when a backtest job fails unexpectedly.
	AlertBacktestFailure AlertType = "backtest_failure"
	// AlertStrategyDegradation fires when a live strategy's performance
	// degrades beyond an acceptable threshold (drawdown, IC decay, etc.).
	AlertStrategyDegradation AlertType = "strategy_degradation"
	// AlertSystemError fires on unrecoverable infrastructure errors
	// (DB unreachable, Redis down, panic in a worker).
	AlertSystemError AlertType = "system_error"
)

// Alert represents an operational alert message routed by AlertManager.
type Alert struct {
	ID        string            `json:"id"`
	Type      AlertType         `json:"type"`
	Severity  Severity          `json:"severity"`
	Title     string            `json:"title"`
	Message   string            `json:"message"`
	Source    string            `json:"source"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// AlertChannel is the sink interface for alert delivery. Implementations
// must be safe for concurrent use; Send should return promptly and must
// not block the caller indefinitely.
type AlertChannel interface {
	Send(alert Alert) error
	Name() string
}

// AlertRule defines when to trigger (or suppress) an alert of a given
// Type. Cooldown prevents the same rule from re-firing within its
// window; Condition (optional) gates firing on a caller-supplied
// predicate evaluated against the Alert value.
type AlertRule struct {
	Name      string
	Type      AlertType
	Condition func(interface{}) bool
	Cooldown  time.Duration
	LastFired time.Time
}

// maxHistory caps the in-memory alert history to avoid unbounded growth.
const maxHistory = 1000

// AlertManager manages alert routing, cooldown enforcement, and delivery
// to registered channels. It is safe for concurrent use.
type AlertManager struct {
	channels []AlertChannel
	rules    []AlertRule
	logger   zerolog.Logger
	mu       sync.RWMutex
	history  []Alert
}

// NewAlertManager creates a new alert manager with the supplied logger.
// The logger is used for internal diagnostics; alert delivery itself is
// performed by registered channels (e.g. LogChannel).
func NewAlertManager(logger zerolog.Logger) *AlertManager {
	return &AlertManager{
		logger:  logger.With().Str("component", "system_alert_manager").Logger(),
		history: make([]Alert, 0, 64),
	}
}

// AddChannel adds a delivery channel (Slack, PagerDuty, log, webhook, etc.).
func (am *AlertManager) AddChannel(ch AlertChannel) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.channels = append(am.channels, ch)
}

// AddRule adds an alert rule. Rules are evaluated in registration order;
// the first matching rule that suppresses (failed Condition or active
// Cooldown) blocks the alert.
func (am *AlertManager) AddRule(rule AlertRule) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.rules = append(am.rules, rule)
}

// Fire evaluates the alert against matching rules (cooldown + condition)
// and, if not suppressed, dispatches it to all registered channels and
// records it in history. Returns nil if the alert was suppressed by a
// rule. Channel errors are logged; the first channel error encountered
// is returned (subsequent channels are still attempted).
func (am *AlertManager) Fire(alert Alert) error {
	now := time.Now()
	if alert.Timestamp.IsZero() {
		alert.Timestamp = now
	}

	am.mu.Lock()
	suppressed, fired := am.evaluateRulesLocked(alert, now)
	if suppressed {
		am.mu.Unlock()
		am.logger.Debug().
			Str("alert_type", string(alert.Type)).
			Str("title", alert.Title).
			Msg("alert suppressed by cooldown/condition")
		return nil
	}
	for i := range am.rules {
		if fired[i] {
			am.rules[i].LastFired = now
		}
	}
	channels := make([]AlertChannel, len(am.channels))
	copy(channels, am.channels)
	am.appendHistoryLocked(alert)
	am.mu.Unlock()

	var firstErr error
	for _, ch := range channels {
		if err := ch.Send(alert); err != nil {
			am.logger.Warn().
				Err(err).
				Str("channel", ch.Name()).
				Str("alert_type", string(alert.Type)).
				Str("alert_id", alert.ID).
				Msg("alert channel send failed")
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// evaluateRulesLocked must be called with am.mu held. Returns (suppressed, fired)
// where suppressed indicates whether the alert should be dropped, and fired[i]
// marks rules whose LastFired should advance. The alert is suppressed when any
// rule with matching Type has a non-nil Condition that returns false, or when
// any rule with matching Type is within its Cooldown window. Otherwise every
// matching rule is marked to advance its LastFired.
func (am *AlertManager) evaluateRulesLocked(alert Alert, now time.Time) (suppressed bool, fired []bool) {
	fired = make([]bool, len(am.rules))
	for i, r := range am.rules {
		if r.Type != alert.Type {
			continue
		}
		if r.Condition != nil && !r.Condition(alert) {
			return true, fired
		}
		if r.Cooldown > 0 && !r.LastFired.IsZero() && now.Sub(r.LastFired) < r.Cooldown {
			return true, fired
		}
		fired[i] = true
	}
	return false, fired
}

// appendHistoryLocked appends to history and trims to maxHistory.
// Must be called with am.mu held.
func (am *AlertManager) appendHistoryLocked(alert Alert) {
	am.history = append(am.history, alert)
	if len(am.history) > maxHistory {
		excess := len(am.history) - maxHistory
		am.history = am.history[excess:]
	}
}

// GetHistory returns up to limit most recent alerts (newest last). If
// limit <= 0, all retained history is returned. The returned slice is
// a copy; callers may mutate it freely.
func (am *AlertManager) GetHistory(limit int) []Alert {
	am.mu.RLock()
	defer am.mu.RUnlock()
	if limit <= 0 || limit > len(am.history) {
		limit = len(am.history)
	}
	start := len(am.history) - limit
	out := make([]Alert, limit)
	copy(out, am.history[start:])
	return out
}

// ============================================================================
// LogChannel — always-on, writes alerts via zerolog
// ============================================================================

// LogChannel writes each alert to the structured logger at the level
// matching the alert severity. It is always available and never fails.
type LogChannel struct {
	logger zerolog.Logger
}

// NewLogChannel constructs a LogChannel with the supplied logger.
func NewLogChannel(logger zerolog.Logger) *LogChannel {
	return &LogChannel{
		logger: logger.With().Str("component", "system_alert_log_channel").Logger(),
	}
}

// Name returns the channel identifier.
func (c *LogChannel) Name() string { return "log" }

// Send emits the alert at the appropriate log level. Always returns nil.
func (c *LogChannel) Send(a Alert) error {
	var event *zerolog.Event
	switch a.Severity {
	case SeverityCritical:
		event = c.logger.Error()
	case SeverityWarning:
		event = c.logger.Warn()
	default:
		event = c.logger.Info()
	}
	event = event.
		Str("alert_id", a.ID).
		Str("type", string(a.Type)).
		Str("severity", string(a.Severity)).
		Str("source", a.Source).
		Str("title", a.Title).
		Time("timestamp", a.Timestamp)
	for k, v := range a.Metadata {
		event = event.Str(k, v)
	}
	event.Msg(a.Message)
	return nil
}

// ============================================================================
// WebhookChannel — POST each alert as JSON to a configured URL
// ============================================================================

// WebhookChannel delivers alerts to an HTTP endpoint as JSON via POST.
// It is synchronous: Send blocks until the HTTP call completes or times
// out. The channel is optional; construct it only when an endpoint is
// configured. Delivery failures surface as errors from Send (and are
// also logged by the manager).
type WebhookChannel struct {
	url     string
	timeout time.Duration
	logger  zerolog.Logger
	client  *http.Client
}

// NewWebhookChannel constructs a WebhookChannel that posts to url with
// the supplied per-request timeout. A non-positive timeout defaults to 5s.
func NewWebhookChannel(url string, timeout time.Duration, logger zerolog.Logger) *WebhookChannel {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &WebhookChannel{
		url:     url,
		timeout: timeout,
		logger:  logger.With().Str("component", "system_alert_webhook_channel").Logger(),
		client:  &http.Client{Timeout: timeout},
	}
}

// Name returns the channel identifier.
func (c *WebhookChannel) Name() string { return "webhook" }

// Send POSTs the alert as JSON to the configured URL. Returns an error
// if encoding, request construction, or the HTTP call fails, or if the
// response status is >= 300.
func (c *WebhookChannel) Send(a Alert) error {
	payload, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("webhook: marshal: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("webhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "quantlab-systemalert/1.0")
	req.Header.Set("X-Alert-Type", string(a.Type))
	req.Header.Set("X-Alert-Severity", string(a.Severity))
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: non-2xx status %d", resp.StatusCode)
	}
	return nil
}
