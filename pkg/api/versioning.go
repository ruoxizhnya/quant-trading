// Package api — 通用 API 基础设施.
//
// versioning 子包 (P2-16) 提供 API 版本化能力:
//   - APIVersionMiddleware: 把 /api/v1/* 透明映射到 /api/* (URL 重写 +
//     re-dispatch), 客户端可以无差别访问 /api/backtest 和
//     /api/v1/backtest.
//   - DeprecationHeader middleware: 对未带版本号的 /api/* 响应加上
//     RFC 8594 Deprecation / Sunset 头 + Link 头, 引导客户端迁移.
//   - 严格模式开关 LegacyRedirect: true 时, GET 响应 301, 其他方法
//     308, 强制重定向到 /api/v1/*.
//
// 设计目标:
//   - 不需要修改任何现有 handler 的 RegisterRoutes 方法 (避免 13 个
//     handler 文件改动).
//   - 客户端可平滑过渡: 旧路径继续工作 (带 deprecation 提示),
//     新路径也工作 (canonical).
//   - 配置化: viper 读取 api.legacy_redirect (bool) 和
//     api.current_version (string).
//
// 实现注意: gin 的 radix-tree 路由在 middleware chain 之前就匹配了
// handler, 所以 URL 重写后必须调用 engine.HandleContext(c) 重新分发.
// re-dispatch 会调用 c.reset() (会清空 c.Keys), 所以版本信息必须
// 放在 c.Request.Header 中跨过 reset 边界.
package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// internalVersionHeader is a request-header used to pass the original
// version across the re-dispatch boundary. Since gin's c.reset()
// wipes c.Keys, we need a different channel. Request headers persist
// through reset.
const internalVersionHeader = "X-Original-API-Version"

// ============================================================
// 配置
// ============================================================

// VersioningConfig 决定中间件行为.
type VersioningConfig struct {
	// CurrentVersion 是当前 canonical 版本 (e.g. "v1"). 该值会出现在
	// 响应头 X-API-Version 和 discovery endpoint.
	CurrentVersion string

	// LegacyRedirect: true → 严格模式, 旧路径强制 301/308 重定向;
	// false (默认) → 旧路径继续工作, 但加 deprecation 响应头.
	LegacyRedirect bool

	// DeprecationDate 旧路径的 deprecation 公告日 (RFC 8594 Deprecation).
	// nil → 不发送 Deprecation 头.
	DeprecationDate *time.Time

	// SunsetDate 旧路径的 sunset 日 (RFC 8594 Sunset). 到达该日后
	// 强制重定向 (即使 LegacyRedirect = false). nil → 永不过期.
	SunsetDate *time.Time

	// DiscoveryPath 是 discovery endpoint 的路径, e.g. "/api/version".
	// 如果为空, 中间件不会自动注册该 endpoint (caller 负责挂载).
	DiscoveryPath string
}

// DefaultVersioningConfig 返回默认配置: 软 deprecation, 不强制重定向.
func DefaultVersioningConfig() VersioningConfig {
	return VersioningConfig{
		CurrentVersion: "v1",
		LegacyRedirect: false,
	}
}

// VersionInfo describes the current API version, served at the
// discovery endpoint (configurable via VersioningConfig.DiscoveryPath).
//
// Clients should poll this on startup + periodically to discover
// version transitions, new endpoints, and deprecation timelines.
type VersionInfo struct {
	Service         string   `json:"service"`
	CurrentVersion  string   `json:"current_version"`
	Supported       []string `json:"supported_versions"`
	DeprecatedSince string   `json:"deprecated_since,omitempty"` // RFC3339
	SunsetAt        string   `json:"sunset_at,omitempty"`         // RFC3339
	LatestStable    string   `json:"latest_stable"`
	Endpoints       []string `json:"endpoints,omitempty"`
}

// ============================================================
// 工具函数
// ============================================================

const apiPrefix = "/api/"

