// Package kernel 是内核装配层：装配 11 个模块、统一管理生命周期
// （Boot 顺序 / Shutdown 逆序）、提供模块检索与核心部件访问。
//
// 职责边界（蓝图 §5 kernel 行）：纯内存装配，无 DB 归属；
// 启动时单向装配，运行时不收消息（kernel 不订阅任何 topic——
// 它发布 kernel.boot / kernel.shutdown，但不是订阅者）。
//
// K0 切片 1：本文件只冻结 Module/Kernel 接口与 BootOrder 顺序
// 契约。stub 方法体固定 panic("contract stub: not implemented")，
// 不含任何实现逻辑；K1 实现直接替换 stub。
package kernel

import (
	"context"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/eventstore"
	"github.com/ruoxizhnya/quant-trading/pkg/msgbus"
)

// Module 是模块生命周期的统一抽象（ADR-027 指出的缺口：此前各组件
// 生命周期散落在 setup.go 硬编码，无统一契约）。
//
// 内核的 11 个模块全部实现本接口；Kernel.Boot/Shutdown 按 BootOrder
// 逐个调用。Init 与 Start 分离的理由：Init 只做本地构造与配置校验
// （可失败且无副作用），Start 才连接外部资源（DB/feed/broker）。
type Module interface {
	// Name 返回模块名，必须与 BootOrder 中的名字一致
	//（由 TestBootOrderContract 与 K1 的装配测试共同冻结）。
	Name() string
	// Init 初始化：只做本地构造与配置校验，不碰外部资源。
	Init(ctx context.Context) error
	// Start 启动：连接外部资源，此后可开始收发消息。
	Start(ctx context.Context) error
	// Stop 关停：释放资源；必须幂等（重复调用不报错）。
	Stop(ctx context.Context) error
}

// Kernel 是装配后的内核，取代 setup.go 的手工编排（蓝图 §2.2 差距③）。
type Kernel interface {
	// Boot 按 BootOrder 顺序启动全部模块（顺序契约见 BootOrder 与
	// 蓝图 §4.2）。任一模块启动失败即整体失败，已启动的模块逆序
	// 回滚关停（不留半启动状态）。
	Boot(ctx context.Context) error
	// Shutdown 按 BootOrder 逆序关停全部模块：msgbus 最先停
	//（先停分发），eventstore 最后停（后停记录），保证关停期消息
	// 先落库的语义不破坏。
	Shutdown(ctx context.Context) error
	// Module 按名检索已装配的模块；未注册的名字返回 error。
	Module(name string) (Module, error)
	// Clock 返回内核时钟（替换点①：回测 VirtualClock / 实盘 LiveClock）。
	Clock() clock.Clock
	// Bus 返回消息总线。
	Bus() msgbus.MsgBus
	// Store 返回事件存储。
	Store() eventstore.EventStore
}

// BootOrder 是 Boot 的模块启动顺序契约；Shutdown 严格逆序。
// 本切片为「宪法」级冻结：改顺序 = 改契约，须走变更评审。
//
// 冻结依据：蓝图 §4.2 内核生命周期 + §5 模块矩阵。两个不变量：
//   - eventstore 必须最先起：此后所有消息才有审计落库（否则启动期
//     消息丢失）；
//   - msgbus 必须最后开：此前消息只记录（eventstore.Append）不派发，
//     防止策略在状态未恢复完就收到 bar。
//
// 全序（10 项，编号即 Boot 顺序）：
//
//  1. eventstore       —— 先开记录
//  2. clock            —— 决定时间观，后续模块初始化要用
//  3. data-engine      —— 建快照 / 连 feed
//  4. portfolio        —— 恢复持仓
//  5. risk-engine      —— 风控就位后 exec 才敢收单
//  6. exec-engine      —— 连 broker
//  7. strategy-runtime —— 加载策略、恢复 L2/L3 状态
//  8. indicators       —— L2 有状态算子就位
//  9. exec-algo        —— 执行算法挂点
//  10. msgbus           —— 最后开分发
//
// 注意（K0 裁决）：蓝图 §5 模块矩阵共 11 模块，其中 kernel 本身是
// 装配者而非被装配模块（内核不能 Boot 自己），不进入本序列，
// 故本切片共 10 项。可测试化断言见 TestBootOrderContract。
var BootOrder = []string{
	"eventstore",
	"clock",
	"data-engine",
	"portfolio",
	"risk-engine",
	"exec-engine",
	"strategy-runtime",
	"indicators",
	"exec-algo",
	"msgbus",
}

// ─── K1 实现落点 ─────────────────────────────────────────────────
//
// K0 切片 1 在此处冻结的 StandardKernel / BaseModule 的 panic stub 已由
// K1 切片 2 替换为真实实现，**本文件从此只保留接口契约与守卫**，实现
// 全部搬到 kernel.go（StandardKernel / NewKernel / BaseModule / NoopModule）
// 与 adapters.go（clock/msgbus/eventstore 的薄包装 Module）。
//
// 这样切分是为了让「契约文件」与「实现文件」的改动面互不污染：改实现不必
// 动契约文件，契约评审只看 interfaces.go / topics.go 与下面的守卫。
//
// 下面的编译期守卫原样保留（含方法表达式守卫——删接口方法即编译失败）。

// ─── 编译期合规检查 + 方法存在性守卫 ────────────────────────────────
//
// 方法表达式守卫：从 Module/Kernel 接口删除任一方法，本包
// go build 直接编译失败（防「删方法后测试仍绿」）。
var (
	_ Kernel = (*StandardKernel)(nil)
	_ Module = (*BaseModule)(nil)

	_ func(Module) string                 = Module.Name
	_ func(Module, context.Context) error = Module.Init
	_ func(Module, context.Context) error = Module.Start
	_ func(Module, context.Context) error = Module.Stop

	_ func(Kernel, context.Context) error  = Kernel.Boot
	_ func(Kernel, context.Context) error  = Kernel.Shutdown
	_ func(Kernel, string) (Module, error) = Kernel.Module
	_ func(Kernel) clock.Clock             = Kernel.Clock
	_ func(Kernel) msgbus.MsgBus           = Kernel.Bus
	_ func(Kernel) eventstore.EventStore   = Kernel.Store
)
