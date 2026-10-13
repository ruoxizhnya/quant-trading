// K16 ⑤ · 对账/双跑差异告警派发断言。
//
// 验收（台账 K16 ⑤）：「人为注入对账差异 → 告警触发点名」。
// 破坏验证（开发期实测）：把 DispatchReconciliationAlerts 循环体改成
// 直接 return → TestReconciliationDispatcher_FiresPerDiscrepancy 红（已还原）。
package alert

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/live/reconciliation"
)

// captureChannel 复用 manager_test.go 的既有定义（同包，snapshot() 读法一致）。
func newReconTestManager(t *testing.T) (*AlertManager, *captureChannel) {
	t.Helper()
	mgr := NewAlertManager(AlertManagerConfig{}, zerolog.Nop())
	cap := &captureChannel{}
	require.True(t, mgr.AddChannel(cap))
	t.Cleanup(mgr.Close)
	return mgr, cap
}

func TestReconciliationDispatcher_FiresPerDiscrepancy(t *testing.T) {
	mgr, cap := newReconTestManager(t)
	disp := NewReconciliationDispatcher(mgr)

	discs := []reconciliation.Discrepancy{
		{
			Kind: reconciliation.KindCash, Severity: reconciliation.SeverityCritical,
			Field: "total_cny", LocalVal: 1000, BrokerVal: 850, Delta: -150, Tolerance: 0.01,
			Note: "现金偏差", DetectedAt: time.Now(),
		},
		{
			Kind: reconciliation.KindQuantity, Severity: reconciliation.SeverityWarning,
			Symbol: "600000.SH", LocalVal: 100, BrokerVal: 90, Delta: -10, Tolerance: 0,
			Note: "持仓偏差", DetectedAt: time.Now(),
		},
	}

	disp.DispatchReconciliationAlerts(context.Background(), "acct-1", discs)

	got := cap.snapshot()
	require.Len(t, got, 2, "一条差异必须一条告警（逐项留痕，不聚合）")
	assert.Equal(t, "recon.cash", got[0].Rule)
	assert.Equal(t, SeverityCritical, got[0].Severity)
	assert.Contains(t, got[0].Message, "acct-1")
	assert.Contains(t, got[0].Message, "现金偏差")
	assert.InDelta(t, -150, got[0].Value, 1e-9)
	assert.Equal(t, "recon.quantity", got[1].Rule)
	assert.Equal(t, SeverityWarning, got[1].Severity)
	assert.Equal(t, "600000.SH", got[1].Symbol)
	assert.Contains(t, got[1].Message, "持仓偏差")
}

// severity 映射：critical/warning/info 词表对齐，未知值 fail-closed 到 warning。
func TestReconSeverity_MapsAndFailsClosed(t *testing.T) {
	assert.Equal(t, SeverityCritical, reconSeverity(reconciliation.SeverityCritical))
	assert.Equal(t, SeverityWarning, reconSeverity(reconciliation.SeverityWarning))
	assert.Equal(t, SeverityInfo, reconSeverity(reconciliation.SeverityInfo))
	assert.Equal(t, SeverityWarning, reconSeverity(reconciliation.DiscrepancySeverity("whatever")),
		"未知严重度必须 fail-closed 到 warning，不得静默吞")
}

// 双跑信号差异：全部 critical（信号一致是硬门禁），点名 (date,symbol)。
func TestSignalDispatcher_FiresCriticalPerDiscrepancy(t *testing.T) {
	mgr, cap := newReconTestManager(t)
	disp := NewSignalDispatcher(mgr)

	disp.DispatchSignalDiscrepancies(context.Background(), []live.SignalDiscrepancy{
		{
			Date: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), Symbol: "000001.SZ",
			Kind: live.KindSignalDirection, PaperVal: "long", BacktestVal: "close",
			Note: "方向漂移",
		},
	})

	got := cap.snapshot()
	require.Len(t, got, 1)
	assert.Equal(t, RuleSignalReconRule, got[0].Rule)
	assert.Equal(t, SeverityCritical, got[0].Severity)
	assert.Equal(t, "000001.SZ", got[0].Symbol)
	assert.Contains(t, got[0].Message, "2026-03-02")
	assert.Contains(t, got[0].Message, "方向漂移")
}

// 双跑对齐超阈告警（gate 裁决结果的告警侧）。
func TestSignalDispatcher_DualRunBreach(t *testing.T) {
	mgr, cap := newReconTestManager(t)
	disp := NewSignalDispatcher(mgr)

	disp.DispatchDualRunBreaches(context.Background(), "run-9", []DualRunBreach{
		{Rule: "metric/sharpe", Message: "sharpe: go=1.2 nt=1.377 |Δ|=0.177 超绝对阈 0.05"},
	})

	got := cap.snapshot()
	require.Len(t, got, 1)
	assert.Equal(t, RuleDualRunGate, got[0].Rule)
	assert.Equal(t, SeverityCritical, got[0].Severity)
	assert.Contains(t, got[0].Message, "run-9")
	assert.Contains(t, got[0].Message, "metric/sharpe")
	assert.Contains(t, got[0].Message, "超绝对阈")
	assert.Equal(t, "metric/sharpe", got[0].Attributes["breach_rule"])
}

// Fire 补齐零值字段；Close 后 no-op（不得 panic）。
func TestFire_FillsDefaultsAndNoopAfterClose(t *testing.T) {
	mgr, cap := newReconTestManager(t)

	mgr.Fire(context.Background(), Alert{Severity: SeverityWarning, Message: "hi"})
	got := cap.snapshot()
	require.Len(t, got, 1)
	assert.NotEmpty(t, got[0].ID)
	assert.Equal(t, "external", got[0].Rule)
	assert.False(t, got[0].Timestamp.IsZero())

	mgr.Close()
	assert.NotPanics(t, func() {
		mgr.Fire(context.Background(), Alert{Severity: SeverityCritical, Message: "after close"})
	})
	assert.Len(t, cap.snapshot(), 1, "Close 后不得再派发")
}
