// strategy-runtime 流式运行时基座（K2 切片 1）。
//
// 本文件实现两个执行器，落 blueprint §6.2「双模式与同构桥」与
// docs/SPEC.md「Streaming Strategy Interface — BarHandler」：
//
//   - StreamRunner —— 用 clock.Clock 逐 bar 驱动任意 BarHandler。
//     回测里时钟是 VirtualClock（数据驱动推进），实盘里是 LiveClock
//     （墙钟自走）；同一段 Run 代码两种模式逐字复用，这就是
//     「回测-实盘同构」的一半（另一半是 DataSource / Broker 的替换）。
//
//   - BatchAdapter —— 把批式 SignalGenerator 包成 BarHandler 的
//     **同构桥**。批式策略（L0/L1，横截面，无状态）不改一行就能进
//     流式引擎：适配器按 Warmup() 声明攒够滚动窗口，窗口满后每逢 bar
//     到达把「最近 window 根」的快照喂给 GenerateSignals。
//
// 本切片**不接 backtest engine**（切片 2）、**不落 DB**（quant.strategy_state
// 的写入留到接线切片）——这里只做运行时驱动 + 状态序列化，并用
// ADR-028 §7「Batch ≡ Step ≡ Step-from-persisted」三路一致性测试把
// 同构属性钉成机器可验证的护栏。
//
// 冻结契约（本切片一字不改）：
//   - pkg/strategy/streaming.go —— BarHandler 接口定义与守卫；
//   - pkg/strategy/interfaces.go —— 批式 SignalGenerator；
//   - pkg/clock/interfaces.go —— Clock / VirtualClock。
package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ─── StreamRunner：逐 bar 执行器 ─────────────────────────────────────

// StreamRunner 按输入顺序逐根驱动 BarHandler，每根 bar 先推进时钟再回调。
//
// 无状态——同一 StreamRunner 可复用于多路回测，逐 bar 的状态活在
// BarHandler 里（&mut self 语义），不在 runner 里。故零值
// StreamRunner{} 与 NewStreamRunner() 完全等价，两种写法都可以用。
type StreamRunner struct{}

// NewStreamRunner 建一个流式执行器（等价于 &StreamRunner{}，只为装配处
// 可读性存在）。
func NewStreamRunner() *StreamRunner { return &StreamRunner{} }

// Run 逐根执行 bars。
//
// 顺序与可复现性（冻结语义）：
//   - 严格按 **输入序** 遍历，每根先 clk.Advance(bar.Date) 再 h.OnBar。
//     同一份 (bars, clk 起点) 输入两次跑，喂给 OnBar 的序列与推进的
//     时间序列逐点一致——回测确定性由此保证。
//   - bars 的时间字段取自 domain.OHLCV.Date（pkg/domain/market/types.go:8，
//     本项目唯一的 bar 时间列；无 domain.Bar 类型）。
//
// 时间倒退护栏（fail-loud）：
//   - clk.Advance 返回 error（VirtualClock 被推进到一个更早的时刻）
//     时**立即返回 error，不再喂后续任何一根 bar**。半推进比不推进更
//     难查：若把倒退那根照喂，OnBar 会看到一根「比上一根旧」的 bar，
//     任何有状态算子都被污染，且污染现场离根因很远。
//   - 同理 OnBar 自身返回 error 时立即停（不吞掉策略的失败）。
func (r *StreamRunner) Run(ctx context.Context, h BarHandler, bars []domain.OHLCV, clk clock.Clock) error {
	if h == nil {
		return errors.New("strategy: StreamRunner.Run 收到 nil BarHandler")
	}
	if clk == nil {
		return errors.New("strategy: StreamRunner.Run 收到 nil Clock")
	}
	for i := range bars {
		bar := bars[i]
		if err := clk.Advance(bar.Date); err != nil {
			return fmt.Errorf(
				"strategy: StreamRunner 在第 %d 根 bar (symbol=%s date=%s) 推进时钟失败，已停止喂后续 bar: %w",
				i, bar.Symbol, bar.Date.UTC().Format(time.RFC3339Nano), err)
		}
		if err := h.OnBar(ctx, bar); err != nil {
			return fmt.Errorf(
				"strategy: StreamRunner 第 %d 根 bar (symbol=%s date=%s) OnBar 失败: %w",
				i, bar.Symbol, bar.Date.UTC().Format(time.RFC3339Nano), err)
		}
	}
	return nil
}

