// Package metrics defines Prometheus metrics for the Quant Lab application.
//
// It exposes a single Metrics handle constructed once at process startup
// via NewMetrics and shared via dependency injection. Each handle owns a
// private prometheus.Registry so that tests can run in parallel without
// colliding on duplicate metric registration.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds Prometheus metrics for the application.
type Metrics struct {
	// API metrics
	APIRequestDuration *prometheus.HistogramVec
	APIRequestCount    *prometheus.CounterVec

	// Backtest metrics
	BacktestDuration prometheus.Histogram
	BacktestActive   prometheus.Gauge

	// Data sync metrics.
	// SyncLagSeconds is labeled by source so that SetSyncLag(source, lag)
	// can track lag per data source (the project ingests from multiple
	// sources: tushare, eastmoney, mootdx, ...).
	SyncLagSeconds  *prometheus.GaugeVec
	SyncLastSuccess *prometheus.GaugeVec

	// Strategy metrics
	StrategyActive prometheus.Gauge

	registry *prometheus.Registry
}

// NewMetrics creates and registers all metrics on a private registry.
func NewMetrics() *Metrics {
	m := &Metrics{
		APIRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "api_request_duration_seconds",
			Help:    "Duration of API requests, labeled by method, path, and status.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path", "status"}),

		APIRequestCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "api_request_total",
			Help: "Total number of API requests, labeled by method, path, and status.",
		}, []string{"method", "path", "status"}),

		BacktestDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "backtest_duration_seconds",
			Help:    "Duration of a backtest execution.",
			Buckets: prometheus.ExponentialBuckets(0.05, 2, 12), // 50ms .. ~200s
		}),

		BacktestActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "backtest_active",
			Help: "Number of currently active backtests.",
		}),

		SyncLagSeconds: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "sync_lag_seconds",
			Help: "Data sync lag in seconds, labeled by source.",
		}, []string{"source"}),

		SyncLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "sync_last_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful sync, labeled by source.",
		}, []string{"source"}),

		StrategyActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "strategy_active",
			Help: "Number of currently active strategies.",
		}),

		registry: prometheus.NewRegistry(),
	}

	m.registry.MustRegister(
		m.APIRequestDuration,
		m.APIRequestCount,
		m.BacktestDuration,
		m.BacktestActive,
		m.SyncLagSeconds,
		m.SyncLastSuccess,
		m.StrategyActive,
	)
	return m
}

// Handler returns an http.Handler for the /metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	if m == nil || m.registry == nil {
		// Defensive fallback: a misconfigured wire-up should be loud at
		// runtime, not silent.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "metrics: nil registry", http.StatusServiceUnavailable)
		})
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// RecordAPIRequest records an API request: it observes the request
// duration on the histogram and increments the request counter.
func (m *Metrics) RecordAPIRequest(method, path, status string, duration time.Duration) {
	if m == nil || m.APIRequestDuration == nil {
		return
	}
	m.APIRequestDuration.WithLabelValues(method, path, status).Observe(duration.Seconds())
	m.APIRequestCount.WithLabelValues(method, path, status).Inc()
}

// RecordBacktest records a backtest execution by observing its duration.
func (m *Metrics) RecordBacktest(duration time.Duration) {
	if m == nil || m.BacktestDuration == nil {
		return
	}
	m.BacktestDuration.Observe(duration.Seconds())
}

// SetSyncLag sets the sync lag in seconds for the given source and
// stamps the source's last-success timestamp to now.
func (m *Metrics) SetSyncLag(source string, lag float64) {
	if m == nil || m.SyncLagSeconds == nil {
		return
	}
	m.SyncLagSeconds.WithLabelValues(source).Set(lag)
	m.SyncLastSuccess.WithLabelValues(source).Set(float64(time.Now().Unix()))
}
