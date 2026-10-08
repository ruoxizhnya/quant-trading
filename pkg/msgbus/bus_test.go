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
//
// ─── K0-P2-3 契约变更后的三条新护栏 ───────────────────────────────
//  1. TestHandlerErrorDoesNotBlockOtherHandlersAndIsAggregated —— handler
//     返回 error 时**继续分发**其余 handler，错误由 errors.Join 聚合上报
//     （破坏验证 K0-P2-3-a：改成「遇错即中断」本条红）。
//  2. TestPublishWithCanceledContextDispatchesNothing —— 已取消/已超时的
//     ctx ⇒ 返回 error 且**零分发**（破坏验证 K0-P2-3-b：Publish 忽略传入
//     ctx 改回用总线自持 ctx，本条红）。
//  3. TestNewSyncBusRequiresClock —— clk 是构造期必填，传 nil 直接 panic
//     （旧实现 nil 兜底墙钟，回测误用静默且审计归零）。
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
// 这行是**编译期**断言：哪一边的签名变了（Append 改名、参数变类型、K0-P2-3
// 加的 ctx 被谁悄悄去掉），这里直接编译失败，不会等到一个空指针 Null 在运行
// 期炸。
var _ msgbus.Tap = (*eventstore.PGEventStore)(nil)

// NewSyncBus 的签名守卫（K0-P2-3）：clk 是**构造期必填**的第二个参数。把构造
// 函数改回单参数（= 隐式墙钟兜底）时，本行编译失败。
var _ func(msgbus.Tap, clock.Clock) *msgbus.SyncBus = msgbus.NewSyncBus

// ─── 内存 Tap（替代真库，让分发语义可以被精确观测） ────────────────

// testStart 给「不关心时间」的用例一个确定的回测起点。
var testStart = time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)

// newTestBus 构造一条注入 VirtualClock 的总线。clk 自 K0-P2-3 起构造期必填，
// 所以**没有「不传时钟」的写法**——这就是本次变更要的效果。
func newTestBus(store msgbus.Tap) *msgbus.SyncBus {
	return msgbus.NewSyncBus(store, clock.NewVirtualClock(testStart))
}

// memStore 是内存版的 EventStore：Append 记下来，Replay 按 ts 升序返回。
// 用它而不是真库，是为了让「分发顺序 / 同步性」这类断言不受连接池与
// 事务时延的干扰——那些噪声会把 flaky 掩盖成 Bug，也会把 Bug 掩盖成 flaky。
type memStore struct {
	appended []msgbus.Message
	failWith error // 非 nil 时 Append 一律失败（反证腿用）
	calls    int
}

