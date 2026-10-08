// kernel.go —— StandardKernel 的真实实现（K1 切片 2）。
//
// K0 切片 1 在 interfaces.go 冻结了 Kernel / Module 接口与 BootOrder 顺序
// 契约，方法体固定 panic("contract stub: not implemented")。本文件把它替换为
// 可运行的内核：Boot 按 BootOrder 逐个 Init→Start（失败逆序回滚）、Shutdown
// 逆序 Stop（幂等）、Module 检索、Clock/Bus/Store 访问。**接口与顺序契约
// 半个字未改**，守卫仍在 interfaces.go。
//
// 内核的职责边界（interfaces.go 包注释）：纯内存装配，无 DB 归属；启动时
// 单向装配，运行时不订阅任何 topic——它只 Publish kernel.boot /
// kernel.shutdown（发布者而非订阅者）。
package kernel

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/eventstore"
	"github.com/ruoxizhnya/quant-trading/pkg/msgbus"
)

// 哨兵 error：调用方用 errors.Is 判定，不要比字符串。
var (
	// ErrNilModule：Register(nil)。空模块无法启动也无法归因。
	ErrNilModule = errors.New("kernel: 模块为 nil")

	// ErrModuleNameEmpty：模块 Name() 返回空串。空名无法进入 BootOrder
	// 匹配，也无法被 Module(name) 检索到，等价于「装配了一个不可达模块」。
	ErrModuleNameEmpty = errors.New("kernel: 模块名为空（必须与 BootOrder 中的名字一致）")

	// ErrModuleAlreadyRegistered：同名模块重复 Register。重名会让检索
	// 出现两种真相（后注册的覆盖前者还是报错？），这里 fail-fast。
	ErrModuleAlreadyRegistered = errors.New("kernel: 模块已注册")

	// ErrModuleNotFound：Module(name) 的名字未注册。
	ErrModuleNotFound = errors.New("kernel: 模块未注册")

	// ErrMissingModule：BootOrder 里的名字没有对应模块。BootOrder 是冻结
	// 顺序契约，缺任何一个都不是「可降级启动」，而是装配不完整。
	ErrMissingModule = errors.New("kernel: BootOrder 中的模块未注册")

	// ErrBootFailed：某个模块 Init 或 Start 失败；Boot 已逆序回滚。
	ErrBootFailed = errors.New("kernel: 模块启动失败")
)

// StandardKernel 是装配后的内核实体（取代 setup.go 的手工编排）。
//
// 并发模型（写死在注释里，免得日后各猜一套）：生命周期的三个阶段
// （Register 装配期 / Boot / Shutdown）之间不并发——装配完再 Boot，Boot 完
// 再 Shutdown。mu 保护的是「装配与检索可能来自不同 goroutine」这一条：
// Boot 期间别的 goroutine 调 Module(name) 不该读到写一半的 map。Boot/Shutdown
// 全程持锁是安全的，因为模块拿不到内核引用（依赖全部构造期注入），不会在
// Init/Start/Stop 里回调内核造成自锁。
type StandardKernel struct {
	// clk/bus/store 是核心部件引用，由装配注入（Clock/Bus/Store 访问器
	// 原样返回它们）。总线是发布 kernel.boot / kernel.shutdown 的通道，
	// 也是「先记录后分发」把生命周期事件落 audit.message_log 的路径。
	clk   clock.Clock
	bus   msgbus.MsgBus
	store eventstore.EventStore

	// modules 是装配进来的模块，键为 Module.Name()。
	modules map[string]Module

	mu sync.Mutex
	// booted 记录**已成功 Start** 的模块名，按 Start 顺序。Boot 失败时按它
	// 逆序 Stop 回滚；Shutdown 时按它逆序 Stop。只装成功的，不装失败的
	// ——失败的那个没起来，回滚它没有意义，也会给失败模块一个「被 Stop」
	// 的假象。
	booted []string
	// shutdown 标记本内核是否已执行过 Shutdown（幂等闸门）。
	shutdown bool
}

// NewKernel 注入三个核心部件构造内核。
//
// 为什么是构造期注入而不是 Boot 参数：这三个部件的**选择**本身就是装配
// 决策（回测传 VirtualClock、实盘传 LiveClock；tap 指向 eventstore），
// 让它们在构造期显式出现，装配代码里「这个内核跑在哪种时间观上」一眼可读。
// 这与 pkg/msgbus 的 NewSyncBus(tap, clk) 强制注入是同一条思路。
//
// 注意（切片 2 裁决）：服务级内核用 **LiveClock**（墙钟）；VirtualClock 是
// **per-回测 run** 的时间观，不属于服务级内核，不能在此注入。
func NewKernel(clk clock.Clock, bus msgbus.MsgBus, store eventstore.EventStore) *StandardKernel {
	return &StandardKernel{
		clk:     clk,
		bus:     bus,
		store:   store,
		modules: make(map[string]Module),
	}
}

