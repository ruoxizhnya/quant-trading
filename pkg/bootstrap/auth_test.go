package bootstrap_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/ruoxizhnya/quant-trading/pkg/bootstrap"
)

// P0-4: 鉴权默认关闭 —— 无 JWT_SECRET 时服务静默进入 open-access，
// 任何能访问网络的人都能触发回测与下单。本测试锁定「fail-closed」契约：
// 没有密钥就拒绝启动，除非显式豁免且只监听 loopback（或声明了发布层回环）。

func TestDecideAuthStartup_SecretPresent_Enabled(t *testing.T) {
	d := bootstrap.DecideAuthStartup("s3cret", false, "0.0.0.0", "")
	assert.False(t, d.Refuse)
	assert.Equal(t, []byte("s3cret"), d.Secret)
	assert.False(t, d.Insecure)
}

func TestDecideAuthStartup_NoSecret_Refuses(t *testing.T) {
	d := bootstrap.DecideAuthStartup("", false, "127.0.0.1", "")
	assert.True(t, d.Refuse, "无密钥且未豁免时必须拒绝启动")
	assert.Contains(t, d.Reason, "JWT_SECRET", "拒绝原因必须给出可操作的修复指引")
	assert.Empty(t, d.Secret)
}

func TestDecideAuthStartup_WhitespaceSecret_Refuses(t *testing.T) {
	// 只有空白字符的密钥等于没有密钥（YAML 里 `jwt_secret: "  "` 很容易出现）。
	d := bootstrap.DecideAuthStartup("   ", false, "127.0.0.1", "")
	assert.True(t, d.Refuse)
}

func TestDecideAuthStartup_InsecureOnLoopback_Allowed(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "127.0.1.5", "::1"} {
		d := bootstrap.DecideAuthStartup("", true, host, "")
		assert.False(t, d.Refuse, "loopback 上的显式豁免应放行: %s", host)
		assert.True(t, d.Insecure)
		assert.Empty(t, d.Secret)
	}
}

func TestDecideAuthStartup_InsecureOffLoopback_Refuses(t *testing.T) {
	for _, host := range []string{"0.0.0.0", "192.168.1.10", "", "[::]"} {
		d := bootstrap.DecideAuthStartup("", true, host, "")
		assert.True(t, d.Refuse, "非 loopback 监听且无发布层声明时不允许 open-access: %q", host)
		assert.Contains(t, d.Reason, host)
	}
}

func TestDecideAuthStartup_SecretWinsOverInsecure(t *testing.T) {
	// 配了密钥就是开了鉴权，不该再被标成 insecure。
	d := bootstrap.DecideAuthStartup("s3cret", true, "0.0.0.0", "")
	assert.False(t, d.Refuse)
	assert.False(t, d.Insecure)
}

// ── 容器形态（2026-09-25 新增，AUD-52 的栈侧一半）──────────────────────
//
// 容器必须绑 0.0.0.0 才能被发布端口转发，所以 bootstrap.IsLoopbackHost 档不住它。
// 判据换成「operator 声明发布层限定为回环」——但**声明必须精确**，
// 写错一律按没声明处理。

func TestDecideAuthStartup_ContainerExposureDeclared_Allowed(t *testing.T) {
	d := bootstrap.DecideAuthStartup("", true, "0.0.0.0", bootstrap.InsecureExposureLoopbackPublished)
	assert.False(t, d.Refuse, "声明了发布层回环后，容器内 0.0.0.0 应放行")
	assert.True(t, d.Insecure)
	assert.Empty(t, d.Secret)
}

func TestDecideAuthStartup_ContainerExposureTypo_Refuses(t *testing.T) {
	// 反证腿：声明必须逐字匹配。拼错/大小写/近义词/布尔值一律 fail closed ——
	// 这一条掉了，等于这个开关可以靠一个笔误打开。
	for _, exposure := range []string{
		"", "loopback", "loopback-only", "published", "true", "1",
		"LOOPBACK-PUBLISHED", "Loopback-Published", "loopback_published",
	} {
		d := bootstrap.DecideAuthStartup("", true, "0.0.0.0", exposure)
		assert.True(t, d.Refuse, "exposure 声明不精确时必须拒绝: %q", exposure)
		assert.Contains(t, d.Reason, "AUTH_INSECURE_EXPOSURE",
			"拒绝原因必须告诉 operator 正确的声明名")
	}
}

func TestDecideAuthStartup_ExposureWithoutAllowInsecure_Refuses(t *testing.T) {
	// 反证腿：exposure 声明**不能**单独解锁 —— 它只是 AUTH_INSECURE 的附加条件。
	// 少了这一条，就有人能只靠 AUTH_INSECURE_EXPOSURE 打开 open-access。
	d := bootstrap.DecideAuthStartup("", false, "0.0.0.0", bootstrap.InsecureExposureLoopbackPublished)
	assert.True(t, d.Refuse, "未显式 AUTH_INSECURE=true 时，exposure 声明不得单独解锁")
}

func TestIsLoopbackHost(t *testing.T) {
	assert.True(t, bootstrap.IsLoopbackHost("127.0.0.1"))
	assert.True(t, bootstrap.IsLoopbackHost("127.255.255.254"))
	assert.True(t, bootstrap.IsLoopbackHost("localhost"))
	assert.True(t, bootstrap.IsLoopbackHost("::1"))
	assert.True(t, bootstrap.IsLoopbackHost("[::1]"))

	assert.False(t, bootstrap.IsLoopbackHost("0.0.0.0"))
	assert.False(t, bootstrap.IsLoopbackHost("10.0.0.5"))
	assert.False(t, bootstrap.IsLoopbackHost("192.168.0.1"))
	assert.False(t, bootstrap.IsLoopbackHost(""))
	assert.False(t, bootstrap.IsLoopbackHost("localhost.example.com"))
}
