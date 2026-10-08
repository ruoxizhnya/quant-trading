// K1 切片 2：pkg/kernel 内核生命周期行为测试（纯内存，不碰 DB）。
//
// 覆盖（对应任务书 3.3）：
//   - Boot 严格按 BootOrder 逐个 Init→Start；
//   - 某模块 Start 失败 → 已启动的**逆序** Stop 回滚，失败模块自己不被 Stop；
//   - Shutdown 按 BootOrder **逆序** Stop，且**幂等**（重复调用不重复 Stop）；
//   - Module(name) 未注册返回 error；
//   - NoopModule 可注册、可检索；
//   - 关停时序：kernel.shutdown 在「停 msgbus 模块」之前发出（D4 落库前提）。
package kernel_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/kernel"
	"github.com/ruoxizhnya/quant-trading/pkg/msgbus"
)

// ─── 测试脚手架 ────────────────────────────────────────────────────

// recorder 顺序记录「发生了什么」。fakeModule 与 fakeBus 共享同一个
// recorder，于是「模块生命周期调用」与「总线发布」落在同一条时间线上——
// 这是断言「先发消息再停 msgbus」这类**跨部件时序**的关键。
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(e string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

// fakeModule 实现 kernel.Module，把每次生命周期调用记进 rec。
// failInit/failStart/failStop 用于注入失败。
type fakeModule struct {
	name      string
	rec       *recorder
	failInit  bool
	failStart bool
	failStop  bool
}

func (m *fakeModule) Name() string { return m.name }

func (m *fakeModule) Init(context.Context) error {
	m.rec.add("init:" + m.name)
	if m.failInit {
		return errors.New("boom-init:" + m.name)
	}
	return nil
}

func (m *fakeModule) Start(context.Context) error {
	m.rec.add("start:" + m.name)
	if m.failStart {
		return errors.New("boom-start:" + m.name)
	}
	return nil
}

func (m *fakeModule) Stop(context.Context) error {
	m.rec.add("stop:" + m.name)
	if m.failStop {
		return errors.New("boom-stop:" + m.name)
	}
	return nil
}

// fakeBus 实现 msgbus.MsgBus，把 Publish 记进同一个 rec。
type fakeBus struct {
	rec    *recorder
	topics []string
}

func (b *fakeBus) Publish(_ context.Context, topic string, _ any) error {
	b.rec.add("publish:" + topic)
	b.topics = append(b.topics, topic)
	return nil
}

func (b *fakeBus) Subscribe(string, msgbus.Handler) error { return nil }

// newKernelWithFakes 装配一个含全部 10 个 fake 模块的内核。
// configure 为可选回调，用于给指定模块注入失败。
func newKernelWithFakes(rec *recorder, bus msgbus.MsgBus, configure func(name string, m *fakeModule)) *kernel.StandardKernel {
	k := kernel.NewKernel(clock.NewLiveClock(), bus, nil)
	for _, name := range kernel.BootOrder {
		m := &fakeModule{name: name, rec: rec}
		if configure != nil {
			configure(name, m)
		}
		if err := k.Register(m); err != nil {
			panic("Register(" + name + ") 失败: " + err.Error())
		}
	}
	return k
}

// stopsOf 从事件流里抽出 Stop 的模块名（按发生顺序）。
func stopsOf(events []string) []string {
	var out []string
	for _, e := range events {
		if rest, ok := strings.CutPrefix(e, "stop:"); ok {
			out = append(out, rest)
		}
	}
	return out
}

// indexOf 返回事件在流中的下标，找不到返回 -1。
func indexOf(events []string, want string) int {
	for i, e := range events {
		if e == want {
			return i
		}
	}
	return -1
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ─── 1. Boot 按 BootOrder 逐个 Init→Start ──────────────────────────

func TestBootFollowsBootOrder(t *testing.T) {
	rec := &recorder{}
	bus := &fakeBus{rec: rec}
	k := newKernelWithFakes(rec, bus, nil)

	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot 失败: %v", err)
	}

	want := make([]string, 0, len(kernel.BootOrder)*2+1)
	for _, name := range kernel.BootOrder {
		want = append(want, "init:"+name, "start:"+name)
	}
	want = append(want, "publish:"+msgbus.TopicKernelBoot)

	if got := rec.snapshot(); !eqStrings(got, want) {
		t.Errorf("Boot 调用序列不符 BootOrder 契约\n got: %v\nwant: %v", got, want)
	}
	// kernel.boot 必须发布（D4 起点）。
	if len(bus.topics) != 1 || bus.topics[0] != msgbus.TopicKernelBoot {
		t.Errorf("Boot 后发布的 topic = %v, want [%s]", bus.topics, msgbus.TopicKernelBoot)
	}
}

// ─── 2. Start 失败 → 已启动的逆序回滚 ─────────────────────────────

func TestBootRollsBackInReverseOnStartFailure(t *testing.T) {
	const failIdx = 5 // risk-engine
	failName := kernel.BootOrder[failIdx]

	rec := &recorder{}
	bus := &fakeBus{rec: rec}
	k := newKernelWithFakes(rec, bus, func(name string, m *fakeModule) {
		if name == failName {
			m.failStart = true
		}
	})

	err := k.Boot(context.Background())
	if err == nil {
		t.Fatal("Boot 应当失败（risk-engine Start 注入失败），但返回 nil")
	}
	if !errors.Is(err, kernel.ErrBootFailed) {
		t.Errorf("Boot error 未包装 ErrBootFailed: %v", err)
	}
	if !strings.Contains(err.Error(), failName) {
		t.Errorf("Boot error 未含失败模块名 %q: %v", failName, err)
	}

	events := rec.snapshot()

	// 回滚断言：已成功 Start 的 0..failIdx-1 必须按**逆序** Stop。
	wantStops := make([]string, 0, failIdx)
	for i := failIdx - 1; i >= 0; i-- {
		wantStops = append(wantStops, kernel.BootOrder[i])
	}
	if got := stopsOf(events); !eqStrings(got, wantStops) {
		t.Errorf("回滚 Stop 顺序不符逆序契约\n got: %v\nwant: %v", got, wantStops)
	}
	// 失败模块自己没有起来，不该被 Stop。
	if indexOf(events, "stop:"+failName) != -1 {
		t.Errorf("失败模块 %q 不应被 Stop（它从未 Start 成功）", failName)
	}
	// 回滚后不得发布 kernel.boot。
	if indexOf(events, "publish:"+msgbus.TopicKernelBoot) != -1 {
		t.Error("Boot 失败后不应发布 kernel.boot")
	}
}

// TestBootRollsBackOnInitFailure 同样验证 Init 阶段失败的回滚路径。
func TestBootRollsBackOnInitFailure(t *testing.T) {
	const failIdx = 3 // portfolio
	failName := kernel.BootOrder[failIdx]

	rec := &recorder{}
	k := newKernelWithFakes(rec, &fakeBus{rec: rec}, func(name string, m *fakeModule) {
		if name == failName {
			m.failInit = true
		}
	})

	err := k.Boot(context.Background())
	if err == nil || !errors.Is(err, kernel.ErrBootFailed) {
		t.Fatalf("Init 失败应使 Boot 失败并包装 ErrBootFailed，got %v", err)
	}

	wantStops := make([]string, 0, failIdx)
	for i := failIdx - 1; i >= 0; i-- {
		wantStops = append(wantStops, kernel.BootOrder[i])
	}
	if got := stopsOf(rec.snapshot()); !eqStrings(got, wantStops) {
		t.Errorf("Init 失败回滚顺序不符逆序契约\n got: %v\nwant: %v", got, wantStops)
	}
}

// ─── 3. Shutdown 逆序 ─────────────────────────────────────────────

func TestShutdownStopsInReverseBootOrder(t *testing.T) {
	rec := &recorder{}
	k := newKernelWithFakes(rec, &fakeBus{rec: rec}, nil)

	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot 失败: %v", err)
	}
	beforeShutdown := len(rec.snapshot())

	if err := k.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown 失败: %v", err)
	}

	// 只看 Shutdown 阶段产生的 Stop。
	after := rec.snapshot()[beforeShutdown:]
	wantStops := make([]string, 0, len(kernel.BootOrder))
	for i := len(kernel.BootOrder) - 1; i >= 0; i-- {
		wantStops = append(wantStops, kernel.BootOrder[i])
	}
	if got := stopsOf(after); !eqStrings(got, wantStops) {
		t.Errorf("Shutdown Stop 顺序不符逆序契约\n got: %v\nwant: %v", got, wantStops)
	}
}

