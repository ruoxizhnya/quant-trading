package main

// 本文件守的是「限流豁免名单」这条缝。
//
// 为什么值得单独立一个护栏文件：这个名单的失效形态**没有本地症状**。
// `/api/auth/status` 少豁免一条，本机单跑任何 Go 测试都是全绿的 —— 它只在
// 「前端被限流」时才发作，而症状是**整站被弹到登录页**（不是某个接口报错）。
// 2026-09-25 实测：连打 130 次 `:8085/api/auth/status` → 200×93 / 429×37；
// 全量 playwright 因此红了 127 条，其中 66 条是「页面根本没渲染出来」的超时。
//
// 所以三腿都要，缺一条这个护栏就是假的：
//   - 行为腿（TestRateLimit_AuthStatusProbeIsNeverThrottled）：证明豁免真的
//     生效。**必须带对照腿** —— 否则限流器整个没接线时，豁免那几条照样全绿
//     （「永真」是假护栏最常见的形状）。
//   - 窄度腿（同上的 login 那条 + TestIsRateLimitExempt_IsExactMatchNotPrefix）：
//     证明豁免是**窄的** —— 没有把 `/api/auth` 前缀整个放开（那会关掉口令
//     爆破的第一道闸），也没有把逐字匹配改成前缀匹配。
//   - 名单腿（TestRateLimitExemptPaths_AreExactlyTheDocumentedThree）：任何
//     增删都必须是有意识的动作。

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// rateLimitTestRouter 只装限流中间件 + 几个被探测的路径。
//
// **每个用例都新建一个**：限流器按 IP 计数，共用一个 router 会让上一个用例
// 的计数漏进下一个，于是「豁免生效」和「额度还没用完」会读出同一个结果 ——
// 那就分不清到底是什么在绿了。
func rateLimitTestRouter(rate int) *gin.Engine {
	r := gin.New()
	// ClientIP 直接读 RemoteAddr，不吃 X-Forwarded-For：httptest 的请求
	// 默认没有 XFF，但 gin 默认信任所有代理，显式关掉省得以后有人加了头就飘。
	_ = r.SetTrustedProxies(nil)
	r.Use(newRateLimiter(rate, time.Minute).middleware())

	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	for _, p := range []string{
		"/health",
		"/api/health",
		"/api/auth/status",
		"/api/auth/login",
		"/api/strategies",
	} {
		r.GET(p, ok)
		r.POST(p, ok)
	}
	return r
}

// rateLimitProbe 打 n 次同一路径，返回「状态码 → 次数」。
func rateLimitProbe(r *gin.Engine, method, path string, n int) map[int]int {
	got := map[int]int{}
	for i := 0; i < n; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		got[w.Code]++
	}
	return got
}

// TestRateLimit_AuthStatusProbeIsNeverThrottled 是本次修复的正面证据：
// 造一个「额度只有 3」的限流器，把探针打 12 次 —— **只有豁免成立才可能全 200**。
func TestRateLimit_AuthStatusProbeIsNeverThrottled(t *testing.T) {
	const (
		budget = 3
		n      = 12
	)

	cases := []struct {
		name    string
		method  string
		path    string
		wantOK  int
		want429 int
		why     string
	}{
		{
			name:    "auth/status 是 SPA 的姿势探针，一次都不许 429",
			method:  http.MethodGet,
			path:    "/api/auth/status",
			wantOK:  n,
			want429: 0,
			why:     "429 会被前端 fail closed 读成「未登录」，把整站弹到登录页",
		},
		{
			name:    "health 探针照旧豁免",
			method:  http.MethodGet,
			path:    "/health",
			wantOK:  n,
			want429: 0,
			why:     "就绪探针原本就豁免，确认这次改动没把它带坏",
		},
		{
			name:    "api/health 探针照旧豁免",
			method:  http.MethodGet,
			path:    "/api/health",
			wantOK:  n,
			want429: 0,
			why:     "同上",
		},
		{
			// 对照腿：没有它，上面三条在「限流器压根没接线」时也会全绿。
			name:    "普通业务端点照样被限流（对照组）",
			method:  http.MethodGet,
			path:    "/api/strategies",
			wantOK:  budget,
			want429: n - budget,
			why:     "证明限流器真的在工作，豁免不等于「谁都不限」",
		},
		{
			// 窄度腿：防「把 /api/auth 前缀整个放开」这个最危险的改法。
			name:    "登录端点必须继续限流（不许前缀放开）",
			method:  http.MethodPost,
			path:    "/api/auth/login",
			wantOK:  budget,
			want429: n - budget,
			why:     "口令爆破的第一道闸；放开它等于把限流关掉",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 每个用例一个全新限流器，计数互不污染。
			got := rateLimitProbe(rateLimitTestRouter(budget), tc.method, tc.path, n)
			assert.Equal(t, tc.wantOK, got[http.StatusOK],
				"%s %s：期望 %d 次 200（%s）；实测 %v", tc.method, tc.path, tc.wantOK, tc.why, got)
			assert.Equal(t, tc.want429, got[http.StatusTooManyRequests],
				"%s %s：期望 %d 次 429（%s）；实测 %v", tc.method, tc.path, tc.want429, tc.why, got)
		})
	}
}

// TestIsRateLimitExempt_IsExactMatchNotPrefix 守的是**匹配机制**而不是名单内容。
//
// 名单腿挡不住「把 `==` 改成 `strings.HasPrefix`」这类看着无害的重构，
// 而那种改动正是最危险的：豁免 `/api/auth/status` 时顺手写前缀，就会连
// `/api/auth/login` 一起放开。所以这里连路径穿越写法也钉一条。
func TestIsRateLimitExempt_IsExactMatchNotPrefix(t *testing.T) {
	assert.True(t, isRateLimitExempt("/api/auth/status"), "这条必须豁免")

	for _, p := range []string{
		"/api/auth/status/../login", // 路径穿越写法
		"/api/auth/statusx",         // 前缀相同但不是它
		"/api/auth/",
		"/api/auth/login",
		"/api/auth/refresh",
		"/api/strategies",
		"",
	} {
		assert.False(t, isRateLimitExempt(p), "%q 不该被豁免（逐字相等，不前缀匹配）", p)
	}
}

// TestRateLimitExemptPaths_AreExactlyTheDocumentedThree 钉住名单本身。
//
// 这不是同义反复：`want` 是硬编码的，所以**任何**增删都会变红。行为腿只能
// 覆盖「在 router 上注册过的那几条」，这条覆盖所有改动 —— 价值最大的是挡住
// 那种「顺手」：把 /api/auth/login 或 /api/auth/refresh 加进来，悄无声息地
// 关掉口令爆破的第一道闸。
func TestRateLimitExemptPaths_AreExactlyTheDocumentedThree(t *testing.T) {
	want := []string{"/health", "/api/health", "/api/auth/status"}
	assert.Equal(t, want, rateLimitExemptPaths,
		"限流豁免名单变了。加一条之前先回答：限流它会不会让一个正常客户端"+
			"分不清 429 与 401？如果理由只是「这个端点很重要」，那不构成豁免。")
}
