// K10 · job 异步路径的内核路由验收腿。
//
// 三条主腿 + 两条支撑：
//  1. 正证据：注册 fake → kernel=fake 的 job 走 fake，结果经**同一条
//     落库路径**（UpdateJobCompleted + BacktestResponse JSON）持久化，
//     且带 fake 的 marker（证明不是 go 跑的）；
//  2. 反证腿：kernel=nautilus（未注册）→ job failed + error_msg 点名
//     内核，且 go runner **零调用**、库内无 result —— 证明没有静默回退；
//  3. 回归腿：不带 kernel → 默认 go runner 执行，请求里 Kernel 字段
//     为空的语义不变（EffectiveKernel 在路由时才解析默认值）。
//  4. 落库形状：kernel 经 params JSONB 保留键往返（CreateJob 写、
//     StartJob 读），不带 kernel 的存量 job params 不多出键。
//
// 破坏验证（开发期实测）：把 EngineRegistry.Resolve 改成「未知名回退
// go」，腿 2 的「go runner 零调用 / 无 result」断言立刻红（已还原）。

package job

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kernelMarkerRunner 带 marker 的 EngineRunner stub —— 产物响应里写
// marker，落库后可证明「这次是哪个内核跑的」。
type kernelMarkerRunner struct {
	marker string

	mu    sync.Mutex
	reqs  []contracts.BacktestRequest
	calls int
}

func (k *kernelMarkerRunner) RunBacktest(ctx context.Context, req contracts.BacktestRequest) (*contracts.BacktestResponse, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls++
	k.reqs = append(k.reqs, req)
	return &contracts.BacktestResponse{
		ID:       "bt-" + k.marker,
		Status:   contracts.StatusCompleted,
		Strategy: "from-" + k.marker,
	}, nil
}

func (k *kernelMarkerRunner) callCount() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.calls
}

// newRoutingJobService 构造一个注册了 go + fake 两个内核的 JobService。
func newRoutingJobService(t *testing.T) (*JobService, *mockJobStore, *kernelMarkerRunner, *kernelMarkerRunner) {
	t.Helper()
	store := newMockJobStore()
	goRunner := &kernelMarkerRunner{marker: "go"}
	fakeRunner := &kernelMarkerRunner{marker: "fake"}
	registry := contracts.NewEngineRegistry()
	registry.Register(contracts.DefaultKernel, goRunner)
	registry.Register("fake", fakeRunner)
	svc := NewJobService(store, registry, zerolog.Nop())
	return svc, store, goRunner, fakeRunner
}

// 腿 1（正证据）：kernel=fake → 走 fake 并落库同 schema。
func TestJobService_KernelFakeRunsFakeAndPersistsSameSchema(t *testing.T) {
	svc, store, goRunner, fakeRunner := newRoutingJobService(t)

	job, err := svc.CreateJob(context.Background(), CreateJobRequest{
		StrategyID:   "momentum",
		Universe:     "600000.SH,600001.SH",
		StartDate:    "2024-01-02",
		EndDate:      "2024-02-02",
		Kernel:       "fake",
		EngineParams: map[string]any{"seed": 7},
	})
	require.NoError(t, err)

	got := waitJobSettled(t, store, job.ID)
	assert.Equal(t, "completed", got["status"], "fake 内核的 job 必须正常 completed")
	assert.Equal(t, 1, fakeRunner.callCount(), "fake runner 必须被调用一次")
	assert.Equal(t, 0, goRunner.callCount(), "路由到 fake 时 go runner 不得被调用")

	// 路由键与引擎参数原样到达 runner（K9 透传 + K10 路由）。
	require.Len(t, fakeRunner.reqs, 1)
	assert.Equal(t, "fake", fakeRunner.reqs[0].Kernel)
	assert.Equal(t, map[string]any{"seed": float64(7)}, fakeRunner.reqs[0].EngineParams)

	// 落库同 schema：经 UpdateJobCompleted 持久化的 result JSON 带 fake
	// marker（BacktestResponse 同一形状），且路由键经 params 保留键往返。
	resultJSON, _ := got["result"].([]byte)
	require.NotEmpty(t, resultJSON, "fake 运行结果必须落库")
	var persisted contracts.BacktestResponse
	require.NoError(t, json.Unmarshal(resultJSON, &persisted))
	assert.Equal(t, "bt-fake", persisted.ID, "落库的必须是 fake 的产物")
	assert.Equal(t, contracts.StatusCompleted, persisted.Status)

	var params map[string]any
	require.NoError(t, json.Unmarshal(got["params"].([]byte), &params))
	assert.Equal(t, "fake", params["kernel"], "kernel 必须经 params 保留键落库（StartJob 读回用）")
}

