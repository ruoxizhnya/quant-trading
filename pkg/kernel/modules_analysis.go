package kernel

// modules_analysis.go —— analysis 服务的 7 个业务模块包装（K1 切片 3：
// 接管 setup.go 装配）。
//
// ─── 映射裁决（模块名 ↔ 现有组件，BootOrder 冻结序）─────────────────
//
//	3. data-engine       ← marketdata 层：HTTP provider + DataAdapter（多源回退）
//	4. portfolio         ← 【无实体，Noop】现有 portfolio 状态活在
//	                       MockTrader / engine 内部，无独立可接线组件；
//	                       蓝图「恢复持仓」语义待实盘线（D1）才有载体。
//	5. risk-engine       ← risk.RiskManager
//	6. exec-engine       ← live.LiveTrader（本地为 MockTrader）
//	7. strategy-runtime  ← backtest.Engine 构造 + Set*(全部依赖注入)
//	8. indicators        ← 【无实体，Noop】K3 的 L2 有状态算子是
//	                       per-run 实例化（SaveState/LoadState），
//	                       服务级无常驻组件。
//	9. exec-algo         ← 【无实体，Noop】K4 执行算法是 per-run
//	                       挂点（蓝图 UC4），服务级无常驻组件。
//
// 三个 Noop 槽位**不是偷懒**：BootOrder 是宪法级冻结（改序 = 改契约），
// 槽位必须占住以保住「Shutdown 逆序完整」；每个 Noop 的理由写在装配处
// （cmd/analysis/kernel_wiring.go），接管 TASKS 条目有登记。
//
// ─── 构造逻辑为什么经闭包注入，而不写死在本包 ────────────────────────
// pkg/kernel 是纯装配机制；bootstrap（builder 库）import 业务包与 viper，
// kernel 若直接调 builder 会把「拿什么装配」（composition root 的职责）
// 混进「怎么装配」（本包职责）。故模块持有 cmd 注入的构造闭包，Init 执行；
// 产物经导出字段暴露，main 从模块检索装 ServerDeps —— 装配顺序的控制权
// 由此移交 BootOrder（本切片的验收核心）。
//
// ─── 顺序依赖的保证方式 ─────────────────────────────────────────────
// StrategyRuntimeModule 的 Init 读前序模块（data-engine / risk-engine /
// exec-engine）的**产物字段**：若 BootOrder 被打乱（宪法变更评审失控），
// 闭包会拿到 nil 并返回 error —— 顺序错误在 Boot 期 fail-fast，而不是
// 带着未注入的引擎静默运行。配套测试见 modules_analysis_test.go。

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/spf13/viper"
)

// ─── data-engine（BootOrder 第 3 位）────────────────────────────────

// DataEngineModule 持有行情数据能力：HTTP provider（行情经 data-service）
// 与 DataAdapter（PG 快照为主、HTTP 回退）。
type DataEngineModule struct {
	// build 由 cmd 注入：构造 provider + adapter（store 依赖由闭包捕获）。
	build func() (marketdata.Provider, *marketdata.DataAdapter, error)

	Provider marketdata.Provider
	Adapter  *marketdata.DataAdapter
}

// NewDataEngineModule 构造 data-engine 模块。
func NewDataEngineModule(build func() (marketdata.Provider, *marketdata.DataAdapter, error)) *DataEngineModule {
	return &DataEngineModule{build: build}
}

// Name 恒为 "data-engine"。
func (m *DataEngineModule) Name() string { return "data-engine" }

// Init 执行构造闭包（本地构造 + 配置校验，无副作用）。
func (m *DataEngineModule) Init(context.Context) error {
	provider, adapter, err := m.build()
	if err != nil {
		return fmt.Errorf("kernel: data-engine 构造失败: %w", err)
	}
	m.Provider, m.Adapter = provider, adapter
	return nil
}

// Start no-op：DataAdapter 无常驻连接（请求期懒连）。
func (m *DataEngineModule) Start(context.Context) error { return nil }

// Stop no-op：provider/adapter 无需释放的资源（连接归 engine/pool）。
func (m *DataEngineModule) Stop(context.Context) error { return nil }

// ─── risk-engine（BootOrder 第 5 位）────────────────────────────────

// RiskEngineModule 持有进程内风控管理器（P1-15 合并形态）。
type RiskEngineModule struct {
	build func() (*risk.RiskManager, error)

	// RiskManager 产出。engine 的 SetRiskManager 注入发生在
	// strategy-runtime（第 7 位），本模块只负责「风控就位」。
	RiskManager *risk.RiskManager
}

// NewRiskEngineModule 构造 risk-engine 模块。
func NewRiskEngineModule(build func() (*risk.RiskManager, error)) *RiskEngineModule {
	return &RiskEngineModule{build: build}
}

// Name 恒为 "risk-engine"。
func (m *RiskEngineModule) Name() string { return "risk-engine" }

// Init 执行构造闭包。
func (m *RiskEngineModule) Init(context.Context) error {
	rm, err := m.build()
	if err != nil {
		return fmt.Errorf("kernel: risk-engine 构造失败: %w", err)
	}
	m.RiskManager = rm
	return nil
}

// Start no-op：风控管理器无常驻资源。
func (m *RiskEngineModule) Start(context.Context) error { return nil }

// Stop no-op：同上。
func (m *RiskEngineModule) Stop(context.Context) error { return nil }

// ─── exec-engine（BootOrder 第 6 位）────────────────────────────────

