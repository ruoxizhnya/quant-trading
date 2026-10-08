package backtest

// K2 切片 2：引擎双模式接入的正证据与确定性护栏。
//
// 本文件服务两件事：
//
//  1. **双模式等价性（核心正证据）**：同一个交易日收益阈值的策略逻辑，
//     分别写成 (a) 原生批式实现（SignalGenerator）与 (b) 流式实现
//     （BarHandler），对**同一个回测请求**跑两遍，断言两条路径产出的信号
//     序列（日期 / symbol / 方向）**逐条一致**。这证明「引擎自动选执行
//     路径」不会改变策略语义——批式与流式是同一份逻辑的两种驱动方式
//     （回到 ADR-028 §7 的同构属性，但这次在**引擎整体**这一层验证）。
//
//  2. **喂序确定性**：同一份输入两次跑，喂给 OnBar 的 bar 序列必须逐条
//     一致。Go 的 map 遍历顺序随机，若引擎按 map 顺序喂 bar，任何
//     per-symbol 递推状态都会随遍历顺序漂移；本测试用记录器策略钉死
//     「按 symbol 字典序喂」这条不变量（破坏验证腿 a 用它变红）。
//
// 落位裁决：本包既有 engine_reproducibility_test.go 关注**同模式**两次跑
// 的逐字节可复现；本文件关注**跨模式**等价 + 流式喂序，新增文件避免
// 改动既有测试逻辑（与 streaming_compliance_test.go 的「只追加」同理）。

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// eqThreshold 是测试策略的收益率阈值：|日收益| 超过它才产生信号。
const eqThreshold = 0.005

// eqAction 是两条路径**共用**的决策函数（同一份逻辑，两种驱动）：
// 日收益 > +阈值 → buy；< -阈值 → sell；否则无信号（返回 ""）。
// 共用是等价性的前提——若两条路径各写一份判断，测的就只是两份实现巧合。
func eqAction(prevClose, close float64) string {
	if prevClose <= 0 {
		return ""
	}
	r := close/prevClose - 1
	switch {
	case r > eqThreshold:
		return "buy"
	case r < -eqThreshold:
		return "sell"
	default:
		return ""
	}
}

// eqSignalKey 把信号压成「日期|symbol|动作」——正是验收要求比较的三元组。
func eqSignalKey(s strategy.Signal) string {
	d := ""
	if t, ok := s.Date.(time.Time); ok {
		d = t.UTC().Format("2006-01-02")
	}
	return d + "|" + s.Symbol + "|" + s.Action
}

// ─── (a) 原生批式实现（只实现 Strategy，不含任何 BarHandler 方法）─────────

type eqBatchStrategy struct {
	name     string
	recorded []strategy.Signal
}

func (s *eqBatchStrategy) Name() string                            { return s.name }
func (s *eqBatchStrategy) Description() string                     { return "batch dual-mode probe" }
func (s *eqBatchStrategy) Parameters() []strategy.Parameter        { return nil }
func (s *eqBatchStrategy) Configure(map[string]interface{}) error  { return nil }
func (s *eqBatchStrategy) Cleanup()                                {}
func (s *eqBatchStrategy) Weight(strategy.Signal, float64) float64 { return 1 }

func (s *eqBatchStrategy) GenerateSignals(_ context.Context, bars map[string][]domain.OHLCV, _ *domain.Portfolio) ([]strategy.Signal, error) {
	syms := make([]string, 0, len(bars))
	for sym := range bars {
		syms = append(syms, sym)
	}
	sort.Strings(syms)

	var out []strategy.Signal
	for _, sym := range syms {
		w := bars[sym]
		if len(w) < 2 {
			continue
		}
		last := w[len(w)-1]
		act := eqAction(w[len(w)-2].Close, last.Close)
		if act == "" {
			continue
		}
		out = append(out, strategy.Signal{
			Symbol: sym, Action: act, Date: last.Date, Price: last.Close, Strength: last.Close,
		})
	}
	s.recorded = append(s.recorded, out...)
	return out, nil
}

