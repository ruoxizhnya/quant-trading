// exec-engine 契约（K0 切片 2）—— 本文件只冻结接口，不含实现逻辑。
//
// 模块职责（蓝图 §5 exec-engine 行）：订单生命周期、撮合（回测）/
// 报送（实盘）、对账。
//   - 收订单意图 → 过风控挂点（risk.CheckOrder）→ broker → 发
//     msgbus.TopicExecFill；被拒发 msgbus.TopicExecOrderRejected；
//   - 订单落 quant.orders、成交落 quant.fills、对账报告落
//     quant.recon_report。
//
// 本文件**只声明、不改动**任何既有实现：Broker（engine.go）、
// OrderStore（order_store.go）、OrderManager struct（order_manager.go）
// 均原地不动，仅被引用 / 被钉进编译期检查。
//
// K0 切片 2：stub 方法体固定 panic("contract stub: not implemented")，
// K1 实现直接替换 stub。
package live

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
)

// OrderIntent 是 risk-engine 冻结的订单意图类型（pkg/risk/interfaces.go）。
//
// 本包以**别名**引用而非重新定义，理由：order_intent 是「策略 → 风控 →
// 执行」三段共用的同一个东西，定义两处必然漂移（一处加字段另一处不知
// 道，编译能过、语义已分叉）。单一事实源在 risk-engine——它是
// BootOrder 中先于 exec-engine 起的模块（第 5 位 vs 第 6 位），
// 依赖方向 risk ← exec 与启动顺序一致，不成环。
type OrderIntent = risk.OrderIntent

// OrderLifecycle 是订单生命周期管理能力。
//
// ─── 裁决：为什么接口不叫 OrderManager ────────────────────────────
// 任务书写名 `OrderManager`，但 pkg/live/order_manager.go:14 已有一个
// `OrderManager` **struct**，Go 同包内不可重名（否则 duplicate
// declaration，编译直接死）。按「不改现有实现文件」的约束，也不能把
// struct 改名。
//
// 故接口命名为 OrderLifecycle：它冻结的是**能力**（订单从提交到成交的
// 生命周期），不是那个具体 struct。二者由下方编译期检查
// `_ OrderLifecycle = (*OrderManager)(nil)` 钉在一起——既有 struct
// 已经满足本接口（7 个方法全在 *OrderManager 上），从 struct 删任一
// 方法，本包 go build 失败。K1 若要换实现，只要仍满足本接口即可替换。
//
// 七个方法逐字来自 *OrderManager 的现有签名（order_manager.go）。
type OrderLifecycle interface {
	SubmitOrder(order domain.Order) (string, error)
	CancelOrder(orderID string) error
	GetOrder(orderID string) (domain.Order, bool)
	GetOrders() []domain.Order
	GetPendingOrders() []domain.Order
	UpdateOrderStatus(orderID string, status string)
	GetTrades() []domain.Trade
}

// Reconciler 是对账能力（蓝图 §5 exec-engine 行 + §7.2 实盘数据流）。
//
// 现有 pkg/live/reconciliation 已有 4 个接口（BrokerQuerier /
// LocalSnapshotter / AlertDispatcher / ReportPersister）与纯函数
// Reconcile(local, broker, cfg)——它们是**算法侧**契约，本接口是
// **模块侧**契约（一次对账 = 一个报告），K1 用它们拼装实现，
// 此处只引用、不重复定义。
type Reconciler interface {
	Reconcile(ctx context.Context, asOf time.Time) (*ReconReport, error)
}

// ReconReport 是一次对账的结果，即 quant.recon_report 的行契约。
//
// Detail 是 JSONB：对账差异的完整明细（沿用 reconciliation.Discrepancy
// 的 JSON 形状），结构化列只留 DiffCount 供查询与告警 threshold 判断。
type ReconReport struct {
	RunID     string          `json:"run_id"`
	AsOf      time.Time       `json:"as_of"`      // 对账基准时刻
	DiffCount int             `json:"diff_count"` // 差异条数（0 = 账实相符）
	Detail    json.RawMessage `json:"detail"`     // JSONB：差异明细
	CreatedAt time.Time       `json:"created_at"` // 报告生成时刻（墙钟，非 AsOf）
}

// ExecEngine 是执行模块的统一抽象。
//
// 方法语义（冻结）：
//   - Submit：报送订单。**必须**先过风控挂点（risk.RiskEngine.CheckOrder）；
//     被拒则发 msgbus.TopicExecOrderRejected 并返回 error，**不得**把
//     被拒订单送给 broker。返回 broker 侧订单号。
//   - Cancel：撤单；订单不存在或已终态返回 error。
//   - Reconcile：按 asOf 跑一次对账，产出 ReconReport（不负责落库，
//     落库由调用方写 quant.recon_report）。
//   - Broker：返回底层 broker 契约入口（回测 = 模拟撮合，实盘 = 券商适配）。
//
// ⚠️ 分寸（D1 拍板）：**真实券商对接留将来（D1）**。本契约是
// 实盘-ready 的抽象——回测注入模拟撮合、paper 注入 MockTrader、
// 未来注入真实 adapter，调用方一行不动；但 K0 不引入任何券商 SDK。
type ExecEngine interface {
	Submit(ctx context.Context, intent OrderIntent) (orderID string, err error)
	Cancel(ctx context.Context, orderID string) error
	Reconcile(ctx context.Context, asOf time.Time) (*ReconReport, error)
	Broker() Broker
}

