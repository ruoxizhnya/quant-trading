// portfolio 契约（K0 切片 2）—— 本文件只冻结接口，不含实现逻辑。
//
// 模块职责（蓝图 §5 portfolio 行）：持仓 / 现金 / 净值。
//   - 收 exec.fill（成交回报）→ ApplyFill 更新持仓与现金 → 发
//     msgbus.TopicPortfolioUpdated；
//   - 快照落 quant.portfolio_snapshot，持仓落 quant.positions。
//
// 本包已有的纯计算原语（ComputeFees / UpdateAvgCost，见 portfolio.go）
// 保持不动：它们是 ApplyFill 内部要调用的公式，不是契约。
//
// K0 切片 2：stub 方法体固定 panic("contract stub: not implemented")，
// K1 实现直接替换 stub。
package portfolio

import (
	"context"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// Fill 是成交回报事件——exec-engine 产出、portfolio 消费的最小契约。
//
// ─── 裁决：为什么不用现成的 domain.Trade ──────────────────────────
// domain.Trade（pkg/domain/backtest.go:31）看着像等价类型，但**不是**：
//  1. 它携带 Commission / TransferFee / StampTax 三项费用——费用是
//     portfolio 自己按 pkg/fees 算出来的**产出**，用它当 Fill 会把
//     「先有成交、再算费」的因果倒置，并强迫 exec-engine 替 portfolio
//     定价；
//  2. 它的 ID 是成交 id，没有 OrderID 字段，无法回链订单生命周期
//     （quant.orders ↔ quant.fills 要对得上）；
//  3. 它住在 pkg/domain/backtest.go——回测账目类型，而 Fill 是回测 /
//     实盘**共用**的成交事件契约（实盘不该依赖 backtest 的类型）。
//
// 故在本文件定义窄契约 Fill（不含费用）：费用在 ApplyFill 内部按
// pkg/fees 计算后再计入现金。可复用的 domain 词汇仍复用——Side 用
// domain.Direction（"long"/"short"/"close"），不另造字符串枚举。
type Fill struct {
	OrderID string           `json:"order_id"` // 回链 quant.orders.order_id
	Symbol  string           `json:"symbol"`
	Side    domain.Direction `json:"side"`
	Qty     float64          `json:"qty"`   // 成交数量（>0）
	Price   float64          `json:"price"` // 成交均价
	Ts      time.Time        `json:"ts"`    // 成交时间：回测=数据时间，实盘=回报时间
}

// Snapshot 是组合在某一时刻的快照，即 quant.portfolio_snapshot 的行契约。
//
// ─── 裁决：为什么不用现成的 domain.Portfolio ──────────────────────
// domain.Portfolio（pkg/domain/types.go:99）是可变的**内存状态**载体
// （Positions map + DailyReturn），且缺 run_id，不适合做落库行的契约。
// 但它的组成被逐项复用：NAV ← TotalValue、Cash ← Cash、Positions ←
// []domain.Position、Ts ← UpdatedAt。唯一新增的是 RunID——快照必须可
// 归因到某次 run（每张 quant.* 表都有 run_id）。
type Snapshot struct {
	RunID     string            `json:"run_id"` // 归属 run（回测/实盘同构，一次 run 一个 id）
	Ts        time.Time         `json:"ts"`     // 快照时刻：回测=VirtualClock 当前，实盘=墙钟
	NAV       float64           `json:"nav"`    // 组合净值 = 现金 + 持仓市值
	Cash      float64           `json:"cash"`
	Positions []domain.Position `json:"positions"`
}

// Portfolio 是组合状态的统一抽象（蓝图 §5 portfolio 行）。
//
// 方法语义（冻结）：
//   - ApplyFill：成交后更新持仓与现金（**唯一**的持仓写入口，禁止他处
//     改持仓——数据归属铁律）。费用在本方法内部按 pkg/fees 计算并计入
//     现金，故 Fill 本身不带费用。必须幂等：同一 OrderID 重复投递不得
//     二次改持仓（实盘回报可能重发）。
//   - Value：返回组合净值（现金 + 持仓市值）。
//   - Positions：返回当前全部持仓。
//   - Snapshot：产出落库快照（quant.portfolio_snapshot）；调用方负责
//     写入，本接口不负责 DB I/O（保持模块可测）。
type Portfolio interface {
	ApplyFill(ctx context.Context, f Fill) error
	Value(ctx context.Context) (float64, error)
	Positions(ctx context.Context) ([]domain.Position, error)
	Snapshot(ctx context.Context) (*Snapshot, error)
}

// ─── Contract stubs（K1 实现替换，勿在此写实现逻辑） ────────────────

// StandardPortfolio 是 K1 组合实现的契约 stub。
type StandardPortfolio struct{}

// ApplyFill 成交后更新持仓与现金（幂等）。
func (p *StandardPortfolio) ApplyFill(ctx context.Context, f Fill) error {
	panic("contract stub: not implemented")
}

// Value 返回组合净值。
func (p *StandardPortfolio) Value(ctx context.Context) (float64, error) {
	panic("contract stub: not implemented")
}

// Positions 返回当前全部持仓。
func (p *StandardPortfolio) Positions(ctx context.Context) ([]domain.Position, error) {
	panic("contract stub: not implemented")
}

// Snapshot 产出组合快照（quant.portfolio_snapshot 的行）。
func (p *StandardPortfolio) Snapshot(ctx context.Context) (*Snapshot, error) {
	panic("contract stub: not implemented")
}

// ─── 编译期合规检查 + 方法存在性守卫 ────────────────────────────────
//
// 方法表达式守卫：从 Portfolio 接口删除任一方法，本包 go build 直接
// 编译失败（防「删方法后测试仍绿」）。
var (
	_ Portfolio = (*StandardPortfolio)(nil)

	_ func(Portfolio, context.Context, Fill) error                = Portfolio.ApplyFill
	_ func(Portfolio, context.Context) (float64, error)           = Portfolio.Value
	_ func(Portfolio, context.Context) ([]domain.Position, error) = Portfolio.Positions
	_ func(Portfolio, context.Context) (*Snapshot, error)         = Portfolio.Snapshot
)
