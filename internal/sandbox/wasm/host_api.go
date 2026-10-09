package wasm

// K6 切片 2：host API 白名单 —— 前视隔离是唯一可行机制（ADR-029 §3）。
//
// 核心原则：不靠审阅代码保证安全，靠**能力隔离**。WASM 实例只能调用宿主
// 显式导入的函数；没有 import 的能力它物理上做不到。本节实现：
//
//  1. host 导入函数白名单（get_bar / get_series_len / get_symbol_count /
//     get_cross_section / get_state / set_state / emit_signal / log），
//     通过 wazero 的 HostModuleBuilder 注册，模块名固定为 "env"。
//  2. `get_bar(t_offset, field_id)` 在 `t_offset > 0` 时**直接 panic**（wazero
//     把 panic 转成 wasm trap），使「读未来数据」物理上不可能。get_cross_section
//     同样 trap。
//  3. 刻意**不提供**文件/网络/时钟/随机源/goroutine/进程/环境变量 ——
//     缺这些正是安全性的来源（ADR-029 §3.2 末）。
//
// 数据模型（per-symbol 实例）：ADR-029 §3 的 get_bar 签名**没有 symbol 参数**，
// 因此一个 WASM 实例绑定「当前 symbol 的序列」（BarContext.Series），get_bar
// 操作它；跨 symbol 用 get_cross_section(t_offset, idx)（BarContext.CrossSection
// + Symbols）。
//
// 「当前回放上下文」通过 context.WithValue 注入：调用 on_bar(t) 前 host 把
// *BarContext 塞进 ctx，host 函数从 ctx 取出（wazero 的 host 函数 ctx 是调用链
// 透传的，见 internal/wasm/gofunc.go 的 reflectGoModuleFunction.Call）。

