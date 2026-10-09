package wasm

// K6 切片 3：协议改造 generate_signals → 逐 bar on_bar(t) + finalize（ADR-029 §3.3）。
//
// 前视漏洞（⑭）：旧协议 generate_signals(bars_ptr, bars_len) 是 host 一次性写
// 入全部 bars，沙箱内代码读 bars[len-1] 就拿到未来数据。新协议改为：
//
//	initialize(params_ptr, params_len) → i32    // 保留
//	on_bar(t: i32) → i64                        // 逐 bar 推进；数据只能经
//	                                            // get_bar(t_offset ≤ 0) 取
//	finalize() → i64                            // 收尾，冲刷最终信号
//
// on_bar(t) 每次由 host 调用，推进「当前 t」；沙箱内取数只能走 get_bar（切片 2
// 已实现 t_offset>0 → trap），故前视物理不可能。finalize 返回打包 (ptr,len) 的
// 最终信号（或 pack(0,0) 表示无信号）。
//
// 状态断点续跑（ADR-029 §3.5 + K6 验收「SaveState/LoadState 续跑一致」）：
// get_state/set_state 让状态由 host 管理。BarContext.State 是状态的载体：
//   - Batch（回测）：host 内存里维护 BarContext，跑完全序列；
//   - Step（实盘）：每 bar 调 on_bar，BarContext.State 序列化持久化，重启后
//     载入继续；
//   一致性属性测试：Batch ≡ Step从零 ≡ Step从持久化恢复（ADR-028 §7 三路）。

import (
	"context"
	"errors"
	"fmt"
)

// OnBarSession 是「逐 bar on_bar(t) + finalize」协议的会话，替代旧
// StrategyPluginSession 的 generate_signals 一次性协议。
//
// 用法：
//
//	sess, _ := sandbox.NewOnBarSession(ctx, wasmBytes)
//	defer sess.Close()
//	sess.Initialize(ctx, paramsJSON)
//	bc := &BarContext{Series: series, State: map[string][]byte{}, ...}
//	for t := 0; t < len(series); t++ {
//	    bc.T = int32(t)
//	    sess.OnBar(ctx, bc)          // 内部把 bc 注入 ctx，调 on_bar(t)
//	}
//	signals, _ := sess.Finalize(ctx, bc)  // 冲刷最终信号
type OnBarSession struct {
	inst    Instance
	sandbox *WASMSandbox
}

// NewOnBarSession compiles wasmBytes and instantiates an on_bar-protocol session.
// 调用方必须先 InstantiateHostModule（见 WazeroRuntime.InstantiateHostModule）。
func (s *WASMSandbox) NewOnBarSession(ctx context.Context, wasmBytes []byte) (*OnBarSession, error) {
	module, err := s.Compile(ctx, wasmBytes)
	if err != nil {
		return nil, err
	}
	inst, err := s.InstantiateModule(ctx, module)
	if err != nil {
		module.Close(ctx)
		return nil, err
	}
	return &OnBarSession{inst: inst, sandbox: s}, nil
}

// Initialize calls the module's "initialize" export with JSON params.
// Returns nil on success (the module returns 0).
func (sp *OnBarSession) Initialize(ctx context.Context, params []byte) error {
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

// OnBar 推进一个 bar：把 bc 注入 ctx，调用 wasm 的 on_bar(t)（t = bc.T）。
//
// 关键：bc 经 context.WithValue 注入，on_bar 内的 get_bar 等 host 函数从 ctx
// 取出（见 host_api.go 的 barContextFrom）。每次调用前调用方应更新 bc.T。
func (sp *OnBarSession) OnBar(ctx context.Context, bc *BarContext) error {
	if bc == nil {
		return errors.New("wasm: OnBar requires non-nil BarContext")
	}
	ctx = withBarContext(ctx, bc)
	_, err := sp.sandbox.CallWithTimeout(ctx, sp.inst, "on_bar", uint64(uint32(bc.T)))
	return err
}

// Finalize 调用 wasm 的 finalize()，返回打包 (ptr,len) 的最终信号（可能是
// pack(0,0) 表示无信号）。bc 注入 ctx 使 finalize 内也能 get_state 冲刷状态。
func (sp *OnBarSession) Finalize(ctx context.Context, bc *BarContext) ([]byte, error) {
	if bc == nil {
		return nil, errors.New("wasm: Finalize requires non-nil BarContext")
	}
	ctx = withBarContext(ctx, bc)
	results, err := sp.sandbox.CallWithTimeout(ctx, sp.inst, "finalize")
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, errors.New("finalize returned no result")
	}
	ptr, length := UnpackPtrLen(results[0])
	if length == 0 {
		return []byte{}, nil
	}
	return sp.inst.ReadMemory(ptr, length)
}

// Close releases the underlying instance.
func (sp *OnBarSession) Close() error {
	return sp.inst.Close()
}