// ─── BatchAdapter：批式 → 流式 的同构桥 ──────────────────────────────

// batchAdapterStateVersion 是 SaveState 载荷的版本号。反序列化时版本
// 不匹配即 fail-loud（见 LoadState）——状态格式将来若改（例如从 JSON
// 换成二进制），旧载荷必须被明确拒绝而不是被误读成新格式。
const batchAdapterStateVersion = 1

// BatchAdapter 把批式 SignalGenerator 包装成 BarHandler。
//
// 语义（本切片裁决）：
//
//  1. 窗口语义 —— 每个 symbol 各持一个「最近 window 根 bar」的滚动窗口。
//     窗口满 **且 配置的每个 symbol 都满** 才算「就绪」；就绪后每逢 bar
//     到达调用一次 GenerateSignals。多 symbol 就绪门要求「全 symbol 都满」
//     而不是「当前 bar 的 symbol 满」——批式策略是**横截面**的，它看到
//     的 bars 快照必须是同一时刻所有 symbol 对齐后的窗口；只喂当前 symbol
//     的窗口会让别的 symbol 拿到残缺历史，产出假信号。
//
//  2. Warmup 前不出信号 —— 就绪门未满足时**绝不调用 GenerateSignals**。
//     这正是 BarHandler.Warmup() 的语义：引擎据此预取历史，窗口不满时
//     策略无从判断，宁可不出信号也不出假信号。
//
//  3. 信号怎么处理（裁决：本切片不消费）—— GenerateSignals 的返回被存进
//     lastSignals，调用方经 LastSignals() 读取。**本适配器不把信号路由到
//     Broker / 引擎**：那是切片 2（接 backtest engine）与切片 5（实盘）
//     的事。把消费逻辑提前塞进来会让适配器同时承担「窗口维护」与
//     「下单路由」两件事，而后者依赖尚未定型的执行上下文——所以这里
//     只产出、不消费，边界清晰。
//
//  4. portfolio 注入（裁决：SetPortfolio 运行时注入）—— 批式
//     GenerateSignals 需要 *domain.Portfolio，但组合在回测/实盘里是
//     **逐 bar 演进** 的，构造时固定注入会拿到一个过期的快照。故用
//     SetPortfolio 让引擎在每根 bar 前刷新。未设置时传 nil——策略若
//     读组合须自行容忍 nil，或由引擎保证注入。
//
//  5. 配置 universe（裁决）—— symbols 枚举横截面。为空的适配器**恒不就绪**
//     （inert）：没有配置 universe 就没有横截面快照，静默按空 map 出信号
//     是危险的。快照中只含配置内的 symbol，其它 symbol 的 bar 被忽略。
//
// 不可序列化之物（SignalGenerator 对象本身、portfolio 指针）不进状态：
// 它们是**注入的接线**，不是**状态**。SaveState/LoadState 只搬运窗口
// 与信号，sg 由构造注入，portfolio 由 SetPortfolio 注入。
type BatchAdapter struct {
	sg          SignalGenerator
	symbols     []string
	symbolSet   map[string]bool
	window      int
	windows     map[string][]domain.OHLCV
	portfolio   *domain.Portfolio
	lastSignals []Signal
}

