package main

import (
	"github.com/ruoxizhnya/quant-trading/internal/httpserver"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// CORS 中间件已收口到 internal/httpserver（P0-4）：cmd/analysis 与
// cmd/data 此前各有一份复制粘贴的版本，都硬编码 `*`。装配见
// setup.go 的 buildRouter。

type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitorInfo
	rate     int
	window   time.Duration
}

type visitorInfo struct {
	count    int
	lastSeen time.Time
}

func newRateLimiter(rate int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*visitorInfo),
		rate:     rate,
		window:   window,
	}
	go rl.cleanup()
	return rl
}

// rateLimitExemptPaths 是**永远不许限流**的请求路径。
//
// 入选判据不是「这个端点重不重要」，而是更窄的一条：
// **限流它，会不会让一个正常客户端分不清 429 与 401？**
//
//   - `/health`、`/api/health` —— 就绪探针（一直是豁免的）。
//   - `/api/auth/status` —— SPA 的「鉴权姿势探针」。前端必须在**手上没有任何
//     凭据**时先问它「这个部署要不要凭据、首个管理员窗口还开着吗」，才决定
//     显示登录页还是「创建首个管理员」页。
//
// 为什么 `/api/auth/status` 必须豁免：前端对探针失败是 **fail closed**
// （拿不到答复就当成未登录，见 `web/src/stores/auth.ts` 的 `probe`）。于是
// 一个 429 会被读成「你没登录」⇒ `authGuard` 把**整个 SPA** 弹到登录页。
// 实测：连打 130 次 `:8085/api/auth/status` → **200×93 / 429×37**；全量
// playwright 因此红了 127 条，其中 66 条是「页面根本没渲染出来」的超时。
// 也就是说：**一个便宜的、无副作用的、本来就公开的探针，被限流的收益远小于
// 它造成的误判代价**（1 个 429 毁掉整站，而不是毁掉一个组件）。
//
// ⚠️ `/api/auth/login` 与 `/api/auth/refresh` **必须继续限流** —— 那是口令
// 爆破的第一道闸。**别把 `/api/auth` 前缀整个放开**；本表是逐字相等匹配，
// 新加一条必须是有意识的动作，护栏见 rate_limit_exempt_test.go。
var rateLimitExemptPaths = []string{
	"/health",
	"/api/health",
	"/api/auth/status",
}

// isRateLimitExempt 逐字相等匹配（不做前缀匹配 —— 前缀匹配会让
// `/api/auth/status/../login` 这类写法有机会绕过限流）。
func isRateLimitExempt(path string) bool {
	for _, p := range rateLimitExemptPaths {
		if p == path {
			return true
		}
	}
	return false
}

func (rl *rateLimiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if isRateLimitExempt(c.Request.URL.Path) {
			c.Next()
			return
		}
		ip := c.ClientIP()
		if !rl.allow(ip) {
			httpserver.Fail(c, http.StatusTooManyRequests, "rate limit exceeded")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	now := time.Now()
	if !exists || now.Sub(v.lastSeen) > rl.window {
		rl.visitors[ip] = &visitorInfo{count: 1, lastSeen: now}
		return true
	}
	v.count++
	v.lastSeen = now
	return v.count <= rl.rate
}

func (rl *rateLimiter) cleanup() {
	for {
		time.Sleep(time.Minute)
		rl.mu.Lock()
		now := time.Now()
		for ip, v := range rl.visitors {
			if now.Sub(v.lastSeen) > rl.window {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}