// Register 装配一个模块。重名（或空名、nil）返回 error，不改动已装配集合。
func (k *StandardKernel) Register(m Module) error {
	if m == nil {
		return ErrNilModule
	}
	name := m.Name()
	if name == "" {
		return ErrModuleNameEmpty
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	if _, exists := k.modules[name]; exists {
		return fmt.Errorf("%w: %q", ErrModuleAlreadyRegistered, name)
	}
	k.modules[name] = m
	return nil
}

// Boot 按 BootOrder 顺序启动全部模块。
//
// 序列（每个模块两步，Init 只做本地构造/校验，Start 才连外部资源）：
//
//	eventstore → clock → data-engine → portfolio → risk-engine →
//	exec-engine → strategy-runtime → indicators → exec-algo → msgbus
//
// fail-fast + 回滚：任一步失败，**已成功 Start 的模块按逆序 Stop 回滚**，
// 不留半启动状态（interfaces.go 的 Boot 契约）。为什么必须回滚而不是
// 「报错让调用方收拾」：半启动的内核里，部分模块已连着 DB/broker，调用方
// 若只是打日志继续跑，那些连接就成了没有归属的野资源；回滚把「要么全起、
// 要么全不起」这条不变量交还给调用方。
//
// 装配不完整（BootOrder 里的名字没注册）同样视为 Boot 失败——BootOrder 是
// 冻结顺序契约，缺项不是可降级场景。
//
// 全部启动成功后发布 kernel.boot（先记录后分发 → 落 audit.message_log）。
// 发布失败视为 Boot 失败并回滚：启动成功却无法留痕，正是 D4 审计要防的
// 「看不见的缺口」，不能吞掉。
func (k *StandardKernel) Boot(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	for _, name := range BootOrder {
		m, ok := k.modules[name]
		if !ok {
			k.rollbackLocked(ctx)
			return fmt.Errorf("%w: %q（BootOrder 第 %d 位）", ErrMissingModule, name, len(k.booted)+1)
		}
		if err := m.Init(ctx); err != nil {
			k.rollbackLocked(ctx)
			return fmt.Errorf("%w: 模块 %q Init 失败: %w", ErrBootFailed, name, err)
		}
		if err := m.Start(ctx); err != nil {
			k.rollbackLocked(ctx)
			return fmt.Errorf("%w: 模块 %q Start 失败: %w", ErrBootFailed, name, err)
		}
		k.booted = append(k.booted, name)
	}

	if err := k.publishLocked(ctx, msgbus.TopicKernelBoot); err != nil {
		k.rollbackLocked(ctx)
		return fmt.Errorf("%w: 发布 %s 失败: %w", ErrBootFailed, msgbus.TopicKernelBoot, err)
	}
	return nil
}

// Shutdown 按 BootOrder 逆序关停全部模块。**幂等**：重复调用不再 Stop、
// 不再发消息，直接返回 nil。
//
// 逆序（msgbus 最先停 —— 先停分发；eventstore 最后停 —— 后停记录）：
//
//	msgbus → exec-algo → indicators → strategy-runtime → exec-engine →
//	risk-engine → portfolio → data-engine → clock → eventstore
//
// ─── 时序裁决（本切片定死，写进注释 + 测试）─────────────────────────
// **先发 kernel.shutdown，再逐个停模块**。理由：这条消息本身要落
// audit.message_log（BusTap 的「先记录」走 msgbus → eventstore.Append），
// 而逆序的第一步就是停 msgbus——若先停 msgbus 再发消息，消息要么发不出去、
// 要么没有审计行，**「内核关停」这件事在库里查不到**，正是 D4 起点要保证的
// 可观测性被自己破坏。
//
// 所以顺序是：① Publish(kernel.shutdown)（此时 msgbus 与 eventstore 都还活着）
// → ② 逆序 Stop（msgbus 先停，eventstore 最后停）。这也是「先停分发后停记录」
// 语义在「发关停消息」这一步上的正确投影：消息在分发链还完整时发出并落库，
// 分发链随即关闭。
//
// 停模块过程中的 error 聚合上报（errors.Join），不因某个模块 Stop 失败而
// 跳过其余模块——半关停比全关停更难查。发布 kernel.shutdown 的 error 也
// 一并聚合（发布失败不阻断关停：关停必须完成，审计缺口上报给调用方）。
func (k *StandardKernel) Shutdown(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.shutdown {
		return nil // 幂等：第二次起不再停、不再发
	}
	k.shutdown = true

	// ① 关闭分发前先发关停消息（见上面「时序裁决」）。此时 msgbus/eventstore
	//    仍在运行，消息能落库。
	publishErr := k.publishLocked(ctx, msgbus.TopicKernelShutdown)

	// ② 逆序 Stop。booted 是 Start 顺序，反向遍历即逆序。
	var errs []error
	for i := len(k.booted) - 1; i >= 0; i-- {
		name := k.booted[i]
		if err := k.modules[name].Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("模块 %q Stop 失败: %w", name, err))
		}
	}
	k.booted = nil

	if publishErr != nil {
		errs = append(errs, publishErr)
	}
	return errors.Join(errs...)
}

