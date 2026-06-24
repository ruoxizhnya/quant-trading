// Package wasm implements a WASM-based sandbox for strategy plugin
// execution (P2-27, AR-003, ADR-007 Phase 3 / ADR-019).
//
// The sandbox provides memory isolation (each strategy runs in its own
// WASM instance), resource limits (max memory, max execution time),
// and a clean input/output protocol via WASM linear memory.
//
// # Runtime abstraction
//
// The concrete WASM execution is abstracted behind the Runtime
// interface. The production implementation will use wazero (a pure-Go
// WebAssembly runtime); until wazero is added to go.mod, an
// InProcessRuntime fallback is provided. The fallback simulates the
// WASM memory model in-process (no real isolation) but exercises the
// same API surface, so callers can develop and test against the
// sandbox today and swap in wazero later without code changes.
//
// # Strategy plugin protocol
//
// A WASM strategy plugin must export two functions:
//
//	initialize(params_ptr: i32, params_len: i32) → i32
//	generate_signals(bars_ptr: i32, bars_len: i32) → i64
//
// The host writes JSON-serialized input to the instance's linear
// memory, calls the function with the (ptr, len) pair, and reads the
// JSON-serialized output back from the returned (ptr, len) pair
// (packed into a single i64: high 32 bits = ptr, low 32 bits = len).
//
// # Usage
//
//	sb := wasm.NewSandbox(wasm.NewInProcessRuntime(), wasm.Config{
//	    MaxMemoryBytes:  64 << 20, // 64 MB
//	    MaxExecutionTime: 30 * time.Second,
//	})
//	output, err := sb.Run(ctx, wasmBytes, inputJSON)
package wasm

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// DefaultMaxMemory is the default per-instance memory limit (64 MB).
const DefaultMaxMemory = 64 << 20

// DefaultMaxExecutionTime is the default per-call wall-clock timeout.
const DefaultMaxExecutionTime = 30 * time.Second

// ErrTimeout is returned when a sandboxed call exceeds the time limit.
var ErrTimeout = errors.New("wasm: execution exceeded time limit")

// ErrMemoryLimitExceeded is returned when an instance tries to grow
// memory beyond the configured limit.
var ErrMemoryLimitExceeded = errors.New("wasm: memory limit exceeded")

// ErrModuleNotFound is returned when Compile receives bytes that the
// runtime cannot interpret as a known module.
var ErrModuleNotFound = errors.New("wasm: module not found")

// ErrFunctionNotExported is returned when Call targets a function the
// module does not export.
var ErrFunctionNotExported = errors.New("wasm: function not exported")

// ErrMemoryOutOfBounds is returned when a ReadMemory/WriteMemory offset
// is outside the instance's current memory.
var ErrMemoryOutOfBounds = errors.New("wasm: memory access out of bounds")

// Config controls sandbox resource limits.
type Config struct {
	// MaxMemoryBytes is the maximum linear memory per instance.
	// Default: 64 MB.
	MaxMemoryBytes int `json:"max_memory_bytes"`
	// MaxExecutionTime is the wall-clock timeout for a single Run call.
	// Default: 30s.
	MaxExecutionTime time.Duration `json:"max_execution_time"`
}

// withDefaults returns a copy of c with zero values replaced by defaults.
func (c Config) withDefaults() Config {
	out := c
	if out.MaxMemoryBytes <= 0 {
		out.MaxMemoryBytes = DefaultMaxMemory
	}
	if out.MaxExecutionTime <= 0 {
		out.MaxExecutionTime = DefaultMaxExecutionTime
	}
	return out
}

// ─── Runtime interface (wazero-backed or in-process) ──────────────

// Runtime is the abstract WASM runtime. Implementations include
// InProcessRuntime (fallback) and a future WazeroRuntime.
type Runtime interface {
	// Compile parses wasmBytes and returns a compiled module.
	Compile(ctx context.Context, wasmBytes []byte) (CompiledModule, error)
	// Close releases all runtime-level resources.
	Close(ctx context.Context) error
}

// CompiledModule is a parsed/compiled WASM module, ready to be
// instantiated one or more times.
type CompiledModule interface {
	// Instantiate creates a new instance with its own isolated memory.
	// memoryLimitBytes caps the instance's linear memory growth.
	Instantiate(ctx context.Context, memoryLimitBytes int) (Instance, error)
	// Exports returns the list of exported function names.
	Exports() []string
	// Close releases the compiled module.
	Close(ctx context.Context) error
}

