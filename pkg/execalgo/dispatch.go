// K17：exec-algo 下单主路径接线 —— 多父单并发的运行时派发器。
//
// 背景（TASKS.md K17）：K4 交付的 ExecAlgorithm（TWAP/VWAP）此前的状态是
// 「算法本身已交付、全仓零生产引用」。K17 把它接进两条下单主路径：
//
//   - 回测侧 pkg/backtest/exec_algo.go（BacktestExecutionService 撮合前拆单）；
//   - paper 侧 pkg/live/paper_exec_algo.go（LiveTrader 报送前拆单）。
//
// 本文件是两条路径**共享**的运行时：父单生命周期 + 子单路由 + 进度账。
//
// ─── 为什么需要 Dispatcher，而不是调用方各拿一个算法实例 ──────────────
//
// K4 的裁决写在 schedule.go：「一个算法实例同一时刻只服务一个父订单
// （Schedule 覆盖前一份计划）」——planState 是单父单运行态。而下单主路径
// 天然多父单并发（回测一天多个信号、paper 一个策略多个 symbol）⇒ 每个
// 父单一个算法实例，由 Dispatcher 按（父单号 / 子单号）路由 OnBar / OnFill。
//
// ─── 确定性契约（K17 验收 4）────────────────────────────────────────
//
// 子单号由 K4 的 childOrderID（(父单号, 片序号) 纯函数）生成；父单号由
// 调用方以纯函数派生（两条路径都是「日期+symbol+方向」，见各自接线文件
// 的 parentOrderID）。Dispatcher 自身不引入任何时间戳 / 随机 / map 遍历序
// （所有输出路径先排序）：同一输入两次运行，Attach → OnBar → OnFill 全
// 序列逐字节一致。
//
// ─── 父单量一致性（K17 要求 3）──────────────────────────────────────
//
//   - Attach 时复核 Σ 子单计划量 == 父单量（allocateQty 的保证在此
//     fail-loud 二次断言，不等即拒绝挂载）；
//   - OnFill 由**本层**独立记账，累计超过父单量即返回 error（超额
//     fail-loud）。注意：算法内部 planState.onFill 对超额是**钳制不报错**
//     （Executor 语义，见 schedule.go 注释）——超额闸门必须由本层补，
//     这是 K17「超额 fail-loud」要求的落点；
//   - 会话结束仍未报完的父单经 Unfinished() 显式交还调用方裁决。两条
//     路径都裁「撤销」而非「强制撮完」，理由写在各自接线文件。
package execalgo

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// AttachSpec 是「把一个父单挂到 Dispatcher」所需的全部参数。
//
// Slices 由调用方解析为 >=1 后传入：片数未指定时的推导同时决定执行窗口
// 长度（窗口 = n 个自然日），故放在接线层用 ResolveExecWindow 一起做，
// Dispatcher 只接收已解析值。
type AttachSpec struct {
	ParentID   string           // 父单号（调用方纯函数派生；会话内唯一，重复挂载 fail-loud）
	RunID      string           // 归属 run（可空）
	Symbol     string           // 父单标的
	Side       domain.Direction // long / short / close
	Qty        float64          // 父单总量（>0，Schedule 会校验）
	LimitPrice float64          // 0 = 市价（子单继承）
	Style      string           // "twap" / "vwap"；未知值 fail-loud
	Slices     int              // 拆单片数（调用方已解析为 >=1）
	Profile    []float64        // VWAP 成交量权重（原始量，算法内归一化；TWAP 忽略）
	OrderType  domain.OrderType // 子单报送时继承的下单类型（market / limit）
	StartAt    time.Time        // 执行窗口起点（两条路径都取「信号当日」）
	EndAt      time.Time        // 执行窗口终点
}

// ParentProgress 是父单的执行进度快照（Unfinished 的返回元素）。
type ParentProgress struct {
	ParentID string  `json:"parent_id"`
	Symbol   string  `json:"symbol"`
	Filled   float64 `json:"filled"` // Σ 已成交量
	Total    float64 `json:"total"`  // 父单量
}

