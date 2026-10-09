package wasm

// K6 切片 1 重写后的 sandbox 测试。InProcessRuntime 已退役，全部测试改用
// 真实 wazero 运行时 + 手工构造的 WASM 字节码模块。
//
// 关键约束：本机无 wasm 编译工具链（无 tinygo/wat2wasm），所以测试模块字节码
// 用本文件底部的极简 builder 结构化构造（参考 wazero 官方测试的 encodeModule
// 做法），而非手写十六进制——手写极易错，builder 可读且可维护。
//
// 测试刻意收敛到「Runtime 接口契约」（编译/实例化/内存/调用/超时/并发），不测
// 策略语义。策略语义 + host API 白名单在 K6 切片 2 用更复杂的模块测。

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── 测试用的 WASM 模块构造 ───────────────────────────────────────

// wasmReturnZero: (module (func (export "run") (result i64) i64.const 0))
// 无内存、无参数，仅验证「编译 + 实例化 + 调用 + 返回常量」。
func wasmReturnZero() []byte {
	m := &wasmModule{
		types:  []wasmFuncType{{results: []byte{valI64}}},
		funcs:  []uint32{0},
		export: []wasmExport{{name: "run", kind: 0, idx: 0}},
		codes:  [][]byte{{opI64Const, 0x00, opEnd}},
	}
	return m.encode()
}

// wasmIdentity: 导出 "run"(ptr i32, len i32) → i64，返回 pack(ptr, len)。
// 即「恒等 echo」——不复制、不处理，直接返回输入指针和长度。WASMSandbox.Run
// 写入 input 到 offset 0，调 run(0, len)，读回 pack(0,len) 指向的原 input，
// 因此 output == input。带 1 页内存（供内存读写测试用）。
//
// pack 约定（见 PackPtrLen）：low 32 = ptr，high 32 = len，即 ptr|(len<<32)。
func wasmIdentity() []byte {
	body := []byte{
		opLocalGet, 0x01, // local.get 1 (len)
		opI64ExtendI32U,  // i64.extend_i32_u
		opI64Const, 0x20, // i64.const 32
		opI64Shl,         // i64.shl → len<<32
		opLocalGet, 0x00, // local.get 0 (ptr)
		opI64ExtendI32U, // i64.extend_i32_u
		opI64Or,         // i64.or → (len<<32)|ptr
		opEnd,
	}
	m := &wasmModule{
		types:  []wasmFuncType{{params: []byte{valI32, valI32}, results: []byte{valI64}}},
		funcs:  []uint32{0},
		memory: &wasmMemory{min: 1},
		export: []wasmExport{
			{name: "memory", kind: 2, idx: 0},
			{name: "run", kind: 0, idx: 0},
		},
		codes: [][]byte{body},
	}
	return m.encode()
}

// wasmStrategy: 导出 initialize(params_ptr,params_len)→i32（返回 0）和
// generate_signals(bars_ptr,bars_len)→i64（恒等返回 pack(ptr,len)）。
// 带 1 页内存（供 Initialize/GenerateSignals 写入 input）。用于
// StrategyPluginSession 测试。
func wasmStrategy() []byte {
	m := &wasmModule{
		types: []wasmFuncType{
			{params: []byte{valI32, valI32}, results: []byte{valI32}}, // initialize
			{params: []byte{valI32, valI32}, results: []byte{valI64}}, // generate_signals
		},
		funcs:  []uint32{0, 1},
		memory: &wasmMemory{min: 1},
		export: []wasmExport{
			{name: "memory", kind: 2, idx: 0},
			{name: "initialize", kind: 0, idx: 0},
			{name: "generate_signals", kind: 0, idx: 1},
		},
		codes: [][]byte{
			{opI32Const, 0x00, opEnd}, // initialize: i32.const 0; end
			{ // generate_signals: pack(ptr, len) = ptr|(len<<32)
				opLocalGet, 0x01, opI64ExtendI32U, opI64Const, 0x20, opI64Shl,
				opLocalGet, 0x00, opI64ExtendI32U, opI64Or, opEnd,
			},
		},
	}
	return m.encode()
}

