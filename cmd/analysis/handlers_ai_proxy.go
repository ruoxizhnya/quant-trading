package main

// AI 路由反代（AI 拆仓阶段 1，ADR-027 §5 第 8 步的前置切片）。
//
// copilot / pipeline / explore / tools 四族 handler 已随 ai-service
// （cmd/ai）迁出本进程；本文件把同名路径族原样转发过去。设计约束：
//
//   - **前端与 openapi 契约零改动** —— 路径、宿主、端口全部维持原样，
//     nginx 的 /api 反代目标仍是本服务（analysis-service）。
//   - **反代在网关层，不在 ai-service** —— 与 registerProxyRoutes 的
//     data-service 先例同构；鉴权中间件（buildRouter 已挂）仍在转发前
//     生效，行为与「handler 在本进程」时代一致。
//   - 流式端点（如 pipeline 的 SSE）经 FlushInterval=-1 逐写直通，
//     不缓冲（与 sync 代理同一条不变量）。
//
// 路由族（与 ai-service 注册的路由一一对应）：
//   - /api/copilot（+ /api/copilot/*path）
//   - /api/pipeline（+ /api/pipeline/*path）
//   - /api/explore（+ /api/explore/*path）
//   - /api/tools（+ /api/tools/*path）
//
// 精确路径与通配各注册一条：gin 的 *path 通配不匹配无尾斜杠的族根
// （如 GET /api/tools 列表端点），漏掉精确路径会让 301 跟随把 POST 变
// GET —— 反代必须在网关就把两种形态都接住。

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

// aiServiceURL 解析 ai-service 地址（ai_service.url，缺省 :8086）。
func aiServiceURL(v *viper.Viper) string {
	if u := v.GetString("ai_service.url"); u != "" {
		return u
	}
	return "http://localhost:8086"
}

// registerAIProxyRoutes 把四族 AI 路由反代到 ai-service。
func registerAIProxyRoutes(router *gin.Engine, v *viper.Viper, logger zerolog.Logger) {
	target, err := url.Parse(aiServiceURL(v))
	if err != nil {
		logger.Error().Err(err).Str("url", aiServiceURL(v)).
			Msg("invalid ai_service.url; AI proxy routes not registered")
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1 // SSE 逐写直通，不缓冲

	// ai-service 未起时返回 503 并点名，而不是网关 502 说不清谁挂了。
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error().Err(err).Str("path", r.URL.Path).Msg("ai-service unreachable")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"ai-service unreachable (is cmd/ai running?)"}`))
	}

	for _, family := range []string{"copilot", "pipeline", "explore", "tools"} {
		family := family
		router.Any("/api/"+family, func(c *gin.Context) {
			proxy.ServeHTTP(c.Writer, c.Request)
		})
		router.Any("/api/"+family+"/*path", func(c *gin.Context) {
			proxy.ServeHTTP(c.Writer, c.Request)
		})
	}
	logger.Info().Str("target", target.String()).
		Msg("AI proxy routes registered: /api/{copilot,pipeline,explore,tools} → ai-service")
}
