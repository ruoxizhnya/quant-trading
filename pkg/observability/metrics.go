// Package observability provides Prometheus metrics and request_id /
// trace propagation middleware for the Quant Lab services.
//
// Sprint 6 P0-3 (ADR-017 §1): the four core metrics exposed at
// /metrics are:
//
//  1. backtest_duration_seconds{strategy,universe}     — Histogram
//  2. http_client_requests_total{service,status}       — Counter
//  3. llm_tokens_total{provider,model}                 — Counter
//  4. cache_hit_ratio{kind}                            — Gauge
//
// Plus a request_id (X-Request-ID header) that is generated for
// inbound requests and propagated to outbound HTTP calls so log
// lines can be correlated end-to-end across services.
//
// K16 ② · 侧车 round-trip 观测（NautilusTrader 双内核）：另注册两条
// NT 侧车专用指标 —— nt_sidecar_calls_total{outcome}（成败计数，失败率 =
// outcome!="ok" 的速率占比）与 nt_sidecar_call_duration_seconds{outcome}
// （延迟分布）。ObserveNTCall 隐式实现 nautilus.RunObserver（消费方定义的
// 窄接口；编译期断言在装配侧 cmd/analysis/setup.go）。outcome 取值封闭为
// ok/unavailable/timeout/internal，与 pkg/errors 错误码族对齐，标签基数有界。
package observability

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// RequestIDHeader is the canonical header used to propagate the
// per-request correlation token. It is intentionally distinct from
// the W3C `traceparent` header so that we can layer OTel on top
// later without breaking existing tooling that scrapes X-Request-ID.
const RequestIDHeader = "X-Request-ID"

// Metrics is the central handle to the four core metrics described
// in ADR-017 §1. Construct it once at process startup via NewMetrics
// and share it via dependency injection.
type Metrics struct {
	BacktestDuration *prometheus.HistogramVec
	HTTPRequests     *prometheus.CounterVec
	LLMTokens        *prometheus.CounterVec
	CacheHitRatio    *prometheus.GaugeVec

	// K16 ② · NT 侧车 round-trip 观测（outcome 取值封闭：ok/unavailable/
	// timeout/internal，与 pkg/errors 错误码族对齐）。
	NTCallCount    *prometheus.CounterVec
	NTCallDuration *prometheus.HistogramVec

	registry *prometheus.Registry
}

// NewMetrics creates a fresh Metrics handle with a private
// prometheus.Registry. Using a private registry (rather than the
// global promauto.DefaultRegisterer) means:
//
//   - tests can run in parallel without colliding on duplicate
//     metric registration
//   - production code can choose to expose /metrics with only the
//     four ADR-017 metrics, not the default Go runtime collectors
//     (call RegisterCollectors to opt in)
func NewMetrics() *Metrics {
	return &Metrics{
		BacktestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "backtest_duration_seconds",
			Help:    "Duration of a backtest execution, labeled by strategy and universe.",
			Buckets: prometheus.ExponentialBuckets(0.05, 2, 12), // 50ms .. ~200s
		}, []string{"strategy", "universe"}),

		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_client_requests_total",
			Help: "Count of outbound HTTP calls the service has made, labeled by target service and HTTP status.",
		}, []string{"service", "status"}),

		LLMTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "llm_tokens_total",
			Help: "Total LLM tokens consumed, labeled by provider and model.",
		}, []string{"provider", "model"}),

		CacheHitRatio: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cache_hit_ratio",
			Help: "Rolling cache hit ratio in [0,1], labeled by cache kind (factor, ohlcv, sync_job, etc.).",
		}, []string{"kind"}),

		NTCallCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "nt_sidecar_calls_total",
			Help: "Count of NT sidecar round trips, labeled by outcome (ok/unavailable/timeout/internal). Failure rate = rate(outcome!=\"ok\") share.",
		}, []string{"outcome"}),

		NTCallDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "nt_sidecar_call_duration_seconds",
			Help:    "Duration of NT sidecar round trips, labeled by outcome.",
			Buckets: prometheus.ExponentialBuckets(0.05, 2, 12), // 50ms .. ~200s，与 backtest_duration 同桶口径
		}, []string{"outcome"}),

		registry: prometheus.NewRegistry(),
	}
}

