package main

// kernel_shadow.go —— 内核影子启动（K1 切片 2，渐进接管第一步）。
//
// ─── 这个文件在做什么、不做什么 ────────────────────────────────────
// 做：在**现有装配流程之外**新增一个「影子内核」——用真实部件
// （LiveClock + PGEventStore + SyncBus）装配 10 个模块并 Boot/Shutdown，
// 让 kernel.boot / kernel.shutdown 经 msgbus 落 audit.message_log（D4 起点）。
//
// 不做：接管或替换 main.go/setup.go 里现有的任何一行装配。影子内核当前
// **不是承重组件**——它跑起来只证明「内核骨架可用 + 生命周期事件可审计」，
// 上层业务仍由原装配驱动。K2+ 才逐个把原装配搬进内核 Boot 序列。
//
// ─── 为什么放新文件、而不是塞进 setup.go 或 main.go 内联 ───────────
// 约束是「setup.go 全部 20 个函数一行不改」。把构造逻辑放进独立文件，
// 既满足该约束，又让 main.go 只留下两处「调用」（见 main.go 的
// // ─── 内核影子启动（K1 切片 2，渐进接管第一步）─── 标记），保持
// main() 作为「薄编排层」的定位（见 main.go 顶部注释）。
//
// 三个核心部件的包装（ClockModule/MsgBusModule/EventStoreModule）放在
// pkg/kernel/adapters.go，理由见该文件顶部（导入方向、名字↔部件映射的
// 归属、保持 cmd 层薄）。本文件只负责「拿什么装配」这一层的裁决。

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/eventstore"
	"github.com/ruoxizhnya/quant-trading/pkg/kernel"
	"github.com/ruoxizhnya/quant-trading/pkg/msgbus"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// shadowPublisher 是影子内核写入 audit.message_log 的 publisher 标识。
// 契约里 msgbus.Message 没有 Publisher()，发布者身份由 store 实例持有；
// 一个进程一条内核管线 = 一个 publisher，故取 "kernel"（而非 "kernel-shadow"
// ——那是内部阶段名，不该渗进审计数据的发布者列，接管后这条记录仍应可读为
// 「kernel 在何时起停」）。
const shadowPublisher = "kernel"

// shadowPlaceholderModules 是切片 2 尚未实现的 7 个模块，用 NoopModule 顶位。
// K2+ 每实现一个就从这里移除并把 Register 换成真模块（见 NoopModule 注释）。
var shadowPlaceholderModules = []string{
	"data-engine",
	"portfolio",
	"risk-engine",
	"exec-engine",
	"strategy-runtime",
	"indicators",
	"exec-algo",
}

// startShadowKernel 构造并 Boot 影子内核。
//
// ─── 依赖从哪来 ────────────────────────────────────────────────────
//   - clock：NewLiveClock()。**服务级内核用墙钟**——VirtualClock 是
//     per-回测 run 的时间观（由数据迭代器推进），不属于服务级内核；内核
//     装配时用哪个时钟，就是「这个内核跑在回测还是实盘」的声明（见
//     pkg/kernel.NewKernel 注释）。
//   - eventstore：NewPGEventStore(store.DB(), "kernel", "")。复用现有
//     *storage.PostgresStore 的 pool（store.DB() 返回 *pgxpool.Pool，
//     见 pkg/storage/postgres.go:65），不另起连接池——一个进程一个池。
//     runID 传空串（落 NULL）：服务级内核的起停不属于任何一次 run。
//   - msgbus：NewLiveBus(es)。tap = 真 eventstore，「先记录后分发」因此在
//     影子内核上真实生效（kernel.boot 先落库再分发）。
//
// ─── 失败策略（已裁决）─────────────────────────────────────────────
// 影子期失败**不阻断**服务启动：构造/装配/Boot 任一环节出错只 log warning
// 并返回 nil。理由：影子内核当前不是承重组件，让它把整个服务拖下水是本末
// 倒置。**接管完成后改为 fail-fast**（届时内核成为承重组件，起不来就不该
// 对外提供服务）——本函数每处 return nil 都写了这条提示。
//
// 返回值：成功返回 *kernel.StandardKernel（供关停）；任何失败返回 nil
// （Boot 失败时内核内部已逆序回滚，无需外部再关）。
func startShadowKernel(store *storage.PostgresStore, logger zerolog.Logger) *kernel.StandardKernel {
	// 影子期不阻断：store 为 nil 说明前面的装配已经 Fail/Fatal 过，这里跳过。
	if store == nil {
		logger.Warn().Msg("shadow kernel: store 为 nil，跳过影子启动（影子期非承重）")
		return nil
	}

	clk := clock.NewLiveClock()
	es, err := eventstore.NewPGEventStore(store.DB(), shadowPublisher, "")
	if err != nil {
		logger.Warn().Err(err).Msg("shadow kernel: 构造 eventstore 失败，跳过影子启动（影子期非承重；接管后改 fail-fast）")
		return nil
	}
	bus := msgbus.NewLiveBus(es)
	k := kernel.NewKernel(clk, bus, es)

	// 装配 10 个模块：eventstore/clock/msgbus 用真模块（薄包装），
	// 其余 7 个用 NoopModule 顶位。顺序无关——BootOrder 决定启动顺序。
	register := func(m kernel.Module) bool {
		if err := k.Register(m); err != nil {
			logger.Warn().Err(err).Str("module", m.Name()).
				Msg("shadow kernel: Register 失败，跳过影子启动（影子期非承重；接管后改 fail-fast）")
			return false
		}
		return true
	}
	if !register(kernel.NewEventStoreModule(es)) ||
		!register(kernel.NewClockModule(clk)) ||
		!register(kernel.NewMsgBusModule(bus)) {
		return nil
	}
	for _, name := range shadowPlaceholderModules {
		if !register(kernel.NewNoopModule(name)) {
			return nil
		}
	}

	// Boot：按 BootOrder 逐个 Init→Start（eventstore 第一步会幂等建表），
	// 成功后发 kernel.boot（落 audit.message_log）。失败内部已逆序回滚。
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := k.Boot(ctx); err != nil {
		logger.Warn().Err(err).
			Msg("shadow kernel boot failed（影子期非承重，不阻断服务；接管完成后改为 fail-fast）")
		return nil
	}

	logger.Info().
		Str("publisher", shadowPublisher).
		Int("modules", len(kernel.BootOrder)).
		Msg("shadow kernel booted (K1 切片 2) — kernel.boot → audit.message_log；现有装配未改动")
	return k
}

// stopShadowKernel 关停影子内核。
//
// ─── 调用时序（重要，见 main.go 的调用点注释）──────────────────────
// 必须在现有 gracefulShutdown **之前**调用：gracefulShutdown 的 phase 4 会
// `store.Close()` 关掉连接池（setup.go），而 kernel.shutdown 要落
// audit.message_log —— 落库时 pool 必须还活着。内核内部顺序：先发
// kernel.shutdown（此时 msgbus/eventstore 仍在运行）再按 BootOrder 逆序停。
//
// 失败只 log：影子期非承重。
func stopShadowKernel(k *kernel.StandardKernel, logger zerolog.Logger) {
	if k == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := k.Shutdown(ctx); err != nil {
		logger.Warn().Err(err).
			Msg("shadow kernel shutdown failed（影子期非承重；接管完成后改为 fail-fast）")
		return
	}
	logger.Info().Msg("shadow kernel shut down (K1 切片 2) — kernel.shutdown → audit.message_log")
}
