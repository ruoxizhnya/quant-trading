package wasm

// K6 切片 2 的 host API 白名单测试。
//
// 核心正证据：get_bar(t_offset>0) → trap（前视物理不可能）；get_bar(t_offset≤0)
// 正常返回历史/当前值；get_state/set_state 往返一致。
//
// 测试用 wasm 模块通过 wasm_builder_test.go 构造。关键点：import 函数的函数索引
// 空间编号（imports 在前，本地 funcs 在后），export 引用本地 func 时要加
// funcIndexBase()。

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wasmGetBar 构造一个「导出 run() → f64，返回 get_bar(offset=0, field=0)」的模块。
// 它 import env.get_bar，调用它并把结果返回。用于验证 host 函数可达 + 返回值正确。
func wasmGetBar() []byte {
	m := &wasmModule{
		types: []wasmFuncType{
			{params: []byte{valI32, valI32}, results: []byte{valF64}}, // get_bar 签名
			{results: []byte{valF64}},                                 // run 签名
		},
		imports: []wasmImport{
			{module: "env", name: "get_bar", typeIdx: 0},
		},
		funcs: []uint32{1}, // run → type 1
		codes: [][]byte{
			{ // run: call get_bar(0, 0)
				opI32Const, 0x00, // t_offset = 0
				opI32Const, 0x00, // field_id = 0
				opCall, 0x00, // call import[0] = get_bar
				opEnd,
			},
		},
	}
	m.export = []wasmExport{
		{name: "run", kind: 0, idx: m.funcIndexBase() + 0},
	}
	return m.encode()
}

// wasmGetBarWithOffset 构造「导出 run(t_offset i32) → f64，返回 get_bar(t_offset, 0)」。
// 调用方控制 t_offset，用于测正负偏移。
func wasmGetBarWithOffset() []byte {
	m := &wasmModule{
		types: []wasmFuncType{
			{params: []byte{valI32, valI32}, results: []byte{valF64}}, // get_bar
			{params: []byte{valI32}, results: []byte{valF64}},         // run(t_offset)
		},
		imports: []wasmImport{
			{module: "env", name: "get_bar", typeIdx: 0},
		},
		funcs: []uint32{1},
		codes: [][]byte{
			{ // run(t_offset): call get_bar(t_offset, 0)
				opLocalGet, 0x00, // t_offset
				opI32Const, 0x00, // field_id = 0
				opCall, 0x00,
				opEnd,
			},
		},
	}
	m.export = []wasmExport{
		{name: "run", kind: 0, idx: m.funcIndexBase() + 0},
	}
	return m.encode()
}

// newHostRuntime 创建 WazeroRuntime 并实例化 host 白名单模块 "env"。
func newHostRuntime(t *testing.T) *WazeroRuntime {
	t.Helper()
	r := NewWazeroRuntime(context.Background(), DefaultMaxMemory)
	require.NoError(t, InstantiateHostModule(context.Background(), r.rt))
	t.Cleanup(func() { r.Close(context.Background()) })
	return r
}

// TestHostGetBar_PastAndCurrent 是正证据：get_bar(t_offset≤0) 正常返回历史/当前值。
func TestHostGetBar_PastAndCurrent(t *testing.T) {
	r := newHostRuntime(t)
	mod, err := r.Compile(context.Background(), wasmGetBar())
	require.NoError(t, err)
	inst, err := mod.Instantiate(context.Background(), DefaultMaxMemory)
	require.NoError(t, err)
	defer inst.Close()

	bc := &BarContext{
		T:      3,
		Series: []float64{10, 11, 12, 13, 14},
		State:  map[string][]byte{},
	}
	ctx := withBarContext(context.Background(), bc)

	// get_bar(0, 0) → 当前 bar close = Series[3] = 13
	res, err := inst.Call(ctx, "run")
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.InDelta(t, 13.0, float64FromF64Bits(res[0]), 1e-9, "get_bar(0) 应返回当前 bar")
}

// TestHostGetBar_NegativeOffset 是正证据：get_bar(t_offset<0) 正常返回历史值。
func TestHostGetBar_NegativeOffset(t *testing.T) {
	r := newHostRuntime(t)
	mod, err := r.Compile(context.Background(), wasmGetBarWithOffset())
	require.NoError(t, err)
	inst, err := mod.Instantiate(context.Background(), DefaultMaxMemory)
	require.NoError(t, err)
	defer inst.Close()

	bc := &BarContext{
		T:      3,
		Series: []float64{10, 11, 12, 13, 14},
		State:  map[string][]byte{},
	}
	ctx := withBarContext(context.Background(), bc)

	// get_bar(-2, 0) → Series[1] = 11。wasm 的 i32 参数经 uint64 传，-2 需
	// 转成 32 位补码（0xfffffffe），再零扩展成 uint64。
	var tOffset int32 = -2
	res, err := inst.Call(ctx, "run", uint64(uint32(tOffset)))
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.InDelta(t, 11.0, float64FromF64Bits(res[0]), 1e-9, "get_bar(-2) 应返回历史 bar")
}

