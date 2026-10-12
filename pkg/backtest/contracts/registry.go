// K10（EngineRunner 可插拔 + 内核路由）：执行器注册表。
//
// 设计依据：设计文档 §2.4/FR-1 与 ADR-031 D1 —— 内核降级为「可替换的
// 执行器」，外围只依赖稳定契约。RunSpec.kernel（K9 冻结的路由字段）是
// 路由键：一次回测运行请求指定由哪个内核执行，未指定时走默认 go 内核
// （FR-1.1，现有行为逐字节不变）。
//
// 三条硬语义（FR-1.3，fail-closed）：
//  1. 未知/未注册内核 → Resolve 返回错误并**点名内核名**，绝不静默回退
//     到默认内核 —— 防「以为在测 NT 其实在测 Go」；
//  2. 注册只在**装配期**发生（Register），运行期不改 —— 无热插拔；
//  3. 注册表与路由口径只有这一处实现：同步 handler、异步 job、batch
//     编排全部经 Resolve 取 runner（walk-forward 每窗口需全新 Engine
//     实例保缓存隔离，保留工厂构造，见 pkg/backtest/aliases.go 的
//     NewWalkForwardEngine 注释）。
//
// 接口选型说明：注册表承载 contracts.EngineRunner
// （RunBacktest(ctx, BacktestRequest) → *BacktestResponse）—— 这是
// 同步 handler / job / batch / walk-forward 四家消费方共同使用的窄
// 接口（pkg/backtest/contracts/contracts.go）。runner.go 里的
// BacktestRunner（五参签名、返回 *domain.BacktestResult）是 AI 管线
// 的端口，**签名不动**，与注册表无关；两者刻意保持分离，避免给调用链
// 引入适配层。
package contracts

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ErrKernelNotRegistered 是 Resolve 对未注册内核返回的哨兵错误。
// 调用方用 errors.Is 判定，禁止字符串匹配。
var ErrKernelNotRegistered = errors.New("kernel not registered")

// EngineRegistry 把内核名映射到 EngineRunner 实现。
//
// 并发模型：Register 只在装配期（单线程）调用；Resolve 可被 HTTP
// handler 与 job worker 并发调用 —— 内部 RWMutex 保证安全。零值可用
// （空注册表，Resolve 一律 fail-closed）。
//
// 内核名区分大小写、精确匹配（"Nautilus" ≠ "nautilus"）—— K13 注册
// NT runner 时沿用小写惯例（对齐 DefaultKernel）。
type EngineRegistry struct {
	mu      sync.RWMutex
	runners map[string]EngineRunner
}

// NewEngineRegistry 返回空注册表。go 内核由装配方显式 Register
// （默认名 contracts.DefaultKernel）—— 注册表不预设任何内核，
// 「默认选中」来自 EffectiveKernel 的键解析，不来自注册表隐式注册。
func NewEngineRegistry() *EngineRegistry {
	return &EngineRegistry{runners: make(map[string]EngineRunner)}
}

// Register 在装配期登记一个内核的执行器。
//
// Must 风格（对齐 sql.Register / regexp.MustCompile 的先例）： misuse
// 一律 panic —— 空名、nil runner、重名都是装配期编程错误，fail-fast
// 好过带着半套注册表对外提供服务。运行期注册（热插拔）刻意不支持。
//
// 注意 typed-nil 陷阱：Register(name, (*Engine)(nil)) 过不了 nil 检查
// （接口非 nil），会在首次 RunBacktest 时崩溃而非静默出错 —— 装配方
// 负责不传 typed-nil（生产接线传的是真引擎）。
func (r *EngineRegistry) Register(name string, runner EngineRunner) {
	if name == "" {
		panic("contracts: EngineRegistry.Register: kernel name must not be empty")
	}
	if runner == nil {
		panic(fmt.Sprintf("contracts: EngineRegistry.Register(%q): runner must not be nil", name))
	}
	r.mu.Lock()
	if r.runners == nil {
		r.runners = make(map[string]EngineRunner)
	}
	if _, dup := r.runners[name]; dup {
		// 先解锁再 panic（审查修复 2026-10-12）：panic 若被 recover
		//（测试的 assert.Panics、将来的 Must 包装器），defer Unlock
		// 不会执行，注册表会带着永久写锁变成死锁陷阱。Must 语义的
		// 正确形状是「panic 后残留状态仍可用」，这里把锁归位。
		r.mu.Unlock()
		panic(fmt.Sprintf(
			"contracts: EngineRegistry.Register(%q): duplicate kernel registration — registration is assembly-time only (no hot-plug)",
			name))
	}
	r.runners[name] = runner
	r.mu.Unlock()
}

// Resolve 按内核名取执行器。未注册（含空名）→ 错误，且**绝不回退**。
//
// 错误信息点名三件事：被拒的内核名、当前已注册集合、以及「拒绝而非
// 回退」的语义 —— 同步路径把它原样透出给调用方（400），job 路径落进
// error_msg，两端都能一眼看出「哪个内核为什么没跑」。
func (r *EngineRegistry) Resolve(name string) (EngineRunner, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	runner, ok := r.runners[name]
	if !ok || runner == nil {
		return nil, fmt.Errorf(
			"%w: kernel %q is not registered (registered kernels: [%s]); run refused — no silent fallback to the default %q kernel",
			ErrKernelNotRegistered, name, strings.Join(r.namesLocked(), ", "), DefaultKernel)
	}
	return runner, nil
}

// Names 返回已注册内核名的排序副本（诊断 / 测试 / 装配日志用）。
// 修改返回值不影响注册表。
func (r *EngineRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.namesLocked()
}

// namesLocked 调用方必须已持有 r.mu（读或写）。
func (r *EngineRegistry) namesLocked() []string {
	names := make([]string, 0, len(r.runners))
	for name := range r.runners {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