// ExecEngineModule 持有执行端（本地为 MockTrader；实盘线 D1 时替换为
// 真券商 adapter —— 这正是「回测-实盘同构」的换件点）。
type ExecEngineModule struct {
	build func() (live.LiveTrader, error)

	Trader live.LiveTrader
}

// NewExecEngineModule 构造 exec-engine 模块。
func NewExecEngineModule(build func() (live.LiveTrader, error)) *ExecEngineModule {
	return &ExecEngineModule{build: build}
}

// Name 恒为 "exec-engine"。
func (m *ExecEngineModule) Name() string { return "exec-engine" }

// Init 执行构造闭包。
func (m *ExecEngineModule) Init(context.Context) error {
	t, err := m.build()
	if err != nil {
		return fmt.Errorf("kernel: exec-engine 构造失败: %w", err)
	}
	m.Trader = t
	return nil
}

// Start no-op：MockTrader 无外部连接（实盘线时此处连 broker）。
func (m *ExecEngineModule) Start(context.Context) error { return nil }

// Stop no-op：同上。
func (m *ExecEngineModule) Stop(context.Context) error { return nil }

// ─── strategy-runtime（BootOrder 第 7 位）───────────────────────────

// StrategyRuntimeDeps 聚合 strategy-runtime 构建所需的前序模块与共享件。
type StrategyRuntimeDeps struct {
	// Store 由 main 在 Boot 前构造（eventstore 模块复用它的 pool ——
	// 一个进程一个连接池），engine 经 SetStore 拿到落库能力。
	Store *storage.PostgresStore
	// DataEngine / RiskEngine / ExecEngine 是前序模块指针；产物在各自
	// Init（BootOrder 3/5/6）时填充，本模块 Init（第 7 位）读取。
	DataEngine *DataEngineModule
	RiskEngine *RiskEngineModule
	ExecEngine *ExecEngineModule
}

// StrategyRuntimeModule 持有回测引擎，并在 Init 时完成对前序模块产物的
// 全部依赖注入（SetStore / SetDataAdapter / SetRiskManager / SetLiveTrader）。
//
// 放在第 7 位是**顺序裁决**：此刻 data-engine(3) / risk-engine(5) /
// exec-engine(6) 的产物全部就位，Set* 一次注完；engine 的构造需要
// data-engine 的 provider，故它不可能在 data-engine 之前就绪。
type StrategyRuntimeModule struct {
	v      *viper.Viper
	logger zerolog.Logger
	deps   *StrategyRuntimeDeps

	// Engine 产出。ServerDeps.Engine 与 walk-forward 工厂都从它来。
	Engine *backtest.Engine
}

// NewStrategyRuntimeModule 构造 strategy-runtime 模块。
func NewStrategyRuntimeModule(v *viper.Viper, logger zerolog.Logger, deps *StrategyRuntimeDeps) *StrategyRuntimeModule {
	return &StrategyRuntimeModule{v: v, logger: logger, deps: deps}
}

// Name 恒为 "strategy-runtime"。
func (m *StrategyRuntimeModule) Name() string { return "strategy-runtime" }

// Init 构造引擎并完成依赖注入；前序产物缺失即 fail-fast。
func (m *StrategyRuntimeModule) Init(context.Context) error {
	d := m.deps
	if d.DataEngine == nil || d.DataEngine.Provider == nil {
		return fmt.Errorf("kernel: strategy-runtime 依赖 data-engine 产物，但其为 nil" +
			"（BootOrder 被打乱？strategy-runtime 在第 7 位，data-engine 在第 3 位）")
	}
	if d.RiskEngine == nil || d.RiskEngine.RiskManager == nil {
		return fmt.Errorf("kernel: strategy-runtime 依赖 risk-engine 产物，但其为 nil" +
			"（BootOrder 被打乱？risk-engine 在第 5 位）")
	}
	if d.ExecEngine == nil || d.ExecEngine.Trader == nil {
		return fmt.Errorf("kernel: strategy-runtime 依赖 exec-engine 产物，但其为 nil" +
			"（BootOrder 被打乱？exec-engine 在第 6 位）")
	}
	if d.Store == nil {
		return fmt.Errorf("kernel: strategy-runtime 依赖 store，但其为 nil（装配传参缺失）")
	}

	engine, err := backtest.NewEngine(m.v, d.DataEngine.Provider, m.logger)
	if err != nil {
		return fmt.Errorf("kernel: strategy-runtime 构造引擎失败: %w", err)
	}
	engine.SetStore(d.Store)
	engine.SetDataAdapter(d.DataEngine.Adapter)
	engine.SetRiskManager(d.RiskEngine.RiskManager)
	engine.SetLiveTrader(d.ExecEngine.Trader)

	m.Engine = engine
	return nil
}

// Start no-op：引擎无常驻连接（回测运行期才拉数）。
func (m *StrategyRuntimeModule) Start(context.Context) error { return nil }

// Stop no-op：引擎无独立资源（store 的 pool 由 storage 统一 Close）。
func (m *StrategyRuntimeModule) Stop(context.Context) error { return nil }

// ─── 编译期合规检查 ────────────────────────────────────────────────
var (
	_ Module = (*DataEngineModule)(nil)
	_ Module = (*RiskEngineModule)(nil)
	_ Module = (*ExecEngineModule)(nil)
	_ Module = (*StrategyRuntimeModule)(nil)
)