// newTestRuntime 创建 WazeroRuntime（生产实现）。
func newTestRuntime() *WazeroRuntime {
	return NewWazeroRuntime(context.Background(), DefaultMaxMemory)
}

// ─── PackPtrLen tests ────────────────────────────────────────────

func TestPackPtrLen(t *testing.T) {
	t.Parallel()
	packed := PackPtrLen(100, 200)
	ptr, length := UnpackPtrLen(packed)
	assert.Equal(t, uint32(100), ptr)
	assert.Equal(t, uint32(200), length)
}

func TestPackPtrLen_Zero(t *testing.T) {
	t.Parallel()
	packed := PackPtrLen(0, 0)
	ptr, length := UnpackPtrLen(packed)
	assert.Equal(t, uint32(0), ptr)
	assert.Equal(t, uint32(0), length)
}

func TestPackPtrLen_MaxValues(t *testing.T) {
	t.Parallel()
	packed := PackPtrLen(^uint32(0), ^uint32(0))
	ptr, length := UnpackPtrLen(packed)
	assert.Equal(t, ^uint32(0), ptr)
	assert.Equal(t, ^uint32(0), length)
}

// ─── Config tests ───────────────────────────────────────────────

func TestConfig_Defaults(t *testing.T) {
	t.Parallel()
	c := Config{}.withDefaults()
	assert.Equal(t, DefaultMaxMemory, c.MaxMemoryBytes)
	assert.Equal(t, DefaultMaxExecutionTime, c.MaxExecutionTime)
}

func TestConfig_OverridesRespected(t *testing.T) {
	t.Parallel()
	c := Config{
		MaxMemoryBytes:   128 << 20,
		MaxExecutionTime: 60 * time.Second,
	}.withDefaults()
	assert.Equal(t, 128<<20, c.MaxMemoryBytes)
	assert.Equal(t, 60*time.Second, c.MaxExecutionTime)
}

// ─── WazeroRuntime tests ─────────────────────────────────────────

func TestWazeroRuntime_Compile(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	mod, err := r.Compile(context.Background(), wasmReturnZero())
	require.NoError(t, err)
	assert.NotNil(t, mod)
	assert.Contains(t, mod.Exports(), "run")
}

func TestWazeroRuntime_Compile_Invalid(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	_, err := r.Compile(context.Background(), []byte("not wasm"))
	assert.ErrorIs(t, err, ErrModuleNotFound)
}

func TestWazeroRuntime_Close(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	_, err := r.Compile(context.Background(), wasmReturnZero())
	require.NoError(t, err)
	err = r.Close(context.Background())
	require.NoError(t, err)
	// After close, Compile should fail (wazero runtime closed).
	_, err = r.Compile(context.Background(), wasmReturnZero())
	assert.Error(t, err)
}

// ─── Instance memory tests ──────────────────────────────────────

func TestInstance_WriteReadMemory(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	mod, err := r.Compile(context.Background(), wasmIdentity())
	require.NoError(t, err)
	inst, err := mod.Instantiate(context.Background(), DefaultMaxMemory)
	require.NoError(t, err)
	defer inst.Close()

	data := []byte("hello world")
	err = inst.WriteMemory(0, data)
	require.NoError(t, err)

	read, err := inst.ReadMemory(0, uint32(len(data)))
	require.NoError(t, err)
	assert.Equal(t, data, read)
}

func TestInstance_ReadOutOfBounds(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	mod, _ := r.Compile(context.Background(), wasmIdentity())
	inst, _ := mod.Instantiate(context.Background(), DefaultMaxMemory)
	defer inst.Close()

	// 1 页 = 64KB；读 64KB+1 越界。
	_, err := inst.ReadMemory(0, 65536+1)
	assert.ErrorIs(t, err, ErrMemoryOutOfBounds)
}

