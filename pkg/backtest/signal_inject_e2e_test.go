package backtest

// K7 切片 2：外部信号注入 → 策略按信号下单的端到端正证据。
//
// 全链路：外部模型把信号写入 SignalStore（内存 fake，落库语义已在
// pkg/strategy/signal_store_test.go 真库覆盖）→ SignalStrategy（Actor）按
// as_of 拉取转成 domain.Signal → 引擎 processSignalsAndExecuteTrades 下单。
//
// 正证据分两层：
//   1. 信号层（SetSignalObserver）：注入的信号被 SignalStrategy 精确转成
//      domain.Signal（symbol / direction / strength 一致）；
//   2. 成交层（resp.Trades）：信号真的驱动了订单成交，而非只产信号不下单。
//
// 隔离边界：信号 as_of 落在回测窗口内（2024-01-02 ~ 2024-01-31，合成引擎的
// 交易日是逐日历日），保证「注入即被消费」可验证。
import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// e2eSignalStore 是 SignalStore 的内存实现（正确兑现 as_of <= upTo 契约）。
// 真库 PGSignalStore 已在 pkg/strategy 覆盖，这里只为把信号注入这条链路
// 拆出来、让端到端不依赖 DB。
type e2eSignalStore struct {
	signals []strategy.ExternalSignal
}

func (f *e2eSignalStore) Save(_ context.Context, sig strategy.ExternalSignal) error {
	f.signals = append(f.signals, sig)
	return nil
}

func (f *e2eSignalStore) ListFor(_ context.Context, modelID, symbol string, upTo time.Time) ([]strategy.ExternalSignal, error) {
	var out []strategy.ExternalSignal
	for _, s := range f.signals {
		if s.ModelID != modelID || s.Symbol != symbol {
			continue
		}
		if s.AsOf.After(upTo) {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AsOf.Before(out[j].AsOf) })
	return out, nil
}

// TestSignalInject_SignalDrivesOrders 是 K7 验收「外部模型信号注入 → 策略按
// 信号下单」的核心正证据。
func TestSignalInject_SignalDrivesOrders(t *testing.T) {
	eng, _, symbols := buildSyntheticEngine(t, 3, 30, 1)

	// 注入两条信号：一条 buy（as_of 在窗口内）、一条 close（as_of 更晚）。
	injectDay := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	closeDay := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	store := &e2eSignalStore{signals: []strategy.ExternalSignal{
		{ModelID: "ml-model", Symbol: symbols[0], Direction: domain.DirectionLong, Strength: 0.8, AsOf: injectDay},
		{ModelID: "ml-model", Symbol: symbols[0], Direction: domain.DirectionClose, Strength: 0.5, AsOf: closeDay},
	}}

	s := strategy.NewSignalStrategy("k7_signal_inject_probe", "ml-model", store)
	if err := strategy.GlobalRegister(s); err != nil {
		t.Fatalf("注册 SignalStrategy 失败：%v", err)
	}

	var observed []domain.Signal
	eng.SetSignalObserver(func(_ time.Time, signals []domain.Signal) {
		observed = append(observed, signals...)
	})

	resp, err := eng.RunBacktest(context.Background(), BacktestRequest{
		Strategy:       "k7_signal_inject_probe",
		StockPool:      symbols,
		StartDate:      "2024-01-02",
		EndDate:        "2024-01-31",
		InitialCapital: 1_000_000,
		RiskFreeRate:   0.03,
	})
	if err != nil {
		t.Fatalf("回测失败：%v", err)
	}
	if resp.Status != "completed" {
		t.Fatalf("回测未完成：status=%s err=%s", resp.Status, resp.Error)
	}

	// ── 信号层正证据：注入的信号被精确转成 domain.Signal ──
	if len(observed) == 0 {
		t.Fatal("SignalStrategy 未产出任何信号 —— 注入链路断了")
	}
	var gotLong, gotClose bool
	for _, sig := range observed {
		if sig.Symbol != symbols[0] {
			t.Fatalf("信号 symbol 漂移：%s, want %s", sig.Symbol, symbols[0])
		}
		switch sig.Direction {
		case domain.DirectionLong:
			gotLong = true
			if gotLong && sig.Strength != 0.8 {
				t.Fatalf("buy 信号 strength 漂移：%v, want 0.8", sig.Strength)
			}
		case domain.DirectionClose:
			gotClose = true
		}
	}
	if !gotLong || !gotClose {
		t.Fatalf("注入的 buy/close 信号未被完整消费（buy=%v close=%v）", gotLong, gotClose)
	}

	// ── 成交层正证据：信号真的驱动了下单 ──
	if resp.TotalTrades == 0 {
		t.Fatal("信号已产出但未驱动任何成交 —— 「策略按信号下单」链路断了")
	}
	for _, tr := range resp.Trades {
		if tr.Symbol != symbols[0] {
			t.Fatalf("成交 symbol 漂移：%s, want %s", tr.Symbol, symbols[0])
		}
	}
	t.Logf("注入信号 → 下单闭环：%d 条 domain.Signal，%d 笔成交，全部落在 %s",
		len(observed), resp.TotalTrades, symbols[0])
}

// TestSignalInject_FutureSignalNotTraded 是防前视的端到端反证腿：注入未来日期
// （窗口外）的信号，引擎全程不得为它下单。与 pkg/strategy 的
// TestSignalStrategyRejectsFutureSignal（fail-loud）互补——这里验证的是「即使
// 走到引擎层，未来信号也绝不成交」。
func TestSignalInject_FutureSignalNotTraded(t *testing.T) {
	eng, _, symbols := buildSyntheticEngine(t, 3, 30, 1)

	// 未来信号：as_of 远在回测窗口之后。
	futureDay := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	store := &e2eSignalStore{signals: []strategy.ExternalSignal{
		{ModelID: "ml-model", Symbol: symbols[0], Direction: domain.DirectionLong, Strength: 0.8, AsOf: futureDay},
	}}

	s := strategy.NewSignalStrategy("k7_signal_future_probe", "ml-model", store)
	if err := strategy.GlobalRegister(s); err != nil {
		t.Fatalf("注册 SignalStrategy 失败：%v", err)
	}

	resp, err := eng.RunBacktest(context.Background(), BacktestRequest{
		Strategy:       "k7_signal_future_probe",
		StockPool:      symbols,
		StartDate:      "2024-01-02",
		EndDate:        "2024-01-31",
		InitialCapital: 1_000_000,
		RiskFreeRate:   0.03,
	})
	if err != nil {
		t.Fatalf("回测失败：%v", err)
	}
	// 注意：未来信号 → 0 成交，会触发 OBS-01 的 zero_trades 裁定 → status=invalid。
	// 这是**预期**结果（0 成交本就是无效运行），不是回测失败。这里只断言
	// 「未来信号绝不成交」，不断言 completed。
	if resp.TotalTrades != 0 {
		t.Fatalf("未来日期信号竟驱动了 %d 笔成交 —— 防前视失效", resp.TotalTrades)
	}
	if resp.Status != "invalid" {
		t.Fatalf("0 成交运行应被 OBS-01 裁为 invalid（zero_trades），got status=%s", resp.Status)
	}
	t.Logf("防前视反证通过：未来日期信号全程零成交，运行被裁为 invalid（zero_trades）")
}
