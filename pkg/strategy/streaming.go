// strategy-runtime 流式契约（K0 切片 2）—— 本文件只冻结接口，
// 不含实现逻辑，**不改动** pkg/strategy/interfaces.go（批式 SignalGenerator
// 那 4 个子接口原地不动）。
//
// 双模式并存（蓝图 §6.2 + docs/SPEC.md「Streaming Strategy Interface —
// BarHandler」节，本接口与之逐字对齐）：
//
//	┌──────────────┬───────────────────────┬──────────────────────────┐
//	│              │ 批式 SignalGenerator   │ 流式 BarHandler（本文件） │
//	├──────────────┼───────────────────────┼──────────────────────────┤
//	│ 服务层        │ L0 / L1（横截面）      │ L2 / L3（有状态）         │
//	│ 回测驱动      │ VirtualClock 攒窗口    │ VirtualClock 逐 bar       │
//	│ 实盘驱动      │ feed 攒窗口            │ feed bar 到达             │
//	│ 状态          │ 无（窗口纯函数 DAG）    │ 有，SaveState/LoadState   │
//	│ 落地          │ quant.strategies      │ quant.strategy_state      │
//	└──────────────┴───────────────────────┴──────────────────────────┘
//
// 引擎启动时检测策略实现了哪个接口，**自动选执行模式**，策略不声明模式：
//   - 批式策略 → 流式引擎：引擎按 Warmup() 攒够窗口，每逢 bar 到达调
//     GenerateSignals——批式策略不做改动就能上实盘；
//   - 流式策略 → 回测：VirtualClock 驱动数据迭代器逐 bar 喂 OnBar，
//     与实盘逐字同一段代码。
//
// 这就是回测-实盘同构：**同一份策略代码，回测与实盘只换三个实现**——
// Clock（VirtualClock / LiveClock）、DataSource（PG 快照 / 推送 feed）、
// Broker（模拟撮合 / 真实券商），策略本身一行不动。
//
// K0 切片 2：stub 方法体固定 panic("contract stub: not implemented")，
// K2 实现直接替换 stub。
package strategy

import (
	"context"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// BarHandler 是流式策略接口——服务 L2 / L3 有状态策略。
//
// ─── 裁决：为什么 OnBar 收 domain.OHLCV 而不是 domain.Bar ──────────
// docs/SPEC.md §Streaming Strategy Interface 与蓝图 §6.2 写的形参是
// domain.Bar，但 **domain.Bar 并不存在**（pkg/domain 与 pkg/domain/market
// 均无此类型；全仓库 grep 无 `domain.Bar` 引用），它是目标形态里的
// 未建类型。故按任务的 fallback 规则用 domain.OHLCV——它已是本项目
// 唯一的 bar 载体（pkg/domain/market/types.go:6，domain.OHLCV 是它的
// 别名，二者同一类型）。
//
// K2 若真要引入 domain.Bar（例如为 bar 加上调整因子 / 复权标记），
// 属**改冻结契约**，须走变更评审并同步改本签名与全部调用方。
type BarHandler interface {
	// OnBar 逐 bar 回调，&mut self 语义（实现持有内部状态，逐 bar 递推）。
	// 每根 bar 只回调一次，按时间升序；不得在回调内回看未来 bar
	//（前视 = 回测结果失真，fail-loud）。
	OnBar(ctx context.Context, bar domain.OHLCV) error
	// Warmup 声明需要多少根历史 bar 才能产出首个有效信号——**静态推导**，
	// 不是运行时试探。引擎据此预取历史窗口（ADR-028 §8：warmup 由算子
	// 声明推导，引擎自动多取）。返回 0 表示无需预热。
	Warmup() int
	// SaveState 序列化内部状态，落 quant.strategy_state（断点续跑 /
	// 回测-实盘迁移）。状态必须不含指针 / 连接等不可序列化物。
	SaveState() ([]byte, error)
	// LoadState 反序列化状态；不合法的输入返回 error（不静默重置为
	// 初始态——静默重置会让「断点续跑」变成「悄悄从头跑」，结果错了
	// 还查不出来）。
	LoadState([]byte) error

	// Signals 取走自上次调用以来 OnBar 产生的全部信号（**取走即清空**）。
	//
	// ─── 裁决：为什么是「取走」而不是「只读快照」 ──────────────────
	// 引擎在每根 bar 喂完后调用并消费返回值。若只提供只读快照，同一批
	// 信号会被重复取到（调用方忘了去重、或引擎与另一处调用点各取一次），
	// 后果是同一根 bar 的信号被重复下单——而且重复发生在撮合侧，离信号
	// 产生点很远，极难查。取走语义把「谁消费了这批信号」钉进契约本身：
	// 取走即清空，重复调用拿到空切片。防重复消费是**接口属性**，不靠
	// 调用方纪律（纪律会忘，接口不会）。
	//
	// 返回切片的所有权移交给调用方：实现方在取走后不得再引用它。
	Signals() []Signal
}

// ─── Contract stubs（K2 实现替换，勿在此写实现逻辑） ────────────────

// StreamingStrategy 是 K2 流式策略基座的契约 stub。
type StreamingStrategy struct{}

// OnBar 逐 bar 回调（&mut self，持内部状态）。
func (s *StreamingStrategy) OnBar(ctx context.Context, bar domain.OHLCV) error {
	panic("contract stub: not implemented")
}

// Warmup 返回产出首个有效信号所需的历史 bar 数。
func (s *StreamingStrategy) Warmup() int { panic("contract stub: not implemented") }

// SaveState 序列化内部状态（落 quant.strategy_state）。
func (s *StreamingStrategy) SaveState() ([]byte, error) { panic("contract stub: not implemented") }

// LoadState 反序列化状态；不合法输入返回 error。
func (s *StreamingStrategy) LoadState(b []byte) error { panic("contract stub: not implemented") }

// Signals 取走自上次调用以来积累的信号（取走即清空）。
func (s *StreamingStrategy) Signals() []Signal { panic("contract stub: not implemented") }

// ─── 编译期合规检查 + 方法存在性守卫 ────────────────────────────────
//
// 第一行：stub 漂移出接口时 go build 失败（切片 1 既有样板）。
//
// 方法表达式守卫（其后各行）：从 BarHandler 删除任一方法，
// BarHandler.<Method> 即未定义，本包 go build 直接编译失败——这是
// K0 验收破坏验证腿 b（删 Warmup → 红）的护栏。
var (
	_ BarHandler = (*StreamingStrategy)(nil)

	_ func(BarHandler, context.Context, domain.OHLCV) error = BarHandler.OnBar
	_ func(BarHandler) int                                  = BarHandler.Warmup
	_ func(BarHandler) ([]byte, error)                      = BarHandler.SaveState
	_ func(BarHandler, []byte) error                        = BarHandler.LoadState
	_ func(BarHandler) []Signal                             = BarHandler.Signals
)
