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
	// loadConfig must run first so viper has the logging.level key
	// that buildRouter consults for ReleaseMode.
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

// TestCorsMiddleware_SetsPreflightHeaders verifies that OPTIONS
// requests get the permissive CORS headers the SPA frontend relies on.
// corsMiddleware short-circuits OPTIONS with 204 No Content.
func TestCorsMiddleware_SetsPreflightHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(corsMiddleware())
	r.OPTIONS("/anything", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodOptions, "/anything", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Methods"))
}

// TestNewRateLimiter_AllowsBurstThenBlocks verifies that the
// fixed-window rate limiter admits requests up to the burst size
// and then rejects the next one with 429.
func TestNewRateLimiter_AllowsBurstThenBlocks(t *testing.T) {
	gin.SetMode(gin.TestMode)
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
