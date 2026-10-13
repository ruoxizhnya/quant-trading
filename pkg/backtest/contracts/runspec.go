package contracts

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// K9（RunSpec 冻结）：内核路由字段（Kernel / EngineParams）的默认语义与
// fail-closed 校验。落点在 contracts 的理由与 validity.go 相同 —— 叶子包，
// API 层（cmd/analysis）与编排层（job / batch / walkforward）都能引用，
// 不引入 import 环，校验口径只有一处。

// DefaultKernel 是未指定 Kernel 时的默认内核（FR-1.1：现有行为不变）。
const DefaultKernel = "go"

// engineParamsByKernel 登记每个内核「认识」的引擎参数名。
//
// **未登记 = 该内核不接受任何引擎特有参数**（fail-closed 的另一面）：
// 请求里出现未知名一律拒绝，绝不静默忽略（FR-4.2）。目前 Go 内核没有引擎
// 特有参数，空集是刻意的 —— 新内核（K13 nautilus 等）接入时在自己的 runner
// 落地任务里登记自己的参数名，别提前为想象中的参数开门。
var engineParamsByKernel = map[string][]string{
	DefaultKernel: {},
}

// KnownEngineParams 返回该内核已登记的引擎参数名（排序后的副本）。
// 未登记的内核返回空切片 —— 与「不接受任何引擎特有参数」同义。
func KnownEngineParams(kernel string) []string {
	names := append([]string(nil), engineParamsByKernel[kernel]...)
	sort.Strings(names)
	return names
}

// EffectiveKernel 返回本次运行实际生效的内核名：显式指定则原样透传
// （路由与未知内核裁定是 K10 的事），空值回落 DefaultKernel。
//
// 默认值**不**写回 r.Kernel：BacktestRequest 会被序列化（job 落库等），
// 回填会让存量请求的 JSON 多出 `"kernel":"go"`，破坏「存量请求序列化行为
// 不变」的契约腿。
func (r BacktestRequest) EffectiveKernel() string {
	if r.Kernel == "" {
		return DefaultKernel
	}
	return r.Kernel
}

// ValidateRunSpec 校验 RunSpec 的内核路由字段，返回非 nil 即请求非法
// （API 层映射 400）。当前拒绝项：engine_params 里的未知名、signals 行的
// 非法值（K15）。
//
// 为什么是「拒绝而非忽略」：静默吞掉一个拼错的参数名，调用方会以为参数
// 生效了并拿这次结果下结论 —— 与 FR-1.3「不得静默回退 go 内核」同一条
// fail-closed 原则，只是发生在参数维度。signals 同理：一行 weight=1.5 的
// 脏数据如果被钳到 1.0，双跑对齐报告里就多了一个「查无实据」的差异。
func (r BacktestRequest) ValidateRunSpec() error {
	if err := r.validateSignals(); err != nil {
		return err
	}

	if len(r.EngineParams) == 0 {
		return nil
	}

	known := KnownEngineParams(r.EffectiveKernel())
	knownSet := make(map[string]struct{}, len(known))
	for _, k := range known {
		knownSet[k] = struct{}{}
	}

	var unknown []string
	for k := range r.EngineParams {
		if _, ok := knownSet[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)

	knownDesc := "(none)"
	if len(known) > 0 {
		knownDesc = strings.Join(known, ", ")
	}
	return fmt.Errorf(
		"engine_params contains unknown key(s) [%s] for kernel %q: unknown engine params are rejected, not ignored (known: %s)",
		strings.Join(unknown, ", "), r.EffectiveKernel(), knownDesc)
}

// validateSignals 校验可选的目标权重信号流（K15）：symbol 非空、weight 为
// [0,1] 内的有限小数、as_of 为 ISO 日期。任一违例整条请求拒绝（400），
// 不钳制、不跳过 —— 与侧车 runspec._parse_signals 逐字同口径，两侧校验
// 不一致会让同一个请求「Go 收、NT 拒」或反之，双跑从入口就不可比。
func (r BacktestRequest) validateSignals() error {
	if len(r.Signals) == 0 {
		return nil
	}
	for i, sig := range r.Signals {
		if strings.TrimSpace(sig.Symbol) == "" {
			return fmt.Errorf("signals[%d].symbol must be a non-blank string", i)
		}
		if math.IsNaN(sig.Weight) || math.IsInf(sig.Weight, 0) || sig.Weight < 0 || sig.Weight > 1 {
			return fmt.Errorf(
				"signals[%d].weight must be a finite fraction in [0, 1], got %v", i, sig.Weight)
		}
		if _, err := time.Parse("2006-01-02", sig.AsOf); err != nil {
			return fmt.Errorf(
				"signals[%d].as_of must be an ISO date (YYYY-MM-DD), got %q", i, sig.AsOf)
		}
	}
	return nil
}