// Append 落库一条消息。ctx 已取消/超时时不记账并原样返回 ctx 的 error——
// 与 PGEventStore.Append 同构（那里由 mergeTimeout 后的 ctx.Err() 检查给出
// 同样的语义），这样内存替身不会比真库更宽容。
func (m *memStore) Append(ctx context.Context, msg msgbus.Message) error {
	m.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
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
	bus := msgbus.NewSyncBus(store, clk)
	ctx := context.Background()

	var received []msgbus.Message
	if err := bus.Subscribe(msgbus.TopicDataBar, func(_ context.Context, m msgbus.Message) error {
		received = append(received, m)
		return nil
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
		if err := bus.Publish(ctx, msgbus.TopicDataBar, p.payload); err != nil {
			t.Fatalf("Publish(%q) 失败: %v", p.payload, err)
		}
	}

	// ① 订阅者确实收到了全部 3 条。
	if len(received) != len(published) {
		t.Fatalf("订阅者收到 %d 条, want %d", len(received), len(published))
	}
	// ② 每一条收到的消息都在 store 里——逐条比对 topic/ts/payload。
	replayed, err := store.Replay(ctx, time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2200, 1, 2, 0, 0, 0, 0, time.UTC))
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
	bus := newTestBus(store)

	handlerCalls := 0
	if err := bus.Subscribe(msgbus.TopicExecOrderIntent, func(context.Context, msgbus.Message) error {
		handlerCalls++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}

	err := bus.Publish(context.Background(), msgbus.TopicExecOrderIntent, map[string]string{"symbol": "600519.SH"})
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

// TestBusStillUsableAfterTapFailure 落库失败是**这一次发布**
// 的失败，不该把整个总线毒化：换个 topic（换个合法 Tap）还能正常用。
// 用另一条总线 + 正常 store 复验一遍 Publish 仍可用。
func TestBusStillUsableAfterTapFailure(t *testing.T) {
	broken := &memStore{failWith: errors.New("down")}
	brokenBus := newTestBus(broken)
	if err := brokenBus.Publish(context.Background(), msgbus.TopicDataBar, "x"); err == nil {
		t.Fatal("前置条件：坏 Tap 上 Publish 应失败")
	}

	healthy := &memStore{}
	healthyBus := newTestBus(healthy)
	got := 0
	if err := healthyBus.Subscribe(msgbus.TopicDataBar, func(context.Context, msgbus.Message) error {
		got++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}
	if err := healthyBus.Publish(context.Background(), msgbus.TopicDataBar, "y"); err != nil {
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
	bus := newTestBus(&memStore{})

	var calls []string
	for _, name := range []string{"h1", "h2", "h3", "h4"} {
		n := name
		if err := bus.Subscribe(msgbus.TopicPortfolioUpdated, func(context.Context, msgbus.Message) error {
			calls = append(calls, n)
			return nil
		}); err != nil {
			t.Fatalf("Subscribe(%s) 失败: %v", n, err)
		}
	}
	if err := bus.Publish(context.Background(), msgbus.TopicPortfolioUpdated, "nav"); err != nil {
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
	bus := newTestBus(&memStore{})

	var trace []string
	if err := bus.Subscribe(msgbus.TopicExecFill, func(context.Context, msgbus.Message) error {
		trace = append(trace, "A-enter")
		trace = append(trace, "A-exit")
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(A) 失败: %v", err)
	}
	if err := bus.Subscribe(msgbus.TopicExecFill, func(context.Context, msgbus.Message) error {
		// A 跑完了才会有这条 "A-exit"；并发分发时它可能在 "A-enter" 之前出现。
		if len(trace) == 0 || trace[len(trace)-1] != "A-exit" {
			trace = append(trace, "B-saw-partial")
			return nil
		}
		trace = append(trace, "B-saw-A-done")
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(B) 失败: %v", err)
	}

	if err := bus.Publish(context.Background(), msgbus.TopicExecFill, "fill-1"); err != nil {
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
	bus := newTestBus(&memStore{})

	const handlerCount = 5
	done := make([]bool, handlerCount)
	for i := 0; i < handlerCount; i++ {
		idx := i
		if err := bus.Subscribe(msgbus.TopicRunDone, func(context.Context, msgbus.Message) error {
			// 每个 handler 都做一点可被观测的工作，别让编译器/调度器有空子。
			time.Sleep(time.Millisecond)
			done[idx] = true
			return nil
		}); err != nil {
			t.Fatalf("Subscribe(%d) 失败: %v", i, err)
		}
	}

	if err := bus.Publish(context.Background(), msgbus.TopicRunDone, "run-42"); err != nil {
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
	bus := newTestBus(&memStore{})

	var barCalls, fillCalls int
	if err := bus.Subscribe(msgbus.TopicDataBar, func(context.Context, msgbus.Message) error {
		barCalls++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(bar) 失败: %v", err)
	}
	if err := bus.Subscribe(msgbus.TopicExecFill, func(context.Context, msgbus.Message) error {
		fillCalls++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(fill) 失败: %v", err)
	}

	if err := bus.Publish(context.Background(), msgbus.TopicDataBar, "bar"); err != nil {
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
	bus := newTestBus(&memStore{})
	ctx := context.Background()

	for _, topic := range []string{"", "data.barr", "DATA.BAR", "exec.fill.v2", "随便写的"} {
		err := bus.Subscribe(topic, func(context.Context, msgbus.Message) error { return nil })
		if !errors.Is(err, msgbus.ErrUnknownTopic) {
			t.Errorf("Subscribe(%q) = %v, want errors.Is(err, ErrUnknownTopic)", topic, err)
		}
		err = bus.Publish(ctx, topic, "x")
		if !errors.Is(err, msgbus.ErrUnknownTopic) {
			t.Errorf("Publish(%q) = %v, want errors.Is(err, ErrUnknownTopic)", topic, err)
		}
	}
}

// TestEveryRegisteredTopicIsPublishable 防空转腿：注册表里的 16 个 topic
// 每个都能成功 Publish（不是「判错了导致全被拒」）。
func TestEveryRegisteredTopicIsPublishable(t *testing.T) {
	bus := newTestBus(&memStore{})
	ctx := context.Background()

	for _, topic := range msgbus.RegisteredTopics() {
		if err := bus.Publish(ctx, topic, "probe"); err != nil {
			t.Errorf("Publish(%q) = %v, want nil（注册过的 topic 必须可发布）", topic, err)
		}
		if err := bus.Subscribe(topic, func(context.Context, msgbus.Message) error { return nil }); err != nil {
			t.Errorf("Subscribe(%q) = %v, want nil", topic, err)
		}
	}
	if got := len(msgbus.RegisteredTopics()); got != 16 {
		t.Errorf("RegisteredTopics() = %d 个, want 16（切片 1 的 9 + 切片 2 的 7）", got)
	}
}

// TestSubscribeNilHandlerRejected nil handler 是装配期错误，早拒早安生。
func TestSubscribeNilHandlerRejected(t *testing.T) {
	bus := newTestBus(&memStore{})
	if err := bus.Subscribe(msgbus.TopicDataBar, nil); !errors.Is(err, msgbus.ErrNilHandler) {
		t.Errorf("Subscribe(nil) = %v, want errors.Is(err, ErrNilHandler)", err)
	}
}

// TestPublishWithoutTapIsRefused 没装 Tap 的总线不许广播——「先记录后分发」
// 缺一半就是全缺。
func TestPublishWithoutTapIsRefused(t *testing.T) {
	bus := msgbus.NewSyncBus(nil, clock.NewVirtualClock(testStart))
	if err := bus.Publish(context.Background(), msgbus.TopicDataBar, "x"); !errors.Is(err, msgbus.ErrNoTap) {
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
		bus := msgbus.NewSyncBus(store, clk)

		var got []time.Time
		if err := bus.Subscribe(msgbus.TopicDataBar, func(_ context.Context, m msgbus.Message) error {
			got = append(got, m.Ts())
			return nil
		}); err != nil {
			t.Fatalf("Subscribe 失败: %v", err)
		}
		for i := 1; i <= 3; i++ {
			ts := time.Date(2200, 3, 1, 9, 30, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
			if err := clk.Advance(ts); err != nil {
				t.Fatalf("Advance 失败: %v", err)
			}
			if err := bus.Publish(context.Background(), msgbus.TopicDataBar, i); err != nil {
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

// TestLiveBusStampsWallClock 实盘形态（NewLiveBus = 显式注入 LiveClock）的
// 消息 ts 落在调用本身的真实时间窗内。
//
// 注意它替代的是旧版 TestPublishWithoutClockFallsBackToWallClock：K0-P2-3
// 之后**不存在「不传时钟」的总线**（clk 构造期必填），墙钟只能显式声明。
func TestLiveBusStampsWallClock(t *testing.T) {
	store := &memStore{}
	bus := msgbus.NewLiveBus(store)

	var got time.Time
	if err := bus.Subscribe(msgbus.TopicDataBar, func(_ context.Context, m msgbus.Message) error {
		got = m.Ts()
		return nil
	}); err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}

	before := time.Now().Add(-time.Second)
	if err := bus.Publish(context.Background(), msgbus.TopicDataBar, "x"); err != nil {
		t.Fatalf("Publish 失败: %v", err)
	}
	after := time.Now().Add(time.Second)

	if got.Before(before) || got.After(after) {
		t.Errorf("消息 ts = %s, 未落在 [%s, %s]——既不虚拟也不墙钟", got, before, after)
	}
}

// ─── K0-P2-3 ①：handler 返回 error —— 继续分发 + 聚合上报 ────────────

// TestHandlerErrorDoesNotBlockOtherHandlersAndIsAggregated —— 破坏验证
// K0-P2-3-a 的红灯。
//
// 裁决：某个 handler 返回 error 时**不中断**，其余 handler 照常收到这条消息；
// 全部 error 由 errors.Join 聚合后随 Publish 返回。
//
// 反向（遇错即中断）的代价是「一个订阅者的失败让其他订阅者收不到消息」——
// 订阅者之间契约上互不认识，那等于把单点故障扩散成全总线静默丢消息。
func TestHandlerErrorDoesNotBlockOtherHandlersAndIsAggregated(t *testing.T) {
	bus := newTestBus(&memStore{})

	errA := errors.New("订阅者 A 处理失败")
	errC := errors.New("订阅者 C 处理失败")
	var calls []string
	subscribe := func(name string, ret error) {
		if err := bus.Subscribe(msgbus.TopicRiskVerdict, func(context.Context, msgbus.Message) error {
			calls = append(calls, name)
			return ret
		}); err != nil {
			t.Fatalf("Subscribe(%s) 失败: %v", name, err)
		}
	}
	subscribe("A", errA)
	subscribe("B", nil)
	subscribe("C", errC)

	err := bus.Publish(context.Background(), msgbus.TopicRiskVerdict, "verdict")
	if err == nil {
		t.Fatal("有 handler 报错时 Publish 返回 nil——错误被吞了")
	}
	// ① 其余 handler 仍然被叫到（不中断）。
	want := []string{"A", "B", "C"}
	if len(calls) != len(want) {
		t.Fatalf("handler 调用序列 = %v, want %v（某个 handler 报错不得拦住后面的订阅者）", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("第 %d 个执行的 handler = %s, want %s", i, calls[i], want[i])
		}
	}
	// ② 聚合：两个原始 error 都能被 errors.Is 判定到（errors.Join 穿透）。
	if !errors.Is(err, errA) {
		t.Errorf("Publish 返回 %v, want errors.Is(err, errA)（聚合后必须仍能定位到具体订阅者的错误）", err)
	}
	if !errors.Is(err, errC) {
		t.Errorf("Publish 返回 %v, want errors.Is(err, errC)", err)
	}
}

// TestPublishNilErrorWhenAllHandlersSucceed 防空转腿：handler 都返回 nil 时
// Publish 必须返回 nil（聚合逻辑不能凭空造出一个 error）。
func TestPublishNilErrorWhenAllHandlersSucceed(t *testing.T) {
	bus := newTestBus(&memStore{})
	for i := 0; i < 3; i++ {
		if err := bus.Subscribe(msgbus.TopicRunStart, func(context.Context, msgbus.Message) error {
			return nil
		}); err != nil {
			t.Fatalf("Subscribe(%d) 失败: %v", i, err)
		}
	}
	if err := bus.Publish(context.Background(), msgbus.TopicRunStart, "run"); err != nil {
		t.Errorf("全部 handler 成功时 Publish = %v, want nil", err)
	}
}

// TestHandlerPanicStillPropagates 契约变更改的是「error 的聚合方式」，
// **没有**改「panic 不 recover」这条：panic 是编程错误，照旧炸穿 Publish。
//
// 顺带钉住一个副作用事实：panic 发生在**落库之后**，所以消息仍在库里
// （「先记录后分发」不受 handler 失败形态的影响）。
func TestHandlerPanicStillPropagates(t *testing.T) {
	store := &memStore{}
	bus := newTestBus(store)

	afterPanicCalls := 0
	if err := bus.Subscribe(msgbus.TopicExecFill, func(context.Context, msgbus.Message) error {
		panic("订阅者内部编程错误")
	}); err != nil {
		t.Fatalf("Subscribe(panic 方) 失败: %v", err)
	}
	if err := bus.Subscribe(msgbus.TopicExecFill, func(context.Context, msgbus.Message) error {
		afterPanicCalls++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(后置方) 失败: %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Error("handler panic 没有向上传播——契约规定 panic 不 recover（fail-loud）")
		}
		if len(store.appended) != 1 {
			t.Errorf("panic 之后 store 里有 %d 条, want 1（先记录后分发：panic 不影响已落库的那条）",
				len(store.appended))
		}
	}()
	_ = bus.Publish(context.Background(), msgbus.TopicExecFill, "fill")
}

// ─── K0-P2-3 ②：Publish 的 ctx —— 取消即零分发 ──────────────────────

// TestPublishWithCanceledContextDispatchesNothing —— 破坏验证 K0-P2-3-b 的
// 红灯。
//
// 裁决：已取消/已超时的 ctx ⇒ Publish 返回 error 且**零分发**（连落库都不
// 尝试）。与「先记录后分发」同向：取消是调用方对「这一次发布」的裁决，
// 只跳过派发而仍落库会写出一条没人处理的审计行。
func TestPublishWithCanceledContextDispatchesNothing(t *testing.T) {
	cases := []struct {
		name    string
		ctx     func() context.Context
		wantErr error // ctx.Err() 的期望值，必须能 errors.Is 到
	}{
		{
			name: "已取消",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			wantErr: context.Canceled,
		},
		{
			name: "已超时",
			ctx: func() context.Context {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer cancel()
				return ctx
			},
			wantErr: context.DeadlineExceeded,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &memStore{}
			bus := newTestBus(store)

			handlerCalls := 0
			if err := bus.Subscribe(msgbus.TopicDataBar, func(context.Context, msgbus.Message) error {
				handlerCalls++
				return nil
			}); err != nil {
				t.Fatalf("Subscribe 失败: %v", err)
			}

			err := bus.Publish(tc.ctx(), msgbus.TopicDataBar, "bar")
			if err == nil {
				t.Fatal("取消的 ctx 上 Publish 返回 nil——取消被忽略了")
			}
			if !errors.Is(err, msgbus.ErrContextCanceled) {
				t.Errorf("error = %v, want errors.Is(err, msgbus.ErrContextCanceled)", err)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want errors.Is(err, %v)（原始取消原因必须留在错误链上）", err, tc.wantErr)
			}
			if handlerCalls != 0 {
				t.Errorf("handler 被调用 %d 次, want 0（ctx 取消 = 零分发）", handlerCalls)
			}
			if store.calls != 0 {
				t.Errorf("Append 被调用 %d 次, want 0（ctx 取消时连落库都不该尝试）", store.calls)
			}
			if len(store.appended) != 0 {
				t.Errorf("store 里留下了 %d 条, want 0", len(store.appended))
			}
		})
	}
}

// TestPublishPassesCallerContextToHandlers ctx 必须**逐消息**透传给 handler：
// 同一条总线用两个不同 value 的 ctx 发布，handler 看到的就是当次那个。
//
// 这条同时是「移除 SyncBus.ctx 字段」的回归护栏——旧实现所有消息共享构造时
// 持有的那一个 ctx，per-message 取消无从谈起。
func TestPublishPassesCallerContextToHandlers(t *testing.T) {
	bus := newTestBus(&memStore{})

	var seen []string
	if err := bus.Subscribe(msgbus.TopicDataBar, func(ctx context.Context, _ msgbus.Message) error {
		seen = append(seen, ctx.Value(runKey{}).(string))
		return nil
	}); err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}

	for _, name := range []string{"run-1", "run-2"} {
		ctx := context.WithValue(context.Background(), runKey{}, name)
		if err := bus.Publish(ctx, msgbus.TopicDataBar, "bar"); err != nil {
			t.Fatalf("Publish(%s) 失败: %v", name, err)
		}
	}

	want := []string{"run-1", "run-2"}
	if len(seen) != len(want) {
		t.Fatalf("handler 看到的 ctx = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("第 %d 次 handler 看到的 ctx = %q, want %q（ctx 必须逐消息透传）", i, seen[i], want[i])
		}
	}
}

// runKey 是 TestPublishPassesCallerContextToHandlers 专用的 ctx key 类型
//（不导出，避免与别的测试/包的 key 撞车）。
type runKey struct{}

// ─── K0-P2-3 ③：时钟构造期强制 ─────────────────────────────────────

// TestNewSyncBusRequiresClock 「没有时间源的总线」必须造不出来。
//
// 旧实现的 clk 为 nil 时兜底墙钟：回测装配漏注入 VirtualClock 时编译照过、
// 运行照跑，audit.message_log 落的是真实世界时刻，回放无法比对——**而且全程
// 不报错**（静默陷阱）。现在改为构造期 panic，把故障钉在离错误最近的地方。
func TestNewSyncBusRequiresClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewSyncBus(tap, nil) 未 panic——隐式墙钟兜底又回来了，" +
				"回测误用会静默把真实世界时刻写进 audit.message_log")
		}
	}()
	_ = msgbus.NewSyncBus(&memStore{}, nil)
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
