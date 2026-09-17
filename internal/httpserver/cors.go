// Package httpserver 放两个 cmd（analysis / data）共用的 HTTP 层构件。
//
// 存在理由：此前 cmd/analysis 与 cmd/data 各有一份复制粘贴的
// corsMiddleware，都硬编码 `Access-Control-Allow-Origin: *`（P0-4）。
// 修一处、漏一处正是这类重复代码的宿命，因此收成一份。
package httpserver

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// CORS 返回按白名单回显 Origin 的中间件（P0-4）。
//
// 规则：
//   - allowed 为空 → 不信任任何跨源，不回显 ACAO（fail closed）
//   - allowed 含 "*" → 显式通配，回显 "*"（要对外开放才写，不再是默认）
//   - 其它 → 仅当请求 Origin 精确命中白名单才回显该 Origin
//
// 没有 Origin 头的请求（同源、curl、服务间调用）不受影响，正常放行，
// 只是拿不到 CORS 头——它们本来就不需要。带 Origin 但不在白名单的
// 预检请求直接 403；普通请求照常往下走，由浏览器侧的同源策略拦截
// （服务端没必要也拦 curl 之类的非浏览器客户端）。
func CORS(allowed []string) gin.HandlerFunc {
	wildcard := false
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		if o == "*" {
			wildcard = true
			continue
		}
		set[o] = true
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		// 只要带 Origin 就声明响应随 Origin 变化，避免被中间缓存串味。
		c.Header("Vary", "Origin")

		if !wildcard && !set[origin] {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		if wildcard {
			c.Header("Access-Control-Allow-Origin", "*")
		} else {
			c.Header("Access-Control-Allow-Origin", origin)
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// AllowedOrigins 从 viper 读 `server.cors.allowed_origins`，
// 兼容三种写法：YAML 列表、逗号分隔的 env 串、env 串里的空格。
//
// viper 的 GetStringSlice 遇到 env 变量时按空白切分，拿到逗号串不会
// 再拆一层，所以这里统一对每个元素再按逗号切一次。
// 未配置时返回 nil —— 调用方拿到空切片即 fail closed。
func AllowedOrigins(v *viper.Viper) []string {
	var out []string
	for _, s := range v.GetStringSlice("server.cors.allowed_origins") {
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}
