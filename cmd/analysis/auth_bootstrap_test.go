package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// P0-4: 鉴权默认关闭 —— 无 JWT_SECRET 时服务静默进入 open-access，
// 任何能访问网络的人都能触发回测与下单。本测试锁定「fail-closed」契约：
// 没有密钥就拒绝启动，除非显式豁免且只监听 loopback。

func TestDecideAuthStartup_SecretPresent_Enabled(t *testing.T) {
	d := decideAuthStartup("s3cret", false, "0.0.0.0")
	assert.False(t, d.refuse)
	assert.Equal(t, []byte("s3cret"), d.secret)
	assert.False(t, d.insecure)
}

func TestDecideAuthStartup_NoSecret_Refuses(t *testing.T) {
	d := decideAuthStartup("", false, "127.0.0.1")
	assert.True(t, d.refuse, "无密钥且未豁免时必须拒绝启动")
	assert.Contains(t, d.reason, "JWT_SECRET", "拒绝原因必须给出可操作的修复指引")
	assert.Empty(t, d.secret)
}

func TestDecideAuthStartup_WhitespaceSecret_Refuses(t *testing.T) {
	// 只有空白字符的密钥等于没有密钥（YAML 里 `jwt_secret: "  "` 很容易出现）。
	d := decideAuthStartup("   ", false, "127.0.0.1")
	assert.True(t, d.refuse)
}

func TestDecideAuthStartup_InsecureOnLoopback_Allowed(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "127.0.1.5", "::1"} {
		d := decideAuthStartup("", true, host)
		assert.False(t, d.refuse, "loopback 上的显式豁免应放行: %s", host)
		assert.True(t, d.insecure)
		assert.Empty(t, d.secret)
	}
}

func TestDecideAuthStartup_InsecureOffLoopback_Refuses(t *testing.T) {
	for _, host := range []string{"0.0.0.0", "192.168.1.10", "", "[::]"} {
		d := decideAuthStartup("", true, host)
		assert.True(t, d.refuse, "非 loopback 监听不允许 open-access: %q", host)
		assert.Contains(t, d.reason, host)
	}
}

func TestDecideAuthStartup_SecretWinsOverInsecure(t *testing.T) {
	// 配了密钥就是开了鉴权，不该再被标成 insecure。
	d := decideAuthStartup("s3cret", true, "0.0.0.0")
	assert.False(t, d.refuse)
	assert.False(t, d.insecure)
}

func TestIsLoopbackHost(t *testing.T) {
	assert.True(t, isLoopbackHost("127.0.0.1"))
	assert.True(t, isLoopbackHost("127.255.255.254"))
	assert.True(t, isLoopbackHost("localhost"))
	assert.True(t, isLoopbackHost("::1"))
	assert.True(t, isLoopbackHost("[::1]"))

	assert.False(t, isLoopbackHost("0.0.0.0"))
	assert.False(t, isLoopbackHost("10.0.0.5"))
	assert.False(t, isLoopbackHost("192.168.0.1"))
	assert.False(t, isLoopbackHost(""))
	assert.False(t, isLoopbackHost("localhost.example.com"))
}
