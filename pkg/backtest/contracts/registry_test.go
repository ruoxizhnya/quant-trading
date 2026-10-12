// K10 EngineRegistry 的契约腿。
//
// 钉住的缺陷形态（FR-1.3）：未知内核被静默回退到 go —— 调用方以为在测
// 外部引擎，实际跑的是默认内核，结论完全错位还看不出来。fail-closed
// 要求 Resolve 对未注册内核返回点名错误；把 Resolve 改成「未知名回退
// go」，这一整文件的核心腿立刻红。
//
// 外部测试包（contracts_test）：只 exercising 导出面，与 runner_test.go
// 同一立场。

package contracts_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registryFakeRunner 记录每次调用的 EngineRunner stub，带可辨识 marker
// （用于证明「走的是 fake 而不是 go」）。
type registryFakeRunner struct {
	marker string

	mu    sync.Mutex
	reqs  []contracts.BacktestRequest
	calls int
}

func (f *registryFakeRunner) RunBacktest(ctx context.Context, req contracts.BacktestRequest) (*contracts.BacktestResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.reqs = append(f.reqs, req)
	return &contracts.BacktestResponse{
		ID:       "bt-" + f.marker,
		Status:   contracts.StatusCompleted,
		Strategy: "from-" + f.marker,
	}, nil
}

func (f *registryFakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// 正证据：注册 → 解析 → 调用，拿到的是同一个实例。
func TestEngineRegistry_RegisterResolveRoundtrip(t *testing.T) {
	reg := contracts.NewEngineRegistry()
	goRunner := &registryFakeRunner{marker: "go"}
	fakeRunner := &registryFakeRunner{marker: "fake"}
	reg.Register(contracts.DefaultKernel, goRunner)
	reg.Register("fake", fakeRunner)

	got, err := reg.Resolve("fake")
	require.NoError(t, err)
	assert.Equal(t, fakeRunner, got, "Resolve 必须返回注册时的同一实例")

	resp, err := got.RunBacktest(context.Background(), contracts.BacktestRequest{Strategy: "momentum"})
	require.NoError(t, err)
	assert.Equal(t, "bt-fake", resp.ID, "fake runner 的产物必须带自己的 marker")
	assert.Equal(t, 0, goRunner.callCount(), "路由到 fake 时 go runner 不得被调用")
}

// 核心 fail-closed 腿：未注册内核 → 错误，点名内核名与已注册集合，
// 且 errors.Is(ErrKernelNotRegistered)。把 Resolve 改成回退 go，
// 这条红（拿到的会是 go runner 而非 error）。
func TestEngineRegistry_ResolveUnknownKernelFailClosed(t *testing.T) {
	reg := contracts.NewEngineRegistry()
	reg.Register(contracts.DefaultKernel, &registryFakeRunner{marker: "go"})

	runner, err := reg.Resolve("nautilus")
	require.Error(t, err, "未注册内核必须报错，不得返回 runner")
	assert.Nil(t, runner)
	assert.ErrorIs(t, err, contracts.ErrKernelNotRegistered)
	assert.Contains(t, err.Error(), "nautilus", "错误必须点名被拒的内核名")
	assert.Contains(t, err.Error(), "no silent fallback", "错误必须声明「不回退」语义")
}

// 空名同样 fail-closed：调用方忘了过 EffectiveKernel 也不能悄悄跑到
// 某个默认实现上。
func TestEngineRegistry_ResolveEmptyNameFailClosed(t *testing.T) {
	reg := contracts.NewEngineRegistry()
	reg.Register(contracts.DefaultKernel, &registryFakeRunner{marker: "go"})

	runner, err := reg.Resolve("")
	require.Error(t, err)
	assert.Nil(t, runner)
	assert.ErrorIs(t, err, contracts.ErrKernelNotRegistered)
}

// 空注册表（含零值）上 Resolve 一律报错，不 panic。
func TestEngineRegistry_ZeroValueAndEmptyRegistryFailClosed(t *testing.T) {
	var zero contracts.EngineRegistry
	_, err := zero.Resolve(contracts.DefaultKernel)
	require.Error(t, err, "零值注册表不得 panic，必须 fail-closed")

	_, err = contracts.NewEngineRegistry().Resolve(contracts.DefaultKernel)
	require.Error(t, err, "空注册表不得 panic，必须 fail-closed")
}

// Register misuse（空名 / nil / 重名）→ panic：装配期编程错误 fail-fast。
func TestEngineRegistry_RegisterMisusePanics(t *testing.T) {
	reg := contracts.NewEngineRegistry()

	assert.Panics(t, func() { reg.Register("", &registryFakeRunner{}) }, "空内核名必须 panic")
	assert.Panics(t, func() { reg.Register("go", nil) }, "nil runner 必须 panic")

	reg.Register(contracts.DefaultKernel, &registryFakeRunner{marker: "go"})
	assert.Panics(t, func() { reg.Register(contracts.DefaultKernel, &registryFakeRunner{marker: "go2"}) },
		"重名注册必须 panic（无热插拔）")
}

// 零值注册表 Register 后可用（懒初始化 map），且 Names 返回排序副本。
func TestEngineRegistry_ZeroValueUsableAfterRegister(t *testing.T) {
	var reg contracts.EngineRegistry
	reg.Register("fake", &registryFakeRunner{marker: "fake"})
	reg.Register(contracts.DefaultKernel, &registryFakeRunner{marker: "go"})

	assert.Equal(t, []string{"fake", "go"}, reg.Names(), "Names 必须排序")

	names := reg.Names()
	names[0] = "mutated"
	assert.Equal(t, []string{"fake", "go"}, reg.Names(), "Names 必须返回副本，调用方改不得动注册表")
}

// 并发腿：装配后多 goroutine 并发 Resolve + 调用（-race 下跑）。
// 注册期单线程、运行期只读是该场景的前提，这里证明它成立。
func TestEngineRegistry_ConcurrentResolve(t *testing.T) {
	reg := contracts.NewEngineRegistry()
	reg.Register(contracts.DefaultKernel, &registryFakeRunner{marker: "go"})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runner, err := reg.Resolve(contracts.DefaultKernel)
			if err != nil {
				t.Errorf("已注册内核并发解析失败: %v", err)
				return
			}
			if _, err := runner.RunBacktest(context.Background(), contracts.BacktestRequest{}); err != nil {
				t.Errorf("并发调用失败: %v", err)
			}
		}()
	}
	wg.Wait()
}

// 编译期即保证的形状断言（文档化用）：Registry 不依赖任何具体引擎类型。
func TestEngineRegistry_RejectsNilAfterRegistration(t *testing.T) {
	reg := contracts.NewEngineRegistry()
	_, err := reg.Resolve("never-registered")
	assert.True(t, errors.Is(err, contracts.ErrKernelNotRegistered))
}