// NewBatchAdapter 建一个同构桥。
//
// sg 为被包装的批式策略；symbols 为横截面 universe；window 为滚动窗口
// 根数（= Warmup() 的返回值）。window < 1 时归一到 1：批式策略至少要
// 看到「当前这根」才有意义，0/负窗口属装配错误，归一为最保守的 1 而
// 不是静默变成「无窗口恒就绪」。
func NewBatchAdapter(sg SignalGenerator, symbols []string, window int) *BatchAdapter {
	if window < 1 {
		window = 1
	}
	set := make(map[string]bool, len(symbols))
	for _, s := range symbols {
		set[s] = true
	}
	return &BatchAdapter{
		sg:        sg,
		symbols:   append([]string(nil), symbols...),
		symbolSet: set,
		window:    window,
		windows:   make(map[string][]domain.OHLCV),
	}
}

// Warmup 声明攒够首个有效信号所需的历史 bar 数 = 窗口大小。
func (a *BatchAdapter) Warmup() int { return a.window }

// SetPortfolio 注入/刷新当前组合。引擎应在每根 bar 前调用，让
// GenerateSignals 看到的是「此刻」的组合而不是构造时的陈旧快照。
func (a *BatchAdapter) SetPortfolio(p *domain.Portfolio) { a.portfolio = p }

// LastSignals 返回最近一次 GenerateSignals 的产出（可能为 nil = 尚未
// 产生首个信号）。返回**副本**，调用方改它不会污染适配器内部状态。
func (a *BatchAdapter) LastSignals() []Signal {
	if a.lastSignals == nil {
		return nil
	}
	cp := make([]Signal, len(a.lastSignals))
	copy(cp, a.lastSignals)
	return cp
}

// OnBar 收一根 bar：并入对应 symbol 的滚动窗口，窗口就绪则调一次
// GenerateSignals。
//
// 不在 universe 内的 symbol 被忽略（不入窗、不入快照）；universe 为空
// 时恒不就绪（inert）。就绪门未满足时**不调用** GenerateSignals。
func (a *BatchAdapter) OnBar(ctx context.Context, bar domain.OHLCV) error {
	if a.sg == nil {
		return errors.New("strategy: BatchAdapter.OnBar 的 SignalGenerator 为 nil")
	}
	if len(a.symbols) == 0 {
		// 无 universe → inert。没有横截面就没有快照，宁可不产出。
		return nil
	}
	if !a.symbolSet[bar.Symbol] {
		// universe 外的 symbol：不进快照（否则会污染横截面对齐）。
		return nil
	}

	w := append(a.windows[bar.Symbol], bar)
	if len(w) > a.window {
		trimmed := make([]domain.OHLCV, a.window)
		copy(trimmed, w[len(w)-a.window:])
		w = trimmed
	}
	a.windows[bar.Symbol] = w

	if !a.ready() {
		return nil
	}

	sigs, err := a.sg.GenerateSignals(ctx, a.snapshot(), a.portfolio)
	if err != nil {
		return fmt.Errorf(
			"strategy: BatchAdapter.GenerateSignals 失败 (symbol=%s date=%s): %w",
			bar.Symbol, bar.Date.UTC().Format(time.RFC3339Nano), err)
	}
	a.lastSignals = sigs
	return nil
}

// ready 判定就绪门：配置的每个 symbol 都已攒满 window 根。
func (a *BatchAdapter) ready() bool {
	if len(a.symbols) == 0 {
		return false
	}
	for _, s := range a.symbols {
		if len(a.windows[s]) < a.window {
			return false
		}
	}
	return true
}

// snapshot 造一份窗口深拷贝，交 GenerateSignals。深拷贝是必需的：
// 策略拿到的是我们的内部切片，若它原地改（例如补 0 填充），会污染
// 下一根的窗口——同构桥就成了数据泄露桥。
func (a *BatchAdapter) snapshot() map[string][]domain.OHLCV {
	m := make(map[string][]domain.OHLCV, len(a.windows))
	for s, w := range a.windows {
		cp := make([]domain.OHLCV, len(w))
		copy(cp, w)
		m[s] = cp
	}
	return m
}