// Module 按名检索已装配的模块；未注册返回包装了 ErrModuleNotFound 的 error。
func (k *StandardKernel) Module(name string) (Module, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	m, ok := k.modules[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrModuleNotFound, name)
	}
	return m, nil
}

// Clock 返回内核时钟（替换点①：回测 VirtualClock / 实盘 LiveClock）。
func (k *StandardKernel) Clock() clock.Clock { return k.clk }

// Bus 返回消息总线。
func (k *StandardKernel) Bus() msgbus.MsgBus { return k.bus }

// Store 返回事件存储。
func (k *StandardKernel) Store() eventstore.EventStore { return k.store }

// ─── 内部辅助 ──────────────────────────────────────────────────────

// rollbackLocked 逆序 Stop 已成功 Start 的模块，并清空 booted。
// 调用方必须持有 k.mu（Boot 持锁路径调用）。
//
// 回滚自身的 error 只能丢弃：Boot 已经把「原始失败 error」作为返回值，回滚
// 是尽力而为的清理，把它的错误 Join 进去会掩盖调用方真正要看的根因。
// 这条取舍写在这里，免得日后有人「顺手」把回滚 error 也 Join 上。
func (k *StandardKernel) rollbackLocked(ctx context.Context) {
	for i := len(k.booted) - 1; i >= 0; i-- {
		_ = k.modules[k.booted[i]].Stop(ctx)
	}
	k.booted = nil
}

// publishLocked 发布一条内核生命周期消息（kernel.boot / kernel.shutdown）。
// 调用方必须持有 k.mu。bus 为 nil 时返回 ErrNoBus——内核没有总线就无法
// 留痕，静默跳过等于让生命周期事件静默消失。
//
// payload 形态**尚未冻结**（K0 只冻结了 topic 名与 Envelope）：本切片用
// 一个带模块名列表的匿名结构，够 audit 回放时看出「当时装了哪些模块」即可；
// 将来若下游要按固定字段消费，走变更评审冻结 payload 结构。
func (k *StandardKernel) publishLocked(ctx context.Context, topic string) error {
	if k.bus == nil {
		return ErrNoBus
	}
	payload := struct {
		Modules []string `json:"modules"`
	}{Modules: append([]string(nil), k.booted...)}
	return k.bus.Publish(ctx, topic, payload)
}

// ErrNoBus：内核未装配 msgbus（NewKernel 传了 nil）。
var ErrNoBus = errors.New("kernel: 未装配 msgbus，无法发布生命周期消息（kernel.boot / kernel.shutdown）")

// ─── BaseModule / NoopModule ───────────────────────────────────────

// BaseModule 是模块实现的安全缺省基类：持有模块名，Init/Start/Stop 全部
// no-op。真实模块内嵌它以省掉不关心的生命周期方法；K0 冻结的
// `_ Module = (*BaseModule)(nil)` 守卫依赖本类型仍是 Module。
//
// 注意：本类型只提供「什么都不做」的默认，**不承担任何业务语义**——
// 需要接外部资源的模块必须覆盖 Init/Start。
type BaseModule struct {
	name string
}

// NewBaseModule 构造一个带名的 no-op 基类。
func NewBaseModule(name string) *BaseModule { return &BaseModule{name: name} }

// Name 返回模块名。
func (m *BaseModule) Name() string { return m.name }

// Init no-op（基类不做本地构造/校验）。
func (m *BaseModule) Init(context.Context) error { return nil }

// Start no-op（基类不连外部资源）。
func (m *BaseModule) Start(context.Context) error { return nil }

// Stop no-op（基类无可释放资源）。
func (m *BaseModule) Stop(context.Context) error { return nil }

// NoopModule 是**未实现模块的占位**：Name 可配置，生命周期全 no-op。
//
// ─── 这是渐进装配的脚手架，不是设计（K1 切片 2 注释）────────────────
// 7 个尚未实现的模块（data-engine / portfolio / risk-engine / exec-engine /
// strategy-runtime / indicators / exec-algo）当前用 NoopModule 顶位，好让
// 装配序列「凑齐 BootOrder 的 10 项」并把内核骨架先跑通。它们**不做任何
// 实际工作**——Boot 通过不代表这 7 个模块可用。
//
// K2+ 每个模块实现到位后，**逐个**把 NoopModule 换成真模块（改的是装配处
// 那一行 Register，不动内核）：届时 NoopModule 的实例数从 7 递减到 0。
// 若某个模块在真实现之前就成了阻塞项，它该在自己的包里有真实实现，而不是
// 让这个 no-op 承担语义——no-op 的职责边界就是「不做事」。
type NoopModule struct {
	*BaseModule
}

// NewNoopModule 构造一个名为 name 的占位模块。
func NewNoopModule(name string) *NoopModule {
	return &NoopModule{BaseModule: NewBaseModule(name)}
}

// ─── 编译期合规检查 ────────────────────────────────────────────────
// BaseModule 的守卫在 interfaces.go（K0 原样保留）；这里补 NoopModule。
var _ Module = (*NoopModule)(nil)