// Register attaches the four core metrics (plus the K16 ② NT sidecar
// pair) to the underlying registry. The constructor above does not
// auto-register so that tests can inspect / re-create metrics;
// production code MUST call Register before exposing /metrics.
func (m *Metrics) Register() {
	m.registry.MustRegister(
		m.BacktestDuration,
		m.HTTPRequests,
		m.LLMTokens,
		m.CacheHitRatio,
		m.NTCallCount,
		m.NTCallDuration,
	)
}

// RegisterCollectors attaches the standard Go process / Go runtime
// collectors. Call this in addition to Register when you want
// memory/CPU/Goroutine visibility alongside the four core metrics.
func (m *Metrics) RegisterCollectors(collectors ...prometheus.Collector) {
	for _, c := range collectors {
		m.registry.MustRegister(c)
	}
}

// Registry returns the underlying registry. Useful for tests that
// need to scrape /metrics directly via testutil.GatherAndCount.
func (m *Metrics) Registry() *prometheus.Registry { return m.registry }

// ObserveBacktest records a single backtest duration. Empty label
// values are allowed (the metric is emitted with an empty label,
// matching the convention used by all four core metrics).
func (m *Metrics) ObserveBacktest(strategyName, universe string, d time.Duration) {
	if m == nil || m.BacktestDuration == nil {
		return
	}
	m.BacktestDuration.WithLabelValues(strategyName, universe).Observe(d.Seconds())
}

// ObserveHTTP records one outbound HTTP call.
func (m *Metrics) ObserveHTTP(service string, status int) {
	if m == nil || m.HTTPRequests == nil {
		return
	}
	m.HTTPRequests.WithLabelValues(service, strconv.Itoa(status)).Inc()
}

// AddLLMTokens increments the LLM token counter.
func (m *Metrics) AddLLMTokens(provider, model string, tokens int) {
	if m == nil || m.LLMTokens == nil || tokens <= 0 {
		return
	}
	m.LLMTokens.WithLabelValues(provider, model).Add(float64(tokens))
}

// SetCacheHitRatio sets the cache hit ratio for a named cache kind.
// The ratio is clamped to [0, 1] to avoid emitting invalid gauge
// values that would confuse alerting rules.
func (m *Metrics) SetCacheHitRatio(kind string, ratio float64) {
	if m == nil || m.CacheHitRatio == nil {
		return
	}
	if ratio < 0 {
		ratio = 0
	} else if ratio > 1 {
		ratio = 1
	}
	m.CacheHitRatio.WithLabelValues(kind).Set(ratio)
}

// ObserveNTCall records one NT sidecar round trip (K16 ②). It implicitly
// satisfies the consumer-defined port nautilus.RunObserver — the compile-time
// assertion lives at the assembly site (cmd/analysis/setup.go) so this package
// keeps no dependency on pkg/backtest/nautilus.
//
// outcome 是封闭词表（ok/unavailable/timeout/internal，见
// nautilus.RunObserver）；label 透传不再校验 —— 校验点在生产者侧
// （nautilus.outcomeFor 对未知错误码 fail-closed 折叠为 internal），
// 这里不重复一道双真相。
func (m *Metrics) ObserveNTCall(outcome string, d time.Duration) {
	if m == nil {
		return
	}
	if m.NTCallCount != nil {
		m.NTCallCount.WithLabelValues(outcome).Inc()
	}
	if m.NTCallDuration != nil {
		m.NTCallDuration.WithLabelValues(outcome).Observe(d.Seconds())
	}
}