// batchAdapterState 是 SaveState 的 JSON 载荷（格式裁决：JSON）。
//
// 选 JSON 的理由：状态里是 OHLCV/float/string 等纯值，无指针无连接；
// JSON 可读、可 diff、跨语言（后续落 quant.strategy_state 的 JSONB 列
// 直接可用），且 encoding/json 对 map 键排序 → 同一状态的字节稳定，
// 这让「两条路径的终态字节相等」成为可断言的属性（三路一致性测试用）。
type batchAdapterState struct {
	Version     int                       `json:"version"`
	Symbols     []string                  `json:"symbols"`
	Window      int                       `json:"window"`
	Windows     map[string][]domain.OHLCV `json:"windows"`
	LastSignals []Signal                  `json:"last_signals"`
}

// SaveState 序列化适配器状态（窗口 + 最近信号）。
//
// 不含 sg（接口对象，不可序列化）与 portfolio（运行时接线，不是状态）。
func (a *BatchAdapter) SaveState() ([]byte, error) {
	st := batchAdapterState{
		Version:     batchAdapterStateVersion,
		Symbols:     append([]string(nil), a.symbols...),
		Window:      a.window,
		Windows:     a.windows,
		LastSignals: a.lastSignals,
	}
	b, err := json.Marshal(st)
	if err != nil {
		return nil, fmt.Errorf("strategy: BatchAdapter.SaveState 序列化失败: %w", err)
	}
	return b, nil
}

// LoadState 反序列化状态。
//
// Fail-loud（不静默重置为初始态）——契约 BarHandler.LoadState 明确要求：
// 不合法的输入返回 error。静默重置会让「断点续跑」变成「悄悄从头跑」，
// 结果错了还查不出来。故：
//
//   - 先解到临时指针 st，**全部校验通过后才提交到接收者**。任何一步失败
//     都返回 error 且**接收者状态一字不动**（原子提交，无半加载）。
//   - 空字节 / 非法 JSON → json 报错。
//   - JSON 字面量 null → 解出 st==nil，显式报错（否则会被当成"合法空状态"
//     悄悄重置，这是最隐蔽的一种静默重置）。
//   - version 不匹配 / window 非法 → 显式报错。
//
// 恢复的是状态（symbols/window/windows/lastSignals）；sg 由构造注入，
// portfolio 由 SetPortfolio 注入——这两者不是状态。
func (a *BatchAdapter) LoadState(b []byte) error {
	var st *batchAdapterState
	if err := json.Unmarshal(b, &st); err != nil {
		return fmt.Errorf("strategy: BatchAdapter.LoadState 反序列化失败（状态未改动）: %w", err)
	}
	if st == nil {
		return errors.New("strategy: BatchAdapter.LoadState 状态为空 (null)（状态未改动）")
	}
	if st.Version != batchAdapterStateVersion {
		return fmt.Errorf(
			"strategy: BatchAdapter.LoadState 状态版本不匹配 got=%d want=%d（状态未改动）",
			st.Version, batchAdapterStateVersion)
	}
	if st.Window < 1 {
		return fmt.Errorf("strategy: BatchAdapter.LoadState 非法 window=%d（状态未改动）", st.Window)
	}

	// ── 校验全部通过，原子提交 ──
	symbols := append([]string(nil), st.Symbols...)
	set := make(map[string]bool, len(symbols))
	for _, s := range symbols {
		set[s] = true
	}
	windows := st.Windows
	if windows == nil {
		windows = make(map[string][]domain.OHLCV)
	}
	a.symbols = symbols
	a.symbolSet = set
	a.window = st.Window
	a.windows = windows
	a.lastSignals = st.LastSignals
	return nil
}

// ─── 编译期合规检查 ────────────────────────────────────────────────
//
// BatchAdapter 必须满足 BarHandler，否则它就不是合法的同构桥。
// 漂移（改签名 / 少方法）时 go build 失败。
var _ BarHandler = (*BatchAdapter)(nil)
