package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain sets gin to test mode once for the entire package.
//
// S7-P0-14 (ODR-043): Individual tests must NOT call gin.SetMode()
// themselves. The middleware tests all use t.Parallel(), and gin.SetMode()
// writes to a package-level global (ginMode) — concurrent calls from
// parallel test goroutines triggered a -race report that failed the
// entire pkg/auth package. Centralizing the call in TestMain runs it
// exactly once before any test goroutine starts.
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

func setupRouter(s *Service) *gin.Engine {
	r := gin.New()
	r.Use(s.Middleware())
	r.GET("/api/secret", func(c *gin.Context) {
		uid, role, ok := UserFromContext(c)
		if !ok {
			// When auth is disabled, no user is attached; return 200
			// with role=anonymous so the test can verify "no-op".
			c.JSON(http.StatusOK, gin.H{"user_id": int64(0), "role": "anonymous"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"user_id": uid, "role": string(role)})
	})
	r.POST("/api/mutate", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.POST("/api/auth/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"login": "ok"})
	})
	return r
}

func TestMiddleware_Disabled_NoOp(t *testing.T) {
	t.Parallel()
	s := NewService(nil, Config{}) // disabled
	r := setupRouter(s)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/secret", nil)
	r.ServeHTTP(w, req)
	// With disabled auth, the handler is hit even without a token.
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "anonymous")
}

func TestMiddleware_NoToken_401(t *testing.T) {
	t.Parallel()
	s := NewService(nil, Config{JWTSecret: []byte("test")})
	r := setupRouter(s)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/secret", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "missing Authorization header")
}

func TestMiddleware_BadFormat_401(t *testing.T) {
	t.Parallel()
	s := NewService(nil, Config{JWTSecret: []byte("test")})
	r := setupRouter(s)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/secret", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_ValidToken_OK(t *testing.T) {
	t.Parallel()
	s := NewService(nil, Config{JWTSecret: []byte("test")})
	r := setupRouter(s)
	u := &User{ID: 99, Username: "alice", Role: RoleTrader}
	access, _, err := s.IssueTokens(u)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/secret", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"user_id":99`)
	assert.Contains(t, w.Body.String(), `"role":"trader"`)
}

func TestMiddleware_RefreshTokenRejected(t *testing.T) {
	t.Parallel()
	s := NewService(nil, Config{JWTSecret: []byte("test")})
	r := setupRouter(s)
	u := &User{ID: 1, Username: "x", Role: RoleAdmin}
	_, refresh, err := s.IssueTokens(u)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/secret", nil)
	req.Header.Set("Authorization", "Bearer "+refresh)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_PublicPath_NoAuth(t *testing.T) {
	t.Parallel()
	s := NewService(nil, Config{JWTSecret: []byte("test")})
	r := setupRouter(s)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddleware_ExpiredToken_401(t *testing.T) {
	t.Parallel()
	s := NewService(nil, Config{JWTSecret: []byte("test")})
	r := setupRouter(s)
	// Manually craft an expired token.
	c := Claims{
		Username: "alice",
		Role:     "admin",
		Kind:     "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "1",
			Issuer:    "test",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	signed, err := tok.SignedString([]byte("test"))
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/secret", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireRole_403(t *testing.T) {
	t.Parallel()
	s := NewService(nil, Config{JWTSecret: []byte("test")})
	r := gin.New()
	r.Use(s.Middleware())
	r.GET("/admin", RequireRole(RoleAdmin), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	u := &User{ID: 1, Username: "viewer", Role: RoleViewer}
	access, _, err := s.IssueTokens(u)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "insufficient role")
}

func TestRequireRole_OK(t *testing.T) {
	t.Parallel()
	s := NewService(nil, Config{JWTSecret: []byte("test")})
	r := gin.New()
	r.Use(s.Middleware())
	r.GET("/admin", RequireRole(RoleAdmin, RoleTrader), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	u := &User{ID: 1, Username: "trader", Role: RoleTrader}
	access, _, err := s.IssueTokens(u)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireRole_NoUser_401(t *testing.T) {
	t.Parallel()
	r := gin.New()
	r.GET("/admin", RequireRole(RoleAdmin), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestServiceRequireRole_Disabled_AllowsEverything pins the short-circuit
// that makes Service.RequireRole usable behind Service.Middleware().
//
// This is the regression guard for AUD-02: if someone "simplifies"
// Service.RequireRole back to auth.RequireRole at a call site, the
// disabled-mode request below turns 401 and this test goes red.
func TestServiceRequireRole_Disabled_AllowsEverything(t *testing.T) {
	t.Parallel()
	// No JWTSecret => Enabled()==false => open access mode.
	s := NewService(nil, Config{})
	require.False(t, s.Enabled())

	r := gin.New()
	r.Use(s.Middleware())
	r.POST("/order", s.RequireRole(RoleTrader, RoleAdmin), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/order", nil)
	// Deliberately no Authorization header — open mode must not need one.
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code,
		"auth disabled must stay open: RequireRole has to no-op when Enabled()==false")
}

// TestServiceRequireRole_Enabled_StillEnforces is the counter-test:
// the short-circuit must not turn into "always allow".
func TestServiceRequireRole_Enabled_StillEnforces(t *testing.T) {
	t.Parallel()
	s := NewService(nil, Config{JWTSecret: []byte("test")})
	require.True(t, s.Enabled())

	r := gin.New()
	r.Use(s.Middleware())
	r.POST("/order", s.RequireRole(RoleTrader, RoleAdmin), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// viewer => 403
	viewer, _, err := s.IssueTokens(&User{ID: 1, Username: "v", Role: RoleViewer})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/order", nil)
	req.Header.Set("Authorization", "Bearer "+viewer)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// trader => 200
	trader, _, err := s.IssueTokens(&User{ID: 2, Username: "t", Role: RoleTrader})
	require.NoError(t, err)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/order", nil)
	req.Header.Set("Authorization", "Bearer "+trader)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// no token => 401
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/order", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuditMiddleware_RecordsMutating(t *testing.T) {
	// We can't easily wire the audit middleware without a real DB, but
	// we can verify the helper `isMutating` matches the documented set.
	t.Parallel()
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		assert.True(t, isMutating(m), "%s should be mutating", m)
	}
	for _, m := range []string{"GET", "HEAD", "OPTIONS"} {
		assert.False(t, isMutating(m), "%s should NOT be mutating", m)
	}
}
