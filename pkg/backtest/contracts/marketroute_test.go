// D7 · 市场 → 内核 路由表验收腿。
//
// 四条主腿 + 三条支撑：
//  1. 正证据：配置里 CN→go / US→nautilus，ResolveRunTarget 按市场给出内核；
//  2. 反证腿（核心）：未登记市场 → ErrMarketNotRegistered，且错误点名市场 +
//     已登记集合 + 「no silent fallback」——**绝不**回退 DefaultKernel；
//  3. 破坏腿（删配置）：空路由表（= kernel_routing.markets 被删）→ 带市场的
//     请求一律被拒，而**不带市场**的存量请求行为不变（"" 透传）；
//  4. 归一腿：大小写/空白不一致不得把一条配好的映射表现成「未注册」。
//
// 支撑腿：构造期 fail-fast（空内核名 / 空市场名 / 归一后冲突路由）、nil 路由表
// 拒绝、market+kernel 双写不一致拒绝。
//
// 破坏验证（开发期实测）：把 ResolveKernel 的 `!ok` 分支改成
// `return DefaultKernel, nil`（回退 go），腿 2 与腿 3 立刻红（已还原）。

package contracts

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMarketRouter(t *testing.T, markets map[string]string) *MarketRouter {
	t.Helper()
	router, err := NewMarketRouter(markets)
	require.NoError(t, err)
	return router
}

// 腿 1（正证据）：按市场解析内核。
func TestMarketRouter_ResolvesConfiguredMarkets(t *testing.T) {
	router := newTestMarketRouter(t, map[string]string{
		"CN": DefaultKernel,
		"US": "nautilus",
	})

	assert.Equal(t, []string{"CN", "US"}, router.Markets())

	got, err := router.ResolveKernel("US")
	require.NoError(t, err)
	assert.Equal(t, "nautilus", got)

	got, err = router.ResolveKernel("CN")
	require.NoError(t, err)
	assert.Equal(t, DefaultKernel, got)
}

// 腿 2（反证腿 · 核心）：未登记市场显式拒绝，零回退。
func TestMarketRouter_UnknownMarketFailsClosedNoFallback(t *testing.T) {
	router := newTestMarketRouter(t, map[string]string{"CN": DefaultKernel, "US": "nautilus"})

	got, err := router.ResolveKernel("HK")
	require.Error(t, err, "未登记市场必须报错")
	assert.True(t, errors.Is(err, ErrMarketNotRegistered), "必须是哨兵错误，调用方要 errors.Is")
	assert.Equal(t, "", got, "被拒时不得返回任何内核名（连默认值也不行）")

	msg := err.Error()
	assert.Contains(t, msg, "HK", "错误必须点名被拒的市场")
	assert.Contains(t, msg, "CN", "错误必须给出已登记市场集合")
	assert.Contains(t, msg, "US")
	assert.Contains(t, msg, "no silent fallback", "错误必须说明是拒绝而非回退")
	assert.Contains(t, msg, DefaultKernel, "错误必须点明它没有回退到哪个内核")
}

// 腿 3（破坏腿 · 删配置）：空表 = kernel_routing.markets 缺失。
func TestMarketRouter_DeletedConfigRefusesMarketsButKeepsLegacyRequests(t *testing.T) {
	router := newTestMarketRouter(t, nil)

	_, err := router.ResolveKernel("US")
	require.Error(t, err, "删掉映射配置后，带市场的请求必须 fail-closed")
	assert.True(t, errors.Is(err, ErrMarketNotRegistered))
	assert.Contains(t, err.Error(), "(none)", "已登记集合为空时必须显式呈现为 (none)")

	// 存量语义不变：不带市场的请求照旧透传（空 = 走 EffectiveKernel 的默认解析）。
	target, err := ResolveRunTarget(router, "", "")
	require.NoError(t, err)
	assert.Equal(t, "", target, "不带市场/内核 → 空串，默认值由 EffectiveKernel 解析，不在此回填")

	target, err = ResolveRunTarget(router, "", "nautilus")
	require.NoError(t, err)
	assert.Equal(t, "nautilus", target, "显式内核的存量请求不得被市场路由干扰")
}

