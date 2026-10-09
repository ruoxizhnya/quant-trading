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
// interface. The production implementation is WazeroRuntime (wazero, a
// pure-Go WebAssembly runtime), which provides **real isolation** — the
// wasm bytecode runs in a constrained linear memory and can only call
// host functions that were explicitly imported (the host API whitelist).
// (The pre-K6 InProcessRuntime fallback that simulated the WASM model
// in-process has been retired.)
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
//	sb := wasm.NewSandbox(wasm.NewWazeroRuntime(ctx, 64<<20), wasm.Config{
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

// ─── Runtime interface (wazero-backed) ───────────────────────────

// Runtime is the abstract WASM runtime. The production implementation
// is WazeroRuntime (see wazero_runtime.go).
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
	// memoryLimitBytes caps the instance's linear memory growth（K6 起该上限
	// 由 runtime 级统一约束，此参数校验请求 ≤ runtime 上限，见 WazeroRuntime）。
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
