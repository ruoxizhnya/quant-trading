// 外部模型信号消费策略（K7 切片 2）—— L3b 信号注入的「Actor 侧」。
//
// SignalStrategy 把「外部模型已落库的信号」变成内核可下单的 strategy.Signal，
// 实现蓝图 §6.4 的「外部模型不直接下单，产信号注入内核、由策略消费」。
// 它对应 nautilus 的 SignalStrategy / Actor.on_signal：模型在进程外只管写
// quant.external_signals（content_hash 幂等坐标），本策略在进程内按 as_of
// 拉取消费——自由度挡在内核外，内核内仍确定、可审阅。
//
// ─── 消费形态（若曦 2026-10-09 拍板：信号表 + 按 as_of 拉取）─────────
// 不是运行时回调推送，而是「拉」：引擎逐 bar 驱动本策略，本策略在 OnBar 里
// 读 store.ListFor(model, symbol, bar.Date)——回测时信号表是已落库的历史，
// 回放时按 as_of 过滤，未来信号物理不可见；实盘时读到「到今天为止」的信号。
// 同一段消费代码，回测与实盘同构。
//
// ─── 信号对齐语义（裁决）────────────────────────────────────────────
// 每条外部信号在「首个 bar 日期 >= 信号 as_of」的那根 bar 上被消费一次：
//   - as_of 命中交易日 → 当天消费；
//   - as_of 落在非交易日（周末/停牌）→ 下一交易日消费（信号在下一个可交易
//     时点生效）；
//   - as_of 早于回测窗口首日（窗口前的历史信号）→ **跳过**（陈旧外推信号
//     不该在窗口首日被当作「今天的新信号」重放，否则会把整个历史期一次性
//     倾倒到首日）；
//   - as_of 晚于当前 bar → **拒绝**（防前视，见 rejectFutureSignals）。
//
// 去重靠 per-symbol 游标（cursor）：每根 bar 只消费 as_of > 游标的信号，游标
// 单调推进到当前 bar 日期。游标是断点续跑的状态（SaveState/LoadState）。
package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// SignalStrategy 消费外部模型信号（Actor 侧）。
//
// 它**同时**实现 Strategy（注册进 DefaultRegistry 用）与 BarHandler（引擎双
// 模式接入点据此走流式路径）——与 K2 切片 2 的双模式探测策略同构（见
// pkg/backtest/engine_streaming_equivalence_test.go 的 eqStreamStrategy）。
type SignalStrategy struct {
	name    string
	modelID string
	store   SignalStore

	// cursor 记录每个 symbol 最近已消费到的信号日期（00:00 UTC 日边界）。
	// 值存在即表示「该 symbol 已见过首根 bar」（seen 哨兵合一），首根 bar
	// 用「as_of == 当日」而非「as_of > 零值」判定，从而跳过窗口前的历史信号。
	cursor map[string]time.Time

	// pending 累积当前 bar 产出的信号，Signals() 取走即清空。
	pending []Signal
}

// NewSignalStrategy 构造一个外部信号消费策略。
//
// name 是注册名（DefaultRegistry 的键）；modelID 是本策略消费哪个外部模型的
// 信号（ListFor 的 model 过滤）；store 是信号后端。modelID 空 / store 为 nil
// 会在 OnBar 时 fail-loud（见 OnBar）——构造期不做 I/O，也不该在构造期隐式
// 校验（与 PGStateStore「构造函数不隐式建表」同源）。
func NewSignalStrategy(name, modelID string, store SignalStore) *SignalStrategy {
	return &SignalStrategy{
		name:    name,
		modelID: modelID,
		store:   store,
		cursor:  make(map[string]time.Time),
	}
}

// ─── Strategy（复合接口的批式半边）──────────────────────────────────

func (s *SignalStrategy) Name() string { return s.name }
func (s *SignalStrategy) Description() string {
	return "consumes external model signals (model=" + s.modelID + ")"
}
func (s *SignalStrategy) Parameters() []Parameter {
	return nil
}
func (s *SignalStrategy) Configure(map[string]interface{}) error { return nil }
func (s *SignalStrategy) Cleanup()                               {}

// Weight 属批式 SignalGenerator 半边，流式路径下引擎不调用（引擎先做
// BarHandler 类型断言）。返回 1 以示「由信号强度直接驱动、不额外加权」。
func (s *SignalStrategy) Weight(Signal, float64) float64 { return 1 }

// GenerateSignals 属批式半边；流式实现下引擎不会调用它（见 K2 双模式裁决）。
func (s *SignalStrategy) GenerateSignals(context.Context, map[string][]domain.OHLCV, *domain.Portfolio) ([]Signal, error) {
	return nil, nil
}

// ─── BarHandler ────────────────────────────────────────────────────

// Warmup 声明 0：外部信号是「注入」的、不是「从 bar 递推」的，信号一存在即
// 可消费，无需历史 bar 预热。
func (s *SignalStrategy) Warmup() int { return 0 }