// ─── 4. Shutdown 幂等 ─────────────────────────────────────────────

func TestShutdownIsIdempotent(t *testing.T) {
	rec := &recorder{}
	k := newKernelWithFakes(rec, &fakeBus{rec: rec}, nil)

	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot 失败: %v", err)
	}
	if err := k.Shutdown(context.Background()); err != nil {
		t.Fatalf("首次 Shutdown 失败: %v", err)
	}
	firstStops := len(stopsOf(rec.snapshot()))

	// 第二次（以及第三次）Shutdown 必须不报错、不重复 Stop。
	for i := 0; i < 2; i++ {
		if err := k.Shutdown(context.Background()); err != nil {
			t.Fatalf("第 %d 次重复 Shutdown 返回 error（应幂等返回 nil）: %v", i+2, err)
		}
	}
	if got := len(stopsOf(rec.snapshot())); got != firstStops {
		t.Errorf("重复 Shutdown 触发了额外 Stop：Stop 次数从 %d 变成 %d", firstStops, got)
	}
}

// ─── 5. 关停消息时序：先发 kernel.shutdown，再停 msgbus ─────────────

func TestShutdownPublishesBeforeStoppingMsgBus(t *testing.T) {
	rec := &recorder{}
	bus := &fakeBus{rec: rec}
	k := newKernelWithFakes(rec, bus, nil)

	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot 失败: %v", err)
	}
	if err := k.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown 失败: %v", err)
	}

	events := rec.snapshot()
	pub := indexOf(events, "publish:"+msgbus.TopicKernelShutdown)
	stopBus := indexOf(events, "stop:msgbus")

	if pub == -1 {
		t.Fatalf("Shutdown 未发布 %s（events=%v）", msgbus.TopicKernelShutdown, events)
	}
	if stopBus == -1 {
		t.Fatalf("Shutdown 未 Stop msgbus（events=%v）", events)
	}
	// 核心断言：发关停消息必须早于停 msgbus —— 否则消息落不了库
	// （BusTap 的「先记录」走 msgbus → eventstore），kernel.shutdown 在库里查不到。
	if pub > stopBus {
		t.Errorf("时序错误：%s 出现在 stop:msgbus 之后（pub=%d, stopBus=%d）；"+
			"关停消息必须在分发链关闭前发出并落库", msgbus.TopicKernelShutdown, pub, stopBus)
	}
	// 关停消息也必须是逆序停之前的第一个动作。
	if first := stopsOf(events); len(first) > 0 {
		if pub > indexOf(events, "stop:"+first[0]) {
			t.Errorf("关停消息应早于任何模块 Stop；pub=%d", pub)
		}
	}
}

