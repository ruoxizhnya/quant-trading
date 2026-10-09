// K4：exec-algo 排程共享件——TWAP / VWAP 复用的纯函数与内存运行态。
//
// 本文件不导出任何新契约类型（K0 冻结的 ExecAlgorithm / ParentOrder /
// ChildOrder 已在 interfaces.go）。此处只放：
//
//  1. 三个**纯函数**：splitWindow（时间片等差切分）、allocateQty（按权重
//     分配数量，Σ 精确）、childOrderID（幂等子单号）；
//  2. planState：一个父订单的内存运行态（计划 + OnBar 游标 + OnFill 进度），
//     TWAP / VWAP 各自 embed 一份，接口方法只做转发。
//
// 裁决（写在此处，TWAP/VWAP 文件引用）：
//   - 时间切分首片落在 StartAt（i=0），步长 = 窗口 / 片数，故 SubmitAt
//     严格等差；末片落在 StartAt + (n-1)*step < EndAt（按片起点报送，
//     而非片终点）。窗口 [StartAt, EndAt] 被切成 n 个等长区间。
//   - 数量分配用「累计目标法」：先 round 各前 n-1 片，最后一片 = 总量 −
//     已分配量。好处：Σ 精确等于总量（整数手时逐位精确），且比例分配
//     不会因 IEEE754（如 1000×0.3 = 299.999…）被 floor 掉 1 手。
//     整数手（A 股数量为整数股）时即「余数并入最后一片」。
package execalgo

