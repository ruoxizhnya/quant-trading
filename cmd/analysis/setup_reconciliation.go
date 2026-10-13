package main

// K16 ⑤ · 资金对账 worker 的**首个生产构造点**。
//
// 背景（台账 K16 ⑤ / K5 遗留）：pkg/live/reconciliation 的
// ReconciliationWorker / AlertDispatcher 一直是「算法侧交付、零生产构造点」
// ——handler 写好了（handlers_reconciliation.go）但装配从没接上，差异没有
// 告警出口。本文件补齐装配：worker ← traderAccountView（本地/券商双视图）
// ← alert.ReconciliationDispatcher ← AlertManager（log + webhook 通道，
// 与风控告警同通道集）。
//
// 券商侧视图的裁决（D1 未接真实券商）：QuerySettlement 目前返回**同一
// MockTrader 状态的自检视图**——这是 pkg/live/reconciliation 包注释里
// 列名的合法实现（"mock: returns the same MockTrader's internal state
// (self-check)"）。真实券商接入（D1）后**只换 broker 侧**一个适配器，
// worker / 告警链路一行不动。
//
// 已知缺口（如实记录，不静默）：LiveTrader 接口不暴露费用台账，
// traderAccountView 的 FeeSnap 恒为零值——两侧同为零，费用维度对账
// 等价于未启用。费用对账要接 OrderStore 的成交费用流水，届时单独立任务。

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/alert"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/live/reconciliation"
	"github.com/spf13/viper"
)

// traderAccountView 把 live.LiveTrader 的账户视图适配成对账两侧的快照
// 接口。本地侧（LocalSnapshotter）与券商自检侧（BrokerQuerier）共用这
// 一个类型：自检语义 = 两侧同源，账实差异恒零（健康基线）；人为注入差异
// 的验收走测试侧的 driftedBrokerView（见 setup_reconciliation_test.go）。
type traderAccountView struct {
	trader    live.LiveTrader
	accountID string // 单账户视图：MockTrader 只有一个账户
}

// compile-time assertions: 双接口同体（自检形态）。
var (
	_ reconciliation.LocalSnapshotter = (*traderAccountView)(nil)
	_ reconciliation.BrokerQuerier    = (*traderAccountView)(nil)
)

func (v *traderAccountView) SnapshotLocal(ctx context.Context, accountID string, asOf time.Time) (*reconciliation.LocalSnapshot, error) {
	if accountID != "" && accountID != v.accountID {
		return nil, errReconAccountMismatch(accountID, v.accountID)
	}
	snap, err := v.snapshot(ctx, asOf)
	if err != nil {
		return nil, err
	}
	return snap, nil
}

// QuerySettlement 自检形态：券商视图 = 同一 trader 状态。真实券商适配器
// （D1）落地后替换本方法所属类型即可，worker 不感知。
func (v *traderAccountView) QuerySettlement(ctx context.Context, accountID string, asOf time.Time) (*reconciliation.AccountSnapshot, error) {
	if accountID != "" && accountID != v.accountID {
		return nil, errReconAccountMismatch(accountID, v.accountID)
	}
	snap, err := v.snapshot(ctx, asOf)
	if err != nil {
		return nil, err
	}
	return &reconciliation.AccountSnapshot{
		AccountID: snap.AccountID,
		AsOf:      snap.AsOf,
		Cash:      snap.Cash,
		Positions: snap.Positions,
		Fees:      snap.Fees,
	}, nil
}

func (v *traderAccountView) snapshot(ctx context.Context, asOf time.Time) (*reconciliation.LocalSnapshot, error) {
	acct, err := v.trader.GetAccount(ctx)
	if err != nil {
		return nil, err
	}
	positions, err := v.trader.GetPositions(ctx)
	if err != nil {
		return nil, err
	}
	snaps := make(map[string]reconciliation.PositionSnap, len(positions))
	for _, p := range positions {
		snaps[p.Symbol] = reconciliation.PositionSnap{
			Symbol:      p.Symbol,
			Quantity:    p.Quantity,
			AvgCost:     p.AvgCost,
			MarketValue: p.MarketValue,
		}
	}
	return &reconciliation.LocalSnapshot{
		AccountID: v.accountID,
		AsOf:      asOf,
		Cash: reconciliation.CashSnap{
			TotalCNY:  acct.Cash,
			Available: acct.Cash, // MockTrader 无冻结金概念：可用 = 全部
		},
		Positions: snaps,
		// Fees 恒零：LiveTrader 接口不暴露费用流水（见文件头「已知缺口」）。
		// 两侧同为零 → 不产生费用维度误报。
		Fees: reconciliation.FeeSnap{},
	}, nil
}