// ─── (b) 流式实现（同时实现 Strategy 与 BarHandler）────────────────────

type eqStreamStrategy struct {
	name     string
	prev     map[string]float64
	pending  []strategy.Signal
	recorded []strategy.Signal
	feed     []string // 记录引擎喂入顺序（确定性护栏）
}

func (s *eqStreamStrategy) Name() string                            { return s.name }
func (s *eqStreamStrategy) Description() string                     { return "streaming dual-mode probe" }
func (s *eqStreamStrategy) Parameters() []strategy.Parameter        { return nil }
func (s *eqStreamStrategy) Configure(map[string]interface{}) error  { return nil }
func (s *eqStreamStrategy) Cleanup()                                {}
func (s *eqStreamStrategy) Weight(strategy.Signal, float64) float64 { return 1 }

// GenerateSignals 属 Strategy 复合接口的批式半边；流式实现下引擎**不会**
// 调用它（引擎先做 BarHandler 类型断言），返回 nil 以示「此处不产出」。
func (s *eqStreamStrategy) GenerateSignals(context.Context, map[string][]domain.OHLCV, *domain.Portfolio) ([]strategy.Signal, error) {
	return nil, nil
}

// ── BarHandler ──

// Warmup 声明需要 1 根前序 bar（算收益要「上一根」）。
func (s *eqStreamStrategy) Warmup() int { return 1 }

func (s *eqStreamStrategy) OnBar(_ context.Context, bar domain.OHLCV) error {
	if s.prev == nil {
		s.prev = make(map[string]float64)
	}
	s.feed = append(s.feed, bar.Date.UTC().Format("2006-01-02")+"|"+bar.Symbol)
	if p, ok := s.prev[bar.Symbol]; ok {
		if act := eqAction(p, bar.Close); act != "" {
			sig := strategy.Signal{
				Symbol: bar.Symbol, Action: act, Date: bar.Date, Price: bar.Close, Strength: bar.Close,
			}
			s.pending = append(s.pending, sig)
			s.recorded = append(s.recorded, sig)
		}
	}
	s.prev[bar.Symbol] = bar.Close
	return nil
}

// Signals 取走自上次调用以来的信号（取走即清空）。
func (s *eqStreamStrategy) Signals() []strategy.Signal {
	out := s.pending
	s.pending = nil
	return out
}

func (s *eqStreamStrategy) SaveState() ([]byte, error) { return nil, nil }
func (s *eqStreamStrategy) LoadState([]byte) error     { return nil }

// 编译期确认两条路径满足各自契约（及双模式策略同时满足两者）。
var (
	_ strategy.Strategy   = (*eqBatchStrategy)(nil)
	_ strategy.Strategy   = (*eqStreamStrategy)(nil)
	_ strategy.BarHandler = (*eqStreamStrategy)(nil)
)

// ─── 运行辅助 ──────────────────────────────────────────────────────

const (
	eqSymbols   = 4
	eqDays      = 40
	eqSeedStart = "2024-01-02"
)

// runDualModeBacktest 用一份全新的合成引擎跑一遍回测，绑定给定策略。
func runDualModeBacktest(t *testing.T, strat strategy.Strategy) *BacktestResponse {
	t.Helper()
	if err := strategy.GlobalRegister(strat); err != nil {
		t.Fatalf("注册策略 %s 失败：%v", strat.Name(), err)
	}
	eng, _, symbols := buildSyntheticEngine(t, eqSymbols, eqDays, 1)

	resp, err := eng.RunBacktest(context.Background(), BacktestRequest{
		Strategy:       strat.Name(),
		StockPool:      symbols,
		StartDate:      eqSeedStart,
		EndDate:        "2024-02-10", // 40 天窗口
		InitialCapital: 1_000_000,
		RiskFreeRate:   0.03,
	})
	if err != nil {
		t.Fatalf("回测 %s 失败：%v", strat.Name(), err)
	}
	if resp.Status != "completed" {
		t.Fatalf("回测 %s 未完成：status=%s err=%s", strat.Name(), resp.Status, resp.Error)
	}
	return resp
}

