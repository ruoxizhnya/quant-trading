// K1 切片 1：pkg/msgbus 的 SyncBus 行为测试（不碰数据库）。
//
// ─── 本文件守的核心不变量：BusTap「先记录后分发」 ─────────────────
// 两个方向都必须有证据，缺一个就等于没测：
//
//	正证据 TestPublishReplayedInOrder —— 订阅者收到的每一条，都能从
//	       EventStore 里 Replay 出来，且**顺序一致**。
//	反证腿 TestAppendFailureBlocksDispatch —— Append 失败时 Publish 返回
//	       error 且 handler **一次都不被调用**（证明「先记录」真拦在分发
//	       之前，而不是事后补记、也不是并发分发碰巧先跑到）。
//
// 加上 TestHandlersRunInRegistrationOrder / TestPublishIsSynchronous 一起，
// 破坏验证 a（把 Append 挪到分发之后）与 b（改 goroutine 并发分发）各有各的
// 红灯。
package msgbus_test

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/eventstore"
	"github.com/ruoxizhnya/quant-trading/pkg/msgbus"
)

// PGEventStore 必须满足总线要求的窄接口 Tap。见 msgbus.Tap 的注释：本该写
// `func NewSyncBus(store eventstore.EventStore)`，但 eventstore import 了
// msgbus，反过来 import 就是导入环，故接口由消费方 msgbus 定义。
//
// 这行是**编译期**断言：哪一边的签名变了（Append 改名、参数变类型），这里
// 直接编译失败，不会等到一个空指针 Null 在运行期炸。
var _ msgbus.Tap = (*eventstore.PGEventStore)(nil)

// ─── 内存 Tap（替代真库，让分发语义可以被精确观测） ────────────────

// memStore 是内存版的 EventStore：Append 记下来，Replay 按 ts 升序返回。
// 用它而不是真库，是为了让「分发顺序 / 同步性」这类断言不受连接池与
// 事务时延的干扰——那些噪声会把 flaky 掩盖成 Bug，也会把 Bug 掩盖成 flaky。
type memStore struct {
	appended []msgbus.Message
	failWith error // 非 nil 时 Append 一律失败（反证腿用）
	calls    int
}

func (m *memStore) Append(msg msgbus.Message) error {
	m.calls++
	if m.failWith != nil {
		return m.failWith
	}
	m.appended = append(m.appended, msg)
	return nil
}

// Replay 按 ts 升序返回（同 ts 保持 Append 顺序），对应 eventstore 的
// ORDER BY ts ASC, id ASC。
func (m *memStore) Replay(_ context.Context, from, to time.Time) ([]msgbus.Message, error) {
	out := make([]msgbus.Message, 0, len(m.appended))
	for _, msg := range m.appended {
		if !msg.Ts().Before(from) && !msg.Ts().After(to) {
			out = append(out, msg)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ts().Before(out[j].Ts()) })
	return out, nil
}

func (m *memStore) Verify() error { return nil }

// memStore 也应被真 store 的实现者关系参照：确认结构与 interface 同形。
var _ msgbus.Tap = (*memStore)(nil)

// ─── BusTap 正证据 ────────────────────────────────────────────────