// Instance is a running WASM instance with isolated linear memory.
type Instance interface {
	// WriteMemory writes data to the instance's linear memory at offset.
	// Grows memory if needed (up to the instance limit).
	WriteMemory(offset uint32, data []byte) error
	// ReadMemory reads length bytes from the instance's memory at offset.
	ReadMemory(offset uint32, length uint32) ([]byte, error)
	// MemorySize returns the current memory size in bytes.
	MemorySize() uint32
	// Call invokes an exported function by name with the given arguments.
	Call(ctx context.Context, name string, args ...uint64) ([]uint64, error)
	// Close releases the instance and its memory.
	Close() error
}

// ─── PackPtrLen helpers ──────────────────────────────────────────
//
// WASM functions that return a byte slice pack the (pointer, length)
// pair into a single i64. We use little-endian encoding: the low 32
// bits are the pointer, the high 32 bits are the length.

// PackPtrLen packs a (pointer, length) pair into a single uint64.
func PackPtrLen(ptr uint32, length uint32) uint64 {
	var buf [8]byte
	binary.LittleEndian.PutUint32(buf[:4], ptr)
	binary.LittleEndian.PutUint32(buf[4:], length)
	return binary.LittleEndian.Uint64(buf[:])
}

// UnpackPtrLen splits a packed uint64 into (pointer, length).
func UnpackPtrLen(packed uint64) (ptr uint32, length uint32) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], packed)
	ptr = binary.LittleEndian.Uint32(buf[:4])
	length = binary.LittleEndian.Uint32(buf[4:])
	return ptr, length
}

// ─── InProcessRuntime (fallback) ─────────────────────────────────
//
// InProcessRuntime simulates the WASM execution model in-process.
// It does NOT provide real isolation — the plugin code runs in the
// host goroutine. It exists so callers can develop and test the
// sandbox API before wazero is integrated.
//
// "WASM bytes" are interpreted as a module name; the runtime looks
// up a registered PluginHandler to execute calls. This lets tests
// inject deterministic plugin implementations.

// PluginHandler is the function signature for in-process plugins.
// It receives the instance (for memory access), the called function
// name, and the call arguments, and returns the call results.
// The name parameter lets a single handler dispatch multiple exports
// (e.g. "initialize" vs "generate_signals").
type PluginHandler func(inst Instance, name string, args []uint64) ([]uint64, error)

// InProcessRuntime is a Runtime that executes plugins in-process.
// It is safe for concurrent use.
type InProcessRuntime struct {
	mu       sync.RWMutex
	plugins  map[string]PluginHandler
	exports  map[string][]string // moduleName → exported function names
	closed   bool
}

// NewInProcessRuntime creates an empty InProcessRuntime.
// Use RegisterPlugin to add modules.
func NewInProcessRuntime() *InProcessRuntime {
	return &InProcessRuntime{
		plugins: make(map[string]PluginHandler),
		exports: make(map[string][]string),
	}
}

// RegisterPlugin registers an in-process plugin under the given module
// name. The wasmBytes passed to Compile must be []byte(moduleName).
// exportNames lists the function names the module "exports" (returned
// by CompiledModule.Exports).
func (r *InProcessRuntime) RegisterPlugin(moduleName string, handler PluginHandler, exportNames []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[moduleName] = handler
	r.exports[moduleName] = exportNames
}

// Compile implements Runtime. wasmBytes is interpreted as a UTF-8
// module name that must have been previously registered via
// RegisterPlugin.
func (r *InProcessRuntime) Compile(_ context.Context, wasmBytes []byte) (CompiledModule, error) {
	r.mu.RLock()
	if r.closed {
		r.mu.RUnlock()
		return nil, errors.New("wasm: runtime closed")
	}
	r.mu.RUnlock()

	name := string(wasmBytes)
	r.mu.RLock()
	handler, ok := r.plugins[name]
	exports := r.exports[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrModuleNotFound, name)
	}
	return &inProcessModule{name: name, handler: handler, exports: exports}, nil
}

// Close implements Runtime.
func (r *InProcessRuntime) Close(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.plugins = nil
	r.exports = nil
	return nil
}

// inProcessModule implements CompiledModule for the in-process runtime.
type inProcessModule struct {
	name    string
	handler PluginHandler
	exports []string
}

func (m *inProcessModule) Instantiate(_ context.Context, memoryLimitBytes int) (Instance, error) {
	if memoryLimitBytes <= 0 {
		memoryLimitBytes = DefaultMaxMemory
	}
	return &inProcessInstance{
		memory:      make([]byte, 0, 4096),
		memLimit:    memoryLimitBytes,
		handler:     m.handler,
		exports:     m.exports,
	}, nil
}

func (m *inProcessModule) Exports() []string {
	out := make([]string, len(m.exports))
	copy(out, m.exports)
	return out
}

func (m *inProcessModule) Close(_ context.Context) error { return nil }

// inProcessInstance implements Instance for the in-process runtime.
type inProcessInstance struct {
	mu      sync.Mutex
	memory  []byte
	memLimit int
	handler  PluginHandler
	exports  []string
	closed   bool
}