// ─── 6. Module 检索 ───────────────────────────────────────────────

func TestModuleNotFoundReturnsError(t *testing.T) {
	k := kernel.NewKernel(clock.NewLiveClock(), &fakeBus{rec: &recorder{}}, nil)
	if _, err := k.Module("does-not-exist"); err == nil {
		t.Fatal("Module(未注册) 应返回 error，实际 nil")
	} else if !errors.Is(err, kernel.ErrModuleNotFound) {
		t.Errorf("Module(未注册) error 未包装 ErrModuleNotFound: %v", err)
	}
}

// ─── 7. NoopModule 可注册、可检索 ─────────────────────────────────

func TestNoopModuleRegistrableAndRetrievable(t *testing.T) {
	k := kernel.NewKernel(clock.NewLiveClock(), &fakeBus{rec: &recorder{}}, nil)

	nm := kernel.NewNoopModule("data-engine")
	if err := k.Register(nm); err != nil {
		t.Fatalf("Register(NoopModule) 失败: %v", err)
	}
	got, err := k.Module("data-engine")
	if err != nil {
		t.Fatalf("Module(NoopModule name) 失败: %v", err)
	}
	if got != nm {
		t.Errorf("Module 返回的不是注册的那个 NoopModule: got=%v want=%v", got, nm)
	}
	// NoopModule 的生命周期全 no-op。
	ctx := context.Background()
	if err := nm.Init(ctx); err != nil {
		t.Errorf("NoopModule.Init = %v, want nil", err)
	}
	if err := nm.Start(ctx); err != nil {
		t.Errorf("NoopModule.Start = %v, want nil", err)
	}
	if err := nm.Stop(ctx); err != nil {
		t.Errorf("NoopModule.Stop = %v, want nil", err)
	}
	if nm.Name() != "data-engine" {
		t.Errorf("NoopModule.Name() = %q, want %q", nm.Name(), "data-engine")
	}
}