// ─── Contract stubs（K1 实现替换，勿在此写实现逻辑） ────────────────

// StandardExecEngine 是 K1 执行引擎实现的契约 stub。
type StandardExecEngine struct{}

// Submit 经过风控挂点后报送订单。
func (e *StandardExecEngine) Submit(ctx context.Context, intent OrderIntent) (string, error) {
	panic("contract stub: not implemented")
}

// Cancel 撤销订单。
func (e *StandardExecEngine) Cancel(ctx context.Context, orderID string) error {
	panic("contract stub: not implemented")
}

// Reconcile 跑一次对账。
func (e *StandardExecEngine) Reconcile(ctx context.Context, asOf time.Time) (*ReconReport, error) {
	panic("contract stub: not implemented")
}

// Broker 返回底层 broker 契约入口。
func (e *StandardExecEngine) Broker() Broker { panic("contract stub: not implemented") }

// StandardReconciler 是 K1 对账实现的契约 stub（底层拼装
// pkg/live/reconciliation 的 BrokerQuerier + LocalSnapshotter + 纯函数
// Reconcile）。
type StandardReconciler struct{}

// Reconcile 按 asOf 跑一次对账并产出报告。
func (r *StandardReconciler) Reconcile(ctx context.Context, asOf time.Time) (*ReconReport, error) {
	panic("contract stub: not implemented")
}

// ─── 编译期合规检查 + 方法存在性守卫 ────────────────────────────────
//
// 前四行：把**既有**实现钉进契约（不改它们一行代码）：
//   - *OrderManager（order_manager.go）满足 OrderLifecycle；
//   - *SimulatedBroker（simulated_broker.go）满足 Broker。
//
// 任一既有 struct 被删方法，本包 go build 失败。
//
// 第五、六行：stub 漂移出接口时 go build 失败（切片 1 既有样板）。
//
// 方法表达式守卫（其后各行）：从 ExecEngine / Reconciler /
// OrderLifecycle / Broker / OrderStore 删除任一方法，本包
// go build 直接编译失败（防「删方法后测试仍绿」）。
var (
	_ OrderLifecycle = (*OrderManager)(nil)
	_ Broker         = (*SimulatedBroker)(nil)

	_ ExecEngine = (*StandardExecEngine)(nil)
	_ Reconciler = (*StandardReconciler)(nil)

	_ func(ExecEngine, context.Context, OrderIntent) (string, error)     = ExecEngine.Submit
	_ func(ExecEngine, context.Context, string) error                    = ExecEngine.Cancel
	_ func(ExecEngine, context.Context, time.Time) (*ReconReport, error) = ExecEngine.Reconcile
	_ func(ExecEngine) Broker                                            = ExecEngine.Broker

	_ func(Reconciler, context.Context, time.Time) (*ReconReport, error) = Reconciler.Reconcile

	_ func(OrderLifecycle, domain.Order) (string, error) = OrderLifecycle.SubmitOrder
	_ func(OrderLifecycle, string) error                 = OrderLifecycle.CancelOrder
	_ func(OrderLifecycle, string) (domain.Order, bool)  = OrderLifecycle.GetOrder
	_ func(OrderLifecycle) []domain.Order                = OrderLifecycle.GetOrders
	_ func(OrderLifecycle) []domain.Order                = OrderLifecycle.GetPendingOrders
	_ func(OrderLifecycle, string, string)               = OrderLifecycle.UpdateOrderStatus
	_ func(OrderLifecycle) []domain.Trade                = OrderLifecycle.GetTrades

	_ func(Broker) error                         = Broker.Connect
	_ func(Broker) error                         = Broker.Disconnect
	_ func(Broker, domain.Order) (string, error) = Broker.SubmitOrder
	_ func(Broker, string) error                 = Broker.CancelOrder
	_ func(Broker, string) (string, error)       = Broker.GetOrderStatus
	_ func(Broker) ([]domain.Position, error)    = Broker.GetPositions
	_ func(Broker) (float64, error)              = Broker.GetAccountBalance

	_ func(OrderStore, context.Context, *OrderRecord) error                          = OrderStore.Save
	_ func(OrderStore, context.Context, string) (*OrderRecord, error)                = OrderStore.Get
	_ func(OrderStore, context.Context, string, OrderStatus) ([]*OrderRecord, error) = OrderStore.List
	_ func(OrderStore, context.Context, string, map[string]interface{}) error        = OrderStore.Update
	_ func(OrderStore, context.Context, string) error                                = OrderStore.Delete
)
