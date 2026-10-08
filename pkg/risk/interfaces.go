// risk-engine 契约（K0 切片 2）—— 本文件只冻结接口，不含实现逻辑。
//
// 模块职责（蓝图 §5 risk-engine 行）：订单前置风控（仓位 / 止损 / 制度）
// + 盘后 regime 判定。
//   - 收 msgbus.TopicExecOrderIntent → CheckOrder → 裁决 → 发
//     msgbus.TopicRiskOrderVerdict（放行 / 拒绝）；
//   - 裁决记录落 quant.risk_events。
//
// 冻结现有 domain.RiskManager 三方法（签名逐字对齐 pkg/domain/types.go
// 的 RiskManager，本包 *RiskManager 已实现之，见下方编译期检查）。
//
// K0 切片 2：stub 方法体固定 panic("contract stub: not implemented")，
// K1 实现直接替换 stub。
package risk

import (
	"context"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// OrderIntent 是订单意图——策略 / 执行算法产出、报送前的「待裁决」订单。
// 它是 risk-engine 的输入契约，也是 exec-engine.Submit 的输入契约
// （pkg/live 以别名引用本类型，单一事实源）。
//
// 字段与 domain.Order 对齐但更窄：不含成交结果（FilledQty / FillPrice /
// Status）——意图是「想做什么」，结果是「做成了什么」，后者归 exec-engine。
type OrderIntent struct {
	OrderID   string           `json:"order_id"` // 调用方生成的幂等键，回链 quant.orders
	RunID     string           `json:"run_id"`   // 归属 run（落 quant.risk_events）
	Symbol    string           `json:"symbol"`
	Side      domain.Direction `json:"side"`       // long / short / close
	OrderType domain.OrderType `json:"order_type"` // market / limit / stop / trailing
	Qty       float64          `json:"qty"`
	Price     float64          `json:"price"` // 限价单的限价；市价单为 0
	Ts        time.Time        `json:"ts"`    // 意图产生时刻（回测=数据时间，实盘=墙钟）
}

// Verdict 是 CheckOrder 的裁决结果。
//
// **fail-closed 语义（冻结）**：零值 Verdict{} 的 Allowed 为 false——
// 未显式放行一律视为拒绝。风控代码任何「忘了填 Allowed」的分支都落在
// 安全侧，而不是放行侧。由 interfaces_compliance_test.go 的
// TestVerdictFailClosed 强制。
type Verdict struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"` // 拒绝原因（Allowed=true 时可为空）
}

// RiskEngine 是风控模块的统一抽象。
//
// 前三个方法是**冻结现有** domain.RiskManager（pkg/domain/types.go:175）
// 的三方法，签名逐字对齐：CalculatePosition / DetectRegime / CheckStopLoss。
// 本包既有的 *RiskManager（manager.go）已实现之，由下方编译期检查钉住。
//
// CheckOrder 是**新增**的订单报送前挂点（蓝图 §5 risk-engine 行：
// order_intent → risk.verdict；数据流见蓝图 §7.1 / §7.2）：
// 任何订单（含 exec-algo 拆出的每一片子订单，见 UC4）在报送 broker
// 之前必须先过这里。
//
// ⚠️ 分寸（D1 拍板）：K0 只冻结挂点契约，**实盘级合规规则留将来
// （D1）**——如涨跌停、T+1、持仓集中度、自成交等制度的落地属 K1+ 的
// 实现范围，本契约不预先规定规则集。
type RiskEngine interface {
	CalculatePosition(ctx context.Context, signal domain.Signal, portfolio *domain.Portfolio, regime *domain.MarketRegime, currentPrice float64, ohlcv []domain.OHLCV) (domain.PositionSize, error)
	DetectRegime(ctx context.Context, ohlcv []domain.OHLCV) (*domain.MarketRegime, error)
	CheckStopLoss(ctx context.Context, positions []domain.Position, prices map[string]float64) ([]domain.StopLossEvent, error)
	CheckOrder(ctx context.Context, intent OrderIntent) (Verdict, error)
}

// ─── Contract stubs（K1 实现替换，勿在此写实现逻辑） ────────────────

// StandardRiskEngine 是 K1 风控实现的契约 stub。
type StandardRiskEngine struct{}

// CalculatePosition 计算仓位大小（冻结自 domain.RiskManager）。
func (r *StandardRiskEngine) CalculatePosition(ctx context.Context, signal domain.Signal, portfolio *domain.Portfolio, regime *domain.MarketRegime, currentPrice float64, ohlcv []domain.OHLCV) (domain.PositionSize, error) {
	panic("contract stub: not implemented")
}

// DetectRegime 判定市场状态（冻结自 domain.RiskManager）。
func (r *StandardRiskEngine) DetectRegime(ctx context.Context, ohlcv []domain.OHLCV) (*domain.MarketRegime, error) {
	panic("contract stub: not implemented")
}

// CheckStopLoss 检查止损 / 止盈触发（冻结自 domain.RiskManager）。
func (r *StandardRiskEngine) CheckStopLoss(ctx context.Context, positions []domain.Position, prices map[string]float64) ([]domain.StopLossEvent, error) {
	panic("contract stub: not implemented")
}

// CheckOrder 订单报送前的风控挂点：放行 / 拒绝。
func (r *StandardRiskEngine) CheckOrder(ctx context.Context, intent OrderIntent) (Verdict, error) {
	panic("contract stub: not implemented")
}

// ─── 编译期合规检查 + 方法存在性守卫 ────────────────────────────────
//
// 第一行：既有 *RiskManager（manager.go）必须继续满足冻结的
// domain.RiskManager——从它删方法，本包 go build 失败。
//
// 第二、三行：stub 漂移出接口时 go build 失败（切片 1 既有样板）。
//
// 方法表达式守卫（其后各行）：从 RiskEngine 删除任一方法，
// RiskEngine.<Method> 即未定义，本包 go build 直接编译失败——这是
// K0 验收破坏验证腿 a（删 CheckOrder → 红）的护栏。
var (
	_ domain.RiskManager = (*RiskManager)(nil)

	_ RiskEngine         = (*StandardRiskEngine)(nil)
	_ domain.RiskManager = (*StandardRiskEngine)(nil)

	_ func(RiskEngine, context.Context, domain.Signal, *domain.Portfolio, *domain.MarketRegime, float64, []domain.OHLCV) (domain.PositionSize, error) = RiskEngine.CalculatePosition
	_ func(RiskEngine, context.Context, []domain.OHLCV) (*domain.MarketRegime, error)                                                                 = RiskEngine.DetectRegime
	_ func(RiskEngine, context.Context, []domain.Position, map[string]float64) ([]domain.StopLossEvent, error)                                        = RiskEngine.CheckStopLoss
	_ func(RiskEngine, context.Context, OrderIntent) (Verdict, error)                                                                                 = RiskEngine.CheckOrder
)