// ─── 8. Register 重名 / nil / 空名 ────────────────────────────────

func TestRegisterRejectsDuplicatesAndInvalids(t *testing.T) {
	rec := &recorder{}
	k := kernel.NewKernel(clock.NewLiveClock(), &fakeBus{rec: rec}, nil)

	if err := k.Register(&fakeModule{name: "dup", rec: rec}); err != nil {
		t.Fatalf("首次 Register 失败: %v", err)
	}
	if err := k.Register(&fakeModule{name: "dup", rec: rec}); !errors.Is(err, kernel.ErrModuleAlreadyRegistered) {
		t.Errorf("重名 Register error = %v, want ErrModuleAlreadyRegistered", err)
	}
	if err := k.Register(nil); !errors.Is(err, kernel.ErrNilModule) {
		t.Errorf("Register(nil) error = %v, want ErrNilModule", err)
	}
	if err := k.Register(kernel.NewNoopModule("")); !errors.Is(err, kernel.ErrModuleNameEmpty) {
		t.Errorf("Register(空名) error = %v, want ErrModuleNameEmpty", err)
	}
}

// ─── 9. 装配不完整（BootOrder 缺项）→ Boot 失败 ────────────────────

func TestBootFailsWhenBootOrderIncomplete(t *testing.T) {
	rec := &recorder{}
	k := kernel.NewKernel(clock.NewLiveClock(), &fakeBus{rec: rec}, nil)
	// 只注册前 9 个，故意漏掉 msgbus。
	for _, name := range kernel.BootOrder[:len(kernel.BootOrder)-1] {
		if err := k.Register(&fakeModule{name: name, rec: rec}); err != nil {
			t.Fatalf("Register(%s) 失败: %v", name, err)
		}
	}
	err := k.Boot(context.Background())
	if !errors.Is(err, kernel.ErrMissingModule) {
		t.Fatalf("BootOrder 缺项时 Boot error = %v, want ErrMissingModule", err)
	}
	// 回滚：已启动的 9 个都要被逆序 Stop。
	if got := len(stopsOf(rec.snapshot())); got != len(kernel.BootOrder)-1 {
		t.Errorf("缺项回滚 Stop 次数 = %d, want %d", got, len(kernel.BootOrder)-1)
	}
}

// ─── 10. 适配器 Name() 与 BootOrder 一致 ───────────────────────────

func TestAdapterNamesMatchBootOrder(t *testing.T) {
	clkMod := kernel.NewClockModule(clock.NewLiveClock())
	busMod := kernel.NewMsgBusModule(&fakeBus{rec: &recorder{}})
	if clkMod.Name() != "clock" {
		t.Errorf("ClockModule.Name() = %q, want \"clock\"（须与 BootOrder 一致）", clkMod.Name())
	}
	if busMod.Name() != "msgbus" {
		t.Errorf("MsgBusModule.Name() = %q, want \"msgbus\"（须与 BootOrder 一致）", busMod.Name())
	}
	// EventStoreModule 需要真 PGEventStore（EnsureSchema 是 Init 的动作），
	// 其 Name 一致性由真库集成测试覆盖。
}
