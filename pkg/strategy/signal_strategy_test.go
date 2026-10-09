package strategy

// K7 切片 2：SignalStrategy（Actor 消费契约）单测。
//
// 用内存 fake SignalStore（filterByAsOf 可关闭以模拟「忘了过滤」的 buggy
// store）覆盖：信号→strategy.Signal 转换、首 bar 跳过窗口前历史、非交易日
// 顺延、游标去重、未来信号拒绝（防前视）、SaveState/LoadState 断点续跑。
//
// 纯内存测试，无 DB 依赖。真库落库/拉取已在 signal_store_test.go 覆盖。
import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/require"
)

// fakeSignalStore 是 SignalStore 的内存实现（测试专用）。
//
// filterByAsOf 控制 ListFor 是否兑现「as_of <= upTo」契约：
//   - true（默认，正确 store）—— 未来信号不可见；
//   - false（buggy store）—— 无视 upTo，返回全部，用于验证 SignalStrategy
//     的纵深防御（rejectFutureSignals）能兜住这种 store。
type fakeSignalStore struct {
	filterByAsOf bool
	signals      []ExternalSignal
}

func (f *fakeSignalStore) Save(_ context.Context, sig ExternalSignal) error {
	f.signals = append(f.signals, sig)
	return nil
}

func (f *fakeSignalStore) ListFor(_ context.Context, modelID, symbol string, upTo time.Time) ([]ExternalSignal, error) {
	var out []ExternalSignal
	for _, s := range f.signals {
		if s.ModelID != modelID || s.Symbol != symbol {
			continue
		}
		if f.filterByAsOf && s.AsOf.After(upTo) {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AsOf.Before(out[j].AsOf) })
	return out, nil
}

// sigBar 造一根测试 bar（只填 SignalStrategy 关心的 Symbol/Date）。
func sigBar(symbol string, day time.Time) domain.OHLCV {
	return domain.OHLCV{Symbol: symbol, Date: day, Close: 100}
}

// drain 调用 Signals() 并断言取走即清空。
func drain(t *testing.T, s *SignalStrategy) []Signal {
	t.Helper()
	out := s.Signals()
	require.Empty(t, s.Signals(), "取走后再次 Signals() 应为空（取走即清空契约）")
	return out
}

// ─── 信号 → strategy.Signal 转换与对齐 ─────────────────────────────

func TestSignalStrategyEmitsSignalOnMatchingBar(t *testing.T) {
	store := &fakeSignalStore{filterByAsOf: true}
	store.signals = []ExternalSignal{
		{ModelID: "m", Symbol: "600000.SH", Direction: domain.DirectionLong, Strength: 0.8, AsOf: sigDay(3)},
	}
	s := NewSignalStrategy("sigtest", "m", store)
	ctx := context.Background()

	// day1 / day2：无匹配信号。
	for _, d := range []int{1, 2} {
		require.NoError(t, s.OnBar(ctx, sigBar("600000.SH", sigDay(d))))
		require.Empty(t, drain(t, s), "day%d 不该产出信号", d)
	}
	// day3：命中。
	require.NoError(t, s.OnBar(ctx, sigBar("600000.SH", sigDay(3))))
	got := drain(t, s)
	require.Len(t, got, 1)
	require.Equal(t, domain.DirectionLong, got[0].Direction)
	require.InDelta(t, 0.8, got[0].Strength, 1e-12)
	require.Equal(t, "600000.SH", got[0].Symbol)
}

func TestSignalStrategySkipsPreWindowSignal(t *testing.T) {
	store := &fakeSignalStore{filterByAsOf: true}
	store.signals = []ExternalSignal{
		{ModelID: "m", Symbol: "600000.SH", Direction: domain.DirectionLong, Strength: 0.8, AsOf: sigDay(1)},
	}
	s := NewSignalStrategy("sigtest", "m", store)
	ctx := context.Background()

	// 首根 bar 从 day3 开始：as_of=day1 的信号在窗口前，必须跳过（不倾倒历史）。
	require.NoError(t, s.OnBar(ctx, sigBar("600000.SH", sigDay(3))))
	require.Empty(t, drain(t, s), "窗口前的历史信号不该在首日被重放")
}

func TestSignalStrategyWeekendSignalEmittedNextTradingDay(t *testing.T) {
	store := &fakeSignalStore{filterByAsOf: true}
	// 2024-03-02 是周六（非交易日），信号应顺延到下一交易日周一（03-04）。
	sat := time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC)
	fri := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	mon := time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC)
	store.signals = []ExternalSignal{
		{ModelID: "m", Symbol: "600000.SH", Direction: domain.DirectionLong, Strength: 0.8, AsOf: sat},
	}
	s := NewSignalStrategy("sigtest", "m", store)
	ctx := context.Background()

	require.NoError(t, s.OnBar(ctx, sigBar("600000.SH", fri)))
	require.Empty(t, drain(t, s), "周五：周六信号还没到，不该产出")
	require.NoError(t, s.OnBar(ctx, sigBar("600000.SH", mon)))
	got := drain(t, s)
	require.Len(t, got, 1, "周一应顺延消费周六信号")
}