import (
	"context"
	"fmt"
	"math"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// HostModuleName 是 host 导入函数的模块名，wasm 二进制 import 它。
const HostModuleName = "env"

// 白名单 host 函数的导出名。
const (
	HostFnGetBar          = "get_bar"
	HostFnGetSeriesLen    = "get_series_len"
	HostFnGetSymbolCount  = "get_symbol_count"
	HostFnGetCrossSection = "get_cross_section"
	HostFnGetState        = "get_state"
	HostFnSetState        = "set_state"
	HostFnEmitSignal      = "emit_signal"
	HostFnLog             = "log"
)

// hostCtxKey 是 context 中注入 *BarContext 的私有键。
type hostCtxKey struct{}

// BarContext 是 host 函数可见的「当前回放上下文」。
//
// 数据模型（per-symbol 实例）：一个 WASM 实例绑定一个 symbol 的序列。
//   - T        ：当前回放的 bar 索引（从 0 起）。[0, T] 是已可见历史 + 当前
//     bar，[T+1, ...] 是未来，get_bar 不可达（t_offset > 0 trap）。
//   - Series   ：当前 symbol 的逐 bar close 序列（get_bar / get_series_len 操作它）。
//   - CrossSection：全市场横截面（t → symbol → close），get_cross_section 用。
//   - Symbols  ：有序 symbol 列表（get_symbol_count / get_cross_section idx 的
//     确定性序）。
//   - State    ：宿主管理的持久化状态（get_state/set_state 载体；切片 3 的
//     SaveState/LoadState 续跑一致性依赖它）。
//   - Signals  ：emit_signal 收集的信号（host 可即时校验）。
type BarContext struct {
	T            int32
	Series       []float64
	CrossSection map[int32]map[string]float64
	Symbols      []string
	State        map[string][]byte
	Signals      []EmittedSignal
}

// EmittedSignal 是 wasm 经 emit_signal 发出的信号。
type EmittedSignal struct {
	SymbolIdx int32
	Action    int32
	Strength  float64
}

// withBarContext 把 *BarContext 注入 ctx，供 host 函数取出。
func withBarContext(ctx context.Context, bc *BarContext) context.Context {
	return context.WithValue(ctx, hostCtxKey{}, bc)
}

// barContextFrom 从 ctx 取 *BarContext；nil 表示调用方漏注入或越权直调，
// host 函数必须 fail-loud（panic → trap），不静默降级。
func barContextFrom(ctx context.Context) *BarContext {
	bc, _ := ctx.Value(hostCtxKey{}).(*BarContext)
	return bc
}

// trapFuture 是「读未来数据」的统一 trap：直接 panic，wazero 转成 wasm trap。
func trapFuture(tOffset int32) {
	panic(fmt.Sprintf("wasm: future access (t_offset=%d > 0)", tOffset))
}

// trapNoContext 是「host 函数在无 BarContext 时被调」的 fail-loud。
func trapNoContext(fn string) {
	panic(fmt.Sprintf("wasm: %s called without BarContext (host injection missing)", fn))
}

// InstantiateHostModule 在 runtime 上实例化 host 白名单模块 "env"。
// 必须在实例化任何依赖它的 wasm 策略模块**之前**调用。
func (r *WazeroRuntime) InstantiateHostModule(ctx context.Context) error {
	_, err := r.rt.NewHostModuleBuilder(HostModuleName).
		NewFunctionBuilder().WithFunc(hostGetBar).Export(HostFnGetBar).
		NewFunctionBuilder().WithFunc(hostGetSeriesLen).Export(HostFnGetSeriesLen).
		NewFunctionBuilder().WithFunc(hostGetSymbolCount).Export(HostFnGetSymbolCount).
		NewFunctionBuilder().WithFunc(hostGetCrossSection).Export(HostFnGetCrossSection).
		NewFunctionBuilder().WithFunc(hostGetState).Export(HostFnGetState).
		NewFunctionBuilder().WithFunc(hostSetState).Export(HostFnSetState).
		NewFunctionBuilder().WithFunc(hostEmitSignal).Export(HostFnEmitSignal).
		NewFunctionBuilder().WithFunc(hostLog).Export(HostFnLog).
		Instantiate(ctx)
	return err
}

// hostFnWhitelist 是白名单 host 函数名集合（import section 扫描用）。
var hostFnWhitelist = map[string]bool{
	HostFnGetBar:          true,
	HostFnGetSeriesLen:    true,
	HostFnGetSymbolCount:  true,
	HostFnGetCrossSection: true,
	HostFnGetState:        true,
	HostFnSetState:        true,
	HostFnEmitSignal:      true,
	HostFnLog:             true,
}

// ValidateImports 扫描编译后模块的 import section，拒绝任何「非 env 模块」或
// 「非白名单函数」的 import。这是 ADR-029 §3.4「可审阅 = 审源码 + host API
// 使用清单」的落地：清单靠扫描 import section 得出，未申报的 import 直接拒绝。
//
// 返回 nil 表示所有 import 都在白名单内；否则返回列出所有越权 import 的错误。
func ValidateImports(cm wazero.CompiledModule) error {
	var bad []string
	for _, fn := range cm.ImportedFunctions() {
		modName, name, isImport := fn.Import()
		if !isImport {
			continue
		}
		if modName != HostModuleName || !hostFnWhitelist[name] {
			bad = append(bad, fmt.Sprintf("%s.%s", modName, name))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("wasm: forbidden imports %v (only env whitelist allowed)", bad)
	}
	return nil
}

// hostGetBar 实现 get_bar(t_offset i32, field_id i32) → f64。
// 取当前 symbol 在 t+T 的 close。t_offset > 0 直接 trap；field_id 本切片恒取
// close（SeriesSpec 接线属后续切片，见注释）。
func hostGetBar(ctx context.Context, tOffset, fieldID int32) float64 {
	if tOffset > 0 {
		trapFuture(tOffset)
	}
	bc := barContextFrom(ctx)
	if bc == nil {
		trapNoContext(HostFnGetBar)
	}
	_ = fieldID // 本切片 field_id 未接线 SeriesSpec，恒取 close
	idx := bc.T + tOffset
	if idx < 0 || int32(len(bc.Series)) <= idx {
		return math.NaN() // 历史越界/无数据 → NaN（策略自行判断「不存在」）
	}
	return bc.Series[idx]
}

// hostGetSeriesLen 实现 get_series_len() → i32：当前 symbol 到当前 bar 的长度。
func hostGetSeriesLen(ctx context.Context) int32 {
	bc := barContextFrom(ctx)
	if bc == nil {
		trapNoContext(HostFnGetSeriesLen)
	}
	return int32(len(bc.Series))
}

// hostGetSymbolCount 实现 get_symbol_count() → i32：全市场 symbol 数。
func hostGetSymbolCount(ctx context.Context) int32 {
	bc := barContextFrom(ctx)
	if bc == nil {
		trapNoContext(HostFnGetSymbolCount)
	}
	return int32(len(bc.Symbols))
}

// hostGetCrossSection 实现 get_cross_section(t_offset i32, field_id i32, idx i32) → f64。
// 取「当前 t + t_offset」横截面上第 idx 个 symbol 的 close。t_offset > 0 trap。
func hostGetCrossSection(ctx context.Context, tOffset, fieldID, idx int32) float64 {
	if tOffset > 0 {
		trapFuture(tOffset)
	}
	bc := barContextFrom(ctx)
	if bc == nil {
		trapNoContext(HostFnGetCrossSection)
	}
	_ = fieldID
	t := bc.T + tOffset
	if t < 0 || idx < 0 || int32(len(bc.Symbols)) <= idx {
		return math.NaN()
	}
	sym := bc.Symbols[idx]
	row, ok := bc.CrossSection[t]
	if !ok {
		return math.NaN()
	}
	if v, ok := row[sym]; ok {
		return v
	}
	return math.NaN()
}

// hostGetState 实现 get_state(key_ptr i32, key_len i32) → i64（打包 ptr|len）。
// 读宿主管理的持久化状态；key 不存在返回 pack(0,0)。
func hostGetState(ctx context.Context, m api.Module, keyPtr, keyLen uint32) uint64 {
	bc := barContextFrom(ctx)
	if bc == nil {
		trapNoContext(HostFnGetState)
	}
	key, ok := m.Memory().Read(keyPtr, keyLen)
	if !ok {
		panic("wasm: get_state key out of range")
	}
	val, exists := bc.State[string(key)]
	if !exists {
		return PackPtrLen(0, 0)
	}
	if !m.Memory().Write(4096, val) {
		panic("wasm: get_state write out of range")
	}
	return PackPtrLen(4096, uint32(len(val)))
}

// hostSetState 实现 set_state(key_ptr, key_len, val_ptr, val_len) → void。
func hostSetState(ctx context.Context, m api.Module, keyPtr, keyLen, valPtr, valLen uint32) {
	bc := barContextFrom(ctx)
	if bc == nil {
		trapNoContext(HostFnSetState)
	}
	key, ok := m.Memory().Read(keyPtr, keyLen)
	if !ok {
		panic("wasm: set_state key out of range")
	}
	val, ok := m.Memory().Read(valPtr, valLen)
	if !ok {
		panic("wasm: set_state value out of range")
	}
	v := make([]byte, len(val))
	copy(v, val)
	bc.State[string(key)] = v
}

// hostEmitSignal 实现 emit_signal(symbol_idx i32, action i32, strength f64) → void。
func hostEmitSignal(ctx context.Context, symbolIdx, action int32, strength float64) {
	bc := barContextFrom(ctx)
	if bc == nil {
		trapNoContext(HostFnEmitSignal)
	}
	bc.Signals = append(bc.Signals, EmittedSignal{
		SymbolIdx: symbolIdx,
		Action:    action,
		Strength:  strength,
	})
}

// hostLog 实现 log(level i32, msg_ptr i32, msg_len i32) → void。
// 刻意空实现：沙箱内日志不落盘、不外发（ADR-029 §3.2 无文件/网络）。
func hostLog(_ context.Context, _ api.Module, _, _, _ uint32) {}
