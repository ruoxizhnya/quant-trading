package main

// AUD-58 的窄测试：**证明那个旋钮真的有效**。
//
// 为什么需要这一条：AUD-58 的落地方式是「给 e2e 栈显式设一个高额度」
// （`RATE_LIMIT_PER_MINUTE`，走 docker-compose.e2e.yml 覆盖层）。如果这个 env
// 覆盖根本没接上，e2e 会继续拿到 429，而**看起来**问题已经处理过了 ——
// 覆盖层里明明写着 high。那就是一个安慰剂。
//
// 四条腿，各自独立（故意写成互不依赖，因为「兜底值恰好等于配置值」会让
// 「读了配置」和「用了兜底」长得一模一样 —— 一种会假绿的形状）：
//
//	A. rateLimitPerMinute 真的是 viper 的函数，不是常量；
//	B. 仓库里 shipped 的 yaml 给出一个**真实的**限额（且不许被调高 —— 见下）；
//	C. 那条 shipped 值确实流过 rateLimitPerMinute；
//	D. env 覆盖压过 yaml（e2e 高额度姿势的唯一支点）。
//
// 另一半：**默认必须仍是一个真实的限额**。否则「把默认值调高」会成为修 AUD-58
// 的捷径 —— 那等于把限流从本地开发里静默删掉，比拿到 429 更糟（429 至少看得见）。

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 测试工作目录是 cmd/analysis，配置在仓库根的 config/ 下。
func shippedAnalysisConfig(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "config", "analysis-service.yaml"))
	require.NoError(t, err)
	return p
}

// wiredLikeLoadConfig 复刻 loadConfig 里与限流有关的那几行装配。
// 刻意不调 loadConfig 本身：它会 logger.Fatal，还会改 zerolog 的全局级别。
func wiredLikeLoadConfig(t *testing.T) *viper.Viper {
	t.Helper()
	v := viper.New()
	v.SetConfigFile(shippedAnalysisConfig(t))
	v.SetConfigType("yaml")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	require.NoError(t, v.ReadInConfig())
	return v
}

// shippedRateLimit 直接读 yaml，不经 viper 的 env 层。
func shippedRateLimit(t *testing.T) int {
	t.Helper()
	raw := viper.New()
	raw.SetConfigFile(shippedAnalysisConfig(t))
	raw.SetConfigType("yaml")
	require.NoError(t, raw.ReadInConfig())
	return raw.GetInt("rate_limit.per_minute")
}

// A. 它是 viper 的函数，不是一个常量。
// 没有这条，「读配置」与「回落到写死的 100」在测试里长得一模一样 ——
// 而后者意味着改 yaml 毫无效果。
func TestRateLimitPerMinute_IsAFunctionOfViper(t *testing.T) {
	v := viper.New()
	v.Set("rate_limit.per_minute", 42)

	assert.Equal(t, 42, rateLimitPerMinute(v),
		"没读 viper 的 rate_limit.per_minute —— 配置成了装饰")
}

// B. shipped 值必须是一个真实的限额，且不许被调高。
func TestRateLimitPerMinute_ShippedDefaultIsStillARealLimit(t *testing.T) {
	shipped := shippedRateLimit(t)

	require.Greater(t, shipped, 0,
		"config/analysis-service.yaml 必须给出一个正的限额（0 或负数等于关掉限流）")

	// 上界不是审美判据，是**反捷径**判据：AUD-58 不能靠「把默认调高」来修。
	// 真要让整套 e2e 跑得动，走 docker-compose.e2e.yml 那个显式覆盖层。
	assert.LessOrEqual(t, shipped, 600,
		"默认限额被调大了 —— 那是把限流从本地开发里静默删掉。"+
			"抬高额度必须是 e2e 专用覆盖层（docker-compose.e2e.yml），不是改默认值（AUD-58）")
}

// C. shipped 值确实流过 rateLimitPerMinute。
func TestRateLimitPerMinute_UsesShippedConfigValue(t *testing.T) {
	// 清掉覆盖，确保读到的是 yaml（viper 默认把空 env 当作「没设」）。
	t.Setenv("RATE_LIMIT_PER_MINUTE", "")

	assert.Equal(t, shippedRateLimit(t), rateLimitPerMinute(wiredLikeLoadConfig(t)),
		"没有 env 覆盖时必须用配置文件里的值")
}

// D. env 覆盖必须真的生效 —— 这是 e2e 高额度姿势的唯一支点。
// 同时这条也钉住了「**env 压过 yaml**」这个优先级（viper 的 env 层在 config 之上）。
func TestRateLimitPerMinute_EnvOverrideActuallyTakesEffect(t *testing.T) {
	t.Setenv("RATE_LIMIT_PER_MINUTE", "100000")

	v := wiredLikeLoadConfig(t)
	shipped := shippedRateLimit(t)
	require.NotEqual(t, 100000, shipped, "前提：覆盖值与 shipped 值不同，否则这条腿测不出东西")

	assert.Equal(t, 100000, rateLimitPerMinute(v),
		"RATE_LIMIT_PER_MINUTE 没接上：e2e 的「高额度」姿势就只是个安慰剂，"+
			"问题会看起来已经处理过，实际仍在 429")
}

// E. 最后一块：**覆盖层本身**必须真的把两个服务都抬起来。
//
// 上面四条腿证明的是「旋钮有效」，但 AUD-58 的修法落在
// `docker-compose.e2e.yml` 这个部署产物上 —— 如果它漏了一个服务、或者写的值
// 不比默认大，测试全绿而 e2e 依然 429。所以这里直接读那个文件。
func TestE2EComposeOverride_RaisesTheLimitForBothServices(t *testing.T) {
	composeE2E, err := filepath.Abs(filepath.Join("..", "..", "docker-compose.e2e.yml"))
	require.NoError(t, err)

	raw := viper.New()
	raw.SetConfigFile(composeE2E)
	raw.SetConfigType("yaml")
	require.NoError(t, raw.ReadInConfig(), "docker-compose.e2e.yml 必须存在且可解析")

	shipped := shippedRateLimit(t)

	for _, svc := range []string{"analysis-service", "data-service"} {
		got := raw.GetString("services." + svc + ".environment.rate_limit_per_minute")
		require.NotEmpty(t, got,
			"%s 没拿到 e2e 覆盖 —— 它会在 429 里继续红（Playwright 同时打 :8085 与 :8081）", svc)

		n, err := strconv.Atoi(got)
		require.NoError(t, err, "%s 的覆盖值必须是整数，实际 %q", svc, got)
		assert.Greater(t, n, shipped,
			"%s 的 e2e 覆盖(%d) 不比默认(%d) 大 —— 那这个覆盖层等于没写", svc, n, shipped)
	}
}
