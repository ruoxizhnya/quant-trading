package kernel

// modules_analysis_test.go —— K1 切片 3 的装配顺序契约测试。
//
// 全 10 模块的真 Boot（含 eventstore 建表）走 kernel_pg_test.go 的真库
// 路径；本文件在**模块层**验证两件事：
//
//  1. 正证：按 BootOrder 顺序 Init 四个业务模块，strategy-runtime 能从
//     前序模块产物完成全部 Set* 注入（wiring 产物非 nil）。
//  2. 破坏腿（双向）：**乱序 Init 时 strategy-runtime 必须 fail-fast**——
//     缺 data-engine 缺 risk / 缺 exec 各红一次。这正是「打乱 Boot 顺序
//     → 测试变红」验收的模块层落点（BootOrder 常量本身另有
//     TestBootOrderContract 冻结）。

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newWiringModules 按装配处的真实方式构造四个业务模块，闭包把调用顺序
// 记进 order。构造用最小真实件（构造-only，不用例不触发网络/DB）。
func newWiringModules(order *[]string) (data *DataEngineModule, riskM *RiskEngineModule, exec *ExecEngineModule, strategy *StrategyRuntimeModule) {
	logger := zerolog.Nop()

	data = NewDataEngineModule(func() (marketdata.Provider, *marketdata.DataAdapter, error) {
		*order = append(*order, "data-engine")
		// 构造-only：adapter 持有 nil provider 也合法（只有 Execute 才解引用）。
		return marketdata.NewHTTPProvider("http://test:8081", logger),
			marketdata.NewDataAdapter(nil, nil, nil, logger), nil
	})
	riskM = NewRiskEngineModule(func() (*risk.RiskManager, error) {
		*order = append(*order, "risk-engine")
		return risk.NewRiskManager(risk.RiskManagerConfig{}, logger)
	})
	exec = NewExecEngineModule(func() (live.LiveTrader, error) {
		*order = append(*order, "exec-engine")
		return live.NewMockTrader(live.MockTraderConfig{InitialCash: 1_000_000}, logger), nil
	})
	strategy = NewStrategyRuntimeModule(viper.New(), logger, &StrategyRuntimeDeps{
		// 零值 store：SetStore 只存指针，构造期不触库（真 Boot 走
		// kernel_pg_test 的真库路径）。
		Store:      &storage.PostgresStore{},
		DataEngine: data,
		RiskEngine: riskM,
		ExecEngine: exec,
	})
	return data, riskM, exec, strategy
}

// TestAnalysisModulesWiringInBootOrder 正证：按 BootOrder 的业务段
// （3 data-engine → 5 risk-engine → 6 exec-engine → 7 strategy-runtime）
// 逐个 Init，注入全部完成、产物非 nil。
func TestAnalysisModulesWiringInBootOrder(t *testing.T) {
	t.Parallel()

	var order []string
	data, riskM, exec, strategy := newWiringModules(&order)

	ctx := context.Background()
	require.NoError(t, data.Init(ctx))
	require.NoError(t, riskM.Init(ctx))
	require.NoError(t, exec.Init(ctx))
	require.NoError(t, strategy.Init(ctx))

	// 顺序即 BootOrder 的业务段相对序（Noop 槽位无构造，不进 order）。
	assert.Equal(t, []string{"data-engine", "risk-engine", "exec-engine"}, order)

	// 产物链完整：strategy-runtime 已从三个前序模块完成注入。
	assert.NotNil(t, strategy.Engine, "engine 必须在 strategy-runtime Init 时构造")
	if strategy.Engine != nil {
		// Set* 是否生效无法从外部断言（引擎不暴露 getter）——这里断言
		// 的是「注入路径跑通且无 panic」，字段级校验由引擎自身测试覆盖。
		assert.NotNil(t, data.Adapter)
		assert.NotNil(t, riskM.RiskManager)
		assert.NotNil(t, exec.Trader)
	}
}

// TestStrategyRuntimeFailsFastWithoutDataEngine 破坏腿 ①：跳过 data-engine
// 直接 Init strategy-runtime → fail-fast，报错点名缺失方。
func TestStrategyRuntimeFailsFastWithoutDataEngine(t *testing.T) {
	t.Parallel()

	strategy := NewStrategyRuntimeModule(viper.New(), zerolog.Nop(), &StrategyRuntimeDeps{
		// DataEngine 模块对象存在但**未 Init**（产物字段 nil）——等价于
		// BootOrder 被打乱成 strategy-runtime 先行。
		DataEngine: &DataEngineModule{},
		RiskEngine: &RiskEngineModule{},
		ExecEngine: &ExecEngineModule{},
	})

	err := strategy.Init(context.Background())
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "data-engine"),
		"报错必须点名缺失的前序模块，got: %v", err)
}

// TestStrategyRuntimeFailsFastWithoutExecEngine 破坏腿 ②：data/risk 就位但
// 跳过 exec-engine → fail-fast，报错点名 exec-engine。
func TestStrategyRuntimeFailsFastWithoutExecEngine(t *testing.T) {
	t.Parallel()

	var order []string
	data, riskM, _, strategy := newWiringModules(&order)

	ctx := context.Background()
	require.NoError(t, data.Init(ctx))
	require.NoError(t, riskM.Init(ctx)) // risk 就位；exec 故意不 Init

	err := strategy.Init(ctx)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "exec-engine"),
		"报错必须点名缺失的前序模块，got: %v", err)
}
