package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/internal/httpserver"
	"github.com/ruoxizhnya/quant-trading/pkg/alert"
	"github.com/ruoxizhnya/quant-trading/pkg/auth"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/spf13/viper"
)

// ─── 限流中间件（原 cmd/analysis/middleware.go，逐字搬运）──────────────

type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitorInfo
	rate     int
	window   time.Duration
}

type visitorInfo struct {
	count    int
	lastSeen time.Time
}

// NewRateLimiter 构造 per-ClientIP 滑动窗口限流器。
func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
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
// 新加一条必须是有意识的动作，护栏见 bootstrap 包的 rate_limit_exempt_test。
var RateLimitExemptPaths = []string{
	"/health",
	"/api/health",
	"/api/auth/status",
}

// isRateLimitExempt 逐字相等匹配（不做前缀匹配 —— 前缀匹配会让
// `/api/auth/status/../login` 这类写法有机会绕过限流）。
func IsRateLimitExempt(path string) bool {
	for _, p := range RateLimitExemptPaths {
		if p == path {
			return true
		}
	}
	return false
}

func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsRateLimitExempt(c.Request.URL.Path) {
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

func (rl *RateLimiter) allow(ip string) bool {
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

func (rl *RateLimiter) cleanup() {
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

// ─── 路由与 HTTP 生命周期（原 setup.go，逐字搬运 + 服务名参数化）──────

// RateLimitPerMinute 返回网关限流值（每 ClientIP 每分钟），来自
// rate_limit.per_minute，缺省 100。env 可经 RATE_LIMIT_PER_MINUTE 覆盖。
func RateLimitPerMinute(v *viper.Viper) int {
	if n := v.GetInt("rate_limit.per_minute"); n > 0 {
		return n
	}
	return 100
}

// ApplyGinMode 按 server.gin_mode 设置 gin 进程级运行模式。
//
// AUD-29：必须在任何 router 构建前恰好执行一次。它曾经待在 buildRouter
// 里以 logging.format 为键 —— 为什么那是错的键、错的位置，见
// internal/httpserver/ginmode.go。
func ApplyGinMode(v *viper.Viper, logger zerolog.Logger) {
	raw := v.GetString(httpserver.ConfigKeyGinMode)
	applied, recognized := httpserver.ApplyGinMode(raw)
	if !recognized {
		logger.Warn().
			Str(httpserver.ConfigKeyGinMode, raw).
			Str("applied", applied).
			Msg("unrecognized gin mode; falling back to release")
	}
}

// BuildRouter 创建 gin router：recovery、CORS、限流、请求日志、
// 鉴权中间件（启用时）。
//
// gin 的运行模式刻意不在这里设。它是进程级全局，由启动期的 ApplyGinMode
// 恰好设一次（AUD-29）；测试会直接调 BuildRouter，在这里改全局会与并行
// 测试竞争。
func BuildRouter(authSvc *auth.Service, v *viper.Viper, logger zerolog.Logger) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	// P0-4: CORS 按白名单回显，白名单来自 server.cors.allowed_origins。
	// 未配置 = 不回显任何 ACAO（fail closed），不再是硬编码的 `*`。
	router.Use(httpserver.CORS(httpserver.AllowedOrigins(v)))
	router.Use(NewRateLimiter(RateLimitPerMinute(v), time.Minute).Middleware())
	router.Use(RequestLogger(logger))
	// P1-2: JWT auth middleware (no-op when auth is disabled) + audit
	// log middleware. Both run before route registration so the
	// handlers can rely on the context values being set.
	if authSvc.Enabled() {
		router.Use(authSvc.Middleware())
		router.Use(authSvc.AuditMiddleware())
	}
	return router
}

// StartHTTPServer 在 goroutine 中启动 HTTP server。返回 *http.Server
// 供优雅关停。serviceName 只影响启动日志的可读性。
func StartHTTPServer(router *gin.Engine, v *viper.Viper, logger zerolog.Logger, serviceName string) *http.Server {
	host := v.GetString("server.host")
	port := v.GetInt("server.port")
	addr := fmt.Sprintf("%s:%d", host, port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	go func() {
		logger.Info().
			Str("address", addr).
			Msgf("%s starting", serviceName)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("Server failed")
		}
	}()
	return srv
}

// WaitForShutdown 阻塞直到收到 SIGINT 或 SIGTERM。
func WaitForShutdown() os.Signal {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	return <-quit
}

// GracefulShutdown 执行有序关停序列（Sprint 6 P0-8, ODR-013）：
//  1. JobService.Shutdown — 拒绝新任务、取消在途 ctx（必须在 srv.Shutdown
//     之前，让跑着的回测看到 ctx 取消并自行写 "failed" 状态）
//  2. AlertManager.Close — 停 webhook 投递 goroutine
//  3. srv.Shutdown — 停止接受新请求，等在途 handler 返回
//  4. JobService.CleanupStaleRunning — 兜底清扫 goroutine 没来得及写的行
//  5. store.Close — 释放 DB 连接
//
// 总预算 30s 父 ctx；阶段 1-3 共享；阶段 4 拿新 5s ctx，卡死的 DB 不拖住关停。
//
// jobService / alertManager 允许 nil（不承载对应职责的服务，如 ai-service），
// nil 的阶段整段跳过 —— 关停序列的「顺序不变量」只对存在的阶段有意义。
func GracefulShutdown(srv *http.Server, jobService *backtest.JobService, alertManager *alert.AlertManager, store *storage.PostgresStore, logger zerolog.Logger) {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if jobService != nil {
		if err := jobService.Shutdown(shutdownCtx); err != nil {
			logger.Warn().Err(err).Msg("JobService.Shutdown did not drain cleanly; will run CleanupStaleRunning")
		}
	}

	if alertManager != nil {
		// P2 alert (ODR-025): close the AlertManager. This stops the
		// in-process Webhook delivery goroutine (if any) and the recorder
		// channel. The PeriodicAlertLoop's Start() goroutine is bound to
		// context.Background() so it does not observe this ctx cancel
		// directly; instead, we close the manager and rely on the next
		// tick's Evaluate failing fast due to closed channels.
		alertManager.Close()
		logger.Info().Msg("AlertManager closed (P2 alert)")
	}

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("HTTP server forced to shutdown")
	} else {
		logger.Info().Msg("HTTP server stopped accepting new requests")
	}

	if jobService == nil {
		// 无 JobService 的服务没有「running 行」需要清扫，直接关库。
		if store != nil {
			store.Close()
			logger.Info().Msg("Postgres store closed")
		}
		logger.Info().Msg("Server exited")
		return
	}

	// Phase 3: sweep any rows still stuck in 'running'. We do this
	// with a fresh, short ctx so a stuck DB doesn't hold the whole
	// shutdown open past the budget.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	transitioned, cleanupErr := jobService.CleanupStaleRunning(cleanupCtx)
	if cleanupErr != nil {
		logger.Error().Err(cleanupErr).Msg("CleanupStaleRunning failed; some jobs may still appear as 'running' in DB")
	} else if transitioned > 0 {
		logger.Info().Int("transitioned", transitioned).Msg("Stale 'running' jobs transitioned to 'failed'")
	} else {
		logger.Info().Msg("No stale 'running' jobs found; DB state is clean")
	}

	// Phase 4: close remaining resources. The PluginLoader's Watch
	// loop is context-driven and exits on its own; we don't need to
	// explicitly stop it. The store gets an explicit Close so the
	// underlying *sql.DB is released and FDs don't leak after exit.
	if store != nil {
		store.Close()
		logger.Info().Msg("Postgres store closed")
	}
	logger.Info().Msg("Server exited")
}