// 支撑腿：nil 路由表（装配方忘了接线）一样 fail-closed，不 panic。
func TestMarketRouter_NilRouterFailsClosed(t *testing.T) {
	var router *MarketRouter
	_, err := ResolveRunTarget(router, "US", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMarketNotRegistered))
	assert.Contains(t, err.Error(), "no silent fallback")
	assert.Nil(t, router.Markets(), "nil 路由表的 Markets() 是空，不是崩溃")
}

// 腿 4（归一腿）：大小写/空白不得把配好的映射表现成未注册。
func TestMarketRouter_NormalisesCaseAndWhitespace(t *testing.T) {
	// viper 读配置会把键小写；请求里写的是大写。两侧都要归一。
	router := newTestMarketRouter(t, map[string]string{"us": "nautilus", "Cn": DefaultKernel})

	for _, in := range []string{"US", "us", " Us ", "\tus\n"} {
		got, err := router.ResolveKernel(in)
		require.NoError(t, err, "输入 %q 应归一后命中", in)
		assert.Equal(t, "nautilus", got)
	}
	assert.Equal(t, []string{"CN", "US"}, router.Markets(), "注册表里的键也归一大写")
}

// 支撑腿：ResolveRunTarget 四种组合。
func TestResolveRunTarget_Combinations(t *testing.T) {
	router := newTestMarketRouter(t, map[string]string{"CN": DefaultKernel, "US": "nautilus"})

	cases := []struct {
		name    string
		market  string
		kernel  string
		want    string
		wantErr string
	}{
		{"both empty → default resolved later", "", "", "", ""},
		{"kernel only → passthrough", "", "nautilus", "nautilus", ""},
		{"market only → routed", "US", "", "nautilus", ""},
		{"market only, CN → go", "CN", "", DefaultKernel, ""},
		{"market + agreeing kernel", "US", "nautilus", "nautilus", ""},
		{"market + disagreeing kernel → refuse", "US", DefaultKernel, "", "refusing to pick one"},
		{"unknown market → refuse", "HK", "", "", "not registered"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveRunTarget(router, tc.market, tc.kernel)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Equal(t, "", got, "被拒时不得给出内核名")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// 支撑腿：构造期 fail-fast。
func TestNewMarketRouter_RejectsMalformedTables(t *testing.T) {
	cases := []struct {
		name    string
		markets map[string]string
		wantMsg string
	}{
		{"blank kernel name", map[string]string{"US": "  "}, "blank kernel name"},
		{"blank market key", map[string]string{" ": "nautilus"}, "market name is blank"},
		{
			"conflicting routes after normalisation",
			map[string]string{"US": "nautilus", "us": DefaultKernel},
			"conflicting routes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, err := NewMarketRouter(tc.markets)
			require.Error(t, err, "畸形配置必须在装配期炸，不能带病对外服务")
			assert.Nil(t, router)
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

// 支撑腿：同名重复（归一后同值）不算冲突——幂等的配置写法应当放行。
func TestNewMarketRouter_IdenticalDuplicateIsIdempotent(t *testing.T) {
	router := newTestMarketRouter(t, map[string]string{"US": "nautilus", "us": "nautilus"})
	got, err := router.ResolveKernel("us")
	require.NoError(t, err)
	assert.Equal(t, "nautilus", got)
}

// 边界：路由表只负责「市场 → 内核名」，**不**负责内核是否已注册。
// 那条 fail-closed 在 EngineRegistry.Resolve（单一实现），此处刻意不重复。
func TestMarketRouter_DoesNotValidateKernelRegistration(t *testing.T) {
	router := newTestMarketRouter(t, map[string]string{"US": "not_a_real_kernel"})
	got, err := router.ResolveKernel("US")
	require.NoError(t, err, "内核注册校验不在这层——见 EngineRegistry.Resolve")
	assert.Equal(t, "not_a_real_kernel", got)
}
