package wasm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Test helpers ────────────────────────────────────────────────

// echoPlugin reads input from memory and writes it back as output.
// Simulates a WASM module that echoes its input.
func echoPlugin(inst Instance, name string, args []uint64) ([]uint64, error) {
	if len(args) < 2 {
		return nil, errors.New("need 2 args")
	}
	ptr := uint32(args[0])
	length := uint32(args[1])
	data, err := inst.ReadMemory(ptr, length)
	if err != nil {
		return nil, err
	}
	// Write output at a new offset (after the input).
	outPtr := uint32(4096)
	if err := inst.WriteMemory(outPtr, data); err != nil {
		return nil, err
	}
	return []uint64{PackPtrLen(outPtr, uint32(len(data)))}, nil
}

// upperPlugin reads input and returns it uppercased (simulates processing).
func upperPlugin(inst Instance, name string, args []uint64) ([]uint64, error) {
	if len(args) < 2 {
		return nil, errors.New("need 2 args")
	}
	ptr := uint32(args[0])
	length := uint32(args[1])
	data, err := inst.ReadMemory(ptr, length)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	for i, b := range data {
		if b >= 'a' && b <= 'z' {
			out[i] = b - 32
		} else {
			out[i] = b
		}
	}
	outPtr := uint32(4096)
	if err := inst.WriteMemory(outPtr, out); err != nil {
		return nil, err
	}
	return []uint64{PackPtrLen(outPtr, uint32(len(out)))}, nil
}

// slowPlugin sleeps for a configurable duration to test timeouts.
func slowPlugin(delay time.Duration) PluginHandler {
	return func(inst Instance, name string, args []uint64) ([]uint64, error) {
		time.Sleep(delay)
		return echoPlugin(inst, name, args)
	}
}

// errorPlugin always returns an error.
func errorPlugin(inst Instance, name string, args []uint64) ([]uint64, error) {
	return nil, errors.New("plugin error")
}

// strategyPlugin simulates the initialize/generate_signals protocol.
// It dispatches based on the function name: "initialize" returns 0
// (success), "generate_signals" echoes the input as the "signals".
func strategyPlugin(inst Instance, name string, args []uint64) ([]uint64, error) {
	switch name {
	case "initialize":
		return []uint64{0}, nil
	case "generate_signals":
		return echoPlugin(inst, name, args)
	default:
		return nil, errors.New("unknown function: " + name)
	}
}

// newTestRuntime creates an InProcessRuntime with the echo plugin registered.
func newTestRuntime() *InProcessRuntime {
	r := NewInProcessRuntime()
	r.RegisterPlugin("echo", echoPlugin, []string{"run"})
	r.RegisterPlugin("upper", upperPlugin, []string{"run"})
	r.RegisterPlugin("error", errorPlugin, []string{"run"})
	r.RegisterPlugin("strategy", strategyPlugin, []string{"initialize", "generate_signals"})
	return r
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
		MaxMemoryBytes:  128 << 20,
		MaxExecutionTime: 60 * time.Second,
	}.withDefaults()
	assert.Equal(t, 128<<20, c.MaxMemoryBytes)
	assert.Equal(t, 60*time.Second, c.MaxExecutionTime)
}

// ─── InProcessRuntime tests ──────────────────────────────────────

func TestInProcessRuntime_Compile(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	mod, err := r.Compile(context.Background(), []byte("echo"))
	require.NoError(t, err)
	assert.NotNil(t, mod)
	assert.Contains(t, mod.Exports(), "run")
}

func TestInProcessRuntime_Compile_NotFound(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	_, err := r.Compile(context.Background(), []byte("nonexistent"))
	assert.ErrorIs(t, err, ErrModuleNotFound)
}

func TestInProcessRuntime_Close(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	err := r.Close(context.Background())
	require.NoError(t, err)
	// After close, Compile should fail.
	_, err = r.Compile(context.Background(), []byte("echo"))
	assert.Error(t, err)
}

// ─── Instance memory tests ──────────────────────────────────────