// errReconAccountMismatch：单账户视图收到陌生账户 ID 时 fail-loud——
// 静默回成自己的账户会把「对错账」伪装成「账实相符」。
func errReconAccountMismatch(accountID, known string) error {
	return fmt.Errorf("reconciliation: unknown account %q (this view serves %q)", accountID, known)
}

func buildReconciliation(v *viper.Viper, mgr *alert.AlertManager, trader live.LiveTrader, logger zerolog.Logger) *reconciliation.ReconciliationWorker {
	if !v.GetBool("reconciliation.enabled") {
		// 未配置 = 不启动（fail-safe：自检对账是合规基线，配置后才开）。
		logger.Info().Msg("reconciliation worker not enabled (set reconciliation.enabled: true to start)")
		return nil
	}
	cfg := reconciliationConfigFromViper(v)
	view := &traderAccountView{trader: trader, accountID: reconAccountID(v)}
	// 自检形态：local 与 broker 共用同一视图（账实差异恒零的健康基线）。
	// 人为注入差异的验收见 setup_reconciliation_test.go 的 driftedBrokerView。
	worker := newReconciliationWorker(cfg, view, view, mgr, logger)
	logger.Info().
		Str("account_id", view.accountID).
		Dur("interval", cfg.Interval).
		Msg("ReconciliationWorker attached in-process with alert dispatcher (K16 ⑤)")
	return worker
}

// newReconciliationWorker 是装配核心：worker + 告警派发 + 报告落盘。
// buildReconciliation 与演示测试（人为注入差异）共用同一段装配，保证
// 「演示走的链路」与「生产走的链路」是同一条。
func newReconciliationWorker(
	cfg reconciliation.ReconciliationConfig,
	local reconciliation.LocalSnapshotter,
	broker reconciliation.BrokerQuerier,
	mgr *alert.AlertManager,
	logger zerolog.Logger,
) *reconciliation.ReconciliationWorker {
	dispatcher := alert.NewReconciliationDispatcher(mgr)

	var persister reconciliation.ReportPersister
	if cfg.ReportPath != "" {
		fs, err := reconciliation.NewFSReportPersister(cfg.ReportPath)
		if err != nil {
			// 落盘失败不静默降级：显式告警级日志 + 持久化停用（worker 内部
			// nil → NullReportPersister），报告仍留在 HistoryBuffer 供查询。
			logger.Error().Err(err).Str("report_path", cfg.ReportPath).
				Msg("reconciliation report persistence disabled (report dir unusable)")
		} else {
			persister = fs
		}
	}
	return reconciliation.NewReconciliationWorker(cfg, broker, local, persister, dispatcher, nil, logger)
}

// reconciliationConfigFromViper 以监管默认值（DefaultReconciliationConfig，
// 中国结算《证券资金对账指引》§3.2）为底，只覆盖 yaml 里显式配置的键——
// 缺键 = 保持监管默认，不被零值冲掉。
func reconciliationConfigFromViper(v *viper.Viper) reconciliation.ReconciliationConfig {
	cfg := reconciliation.DefaultReconciliationConfig()
	if v.IsSet("reconciliation.interval_sec") {
		cfg.Interval = time.Duration(v.GetInt("reconciliation.interval_sec")) * time.Second
	}
	if v.IsSet("reconciliation.cash_tolerance") {
		cfg.CashTolerance = v.GetFloat64("reconciliation.cash_tolerance")
	}
	if v.IsSet("reconciliation.quantity_tolerance") {
		cfg.QuantityTolerance = v.GetFloat64("reconciliation.quantity_tolerance")
	}
	if v.IsSet("reconciliation.market_value_tol") {
		cfg.MarketValueTol = v.GetFloat64("reconciliation.market_value_tol")
	}
	if v.IsSet("reconciliation.fee_tolerance") {
		cfg.FeeTolerance = v.GetFloat64("reconciliation.fee_tolerance")
	}
	if v.IsSet("reconciliation.report_path") {
		cfg.ReportPath = v.GetString("reconciliation.report_path")
	}
	if v.IsSet("reconciliation.history_limit") {
		cfg.HistoryLimit = v.GetInt("reconciliation.history_limit")
	}
	cfg.Enabled = v.GetBool("reconciliation.enabled")
	return cfg
}

func reconAccountID(v *viper.Viper) string {
	if id := v.GetString("reconciliation.account_id"); id != "" {
		return id
	}
	return "default"
}
