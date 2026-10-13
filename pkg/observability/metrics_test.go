// K16 ② · NT 侧车 round-trip 指标断言：ObserveNTCall 必须把成败计数与延迟
// 分布写进 Prometheus registry（/metrics 可抓），nil 接收者不 panic。
package observability

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObserveNTCall_RecordsCountAndDuration(t *testing.T) {
	m := NewMetrics()
	m.Register()

	m.ObserveNTCall("ok", 120*time.Millisecond)
	m.ObserveNTCall("ok", 30*time.Millisecond)
	m.ObserveNTCall("unavailable", 5*time.Millisecond)

	// 失败率断言的数据面：outcome!="ok" 的计数与 ok 计数分开可读。
	assert.InDelta(t, 2, testutil.ToFloat64(m.NTCallCount.WithLabelValues("ok")), 1e-9)
	assert.InDelta(t, 1, testutil.ToFloat64(m.NTCallCount.WithLabelValues("unavailable")), 1e-9)
	assert.InDelta(t, 0, testutil.ToFloat64(m.NTCallCount.WithLabelValues("timeout")), 1e-9)

	// 延迟分布：直方图 series 数（"ok" 与 "unavailable" 两条 label）。
	assert.Equal(t, 2, testutil.CollectAndCount(m.NTCallDuration),
		"NTCallDuration 应有 ok/unavailable 两条 series")

	// registry 能抓到两条指标（/metrics 暴露的前提）。GatherAndCount 数的是
	// series 总数：counter 3 条（ok/unavailable/timeout）+ histogram 2 条。
	n, err := testutil.GatherAndCount(m.Registry(),
		"nt_sidecar_calls_total", "nt_sidecar_call_duration_seconds")
	require.NoError(t, err)
	assert.Equal(t, 5, n)
}

// nil 接收者是 no-op：观察永远不得 panic（观察不承重）。
func TestObserveNTCall_NilMetricsIsNoop(t *testing.T) {
	var m *Metrics
	assert.NotPanics(t, func() { m.ObserveNTCall("ok", time.Millisecond) })
}