// TestPublishReplayedInOrder —— 验收第 3 条的正证据。
//
// 「订阅者收到的每一条都能在 eventstore 里 Replay 查到，且顺序一致」。
// 这一条是 BusTap 的全部价值：订阅者看到的 == 事后回放出来的。
func TestPublishReplayedInOrder(t *testing.T) {
	store := &memStore{}
	clk := clock.NewVirtualClock(time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC))
	bus := msgbus.NewSyncBusWithClock(store, clk)

	var received []msgbus.Message
	if err := bus.Subscribe(msgbus.TopicDataBar, func(_ context.Context, m msgbus.Message) {
		received = append(received, m)
	}); err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}

	published := []struct {
		ts      time.Time
		payload string
	}{
		{time.Date(2200, 1, 1, 9, 30, 0, 0, time.UTC), "bar-1"},
		{time.Date(2200, 1, 1, 9, 31, 0, 0, time.UTC), "bar-2"},
		{time.Date(2200, 1, 1, 9, 32, 0, 0, time.UTC), "bar-3"},
	}
	for _, p := range published {
		if err := clk.Advance(p.ts); err != nil {
			t.Fatalf("推进虚拟时钟失败: %v", err)
		}
		if err := bus.Publish(msgbus.TopicDataBar, p.payload); err != nil {
			t.Fatalf("Publish(%q) 失败: %v", p.payload, err)
		}
	}

	// ① 订阅者确实收到了全部 3 条。
	if len(received) != len(published) {
		t.Fatalf("订阅者收到 %d 条, want %d", len(received), len(published))
	}
	// ② 每一条收到的消息都在 store 里——逐条比对 topic/ts/payload。
	replayed, err := store.Replay(context.Background(), time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2200, 1, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Replay 失败: %v", err)
	}
	if len(replayed) != len(received) {
		t.Fatalf("Replay 返回 %d 条, 订阅者收到 %d 条——两者必须一一对应（BusTap 的核心）",
			len(replayed), len(received))
	}
	for i := range received {
		if replayed[i].Topic() != received[i].Topic() {
			t.Errorf("第 %d 条 topic: 回放 %q vs 收到 %q", i, replayed[i].Topic(), received[i].Topic())
		}
		if !replayed[i].Ts().Equal(received[i].Ts()) {
			t.Errorf("第 %d 条 ts: 回放 %s vs 收到 %s", i, replayed[i].Ts(), received[i].Ts())
		}
		if replayed[i].Payload() != received[i].Payload() {
			t.Errorf("第 %d 条 payload: 回放 %v vs 收到 %v", i, replayed[i].Payload(), received[i].Payload())
		}
	}
	// ③ 顺序一致（这才叫「同步分发」可观测）。
	for i, want := range published {
		if received[i].Payload() != want.payload {
			t.Errorf("第 %d 条收到的 payload = %v, want %v（顺序一致）", i, received[i].Payload(), want.payload)
		}
		if !received[i].Ts().Equal(want.ts) {
			t.Errorf("第 %d 条 ts = %s, want %s（消息 ts 必须来自注入的 VirtualClock）",
				i, received[i].Ts(), want.ts)
		}
	}
}

// ─── BusTap 反证腿 ────────────────────────────────────────────────

// TestAppendFailureBlocksDispatch —— 验收第 3 条的反证腿，也是破坏验证 a
// （把 Append 挪到分发之后）的红灯。
//
// Append 失败 ⇒ Publish 必须返回 error，且**一个 handler 都不许跑**。
// 这条证明了「先记录」是一道真闸门，而不是「记录失败也照发」的软建议：
// 若落成软建议，audit.message_log 会静默缺行，而缺口只有在事后对账时才
// 暴露——那时现场早没了（这正是 BusTap 要防的事）。
//
// 判据是硬的：**哪怕只溜过去一条**，就说明 BusTap 被绕过——因此断言的是
// 「handler 调用次数 == 0」，而不是「消息最终没被处理」这种能被事后补救的
// 软说法。
func TestAppendFailureBlocksDispatch(t *testing.T) {
	appendErr := errors.New("注入的落库故障")
	store := &memStore{failWith: appendErr}
	bus := msgbus.NewSyncBus(store)

	handlerCalls := 0
	if err := bus.Subscribe(msgbus.TopicExecOrderIntent, func(context.Context, msgbus.Message) {
		handlerCalls++
	}); err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}

	err := bus.Publish(msgbus.TopicExecOrderIntent, map[string]string{"symbol": "600519.SH"})
	if err == nil {
		t.Fatal("Append 失败时 Publish 返回 nil——未记录的消息被放行，BusTap 失效")
	}
	if !errors.Is(err, msgbus.ErrAppendFailed) {
		t.Errorf("error = %v, want errors.Is(err, msgbus.ErrAppendFailed)", err)
	}
	if !errors.Is(err, appendErr) {
		t.Errorf("error = %v, want 能 errors.Is 到原始落库错误（错误链不能断）", err)
	}
	if handlerCalls != 0 {
		t.Errorf("handler 被调用 %d 次, want 0（落库失败的消息一个订阅者都不该看见）", handlerCalls)
	}
	if store.appended != nil {
		t.Errorf("store 里留下了 %d 条记录, want 0", len(store.appended))
	}
}

