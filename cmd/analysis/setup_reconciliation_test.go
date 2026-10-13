package main

// K16 ⑤ 验收（台账）：「人为注入对账差异 → 告警触发点名」。
//
// 演示走的是**生产同一条装配链路**（newReconciliationWorker：worker →
// alert.ReconciliationDispatcher → AlertManager → 通道），只把券商侧视图
// 换成 driftedBrokerView（故意少记 / 多记现金与持仓）——这正是真实券商
// 适配器（D1）落地后会自然产生差异的形态。

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/alert"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/live/reconciliation"
)

// reconCaptureChannel 捕获派发进告警通道的每条 alert。
type reconCaptureChannel struct {
	mu     sync.Mutex
	alerts []alert.Alert
}

func (c *reconCaptureChannel) Send(_ context.Context, a alert.Alert) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.alerts = append(c.alerts, a)
}

func (c *reconCaptureChannel) Close() {}

func (c *reconCaptureChannel) snapshot() []alert.Alert {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]alert.Alert, len(c.alerts))
	copy(out, c.alerts)
	return out
}

// driftedBrokerView 故意给券商侧注入差异（现金少记 500、600000.SH 少 100 股）。
type driftedBrokerView struct {
	inner *traderAccountView
}

func (d *driftedBrokerView) QuerySettlement(ctx context.Context, accountID string, asOf time.Time) (*reconciliation.AccountSnapshot, error) {
	snap, err := d.inner.QuerySettlement(ctx, accountID, asOf)
	if err != nil {
		return nil, err
	}
	snap.Cash.TotalCNY -= 500 // 券商少记 500 → 现金差异
	if p, ok := snap.Positions["600000.SH"]; ok {
		p.Quantity -= 100 // 券商少记 100 股 → 持仓差异
		snap.Positions["600000.SH"] = p
	}
	return snap, nil
}

// newReconTestTrader 构造一个有持仓的 MockTrader（本地侧视图）。
func newReconTestTrader(t *testing.T) *live.MockTrader {
	t.Helper()
	trader := live.NewMockTrader(live.MockTraderConfig{InitialCash: 1_000_000}, zerolog.Nop())
	_, err := trader.SubmitOrder(context.Background(), "600000.SH", domain.DirectionLong, domain.OrderTypeMarket, 200, 10)
	require.NoError(t, err)
	return trader
}

// TestReconciliation_InjectedDiscrepancyFiresNamedAlert 是 K16 ⑤ 的验收
// 演示：人为注入对账差异 → 告警触发且**点名**（account / symbol / 数值）。
func TestReconciliation_InjectedDiscrepancyFiresNamedAlert(t *testing.T) {
	trader := newReconTestTrader(t)
	local := &traderAccountView{trader: trader, accountID: "acct-demo"}

	mgr := alert.NewAlertManager(alert.AlertManagerConfig{}, zerolog.Nop())
	cap := &reconCaptureChannel{}
	require.True(t, mgr.AddChannel(cap))
	t.Cleanup(mgr.Close)

	cfg := reconciliation.DefaultReconciliationConfig()
	cfg.ReportPath = "" // 不落盘（测试不写 compliance/ 目录）
	worker := newReconciliationWorker(cfg, local, &driftedBrokerView{inner: local}, mgr, zerolog.Nop())

	rep, err := worker.ReconcileOnce(context.Background(), "acct-demo")
	require.NoError(t, err)
	require.False(t, rep.Healthy, "注入差异后对账不得报 healthy")
	require.NotEmpty(t, rep.Discrepancies, "注入的差异必须被 Reconcile 捕获")

	got := cap.snapshot()
	require.NotEmpty(t, got, "对账差异必须触发告警（零告警 = 告警通道没接上）")
	require.Len(t, got, len(rep.Discrepancies), "一条差异一条告警（逐项留痕，不聚合）")

	// 点名验收：现金差异的告警必须点名 account 与数值方向。
	// Reconcile 的 Delta = local - broker：券商少记 500 → Delta=+500。
	// 现金是账户级差异，本就不带 symbol（symbol 点名验收在下方持仓腿）。
	var cashAlert *alert.Alert
	for i := range got {
		if got[i].Attributes["field"] == "total_cny" {
			cashAlert = &got[i]
			break
		}
	}
	require.NotNil(t, cashAlert, "现金差异告警缺失")
	assert.Contains(t, cashAlert.Message, "acct-demo", "告警必须点名账户")
	assert.InDelta(t, 500, cashAlert.Value, 1e-9, "告警必须带差异值（+500，local>broker）")
	assert.Equal(t, alert.RuleReconPrefix+"cash", cashAlert.Rule)

	// 持仓差异同理（local 200 股 vs broker 100 股 → Delta=+100）。
	var qtyAlert *alert.Alert
	for i := range got {
		if got[i].Attributes["field"] == "quantity" {
			qtyAlert = &got[i]
			break
		}
	}
	require.NotNil(t, qtyAlert, "持仓差异告警缺失")
	assert.Equal(t, "600000.SH", qtyAlert.Symbol)
	assert.InDelta(t, 100, qtyAlert.Value, 1e-9)
}

// TestReconciliation_HealthySelfCheckFiresNoAlert：自检形态（两侧同源）
// 账实相符 → 零差异零告警（健康基线，防误报）。
func TestReconciliation_HealthySelfCheckFiresNoAlert(t *testing.T) {
	trader := newReconTestTrader(t)
	local := &traderAccountView{trader: trader, accountID: "acct-1"}

	mgr := alert.NewAlertManager(alert.AlertManagerConfig{}, zerolog.Nop())
	cap := &reconCaptureChannel{}
	require.True(t, mgr.AddChannel(cap))
	t.Cleanup(mgr.Close)

	cfg := reconciliation.DefaultReconciliationConfig()
	cfg.ReportPath = ""
	worker := newReconciliationWorker(cfg, local, local, mgr, zerolog.Nop()) // 自检：同一视图

	rep, err := worker.ReconcileOnce(context.Background(), "acct-1")
	require.NoError(t, err)
	assert.True(t, rep.Healthy, "自检形态应账实相符")
	assert.Empty(t, rep.Discrepancies)
	assert.Empty(t, cap.snapshot(), "账实相符不得触发告警（防误报）")
}

// TestTraderAccountView_RejectsUnknownAccount：陌生账户 ID 必须 fail-loud，
// 不得静默回成本账户（那会把「对错账」伪装成「账实相符」）。
func TestTraderAccountView_RejectsUnknownAccount(t *testing.T) {
	trader := newReconTestTrader(t)
	view := &traderAccountView{trader: trader, accountID: "acct-1"}

	_, err := view.SnapshotLocal(context.Background(), "acct-OTHER", time.Now())
	require.Error(t, err, "陌生账户必须报错")
	_, err = view.QuerySettlement(context.Background(), "acct-OTHER", time.Now())
	require.Error(t, err)

	// 同账户 + 空账户（默认发现路径）放行。
	_, err = view.SnapshotLocal(context.Background(), "acct-1", time.Now())
	require.NoError(t, err)
	_, err = view.SnapshotLocal(context.Background(), "", time.Now())
	require.NoError(t, err)
}