// 腿 2（反证腿）：kernel=nautilus（未注册）→ failed + error_msg 点名内核，
// go runner 零调用、库内无 result —— 没有静默回退。
func TestJobService_UnknownKernelFailsClosedNoGoProduct(t *testing.T) {
	svc, store, goRunner, fakeRunner := newRoutingJobService(t)

	job, err := svc.CreateJob(context.Background(), CreateJobRequest{
		StrategyID: "momentum",
		Universe:   "600000.SH,600001.SH",
		StartDate:  "2024-01-02",
		EndDate:    "2024-02-02",
		Kernel:     "nautilus",
	})
	require.NoError(t, err)

	got := waitJobSettled(t, store, job.ID)
	assert.Equal(t, "failed", got["status"], "未注册内核的 job 必须 failed")

	errMsg, _ := got["error_msg"].(string)
	assert.Contains(t, errMsg, "nautilus", "error_msg 必须点名被拒的内核名")
	assert.Contains(t, errMsg, "not registered", "error_msg 必须说明是未注册拒绝")

	// 反证本体：两个 runner 都零调用，库里没有 result —— 若路由回退 go，
	// 这里会多出一个 go 产物（status=completed + result=bt-go）。
	// （断言 Empty 而非 Nil：mockJobStore 的 cloneJobMap 会把 nil []byte
	// 复制成非 nil 空切片，nil 性不是这里要钉的东西，空才是。）
	assert.Equal(t, 0, goRunner.callCount(), "未注册内核不得回退到 go runner")
	assert.Equal(t, 0, fakeRunner.callCount(), "fake runner 与本腿无关")
	assert.Empty(t, got["result"], "库内不得留下任何内核产物")
}

// 腿 3（回归腿）：不带 kernel → 默认 go runner 执行，且请求里 Kernel
// 字段保持空（默认值不在请求层回填，EffectiveKernel 路由时才解析）。
func TestJobService_NoKernelUsesDefaultGoRunner(t *testing.T) {
	svc, store, goRunner, fakeRunner := newRoutingJobService(t)

	job, err := svc.CreateJob(context.Background(), CreateJobRequest{
		StrategyID: "momentum",
		Universe:   "600000.SH,600001.SH",
		StartDate:  "2024-01-02",
		EndDate:    "2024-02-02",
	})
	require.NoError(t, err)

	got := waitJobSettled(t, store, job.ID)
	assert.Equal(t, "completed", got["status"])
	assert.Equal(t, 1, goRunner.callCount(), "不带 kernel 必须走默认 go runner")
	assert.Equal(t, 0, fakeRunner.callCount())

	require.Len(t, goRunner.reqs, 1)
	assert.Empty(t, goRunner.reqs[0].Kernel, "默认值不得回填进请求字段（序列化不变式）")

	// 存量形状：不带 kernel 的 job，params 里不得多出保留键。
	var params map[string]any
	require.NoError(t, json.Unmarshal(got["params"].([]byte), &params))
	_, hasKernel := params["kernel"]
	assert.False(t, hasKernel, "不带 kernel 的存量 job params 不得多出键（逐字节不变）")
}

// 支撑腿：注册表是硬依赖 —— nil registry 构造即 panic（装配错误
// fail-fast，而不是所有 job 执行期才崩）。
func TestJobService_NilRegistryPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewJobService(newMockJobStore(), nil, zerolog.Nop())
	}, "nil registry 必须 fail-fast")
}