// TestAppendFailureDoesNotTouchOtherTopicsQueue 落库失败是**这一次发布**
// 的失败，不该把整个总线毒化：换个 topic（换个合法 Tap）还能正常用。
// 用另一条总线 + 正常 store 复验一遍 Publish 仍可用。
func TestBusStillUsableAfterTapFailure(t *testing.T) {
	broken := &memStore{failWith: errors.New("down")}
	brokenBus := msgbus.NewSyncBus(broken)
	if err := brokenBus.Publish(msgbus.TopicDataBar, "x"); err == nil {
		t.Fatal("前置条件：坏 Tap 上 Publish 应失败")
	}

	healthy := &memStore{}
	healthyBus := msgbus.NewSyncBus(healthy)
	got := 0
	if err := healthyBus.Subscribe(msgbus.TopicDataBar, func(context.Context, msgbus.Message) { got++ }); err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}
	if err := healthyBus.Publish(msgbus.TopicDataBar, "y"); err != nil {
		t.Fatalf("新总线 Publish 失败（一个坏实例污染了别的实例？）: %v", err)
	}
	if got != 1 {
		t.Errorf("handler 调用 %d 次, want 1", got)
	}
}

// ─── 同步 / 顺序 ──────────────────────────────────────────────────

// TestHandlersRunInRegistrationOrder handler 按订阅顺序执行。
// 破坏验证 b（改 goroutine 并发分发）的第一盏红灯。
func TestHandlersRunInRegistrationOrder(t *testing.T) {
	bus := msgbus.NewSyncBus(&memStore{})

	var calls []string
	for _, name := range []string{"h1", "h2", "h3", "h4"} {
		n := name
		if err := bus.Subscribe(msgbus.TopicPortfolioUpdated, func(context.Context, msgbus.Message) {
			calls = append(calls, n)
		}); err != nil {
			t.Fatalf("Subscribe(%s) 失败: %v", n, err)
		}
	}
	if err := bus.Publish(msgbus.TopicPortfolioUpdated, "nav"); err != nil {
		t.Fatalf("Publish 失败: %v", err)
	}

	want := []string{"h1", "h2", "h3", "h4"}
	if len(calls) != len(want) {
		t.Fatalf("handler 调用序列 = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("第 %d 个执行的 handler = %s, want %s（必须按注册顺序）", i, calls[i], want[i])
		}
	}
}

// TestHandlersObserveEachOthersSideEffects 比「顺序」更强的证据：handler A
// 的副作用在 handler B 执行时**已经可见**。
//
// 若分发是并发的，B 看到的 A 是否已完成是不确定的——配合 -race 跑会直接报
// 数据竞争。这是「单线程语义」真正被实现出来的证据，而不是仅仅没炸。
func TestHandlersObserveEachOthersSideEffects(t *testing.T) {
	bus := msgbus.NewSyncBus(&memStore{})

	var trace []string
	if err := bus.Subscribe(msgbus.TopicExecFill, func(context.Context, msgbus.Message) {
		trace = append(trace, "A-enter")
		trace = append(trace, "A-exit")
	}); err != nil {
		t.Fatalf("Subscribe(A) 失败: %v", err)
	}
	if err := bus.Subscribe(msgbus.TopicExecFill, func(context.Context, msgbus.Message) {
		// A 跑完了才会有这条 "A-exit"；并发分发时它可能在 "A-enter" 之前出现。
		if len(trace) == 0 || trace[len(trace)-1] != "A-exit" {
			trace = append(trace, "B-saw-partial")
			return
		}
		trace = append(trace, "B-saw-A-done")
	}); err != nil {
		t.Fatalf("Subscribe(B) 失败: %v", err)
	}

	if err := bus.Publish(msgbus.TopicExecFill, "fill-1"); err != nil {
		t.Fatalf("Publish 失败: %v", err)
	}

	want := []string{"A-enter", "A-exit", "B-saw-A-done"}
	if len(trace) != len(want) {
		t.Fatalf("执行轨迹 = %v, want %v（B 看到了不完整状态说明分发不是同步串行的）", trace, want)
	}
	for i := range want {
		if trace[i] != want[i] {
			t.Errorf("轨迹第 %d 步 = %s, want %s", i, trace[i], want[i])
		}
	}
}

