package backtest

// P1-19 (Sprint 6, ODR-013 CQ-005, ADR-020):
// EngineOption — 函数式依赖注入模式（Go 惯用法）。
//
// 该文件实现 ADR-020 §3 "EngineOption 函数式注入"：
//
//   type EngineOption func(*Engine)
//
//   func WithRiskManager(rm *risk.RiskManager) EngineOption { ... }
//   func NewEngineWithOptions(cfg, provider, opts...) (*Engine, error)
//
// 收益：
//   - 10+ Setter → 1 variadic options pattern
//   - 必需参数（config, provider）vs 可选参数（riskManager, liveTrader）
//     在签名层面分离
//   - 测试构造更简洁：
//       NewEngineWithOptions(cfg, prov, WithRiskManager(mockRM), WithLiveTrader(fakeT))
//   - 未来扩展无需修改 Engine struct（直接新加一个 With* 选项）
//
// Backward Compat (per ADR-020 §2)：
//   - NewEngine(v, provider, logger) 旧签名保留 6 个月
//   - SetDataAdapter / SetStore / SetRiskManager / SetLiveTrader / SetExecutionService
//     5 个 engine setter 全部保留为 shim，内部委托到对应 bridge / field
//   - pkg/strategy.SetFactorCache 同步保留
//
// 与桥接组件的关系：
//   - WithLiveTrader / WithExecutionService 直接调用 bridge.Set
//   - WithRiskManager 写入 e.riskManager（in-process 风控不走 bridge）
//   - WithDataAdapter 写入 e.dataAdapter（multi-source 适配）
//   - WithStore 写入 e.store（factor DB 预热）

import (
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// EngineOption 函数式注入：每个选项返回的闭包会修改 *Engine。
//
// 设计要点：
//   - 全部"幂等"：重复应用同一选项以最后一次为准
//   - "失败" 仅来自 NewEngineWithOptions 内部（config 解析、provider 校验）
//   - 不在选项内部做重活（不发请求、不开连接）——只设引用
type EngineOption func(*Engine)

// WithRiskManager 注入 in-process 风控管理器。传 nil 退回 HTTP 风控服务。
func WithRiskManager(rm *risk.RiskManager) EngineOption {
	return func(e *Engine) {
		e.mu.Lock()
		e.riskManager = rm
		e.mu.Unlock()
	}
}

// WithDataAdapter 注入多数据源适配器。传 nil 退回原始 provider。
func WithDataAdapter(adapter *marketdata.DataAdapter) EngineOption {
	return func(e *Engine) {
		e.mu.Lock()
		e.dataAdapter = adapter
		e.mu.Unlock()
		if adapter != nil {
			e.logger.Info().Str("source", adapter.Primary()).Msg("DataAdapter attached via WithDataAdapter")
		}
	}
}

// WithStore 注入 Postgres 存储（用于 factor cache 预热、index constituents）。
func WithStore(store *storage.PostgresStore) EngineOption {
	return func(e *Engine) {
		e.mu.Lock()
		e.store = store
		e.mu.Unlock()
		if store != nil {
			e.logger.Info().Msg("PostgresStore attached via WithStore — factor cache warming enabled")
		}
	}
}

// WithLiveTrader 注入 live / paper trading trader。等价于 SetLiveTrader。
func WithLiveTrader(trader live.LiveTrader) EngineOption {
	return func(e *Engine) {
		e.liveBridge.Set(trader)
	}
}

// WithExecutionService 注入 ExecutionService。等价于 SetExecutionService。
func WithExecutionService(svc ExecutionService) EngineOption {
	return func(e *Engine) {
		e.executionBridge.Set(svc)
	}
}

// WithParallelWorkers 设置并发拉取 worker 数。<= 0 表示串行（1 worker）。
// 必须在 RunBacktest 之前调用。
func WithParallelWorkers(n int) EngineOption {
	return func(e *Engine) {
		e.parallelWorkers = n
	}
}

// WithLogger 覆盖 engine 默认 logger。
// 主要用于测试（zerolog.New(nil) 静默输出）。
func WithLogger(logger zerolog.Logger) EngineOption {
	return func(e *Engine) {
		e.mu.Lock()
		e.logger = logger
		e.mu.Unlock()
	}
}

// WithStateStore 注入自定义 StateStore（P1-18, ADR-020）。
//
// 默认 Engine 使用 LRUStateStore（capacity=1000）。以下场景可注入：
//   - 测试：WithStateStore(NewNoopStateStore()) 关闭 LRU 行为
//   - 生产：WithStateStore(newPersistentStateStore(db)) 落 PG
//   - 批处理：WithStateStore(NewLRUStateStore(10000)) 调高容量
//
// 注意：Option 在 NewEngineWithOptions 构造时立即覆盖默认 store;
// 后续 RunBacktest 创建的状态会写入新 store,旧 store 不会自动迁移。
func WithStateStore(store StateStore) EngineOption {
	return func(e *Engine) {
		if store == nil {
			e.logger.Warn().Msg("WithStateStore(nil) — keeping default LRUStateStore")
			return
		}
		e.mu.Lock()
		e.stateStore = store
		e.mu.Unlock()
		e.logger.Info().Msg("Custom StateStore attached via WithStateStore")
	}
}

// WithStateStoreCapacity 便捷选项：用给定容量构建 LRUStateStore。
// 等价于 WithStateStore(NewLRUStateStore(capacity))。
// capacity <= 0 时回退为 NoopStateStore（无界）。
func WithStateStoreCapacity(capacity int) EngineOption {
	return func(e *Engine) {
		if capacity <= 0 {
			e.mu.Lock()
			e.stateStore = NewNoopStateStore()
			e.mu.Unlock()
			return
		}
		e.mu.Lock()
		e.stateStore = NewLRUStateStore(capacity)
		e.mu.Unlock()
	}
}

// applyOptions 按顺序应用所有选项。
func applyOptions(e *Engine, opts []EngineOption) {
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
}
