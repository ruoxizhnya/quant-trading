package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Channel is the sink interface for AlertManager. Implementations are
// expected to be safe for concurrent use by multiple goroutines (the
// manager dispatches to channels from a single goroutine but tests
// may call Send directly).
//
// Send must not block the caller for an unbounded time. Implementations
// should respect ctx and return promptly. Channels that perform I/O
// (webhook) should run their delivery asynchronously or use a short
// timeout.
//
// Close is called once by the manager during shutdown. After Close,
// Send is a no-op.
type Channel interface {
	Send(ctx context.Context, alert Alert)
	Close()
}

// ============================================================================
// LogChannel — always-on, writes alerts via zerolog
// ============================================================================

// LogChannel writes each alert to the structured logger at the level
// matching the alert severity. LogChannel is a safe default: alerts are
// always visible to operators, no external dependencies required.
type LogChannel struct {
	logger zerolog.Logger
}

// NewLogChannel constructs a LogChannel with the supplied logger.
func NewLogChannel(logger zerolog.Logger) *LogChannel {
	return &LogChannel{
		logger: logger.With().Str("component", "alert_log_channel").Logger(),
	}
}

// Send emits the alert at the appropriate log level. The alert payload is
// rendered as structured fields; the message body is the Alert.Message.
func (c *LogChannel) Send(_ context.Context, a Alert) {
	event := c.logger.Info().
		Str("alert_id", a.ID).
		Str("rule", a.Rule).
		Float64("value", a.Value).
		Float64("threshold", a.Threshold).
		Time("timestamp", a.Timestamp)

	if a.Symbol != "" {
		event = event.Str("symbol", a.Symbol)
	}
	if a.Sector != "" {
		event = event.Str("sector", a.Sector)
	}
	for k, v := range a.Attributes {
		event = event.Interface(k, v)
	}

	switch a.Severity {
	case SeverityCritical:
		event = c.logger.Error()
		event.Msg(a.Message)
	case SeverityWarning:
		event = c.logger.Warn()
		event.Msg(a.Message)
	default:
		event.Msg(a.Message)
	}
}

// Close is a no-op for LogChannel.
func (c *LogChannel) Close() {}

// ============================================================================
// WebhookChannel — POST each alert as JSON to a configured URL
// ============================================================================

// WebhookChannel delivers alerts to an HTTP endpoint as JSON. The channel
// is fire-and-forget: Send enqueues the alert onto an internal goroutine
// that performs the HTTP POST with a configurable timeout. Delivery
// failures are logged but do not block the caller.
//
// The default HTTP client is a singleton configured with a 5s timeout;
// callers can supply a custom client via SetHTTPClient for testing or for
// advanced connection pooling needs.
type WebhookChannel struct {
	url     string
	timeout time.Duration
	logger  zerolog.Logger

	mu      sync.Mutex
	client  *http.Client
	queue   chan Alert
	closed  bool
	done    chan struct{}
}

// NewWebhookChannel constructs a WebhookChannel that posts to url with the
// supplied per-request timeout. The delivery goroutine starts immediately;
// callers must Close the channel when done to drain in-flight deliveries.
func NewWebhookChannel(url string, timeout time.Duration, logger zerolog.Logger) *WebhookChannel {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ch := &WebhookChannel{
		url:     url,
		timeout: timeout,
		logger:  logger.With().Str("component", "alert_webhook_channel").Logger(),
		client:  &http.Client{Timeout: timeout},
		queue:   make(chan Alert, 64),
		done:    make(chan struct{}),
	}
	go ch.run()
	return ch
}

// SetHTTPClient swaps the HTTP client (intended for tests). The client's
// Timeout is honored for the per-request budget; the WebhookChannel's
// internal timeout acts as a backstop via context.
func (c *WebhookChannel) SetHTTPClient(client *http.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if client != nil {
		c.client = client
	}
}

// Send enqueues the alert for asynchronous delivery. The call returns
// immediately; if the queue is full the alert is dropped and a warning
// is logged. This is intentional: an alert pipeline must not block the
// caller under back-pressure.
func (c *WebhookChannel) Send(_ context.Context, a Alert) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return
	}
	select {
	case c.queue <- a:
	default:
		c.logger.Warn().
			Str("alert_id", a.ID).
			Str("rule", a.Rule).
			Msg("WebhookChannel queue full, alert dropped")
	}
}

// Close marks the channel as closed and waits for the delivery goroutine
// to drain the queue. Safe to call multiple times.
func (c *WebhookChannel) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.queue)
	c.mu.Unlock()
	<-c.done
}

func (c *WebhookChannel) run() {
	defer close(c.done)
	for a := range c.queue {
		c.deliver(a)
	}
}

func (c *WebhookChannel) deliver(a Alert) {
	payload, err := json.Marshal(a)
	if err != nil {
		c.logger.Error().Err(err).Str("alert_id", a.ID).Msg("webhook: marshal failed")
		return
	}

	c.mu.Lock()
	client := c.client
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		c.logger.Error().Err(err).Str("alert_id", a.ID).Msg("webhook: build request failed")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "quantlab-alertmanager/1.0")
	req.Header.Set("X-Alert-ID", a.ID)
	req.Header.Set("X-Alert-Rule", a.Rule)
	req.Header.Set("X-Alert-Severity", string(a.Severity))

	resp, err := client.Do(req)
	if err != nil {
		c.logger.Warn().Err(err).Str("alert_id", a.ID).Str("url", c.url).Msg("webhook: POST failed")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		c.logger.Warn().
			Int("status", resp.StatusCode).
			Str("alert_id", a.ID).
			Str("url", c.url).
			Msg("webhook: non-2xx response")
		return
	}
	c.logger.Debug().Str("alert_id", a.ID).Msg("webhook: delivered")
}
