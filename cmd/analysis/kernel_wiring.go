package main

// kernel_wiring.go —— 内核接管装配（K1 切片 3）。
//
// ─── 与切片 2（影子启动）的关系 ────────────────────────────────────
// 切片 2 的影子内核在现有装配**之外**跑（非承重、失败不阻断、7 个 Noop
// 顶位）。本切片把业务组件的真实构造搬进模块 Init，装配顺序的控制权移交
// BootOrder；影子启动随之删除，「失败不阻断」按裁决改为 **fail-fast** ——
// 内核已是承重组件，起不来就不该对外提供服务。
//
// ─── 装配全景（谁建什么）────────────────────────────────────────────
//   main()：logger / metrics / config / **store**（Boot 前构造：eventstore
//           模块复用它的 pool，连接池一个进程一个）
//   kernel.Boot（按 BootOrder）：
//     1. eventstore        真模块（adapters.go，Init 建表）
//     2. clock             真模块（LiveClock——服务级内核用墙钟）
//     3. data-engine       真模块（provider + DataAdapter）
//     4. portfolio         Noop（无实体组件，理由见 modules_analysis.go）
//     5. risk-engine       真模块（RiskManager）
//     6. exec-engine       真模块（MockTrader）
//     7. strategy-runtime  真模块（Engine 构造 + Set* 全部注入）
//     8. indicators        Noop（L2 算子 per-run 实例化）
//     9. exec-algo         Noop（K4 执行算法 per-run 挂点）
//    10. msgbus            真模块（最后开分发）
//   main()（Boot 之后）：从模块检索产物装 ServerDeps；JobService/WF/Batch/
//           FactorAttributor/StrategyDB/auth/alert 等 HTTP 面组件仍由
//           buildDataServices / initStrategyAndPlugins / initAuth 构造
//           —— 它们不在蓝图 10 模块矩阵内（无 BootOrder 槽位；改序列
//           = 改宪法须走变更评审），偏差已在接管 TASKS 条目登记。
//
// ─── 关停时序（继承切片 2 的裁决，不变）──────────────────────────────
// k.Shutdown 必须在 gracefulShutdown **之前**调用：gracefulShutdown 的
// phase 4 会 store.Close() 关连接池，而 kernel.shutdown 要落
// audit.message_log —— 落库时 pool 必须活着。内核内部先发 kernel.shutdown
// 再按 BootOrder 逆序停（msgbus 最先停、eventstore 最后停）。

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/bootstrap"
	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/eventstore"
	"github.com/ruoxizhnya/quant-trading/pkg/kernel"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/msgbus"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/spf13/viper"
)

// kernelPublisher 是内核写入 audit.message_log 的 publisher 标识（继承
// 影子期的取值裁决：接管后这条记录仍应可读为「kernel 在何时起停」）。
const kernelPublisher = "kernel"

// AnalysisWiring 是 Boot 后从模块检索出的产物包，main 用它装 ServerDeps。
// 字段与接管前 main() 的局部变量一一对应 —— 装配产物没变，变的只是
// 「谁决定构造顺序」。
type AnalysisWiring struct {
	Engine   *backtest.Engine
	Provider marketdata.Provider
	Adapter  *marketdata.DataAdapter

	RiskManager     *risk.RiskManager
	ExecutionTrader live.LiveTrader
}

// AnalysisKernel 聚合 Boot 成功后的内核与产物检索结果。
type AnalysisKernel struct {
	Kernel *kernel.StandardKernel
	Wiring *AnalysisWiring
}