// OnBar 消费一根 bar 当日（及之前未消费）的外部信号，产出 strategy.Signal。
//
// 流程：拉取 as_of <= 当日 → 防前视拒绝（纵深防御）→ 按游标去重消费 →
// 游标推进到当日。任一步失败立即返回 error（fail-loud，不吞策略失败）。
func (s *SignalStrategy) OnBar(ctx context.Context, bar domain.OHLCV) error {
	if s.store == nil {
		return errors.New("strategy: SignalStrategy 的 SignalStore 为 nil——未接线，无法拉取信号")
	}
	if s.modelID == "" {
		return errors.New("strategy: SignalStrategy 的 modelID 为空——不知道消费哪个模型，静默产零信号比报错更危险")
	}

	today := dayOf(bar.Date)
	sigs, err := s.store.ListFor(ctx, s.modelID, bar.Symbol, today)
	if err != nil {
		return fmt.Errorf("strategy: SignalStrategy 拉取信号失败 (symbol=%s date=%s): %w",
			bar.Symbol, today.Format(time.RFC3339), err)
	}

	// 防前视（纵深防御）：store 契约已保证 as_of <= upTo（切片 1 主边界），
	// 这里再拒一道——任何返回未来信号的 store 实现 / 批量读路径都必须 fail-loud，
	// 而不是把未来信号转成今日下单。
	if err := rejectFutureSignals(sigs, today); err != nil {
		return err
	}

	last, seen := s.cursor[bar.Symbol]
	for _, sig := range sigs {
		if !seen {
			// 首根 bar：窗口从 today 开始，只消费 as_of == today 的信号，
			// 窗口前的历史信号（as_of < today）跳过。
			if !sig.AsOf.Equal(today) {
				continue
			}
		} else if !sig.AsOf.After(last) {
			// 已消费过（as_of <= 游标）。
			continue
		}
		s.pending = append(s.pending, s.toSignal(sig, today))
	}
	s.cursor[bar.Symbol] = today
	return nil
}

// Signals 取走自上次调用以来 OnBar 产出的信号（取走即清空，见 BarHandler 契约）。
func (s *SignalStrategy) Signals() []Signal {
	out := s.pending
	s.pending = nil
	return out
}

// ─── 状态序列化（断点续跑）──────────────────────────────────────────

// signalStrategyStateVersion 是 SignalStrategy.SaveState 载荷的版本号。
const signalStrategyStateVersion = 1

// signalStrategyState 是序列化状态：游标（per-symbol 已消费到的日期）+ 未取走
// 的 pending。modelID / store 是注入的接线、不是状态（与 BatchAdapter 的
// 「sg 由构造注入、portfolio 由 SetPortfolio 注入」同源裁决）。
type signalStrategyState struct {
	Version int                  `json:"version"`
	Cursor  map[string]time.Time `json:"cursor"`
	Pending []Signal             `json:"pending"`
}

// SaveState 序列化游标 + pending。游标值已归一 UTC 日边界，JSON 往返稳定。
func (s *SignalStrategy) SaveState() ([]byte, error) {
	st := signalStrategyState{
		Version: signalStrategyStateVersion,
		Cursor:  s.cursor,
		Pending: s.pending,
	}
	b, err := json.Marshal(st)
	if err != nil {
		return nil, fmt.Errorf("strategy: SignalStrategy.SaveState 序列化失败: %w", err)
	}
	return b, nil
}

// LoadState 反序列化状态。fail-loud（不静默重置）：先解到临时指针，全校验
// 通过后原子提交；version 不匹配 / null / 非法 JSON 均返回 error 且状态一字
// 不动。
func (s *SignalStrategy) LoadState(b []byte) error {
	var st *signalStrategyState
	if err := json.Unmarshal(b, &st); err != nil {
		return fmt.Errorf("strategy: SignalStrategy.LoadState 反序列化失败（状态未改动）: %w", err)
	}
	if st == nil {
		return errors.New("strategy: SignalStrategy.LoadState 状态为空 (null)（状态未改动）")
	}
	if st.Version != signalStrategyStateVersion {
		return fmt.Errorf("strategy: SignalStrategy.LoadState 版本不匹配 got=%d want=%d（状态未改动）",
			st.Version, signalStrategyStateVersion)
	}

	cursor := st.Cursor
	if cursor == nil {
		cursor = make(map[string]time.Time)
	}
	s.cursor = cursor
	s.pending = st.Pending
	return nil
}

// toSignal 把外部信号转成 strategy.Signal。Date 用「消费日」emitDay（对齐引擎
// 当前处理日），模型声明的 as_of 与 content_hash 坐标放进 Metadata 供审计追溯。
func (s *SignalStrategy) toSignal(sig ExternalSignal, emitDay time.Time) Signal {
	return Signal{
		Symbol:    sig.Symbol,
		Direction: sig.Direction,
		Strength:  sig.Strength,
		Date:      emitDay,
		OrderType: domain.OrderTypeMarket,
		Factors:   sig.Factors,
		Metadata: map[string]interface{}{
			"model_id":     sig.ModelID,
			"as_of":        sig.AsOf.UTC().Format(time.RFC3339),
			"content_hash": SignalContentHash(sig),
		},
	}
}

// rejectFutureSignals fail-loud 拒绝任何 as_of > upTo 的信号。
//
// 这是「注入未来日期信号 → 拒绝」的显式落点（切片 2 破坏验证腿 a 的对象）：
// 未来信号不是被静默跳过，而是返回 error 让策略当场失败——静默跳过会让
// 「偷看未来」变成「悄悄少下单」，结果错了还查不出来。
func rejectFutureSignals(sigs []ExternalSignal, upTo time.Time) error {
	for _, s := range sigs {
		if s.AsOf.After(upTo) {
			return fmt.Errorf(
				"strategy: 外部信号未来日期被拒 (as_of=%s > 当前回放 %s, model=%s symbol=%s)——防前视",
				s.AsOf.UTC().Format(time.RFC3339), upTo.UTC().Format(time.RFC3339), s.ModelID, s.Symbol)
		}
	}
	return nil
}

// ─── 编译期合规检查 ────────────────────────────────────────────────
//
// SignalStrategy 必须同时满足 Strategy 与 BarHandler（引擎双模式接入据此走
// 流式路径）。
var (
	_ Strategy   = (*SignalStrategy)(nil)
	_ BarHandler = (*SignalStrategy)(nil)
)