// DueChild 是到点、待报送的子单：K4 的 ChildOrder + 继承自父单的下单类型。
//
// 为什么带 OrderType：ChildOrder 是 K0 冻结契约（interfaces.go），不为
// 接线便利改它；两条路径都需要知道子单按什么类型报送（market/limit），
// 故在接线层包一层。
type DueChild struct {
	ChildOrder
	OrderType domain.OrderType
}

// Dispatcher 是 exec-algo 的运行时派发器（多父单并发）。
//
// 线程安全：内部 mutex 保护全部可变状态。回测引擎日循环是单线程调用，
// paper 回放同样是同步日循环——加锁是为了让「并发跑多个回测共享一个
// Engine」的场景下每个 run 各持一个 Dispatcher 实例时也绝对安全。
type Dispatcher struct {
	mu       sync.Mutex
	parents  map[string]*parentRun
	childIdx map[string]string // 子单号 → 父单号（OnFill 路由）
}

// parentRun 是一个父单的完整运行态。
type parentRun struct {
	spec      AttachSpec
	algo      ExecAlgorithm
	due       dueSource // algo 具体类型（TWAP/VWAP）才有的 DueChildOrders 能力
	plan      []ChildOrder
	filled    float64 // Σ 已成交量（本层独立记账，超额 fail-loud 判据）
	submitted float64 // Σ 已报送子单量（OnBar 取走即计）
}

// dueSource 是「取走即清空」能力。K4 的 TWAP / VWAP 都有
// DueChildOrders()，但它**不在** ExecAlgorithm 接口里（K0 冻结），K17
// 不改冻结接口签名，故在接线层按具体类型断言（两个实现都满足，见
// twap.go / vwap.go 的编译期守卫）。
type dueSource interface {
	DueChildOrders() []ChildOrder
}

// NewDispatcher 构造空派发器。
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		parents:  make(map[string]*parentRun),
		childIdx: make(map[string]string),
	}
}

// ResolveExecWindow 解析「片数 + 执行窗口」，两条下单路径共用同一口径。
//
// 片数推导：slices>0 用之；否则取 len(profile)（VWAP 场景）；再否则
// 1 片（等价整单，但仍走算法路径——保证 OnFill / 进度 / 会话结束裁决
// 语义一致，不造「有时拆有时不拆」的第二真相）。
//
// 窗口口径（裁决）：日线粒度下窗口 = n 个自然日，故 splitWindow 的步长
// 恰为 1 个自然日、SubmitAt 严格等差且首片落在信号当日。非交易日
// （周末 / 假期）不到点则顺延到下一个有 bar 的日子——这是 K4 文档化的
// bar 粒度效应（planState.onBar 注释：「同一根 bar 内到点的多片会一次
// 性发射」），不是缺陷：计划的时间片仍然均匀，只是执行日按 bar 归并。
func ResolveExecWindow(start time.Time, slices int, profile []float64) (n int, end time.Time) {
	n = slices
	if n <= 0 && len(profile) > 0 {
		n = len(profile)
	}
	if n <= 0 {
		n = 1
	}
	return n, start.AddDate(0, 0, n)
}