func eqSeqOf(sigs []strategy.Signal) []string {
	out := make([]string, len(sigs))
	for i, s := range sigs {
		out[i] = eqSignalKey(s)
	}
	return out
}

// ─── 测试 1：双模式等价性（核心正证据）────────────────────────────────

func TestEngineDualModeSignalEquivalence(t *testing.T) {
	batch := &eqBatchStrategy{name: "k2_dual_batch_probe"}
	stream := &eqStreamStrategy{name: "k2_dual_stream_probe"}

	runDualModeBacktest(t, batch)
	runDualModeBacktest(t, stream)

	seqBatch := eqSeqOf(batch.recorded)
	seqStream := eqSeqOf(stream.recorded)

	// 正证据必须非空——否则「一致」是空洞的（两边都空也叫一致）。
	if len(seqBatch) == 0 {
		t.Fatal("批式路径未产出任何信号，等价性断言无意义")
	}
	if len(seqBatch) != len(seqStream) {
		t.Fatalf("两模式信号条数不同：批式 %d 条，流式 %d 条", len(seqBatch), len(seqStream))
	}
	// 逐条比对（日期 / symbol / 方向）。
	for i := range seqBatch {
		if seqBatch[i] != seqStream[i] {
			t.Fatalf("第 %d 条信号不一致：\n  批式 %s\n  流式 %s", i, seqBatch[i], seqStream[i])
		}
	}
	t.Logf("双模式等价：两条路径各产出 %d 条信号，逐条一致（日期/symbol/方向）", len(seqBatch))
	t.Logf("批式前 5 条：%v", head(seqBatch, 5))
	t.Logf("流式前 5 条：%v", head(seqStream, 5))
}

// ─── 测试 2：喂序确定性（同一输入两次跑，OnBar 喂入序列一致）───────────

func TestEngineStreamingFeedOrderIsDeterministic(t *testing.T) {
	a := &eqStreamStrategy{name: "k2_feed_det_a"}
	b := &eqStreamStrategy{name: "k2_feed_det_b"}

	runDualModeBacktest(t, a)
	runDualModeBacktest(t, b)

	if len(a.feed) == 0 {
		t.Fatal("未记录到任何喂入 bar")
	}
	if len(a.feed) != len(b.feed) {
		t.Fatalf("两次跑的喂入 bar 数不同：%d vs %d", len(a.feed), len(b.feed))
	}
	for i := range a.feed {
		if a.feed[i] != b.feed[i] {
			t.Fatalf("第 %d 根喂入不一致（喂序不确定）：%s vs %s", i, a.feed[i], b.feed[i])
		}
	}
	// 钉住「按 symbol 字典序」这条不变量：同一天的相邻 symbol 必须升序。
	// （仅断言单日内部单调——跨日的边界不断言，避免耦合日期切分细节。）
	for i := 1; i < len(a.feed); i++ {
		pi := strings.Split(a.feed[i-1], "|")
		ci := strings.Split(a.feed[i], "|")
		if pi[0] == ci[0] && pi[1] > ci[1] {
			t.Fatalf("同一日 %s 内的喂序非字典序：%s 出现在 %s 之后", ci[0], ci[1], pi[1])
		}
	}
	t.Logf("喂序确定性：两次跑 %d 根 bar 逐条一致；同日喂序按 symbol 字典序", len(a.feed))
	t.Logf("前 %d 根：%v", len(head(a.feed, 8)), head(a.feed, 8))
}

func head(xs []string, n int) []string {
	if len(xs) < n {
		n = len(xs)
	}
	return xs[:n]
}