// TestPublishIsSynchronous Publish 返回时，全部 handler 必须已经跑完。
// 破坏验证 b 的第二盏红灯：goroutine 版会让「Publish 返回」与「handler 跑完」
// 脱钩。
func TestPublishIsSynchronous(t *testing.T) {
	bus := msgbus.NewSyncBus(&memStore{})

	const handlerCount = 5
	done := make([]bool, handlerCount)
	for i := 0; i < handlerCount; i++ {
		idx := i
		if err := bus.Subscribe(msgbus.TopicRunDone, func(context.Context, msgbus.Message) {
			// 每个 handler 都做一点可被观测的工作，别让编译器/调度器有空子。
			time.Sleep(time.Millisecond)
			done[idx] = true
		}); err != nil {
			t.Fatalf("Subscribe(%d) 失败: %v", i, err)
		}
	}

	if err := bus.Publish(msgbus.TopicRunDone, "run-42"); err != nil {
		t.Fatalf("Publish 失败: %v", err)
	}

	// Publish 已返回：此刻所有 handler 都必须是已完成状态，一个都不能漏。
	for i, ok := range done {
		if !ok {
			t.Errorf("Publish 返回后第 %d 个 handler 还没跑完——分发不是同步的（goroutine 派发？）", i)
		}
	}
}

// TestPublishOnlyReachesSubscribersOfThatTopic 订阅隔离：别的 topic 的
// 订阅者不会被叫醒。
func TestPublishOnlyReachesSubscribersOfThatTopic(t *testing.T) {
	bus := msgbus.NewSyncBus(&memStore{})

	var barCalls, fillCalls int
	if err := bus.Subscribe(msgbus.TopicDataBar, func(context.Context, msgbus.Message) { barCalls++ }); err != nil {
		t.Fatalf("Subscribe(bar) 失败: %v", err)
	}
	if err := bus.Subscribe(msgbus.TopicExecFill, func(context.Context, msgbus.Message) { fillCalls++ }); err != nil {
		t.Fatalf("Subscribe(fill) 失败: %v", err)
	}

	if err := bus.Publish(msgbus.TopicDataBar, "bar"); err != nil {
		t.Fatalf("Publish 失败: %v", err)
	}
	if barCalls != 1 {
		t.Errorf("bar 订阅者调用 %d 次, want 1", barCalls)
	}
	if fillCalls != 0 {
		t.Errorf("fill 订阅者调用 %d 次, want 0（topic 隔离）", fillCalls)
	}
}

// ─── topic 校验 ───────────────────────────────────────────────────

// TestPublishAndSubscribeRejectUnknownTopic 未注册 topic 在**订阅期**就必须
// 报错——订阅一个永远收不到消息的 topic，运行期症状是消息静默丢失，
// 而现场一点线索都没有。
func TestPublishAndSubscribeRejectUnknownTopic(t *testing.T) {
	bus := msgbus.NewSyncBus(&memStore{})

	for _, topic := range []string{"", "data.barr", "DATA.BAR", "exec.fill.v2", "随便写的"} {
		err := bus.Subscribe(topic, func(context.Context, msgbus.Message) {})
		if !errors.Is(err, msgbus.ErrUnknownTopic) {
			t.Errorf("Subscribe(%q) = %v, want errors.Is(err, ErrUnknownTopic)", topic, err)
		}
		err = bus.Publish(topic, "x")
		if !errors.Is(err, msgbus.ErrUnknownTopic) {
			t.Errorf("Publish(%q) = %v, want errors.Is(err, ErrUnknownTopic)", topic, err)
		}
	}
}

// TestEveryRegisteredTopicIsPublishable 防空转腿：注册表里的 16 个 topic
// 每个都能成功 Publish（不是「判错了导致全被拒」）。
func TestEveryRegisteredTopicIsPublishable(t *testing.T) {
	bus := msgbus.NewSyncBus(&memStore{})

	for _, topic := range msgbus.RegisteredTopics() {
		if err := bus.Publish(topic, "probe"); err != nil {
			t.Errorf("Publish(%q) = %v, want nil（注册过的 topic 必须可发布）", topic, err)
		}
		if err := bus.Subscribe(topic, func(context.Context, msgbus.Message) {}); err != nil {
			t.Errorf("Subscribe(%q) = %v, want nil", topic, err)
		}
	}
	if got := len(msgbus.RegisteredTopics()); got != 16 {
		t.Errorf("RegisteredTopics() = %d 个, want 16（切片 1 的 9 + 切片 2 的 7）", got)
	}
}

// TestSubscribeNilHandlerRejected nil handler 是装配期错误，早拒早安生。
func TestSubscribeNilHandlerRejected(t *testing.T) {
	bus := msgbus.NewSyncBus(&memStore{})
	if err := bus.Subscribe(msgbus.TopicDataBar, nil); !errors.Is(err, msgbus.ErrNilHandler) {
		t.Errorf("Subscribe(nil) = %v, want errors.Is(err, ErrNilHandler)", err)
	}
}