// TestInstance_ReadMemoryIsCopy 钉死 ReadMemory 的**隔离性**：返回的切片是副本，
// 调用方修改它不得污染 wasm 内存（退役前的 InProcessRuntime 是 make+copy 语义，
// wazero 的 Memory.Read 返回 write-through view，必须显式 copy 才能保住隔离）。
func TestInstance_ReadMemoryIsCopy(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	mod, _ := r.Compile(context.Background(), wasmIdentity())
	inst, _ := mod.Instantiate(context.Background(), DefaultMaxMemory)
	defer inst.Close()

	err := inst.WriteMemory(0, []byte("original"))
	require.NoError(t, err)

	read, err := inst.ReadMemory(0, 8)
	require.NoError(t, err)

	// 修改返回值，再读一次，必须还是原文。
	read[0] = 'X'
	again, err := inst.ReadMemory(0, 8)
	require.NoError(t, err)
	assert.Equal(t, []byte("original"), again, "ReadMemory 返回值必须是副本，改它不得污染 wasm 内存")
}

// TestInstance_WriteMemoryGrows 钉死 WriteMemory 的 grow 语义：写超过当前内存
// 大小的数据，应当 grow（接口契约「Grows memory if needed」），而非报越界。
func TestInstance_WriteMemoryGrows(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	mod, _ := r.Compile(context.Background(), wasmIdentity())
	inst, _ := mod.Instantiate(context.Background(), DefaultMaxMemory)
	defer inst.Close()

	// wasmIdentity 声明 1 页（64KB）。写 128KB 应触发 grow 并成功。
	big := make([]byte, 128*1024)
	err := inst.WriteMemory(0, big)
	assert.NoError(t, err, "写超过当前页的数据应 grow 而非报越界（接口契约）")

	read, err := inst.ReadMemory(0, uint32(len(big)))
	require.NoError(t, err)
	assert.Equal(t, big, read)
}

// TestInstance_MemoryLimitExceeded 钉死 grow 超上限的报错：runtime 上限 64KB，
// 写 128KB 需要 2 页，grow 被 WithMemoryLimitPages 拒绝 → ErrMemoryLimitExceeded。
func TestInstance_MemoryLimitExceeded(t *testing.T) {
	t.Parallel()
	r := NewWazeroRuntime(context.Background(), 65536) // 1 页 = 64KB 上限
	defer r.Close(context.Background())
	mod, _ := r.Compile(context.Background(), wasmIdentity())
	inst, _ := mod.Instantiate(context.Background(), 65536)
	defer inst.Close()

	big := make([]byte, 128*1024) // 需要 2 页，超 1 页上限
	err := inst.WriteMemory(0, big)
	assert.ErrorIs(t, err, ErrMemoryLimitExceeded)
}

func TestInstance_Call_NotExported(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	mod, _ := r.Compile(context.Background(), wasmReturnZero())
	inst, _ := mod.Instantiate(context.Background(), DefaultMaxMemory)
	defer inst.Close()

	_, err := inst.Call(context.Background(), "nonexistent_fn")
	assert.ErrorIs(t, err, ErrFunctionNotExported)
}

func TestInstance_Close(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	mod, _ := r.Compile(context.Background(), wasmIdentity())
	inst, _ := mod.Instantiate(context.Background(), DefaultMaxMemory)

	err := inst.Close()
	require.NoError(t, err)
}

// ─── WASMSandbox.Run tests ──────────────────────────────────────

func TestSandbox_Run_Identity(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	sb := NewSandbox(r, Config{})

	input := []byte("hello wasm")
	output, err := sb.Run(context.Background(), wasmIdentity(), input)
	require.NoError(t, err)
	assert.Equal(t, input, output)
}

func TestSandbox_Run_EmptyInput(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	sb := NewSandbox(r, Config{})

	output, err := sb.Run(context.Background(), wasmIdentity(), []byte{})
	require.NoError(t, err)
	assert.Equal(t, []byte{}, output)
}

func TestSandbox_Run_ModuleNotFound(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	sb := NewSandbox(r, Config{})

	_, err := sb.Run(context.Background(), []byte("not wasm"), []byte("x"))
	assert.ErrorIs(t, err, ErrModuleNotFound)
}

