package main

// Tests for the S7-P2-5 split of cmd/data/main.go (ODR-043).
// The 1713-line God File was decomposed into:
//   - main.go         — thin orchestrator (main + registerRoutes)
//   - setup.go        — composition-root builders
//   - middleware.go   — CORS + rate-limiter
//   - handlers_*.go   — domain-grouped HTTP handlers
//
// These tests pin the structural invariants of the split (no
// duplicate declarations, read/sync handler separation) and exercise
// the pure-function builders that don't need a database.
//
// AUD-28 (ODR-065): gin's mode is set once in TestMain (main_test.go);
// no test here calls gin.SetMode. These tests deliberately do NOT call
// t.Parallel() — they drive the *global* viper (loadConfig() writes
// defaults, TestBuildRouter_WiresCORSAllowlist does viper.Set), so
// running them concurrently would race on viper's shared state. That is
// a different race from the gin one AUD-28 fixes, and parallelising
// would trade one race for another.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/internal/httpserver"
)

// TestLoadConfig_SetsDefaults verifies that loadConfig populates
// viper with the expected defaults even when the YAML file is
// minimal. The data service must boot with these values without
// requiring every key in the config file.
func TestLoadConfig_SetsDefaults(t *testing.T) {
	// loadConfig searches ./config, ../config, ../../config — from
	// the test cwd (cmd/data/) the second path resolves to the
	// repo-root config dir.
	if err := loadConfig(); err != nil {
		t.Fatalf("loadConfig() failed: %v", err)
	}

	cases := []struct {
		key  string
		want interface{}
	}{
		{"server.host", "0.0.0.0"},
		{"server.port", 8081},
		{"logging.level", "info"},
		{"logging.format", "json"},
		{"database.sslmode", "disable"},
		{"tushare.max_retries", 3},
	}
	for _, c := range cases {
		if got := viper.Get(c.key); !reflect.DeepEqual(got, c.want) {
			t.Errorf("viper.Get(%q) = %v (%T), want %v (%T)",
				c.key, got, got, c.want, c.want)
		}
	}
}

