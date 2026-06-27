package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// histogramSampleCount gathers the registry and returns the total number
// of observations recorded for the named histogram metric (summed across
// all label-value series). testutil.GatherAndCount counts metric series,
// not observations, so it is useless for asserting how many times a
// histogram was observed.
func histogramSampleCount(t *testing.T, g prometheusGatherer, name string) uint64 {
	t.Helper()
	mfs, err := g.Gather()
	require.NoError(t, err)
	var total uint64
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if h := m.GetHistogram(); h != nil {
				total += h.GetSampleCount()
			}
		}
	}
	return total
}

type prometheusGatherer interface {
	Gather() ([]*dto.MetricFamily, error)
}

func TestMetrics_New(t *testing.T) {
	m := NewMetrics()

	assert.NotNil(t, m.APIRequestDuration)
	assert.NotNil(t, m.APIRequestCount)
	assert.NotNil(t, m.BacktestDuration)
	assert.NotNil(t, m.BacktestActive)
	assert.NotNil(t, m.SyncLagSeconds)
	assert.NotNil(t, m.SyncLastSuccess)
	assert.NotNil(t, m.StrategyActive)
	assert.NotNil(t, m.registry)
}

func TestMetrics_RecordAPIRequest(t *testing.T) {
	m := NewMetrics()

	m.RecordAPIRequest("GET", "/api/backtest", "200", 120*time.Millisecond)

	// Counter must reflect the single recorded request.
	count := testutil.ToFloat64(m.APIRequestCount.WithLabelValues("GET", "/api/backtest", "200"))
	assert.Equal(t, 1.0, count)

	// The histogram must have observed one sample.
	assert.Equal(t, uint64(1), histogramSampleCount(t, m.registry, "api_request_duration_seconds"))

	// A second call with a different label set must not affect the first.
	m.RecordAPIRequest("POST", "/api/backtest", "500", 2*time.Second)
	count2 := testutil.ToFloat64(m.APIRequestCount.WithLabelValues("GET", "/api/backtest", "200"))
	assert.Equal(t, 1.0, count2, "first label set must be unchanged")

	// Both observations are now recorded (two series, one sample each).
	assert.Equal(t, uint64(2), histogramSampleCount(t, m.registry, "api_request_duration_seconds"))
}

func TestMetrics_RecordBacktest(t *testing.T) {
	m := NewMetrics()

	m.RecordBacktest(3 * time.Second)
	assert.Equal(t, uint64(1), histogramSampleCount(t, m.registry, "backtest_duration_seconds"))

	// Recording again must accumulate observations.
	m.RecordBacktest(500 * time.Millisecond)
	assert.Equal(t, uint64(2), histogramSampleCount(t, m.registry, "backtest_duration_seconds"))
}

func TestMetrics_SetSyncLag(t *testing.T) {
	m := NewMetrics()

	m.SetSyncLag("tushare", 42.0)
	m.SetSyncLag("eastmoney", 7.5)

	assert.Equal(t, 42.0, testutil.ToFloat64(m.SyncLagSeconds.WithLabelValues("tushare")))
	assert.Equal(t, 7.5, testutil.ToFloat64(m.SyncLagSeconds.WithLabelValues("eastmoney")))

	// Last-success timestamp must be a positive unix time.
	ts := testutil.ToFloat64(m.SyncLastSuccess.WithLabelValues("tushare"))
	assert.Greater(t, ts, 0.0)
}

func TestMetrics_Handler(t *testing.T) {
	t.Run("serves metrics", func(t *testing.T) {
		m := NewMetrics()
		m.RecordAPIRequest("GET", "/health", "200", 10*time.Millisecond)

		h := m.Handler()
		require.NotNil(t, h)

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Header().Get("Content-Type"), "text/plain")
		assert.Contains(t, rr.Body.String(), "api_request_total")
	})

	t.Run("nil metrics returns 503", func(t *testing.T) {
		var m *Metrics
		h := m.Handler()
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	})
}
