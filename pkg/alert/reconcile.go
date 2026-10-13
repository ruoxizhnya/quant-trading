// K16 ⑤（K5 遗留认领）：对账 / 双跑差异的生产告警通道接线。
//
// 背景：K5 切片 2 交付 ReconcileSignals（信号对账纯函数）时，差异只以
// 返回列表 + e2e fail-loud 兑现，**没有**接进任何告警通道；资金对账的
// reconciliation.AlertDispatcher 同样零生产构造点。本文件是两者的
// AlertDispatcher 生产实现——差异经 AlertManager.Fire 走与风控告警
// 同一套通道（log + webhook），不再各写各的旁路。
//
// 依赖方向：pkg/alert → pkg/live（SignalDiscrepancy）/ pkg/live/reconciliation
// （Discrepancy）是安全的——这两个包都不 import pkg/alert（AlertDispatcher
// 是消费方定义的窄接口，reconciliation 只声明它），与 K16 ② 的
// nautilus.RunObserver 同一裁决模式。
package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/live/reconciliation"
)

// Rule 命名约定：外部告警的 rule 一律带来源前缀（recon.* / signal_recon.*），
// 与 detectors 的 rule 命名区分，便于告警规则按来源键控。
const (
	RuleReconPrefix     = "recon."
	RuleSignalReconRule = "signal_recon"
	RuleDualRunGate     = "dualrun_gate"
)

// ReconciliationDispatcher 是 reconciliation.AlertDispatcher 的生产实现：
// 每条资金对账差异 → 一条 alert，severity 直接映射（critical/warning/info
// 词表两侧同名）。构造后即可注入 NewReconciliationWorker 的 dispatcher 槽。
type ReconciliationDispatcher struct {
	mgr *AlertManager
}

// compile-time assertion: 本类型就是 reconciliation worker 要的 dispatcher。
var _ reconciliation.AlertDispatcher = (*ReconciliationDispatcher)(nil)

func NewReconciliationDispatcher(mgr *AlertManager) *ReconciliationDispatcher {
	return &ReconciliationDispatcher{mgr: mgr}
}

// DispatchReconciliationAlerts 把一轮对账的全部差异逐条发进告警通道。
// 一条差异一条告警（不是聚合成一条）——监管口径要求「偏差项目逐项留痕」
// （《证券公司内部控制指引》§4.3），聚合会吞掉点名信息。
func (d *ReconciliationDispatcher) DispatchReconciliationAlerts(ctx context.Context, accountID string, discrepancies []reconciliation.Discrepancy) {
	if d == nil || d.mgr == nil {
		return
	}
	for _, disc := range discrepancies {
		d.mgr.Fire(ctx, Alert{
			Rule:     RuleReconPrefix + string(disc.Kind),
			Severity: reconSeverity(disc.Severity),
			Message: fmt.Sprintf("对账差异 [%s/%s] account=%s symbol=%s local=%.4f broker=%.4f delta=%.4f（容差 %.4f）: %s",
				disc.Kind, disc.Severity, accountID, disc.Symbol,
				disc.LocalVal, disc.BrokerVal, disc.Delta, disc.Tolerance, disc.Note),
			Value:     disc.Delta,
			Threshold: disc.Tolerance,
			Symbol:    disc.Symbol,
			Timestamp: disc.DetectedAt,
			Attributes: map[string]any{
				"account_id": accountID,
				"kind":       string(disc.Kind),
				"field":      disc.Field,
				"local_val":  disc.LocalVal,
				"broker_val": disc.BrokerVal,
			},
		})
	}
}

// reconSeverity 映射 reconciliation 的差异严重度到 alert 的告警严重度。
// 两侧词表同名（critical/warning/info）；未知值 fail-closed 到 warning
// ——宁可多吵不静默吞（对账差异是资金安全事件）。
func reconSeverity(s reconciliation.DiscrepancySeverity) Severity {
	switch s {
	case reconciliation.SeverityCritical:
		return SeverityCritical
	case reconciliation.SeverityInfo:
		return SeverityInfo
	default:
		return SeverityWarning
	}
}

// SignalDispatcher 把双跑（paper vs 回测）信号对账差异发进告警通道
// （K5 遗留：ReconcileSignals 的差异此前只回列表 + e2e fail-loud，
// 生产告警通道未接）。
type SignalDispatcher struct {
	mgr *AlertManager
}

func NewSignalDispatcher(mgr *AlertManager) *SignalDispatcher {
	return &SignalDispatcher{mgr: mgr}
}

// DispatchSignalDiscrepancies 发一批信号对账差异。判据（signal_reconcile.go）：
// 「信号一致是硬门禁」，任何超出浮点噪声的差异都是 critical——故这里
// 不做 severity 分级，全部 critical。一条差异一条告警，点名 (date,symbol)。
func (d *SignalDispatcher) DispatchSignalDiscrepancies(ctx context.Context, discrepancies []live.SignalDiscrepancy) {
	if d == nil || d.mgr == nil {
		return
	}
	for _, disc := range discrepancies {
		d.mgr.Fire(ctx, Alert{
			Rule:     RuleSignalReconRule,
			Severity: SeverityCritical,
			Message: fmt.Sprintf("双跑信号差异 [%s] %s %s: paper=%s backtest=%s %s",
				disc.Kind, disc.Date.Format("2006-01-02"), disc.Symbol,
				disc.PaperVal, disc.BacktestVal, disc.Note),
			Symbol:    disc.Symbol,
			Timestamp: time.Now(), // SignalDiscrepancy 无 DetectedAt 字段（Date 是信号日，非发现时刻）
			Attributes: map[string]any{
				"kind":         string(disc.Kind),
				"date":         disc.Date.Format("2006-01-02"),
				"paper_val":    disc.PaperVal,
				"backtest_val": disc.BacktestVal,
			},
		})
	}
}

// DualRunBreach 是一条双跑对齐超阈/破坏项（对齐 cmd/dualrun.GateViolation
// 的信息面：rule + 点名消息）。门禁的违例消息自带指标数值与阈值（供 CI
// 日志直接可读），告警侧不再拆结构化字段——一条违例一条告警原样转达。
type DualRunBreach struct {
	Rule    string
	Message string
}

// DispatchDualRunBreaches 发双跑对齐超阈告警（K16 ⑤：「双跑差异超阈经
// AlertDispatcher 告警」）。runID 标识一次双跑，breaches 是门禁（K16 ①
// cmd/dualrun -gate，阈值表见 k15-dualrun-report.md §2）的违例清单。
// 信号/对齐一致是硬门禁，全部 critical——门禁红即告警，不做分级。
func (d *SignalDispatcher) DispatchDualRunBreaches(ctx context.Context, runID string, breaches []DualRunBreach) {
	if d == nil || d.mgr == nil {
		return
	}
	for _, b := range breaches {
		d.mgr.Fire(ctx, Alert{
			Rule:      RuleDualRunGate,
			Severity:  SeverityCritical,
			Message:   fmt.Sprintf("双跑对齐超阈 run=%s [%s] %s", runID, b.Rule, b.Message),
			Timestamp: time.Now(),
			Attributes: map[string]any{
				"run_id":      runID,
				"breach_rule": b.Rule,
			},
		})
	}
}