// isVersioned reports whether the path already starts with
// /api/v<NUMBER>/... .
func isVersioned(path string) bool {
	if !strings.HasPrefix(path, apiPrefix) {
		return false
	}
	rest := strings.TrimPrefix(path, apiPrefix)
	// rest = "v1/backtest/..." or "backtest/..."
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "v") {
		return false
	}
	// parts[0] = "v1", "v2", ... — validate that the suffix is a
	// non-negative integer.
	suffix := strings.TrimPrefix(parts[0], "v")
	if suffix == "" {
		return false
	}
	if _, err := strconv.Atoi(suffix); err != nil {
		return false
	}
	return true
}

// stripVersionPrefix removes the /v<NUMBER>/ segment from the path,
// rewriting it to the unversioned form. E.g.,
// "/api/v1/backtest/123" → "/api/backtest/123".
// Returns the original path if it doesn't carry a version prefix.
func stripVersionPrefix(path string) string {
	if !isVersioned(path) {
		return path
	}
	rest := strings.TrimPrefix(path, apiPrefix)
	parts := strings.SplitN(rest, "/", 2)
	// parts = ["v1", "backtest/123"]
	if len(parts) < 2 {
		// /api/v1 → /api/ (root). Preserve trailing slash for path
		// consistency with non-versioned equivalent.
		return apiPrefix
	}
	return apiPrefix + parts[1]
}