// TestBuildRouter_HasMiddleware verifies that buildRouter wires the
// four middleware layers (recovery, CORS, rate-limit, request-logger)
// and returns a usable gin.Engine.
func TestBuildRouter_HasMiddleware(t *testing.T) {
	// loadConfig must run first so viper carries the server.* keys that
	// buildRouter reads (AUD-29: it no longer reads logging.level to pick
	// a gin mode — that moved to applyGinMode at startup).
	require.NoError(t, loadConfig())

	r := buildRouter()
	require.NotNil(t, r)

	// buildRouter should not panic and should produce a router that
	// can serve a basic request (even a 404 proves the middleware
	// chain ran without blowing up).
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/__nonexistent__", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestBuildRouter_DoesNotTouchGinMode is the AUD-29 regression guard.
//
// gin's run mode is a process-wide global, applied once at startup by
// applyGinMode (see internal/httpserver/ginmode.go). buildRouter used to
// write it — `if viper.GetString("logging.level") != "debug"
// { gin.SetMode(gin.ReleaseMode) }` — which meant any test calling
// buildRouter mutated global state mid-run.
//
// The config below is exactly the one the old line would have reacted to,
// so this fails against the old code and passes against the new.
func TestBuildRouter_DoesNotTouchGinMode(t *testing.T) {
	require.NoError(t, loadConfig())
	viper.Set("logging.level", "info")
	viper.Set("logging.format", "json")

	before := gin.Mode()
	r := buildRouter()
	require.NotNil(t, r)
	assert.Equal(t, before, gin.Mode(),
		"buildRouter 不得改进程级 gin mode；它由启动期的 applyGinMode 设一次")
}

// TestCorsMiddleware_SetsPreflightHeaders verifies that an allowed
// origin's OPTIONS preflight short-circuits with 204 + CORS headers.
//
// P0-4: 此前断言的是 `Access-Control-Allow-Origin: *`（硬编码通配）。
// 现在白名单来自配置，未命中就不回显；契约测试在
// internal/httpserver/cors_test.go，这里只守装配仍然生效。
func TestCorsMiddleware_SetsPreflightHeaders(t *testing.T) {
	r := gin.New()
	r.Use(httpserver.CORS([]string{"http://localhost:5173"}))
	r.OPTIONS("/anything", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodOptions, "/anything", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "http://localhost:5173", w.Header().Get("Access-Control-Allow-Origin"))
	assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Methods"))
}

// TestBuildRouter_WiresCORSAllowlist (P0-4) 守装配：中间件实现改对了但
// buildRouter 没接上，等于没改。白名单通过 viper override 注入，避免测试
// 依赖仓库里的真实配置文件。
func TestBuildRouter_WiresCORSAllowlist(t *testing.T) {
	require.NoError(t, loadConfig())
	viper.Set("server.cors.allowed_origins", []string{"http://localhost:5173"})
	defer viper.Set("server.cors.allowed_origins", nil)

	r := buildRouter()
	r.GET("/probe", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	req, _ := http.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, "http://localhost:5173", w.Header().Get("Access-Control-Allow-Origin"))

	req, _ = http.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

// TestNewRateLimiter_AllowsBurstThenBlocks verifies that the
// fixed-window rate limiter admits requests up to the burst size
// and then rejects the next one with 429.
func TestNewRateLimiter_AllowsBurstThenBlocks(t *testing.T) {
	// rate of 3 with a 1-second window — the 4th request within
	// the same window must be blocked. A non-zero window is required
	// because window=0 makes resetAt = now, which is always in the
	// past by the time the next request arrives.
	rl := newRateLimiter(3, time.Second)
	require.NotNil(t, rl)

	r := gin.New()
	r.Use(rl.middleware())
	r.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/ping", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "request %d should pass", i+1)
	}

	// 4th request — over the limit.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

// TestSplit_NoDuplicateHandlerDeclarations is the regression guard
// for the bug that blocked S7-P2-5: the sed-based extraction
// overlapped on the syncFundamentalsHandler range and produced a
// "redeclared in this block" build error. This test walks every
// func declaration across the split files and asserts that no name
// appears more than once.
func TestSplit_NoDuplicateHandlerDeclarations(t *testing.T) {
	splitFiles := []string{
		"main.go",
		"setup.go",
		"middleware.go",
		"handlers_stocks.go",
		"handlers_ohlcv.go",
		"handlers_fundamentals.go",
		"handlers_sync.go",
		"handlers_factor.go",
		"handlers_ingest.go",
		"handlers_equitydeep_ingest.go",
	}

	_, testFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	pkgDir := filepath.Dir(testFile)

	seen := make(map[string]string) // func name → first file seen in
	for _, f := range splitFiles {
		path := filepath.Join(pkgDir, f)
		body, err := os.ReadFile(path)
		require.NoError(t, err, "cannot read %s", f)
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "func ") {
				continue
			}
			// Strip "func ", optional receiver, and the arg list.
			rest := strings.TrimPrefix(line, "func ")
			if strings.HasPrefix(rest, "(") {
				if idx := strings.Index(rest, ") "); idx >= 0 {
					rest = rest[idx+2:]
				}
			}
			name := rest
			if idx := strings.IndexAny(name, "( "); idx >= 0 {
				name = name[:idx]
			}
			if prev, dup := seen[name]; dup {
				t.Fatalf("duplicate func %q declared in both %s and %s", name, prev, f)
			}
			seen[name] = f
		}
	}
}