func TestSandbox_Run_Timeout(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	sb := NewSandbox(r, Config{MaxExecutionTime: 1 * time.Millisecond})

	// wasmInfiniteLoop: 无限循环模块，触发超时。
	_, err := sb.Run(context.Background(), wasmInfiniteLoop(), []byte("x"))
	assert.Error(t, err) // ErrTimeout 或 ctx deadline
}

func TestSandbox_Run_ParentContextCancel(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	sb := NewSandbox(r, Config{MaxExecutionTime: 30 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := sb.Run(ctx, wasmInfiniteLoop(), []byte("x"))
	assert.Error(t, err) // ctx.Err() 或 ErrTimeout
}

// ─── WASMSandbox config tests ───────────────────────────────────

func TestSandbox_Config(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	sb := NewSandbox(r, Config{MaxMemoryBytes: 32 << 20, MaxExecutionTime: 10 * time.Second})
	cfg := sb.Config()
	assert.Equal(t, 32<<20, cfg.MaxMemoryBytes)
	assert.Equal(t, 10*time.Second, cfg.MaxExecutionTime)
}

func TestSandbox_CompileReuse(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	sb := NewSandbox(r, Config{})

	mod, err := sb.Compile(context.Background(), wasmIdentity())
	require.NoError(t, err)
	defer mod.Close(context.Background())

	inst1, err := sb.InstantiateModule(context.Background(), mod)
	require.NoError(t, err)
	inst2, err := sb.InstantiateModule(context.Background(), mod)
	require.NoError(t, err)

	err = inst1.WriteMemory(0, []byte("inst1"))
	require.NoError(t, err)
	err = inst2.WriteMemory(0, []byte("inst2"))
	require.NoError(t, err)

	r1, _ := inst1.ReadMemory(0, 5)
	r2, _ := inst2.ReadMemory(0, 5)
	assert.Equal(t, []byte("inst1"), r1)
	assert.Equal(t, []byte("inst2"), r2)

	inst1.Close()
	inst2.Close()
}

func TestSandbox_CallWithTimeout(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	sb := NewSandbox(r, Config{MaxExecutionTime: 5 * time.Second})
	mod, _ := sb.Compile(context.Background(), wasmReturnZero())
	inst, _ := sb.InstantiateModule(context.Background(), mod)
	defer inst.Close()

	results, err := sb.CallWithTimeout(context.Background(), inst, "run")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, uint64(0), results[0])
}

// ─── StrategyPluginSession tests ─────────────────────────────────

func TestStrategyPluginSession_InitializeAndGenerate(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	sb := NewSandbox(r, Config{})

	session, err := sb.NewStrategyPluginSession(context.Background(), wasmStrategy())
	require.NoError(t, err)
	defer session.Close()

	err = session.Initialize(context.Background(), []byte(`{"lookback":20}`))
	require.NoError(t, err)

	signals, err := session.GenerateSignals(context.Background(), []byte(`[{"symbol":"AAPL","close":150}]`))
	require.NoError(t, err)
	assert.NotEmpty(t, signals)
}

// ─── Concurrency tests ──────────────────────────────────────────

func TestSandbox_ConcurrentRuns(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	defer r.Close(context.Background())
	sb := NewSandbox(r, Config{})

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := sb.Run(context.Background(), wasmIdentity(), []byte("concurrent"))
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent run failed: %v", err)
	}
}

func TestSandbox_Close(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	sb := NewSandbox(r, Config{})
	err := sb.Close(context.Background())
	require.NoError(t, err)
}

// wasmInfiniteLoop 是一个死循环模块（带 1 页内存，供 Run 写入 input），
// 用于超时/取消测试。
func wasmInfiniteLoop() []byte {
	m := &wasmModule{
		types:  []wasmFuncType{{}},
		funcs:  []uint32{0},
		memory: &wasmMemory{min: 1},
		export: []wasmExport{
			{name: "memory", kind: 2, idx: 0},
			{name: "run", kind: 0, idx: 0},
		},
		codes: [][]byte{
			{opLoop, 0x40, opBr, 0x00, opEnd, opEnd}, // loop $L; br 0; end; end
		},
	}
	return m.encode()
}