// extractVersion returns the version segment from a path like
// "/api/v1/backtest" → "v1". Caller must ensure path is versioned
// (use isVersioned first).
func extractVersion(path string) string {
	rest := strings.TrimPrefix(path, apiPrefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// ============================================================
// 中间件
// ============================================================

// APIVersionMiddleware applies the versioning + deprecation rules
// from cfg. The middleware MUST be installed via engine.Use() before
// any /api/... route handlers, AND the engine reference must be
// passed in (the middleware re-dispatches the request to the engine
// after rewriting the URL, since gin's radix-tree routing runs
// before the middleware chain).
//
// Usage:
//
//	engine := gin.New()
//	engine.Use(APIVersionMiddleware(engine, cfg))
//	engine.Use(DeprecationHeader(cfg))
//	engine.GET("/api/backtest", handler) // legacy path
//
// Transformations:
//
//  1. /api/v1/foo → /api/foo (URL rewrite + re-dispatch).
//     This makes canonical paths work with the existing legacy
//     route registrations without modifying each handler.
//
//  2. If cfg.LegacyRedirect is true (or sunset has passed), returns
//     301 (GET/HEAD) or 308 (other methods) for unversioned /api/foo
//     requests. Otherwise, the request proceeds but the response
//     receives RFC 8594 Deprecation / Sunset headers via the
//     DeprecationHeader middleware (must be applied after this one).
func APIVersionMiddleware(engine *gin.Engine, cfg VersioningConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// Re-entry guard: if the request was already rewritten by a
		// previous invocation of this middleware, the URL is now in
		// its unversioned form. Skip the URL rewrite + redirect
		// logic; just continue the chain. The version info is
		// carried in the request header.
		if origVer := c.Request.Header.Get(internalVersionHeader); origVer != "" {
			c.Header("X-API-Version", origVer)
			c.Next()
			return
		}

		// Fast-path: not under /api/ at all — skip.
		if !strings.HasPrefix(path, apiPrefix) {
			c.Next()
			return
		}

		// 1. Canonical /api/v1/... → /api/... (URL rewrite + re-dispatch).
		if isVersioned(path) {
			newPath := stripVersionPrefix(path)
			ver := extractVersion(path)
			c.Header("X-API-Version", ver)
			if newPath != path {
				// Stash the version on the request header so it
				// survives c.reset() inside engine.HandleContext.
				c.Request.Header.Set(internalVersionHeader, ver)
				// gin matches the radix tree before middleware runs,
				// so we must re-dispatch with the rewritten path.
				c.Request.URL.Path = newPath
				c.Request.RequestURI = newPath
				engine.HandleContext(c)
				c.Abort()
				return
			}
			c.Next()
			return
		}

		// 2. Unversioned /api/... path. Check deprecation / sunset.
		c.Header("X-API-Version", "legacy")
		c.Request.Header.Set(internalVersionHeader, "legacy")

		now := time.Now().UTC()
		sunset := cfg.SunsetDate != nil && now.After(*cfg.SunsetDate)
		if cfg.LegacyRedirect || sunset {
			// Build the canonical /api/v1/<rest> target.
			canonical := "/api/" + cfg.CurrentVersion + strings.TrimPrefix(path, "/api")
			code := http.StatusPermanentRedirect // 308 — preserves method + body
			if c.Request.Method == "GET" || c.Request.Method == "HEAD" {
				code = http.StatusMovedPermanently // 301
			}
			c.Header("Deprecation", "true")
			if cfg.DeprecationDate != nil {
				c.Header("Deprecation", cfg.DeprecationDate.UTC().Format(http.TimeFormat))
			}
			if cfg.SunsetDate != nil {
				c.Header("Sunset", cfg.SunsetDate.UTC().Format(http.TimeFormat))
			}
			c.Header("Link", `</api/`+cfg.CurrentVersion+`>; rel="successor-version"`)
			c.Redirect(code, canonical)
			c.Abort()
			return
		}

		// Soft deprecation: just add headers (handled by DeprecationHeader).
		c.Next()
	}
}

// DeprecationHeader middleware adds RFC 8594 deprecation headers to
// all /api/... responses that were originally unversioned. Apply
// this AFTER APIVersionMiddleware. The middleware reads the
// internal version header (set by APIVersionMiddleware) to detect
// whether the request was originally versioned, and skips adding
// deprecation headers in that case.
//
// Headers set:
//   - Deprecation: <RFC3339> (or "true" if no date provided)
//   - Sunset:      <RFC3339>  (if SunsetDate set)
//   - Link:        </api/v1>; rel="successor-version"
//   - X-API-Deprecated: true
func DeprecationHeader(cfg VersioningConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		// Skip if the request was originally versioned.
		if v := c.Request.Header.Get(internalVersionHeader); v != "" && v != "legacy" {
			return
		}
		// Skip non-/api paths.
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, apiPrefix) {
			return
		}
		// Deprecation header (RFC 8594 / RFC 9745).
		if cfg.DeprecationDate != nil {
			c.Header("Deprecation", cfg.DeprecationDate.UTC().Format(http.TimeFormat))
		} else {
			c.Header("Deprecation", "true")
		}
		if cfg.SunsetDate != nil {
			c.Header("Sunset", cfg.SunsetDate.UTC().Format(http.TimeFormat))
		}
		c.Header("Link", `</api/`+cfg.CurrentVersion+`>; rel="successor-version"`)
		c.Header("X-API-Deprecated", "true")
	}
}

// DiscoveryHandler returns a gin.HandlerFunc that serves the
// VersionInfo discovery document at the configured path.
func DiscoveryHandler(serviceName string, cfg VersioningConfig, endpoints []string) gin.HandlerFunc {
	info := VersionInfo{
		Service:        serviceName,
		CurrentVersion: cfg.CurrentVersion,
		Supported:      []string{cfg.CurrentVersion},
		LatestStable:   cfg.CurrentVersion,
		Endpoints:      endpoints,
	}
	if cfg.DeprecationDate != nil {
		info.DeprecatedSince = cfg.DeprecationDate.UTC().Format(time.RFC3339)
	}
	if cfg.SunsetDate != nil {
		info.SunsetAt = cfg.SunsetDate.UTC().Format(time.RFC3339)
	}
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, info)
	}
}

// ============================================================
// 工具函数
// ============================================================

// CurrentAPIVersion returns the API version that was set by the
// middleware (via request header). Returns "legacy" if the
// middleware hasn't run on this request.
func CurrentAPIVersion(c *gin.Context) string {
	if v := c.Request.Header.Get(internalVersionHeader); v != "" {
		return v
	}
	return "legacy"
}
