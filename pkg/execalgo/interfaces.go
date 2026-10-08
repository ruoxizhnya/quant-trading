// Package execalgo — exec-algo 模块（K0 切片 2 契约冻结，全新包）。
//
// 模块职责（蓝图 §5 exec-algo 行）：执行算法（TWAP / VWAP / 拆单），
// **与策略同构 trait**——对照 nautilus
// crates/trading/src/algorithm/：算法不是策略的下属，而是和策略一样
// 吃 bar 事件、吃成交回报、产订单的对象。
//
// 数据流（蓝图 §8 UC4）：收父订单 → 拆子订单（OnBar 按时间片切）→
// **每片子订单都过风控挂点** risk.RiskEngine.CheckOrder → broker 报送
// → fill 回报 → OnFill 更新进度 → 全部成交 → portfolio.ApplyFill。
// 子订单发 msgbus.TopicExecAlgoChildOrder。
//
// 本模块无 DB 归属（状态在内存 + quant.orders），故 7 张 quant.* 表里
// 没有 exec-algo 的表。
//
// **K4 实现 TWAP / VWAP + 市场冲击模型（D5 拍板）**：本切片只冻结
// trait，不实现任何算法。
//
// K0 切片 2：stub 方法体固定 panic("contract stub: not implemented")，
// K4 实现直接替换 stub。
package execalgo

import (
	"context"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/portfolio"
)

// Fill 是成交回报事件——与 portfolio 共用同一契约
// （pkg/portfolio/interfaces.go）。
//
// 以**别名**引用而非重新定义：父订单拆出的子订单成交后，exec-algo 与
// portfolio 看到的是同一个 fill 事件；定义两处必然漂移。依赖方向
// execalgo → portfolio 不成环（portfolio 不依赖 execalgo）。
type Fill = portfolio.Fill

// ParentOrder 是父订单——策略产出的「要买/卖多少」的大单，算法把它
// 拆成若干子订单。
type ParentOrder struct {
	OrderID    string           `json:"order_id"` // 父订单号（回链 quant.orders）
	RunID      string           `json:"run_id"`   // 归属 run
	Symbol     string           `json:"symbol"`
	Side       domain.Direction `json:"side"`
	Qty        float64          `json:"qty"`         // 总数量
	LimitPrice float64          `json:"limit_price"` // 0 = 不限价（跟随市价）
	Style      string           `json:"style"`       // 拆单算法："" / "twap" / "vwap"
	StartAt    time.Time        `json:"start_at"`    // 执行窗口起点
	EndAt      time.Time        `json:"end_at"`      // 执行窗口终点（TWAP 按此均分）
}

// ChildOrder 是子订单——算法按时间片 / 成交量分布拆出的每一笔，
// 逐笔过风控后交给 exec-engine。
type ChildOrder struct {
	OrderID    string           `json:"order_id"`  // 子订单号（由算法生成，须幂等）
	ParentID   string           `json:"parent_id"` // 回链 ParentOrder.OrderID
	RunID      string           `json:"run_id"`
	Symbol     string           `json:"symbol"`
	Side       domain.Direction `json:"side"`
	Qty        float64          `json:"qty"`         // 本片数量
	LimitPrice float64          `json:"limit_price"` // 0 = 市价
	SubmitAt   time.Time        `json:"submit_at"`   // 计划报送时刻（由 Schedule 排定）
}

// ExecAlgorithm 是执行算法的统一抽象（与策略同构 trait）。
//
// 方法语义（冻结）：
//   - Name：算法名（"twap" / "vwap" / ...），注册表键。
//   - OnBar：逐 bar 回调（与 strategy.BarHandler.OnBar 同构）——算法按
//     bar 推进拆单节奏：到点了就产出该片的子订单。
//   - OnFill：子订单成交回报，更新父订单的执行进度；全部成交后算法
//     自行了结（成交结果本身由 portfolio.ApplyFill 记账，两边不重复写）。
//   - Schedule：把父订单一次性排成子订单计划（时间片 / 量分布）。
//     纯排程：不发单、不落库，返回的计划由调用方逐片过风控后报送。
//     无法排程（如窗口非法、Qty ≤ 0）返回 error。
type ExecAlgorithm interface {
	Name() string
	OnBar(ctx context.Context, bar domain.OHLCV) error
	OnFill(ctx context.Context, f Fill) error
	Schedule(ctx context.Context, parent ParentOrder) ([]ChildOrder, error)
}

// ─── Contract stubs（K4 实现替换，勿在此写实现逻辑） ────────────────

// BaseExecAlgorithm 是 K4 执行算法实现的契约 stub（TWAP / VWAP +
// 市场冲击模型，D5）。
type BaseExecAlgorithm struct{}

// Name 返回算法名（注册表键）。
func (a *BaseExecAlgorithm) Name() string { panic("contract stub: not implemented") }

// OnBar 逐 bar 推进拆单节奏。
func (a *BaseExecAlgorithm) OnBar(ctx context.Context, bar domain.OHLCV) error {
	panic("contract stub: not implemented")
}

// OnFill 成交回报，更新父订单执行进度。
func (a *BaseExecAlgorithm) OnFill(ctx context.Context, f Fill) error {
	panic("contract stub: not implemented")
}

// Schedule 把父订单排成子订单计划（纯排程，不发单）。
func (a *BaseExecAlgorithm) Schedule(ctx context.Context, parent ParentOrder) ([]ChildOrder, error) {
	panic("contract stub: not implemented")
}

// ─── 编译期合规检查 + 方法存在性守卫 ────────────────────────────────
//
// 第一行：stub 漂移出接口时 go build 失败（切片 1 既有样板）。
//
// 方法表达式守卫（其后各行）：从 ExecAlgorithm 删除任一方法，
// ExecAlgorithm.<Method> 即未定义，本包 go build 直接编译失败。
var (
	_ ExecAlgorithm = (*BaseExecAlgorithm)(nil)

	_ func(ExecAlgorithm) string                                              = ExecAlgorithm.Name
	_ func(ExecAlgorithm, context.Context, domain.OHLCV) error                = ExecAlgorithm.OnBar
	_ func(ExecAlgorithm, context.Context, Fill) error                        = ExecAlgorithm.OnFill
	_ func(ExecAlgorithm, context.Context, ParentOrder) ([]ChildOrder, error) = ExecAlgorithm.Schedule
)