func TestSignalStrategyCursorDedupNoDoubleEmit(t *testing.T) {
	store := &fakeSignalStore{filterByAsOf: true}
	store.signals = []ExternalSignal{
		{ModelID: "m", Symbol: "600000.SH", Direction: domain.DirectionLong, Strength: 0.8, AsOf: sigDay(3)},
	}
	s := NewSignalStrategy("sigtest", "m", store)
	ctx := context.Background()

	require.NoError(t, s.OnBar(ctx, sigBar("600000.SH", sigDay(3))))
	require.Len(t, drain(t, s), 1, "day3 应产出一次")
	// day4 再喂：游标已推进到 day3，as_of=day3 的信号不该被再次消费。
	require.NoError(t, s.OnBar(ctx, sigBar("600000.SH", sigDay(4))))
	require.Empty(t, drain(t, s), "游标去重失败：day3 信号被重复消费")
}

// ─── 防前视：未来信号拒绝（破坏验证腿 a 的对象）────────────────────

func TestSignalStrategyRejectsFutureSignal(t *testing.T) {
	// buggy store：ListFor 无视 upTo，把未来信号也吐出来。
	store := &fakeSignalStore{filterByAsOf: false}
	store.signals = []ExternalSignal{
		{ModelID: "m", Symbol: "600000.SH", Direction: domain.DirectionLong, Strength: 0.8, AsOf: sigDay(5)},
	}
	s := NewSignalStrategy("sigtest", "m", store)
	ctx := context.Background()

	// 回放到 day3，store 却返回 as_of=day5 的未来信号 → 必须 fail-loud 拒绝。
	err := s.OnBar(ctx, sigBar("600000.SH", sigDay(3)))
	require.Error(t, err, "未来日期信号必须被拒（fail-loud），而非静默跳过或下单")
	require.Contains(t, err.Error(), "未来日期", "错误应点明是防前视拒绝")
}

func TestRejectFutureSignalsPureFunc(t *testing.T) {
	today := sigDay(3)
	require.NoError(t, rejectFutureSignals([]ExternalSignal{
		{ModelID: "m", Symbol: "s", Direction: domain.DirectionLong, Strength: 0.1, AsOf: sigDay(3)},
	}, today), "as_of == upTo 是合法（当日信号）")
	require.Error(t, rejectFutureSignals([]ExternalSignal{
		{ModelID: "m", Symbol: "s", Direction: domain.DirectionLong, Strength: 0.1, AsOf: sigDay(4)},
	}, today), "as_of > upTo 必须被拒")
}

// ─── 断点续跑（SaveState / LoadState）──────────────────────────────

func TestSignalStrategySaveLoadStateResume(t *testing.T) {
	store := &fakeSignalStore{filterByAsOf: true}
	store.signals = []ExternalSignal{
		{ModelID: "m", Symbol: "600000.SH", Direction: domain.DirectionLong, Strength: 0.8, AsOf: sigDay(3)},
		{ModelID: "m", Symbol: "600000.SH", Direction: domain.DirectionClose, Strength: 0.5, AsOf: sigDay(5)},
	}
	ctx := context.Background()

	// 第一段：跑到 day3，消费了 day3 信号。
	a := NewSignalStrategy("sigtest", "m", store)
	require.NoError(t, a.OnBar(ctx, sigBar("600000.SH", sigDay(1))))
	require.NoError(t, a.OnBar(ctx, sigBar("600000.SH", sigDay(2))))
	require.NoError(t, a.OnBar(ctx, sigBar("600000.SH", sigDay(3))))
	require.Len(t, drain(t, a), 1, "day3 应产出 day3 信号")

	state, err := a.SaveState()
	require.NoError(t, err)

	// 第二段：新实例从断点续跑（游标已含 day3）。
	b := NewSignalStrategy("sigtest", "m", store)
	require.NoError(t, b.LoadState(state))
	require.NoError(t, b.OnBar(ctx, sigBar("600000.SH", sigDay(4))))
	require.Empty(t, drain(t, b), "day4 无新信号")
	require.NoError(t, b.OnBar(ctx, sigBar("600000.SH", sigDay(5))))
	got := drain(t, b)
	require.Len(t, got, 1, "day5 应产出 day5 信号（续跑不重放 day3）")
	require.Equal(t, domain.DirectionClose, got[0].Direction)
}

func TestSignalStrategyLoadStateRejectsBadInput(t *testing.T) {
	s := NewSignalStrategy("sigtest", "m", &fakeSignalStore{filterByAsOf: true})

	require.Error(t, s.LoadState(nil), "空状态必须被拒")
	require.Error(t, s.LoadState([]byte("null")), "null 必须被拒")
	require.Error(t, s.LoadState([]byte(`{"version":999}`)), "版本不匹配必须被拒")
	require.Error(t, s.LoadState([]byte(`{not-json`)), "非法 JSON 必须被拒")
}

// ─── 接线 fail-loud ────────────────────────────────────────────────

func TestSignalStrategyFailLoudOnMisconfig(t *testing.T) {
	ctx := context.Background()
	bar := sigBar("600000.SH", sigDay(1))

	// nil store：拉不到信号，必须报错而非静默产零信号。
	nilStore := NewSignalStrategy("sigtest", "m", nil)
	require.Error(t, nilStore.OnBar(ctx, bar), "nil store 必须 fail-loud")

	// 空 modelID：不知道消费哪个模型，静默产零信号比报错更危险。
	emptyModel := NewSignalStrategy("sigtest", "", &fakeSignalStore{filterByAsOf: true})
	require.Error(t, emptyModel.OnBar(ctx, bar), "空 modelID 必须 fail-loud")
}
