// adapters.go —— 把已实现的核心部件薄包装成 kernel.Module（K1 切片 2）。
//
// ─── 为什么放这里，而不是 cmd/analysis ─────────────────────────────
// 裁决：包装放 pkg/kernel，不放 composition root。
//
//  1. **导入方向本来就允许**：pkg/kernel 已经 import clock / msgbus /
//     eventstore（Kernel.Clock/Bus/Store 的返回类型就是它们）。反向依赖
//     不存在（这三个包都不 import kernel，msgbus←clock、eventstore←msgbus
//     两条边都与 kernel 无关），所以在这里写包装**不制造导入环**。
//  2. **名字↔部件的映射是内核的职责**：模块名（"clock"/"msgbus"/"eventstore"）
//     必须与 BootOrder 完全一致，这个「一致性」是内核装配契约的一部分，
//     放在 kernel 包内比散在 cmd 层更靠近它的守卫。
//  3. **保持 cmd 层薄**：main.go 是 composition root，只该写「拿什么装配」，
//     不该写「怎么把 A 包成 B」的样板。包装是一次性的结构适配，属 kernel。
//
// 这些类型是**结构适配器**，不是新契约：它们不冻结任何新方法，只把既有
// 部件的构造/建表动作映射到 Init/Start/Stop。
//
// ─── 为什么 clock/msgbus 的 Init 是 no-op，eventstore 的不是 ────────
// Init 的契约是「本地构造与配置校验，不碰外部资源」。clock 与 msgbus 是
// 纯内存部件，构造即就绪；而 eventstore 的 Init 必须**先建表**
// （EnsureSchema）——蓝图 §4.2：EventStore 必须第一个起，否则启动期消息
// 无审计落点。这正是「eventstore 在 BootOrder 首位」这条不变量的落点。
package kernel

import (
	"context"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/eventstore"
	"github.com/ruoxizhnya/quant-trading/pkg/msgbus"
)

// ClockModule 把 clock.Clock 包装成模块。Name 恒为 "clock"（BootOrder 第 2 位）。
//
// Clock 是替换点①，装配期就已选定实现（回测 VirtualClock / 实盘 LiveClock），
// 本模块只负责把它挂进生命周期，Init/Start/Stop 均无额外动作。
type ClockModule struct {
	clk clock.Clock
}

// NewClockModule 包装一个时钟实现。
func NewClockModule(clk clock.Clock) *ClockModule { return &ClockModule{clk: clk} }

// Name 恒为 "clock"。
func (m *ClockModule) Name() string { return "clock" }

// Init no-op：时钟构造即就绪，无本地校验。
func (m *ClockModule) Init(context.Context) error { return nil }

// Start no-op：时钟无外部资源。
func (m *ClockModule) Start(context.Context) error { return nil }

// Stop no-op：时钟无可释放资源。
func (m *ClockModule) Stop(context.Context) error { return nil }

// EventStoreModule 把 *eventstore.PGEventStore 包装成模块。Name 恒为
// "eventstore"（BootOrder 第 1 位）。
//
// Init 建表（EnsureSchema）而不是 no-op：audit.message_log 必须在内核其余
// 部分启动前就位，否则启动期消息静默丢失。Start/Stop 无额外动作——真正的
// 「先记录」在每次 msgbus.Publish 里发生，不是一次性动作。
type EventStoreModule struct {
	store *eventstore.PGEventStore
}

// NewEventStoreModule 包装一个 PGEventStore。
//
// 收具体类型 *PGEventStore 而非 eventstore.EventStore 接口，是因为 Init 要
// 调的 EnsureSchema 不在 EventStore 接口上（接口只有 Append/Replay/Verify）。
// 这不是契约缺陷：建表是**安装**动作，不该进运行时读写接口。
func NewEventStoreModule(store *eventstore.PGEventStore) *EventStoreModule {
	return &EventStoreModule{store: store}
}

// Name 恒为 "eventstore"。
func (m *EventStoreModule) Name() string { return "eventstore" }

// Init 幂等建 audit schema 与 message_log 表（先开记录）。
func (m *EventStoreModule) Init(ctx context.Context) error { return m.store.EnsureSchema(ctx) }

// Start no-op：eventstore 无独立「启动」动作。
func (m *EventStoreModule) Start(context.Context) error { return nil }

// Stop no-op：连接池由 pkg/storage 持有并统一 Close，本模块不越权关它。
func (m *EventStoreModule) Stop(context.Context) error { return nil }

// MsgBusModule 把 msgbus.MsgBus 包装成模块。Name 恒为 "msgbus"
// （BootOrder 末位——最后开分发）。
//
// Init/Start/Stop 均 no-op：本切片的 SyncBus 构造即就绪，无独立启动/停止
// 动作。Stop 之所以不「关总线」，是因为「关分发」在本内核里的实现时机是
// **Shutdown 的逆序遍历**（msgbus 在 booted 末位 → 逆序第一个被 Stop），
// 而 SyncBus 没有需要释放的资源；语义上的「先停分发」由 Shutdown 的发布
// 时序（先发 kernel.shutdown 再逆序停）保证，见 kernel.go 的时序裁决。
type MsgBusModule struct {
	bus msgbus.MsgBus
}

// NewMsgBusModule 包装一个消息总线。
func NewMsgBusModule(bus msgbus.MsgBus) *MsgBusModule { return &MsgBusModule{bus: bus} }

// Name 恒为 "msgbus"。
func (m *MsgBusModule) Name() string { return "msgbus" }

// Init no-op：总线构造即就绪。
func (m *MsgBusModule) Init(context.Context) error { return nil }

// Start no-op：本切片 SyncBus 无独立启动动作。
func (m *MsgBusModule) Start(context.Context) error { return nil }

// Stop no-op：见类型注释。
func (m *MsgBusModule) Stop(context.Context) error { return nil }

// ─── 编译期合规检查 ────────────────────────────────────────────────
var (
	_ Module = (*ClockModule)(nil)
	_ Module = (*EventStoreModule)(nil)
	_ Module = (*MsgBusModule)(nil)
)