func TestInstance_WriteReadMemory(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	mod, err := r.Compile(context.Background(), []byte("echo"))
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

func TestInstance_MemoryGrows(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	mod, _ := r.Compile(context.Background(), []byte("echo"))
	inst, _ := mod.Instantiate(context.Background(), DefaultMaxMemory)
	defer inst.Close()

	// Initially memory is small.
	initialSize := inst.MemorySize()
	// Write beyond initial size.
	big := make([]byte, 8192)
	err := inst.WriteMemory(0, big)
	require.NoError(t, err)
	assert.Greater(t, inst.MemorySize(), initialSize)
	assert.GreaterOrEqual(t, inst.MemorySize(), uint32(8192))
}

func TestInstance_MemoryLimitExceeded(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	mod, _ := r.Compile(context.Background(), []byte("echo"))
	inst, _ := mod.Instantiate(context.Background(), 1024) // 1KB limit
	defer inst.Close()

	big := make([]byte, 2048) // exceeds 1KB
	err := inst.WriteMemory(0, big)
	assert.ErrorIs(t, err, ErrMemoryLimitExceeded)
}

func TestInstance_ReadOutOfBounds(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	mod, _ := r.Compile(context.Background(), []byte("echo"))
	inst, _ := mod.Instantiate(context.Background(), DefaultMaxMemory)
	defer inst.Close()

	_, err := inst.ReadMemory(0, 100) // nothing written yet
	assert.ErrorIs(t, err, ErrMemoryOutOfBounds)
}

func TestInstance_Call_NotExported(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	mod, _ := r.Compile(context.Background(), []byte("echo"))
	inst, _ := mod.Instantiate(context.Background(), DefaultMaxMemory)
	defer inst.Close()

	_, err := inst.Call(context.Background(), "nonexistent_fn")
	assert.ErrorIs(t, err, ErrFunctionNotExported)
}

func TestInstance_Close(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	mod, _ := r.Compile(context.Background(), []byte("echo"))
	inst, _ := mod.Instantiate(context.Background(), DefaultMaxMemory)

	err := inst.Close()
	require.NoError(t, err)
	// After close, operations should fail.
	err = inst.WriteMemory(0, []byte("x"))
	assert.Error(t, err)
}

// ─── WASMSandbox.Run tests ──────────────────────────────────────

func TestSandbox_Run_Echo(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	sb := NewSandbox(r, Config{})

	input := []byte("hello wasm")
	output, err := sb.Run(context.Background(), []byte("echo"), input)
	require.NoError(t, err)
	assert.Equal(t, input, output)
}

func TestSandbox_Run_Upper(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	sb := NewSandbox(r, Config{})

	output, err := sb.Run(context.Background(), []byte("upper"), []byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, []byte("HELLO"), output)
}

func TestSandbox_Run_EmptyInput(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	sb := NewSandbox(r, Config{})

	output, err := sb.Run(context.Background(), []byte("echo"), []byte{})
	require.NoError(t, err)
	assert.Equal(t, []byte{}, output)
}

func TestSandbox_Run_ModuleNotFound(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	sb := NewSandbox(r, Config{})

	_, err := sb.Run(context.Background(), []byte("nonexistent"), []byte("x"))
	assert.ErrorIs(t, err, ErrModuleNotFound)
}

func TestSandbox_Run_PluginError(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	sb := NewSandbox(r, Config{})

	_, err := sb.Run(context.Background(), []byte("error"), []byte("x"))
	assert.Error(t, err)
}

func TestSandbox_Run_Timeout(t *testing.T) {
	t.Parallel()
	r := NewInProcessRuntime()
	r.RegisterPlugin("slow", slowPlugin(5*time.Second), []string{"run"})
	sb := NewSandbox(r, Config{MaxExecutionTime: 100 * time.Millisecond})

	_, err := sb.Run(context.Background(), []byte("slow"), []byte("x"))
	assert.ErrorIs(t, err, ErrTimeout)
}

func TestSandbox_Run_ParentContextCancel(t *testing.T) {
	t.Parallel()
	r := NewInProcessRuntime()
	r.RegisterPlugin("slow", slowPlugin(5*time.Second), []string{"run"})
	sb := NewSandbox(r, Config{MaxExecutionTime: 30 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := sb.Run(ctx, []byte("slow"), []byte("x"))
	assert.Error(t, err) // either ctx.Err() or ErrTimeout
}

// ─── WASMSandbox config tests ───────────────────────────────────

func TestSandbox_Config(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	sb := NewSandbox(r, Config{MaxMemoryBytes: 32 << 20, MaxExecutionTime: 10 * time.Second})
	cfg := sb.Config()
	assert.Equal(t, 32<<20, cfg.MaxMemoryBytes)
	assert.Equal(t, 10*time.Second, cfg.MaxExecutionTime)
}

func TestSandbox_CompileReuse(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	sb := NewSandbox(r, Config{})

	// Compile once, instantiate many times.
	mod, err := sb.Compile(context.Background(), []byte("echo"))
	require.NoError(t, err)
	defer mod.Close(context.Background())

	inst1, err := sb.InstantiateModule(context.Background(), mod)
	require.NoError(t, err)
	inst2, err := sb.InstantiateModule(context.Background(), mod)
	require.NoError(t, err)

	// Each instance has independent memory.
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
	sb := NewSandbox(r, Config{MaxExecutionTime: 50 * time.Millisecond})
	mod, _ := sb.Compile(context.Background(), []byte("echo"))
	inst, _ := sb.InstantiateModule(context.Background(), mod)
	defer inst.Close()

	err := inst.WriteMemory(0, []byte("test"))
	require.NoError(t, err)

	results, err := sb.CallWithTimeout(context.Background(), inst, "run", 0, 4)
	require.NoError(t, err)
	require.Len(t, results, 1)

	ptr, length := UnpackPtrLen(results[0])
	out, err := inst.ReadMemory(ptr, length)
	require.NoError(t, err)
	assert.Equal(t, []byte("test"), out)
}

// ─── StrategyPluginSession tests ─────────────────────────────────

func TestStrategyPluginSession_InitializeAndGenerate(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	sb := NewSandbox(r, Config{})

	session, err := sb.NewStrategyPluginSession(context.Background(), []byte("strategy"))
	require.NoError(t, err)
	defer session.Close()

	// Initialize with params.
	err = session.Initialize(context.Background(), []byte(`{"lookback":20}`))
	require.NoError(t, err)

	// Generate signals from bars.
	signals, err := session.GenerateSignals(context.Background(), []byte(`[{"symbol":"AAPL","close":150}]`))
	require.NoError(t, err)
	assert.NotEmpty(t, signals)
}

// ─── Concurrency tests ──────────────────────────────────────────

func TestSandbox_ConcurrentRuns(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	sb := NewSandbox(r, Config{})

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			input := []byte("concurrent")
			_, err := sb.Run(context.Background(), []byte("echo"), input)
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent run failed: %v", err)
	}
}

func TestInstance_ConcurrentMemoryAccess(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	mod, _ := r.Compile(context.Background(), []byte("echo"))
	inst, _ := mod.Instantiate(context.Background(), DefaultMaxMemory)
	defer inst.Close()

	// Each goroutine writes to a different offset.
	const n = 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			offset := uint32(i * 256)
			err := inst.WriteMemory(offset, []byte{byte(i)})
			if err != nil {
				t.Errorf("write at %d: %v", offset, err)
			}
		}(i)
	}
	wg.Wait()

	// Verify all writes.
	for i := 0; i < n; i++ {
		offset := uint32(i * 256)
		data, err := inst.ReadMemory(offset, 1)
		require.NoError(t, err)
		assert.Equal(t, byte(i), data[0])
	}
}

// ─── Close tests ────────────────────────────────────────────────

func TestSandbox_Close(t *testing.T) {
	t.Parallel()
	r := newTestRuntime()
	sb := NewSandbox(r, Config{})
	err := sb.Close(context.Background())
	require.NoError(t, err)
}