import (
	"fmt"
	"math"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// DefaultSliceInterval 是片数未显式指定时，推导片数所用的默认片间隔。
//
// 片数策略（裁决）：构造参数 slices > 0 → 用该片数；slices <= 0 → 按
// 执行窗口时长 / DefaultSliceInterval 向上取整，至少 1 片。5 分钟是
// A 股日内拆单的常见粒度（一整段 09:30–15:00 约 60 片），既不至于每片
// 太小被拒，也不至于一天只发一两笔暴露冲击。
const DefaultSliceInterval = 5 * time.Minute

// validateParent 校验父订单的可排程性。非法输入一律返回 error（fail-loud，
// 对齐 OBS-01：不可排程就拒绝，绝不产空计划）。
func validateParent(parent ParentOrder) error {
	if parent.Symbol == "" {
		return fmt.Errorf("execalgo: parent order %q has empty symbol", parent.OrderID)
	}
	if parent.Qty <= 0 {
		return fmt.Errorf("execalgo: parent order %q qty must be > 0, got %v", parent.OrderID, parent.Qty)
	}
	if !parent.EndAt.After(parent.StartAt) {
		return fmt.Errorf("execalgo: parent order %q end_at (%s) must be after start_at (%s)",
			parent.OrderID, parent.EndAt.Format(time.RFC3339), parent.StartAt.Format(time.RFC3339))
	}
	return nil
}

// deriveSlices 在未显式指定片数时，按窗口时长推导片数。
func deriveSlices(window time.Duration) int {
	if window <= 0 {
		return 1
	}
	n := int(math.Ceil(float64(window) / float64(DefaultSliceInterval)))
	if n < 1 {
		n = 1
	}
	return n
}

// effectiveSlices 防止「片数 > 整数手总量」时产出 0 手子单（券商拒收）。
//
// 仅对整数手总量收缩片数：Qty=2 手要拆 7 片 → 收缩为 2 片，每片 1 手。
// 非整数手不做收缩（分数量本身可按小数均分，不会出现 0）。这是**静默
// 修正的可观测例外**：调用方给定了片数但被下调，故 Schedule 的返回计划
// 长度即为最终片数（自证可观测）。
func effectiveSlices(n int, qty float64) int {
	if qty == math.Trunc(qty) && qty >= 1 && float64(n) > qty {
		return int(qty)
	}
	return n
}

// splitWindow 把 [start, end] 切成 n 个等长区间，返回每片**起点**作为
// SubmitAt。step = (end-start)/n，故相邻 SubmitAt 之差恒为 step（严格
// 等差 —— TWAP 均匀性的验收判据）。
func splitWindow(start, end time.Time, n int) []time.Time {
	out := make([]time.Time, n)
	if n <= 0 {
		return out
	}
	step := end.Sub(start) / time.Duration(n)
	for i := 0; i < n; i++ {
		out[i] = start.Add(time.Duration(i) * step)
	}
	return out
}

// allocateQty 按权重把 total 分配到 n 片，保证 Σ == total。
//
// weights 长度 == n，通常已归一化（Σ weights == 1）。整数手总量下：
// 前 n-1 片取 round(total*weight_i)，末片 = total − Σ(前 n-1)（余数并入
// 最后一片），故逐位精确；非整数手总量下前 n-1 片取 total*weight_i，
// 末片补差，Σ 精确到浮点舍入。
//
// 末片理论上可能因 round 溢出为负（权重和 > 1 的病态输入），此时被
// 归零并记为 0 —— 但 validateParent 已挡掉 Qty<=0，且 allocateQty 只接受
// 归一化权重，正常路径不会触发。
func allocateQty(total float64, weights []float64) []float64 {
	n := len(weights)
	out := make([]float64, n)
	if n == 0 {
		return out
	}
	if n == 1 {
		out[0] = total
		return out
	}
	integral := total == math.Trunc(total)
	var acc float64
	for i := 0; i < n-1; i++ {
		q := total * weights[i]
		if integral {
			q = math.Round(q)
		}
		out[i] = q
		acc += q
	}
	last := total - acc
	if last < 0 {
		last = 0
	}
	out[n-1] = last
	return out
}

// childOrderID 是子订单号的幂等生成器：同一 (父单号, 片序号) 永远得到
// 同一串 ID。**禁止随机数 / UUID 随机段** —— 幂等是本切片验收要求，也是
// 风控去重、回报归并的前提（重放 Schedule 不得产出「新的」子单）。
func childOrderID(parentID string, i int) string {
	return fmt.Sprintf("%s-C%04d", parentID, i)
}

// buildPlan 由数量、时间两组切片装配 ChildOrder 计划。
func buildPlan(parent ParentOrder, qtys []float64, times []time.Time) []ChildOrder {
	n := len(qtys)
	if len(times) < n {
		n = len(times)
	}
	plan := make([]ChildOrder, n)
	for i := 0; i < n; i++ {
		plan[i] = ChildOrder{
			OrderID:    childOrderID(parent.OrderID, i),
			ParentID:   parent.OrderID,
			RunID:      parent.RunID,
			Symbol:     parent.Symbol,
			Side:       parent.Side,
			Qty:        qtys[i],
			LimitPrice: parent.LimitPrice,
			SubmitAt:   times[i],
		}
	}
	return plan
}

// ─── planState：一个父订单的内存运行态 ──────────────────────────────

// planState 持有 Schedule 产出的计划、OnBar 推进的发射游标、OnFill 累计的
// 成交进度。TWAP / VWAP 各 embed 一份。
//
// 生命周期：Schedule 重置全部字段；OnBar 推进发射；OnFill 累计进度。
// 一个算法实例同一时刻只服务**一个**父订单（Schedule 覆盖前一份计划）——
// 这是本切片的裁决；多父单并发是 K5/后续接线的事，本切片只交付算法本身。
type planState struct {
	parent   ParentOrder
	plan     []ChildOrder
	childIDs map[string]bool // 本计划内的子单号集合，OnFill 归属判定用
	nextIdx  int             // 下一个待发射片的序号
	due      []ChildOrder    // 已到点、待取走的子单
	filled   float64         // 累计已成交量
	total    float64         // 父单总量（Σ plan.Qty）
	done     bool
}

// reset 用新的父订单与计划覆盖运行态（Schedule 调用）。
func (s *planState) reset(parent ParentOrder, plan []ChildOrder) {
	s.parent = parent
	s.plan = plan
	s.childIDs = make(map[string]bool, len(plan))
	s.total = 0
	for i := range plan {
		s.childIDs[plan[i].OrderID] = true
		s.total += plan[i].Qty
	}
	s.nextIdx = 0
	s.due = nil
	s.filled = 0
	s.done = false
}

// onBar 推进拆单节奏：把 SubmitAt <= bar.Date 的未发射片全部推入 due 缓冲。
//
// 到点判据用 `!SubmitAt.After(bar.Date)`（即 SubmitAt <= bar.Date），
// 精确到 bar 粒度：回测逐日 bar 时同一根 bar 内到点的多片会一次性发射，
// 由调用方逐片过风控（UC4）。未 Schedule 即 OnBar → error（fail-loud）。
func (s *planState) onBar(bar domain.OHLCV) error {
	if s.parent.OrderID == "" {
		return fmt.Errorf("execalgo: OnBar called before Schedule (no parent order scheduled)")
	}
	for s.nextIdx < len(s.plan) && !s.plan[s.nextIdx].SubmitAt.After(bar.Date) {
		s.due = append(s.due, s.plan[s.nextIdx])
		s.nextIdx++
	}
	return nil
}

// dueChildOrders 取走并清空待报送子单（取走即清空，对齐
// strategy.BarHandler.Signals() 的语义）。取走语义的理由：子单只应报送
// 一次，若保留在缓冲内，调用方两轮循环读到同一片就会**重复报送**。
func (s *planState) dueChildOrders() []ChildOrder {
	if len(s.due) == 0 {
		return nil
	}
	out := s.due
	s.due = nil
	return out
}

// onFill 累计成交进度。
//
// 归属判定：portfolio.Fill **没有** ParentID 字段，故以 Fill.OrderID
// 是否 ∈ 本计划子单号集合 判定是否属于本父单（回链的锚点是子单号——
// 见 interfaces.go ChildOrder.ParentID 的设计意图）。不属于本计划的
// 回报被忽略（返回 nil）：一个算法实例可能收到别的父单的回报，报错会
// 让上游误以为本报废。
//
// 超额 fill（含同一子单重复投递）不 panic：filled 被**钳制**在 [0,total]，
// 到 total 即 done。去重是 portfolio.ApplyFill 的职责（按 OrderID 幂等，
// 见 pkg/portfolio/interfaces.go），exec-algo 侧只做进度监控，不重复记账。
func (s *planState) onFill(f Fill) error {
	if !s.childIDs[f.OrderID] {
		return nil
	}
	s.filled += f.Qty
	if s.filled > s.total {
		s.filled = s.total
	}
	if s.filled >= s.total {
		s.done = true
	}
	return nil
}

// progress 返回 (已成交量, 总量, 是否全部成交)。未 Schedule 时为 (0,0,false)。
func (s *planState) progress() (float64, float64, bool) {
	return s.filled, s.total, s.done
}