// Attach 挂载一个父单：构造算法实例 → Schedule → 登记，返回子单计划。
//
// fail-loud 情形（一律 error，不静默）：
//   - 空 ParentID / 未知 Style / Schedule 非法输入（Qty<=0、窗口非法等）；
//   - 同一 ParentID 重复挂载（会话内父单号必须唯一）；
//   - Σ 子单计划量 != 父单量（allocateQty 保证的复核）；
//   - 子单号在会话内撞车（childOrderID 含父单号，正常不会发生——
//     发生了说明父单号派生规则被破坏）。
func (d *Dispatcher) Attach(ctx context.Context, spec AttachSpec) ([]ChildOrder, error) {
	if spec.ParentID == "" {
		return nil, fmt.Errorf("execalgo: attach requires non-empty parent id")
	}

	var algo ExecAlgorithm
	switch spec.Style {
	case "twap":
		algo = NewTWAP(spec.Slices)
	case "vwap":
		algo = NewVWAP(spec.Slices, spec.Profile)
	default:
		return nil, fmt.Errorf("execalgo: unknown exec algo %q for parent %s (want twap|vwap)", spec.Style, spec.ParentID)
	}
	due, ok := algo.(dueSource)
	if !ok {
		// 编译期已由 twap.go / vwap.go 的守卫保证可达；此处是纵深防御。
		return nil, fmt.Errorf("execalgo: algo %q does not expose DueChildOrders", spec.Style)
	}

	parent := ParentOrder{
		OrderID:    spec.ParentID,
		RunID:      spec.RunID,
		Symbol:     spec.Symbol,
		Side:       spec.Side,
		Qty:        spec.Qty,
		LimitPrice: spec.LimitPrice,
		Style:      spec.Style,
		StartAt:    spec.StartAt,
		EndAt:      spec.EndAt,
	}
	plan, err := algo.Schedule(ctx, parent)
	if err != nil {
		return nil, fmt.Errorf("execalgo: schedule parent %s: %w", spec.ParentID, err)
	}
	if len(plan) == 0 {
		return nil, fmt.Errorf("execalgo: parent %s produced empty plan", spec.ParentID)
	}

	var sum float64
	for i := range plan {
		sum += plan[i].Qty
		if plan[i].Qty <= 0 {
			return nil, fmt.Errorf("execalgo: parent %s slice %d has non-positive qty %v", spec.ParentID, i, plan[i].Qty)
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, dup := d.parents[spec.ParentID]; dup {
		return nil, fmt.Errorf("execalgo: duplicate parent order %s (one algo instance serves one parent; re-attach would orphan the running plan)", spec.ParentID)
	}
	for i := range plan {
		if _, clash := d.childIdx[plan[i].OrderID]; clash {
			return nil, fmt.Errorf("execalgo: child id %s of parent %s collides with an existing parent (parent id derivation must be unique)", plan[i].OrderID, spec.ParentID)
		}
	}
	if diff := math.Abs(sum - spec.Qty); diff > qtySumTolerance(spec.Qty) {
		return nil, fmt.Errorf("execalgo: parent %s plan qty sum %v != parent qty %v (diff %v)", spec.ParentID, sum, spec.Qty, diff)
	}

	run := &parentRun{
		spec:   spec,
		algo:   algo,
		due:    due,
		plan:   plan,
		filled: 0,
	}
	d.parents[spec.ParentID] = run
	for i := range plan {
		d.childIdx[plan[i].OrderID] = spec.ParentID
	}
	// 返回计划副本，防止调用方改写内部状态。
	out := make([]ChildOrder, len(plan))
	copy(out, plan)
	return out, nil
}

// OnBar 喂一根 bar 给派发器，返回本 bar 到点的子单（DueChild）。
//
// 路由规则：只有 symbol 与 bar 一致的父单推进节奏（跨 symbol 的 bar 不
// 互相触发——到点判据在 K4 是纯时间比较，不路由就会串）。返回序列按
// 父单号排序，同一父单内按片序号（map 遍历序随机，输出必须定序）。
//
// 取走语义：子单从算法的 due 缓冲取走即清空（K4 防重复报送设计），
// 同时计入父单的 submitted——调用方收到即视为「已报送」。
func (d *Dispatcher) OnBar(ctx context.Context, bar domain.OHLCV) ([]DueChild, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var out []DueChild
	for _, pid := range d.sortedParentIDsLocked() {
		run := d.parents[pid]
		if run.spec.Symbol != bar.Symbol {
			continue
		}
		if err := run.algo.OnBar(ctx, bar); err != nil {
			return out, fmt.Errorf("execalgo: OnBar parent %s: %w", pid, err)
		}
		for _, c := range run.due.DueChildOrders() {
			run.submitted += c.Qty
			out = append(out, DueChild{ChildOrder: c, OrderType: run.spec.OrderType})
		}
	}
	return out, nil
}

// OnFill 回填一笔子单成交，推进父单进度。
//
//   - 子单号不属本派发器（含非算法路径的成交）→ 忽略返回 nil——一个
//     Dispatcher 不该对管辖外的回报报错（与 planState.onFill 同语义）；
//   - 累计将超过父单量 → error（超额 fail-loud，状态不变）。重复投递
//     同一笔 fill 会在此现形：算法侧 planState 对重复是钳制，但簿记上
//     重复就是超额，K17 要求 fail-loud。
func (d *Dispatcher) OnFill(f Fill) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	pid, ok := d.childIdx[f.OrderID]
	if !ok {
		return nil
	}
	run := d.parents[pid]
	next := run.filled + f.Qty
	if diff := next - run.spec.Qty; diff > qtySumTolerance(run.spec.Qty) {
		return fmt.Errorf("execalgo: parent %s over-filled: filled %v + %v exceeds qty %v (diff %v) — child id %s",
			pid, run.filled, f.Qty, run.spec.Qty, diff, f.OrderID)
	}
	run.filled = next
	return nil
}

// Progress 返回父单的 (已成交量, 父单量, 是否存在)。
func (d *Dispatcher) Progress(parentID string) (filled, total float64, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	run, exists := d.parents[parentID]
	if !exists {
		return 0, 0, false
	}
	return run.filled, run.spec.Qty, true
}

// Unfinished 返回「未全部成交」的父单进度快照，按父单号排序（确定性）。
// 会话结束裁决（撤销/强撮）的输入；正常结束时返回空切片。
func (d *Dispatcher) Unfinished() []ParentProgress {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []ParentProgress
	for _, pid := range d.sortedParentIDsLocked() {
		run := d.parents[pid]
		if run.filled < run.spec.Qty-qtySumTolerance(run.spec.Qty) {
			out = append(out, ParentProgress{
				ParentID: pid,
				Symbol:   run.spec.Symbol,
				Filled:   run.filled,
				Total:    run.spec.Qty,
			})
		}
	}
	return out
}

// Symbols 返回有活跃父单的 symbol 集合（去重、字典序）——调用方据此
// 只喂相关 bar，无活跃父单时零开销。
func (d *Dispatcher) Symbols() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	seen := make(map[string]bool, len(d.parents))
	var out []string
	for _, run := range d.parents {
		if !seen[run.spec.Symbol] {
			seen[run.spec.Symbol] = true
			out = append(out, run.spec.Symbol)
		}
	}
	sort.Strings(out)
	return out
}