func (inst *inProcessInstance) ensureSize(needed uint32) error {
	if int(needed) > inst.memLimit {
		return fmt.Errorf("%w: need %d bytes, limit %d", ErrMemoryLimitExceeded, needed, inst.memLimit)
	}
	if int(needed) > len(inst.memory) {
		// Grow in powers of two for efficiency.
		newSize := uint32(len(inst.memory))
		if newSize == 0 {
			newSize = 4096
		}
		for newSize < needed {
			newSize *= 2
		}
		if int(newSize) > inst.memLimit {
			newSize = uint32(inst.memLimit)
		}
		grown := make([]byte, newSize)
		copy(grown, inst.memory)
		inst.memory = grown
	}
	return nil
}

func (inst *inProcessInstance) WriteMemory(offset uint32, data []byte) error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return errors.New("wasm: instance closed")
	}
	end := offset + uint32(len(data))
	if err := inst.ensureSize(end); err != nil {
		return err
	}
	copy(inst.memory[offset:end], data)
	return nil
}

func (inst *inProcessInstance) ReadMemory(offset uint32, length uint32) ([]byte, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		return nil, errors.New("wasm: instance closed")
	}
	end := offset + length
	if int(end) > len(inst.memory) {
		return nil, fmt.Errorf("%w: read [%d:%d] from %d-byte memory", ErrMemoryOutOfBounds, offset, end, len(inst.memory))
	}
	out := make([]byte, length)
	copy(out, inst.memory[offset:end])
	return out, nil
}

func (inst *inProcessInstance) MemorySize() uint32 {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return uint32(len(inst.memory))
}

func (inst *inProcessInstance) Call(ctx context.Context, name string, args ...uint64) ([]uint64, error) {
	inst.mu.Lock()
	if inst.closed {
		inst.mu.Unlock()
		return nil, errors.New("wasm: instance closed")
	}
	// Verify the function is exported.
	exported := false
	for _, e := range inst.exports {
		if e == name {
			exported = true
			break
		}
	}
	handler := inst.handler
	inst.mu.Unlock()

	if !exported {
		return nil, fmt.Errorf("%w: %s", ErrFunctionNotExported, name)
	}
	if handler == nil {
		return nil, fmt.Errorf("%w: %s (no handler)", ErrFunctionNotExported, name)
	}
	// The handler runs in-process; the context timeout still applies.
	// We run it in a goroutine so we can enforce the deadline.
	type result struct {
		vals []uint64
		err  error
	}
	done := make(chan result, 1)
	go func() {
		// Re-acquire the instance pointer (not the lock) for the handler.
		vals, err := handler(inst, name, args)
		done <- result{vals, err}
	}()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %s: %v", ErrTimeout, name, ctx.Err())
	case r := <-done:
		return r.vals, r.err
	}
}

func (inst *inProcessInstance) Close() error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.closed = true
	inst.memory = nil
	return nil
}

// ─── WASMSandbox ─────────────────────────────────────────────────

// WASMSandbox is the high-level sandbox for running strategy plugins.
// It wraps a Runtime with resource-limit enforcement.
type WASMSandbox struct {
	runtime Runtime
	config  Config
	log     zerolog.Logger
}

// NewSandbox creates a WASMSandbox with the given runtime and config.
// If config fields are zero, defaults are applied.
func NewSandbox(runtime Runtime, config Config) *WASMSandbox {
	return &WASMSandbox{
		runtime: runtime,
		config:  config.withDefaults(),
		log:     zerolog.Nop(),
	}
}

// SetLogger installs a logger for sandbox diagnostics.
func (s *WASMSandbox) SetLogger(l zerolog.Logger) {
	s.log = l
}

// Run compiles wasmBytes, instantiates an isolated instance, writes
// input to memory, calls the "run" exported function, and reads the
// output back. The "run" function is expected to take (input_ptr,
// input_len) as two i32 args and return a packed (output_ptr,
// output_len) i64.
//
// This is the convenience entry point for one-shot strategy execution.
// For multi-step protocols (initialize → generate_signals), use
// Compile + Instantiate directly.
func (s *WASMSandbox) Run(ctx context.Context, wasmBytes []byte, input []byte) ([]byte, error) {
	cfg := s.config

	// Enforce the wall-clock timeout.
	runCtx, cancel := context.WithTimeout(ctx, cfg.MaxExecutionTime)
	defer cancel()

	// Compile the module.
	module, err := s.runtime.Compile(runCtx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("wasm: compile: %w", err)
	}
	defer module.Close(runCtx)

	// Instantiate with memory limit.
	inst, err := module.Instantiate(runCtx, cfg.MaxMemoryBytes)
	if err != nil {
		return nil, fmt.Errorf("wasm: instantiate: %w", err)
	}
	defer inst.Close()

	// Write input to memory at offset 0.
	if err := inst.WriteMemory(0, input); err != nil {
		return nil, fmt.Errorf("wasm: write input: %w", err)
	}

	// Call "run(input_ptr=0, input_len=len(input))".
	results, err := inst.Call(runCtx, "run", 0, uint64(len(input)))
	if err != nil {
		return nil, fmt.Errorf("wasm: call run: %w", err)
	}
	if len(results) == 0 {
		return nil, errors.New("wasm: run returned no result")
	}

	// Unpack the (ptr, len) pair.
	ptr, length := UnpackPtrLen(results[0])
	if length == 0 {
		return []byte{}, nil
	}

	// Read output from memory.
	output, err := inst.ReadMemory(ptr, length)
	if err != nil {
		return nil, fmt.Errorf("wasm: read output: %w", err)
	}
	return output, nil
}