// TestSplit_HandlersFundamentals_HasOnlyReadHandlers verifies the
// clean separation enforced by S7-P2-5: handlers_fundamentals.go
// must contain ONLY read handlers (get*). Sync handlers belong in
// handlers_sync.go. This prevents the overlap that caused the
// original redeclaration bug from creeping back.
func TestSplit_HandlersFundamentals_HasOnlyReadHandlers(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	pkgDir := filepath.Dir(testFile)

	body, err := os.ReadFile(filepath.Join(pkgDir, "handlers_fundamentals.go"))
	require.NoError(t, err)

	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "func ") {
			continue
		}
		// Extract the function name.
		rest := strings.TrimPrefix(line, "func ")
		if strings.HasPrefix(rest, "(") {
			if idx := strings.Index(rest, ") "); idx >= 0 {
				rest = rest[idx+2:]
			}
		}
		name := rest
		if idx := strings.IndexAny(name, "( "); idx >= 0 {
			name = name[:idx]
		}
		if !strings.HasPrefix(name, "get") {
			t.Errorf("handlers_fundamentals.go should only contain read (get*) handlers; found %q", name)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────
// AUD-35 guard: the env names for logging.* are LOGGING_LEVEL / LOGGING_FORMAT
// ──────────────────────────────────────────────────────────────────────

// The analysis service has the same guard in cmd/analysis/setup_test.go
// (with the full rationale). It is duplicated here because this package
// drives the *global* viper rather than a fresh viper.New() instance — a
// different wiring that a test in the other package cannot cover.
//
// viper.Reset() first: TestBuildRouter_DoesNotTouchGinMode calls viper.Set,
// and an explicit Set outranks env in viper's precedence order, so a
// leftover override would mask what these tests are measuring.

func TestLoadConfig_LoggingEnvNamesAreTheOnesThatWork(t *testing.T) {
	viper.Reset()
	t.Setenv("LOGGING_LEVEL", "debug")
	t.Setenv("LOGGING_FORMAT", "text")

	require.NoError(t, loadConfig())

	assert.Equal(t, "debug", viper.GetString("logging.level"),
		"LOGGING_LEVEL 必须覆盖 yaml 里的 logging.level")
	assert.Equal(t, "text", viper.GetString("logging.format"),
		"LOGGING_FORMAT 必须覆盖 yaml 里的 logging.format")
}

func TestLoadConfig_RetiredLogEnvNamesStayDead(t *testing.T) {
	viper.Reset()
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "text")

	require.NoError(t, loadConfig())

	assert.Equal(t, "info", viper.GetString("logging.level"),
		"LOG_LEVEL 是已退役的名字（AUD-35）")
	assert.Equal(t, "json", viper.GetString("logging.format"),
		"LOG_FORMAT 是已退役的名字（AUD-35）")
}

// ──────────────────────────────────────────────────────────────────────
// AUD-58 guard: RATE_LIMIT_PER_MINUTE 是本包（全局 viper）真正读得到的名字
// ──────────────────────────────────────────────────────────────────────

// 与上面 logging.* 那对同源：本包驱动的是**全局** viper，所以
// `cmd/analysis/rate_limit_budget_test.go` 覆盖不到这里 —— 那个包用的是
// 注入的 `viper.New()` 实例（见本文件开头那段注释）。而 AUD-58 的 e2e 覆盖层
// 给**两个**服务都设了 `RATE_LIMIT_PER_MINUTE`：只证明 analysis 认它是不够的，
// 另一半若读不到，就是**安慰剂** —— 而安慰剂比没有更糟，因为它看起来在做事。
//
// 取 42 而不是 100：`rateLimitPerMinute()` 的兜底是 `return 100`，而 shipped
// 配置**也是** 100 —— 取 100 的话「读到了覆盖」与「用了兜底」在断言里长得一模一样。
func TestLoadConfig_RateLimitEnvNameIsTheOneThatWorks(t *testing.T) {
	viper.Reset()
	t.Setenv("RATE_LIMIT_PER_MINUTE", "42")

	require.NoError(t, loadConfig())

	got := rateLimitPerMinute()
	require.NotEqual(t, 100, got, "100 是兜底值 —— 落在这里说明覆盖根本没生效")
	assert.Equal(t, 42, got,
		"RATE_LIMIT_PER_MINUTE 必须真的被 rateLimitPerMinute() 读到")
}

// 反证腿：名字不对（少了 _PER_MINUTE）时不许生效 —— 否则上面那个 42 可能
// 来自「任何环境变量都能改限流」这种错误接线。
func TestLoadConfig_RetiredRateLimitEnvNameStaysDead(t *testing.T) {
	viper.Reset()
	t.Setenv("RATE_LIMIT", "42")

	require.NoError(t, loadConfig())

	assert.Equal(t, 100, rateLimitPerMinute(),
		"RATE_LIMIT 不是正确名字（正确名是 RATE_LIMIT_PER_MINUTE）—— "+
			"它生效就说明限流的读法比预期宽松")
}
