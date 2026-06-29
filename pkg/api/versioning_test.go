package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestRouter builds a router with the versioning middleware + a
// sample /api/backtest route to verify URL rewriting.
func newTestRouter(cfg VersioningConfig) *gin.Engine {
	r := gin.New()
	r.Use(APIVersionMiddleware(r, cfg))
	r.Use(DeprecationHeader(cfg))
	// Sample legacy route (registered on /api/, not /api/v1/).
	r.GET("/api/backtest", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"path": c.Request.URL.Path, "version": CurrentAPIVersion(c)})
	})
	r.POST("/api/backtest", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"path": c.Request.URL.Path, "version": CurrentAPIVersion(c)})
	})
	// Discovery.
	if cfg.DiscoveryPath != "" {
		r.GET(cfg.DiscoveryPath, DiscoveryHandler("test-service", cfg, []string{
			"GET  /api/backtest",
			"POST /api/backtest",
		}))
	}
	return r
}

// ============================================================
// isVersioned + stripVersionPrefix + extractVersion
// ============================================================

func TestIsVersioned(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/api/v1/backtest", true},
		{"/api/v2/backtest", true},
		{"/api/v10/whatever", true},
		{"/api/backtest", false},
		{"/api/v1", true},
		{"/api/v1/", true},
		{"/api/v1abc", false},   // not pure number
		{"/api/v", false},       // no number
		{"/health", false},      // not /api/
		{"/api/version", false}, // "version" is not v<NUM>
		{"/api/versioning", false},
	}
	for _, tc := range cases {
		if got := isVersioned(tc.path); got != tc.want {
			t.Errorf("isVersioned(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestStripVersionPrefix(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/api/v1/backtest", "/api/backtest"},
		{"/api/v2/backtest/123", "/api/backtest/123"},
		{"/api/v1", "/api/"},
		{"/api/v1/", "/api/"},
		{"/api/backtest", "/api/backtest"}, // unversioned — unchanged
		{"/health", "/health"},
		{"/api/v10/some/deep/path", "/api/some/deep/path"},
	}
	for _, tc := range cases {
		if got := stripVersionPrefix(tc.in); got != tc.want {
			t.Errorf("stripVersionPrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtractVersion(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/api/v1/backtest", "v1"},
		{"/api/v2/backtest/123", "v2"},
		{"/api/v10/foo", "v10"},
		{"/api/v1", "v1"},
		{"/api/v1/", "v1"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := extractVersion(tc.in); got != tc.want {
			t.Errorf("extractVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ============================================================
// APIVersionMiddleware
// ============================================================

func TestMiddleware_CanonicalPathRewritesToLegacy(t *testing.T) {
	r := newTestRouter(DefaultVersioningConfig())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/backtest", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"path":"/api/backtest"`) {
		t.Errorf("expected path=/api/backtest in body, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"version":"v1"`) {
		t.Errorf("expected version=v1 in body, got %s", w.Body.String())
	}
	if got := w.Header().Get("X-API-Version"); got != "v1" {
		t.Errorf("expected X-API-Version=v1, got %q", got)
	}
}

func TestMiddleware_LegacyPathWorksWithoutRedirect(t *testing.T) {
	r := newTestRouter(DefaultVersioningConfig())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/backtest", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"version":"legacy"`) {
		t.Errorf("expected version=legacy, got %s", w.Body.String())
	}
}

func TestMiddleware_LegacyPath_AddsDeprecationHeader(t *testing.T) {
	r := newTestRouter(DefaultVersioningConfig())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/backtest", nil)
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Deprecation"); got == "" {
		t.Error("expected Deprecation header on legacy response")
	}
	if got := w.Header().Get("Link"); !strings.Contains(got, `rel="successor-version"`) {
		t.Errorf("expected Link header with successor-version, got %q", got)
	}
	if got := w.Header().Get("X-API-Deprecated"); got != "true" {
		t.Errorf("expected X-API-Deprecated=true, got %q", got)
	}
}

func TestMiddleware_CanonicalPath_NoDeprecationHeader(t *testing.T) {
	r := newTestRouter(DefaultVersioningConfig())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/backtest", nil)
	r.ServeHTTP(w, req)
	if got := w.Header().Get("Deprecation"); got != "" {
		t.Errorf("expected NO Deprecation on canonical path, got %q", got)
	}
	if got := w.Header().Get("X-API-Deprecated"); got != "" {
		t.Errorf("expected NO X-API-Deprecated on canonical path, got %q", got)
	}
}

func TestMiddleware_StrictRedirect_GET(t *testing.T) {
	cfg := DefaultVersioningConfig()
	cfg.LegacyRedirect = true
	r := newTestRouter(cfg)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/backtest", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMovedPermanently {
		t.Errorf("expected 301, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/api/v1/backtest" {
		t.Errorf("expected Location=/api/v1/backtest, got %q", loc)
	}
}

func TestMiddleware_StrictRedirect_POST(t *testing.T) {
	cfg := DefaultVersioningConfig()
	cfg.LegacyRedirect = true
	r := newTestRouter(cfg)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/backtest", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	// POST → 308 (preserves method + body)
	if w.Code != http.StatusPermanentRedirect {
		t.Errorf("expected 308, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/api/v1/backtest" {
		t.Errorf("expected Location=/api/v1/backtest, got %q", loc)
	}
}

func TestMiddleware_StrictRedirect_HEAD(t *testing.T) {
	cfg := DefaultVersioningConfig()
	cfg.LegacyRedirect = true
	r := newTestRouter(cfg)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("HEAD", "/api/backtest", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMovedPermanently {
		t.Errorf("HEAD should also get 301, got %d", w.Code)
	}
}

func TestMiddleware_NonAPIPathNotAffected(t *testing.T) {
	r := newTestRouter(DefaultVersioningConfig())
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Deprecation"); got != "" {
		t.Errorf("expected NO Deprecation on /health, got %q", got)
	}
}

func TestMiddleware_SunsetDate_TriggersRedirect(t *testing.T) {
	cfg := DefaultVersioningConfig()
	cfg.LegacyRedirect = false // soft mode
	past := time.Now().Add(-1 * time.Hour)
	cfg.SunsetDate = &past
	r := newTestRouter(cfg)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/backtest", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMovedPermanently {
		t.Errorf("expected 301 (sunset), got %d", w.Code)
	}
}

func TestMiddleware_FutureSunsetDate_NoRedirect(t *testing.T) {
	cfg := DefaultVersioningConfig()
	future := time.Now().Add(24 * time.Hour)
	cfg.SunsetDate = &future
	r := newTestRouter(cfg)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/backtest", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (sunset in future), got %d", w.Code)
	}
}

func TestMiddleware_DeprecationDateHeader(t *testing.T) {
	cfg := DefaultVersioningConfig()
	dep := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cfg.DeprecationDate = &dep
	r := newTestRouter(cfg)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/backtest", nil)
	r.ServeHTTP(w, req)
	got := w.Header().Get("Deprecation")
	if !strings.Contains(got, "2026") {
		t.Errorf("expected Deprecation header to include 2026, got %q", got)
	}
}

func TestMiddleware_CanonicalV2Path(t *testing.T) {
	cfg := DefaultVersioningConfig()
	cfg.CurrentVersion = "v2"
	r := newTestRouter(cfg)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v2/backtest", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"path":"/api/backtest"`) {
		t.Errorf("expected path=/api/backtest, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"version":"v2"`) {
		t.Errorf("expected version=v2, got %s", w.Body.String())
	}
}

func TestMiddleware_CanonicalPOSTRewritesToLegacy(t *testing.T) {
	r := newTestRouter(DefaultVersioningConfig())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/backtest", strings.NewReader(`{"x":1}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"path":"/api/backtest"`) {
		t.Errorf("expected path=/api/backtest, got %s", w.Body.String())
	}
}

func TestMiddleware_RootCanonicalPath(t *testing.T) {
	r := newTestRouter(DefaultVersioningConfig())
	// Register the API root endpoint.
	r.GET("/api/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"path": c.Request.URL.Path, "version": CurrentAPIVersion(c)})
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestMiddleware_DeepCanonicalPath(t *testing.T) {
	r := newTestRouter(DefaultVersioningConfig())
	r.GET("/api/strategies/123/run", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"path": c.Request.URL.Path})
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/strategies/123/run", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"path":"/api/strategies/123/run"`) {
		t.Errorf("expected rewritten path, got %s", w.Body.String())
	}
}

func TestMiddleware_NotFoundOnCanonicalPathWithoutRoute(t *testing.T) {
	r := newTestRouter(DefaultVersioningConfig())
	w := httptest.NewRecorder()
	// /api/v1/nonexistent — after strip, /api/nonexistent — no route → 404
	req, _ := http.NewRequest("GET", "/api/v1/nonexistent", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown canonical path, got %d", w.Code)
	}
}

func TestMiddleware_DeprecationHeader_DeprecationDate(t *testing.T) {
	cfg := DefaultVersioningConfig()
	dep := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cfg.DeprecationDate = &dep
	r := newTestRouter(cfg)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/backtest", nil)
	r.ServeHTTP(w, req)
	got := w.Header().Get("Deprecation")
	want := dep.UTC().Format(http.TimeFormat)
	if got != want {
		t.Errorf("Deprecation header = %q, want %q", got, want)
	}
}

func TestMiddleware_DeprecationHeader_SunsetHeader(t *testing.T) {
	cfg := DefaultVersioningConfig()
	sun := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	cfg.SunsetDate = &sun
	r := newTestRouter(cfg)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/backtest", nil)
	r.ServeHTTP(w, req)
	got := w.Header().Get("Sunset")
	want := sun.UTC().Format(http.TimeFormat)
	if got != want {
		t.Errorf("Sunset header = %q, want %q", got, want)
	}
}

// ============================================================
// DiscoveryHandler
// ============================================================

func TestDiscoveryHandler(t *testing.T) {
	cfg := DefaultVersioningConfig()
	cfg.DiscoveryPath = "/api/version"
	r := newTestRouter(cfg)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/version", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"current_version":"v1"`) {
		t.Errorf("expected current_version=v1, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"service":"test-service"`) {
		t.Errorf("expected service=test-service, got %s", w.Body.String())
	}
}

func TestDiscoveryHandler_WithSunsetAndDeprecation(t *testing.T) {
	cfg := DefaultVersioningConfig()
	dep := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sun := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	cfg.DeprecationDate = &dep
	cfg.SunsetDate = &sun
	cfg.DiscoveryPath = "/api/version"
	r := newTestRouter(cfg)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/version", nil)
	r.ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, `"deprecated_since":"2026-01-01`) {
		t.Errorf("expected deprecated_since, got %s", body)
	}
	if !strings.Contains(body, `"sunset_at":"2027-01-01`) {
		t.Errorf("expected sunset_at, got %s", body)
	}
}

// ============================================================
// CurrentAPIVersion
// ============================================================

func TestCurrentAPIVersion(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{"/api/v1/backtest", "v1"},
		{"/api/v2/backtest", "v2"},
		{"/api/backtest", "legacy"},
		{"/health", "legacy"},
	}
	for _, tc := range cases {
		var got string
		// Build a router that captures CurrentAPIVersion inside the
		// route handler. For /api/* paths, we install the actual
		// versioning middleware (which sets the request header).
		// For /health, no middleware is needed since it's not /api/.
		r := gin.New()
		if strings.HasPrefix(tc.path, "/api/") {
			r.Use(APIVersionMiddleware(r, DefaultVersioningConfig()))
			r.GET("/api/backtest", func(c *gin.Context) {
				got = CurrentAPIVersion(c)
				c.Status(204)
			})
		} else {
			r.GET("/health", func(c *gin.Context) {
				got = CurrentAPIVersion(c)
				c.Status(204)
			})
		}
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", tc.path, nil)
		r.ServeHTTP(w, req)
		if got != tc.want {
			t.Errorf("path=%q: got %q, want %q", tc.path, got, tc.want)
		}
	}
}

// ============================================================
// Config defaults
// ============================================================

func TestDefaultVersioningConfig(t *testing.T) {
	cfg := DefaultVersioningConfig()
	if cfg.CurrentVersion != "v1" {
		t.Errorf("CurrentVersion = %q, want v1", cfg.CurrentVersion)
	}
	if cfg.LegacyRedirect != false {
		t.Errorf("LegacyRedirect = %v, want false", cfg.LegacyRedirect)
	}
}