// Compile is a thin wrapper around the underlying runtime's Compile,
// exposed so callers can reuse a compiled module across multiple
// instantiations (e.g. for the initialize → generate_signals protocol).
func (s *WASMSandbox) Compile(ctx context.Context, wasmBytes []byte) (CompiledModule, error) {
	return s.runtime.Compile(ctx, wasmBytes)
}

// InstantiateModule creates a new isolated instance from a compiled
// module, applying the sandbox's memory limit.
func (s *WASMSandbox) InstantiateModule(ctx context.Context, module CompiledModule) (Instance, error) {
	return module.Instantiate(ctx, s.config.MaxMemoryBytes)
}

// CallWithTimeout calls an exported function on the instance, applying
// the sandbox's execution-time limit. This is the preferred way to
// invoke instance functions; it ensures the timeout is enforced even
// if the caller's context has no deadline.
func (s *WASMSandbox) CallWithTimeout(ctx context.Context, inst Instance, name string, args ...uint64) ([]uint64, error) {
	callCtx, cancel := context.WithTimeout(ctx, s.config.MaxExecutionTime)
	defer cancel()
	return inst.Call(callCtx, name, args...)
}

// Config returns the sandbox's effective configuration (with defaults applied).
func (s *WASMSandbox) Config() Config {
	return s.config
}

// Close releases the underlying runtime.
func (s *WASMSandbox) Close(ctx context.Context) error {
	return s.runtime.Close(ctx)
}

// ─── StrategyPlugin helpers ──────────────────────────────────────
//
// These helpers implement the standard strategy-plugin protocol on top
// of the raw Instance API. They handle the JSON serialization and
// memory packing so callers don't have to.

// StrategyPluginSession wraps an Instance with the standard
// initialize/generate_signals protocol.
type StrategyPluginSession struct {
	inst    Instance
	sandbox *WASMSandbox
}

// NewStrategyPluginSession compiles wasmBytes and instantiates a
// session for the standard strategy plugin protocol.
func (s *WASMSandbox) NewStrategyPluginSession(ctx context.Context, wasmBytes []byte) (*StrategyPluginSession, error) {
	module, err := s.Compile(ctx, wasmBytes)
	if err != nil {
		return nil, err
	}
	inst, err := s.InstantiateModule(ctx, module)
	if err != nil {
		module.Close(ctx)
		return nil, err
	}
	return &StrategyPluginSession{inst: inst, sandbox: s}, nil
}

// Initialize calls the module's "initialize" export with JSON params.
// Returns nil on success (the module returns 0).
func (sp *StrategyPluginSession) Initialize(ctx context.Context, params []byte) error {
	if err := sp.inst.WriteMemory(0, params); err != nil {
		return fmt.Errorf("write params: %w", err)
	}
	results, err := sp.sandbox.CallWithTimeout(ctx, sp.inst, "initialize", 0, uint64(len(params)))
	if err != nil {
		return err
	}
	if len(results) == 0 || results[0] != 0 {
		return fmt.Errorf("initialize returned non-zero: %v", results)
	}
	return nil
}

// GenerateSignals calls the module's "generate_signals" export with
// JSON-serialized bars and returns the JSON-serialized signals.
func (sp *StrategyPluginSession) GenerateSignals(ctx context.Context, bars []byte) ([]byte, error) {
	if err := sp.inst.WriteMemory(0, bars); err != nil {
		return nil, fmt.Errorf("write bars: %w", err)
	}
	results, err := sp.sandbox.CallWithTimeout(ctx, sp.inst, "generate_signals", 0, uint64(len(bars)))
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, errors.New("generate_signals returned no result")
	}
	ptr, length := UnpackPtrLen(results[0])
	if length == 0 {
		return []byte{}, nil
	}
	return sp.inst.ReadMemory(ptr, length)
}

// Close releases the underlying instance.
func (sp *StrategyPluginSession) Close() error {
	return sp.inst.Close()
}