// TestPublishWithoutTapIsRefused 没装 Tap 的总线不许广播——「先记录后分发」
// 缺一半就是全缺。
func TestPublishWithoutTapIsRefused(t *testing.T) {
	bus := msgbus.NewSyncBus(nil)
	if err := bus.Publish(msgbus.TopicDataBar, "x"); !errors.Is(err, msgbus.ErrNoTap) {
		t.Errorf("Publish 无 Tap = %v, want errors.Is(err, ErrNoTap)", err)
	}
}

// ─── 时间戳来源 ───────────────────────────────────────────────────

// TestPublishStampComesFromInjectedClock 回测确定性：消息 ts 必须来自注入的
// VirtualClock（数据时间），而不是墙钟。两次「同一虚拟时间序列」回放必须得到
// 同一串 ts。
func TestPublishStampComesFromInjectedClock(t *testing.T) {
	run := func() []time.Time {
		store := &memStore{}
		clk := clock.NewVirtualClock(time.Date(2200, 3, 1, 0, 0, 0, 0, time.UTC))
		bus := msgbus.NewSyncBusWithClock(store, clk)

		var got []time.Time
		if err := bus.Subscribe(msgbus.TopicDataBar, func(_ context.Context, m msgbus.Message) {
			got = append(got, m.Ts())
		}); err != nil {
			t.Fatalf("Subscribe 失败: %v", err)
		}
		for i := 1; i <= 3; i++ {
			ts := time.Date(2200, 3, 1, 9, 30, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
			if err := clk.Advance(ts); err != nil {
				t.Fatalf("Advance 失败: %v", err)
			}
			if err := bus.Publish(msgbus.TopicDataBar, i); err != nil {
				t.Fatalf("Publish 失败: %v", err)
			}
		}
		return got
	}

	first, second := run(), run()
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("收到 %d / %d 条, want 3 / 3", len(first), len(second))
	}
	for i := range first {
		if !first[i].Equal(second[i]) {
			t.Errorf("第 %d 条 ts 不确定: %s vs %s——ts 没有全部来自 VirtualClock", i, first[i], second[i])
		}
	}
}

// TestPublishWithoutClockFallsBackToWallClock 不注入时钟时退化为墙钟（实盘
// 形态）。读数必须落在调用本身的真实时间窗内——否则是某个冻结值。
func TestPublishWithoutClockFallsBackToWallClock(t *testing.T) {
	store := &memStore{}
	bus := msgbus.NewSyncBus(store)

	var got time.Time
	if err := bus.Subscribe(msgbus.TopicDataBar, func(_ context.Context, m msgbus.Message) { got = m.Ts() }); err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}

	before := time.Now().Add(-time.Second)
	if err := bus.Publish(msgbus.TopicDataBar, "x"); err != nil {
		t.Fatalf("Publish 失败: %v", err)
	}
	after := time.Now().Add(time.Second)

	if got.Before(before) || got.After(after) {
		t.Errorf("消息 ts = %s, 未落在 [%s, %s]——既不虚拟也不墙钟", got, before, after)
	}
}

// ─── Envelope ─────────────────────────────────────────────────────

// TestEnvelopeAccessors 信封三个访问器必须与构造时一致，且 payload 是同一个
// 对象（总线不拷贝、不加工 payload——加工是 eventstore 落库时的事）。
func TestEnvelopeAccessors(t *testing.T) {
	ts := time.Date(2200, 5, 1, 0, 0, 0, 0, time.UTC)
	payload := map[string]float64{"close": 10.5}
	e := msgbus.NewEnvelope(msgbus.TopicExecOrderIntent, ts, payload)

	if e.Topic() != msgbus.TopicExecOrderIntent {
		t.Errorf("Topic() = %q, want %q", e.Topic(), msgbus.TopicExecOrderIntent)
	}
	if !e.Ts().Equal(ts) {
		t.Errorf("Ts() = %s, want %s", e.Ts(), ts)
	}
	got, ok := e.Payload().(map[string]float64)
	if !ok {
		t.Fatalf("Payload() 类型 = %T, want map[string]float64（总线不加工 payload）", e.Payload())
	}
	if got["close"] != 10.5 {
		t.Errorf("Payload() = %v, want 原对象", got)
	}
	var m msgbus.Message = e
	if m.Topic() != msgbus.TopicExecOrderIntent {
		t.Errorf("作为 Message 使用时 Topic() = %q", m.Topic())
	}
}