// Len 返回活跃父单数。接线层的快路径判空：0 = 本次会话没有任何算法单，
// 日循环里的拆单步骤立即返回（默认路径数值零变化的保证之一）。
func (d *Dispatcher) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.parents)
}

// Plan 返回某父单的子单计划副本（只读，测试/诊断用）。
func (d *Dispatcher) Plan(parentID string) ([]ChildOrder, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	run, ok := d.parents[parentID]
	if !ok {
		return nil, false
	}
	out := make([]ChildOrder, len(run.plan))
	copy(out, run.plan)
	return out, true
}

// sortedParentIDsLocked 返回排序后的父单号（调用方持锁）。
// map 遍历序随机，任何遍历 d.parents 的输出路径都必须先过这里。
func (d *Dispatcher) sortedParentIDsLocked() []string {
	ids := make([]string, 0, len(d.parents))
	for pid := range d.parents {
		ids = append(ids, pid)
	}
	sort.Strings(ids)
	return ids
}

// qtySumTolerance 是数量求和的浮点容差（相对量级）。A 股数量为整数手，
// allocateQty 对整数手逐位精确；这里只防 IEEE754 求和的末位噪声被误判
// 成超额/不等。
func qtySumTolerance(qty float64) float64 {
	return 1e-9 * math.Max(1, math.Abs(qty))
}