// TestHostGetBar_FutureTraps 是核心正证据（前视隔离）：get_bar(t_offset>0) → trap。
func TestHostGetBar_FutureTraps(t *testing.T) {
	r := newHostRuntime(t)
	mod, err := r.Compile(context.Background(), wasmGetBarWithOffset())
	require.NoError(t, err)
	inst, err := mod.Instantiate(context.Background(), DefaultMaxMemory)
	require.NoError(t, err)
	defer inst.Close()

	bc := &BarContext{
		T:      3,
		Series: []float64{10, 11, 12, 13, 14, 15, 16},
		State:  map[string][]byte{},
	}
	ctx := withBarContext(context.Background(), bc)

	// get_bar(+1, 0) → 必须 trap（error，非正常返回）。
	_, err = inst.Call(ctx, "run", 1)
	require.Error(t, err, "get_bar(t_offset>0) 必须 trap，前视物理不可能")
	t.Logf("防前视 trap 生效：err=%v", err)
}

// TestHostGetStateSetState 是 get_state/set_state 往返一致的正证据。
func TestHostGetStateSetState(t *testing.T) {
	// 直接测 host 函数（无需 wasm 模块）—— 通过直接调用 hostGetState/hostSetState
	// 验证状态载体语义，等价于 wasm 侧往返。
	bc := &BarContext{State: map[string][]byte{}}
	ctx := withBarContext(context.Background(), bc)

	// set 需要 api.Module 的内存；这里用一个真实实例的内存。
	r := newHostRuntime(t)
	mod, _ := r.Compile(context.Background(), wasmIdentity())
	inst, _ := mod.Instantiate(context.Background(), DefaultMaxMemory)
	defer inst.Close()
	winst := inst.(*wazeroInstance)

	// 写 key="k1" 到内存 offset 0，val="v1" 到 offset 100。
	require.NoError(t, winst.WriteMemory(0, []byte("k1")))
	require.NoError(t, winst.WriteMemory(100, []byte("v1")))

	hostSetState(ctx, winst.mod, 0, 2, 100, 2)
	assert.Equal(t, []byte("v1"), bc.State["k1"], "set_state 应写入宿主状态")

	// get_state 读回。
	packed := hostGetState(ctx, winst.mod, 0, 2)
	ptr, length := UnpackPtrLen(packed)
	assert.Equal(t, uint32(2), length)
	val, ok := winst.mod.Memory().Read(ptr, length)
	require.True(t, ok)
	assert.Equal(t, []byte("v1"), val)
}

// wasmForbiddenImport 构造一个「import env.forbidden_fn」的模块——白名单外的
// import，Compile 必须拒绝（fail-closed）。
func wasmForbiddenImport() []byte {
	m := &wasmModule{
		types: []wasmFuncType{
			{results: []byte{valI32}}, // forbidden_fn 签名
		},
		imports: []wasmImport{
			{module: "env", name: "forbidden_fn", typeIdx: 0},
		},
	}
	return m.encode()
}

// TestValidateImports_Forbidden 是 import section 扫描器的核心正证据：白名单外
// 的 import 被拒绝。
func TestValidateImports_Forbidden(t *testing.T) {
	r := newHostRuntime(t)
	_, err := r.Compile(context.Background(), wasmForbiddenImport())
	require.Error(t, err, "白名单外的 import 必须被拒绝")
	assert.Contains(t, err.Error(), "forbidden", "错误应点名越权 import")
	t.Logf("import 扫描器拒绝：%v", err)
}

// TestValidateImports_WhitelistPasses 是白名单内 import 通过的对照：get_bar 合法。
func TestValidateImports_WhitelistPasses(t *testing.T) {
	r := newHostRuntime(t)
	_, err := r.Compile(context.Background(), wasmGetBar())
	assert.NoError(t, err, "白名单内 import（get_bar）应通过")
}

// TestValidateImports_WrongModule 是「非 env 模块」的拒绝：import wasi.xxx。
func TestValidateImports_WrongModule(t *testing.T) {
	m := &wasmModule{
		types: []wasmFuncType{
			{results: []byte{valI32}},
		},
		imports: []wasmImport{
			{module: "wasi_snapshot_preview1", name: "proc_exit", typeIdx: 0},
		},
	}
	r := newHostRuntime(t)
	_, err := r.Compile(context.Background(), m.encode())
	require.Error(t, err, "非 env 模块的 import 必须被拒绝")
	assert.Contains(t, err.Error(), "wasi_snapshot_preview1", "错误应点名越权模块")
}

// float64FromF64Bits 把 wasm 返回的 uint64（f64 的位模式）还原为 float64。
func float64FromF64Bits(bits uint64) float64 {
	return math.Float64frombits(bits)
}