// assembleKernel 构造 10 模块内核并 Boot（fail-fast）。
//
// 任一环节失败即返回 error（main 里 logger.Fatal）——接管后内核是承重
// 组件，不再有「失败不阻断」的豁免。
func assembleKernel(v *viper.Viper, store *storage.PostgresStore, logger zerolog.Logger) (*AnalysisKernel, error) {
	if store == nil {
		return nil, fmt.Errorf("kernel: store 为 nil —— Boot 前必须先构造 PostgresStore" +
			"（eventstore 模块复用它的 pool，见 pkg/eventstore 契约）")
	}

	clk := clock.NewLiveClock()
	es, err := eventstore.NewPGEventStore(store.DB(), kernelPublisher, "")
	if err != nil {
		return nil, fmt.Errorf("kernel: 构造 eventstore 失败: %w", err)
	}
	bus := msgbus.NewLiveBus(es)
	k := kernel.NewKernel(clk, bus, es)

	// data-engine（第 3 位）：行情能力。构造逻辑与接管前
	// buildBacktestEngine + buildDataServices 的对应行逐字一致。
	dataMod := kernel.NewDataEngineModule(func() (marketdata.Provider, *marketdata.DataAdapter, error) {
		httpProvider := marketdata.NewHTTPProvider(dataServiceURL(v), logger)
		pgProvider := marketdata.NewPostgresProvider(store, logger)
		adapter := marketdata.NewDataAdapter(nil, pgProvider, httpProvider, logger)
		return httpProvider, adapter, nil
	})

	// risk-engine（第 5 位）。
	riskMod := kernel.NewRiskEngineModule(func() (*risk.RiskManager, error) {
		return bootstrap.BuildRiskManager(v, logger), nil
	})

	// exec-engine（第 6 位）。
	execMod := kernel.NewExecEngineModule(func() (live.LiveTrader, error) {
		return bootstrap.BuildExecutionTrader(v, logger), nil
	})

	// strategy-runtime（第 7 位）：引擎构造 + 全部 Set* 注入在模块 Init 内
	// 完成（依赖前序产物，缺失即 fail-fast，见 modules_analysis.go）。
	strategyMod := kernel.NewStrategyRuntimeModule(v, logger, &kernel.StrategyRuntimeDeps{
		Store:      store,
		DataEngine: dataMod,
		RiskEngine: riskMod,
		ExecEngine: execMod,
	})

	// 三个 Noop 槽位（4/8/9）—— 理由见 pkg/kernel/modules_analysis.go 头部
	// 映射裁决表；BootOrder 是宪法，槽位必须占住。
	placeholders := []*kernel.NoopModule{
		kernel.NewNoopModule("portfolio"),
		kernel.NewNoopModule("indicators"),
		kernel.NewNoopModule("exec-algo"),
	}

	for _, m := range []kernel.Module{
		kernel.NewEventStoreModule(es),
		kernel.NewClockModule(clk),
		dataMod,
		placeholders[0],
		riskMod,
		execMod,
		strategyMod,
		placeholders[1],
		placeholders[2],
		kernel.NewMsgBusModule(bus),
	} {
		if err := k.Register(m); err != nil {
			return nil, fmt.Errorf("kernel: 注册模块 %s 失败: %w", m.Name(), err)
		}
	}

	// Boot：按 BootOrder 逐个 Init→Start，失败内部已逆序回滚；此处
	// fail-fast（接管后内核承重）。超时 20s 继承影子期取值。
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := k.Boot(ctx); err != nil {
		return nil, fmt.Errorf("kernel: Boot 失败（接管后为承重路径，服务拒绝启动）: %w", err)
	}

	// 产物检索：各模块的导出字段 → wiring（字段与接管前 main 的局部变量
	// 一一对应）。
	wiring := &AnalysisWiring{
		Engine:          strategyMod.Engine,
		Provider:        dataMod.Provider,
		Adapter:         dataMod.Adapter,
		RiskManager:     riskMod.RiskManager,
		ExecutionTrader: execMod.Trader,
	}

	logger.Info().
		Str("publisher", kernelPublisher).
		Int("modules", len(kernel.BootOrder)).
		Msg("kernel booted (K1 切片 3) — 装配顺序由 BootOrder 接管，kernel.boot → audit.message_log")

	return &AnalysisKernel{Kernel: k, Wiring: wiring}, nil
}

// dataServiceURL 取 data-service 地址（与 bootstrap.BuildBacktestEngine
// 的缺省口径一致：留空回落 :8081）。
func dataServiceURL(v *viper.Viper) string {
	if u := v.GetString("data_service.url"); u != "" {
		return u
	}
	return "http://localhost:8081"
}

// shutdownKernel 关停内核（继承切片 2 的时序裁决：必须在 gracefulShutdown
// 之前调用，理由见文件头）。接管后失败**不再静默**：关停失败打 error 级
// 日志（kernel.shutdown 是否落库，运维必须知道）。
func shutdownKernel(k *kernel.StandardKernel, logger zerolog.Logger) {
	if k == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := k.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("kernel shutdown failed — kernel.shutdown 事件可能未落 audit.message_log")
		return
	}
	logger.Info().Msg("kernel shut down (K1 切片 3) — kernel.shutdown → audit.message_log")
}
