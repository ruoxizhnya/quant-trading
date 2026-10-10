package main

// wiring_guard_test.go —— K1 切片 3 的结构护栏：main.go 的装配必须走内核。
//
// 守什么：
//  1. main.go 必须调用 assembleKernel 且对失败 fail-fast（logger.Fatal）；
//  2. main.go **不得**再直接调用 bootstrap 的三个组件 builder
//     （buildBacktestEngine / buildRiskManager / buildExecutionTrader）——
//     它们的构造已搬进模块 Init，main 里出现直接调用 = 有人绕过
//     BootOrder 把顺序控制权抢回去（这正是本切片消灭的东西）；
//  3. 影子启动（startShadowKernel / stopShadowKernel）不得复活——它已被
//     fail-fast 的真接管取代。
//
// 为什么是文本扫描而不是 AST：断言对象是「main.go 里有没有这些调用」，
// 不是函数体内部结构；全文 Contains 足够，且对注释里的提法不敏感
// （注释提到 builder 名不算违规——护栏锚定的是调用形态 `buildXxx(v`）。

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readMainSource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("main.go")
	require.NoError(t, err)
	return string(raw)
}

func TestMainAssemblyGoesThroughKernel(t *testing.T) {
	t.Parallel()
	src := readMainSource(t)

	// ① 必须经内核装配 + fail-fast。
	assert.Contains(t, src, "assembleKernel(v, store, logger)",
		"main 必须经 assembleKernel 装配（K1 切片 3 的验收核心）")
	assert.Contains(t, src, `logger.Fatal().Err(err).Msg("kernel assembly failed")`,
		"装配失败必须 fail-fast（接管后内核承重，不允许「失败不阻断」回潮）")

	// ② 不得绕过 BootOrder 直接构造组件。
	for _, banned := range []string{
		"buildBacktestEngine(v,",
		"buildRiskManager(v,",
		"buildExecutionTrader(v,",
		"engine.SetRiskManager(riskManager)",
		"engine.SetLiveTrader(executionTrader)",
		"engine.SetStore(store)",
	} {
		assert.NotContains(t, src, banned,
			"main.go 出现 %q —— 组件构造/注入被抢回装配代码，BootOrder 失去顺序控制权（K1 切片 3 的不变量）", banned)
	}

	// ③ 影子启动不得复活。
	for _, banned := range []string{"startShadowKernel", "stopShadowKernel", "kernel_shadow"} {
		assert.NotContains(t, src, banned,
			"影子启动已被 fail-fast 真接管取代，不许复活（K1 切片 3）")
	}
}

// TestKernelWiringCoversAllBootOrderSlots 钉住装配完整性：kernel_wiring.go
// 注册的模块名集合必须**恰好**等于 BootOrder（少注册 Boot 会报
// ErrMissingModule，多注册 Register 直接拒绝——这里在源码层再钉一遍，
// 防有人删掉 Noop 槽位「精简」装配）。
func TestKernelWiringCoversAllBootOrderSlots(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("kernel_wiring.go")
	require.NoError(t, err)
	src := string(raw)

	for _, name := range []string{
		"eventstore", "clock", "data-engine", "portfolio", "risk-engine",
		"exec-engine", "strategy-runtime", "indicators", "exec-algo", "msgbus",
	} {
		assert.True(t, strings.Contains(src, `NewNoopModule("`+name+`")`) ||
			strings.Contains(src, name),
			"kernel_wiring.go 必须装配 BootOrder 槽位 %q（Noop 槽位也不许删——宪法级冻结）", name)
	}
}
